// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"io"
	"strings"
	"sync"
)

// FilterWriter wraps a writer and suppresses lines matching known patterns.
// Used during .bashrc loading to silence mvdan/sh incompatibility warnings.
type FilterWriter struct {
	mu       sync.Mutex
	writer   io.Writer
	suppress bool
	patterns []string
}

var bashrcSuppressPatterns = []string{
	"complete: unimplemented",
	"shopt: unsupported option",
	"shopt: invalid option",
	"set: invalid option",
	"return: can only be done",
	"extended globbing operator",
	"alias: could not parse",
	"not a valid word",
	"unimplemented builtin",
}

func newFilterWriter(writer io.Writer, suppress bool) *FilterWriter {
	return &FilterWriter{
		writer:   writer,
		suppress: suppress,
		patterns: bashrcSuppressPatterns,
	}
}

func (fw *FilterWriter) Write(p []byte) (int, error) {
	if !fw.suppress {
		return fw.writer.Write(p)
	}

	fw.mu.Lock()
	defer fw.mu.Unlock()

	text := string(p)
	for _, line := range strings.Split(text, "\n") {
		if shouldSuppress(line, fw.patterns) {
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		fw.writer.Write([]byte(line + "\n"))
	}
	return len(p), nil
}

func (fw *FilterWriter) SetSuppress(suppress bool) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.suppress = suppress
}

func shouldSuppress(line string, patterns []string) bool {
	lower := strings.ToLower(line)
	for _, pattern := range patterns {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}
