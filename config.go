// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// KishConfig represents ~/.kish/config.yaml
type KishConfig struct {
	// KI provider configuration
	KI KIConfig `yaml:"ki"`

	// MCP servers
	MCP []MCPServer `yaml:"mcp,omitempty"`
}

// KIConfig configures the KI engine
type KIConfig struct {
	// Provider: "ollama", "openai", "anthropic", "heinzel", or empty (disabled)
	Provider string `yaml:"provider"`

	// Prefix: how to address the KI. Default: "@ki"
	// Examples: "@ki", "@ai", "@h", "@heinzel"
	Prefix string `yaml:"prefix,omitempty"`

	// Model name (e.g. "llama3", "gpt-4o", "claude-sonnet-4-20250514")
	Model string `yaml:"model"`

	// APIKey for cloud providers (can also use env: OPENAI_API_KEY, ANTHROPIC_API_KEY)
	APIKey string `yaml:"api_key,omitempty"`

	// BaseURL for Ollama or custom endpoints (default: http://localhost:11434)
	BaseURL string `yaml:"base_url,omitempty"`

	// MaxTokens per response (default: 1024)
	MaxTokens int `yaml:"max_tokens,omitempty"`

	// CostLimit monthly cost limit in USD (0 = unlimited)
	CostLimit float64 `yaml:"cost_limit,omitempty"`

	// SystemPrompt prepended to every query
	SystemPrompt string `yaml:"system_prompt,omitempty"`
}

// DefaultConfig returns a config with sensible defaults
func DefaultConfig() *KishConfig {
	return &KishConfig{
		KI: KIConfig{
			Provider:  "", // disabled by default
			Model:     "llama3",
			BaseURL:   "http://localhost:11434",
			MaxTokens: 1024,
		},
	}
}

// LoadConfig reads ~/.kish/config.yaml or returns defaults
func LoadConfig() *KishConfig {
	cfg := DefaultConfig()
	path := filepath.Join(kishDir(), "config.yaml")

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "kish: config error: %s\n", err)
		return DefaultConfig()
	}

	// API key fallback to environment
	if cfg.KI.APIKey == "" {
		switch cfg.KI.Provider {
		case "openai":
			cfg.KI.APIKey = os.Getenv("OPENAI_API_KEY")
		case "anthropic":
			cfg.KI.APIKey = os.Getenv("ANTHROPIC_API_KEY")
		}
	}

	return cfg
}

// WriteDefaultConfig creates a default config.yaml if none exists
func WriteDefaultConfig() {
	path := filepath.Join(kishDir(), "config.yaml")
	if fileExists(path) {
		return
	}

	content := `# kish configuration
# KI provider: ollama (local), openai, anthropic, heinzel, or empty (disabled)
ki:
  provider: ""
  model: "llama3"
  base_url: "http://localhost:11434"
  max_tokens: 1024
  # api_key: ""          # or use OPENAI_API_KEY / ANTHROPIC_API_KEY env vars
  # cost_limit: 10.0     # monthly USD limit (0 = unlimited)
  # system_prompt: ""    # prepended to every query
`
	os.WriteFile(path, []byte(content), 0644)
}
