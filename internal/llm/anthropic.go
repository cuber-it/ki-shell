// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
//
// Anthropic Claude chat completion provider.
package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// defaultAnthropicMaxTokens is the required max_tokens when the caller does not
// set one (the Anthropic API rejects requests without it).
const defaultAnthropicMaxTokens = 4096

type AnthropicProvider struct {
	config ProviderConfig
	client *http.Client
}

func NewAnthropic(config ProviderConfig) *AnthropicProvider {
	if config.APIBase == "" {
		config.APIBase = "https://api.anthropic.com/v1"
	}
	if config.DefaultModel == "" {
		config.DefaultModel = "claude-sonnet-4-20250514"
	}
	return &AnthropicProvider{config: config, client: defaultClient()}
}

func (p *AnthropicProvider) Name() string         { return "anthropic" }
func (p *AnthropicProvider) DefaultModel() string { return p.config.DefaultModel }

func (p *AnthropicProvider) Health() HealthStatus {
	reqBody, err := json.Marshal(map[string]interface{}{
		"model": p.config.DefaultModel, "max_tokens": 1,
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		return HealthStatus{OK: false, Error: err.Error()}
	}
	req, err := http.NewRequest("POST", p.config.APIBase+"/messages", bytes.NewReader(reqBody))
	if err != nil {
		return HealthStatus{OK: false, Error: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	p.auth(req)
	resp, err := p.client.Do(req)
	if err != nil {
		return HealthStatus{OK: false, Error: err.Error()}
	}
	resp.Body.Close()
	return HealthStatus{OK: resp.StatusCode == 200}
}

func (p *AnthropicProvider) ChatStream(req ChatRequest, onChunk func(StreamChunk)) error {
	model := req.Model
	if model == "" {
		model = p.config.DefaultModel
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultAnthropicMaxTokens
	}

	// Anthropic takes the system prompt as a top-level field, not a message.
	// kish puts the system prompt in req.System AND/OR as a "system" role
	// message; handle both, skipping system-role messages from the array.
	system := req.System
	messages := make([]map[string]string, 0, len(req.Messages))
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			if system == "" {
				system = msg.Content
			}
			continue
		}
		messages = append(messages, map[string]string{"role": msg.Role, "content": msg.Content})
	}

	body := map[string]interface{}{
		"model":      model,
		"max_tokens": maxTokens,
		"messages":   messages,
		"stream":     true,
	}
	if system != "" {
		body["system"] = system
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	httpReq, err := http.NewRequest("POST", p.config.APIBase+"/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	p.auth(httpReq)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		errBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("HTTP %d (read error: %w)", resp.StatusCode, readErr)
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(errBody))
	}

	// Anthropic reports input_tokens in message_start and output_tokens
	// (cumulative) in message_delta. Accumulate both and emit one usage chunk.
	var inputTokens, outputTokens int

	scanSSE(resp.Body, func(data string) bool {
		var event struct {
			Type    string `json:"type"`
			Message *struct {
				Usage *struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
			Delta *struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
			Usage *struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return true
		}

		switch event.Type {
		case "message_start":
			if event.Message != nil && event.Message.Usage != nil {
				inputTokens = event.Message.Usage.InputTokens
				outputTokens = event.Message.Usage.OutputTokens
			}
		case "content_block_delta":
			if event.Delta != nil && event.Delta.Type == "text_delta" && event.Delta.Text != "" {
				onChunk(StreamChunk{Type: "content_delta", Content: event.Delta.Text})
			}
		case "message_delta":
			if event.Usage != nil {
				if event.Usage.InputTokens > 0 {
					inputTokens = event.Usage.InputTokens
				}
				outputTokens = event.Usage.OutputTokens
			}
		case "message_stop":
			onChunk(StreamChunk{Type: "usage", Usage: &Usage{
				InputTokens:  inputTokens,
				OutputTokens: outputTokens,
			}})
			onChunk(StreamChunk{Type: "done"})
			return false
		}
		return true
	})

	return nil
}

func (p *AnthropicProvider) auth(req *http.Request) {
	req.Header.Set("x-api-key", p.config.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
}
