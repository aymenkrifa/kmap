package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrefixWriterPrefixesCompleteLines(t *testing.T) {
	var out bytes.Buffer
	w := NewPrefixWriter(&out, "[api] ")
	w.Write([]byte("one\ntwo\n"))
	want := "[api] one\n[api] two\n"
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}

func TestPrefixWriterBuffersPartialLines(t *testing.T) {
	var out bytes.Buffer
	w := NewPrefixWriter(&out, "[api] ")
	w.Write([]byte("par"))
	if out.Len() != 0 {
		t.Fatalf("emitted early: %q", out.String())
	}
	w.Write([]byte("tial\n"))
	if out.String() != "[api] partial\n" {
		t.Errorf("got %q", out.String())
	}
}

func TestPrefixWriterCloseFlushes(t *testing.T) {
	var out bytes.Buffer
	w := NewPrefixWriter(&out, "[api] ")
	w.Write([]byte("no newline"))
	w.Close()
	if !strings.Contains(out.String(), "[api] no newline") {
		t.Errorf("got %q", out.String())
	}
}

func TestPrefixWriterIsConcurrencySafe(t *testing.T) {
	var out bytes.Buffer
	shared := NewSyncWriter(&out)
	a := NewPrefixWriter(shared, "[a] ")
	b := NewPrefixWriter(shared, "[b] ")

	done := make(chan struct{}, 2)
	go func() {
		for i := 0; i < 100; i++ {
			a.Write([]byte("x\n"))
		}
		done <- struct{}{}
	}()
	go func() {
		for i := 0; i < 100; i++ {
			b.Write([]byte("y\n"))
		}
		done <- struct{}{}
	}()
	<-done
	<-done

	if n := strings.Count(out.String(), "\n"); n != 200 {
		t.Errorf("got %d lines, want 200", n)
	}
	// no line may carry both prefixes: that would mean a torn write
	for _, l := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
		if strings.Contains(l, "[a] ") && strings.Contains(l, "[b] ") {
			t.Fatalf("interleaved line: %q", l)
		}
	}
}
