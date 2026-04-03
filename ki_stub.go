// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/cuber-it/heinzel-ai-core-go/provider"
)

var openaiPricing = map[string]provider.ModelPricing{
	"gpt-4o":       {Input: 2.50, Output: 10.00},
	"gpt-4o-mini":  {Input: 0.15, Output: 0.60},
	"gpt-4.1":      {Input: 2.00, Output: 8.00},
	"gpt-4.1-mini": {Input: 0.40, Output: 1.60},
	"gpt-4.1-nano": {Input: 0.10, Output: 0.40},
	"o3":           {Input: 10.00, Output: 40.00},
	"o3-mini":      {Input: 1.10, Output: 4.40},
	"o4-mini":      {Input: 1.10, Output: 4.40},
}

var anthropicPricing = map[string]provider.ModelPricing{
	"claude-sonnet-4-20250514":  {Input: 3.00, Output: 15.00},
	"claude-opus-4-20250514":    {Input: 15.00, Output: 75.00},
	"claude-haiku-4-5-20251001": {Input: 0.80, Output: 4.00},
}

func initKIEngine(cfg *KishConfig) KIEngine {
	apiKey := cfg.KI.APIKey

	switch cfg.KI.Provider {
	case "openai":
		if apiKey == "" {
			apiKey = os.Getenv("OPENAI_API_KEY")
		}
		if apiKey == "" {
			return &StubKIEngine{reason: "openai: no API key (set OPENAI_API_KEY or ki.api_key)"}
		}
		provCfg := provider.ProviderConfig{
			Name:         "openai",
			APIKey:       apiKey,
			DefaultModel: cfg.KI.Model,
			Pricing:      openaiPricing,
		}
		if cfg.KI.BaseURL != "" && cfg.KI.BaseURL != "http://localhost:11434" {
			provCfg.APIBase = cfg.KI.BaseURL
		}
		return NewProviderEngine(provider.NewOpenAI(provCfg), provCfg)

	case "anthropic":
		if apiKey == "" {
			apiKey = os.Getenv("ANTHROPIC_API_KEY")
		}
		if apiKey == "" {
			return &StubKIEngine{reason: "anthropic: no API key (set ANTHROPIC_API_KEY or ki.api_key)"}
		}
		provCfg := provider.ProviderConfig{
			Name:         "anthropic",
			APIKey:       apiKey,
			DefaultModel: cfg.KI.Model,
			Pricing:      anthropicPricing,
		}
		return NewProviderEngine(provider.NewAnthropic(provCfg), provCfg)

	case "":
		return &StubKIEngine{reason: "no provider configured"}
	default:
		return &StubKIEngine{reason: fmt.Sprintf("unknown provider: %s", cfg.KI.Provider)}
	}
}

// StubKIEngine is a placeholder when no KI provider is configured.
type StubKIEngine struct {
	reason string
}

func (s *StubKIEngine) Query(ctx context.Context, input string, shellCtx ShellContext, out io.Writer) (*KIResponse, error) {
	fmt.Fprintf(out, "\033[1;33m[kish/ki]\033[0m %s\n", input)
	fmt.Fprintf(out, "\033[2m%s\033[0m\n", s.reason)
	fmt.Fprintf(out, "\033[2mSetup: ~/.kish/config.yaml → provider: ollama | openai | anthropic\033[0m\n")

	if shellCtx.Cwd != "" {
		fmt.Fprintf(out, "\033[2mKontext: %s", shellCtx.Cwd)
		if shellCtx.GitBranch != "" {
			fmt.Fprintf(out, " [%s]", shellCtx.GitBranch)
		}
		if shellCtx.ProjectType != "" {
			fmt.Fprintf(out, " (%s)", shellCtx.ProjectType)
		}
		fmt.Fprintln(out, "\033[0m")
	}
	return &KIResponse{Text: "(stub)"}, nil
}

func (s *StubKIEngine) Available() bool { return false }
func (s *StubKIEngine) Name() string    { return "stub" }
