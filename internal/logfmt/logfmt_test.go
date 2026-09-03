package logfmt

import (
	"bytes"
	"strings"
	"testing"
)

func TestFormatPassesThroughNonJSON(t *testing.T) {
	in := "INFO:     Uvicorn running on http://0.0.0.0:80"
	if got := Format(in); got != in {
		t.Errorf("Format(%q) = %q, want unchanged", in, got)
	}
}

func TestFormatMalformedJSONPassesThrough(t *testing.T) {
	in := `{"timestamp": "broken`
	if got := Format(in); got != in {
		t.Errorf("Format(%q) = %q, want unchanged", in, got)
	}
}

func TestFormatRendersTimestampAndLevel(t *testing.T) {
	in := `{"timestamp":"2026-09-03T19:14:02.123456Z","status":"info","message":"ready"}`
	got := Format(in)
	if !strings.HasPrefix(got, "03-09-2026 19:14:02 - ") {
		t.Errorf("timestamp not reformatted: %q", got)
	}
	if !strings.Contains(got, "INFO") {
		t.Errorf("level not uppercased: %q", got)
	}
	if !strings.Contains(got, "\x1b[32m") {
		t.Errorf("info should be green: %q", got)
	}
	if !strings.HasSuffix(got, "ready") {
		t.Errorf("message missing: %q", got)
	}
}

func TestFormatLevelColors(t *testing.T) {
	for status, want := range map[string]string{
		"debug":    "\x1b[36m",
		"info":     "\x1b[32m",
		"warning":  "\x1b[33m",
		"error":    "\x1b[31m",
		"critical": "\x1b[91m",
	} {
		in := `{"timestamp":"2026-09-03T19:14:02Z","status":"` + status + `","message":"m"}`
		if got := Format(in); !strings.Contains(got, want) {
			t.Errorf("status %s: %q missing %q", status, got, want)
		}
	}
}

func TestFormatRecolorsHTTPStatusInPlace(t *testing.T) {
	for code, want := range map[string]string{
		"204": "\x1b[32m",
		"301": "\x1b[33m",
		"404": "\x1b[31m",
		"503": "\x1b[91m",
	} {
		in := `{"timestamp":"2026-09-03T19:14:02Z","status":"info","message":"GET /x \" ` + code + `"}`
		got := Format(in)
		if !strings.Contains(got, want+code) {
			t.Errorf("code %s: %q should colour the code with %q", code, got, want)
		}
		// nothing appended after the code and its reset
		if !strings.HasSuffix(got, code+"\x1b[0m") {
			t.Errorf("code %s: %q should end at the code", code, got)
		}
	}
}

func TestFormatAppendsErrorStack(t *testing.T) {
	in := `{"timestamp":"2026-09-03T19:14:02Z","status":"error","message":"boom","error":{"stack":"line1\nline2"}}`
	got := Format(in)
	if !strings.HasSuffix(got, "\nline1\nline2") {
		t.Errorf("stack not appended: %q", got)
	}
}

func TestWriterFormatsCompleteLinesOnly(t *testing.T) {
	var out bytes.Buffer
	w := NewWriter(&out)

	w.Write([]byte(`{"timestamp":"2026-09-03T19:14:02Z","status":"info","message":"a"}` + "\n"))
	w.Write([]byte(`{"timestamp":"2026-09-03T19:14:03Z",`)) // partial: must not emit
	if strings.Count(out.String(), "\n") != 1 {
		t.Fatalf("partial line emitted early: %q", out.String())
	}
	w.Write([]byte(`"status":"info","message":"b"}` + "\n"))
	if strings.Count(out.String(), "\n") != 2 {
		t.Fatalf("reassembled line missing: %q", out.String())
	}
	if !strings.Contains(out.String(), "a") || !strings.Contains(out.String(), "b") {
		t.Errorf("out = %q", out.String())
	}
}

func TestWriterCloseFlushesTrailingPartial(t *testing.T) {
	var out bytes.Buffer
	w := NewWriter(&out)
	w.Write([]byte("no trailing newline"))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no trailing newline") {
		t.Errorf("Close did not flush: %q", out.String())
	}
}
