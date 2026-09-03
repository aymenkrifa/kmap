// Package ui renders kmap's terminal output.
package ui

import (
	"os"
)

// The escape sequences themselves. Nothing outside this file uses them
// directly: the exported names below are blank when colour is off.
const (
	escReset  = "\x1b[0m"
	escBold   = "\x1b[1m"
	escGray   = "\x1b[90m"
	escRed    = "\x1b[31m"
	escGreen  = "\x1b[32m"
	escYellow = "\x1b[33m"
)

// Colour used by kmap's output. These are variables, not constants, so that a
// redirected run emits none of them — see SetColor.
var (
	Reset  string
	Bold   string
	Gray   string
	Red    string
	Green  string
	Yellow string
)

// palette cycles through readable foreground colours for per-alias prefixes.
var palette []string

// Colors reports whether ANSI colour is being emitted.
var Colors bool

// Hyperlinks reports whether output can carry OSC 8 terminal hyperlinks. It is
// false when stdout is redirected, so a piped or captured run emits plain URLs
// that grep and copy-paste can use, and false under NO_COLOR.
var Hyperlinks = stdoutIsTerminal()

func init() { SetColor(stdoutIsTerminal()) }

// SetColor turns colour on or off. Off blanks every sequence, so a piped run
// carries no escapes at all rather than escapes a pager or grep has to strip.
func SetColor(on bool) {
	Colors = on
	if on {
		Reset, Bold, Gray = escReset, escBold, escGray
		Red, Green, Yellow = escRed, escGreen, escYellow
		palette = []string{
			"\x1b[36m", "\x1b[35m", "\x1b[32m", "\x1b[33m", "\x1b[34m", "\x1b[91m",
		}
		return
	}
	Reset, Bold, Gray, Red, Green, Yellow = "", "", "", "", "", ""
	palette = []string{"", "", "", "", "", ""}
}

// Color returns a stable colour for index i.
func Color(i int) string { return palette[i%len(palette)] }

// NoColor reports whether colour should be suppressed.
func NoColor() bool { return os.Getenv("NO_COLOR") != "" }

func stdoutIsTerminal() bool {
	if NoColor() {
		return false
	}
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
