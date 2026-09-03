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

// visibleLen ignores ANSI escape sequences when measuring width.
func visibleLen(s string) int {
	n, inEsc := 0, false
	for _, r := range s {
		switch {
		case inEsc:
			if r == 'm' {
				inEsc = false
			}
		case r == '\x1b':
			inEsc = true
		default:
			n++
		}
	}
	return n
}

// ClearLines emits the escape sequence to move up n lines and clear them, for
// in-place refresh that preserves scrollback.
func ClearLines(w io.Writer, n int) {
	for i := 0; i < n; i++ {
		fmt.Fprint(w, "\x1b[1A\x1b[2K")
	}
}
