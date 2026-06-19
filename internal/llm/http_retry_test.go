// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package llm

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// noSleep replaces the backoff sleep with a no-op for the duration of a test so
// retry tests run instantly. Returns a restore function.
func noSleep(t *testing.T) {
	t.Helper()
	prev := sleepFunc
	sleepFunc = func(time.Duration) {}
	t.Cleanup(func() { sleepFunc = prev })
}

// sseOK writes a minimal OpenAI-style streaming success response.
func sseOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n")
	fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":1}}\n")
	fmt.Fprint(w, "data: [DONE]\n")
}

func newOpenAITestProvider(serverURL string, maxRetries int) *OpenAIProvider {
	return NewOpenAI(ProviderConfig{
		APIBase:      serverURL,
		APIKey:       "test",
		DefaultModel: "gpt-test",
		MaxRetries:   maxRetries,
	})
}

func collectStream(p *OpenAIProvider) (string, *Usage, error) {
	var content string
	var usage *Usage
	err := p.ChatStream(ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "x"}}}, func(c StreamChunk) {
		switch c.Type {
		case "content_delta":
			content += c.Content
		case "usage":
			usage = c.Usage
		}
	})
	return content, usage, err
}

func TestRetryOn429ThenSuccess(t *testing.T) {
	noSleep(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		sseOK(w)
	}))
	defer srv.Close()

	content, usage, err := collectStream(newOpenAITestProvider(srv.URL, 0))
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("expected 3 attempts (2x429 + success), got %d", got)
	}
	if content != "hi" {
		t.Fatalf("expected streamed content 'hi', got %q", content)
	}
	if usage == nil || usage.InputTokens != 3 || usage.OutputTokens != 1 {
		t.Fatalf("expected real provider usage 3/1, got %+v", usage)
	}
}

func TestRetryOn500ThenSuccess(t *testing.T) {
	noSleep(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		sseOK(w)
	}))
	defer srv.Close()

	if _, _, err := collectStream(newOpenAITestProvider(srv.URL, 0)); err != nil {
		t.Fatalf("expected success after 5xx retry, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected 2 attempts (1x500 + success), got %d", got)
	}
}

func TestNoRetryOn4xx(t *testing.T) {
	noSleep(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	_, _, err := collectStream(newOpenAITestProvider(srv.URL, 0))
	if err == nil {
		t.Fatal("expected error on 400, got nil")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("4xx must not be retried: expected 1 attempt, got %d", got)
	}
}

func TestRetryExhaustedFailsClosed(t *testing.T) {
	noSleep(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, _, err := collectStream(newOpenAITestProvider(srv.URL, 2))
	if err == nil {
		t.Fatal("expected error after retries exhausted, got nil")
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("expected maxRetries+1 = 3 attempts, got %d", got)
	}
}

func TestRetriesDisabled(t *testing.T) {
	noSleep(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		sseOK(w)
	}))
	defer srv.Close()

	// MaxRetries < 0 disables retries: the first 429 surfaces immediately.
	_, _, err := collectStream(newOpenAITestProvider(srv.URL, -1))
	if err == nil {
		t.Fatal("expected error with retries disabled, got nil")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("retries disabled: expected 1 attempt, got %d", got)
	}
}

func TestRetryableStatus(t *testing.T) {
	for _, code := range []int{429, 500, 502, 503, 504} {
		if !retryableStatus(code) {
			t.Errorf("status %d should be retryable", code)
		}
	}
	for _, code := range []int{200, 400, 401, 403, 404, 422} {
		if retryableStatus(code) {
			t.Errorf("status %d should NOT be retryable", code)
		}
	}
}

func TestBackoffHonorsRetryAfter(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", "2")
	if got := backoffDelay(1, resp); got != 2*time.Second {
		t.Errorf("expected 2s from Retry-After, got %v", got)
	}
	// Retry-After above the cap is clamped.
	resp.Header.Set("Retry-After", "999")
	if got := backoffDelay(1, resp); got != maxBackoff {
		t.Errorf("expected Retry-After clamped to %v, got %v", maxBackoff, got)
	}
	// No header → exponential, capped.
	if got := backoffDelay(1, nil); got != 1*time.Second {
		t.Errorf("attempt 1 → 1s, got %v", got)
	}
	if got := backoffDelay(3, nil); got != 4*time.Second {
		t.Errorf("attempt 3 → 4s, got %v", got)
	}
	if got := backoffDelay(10, nil); got != maxBackoff {
		t.Errorf("attempt 10 → capped %v, got %v", maxBackoff, got)
	}
}

func TestResolveMaxRetries(t *testing.T) {
	if got := resolveMaxRetries(0); got != defaultMaxRetries {
		t.Errorf("0 → default %d, got %d", defaultMaxRetries, got)
	}
	if got := resolveMaxRetries(-1); got != 0 {
		t.Errorf("negative → 0, got %d", got)
	}
	if got := resolveMaxRetries(5); got != 5 {
		t.Errorf("5 → 5, got %d", got)
	}
}
