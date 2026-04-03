// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"testing"
)

func TestIsKIRequest(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		// @ki prefix
		{"@ki was ist los", true},
		{"@ki", true},
		{"@ki rm -rf /tmp", true},
		// ? shortcut
		{"? warum", true},
		{"?", true},
		{"? ", true},
		// NOT ki requests
		{"ls -la", false},
		{"echo hello", false},
		{"git status", false},
		{"", false},
		{"@kino tickets", false}, // @kino != @ki
		{"ki something", true},   // ki is a built-in shorthand
	}

	kiPrefix = "@ki" // ensure default
	for _, tt := range tests {
		result := isKIRequest(tt.input)
		if result != tt.expected {
			t.Errorf("isKIRequest(%q) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestStripKIPrefix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"@ki was ist los", "was ist los"},
		{"@ki", ""},
		{"? warum", "warum"},
		{"?", ""},
		{"ls -la", "ls -la"}, // not a ki request, returned as-is
	}

	kiPrefix = "@ki"
	for _, tt := range tests {
		result := stripKIPrefix(tt.input)
		if result != tt.expected {
			t.Errorf("stripKIPrefix(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestCustomPrefix(t *testing.T) {
	kiPrefix = "@ai"
	defer func() { kiPrefix = "@ki" }()

	if !isKIRequest("@ai hilfe") {
		t.Error("@ai should be recognized with custom prefix")
	}
	if isKIRequest("@ki hilfe") {
		t.Error("@ki should NOT be recognized when prefix is @ai")
	}
	if stripKIPrefix("@ai was ist los") != "was ist los" {
		t.Error("stripKIPrefix should work with custom prefix")
	}
}
