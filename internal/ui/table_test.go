package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestTableAlignsColumns(t *testing.T) {
	tb := NewTable("ALIAS", "STATUS")
	tb.AddRow("api", "Running")
	tb.AddRow("search", "CrashLoopBackOff")

	var out bytes.Buffer
	tb.Render(&out)
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want header + 2 rows, got %d: %q", len(lines), out.String())
	}
	// every STATUS starts at the same column
	col := strings.Index(lines[0], "STATUS")
	for i, l := range lines[1:] {
		if !strings.HasPrefix(l[col:], strings.Fields(l)[1]) {
			t.Errorf("row %d misaligned: %q (status column %d)", i, l, col)
		}
	}
}

func TestTableLineCount(t *testing.T) {
	tb := NewTable("A")
	tb.AddRow("1")
	tb.AddRow("2")
	if got := tb.Lines(); got != 3 {
		t.Errorf("Lines = %d, want 3", got)
	}
}

// A coloured cell is wider in bytes than on screen; alignment must use the
// visible width or every row after a coloured one drifts.
func TestTableAlignsAroundANSI(t *testing.T) {
	tb := NewTable("ALIAS", "STATUS")
	tb.AddRow("api", Green+"Running"+Reset)
	tb.AddRow("search", "Pending")

	var out bytes.Buffer
	tb.Render(&out)
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	col := strings.Index(lines[0], "STATUS")
	if got := visibleLen(lines[1][:strings.Index(lines[1], "\x1b")]); got != col {
		t.Errorf("coloured row's status starts at visible column %d, want %d", got, col)
	}
}
