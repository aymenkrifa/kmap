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

// NoColor reports whether colour should be suppressed.
func NoColor() bool { return os.Getenv("NO_COLOR") != "" }
