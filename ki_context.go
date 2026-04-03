// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// collectEnvVars returns a filtered set of environment variables relevant for KI context.
// We don't send everything -- only what helps the KI understand the environment.
func collectEnvVars() map[string]string {
	relevant := []string{
		"HOME", "USER", "SHELL", "LANG",
		"GOPATH", "GOROOT",
		"VIRTUAL_ENV", "CONDA_DEFAULT_ENV",
		"NODE_ENV",
		"DOCKER_HOST",
		"SSH_CONNECTION",
		"EDITOR", "VISUAL",
	}
	result := make(map[string]string)
	for _, key := range relevant {
		if val := os.Getenv(key); val != "" {
			result[key] = val
		}
	}
	return result
}

func detectGitBranch() string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Stderr = nil
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func detectProjectType() string {
	cwd, _ := os.Getwd()

	markers := []struct {
		file     string
		projType string
	}{
		{"go.mod", "go"},
		{"Cargo.toml", "rust"},
		{"package.json", "node"},
		{"pyproject.toml", "python"},
		{"setup.py", "python"},
		{"requirements.txt", "python"},
		{"Gemfile", "ruby"},
		{"pom.xml", "java"},
		{"build.gradle", "java"},
		{"CMakeLists.txt", "cmake"},
		{"Makefile", "make"},
		{"Dockerfile", "docker"},
		{"docker-compose.yml", "docker"},
		{"compose.yml", "docker"},
	}

	for _, marker := range markers {
		if _, err := os.Stat(filepath.Join(cwd, marker.file)); err == nil {
			return marker.projType
		}
	}
	return ""
}

// detectProjectInfo reads README or CLAUDE.md for project context.
// Returns a short summary (first 500 chars) or empty string.
func detectProjectInfo() string {
	cwd, _ := os.Getwd()
	candidates := []string{"CLAUDE.md", "README.md", "README"}

	for _, name := range candidates {
		data, err := os.ReadFile(filepath.Join(cwd, name))
		if err != nil {
			continue
		}
		content := string(data)
		if len(content) > 500 {
			content = content[:500] + "..."
		}
		return fmt.Sprintf("[%s]\n%s", name, content)
	}
	return ""
}

func truncateLines(text string, maxLines int) string {
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) <= maxLines {
		return text
	}
	return "...\n" + strings.Join(lines[len(lines)-maxLines:], "\n")
}
