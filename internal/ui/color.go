// Package ui renders kmap's terminal output.
package ui

import (
	"os"
)

const (
	Reset  = "\x1b[0m"
	Bold   = "\x1b[1m"
	Gray   = "\x1b[90m"
	Red    = "\x1b[31m"
	Green  = "\x1b[32m"
	Yellow = "\x1b[33m"
)

// palette cycles through readable foreground colours for per-alias prefixes.
var palette = []string{
	"\x1b[36m", "\x1b[35m", "\x1b[32m", "\x1b[33m", "\x1b[34m", "\x1b[91m",
}

// Color returns a stable colour for index i.
func Color(i int) string { return palette[i%len(palette)] }

// Hyperlinks reports whether output can carry OSC 8 terminal hyperlinks. It is
// false when stdout is redirected, so a piped or captured run emits plain URLs
// that grep and copy-paste can use, and false under NO_COLOR.
var Hyperlinks = stdoutIsTerminal()

func stdoutIsTerminal() bool {
	if NoColor() {
		return false
	}
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// NoColor reports whether colour should be suppressed.
func NoColor() bool { return os.Getenv("NO_COLOR") != "" }
