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

func TestVisibleLenIgnoresHyperlinksAndColour(t *testing.T) {
	saved := Hyperlinks
	Hyperlinks = true // measure the escaped form, whatever stdout is here
	defer func() { Hyperlinks = saved }()

	cases := []struct {
		in   string
		want int
	}{
		{"plain", 5},
		{Green + "Running" + Reset, 7},
		{Link("https://api.example.com/", "api.example.com"), 15},
		{Green + Link("https://x/", "abc") + Reset, 3},
		{"—", 1},
	}
	for _, c := range cases {
		if got := visibleLen(c.in); got != c.want {
			t.Errorf("visibleLen(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestTableAlignsAroundHyperlinks(t *testing.T) {
	saved := Hyperlinks
	Hyperlinks = true
	defer func() { Hyperlinks = saved }()

	tb := NewTable("ALIAS", "URL", "AGE")
	tb.AddRow("api", Link("https://api.example.com/", "api.example.com"), "29h")
	tb.AddRow("web", "cdn-next.example.com", "1h")
	var out bytes.Buffer
	tb.Render(&out)

	// AGE must start at the same visible column on every line, even though the
	// hyperlinked row carries a URL that prints as nothing.
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	want := -1
	for i, l := range lines {
		idx := strings.LastIndex(l, "  ")
		if idx < 0 {
			t.Fatalf("line %d has no column gap: %q", i, l)
		}
		got := visibleLen(l[:idx+2])
		if want == -1 {
			want = got
		} else if got != want {
			t.Errorf("line %d starts its last column at %d, want %d: %q", i, got, want, l)
		}
	}
}

func TestLinkFallsBackToPlainURLOffTerminal(t *testing.T) {
	saved := Hyperlinks
	defer func() { Hyperlinks = saved }()

	Hyperlinks = true
	got := Link("https://api.example.com/", "api.example.com")
	if !strings.HasPrefix(got, "\x1b]8;;https://api.example.com/") {
		t.Errorf("on a terminal, want an OSC 8 link, got %q", got)
	}
	if visibleLen(got) != len("api.example.com") {
		t.Errorf("a link should print only its text, got width %d", visibleLen(got))
	}

	// piped: escape codes would be noise, and the full URL is what you grep for
	Hyperlinks = false
	got = Link("https://api.example.com/", "api.example.com")
	if got != "https://api.example.com/" {
		t.Errorf("off a terminal, want the plain URL, got %q", got)
	}
	if strings.Contains(got, "\x1b") {
		t.Errorf("piped output must carry no escapes: %q", got)
	}
}
