// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
//
// OpenAI (and OpenAI-compatible, e.g. LiteLLM/local) chat completion provider.
package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type OpenAIProvider struct {
	config ProviderConfig
	client *http.Client
}

// NewOpenAI builds an OpenAI provider. An empty APIBase defaults to the public
// OpenAI endpoint; set it for OpenAI-compatible gateways.
func NewOpenAI(config ProviderConfig) *OpenAIProvider {
	if config.APIBase == "" {
		config.APIBase = "https://api.openai.com/v1"
	}
	if config.DefaultModel == "" {
		config.DefaultModel = "gpt-4.1"
	}
	return &OpenAIProvider{config: config, client: defaultClient()}
}

func (p *OpenAIProvider) Name() string         { return "openai" }
func (p *OpenAIProvider) DefaultModel() string { return p.config.DefaultModel }

func (p *OpenAIProvider) Health() HealthStatus {
	req, err := http.NewRequest("GET", p.config.APIBase+"/models", nil)
	if err != nil {
		return HealthStatus{OK: false, Error: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	resp, err := p.client.Do(req)
	if err != nil {
		return HealthStatus{OK: false, Error: err.Error()}
	}
	resp.Body.Close()
	return HealthStatus{OK: resp.StatusCode == 200}
}

func (p *OpenAIProvider) ChatStream(req ChatRequest, onChunk func(StreamChunk)) error {
	model := req.Model
	if model == "" {
		model = p.config.DefaultModel
	}

	messages := make([]map[string]string, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, map[string]string{"role": "system", "content": req.System})
	}
	for _, msg := range req.Messages {
		messages = append(messages, map[string]string{"role": msg.Role, "content": msg.Content})
	}

	body := map[string]interface{}{
		"model":          model,
		"messages":       messages,
		"stream":         true,
		"stream_options": map[string]interface{}{"include_usage": true},
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	httpReq, err := http.NewRequest("POST", p.config.APIBase+"/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.config.APIKey)

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

	scanSSE(resp.Body, func(data string) bool {
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return true // tolerate non-JSON keepalive lines
		}

		if chunk.Usage != nil {
			onChunk(StreamChunk{Type: "usage", Usage: &Usage{
				InputTokens:  chunk.Usage.PromptTokens,
				OutputTokens: chunk.Usage.CompletionTokens,
			}})
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				onChunk(StreamChunk{Type: "content_delta", Content: choice.Delta.Content})
			}
		}
		return true
	})

	onChunk(StreamChunk{Type: "done"})
	return nil
}
