// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Skill struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Script      string `yaml:"script"`
}

var loadedSkills []Skill

func initSkills() {
	dir := filepath.Join(kishDir(), "skills")
	os.MkdirAll(dir, 0755)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var skill Skill
		if err := yaml.Unmarshal(data, &skill); err != nil {
			continue
		}
		if skill.Name != "" && skill.Script != "" {
			loadedSkills = append(loadedSkills, skill)
		}
	}
}

func findSkill(name string) *Skill {
	name = strings.ToLower(name)
	for i := range loadedSkills {
		if strings.ToLower(loadedSkills[i].Name) == name {
			return &loadedSkills[i]
		}
	}
	return nil
}

func skillsForPrompt() string {
	if len(loadedSkills) == 0 {
		return ""
	}
	var lines []string
	lines = append(lines, "Verfügbare Skills (vorgefertigte Scripts, bevorzuge diese statt eigene zu schreiben):")
	for _, s := range loadedSkills {
		lines = append(lines, fmt.Sprintf("  skill:%s — %s", s.Name, s.Description))
	}
	lines = append(lines, "Rufe Skills auf mit: skill:<name> in einem ```action Block.")
	return strings.Join(lines, "\n")
}

func listSkills() string {
	if len(loadedSkills) == 0 {
		return "No skills installed. Add YAML files to ~/.kish/skills/"
	}
	var lines []string
	for _, s := range loadedSkills {
		lines = append(lines, fmt.Sprintf("  %-20s %s", s.Name, s.Description))
	}
	return strings.Join(lines, "\n")
}
