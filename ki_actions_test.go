// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"testing"
)

func TestExtractActions(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		// bash block
		{"text\n```bash\nls -la\n```\nmore text", []string{"ls -la"}},
		// action block
		{"```action\ndocker ps\n```", []string{"docker ps"}},
		// sh block
		{"```sh\necho hello\n```", []string{"echo hello"}},
		// multiple commands in one block
		{"```bash\nls\npwd\n```", []string{"ls", "pwd"}},
		// no blocks
		{"just text no code", nil},
		// comments ignored
		{"```bash\n# comment\nls\n```", []string{"ls"}},
		// multiple blocks
		{"```bash\nls\n```\ntext\n```bash\npwd\n```", []string{"ls", "pwd"}},
	}

	for _, tt := range tests {
		result := extractActions(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("extractActions(%q) = %v, want %v", tt.input, result, tt.expected)
			continue
		}
		for i := range result {
			if result[i] != tt.expected[i] {
				t.Errorf("extractActions(%q)[%d] = %q, want %q", tt.input, i, result[i], tt.expected[i])
			}
		}
	}
}

func TestClassifyAction(t *testing.T) {
	perms := DefaultPermissions()

	tests := []struct {
		cmd      string
		expected ActionLevel
	}{
		// Read-only → AutoRead
		{"ls -la", ActionAutoRead},
		{"cat /etc/hosts", ActionAutoRead},
		{"git status", ActionAutoRead},
		{"docker ps", ActionAutoRead},
		{"grep error logfile", ActionAutoRead},
		{"ps aux", ActionAutoRead},
		// Destructive → Confirm
		{"rm -rf /tmp/build", ActionConfirm},
		{"kill -9 12345", ActionConfirm},
		// Normal write → Confirm
		{"echo hello > file.txt", ActionConfirm},
		{"mv old new", ActionConfirm},
		// Self-modify → Blocked
		{"vi ~/.kish/config.yaml", ActionBlocked},
		{"vi ~/.kish/permissions.yaml", ActionBlocked},
	}

	for _, tt := range tests {
		level, _ := ClassifyAction(tt.cmd, &perms)
		if level != tt.expected {
			t.Errorf("ClassifyAction(%q) = %v, want %v", tt.cmd, level, tt.expected)
		}
	}
}

func TestClassifyAction_SSH(t *testing.T) {
	perms := DefaultPermissions()

	tests := []struct {
		cmd      string
		expected ActionLevel
	}{
		// SSH with read-only remote command → AutoRead
		{"ssh user@host docker ps", ActionAutoRead},
		{"ssh -p 2222 host ls -la", ActionAutoRead},
		// SSH without command → Confirm
		{"ssh host", ActionConfirm},
		// SSH with write command → Confirm
		{"ssh host rm -rf /tmp", ActionConfirm},
	}

	for _, tt := range tests {
		level, _ := ClassifyAction(tt.cmd, &perms)
		if level != tt.expected {
			t.Errorf("ClassifyAction(%q) = %v, want %v", tt.cmd, level, tt.expected)
		}
	}
}

func TestStripActions(t *testing.T) {
	input := "Here is the result:\n```bash\nls -la\n```\nDone."
	result := stripActions(input)
	// Action block should be removed, text before and after preserved
	if result != "Here is the result:\n\nDone." {
		t.Errorf("stripActions got %q", result)
	}
}
