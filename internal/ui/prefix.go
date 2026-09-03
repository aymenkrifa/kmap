package ui

import (
	"bytes"
	"io"
	"strings"
	"sync"
)

// SyncWriter serialises writes from several goroutines onto one stream.
type SyncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func NewSyncWriter(w io.Writer) *SyncWriter { return &SyncWriter{w: w} }

func (s *SyncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// PrefixWriter prefixes every complete line. Partial writes are held until
// their newline arrives, so interleaved streams never split a line.
type PrefixWriter struct {
	w      io.Writer
	prefix string
	mu     sync.Mutex
	buf    bytes.Buffer
}

func NewPrefixWriter(w io.Writer, prefix string) *PrefixWriter {
	return &PrefixWriter{w: w, prefix: prefix}
}

func (p *PrefixWriter) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.buf.Write(b)
	for {
		line, err := p.buf.ReadString('\n')
		if err != nil {
			p.buf.Reset()
			p.buf.WriteString(line)
			return len(b), nil
		}
		if _, err := io.WriteString(p.w, p.prefix+line); err != nil {
			return len(b), err
		}
	}
}

func (p *PrefixWriter) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.buf.Len() == 0 {
		return nil
	}
	_, err := io.WriteString(p.w, p.prefix+strings.TrimSuffix(p.buf.String(), "\n")+"\n")
	p.buf.Reset()
	return err
}
