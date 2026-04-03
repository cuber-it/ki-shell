// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"os"
	"strings"
)

// KI prefix -- configurable via config.yaml (ki.prefix), default "@ki"
var kiPrefix = "@ki"

// isKIRequest checks if input starts with the KI prefix or the ? shortcut.
// No guessing, no heuristics. Explicit is better than implicit.
func isKIRequest(input string) bool {
	if strings.HasPrefix(input, kiPrefix+" ") || input == kiPrefix {
		return true
	}
	if strings.HasPrefix(input, "ki ") || input == "ki" {
		return true
	}
	if strings.HasPrefix(input, "? ") || input == "?" {
		return true
	}
	return false
}

func stripKIPrefix(input string) string {
	if strings.HasPrefix(input, kiPrefix+" ") {
		return strings.TrimSpace(input[len(kiPrefix)+1:])
	}
	if input == kiPrefix {
		return ""
	}
	if strings.HasPrefix(input, "ki ") {
		return strings.TrimSpace(input[3:])
	}
	if input == "ki" {
		return ""
	}
	if strings.HasPrefix(input, "? ") {
		return strings.TrimSpace(input[2:])
	}
	if input == "?" {
		return ""
	}
	return input
}

func initKIPrefix(cfg *KishConfig) {
	if cfg.KI.Prefix != "" {
		kiPrefix = cfg.KI.Prefix
	}
	// Prevent conflicts with shell syntax
	if kiPrefix == "" || kiPrefix == "|" || kiPrefix == ">" || kiPrefix == "<" {
		kiPrefix = "@ki"
	}
}

func commandInPath(name string) bool {
	pathEnv := os.Getenv("PATH")
	for _, dir := range strings.Split(pathEnv, ":") {
		if dir == "" {
			continue
		}
		fullPath := dir + "/" + name
		info, err := os.Stat(fullPath)
		if err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return true
		}
	}
	return false
}
