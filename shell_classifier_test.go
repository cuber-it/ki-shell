// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import "testing"

func TestIsKIRequest(t *testing.T) {
	kiPrefix = "ki"
	tests := []struct {
		input    string
		expected bool
	}{
		{"ki was ist los", true},
		{"ki", true},
		{"ki rm -rf /tmp", true},
		{"? warum", true},
		{"?", true},
		{"ls -la", false},
		{"echo hello", false},
		{"git status", false},
		{"", false},
		{"kino tickets", false},
	}
	for _, tt := range tests {
		if got := isKIRequest(tt.input); got != tt.expected {
			t.Errorf("isKIRequest(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestStripKIPrefix(t *testing.T) {
	kiPrefix = "ki"
	tests := []struct {
		input, expected string
	}{
		{"ki was ist los", "was ist los"},
		{"ki", ""},
		{"? warum", "warum"},
		{"?", ""},
		{"ls -la", "ls -la"},
	}
	for _, tt := range tests {
		if got := stripKIPrefix(tt.input); got != tt.expected {
			t.Errorf("stripKIPrefix(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestCustomPrefix(t *testing.T) {
	kiPrefix = "ai"
	defer func() { kiPrefix = "ki" }()

	if !isKIRequest("ai hilfe") {
		t.Error("custom prefix should work")
	}
	if isKIRequest("ki hilfe") {
		t.Error("old prefix should not match")
	}
}
