// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"bytes"
	"io"
	"os"
	"sync"
)

// TeeWriter writes to an underlying writer AND captures a copy in a buffer.
// Used to capture stdout/stderr for the shell log while still showing output to the user.
// Implements os.File-like interface so programs like vim detect a real terminal.
type TeeWriter struct {
	mu     sync.Mutex
	writer io.Writer // original destination (os.Stdout / os.Stderr)
	file   *os.File  // the underlying file (for Fd() — terminal detection)
	buffer bytes.Buffer
	limit  int // max bytes to capture (0 = unlimited)
}

// Fd returns the file descriptor of the underlying writer.
// This is critical — programs like vim call isatty(fd) to detect terminals.
func (tw *TeeWriter) Fd() uintptr {
	if tw.file != nil {
		return tw.file.Fd()
	}
	return 0
}

func newTeeWriter(writer io.Writer, limit int) *TeeWriter {
	if limit == 0 {
		limit = 64 * 1024 // 64KB default
	}
	tw := &TeeWriter{writer: writer, limit: limit}
	// Preserve the underlying *os.File for terminal detection
	if f, ok := writer.(*os.File); ok {
		tw.file = f
	}
	return tw
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
