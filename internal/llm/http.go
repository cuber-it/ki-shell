// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package llm

import (
	"bufio"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// defaultClient is the shared HTTP client for all providers.
func defaultClient() *http.Client {
	return &http.Client{Timeout: 120 * time.Second}
}

// sleepFunc is the backoff sleep, indirected so tests can make it a no-op.
var sleepFunc = time.Sleep

const (
	// defaultMaxRetries is used when ProviderConfig.MaxRetries is 0.
	defaultMaxRetries = 3
	// maxBackoff caps a single backoff wait so an interactive shell never
	// blocks for too long between attempts.
	maxBackoff = 8 * time.Second
)

// resolveMaxRetries maps a ProviderConfig value to an effective retry count:
// 0 → default, negative → disabled (0 retries).
func resolveMaxRetries(configured int) int {
	if configured == 0 {
		return defaultMaxRetries
	}
	if configured < 0 {
		return 0
	}
	return configured
}

// retryableStatus reports whether an HTTP status code warrants a retry: rate
// limiting (429) and server errors (5xx) are transient; 4xx (bad request, auth)
// are not and must surface immediately.
func retryableStatus(code int) bool {
	return code == http.StatusTooManyRequests || (code >= 500 && code <= 599)
}

// backoffDelay computes the wait before the next attempt. It honors a numeric
// Retry-After header (seconds) when the server sent one, otherwise falls back to
// exponential backoff (1s, 2s, 4s …) capped at maxBackoff. attempt is 1-based.
func backoffDelay(attempt int, resp *http.Response) time.Duration {
	if resp != nil {
		if raw := strings.TrimSpace(resp.Header.Get("Retry-After")); raw != "" {
			if secs, err := strconv.Atoi(raw); err == nil && secs > 0 {
				wait := time.Duration(secs) * time.Second
				if wait > maxBackoff {
					wait = maxBackoff
				}
				return wait
			}
		}
	}
	wait := time.Duration(1<<uint(attempt-1)) * time.Second
	if wait > maxBackoff {
		wait = maxBackoff
	}
	return wait
}

// doWithRetry issues the request produced by newReq, retrying on connection
// errors and 429/5xx responses with backoff.
//
// Safety: it only ever returns BEFORE the response body is consumed. A retried
// request has not yet produced a single token, so retrying can never cause
// double cost or duplicated output — the caller must NOT retry once it has begun
// reading the stream. The request body is rebuilt per attempt via newReq, so it
// is always replayable. On a non-retryable status (incl. success) it returns the
// live response for the caller to inspect; after exhausting retries it returns
// the last response or error so the caller fails closed.
func doWithRetry(client *http.Client, newReq func() (*http.Request, error), maxRetries int) (*http.Response, error) {
	var resp *http.Response
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			sleepFunc(backoffDelay(attempt, resp))
		}
		req, err := newReq()
		if err != nil {
			return nil, err // construction errors are deterministic, not retryable
		}
		resp, lastErr = client.Do(req)
		if lastErr != nil {
			continue // connection-level error → retry
		}
		if retryableStatus(resp.StatusCode) && attempt < maxRetries {
			resp.Body.Close() // discard the error body; we will retry
			continue
		}
		return resp, nil // success, or a non-retryable / final response
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return resp, nil
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
