// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
//
// Package llm is kish's own slim LLM provider layer. It replaces the former
// external core dependency so that kish builds and runs anywhere, with no
// local replace directive pointing at a sibling repository.
//
// Scope is deliberately narrow: exactly what kish uses — OpenAI and Anthropic
// chat with SSE streaming and prompt/completion token usage. Pure net/http +
// encoding/json, no heavy dependencies. Provider breadth is NOT a goal here.
package llm

// ChatMessage is a single message in a conversation.
type ChatMessage struct {
	Role    string `json:"role"`    // system, user, assistant
	Content string `json:"content"` // kish only ever sends plain text
}

// ChatRequest is the unified request format.
type ChatRequest struct {
	Model     string        `json:"model"`
	Messages  []ChatMessage `json:"messages"`
	System    string        `json:"system,omitempty"`
	MaxTokens int           `json:"max_tokens,omitempty"`
	Stream    bool          `json:"stream,omitempty"`
}

// Usage carries token counts for one call.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// StreamChunk is one piece of a streaming response. Type is one of:
// "content_delta", "usage", "done", "error".
type StreamChunk struct {
	Type    string `json:"type"`
	Content string `json:"content,omitempty"`
	Usage   *Usage `json:"usage,omitempty"`
	Error   string `json:"error,omitempty"`
}

// ModelPricing holds per-million-token pricing in USD.
type ModelPricing struct {
	Input  float64 `yaml:"input"`  // USD per 1M input tokens
	Output float64 `yaml:"output"` // USD per 1M output tokens
}

// ProviderConfig holds configuration for a single provider.
type ProviderConfig struct {
	Name         string                  `yaml:"name"`
	APIBase      string                  `yaml:"api_base"`
	APIKey       string                  `yaml:"api_key"`
	DefaultModel string                  `yaml:"default_model"`
	Pricing      map[string]ModelPricing `yaml:"pricing"`
}

// CostForTokens calculates the cost in USD for a given model and token counts.
// Returns 0 if the model has no pricing entry.
func (cfg ProviderConfig) CostForTokens(model string, inputTokens, outputTokens int) float64 {
	pricing, ok := cfg.Pricing[model]
	if !ok {
		return 0
	}
	return (float64(inputTokens) * pricing.Input / 1_000_000) +
		(float64(outputTokens) * pricing.Output / 1_000_000)
}

// HealthStatus reports provider reachability.
type HealthStatus struct {
	OK    bool
	Error string
}

// Provider is the minimal interface kish consumes.
type Provider interface {
	Name() string
	DefaultModel() string
	ChatStream(req ChatRequest, onChunk func(StreamChunk)) error
	Health() HealthStatus
}
