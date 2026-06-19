// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package llm

import (
	"bufio"
	"io"
	"net/http"
	"strings"
	"time"
)

// defaultClient is the shared HTTP client for all providers.
func defaultClient() *http.Client {
	return &http.Client{Timeout: 120 * time.Second}
}

// scanSSE reads Server-Sent Events from a reader, calling onLine for each
// "data: ..." line. Stops on "[DONE]" or when onLine returns false.
func scanSSE(body io.Reader, onLine func(data string) bool) {
	scanner := bufio.NewScanner(body)
	// Allow large SSE lines (default 64 KiB is too small for big chunks).
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		if !onLine(data) {
			break
		}
	}
}
