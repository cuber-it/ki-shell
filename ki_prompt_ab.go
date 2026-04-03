// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// PromptVariant is a named system prompt variant for A/B testing
type PromptVariant struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Prompt      string `yaml:"prompt"`
	Active      bool   `yaml:"active"`
}

// PromptABConfig holds all prompt variants
type PromptABConfig struct {
	Variants []PromptVariant `yaml:"variants"`
}

// PromptABLog records which variant was used and user feedback
type PromptABEntry struct {
	Timestamp time.Time `yaml:"timestamp"`
	Variant   string    `yaml:"variant"`
	Query     string    `yaml:"query"`
	Rating    int       `yaml:"rating"` // 1-5, 0=unrated
}

// LoadPromptVariants reads prompt variants from ~/.kish/prompts.yaml
func LoadPromptVariants() []PromptVariant {
	path := filepath.Join(kishDir(), "prompts.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cfg PromptABConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	return cfg.Variants
}

// ActivePromptVariant returns the currently active variant's prompt text, or empty
func ActivePromptVariant() string {
	variants := LoadPromptVariants()
	for _, v := range variants {
		if v.Active {
			return v.Prompt
		}
	}
	return ""
}

// WriteDefaultPromptVariants creates a starter prompts.yaml if none exists
func WriteDefaultPromptVariants() {
	path := filepath.Join(kishDir(), "prompts.yaml")
	if fileExists(path) {
		return
	}

	content := `# kish prompt variants for A/B testing
# Set active: true on the variant you want to use
# The active variant's prompt is appended to the system prompt
# Use ki:prompt to see the full prompt, ki:variant to switch

variants:
  - name: default
    description: "Standard kish prompt — direct and competent"
    prompt: ""
    active: true

  - name: expert
    description: "Senior engineer style — terse, opinionated"
    prompt: "Antworte wie ein Senior Engineer mit 20 Jahren Erfahrung. Kurz, meinungsstark, keine Floskeln."
    active: false

  - name: teacher
    description: "Explains more, good for learning"
    prompt: "Erkläre kurz warum, nicht nur was. Der User lernt gerade."
    active: false

  - name: minimal
    description: "Absolute minimum output"
    prompt: "Maximal 1-2 Sätze. Wenn ein Befehl die Antwort ist: NUR den Befehl, kein Text."
    active: false
`
	os.WriteFile(path, []byte(content), 0644)
}

// SwitchVariant activates a named variant and deactivates all others
func SwitchVariant(name string) error {
	path := filepath.Join(kishDir(), "prompts.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var cfg PromptABConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return err
	}

	found := false
	for i := range cfg.Variants {
		cfg.Variants[i].Active = (cfg.Variants[i].Name == name)
		if cfg.Variants[i].Name == name {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("variant '%s' nicht gefunden", name)
	}

	out, err := yaml.Marshal(&cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0644)
}

// ListVariants returns a formatted list of all variants
func ListVariants() string {
	variants := LoadPromptVariants()
	if len(variants) == 0 {
		return "Keine Varianten konfiguriert. Siehe ~/.kish/prompts.yaml"
	}
	var lines []string
	for _, v := range variants {
		marker := "  "
		if v.Active {
			marker = "→ "
		}
		lines = append(lines, fmt.Sprintf("%s%-12s %s", marker, v.Name, v.Description))
	}
	return strings.Join(lines, "\n")
}
