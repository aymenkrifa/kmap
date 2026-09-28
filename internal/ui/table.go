package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Table renders fixed-width columns.
type Table struct {
	header []string
	rows   [][]string
}

func NewTable(header ...string) *Table { return &Table{header: header} }

func (t *Table) AddRow(cells ...string) { t.rows = append(t.rows, cells) }

// Lines is how many lines Render will emit, which watch mode needs in order to
// move the cursor back up.
func (t *Table) Lines() int { return len(t.rows) + 1 }

func (t *Table) Render(w io.Writer) {
	widths := make([]int, len(t.header))
	for i, h := range t.header {
		widths[i] = len(h)
	}
	for _, r := range t.rows {
		for i, c := range r {
			if i < len(widths) && visibleLen(c) > widths[i] {
				widths[i] = visibleLen(c)
			}
		}
	}
	writeRow(w, t.header, widths)
	for _, r := range t.rows {
		writeRow(w, r, widths)
	}
}

func writeRow(w io.Writer, cells []string, widths []int) {
	var b strings.Builder
	for i, c := range cells {
		b.WriteString(c)
		if i < len(cells)-1 && i < len(widths) {
			b.WriteString(strings.Repeat(" ", widths[i]-visibleLen(c)+2))
		}
	}
	fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
}

// visibleLen measures how wide a cell prints, ignoring escape sequences. It
// handles both CSI colour codes (ESC [ ... m) and OSC 8 hyperlinks
// (ESC ] 8 ; ; uri ST), whose URI is not displayed at all.
func visibleLen(s string) int {
	n := 0
	for i := 0; i < len(s); {
		if s[i] != 0x1b {
			if s[i]&0xC0 != 0x80 { // count runes, not continuation bytes
				n++
			}
			i++
			continue
		}
		i++
		if i >= len(s) {
			break
		}
		switch s[i] {
		case '[': // CSI: runs to the first byte in @-~
			i++
			for i < len(s) && (s[i] < '@' || s[i] > '~') {
				i++
			}
			i++
		case ']': // OSC: runs to BEL or ST (ESC \)
			i++
			for i < len(s) {
				if s[i] == 0x07 {
					i++
					break
				}
				if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
					i += 2
					break
				}
				i++
			}
		default:
			i++
		}
	}
	return n
}

// Link renders text as an OSC 8 terminal hyperlink, so the cell stays narrow
// while the whole URL is what gets opened. When stdout is not a terminal the
// escape codes would just be noise in a pipe, so the plain URL is emitted
// instead — that is the form worth grepping for anyway.
func Link(url, text string) string {
	if url == "" {
		return text
	}
	if !Hyperlinks {
		return url
	}
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

// Redraw paints frame over the whole screen in one write, so the terminal
// never shows a half-cleared screen between two frames. It homes the cursor
// rather than counting lines back up, so anything else that printed in the
// meantime is painted over instead of shifting the table. Lines are cleared to
// their end, then anything left below from a longer previous frame is erased.
// Meant for the screen FullScreen switches to.
func Redraw(w io.Writer, frame string) {
	var b strings.Builder
	b.WriteString("\x1b[H")
	for _, l := range strings.Split(strings.TrimSuffix(frame, "\n"), "\n") {
		b.WriteString(l)
		b.WriteString("\x1b[K\n")
	}
	b.WriteString("\x1b[J")
	io.WriteString(w, b.String())
}

// FullScreen switches to the terminal's alternate screen, as watch and top do,
// with auto-wrap off so a line wider than the terminal is clipped instead of
// spilling onto another row, and the cursor hidden. The returned func puts the
// normal screen, wrapping and cursor back, leaving scrollback as it was.
func FullScreen(w io.Writer) (restore func()) {
	io.WriteString(w, "\x1b[?1049h\x1b[?7l\x1b[?25l")
	return func() { io.WriteString(w, "\x1b[?25h\x1b[?7h\x1b[?1049l") }
}

// IsTerminal reports whether w is a terminal, and so can take cursor movement.
func IsTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
