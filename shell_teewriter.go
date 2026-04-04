// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"bytes"
	"io"
	"os"
	"sync"
)

type TeeWriter struct {
	mu     sync.Mutex
	writer io.Writer
	file   *os.File // underlying file for Fd() -- terminal detection
	buffer bytes.Buffer
	limit  int
}

func (tw *TeeWriter) Fd() uintptr {
	if tw.file != nil {
		return tw.file.Fd()
	}
	return 0
}

func newTeeWriter(writer io.Writer, limit int) *TeeWriter {
	if limit == 0 {
		limit = 64 * 1024
	}
	tw := &TeeWriter{writer: writer, limit: limit}
	if f, ok := writer.(*os.File); ok {
		tw.file = f
	}
	return tw
}

func (tw *TeeWriter) Write(p []byte) (int, error) {
	n, err := tw.writer.Write(p)

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

func (tw *TeeWriter) String() string {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	s := tw.buffer.String()
	tw.buffer.Reset()
	return s
}

func (tw *TeeWriter) Reset() {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	tw.buffer.Reset()
}
