package ui

import (
	"fmt"
	"io"
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

// Link renders text as an OSC 8 terminal hyperlink. Terminals that do not
// understand it simply show the text, so this is safe everywhere.
func Link(url, text string) string {
	if url == "" {
		return text
	}
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

// ClearLines emits the escape sequence to move up n lines and clear them, for
// in-place refresh that preserves scrollback.
func ClearLines(w io.Writer, n int) {
	for i := 0; i < n; i++ {
		fmt.Fprint(w, "\x1b[1A\x1b[2K")
	}
}
