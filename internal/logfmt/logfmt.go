// Package logfmt renders structured JSON log lines as coloured text. Lines
// that are not JSON pass through untouched, so it is safe on any stream.
//
// This is a port of the jq program kmap replaces. It matches that program's
// output for every line the services actually emit, and diverges only where jq
// would have raised an error: a JSON object with no timestamp, an error object
// with no stack, or a timestamp carrying a numeric offset instead of Z all pass
// through or render here rather than killing the pipe.
package logfmt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aymenkrifa/kmap/internal/ui"
)

const escReset = "\x1b[0m"

var levelColor = map[string]string{
	"debug":    "\x1b[36m",
	"info":     "\x1b[32m",
	"warning":  "\x1b[33m",
	"error":    "\x1b[31m",
	"critical": "\x1b[91m",
}

// Colors follows the same rule as the rest of kmap's output: a redirected run
// emits no escapes, so `kmap logs -j | grep` sees clean text.
var Colors = ui.Colors

// SetColor turns this formatter's colour on or off.
func SetColor(on bool) { Colors = on }

// col returns an escape sequence, or nothing when colour is off.
func col(code string) string {
	if !Colors {
		return ""
	}
	return code
}

func reset() string { return col(escReset) }

// httpTail matches a message ending in `" <three digits>`, the shape uvicorn
// access logs use. The code is recoloured in place; nothing is appended.
var httpTail = regexp.MustCompile(`^(.*" )([0-9]{3})$`)

type entry struct {
	Timestamp string `json:"timestamp"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	Error     *struct {
		Stack string `json:"stack"`
	} `json:"error"`
}

// Format renders one line. Input that is not a JSON object with a timestamp is
// returned unchanged.
func Format(line string) string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "{") {
		return line
	}
	var e entry
	if err := json.Unmarshal([]byte(trimmed), &e); err != nil || e.Timestamp == "" {
		return line
	}

	var b strings.Builder
	b.WriteString(formatTime(e.Timestamp))
	b.WriteString(" - ")
	b.WriteString(col(levelColor[strings.ToLower(e.Status)]))
	b.WriteString(strings.ToUpper(e.Status))
	b.WriteString(reset())
	b.WriteString(" - ")
	b.WriteString(colorHTTP(e.Message))
	if e.Error != nil && e.Error.Stack != "" {
		b.WriteString("\n")
		b.WriteString(e.Error.Stack)
	}
	return b.String()
}

// formatTime renders an RFC3339 timestamp as DD-MM-YYYY HH:MM:SS, dropping
// fractional seconds. Unparseable input is returned as-is.
func formatTime(ts string) string {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, ts); err == nil {
			return t.Format("02-01-2006 15:04:05")
		}
	}
	return ts
}

func colorHTTP(msg string) string {
	m := httpTail.FindStringSubmatch(msg)
	if m == nil {
		return msg
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return msg
	}
	var c string
	switch {
	case n >= 500:
		c = "\x1b[91m"
	case n >= 400:
		c = "\x1b[31m"
	case n >= 300:
		c = "\x1b[33m"
	case n >= 200:
		c = "\x1b[32m"
	default:
		c = "\x1b[97m"
	}
	return fmt.Sprintf("%s%s%s%s", m[1], col(c), m[2], reset())
}

// Writer formats each complete line written to it. Partial writes are buffered
// until their newline arrives, so a stream split mid-line still renders.
type Writer struct {
	w   io.Writer
	buf bytes.Buffer
}

func NewWriter(w io.Writer) *Writer { return &Writer{w: w} }

func (w *Writer) Write(p []byte) (int, error) {
	w.buf.Write(p)
	for {
		line, err := w.buf.ReadString('\n')
		if err != nil {
			// no complete line left; put the remainder back
			w.buf.Reset()
			w.buf.WriteString(line)
			return len(p), nil
		}
		if _, err := io.WriteString(w.w, Format(strings.TrimSuffix(line, "\n"))+"\n"); err != nil {
			return len(p), err
		}
	}
}

// Close flushes a trailing line that never received a newline.
func (w *Writer) Close() error {
	if w.buf.Len() == 0 {
		return nil
	}
	_, err := io.WriteString(w.w, Format(w.buf.String())+"\n")
	w.buf.Reset()
	return err
}
