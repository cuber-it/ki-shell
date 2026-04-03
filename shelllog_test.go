// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"strings"
	"testing"
)

func TestScrubSecrets(t *testing.T) {
	tests := []struct {
		input    string
		contains string // must NOT contain this after scrubbing
	}{
		// OpenAI key
		{"export OPENAI_API_KEY=sk-proj-H4abc123def456ghi789jklmnop", "sk-proj-H4abc123"},
		// GitHub PAT
		{"git clone https://ghp_1234567890abcdefghijklmnopqrstuvwxyz@github.com/repo", "ghp_1234567890"},
		// Password in URL
		{"curl https://admin:SuperSecret123@db.example.com/api", "SuperSecret123"},
		// Authorization header
		{"Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U", "eyJhbGciOi"},
		// AWS key
		{"AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE", "AKIAIOSFODNN7"},
		// Generic password
		{"password: MySecretPass123", "MySecretPass123"},
		// Private key
		{"-----BEGIN RSA PRIVATE KEY-----", "PRIVATE KEY"},
	}

	for _, tt := range tests {
		result := scrubSecrets(tt.input)
		if strings.Contains(result, tt.contains) {
			t.Errorf("scrubSecrets(%q) still contains %q\nResult: %s", tt.input, tt.contains, result)
		}
		if !strings.Contains(result, "[REDACTED]") {
			t.Errorf("scrubSecrets(%q) should contain [REDACTED]\nResult: %s", tt.input, result)
		}
	}
}

func TestScrubSecrets_NoFalsePositives(t *testing.T) {
	safe := []string{
		"ls -la /home/user",
		"echo hello world",
		"git commit -m 'fix bug'",
		"docker ps",
		"export PATH=/usr/local/bin:$PATH",
	}
	for _, input := range safe {
		result := scrubSecrets(input)
		if strings.Contains(result, "[REDACTED]") {
			t.Errorf("scrubSecrets(%q) false positive: %s", input, result)
		}
	}
}
