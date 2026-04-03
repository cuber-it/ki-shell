// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type KishConfig struct {
	KI  KIConfig    `yaml:"ki"`
	MCP []MCPServer `yaml:"mcp,omitempty"`
}

type KIConfig struct {
	Provider     string  `yaml:"provider"`
	Prefix       string  `yaml:"prefix,omitempty"`
	Model        string  `yaml:"model"`
	APIKey       string  `yaml:"api_key,omitempty"`
	BaseURL      string  `yaml:"base_url,omitempty"`
	MaxTokens    int     `yaml:"max_tokens,omitempty"`
	CostLimit    float64 `yaml:"cost_limit,omitempty"`
	SystemPrompt string  `yaml:"system_prompt,omitempty"`
}

func DefaultConfig() *KishConfig {
	return &KishConfig{
		KI: KIConfig{
			Provider:  "",
			Model:     "llama3",
			BaseURL:   "http://localhost:11434",
			MaxTokens: 1024,
		},
	}
}

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
