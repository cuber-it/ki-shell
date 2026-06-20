// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
// Adapter between aish's KIEngine interface and aish's own slim LLM layer
// (internal/llm) for OpenAI and Anthropic. No external provider dependency.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cuber-it/ki-shell/internal/llm"
)

type ProviderEngine struct {
	provider          llm.Provider
	model             string
	db                *llm.DB
	config            llm.ProviderConfig
	sysPromptOverride string
}

func NewProviderEngine(p llm.Provider, cfg llm.ProviderConfig) *ProviderEngine {
	model := cfg.DefaultModel
	if model == "" {
		model = p.DefaultModel()
	}

	dbPath := filepath.Join(aishDir(), "costs.db")
	db, err := llm.NewDB("file:"+dbPath, p.Name())
	if err != nil {
		fmt.Fprintf(os.Stderr, "aish: cost db error: %s\n", err)
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

func (e *ProviderEngine) SetSystemPromptOverride(prompt string) {
	e.sysPromptOverride = prompt
}

func (e *ProviderEngine) Close() {
	if e.db != nil {
		e.db.Close()
	}
}

// usageAdapter exposes the engine's cost DB to the CostGuard. A nil db means
// the cost store is unavailable -> every read returns an error so the guard
// fails closed (refuses the call) rather than treating "no data" as "$0 spent".
type usageAdapter struct {
	engine *ProviderEngine
}

func (u usageAdapter) TodayUsd() (float64, error) {
	if u.engine == nil || u.engine.db == nil {
		return 0, fmt.Errorf("costs.db unavailable")
	}
	return u.engine.db.TodayStats().Cost, nil
}

func (u usageAdapter) TodayTokens() (int64, error) {
	if u.engine == nil || u.engine.db == nil {
		return 0, fmt.Errorf("costs.db unavailable")
	}
	stats := u.engine.db.TodayStats()
	return stats.InputTokens + stats.OutputTokens, nil
}

// MonthUsd uses the lifetime total as the monthly figure. costs.db has no
// monthly query (only Stats/TodayStats per the spec); this overcounts older
// months but only ever makes the guard stricter, never looser — acceptable for
// a fail-closed cost ceiling.
func (u usageAdapter) MonthUsd() (float64, error) {
	if u.engine == nil || u.engine.db == nil {
		return 0, fmt.Errorf("costs.db unavailable")
	}
	_, _, _, totalCost := u.engine.db.Stats()
	return totalCost, nil
}

func (e *ProviderEngine) Query(ctx context.Context, input string, shellCtx ShellContext, out io.Writer) (*KIResponse, error) {
	// FAIL-CLOSED cost guard: build the guard and run the pre-call budget check
	// BEFORE any API call. On limit breach or any error reading usage/budget,
	// refuse the call — never "log and continue".
	guard, err := newCostGuard(kiConfig, usageAdapter{engine: e})
	if err != nil {
		return nil, err
	}
	if err := guard.PreCheck(); err != nil {
		return nil, err
	}

	sysPrompt := buildSystemPrompt(shellCtx, kiMemory, e.sysPromptOverride)
	if suffix := guard.SparmodeSuffix(); suffix != "" {
		sysPrompt += suffix
	}
	vSystemPrompt(sysPrompt)
	vKIRequest(input)

	var messages []llm.ChatMessage
	messages = append(messages, llm.ChatMessage{Role: "system", Content: sysPrompt})
	for _, turn := range kiConversation.Recent() {
		messages = append(messages, llm.ChatMessage{Role: "user", Content: turn.UserInput})
		messages = append(messages, llm.ChatMessage{Role: "assistant", Content: turn.Response})
	}
	messages = append(messages, llm.ChatMessage{Role: "user", Content: input})

	req := llm.ChatRequest{
		Model:    e.model,
		Messages: messages,
		Stream:   true,
	}

	start := time.Now()
	var fullText strings.Builder
	var usage llm.Usage

	err = e.provider.ChatStream(req, func(chunk llm.StreamChunk) {
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
	fmt.Fprintln(out)

	cost := e.config.CostForTokens(e.model, usage.InputTokens, usage.OutputTokens)
	status := "ok"
	errMsg := ""
	if err != nil {
		status = "error"
		errMsg = err.Error()
	}
	if e.db != nil {
		e.db.LogUsage(e.model, usage.InputTokens, usage.OutputTokens, latency.Milliseconds(), status, errMsg, "", cost)
	}
	// Book consumption into the cost audit log.
	guard.RecordUsage(e.model, usage.InputTokens, usage.OutputTokens, cost)

	// Append a raw action record (audit trail + learning source). Failed
	// attempts are logged too — they are part of the audit and signal "this did
	// not work" for later consolidation. logAction never fails the flow.
	responseText := fullText.String()
	logKIAction(KIActionRecord{
		Event:            "ki_query",
		Cwd:              shellCtx.Cwd,
		Prompt:           input,
		Model:            e.model,
		Response:         responseText,
		SuggestedCommand: extractCommand(responseText),
		Status:           status,
		Error:            errMsg,
		InputTokens:      usage.InputTokens,
		OutputTokens:     usage.OutputTokens,
		CostUSD:          cost,
	})

	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}

	kiConversation.Add(input, responseText)
	vKIResponse(responseText)

	return &KIResponse{
		Text:             responseText,
		SuggestedCommand: extractCommand(responseText),
		Confidence:       -1,
		TokensUsed:       usage.InputTokens + usage.OutputTokens,
	}, nil
}

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

func (e *ProviderEngine) TodayStats() *llm.UsageSummary {
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
