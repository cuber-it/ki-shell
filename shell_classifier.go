// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"os"
	"strings"
)

var kiPrefix = "ki"

func isKIRequest(input string) bool {
	if strings.HasPrefix(input, kiPrefix+" ") || input == kiPrefix {
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
}

func commandInPath(name string) bool {
	for _, dir := range strings.Split(os.Getenv("PATH"), ":") {
		if dir == "" {
			continue
		}
		info, err := os.Stat(dir + "/" + name)
		if err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return true
		}
	}
	return false
}
