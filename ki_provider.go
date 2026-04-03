// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
// ki_provider.go — Adapter between kish's KIEngine interface and heinzel's Provider library.
// Uses github.com/cuber-it/heinzel-ai-core-go/provider for OpenAI, Anthropic, etc.
// No custom HTTP code — that's all in heinzel.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cuber-it/heinzel-ai-core-go/provider"
)

// ProviderEngine wraps a heinzel Provider as a kish KIEngine.
type ProviderEngine struct {
	provider          provider.Provider
	model             string
	db                *provider.DB
	config            provider.ProviderConfig
	sysPromptOverride string
}

// NewProviderEngine creates a KIEngine from a heinzel Provider.
func NewProviderEngine(p provider.Provider, cfg provider.ProviderConfig) *ProviderEngine {
	model := cfg.DefaultModel
	if model == "" {
		model = p.DefaultModel()
	}

	// Cost tracking DB in ~/.kish/
	dbPath := filepath.Join(kishDir(), "costs.db")
	db, err := provider.NewDB("file:"+dbPath, p.Name())
	if err != nil {
		fmt.Fprintf(os.Stderr, "kish: cost db error: %s\n", err)
	}

	return &ProviderEngine{
		provider: p,
		model:    model,
		db:       db,
		config:   cfg,
	}
}

func (e *ProviderEngine) Available() bool {
	return e.provider.Health().OK
}

func (e *ProviderEngine) Name() string {
	return fmt.Sprintf("%s/%s", e.provider.Name(), e.model)
}

// SetSystemPromptOverride temporarily overrides the system prompt.
// Pass empty string to reset to default.
func (e *ProviderEngine) SetSystemPromptOverride(prompt string) {
	e.sysPromptOverride = prompt
}

func (e *ProviderEngine) Close() {
	if e.db != nil {
		e.db.Close()
	}
}

func (e *ProviderEngine) Query(ctx context.Context, input string, shellCtx ShellContext, out io.Writer) (*KIResponse, error) {
	customPrompt := e.sysPromptOverride
	sysPrompt := buildSystemPrompt(shellCtx, kiMemory, customPrompt)
	vSystemPrompt(sysPrompt)
	vKIRequest(input)

	// Build messages from conversation history
	var messages []provider.ChatMessage
	messages = append(messages, provider.ChatMessage{Role: "system", Content: sysPrompt})
	for _, turn := range kiConversation.Recent() {
		messages = append(messages, provider.ChatMessage{Role: "user", Content: turn.UserInput})
		messages = append(messages, provider.ChatMessage{Role: "assistant", Content: turn.Response})
	}
	messages = append(messages, provider.ChatMessage{Role: "user", Content: input})

	req := provider.ChatRequest{
		Model:    e.model,
		Messages: messages,
		Stream:   true,
	}

	// Stream response
	start := time.Now()
	var fullText strings.Builder
	var usage provider.Usage

	err := e.provider.ChatStream(req, func(chunk provider.StreamChunk) {
		switch chunk.Type {
		case "content_delta":
			fmt.Fprint(out, chunk.Content)
			fullText.WriteString(chunk.Content)
		case "usage":
			if chunk.Usage != nil {
				usage = *chunk.Usage
			}
		case "error":
			fmt.Fprintf(os.Stderr, "\nkish: stream error: %s\n", chunk.Error)
		}
	})

	latency := time.Since(start)
	fmt.Fprintln(out) // newline after streamed output

	// Log usage
	if e.db != nil {
		cost := e.config.CostForTokens(e.model, usage.InputTokens, usage.OutputTokens)
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		e.db.LogUsage(e.model, usage.InputTokens, usage.OutputTokens, latency.Milliseconds(), status, errMsg, "", cost)
	}

	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}

	responseText := fullText.String()
	kiConversation.Add(input, responseText)
	vKIResponse(responseText)

	suggestedCmd := extractCommand(responseText)

	return &KIResponse{
		Text:             responseText,
		SuggestedCommand: suggestedCmd,
		Confidence:       -1,
		TokensUsed:       usage.InputTokens + usage.OutputTokens,
	}, nil
}

// extractCommand finds a ```bash block in the response and returns the command
func extractCommand(text string) string {
	start := strings.Index(text, "```bash\n")
	if start < 0 {
		start = strings.Index(text, "```sh\n")
		if start < 0 {
			return ""
		}
		start += 6
	} else {
		start += 8
	}
	end := strings.Index(text[start:], "```")
	if end < 0 {
		return ""
	}
	cmd := strings.TrimSpace(text[start : start+end])
	if strings.Contains(cmd, "\n") {
		return ""
	}
	return cmd
}

// CostStats returns cost tracking data for display
func (e *ProviderEngine) TodayStats() *provider.UsageSummary {
	if e.db == nil {
		return nil
	}
	stats := e.db.TodayStats()
	return &stats
}

func (e *ProviderEngine) TotalStats() (int, int64, int64, float64) {
	if e.db == nil {
		return 0, 0, 0, 0
	}
	return e.db.Stats()
}

func (e *ProviderEngine) RecentRequests(n int) []map[string]interface{} {
	if e.db == nil {
		return nil
	}
	return e.db.RecentRequests(n)
}
