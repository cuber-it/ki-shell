// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"bytes"
	"io"
	"sync"
)

// TeeWriter writes to an underlying writer AND captures a copy in a buffer.
// Used to capture stdout/stderr for the shell log while still showing output to the user.
type TeeWriter struct {
	mu     sync.Mutex
	writer io.Writer // original destination (os.Stdout / os.Stderr)
	buffer bytes.Buffer
	limit  int // max bytes to capture (0 = unlimited)
}

func newTeeWriter(writer io.Writer, limit int) *TeeWriter {
	if limit == 0 {
		limit = 64 * 1024 // 64KB default
	}
	return &TeeWriter{writer: writer, limit: limit}
}

func (tw *TeeWriter) Write(p []byte) (int, error) {
	// Always write to the original destination
	n, err := tw.writer.Write(p)

	// Capture in buffer (up to limit)
	tw.mu.Lock()
	if tw.buffer.Len() < tw.limit {
		remaining := tw.limit - tw.buffer.Len()
		if len(p) > remaining {
			tw.buffer.Write(p[:remaining])
		} else {
			tw.buffer.Write(p)
		}
	}
	tw.mu.Unlock()

	return n, err
}

// String returns the captured output and resets the buffer
func (tw *TeeWriter) String() string {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	s := tw.buffer.String()
	tw.buffer.Reset()
	return s
}

// Reset clears the captured buffer
func (tw *TeeWriter) Reset() {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	tw.buffer.Reset()
}
