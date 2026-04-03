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

// Permissions controls what the KI is allowed to do.
// Defaults are paranoid — everything needs explicit confirmation.
type Permissions struct {
	// AutoExecute: if true, KI-suggested commands run without asking.
	// DEFAULT: false. Should almost never be true.
	AutoExecute bool `yaml:"auto_execute"`

	// ConfirmDestructive: if true, destructive commands need extra confirmation
	// even if AutoExecute is on. DEFAULT: true.
	ConfirmDestructive bool `yaml:"confirm_destructive"`

	// AllowedCommands: additional commands the KI may auto-execute (read-only).
	// Added to the built-in read-only whitelist (ls, cat, grep, git status, docker ps, etc.)
	AllowedCommands []string `yaml:"allowed_commands,omitempty"`

	// AgentMode: if true, KI can request multi-step action loops.
	// The KI gathers information autonomously using read-only commands,
	// and asks for confirmation on anything that modifies state.
	// DEFAULT: true (this is what makes kish powerful)
	AgentMode bool `yaml:"agent_mode"`

	// MaxAgentSteps: max iterations in an agent loop (prevents infinite loops)
	// DEFAULT: 5
	MaxAgentSteps int `yaml:"max_agent_steps"`

	// BlockedCommands: commands the KI is never allowed to suggest or execute.
	// These are blocked even in manual confirmation mode.
	BlockedCommands []string `yaml:"blocked_commands"`

	// DestructivePatterns: patterns that trigger extra confirmation.
	// Matched against the full command string.
	DestructivePatterns []string `yaml:"destructive_patterns"`

	// SendContext: what shell context is sent to the KI API.
	SendContext ContextPermissions `yaml:"send_context"`

	// MaxTokensPerQuery: hard limit on tokens per KI query (0 = use config default)
	MaxTokensPerQuery int `yaml:"max_tokens_per_query,omitempty"`

	// RequireConfirmation: always ask before sending to KI API.
	// DEFAULT: false (queries go to API immediately).
	// Set true for maximum privacy — shows what would be sent before sending.
	RequireConfirmation bool `yaml:"require_confirmation"`
}

// ContextPermissions controls what data is sent to the KI API
type ContextPermissions struct {
	// SendCwd: send current working directory. DEFAULT: true.
	SendCwd bool `yaml:"send_cwd"`

	// SendGitBranch: send git branch name. DEFAULT: true.
	SendGitBranch bool `yaml:"send_git_branch"`

	// SendCommandHistory: send recent commands. DEFAULT: true.
	SendCommandHistory bool `yaml:"send_command_history"`

	// SendCommandOutput: send stdout/stderr of recent commands. DEFAULT: false.
	// This can leak sensitive data (passwords in logs, tokens, etc.)
	SendCommandOutput bool `yaml:"send_command_output"`

	// SendEnvVars: send filtered environment variables. DEFAULT: true.
	SendEnvVars bool `yaml:"send_env_vars"`

	// SendProjectType: send detected project type. DEFAULT: true.
	SendProjectType bool `yaml:"send_project_type"`

	// SendMemory: send vault memories as context. DEFAULT: true.
	SendMemory bool `yaml:"send_memory"`

	// MaxHistoryCommands: how many recent commands to send (0 = none).
	// DEFAULT: 5.
	MaxHistoryCommands int `yaml:"max_history_commands"`
}

// DefaultPermissions returns the safe defaults.
// Paranoid: nothing auto-executes, destructive commands blocked,
// context is sent but no command output.
func DefaultPermissions() Permissions {
	return Permissions{
		AutoExecute:        false,
		ConfirmDestructive: true,
		AgentMode:          true, // KI can gather info autonomously
		MaxAgentSteps:      5,
		AllowedCommands:    nil,
		BlockedCommands: []string{
			":(){ :|:& };:", // fork bomb
			"> /dev/sda",
			"curl | sh", "curl | bash", "wget | sh", "wget | bash",
		},
		DestructivePatterns: []string{
			"rm -rf", "rm -r",
			"rmdir",
			"mkfs",
			"dd ",
			"format",
			"> /dev/",
			"kill -9",
			"killall",
			"pkill",
			"shutdown", "reboot", "halt",
			"systemctl stop", "systemctl disable",
			"chmod -R", "chown -R",
			"DROP TABLE", "DROP DATABASE", "DELETE FROM",
			"git push --force", "git reset --hard",
			"docker rm", "docker rmi", "docker system prune",
		},
		SendContext: ContextPermissions{
			SendCwd:            true,
			SendGitBranch:      true,
			SendCommandHistory: true,
			SendCommandOutput:  false, // paranoid default
			SendEnvVars:        true,
			SendProjectType:    true,
			SendMemory:         true,
			MaxHistoryCommands: 5,
		},
		RequireConfirmation: false,
	}
}

// GodMode disables ALL hardcoded safety checks. The KI can suggest anything.
// Can ONLY be enabled via environment variable KISH_GOD_MODE=yes — NOT via config file.
// If you enable this, you take full responsibility. There are no more guardrails.
func isGodMode() bool {
	return os.Getenv("KISH_GOD_MODE") == "yes"
}

// SelfModifyPaths are files the KI may NEVER modify — not even with user confirmation.
// This is the ONLY hardcoded block. It prevents the KI from escalating its own privileges.
// Everything else goes through the normal permission system (destructive → confirm).
var SelfModifyPaths = []string{
	"~/.kish/config.yaml",
	"~/.kish/permissions.yaml",
	"~/.kish/kishrc",
}

// ProtectedPaths trigger a destructive-level confirmation (red warning).
// The KI CAN modify these — but only after the user explicitly confirms.
var ProtectedPaths = []string{
	"~/.kish/vault/",
	"/etc/shells",
	"/etc/passwd",
	"/etc/shadow",
	"/etc/sudoers",
	"~/.ssh/config",
	"~/.ssh/authorized_keys",
	"~/.ssh/known_hosts",
	"~/.ssh/id_",
}

// CheckCommand validates a KI-suggested command against permissions.
// Returns: (allowed, needsConfirmation, reason)
//
// Only ONE hardcoded block: KI cannot modify its own config (privilege escalation).
// Everything else is allowed with appropriate confirmation level.
func (p *Permissions) CheckCommand(command string) (bool, bool, string) {
	cmdLower := strings.ToLower(command)

	// === ONLY HARDCODED BLOCK: KI self-modification ===
	// The KI must NEVER be able to change its own permissions or config.
	// This cannot be overridden, not even by god mode.
	readOnlyCmds := map[string]bool{
		"cat": true, "less": true, "more": true,
		"head": true, "tail": true, "grep": true,
		"wc": true, "file": true, "stat": true,
		"ls": true, "diff": true, "md5sum": true,
	}
	firstWord := ""
	if fields := strings.Fields(command); len(fields) > 0 {
		firstWord = strings.ToLower(fields[0])
	}
	if !readOnlyCmds[firstWord] {
		if touches, selfPath := commandTouchesSelfModifyPath(command); touches {
			return false, false, fmt.Sprintf("KI darf eigene Config nicht ändern: %s", selfPath)
		}
	}

	// === User-configurable blocks ===
	for _, blocked := range p.BlockedCommands {
		if strings.Contains(cmdLower, strings.ToLower(blocked)) {
			return false, false, fmt.Sprintf("Befehl blockiert (config): '%s'", blocked)
		}
	}

	// === Destructive patterns → red confirmation ===
	for _, pattern := range p.DestructivePatterns {
		if strings.Contains(cmdLower, strings.ToLower(pattern)) {
			return true, true, fmt.Sprintf("Destruktiver Befehl: '%s'", pattern)
		}
	}

	// === Protected paths → red confirmation (but allowed) ===
	if !readOnlyCmds[firstWord] {
		if touches, protPath := commandTouchesProtectedPath(command); touches {
			return true, true, fmt.Sprintf("Geschützte Datei: %s", protPath)
		}
	}

	// === Elevated patterns → yellow confirmation ===
	elevatedPatterns := []string{
		"sudo", "su ", "passwd", "visudo",
		"chmod +s", "chmod u+s", "chmod g+s",
		"crontab",
		"ld_preload=", "ld_library_path=",
	}
	for _, pattern := range elevatedPatterns {
		if strings.Contains(cmdLower, pattern) {
			return true, true, fmt.Sprintf("Erhöhte Rechte: '%s'", pattern)
		}
	}

	// === Command substitution / pipes to network → yellow confirmation ===
	if !readOnlyCmds[firstWord] {
		if strings.Contains(command, "$(") || strings.Contains(command, "`") {
			return true, true, "Befehl enthält Command-Substitution"
		}
		pipeToNetwork := []string{"| nc ", "| ncat ", "| netcat ", "| curl ", "| wget "}
		for _, pattern := range pipeToNetwork {
			if strings.Contains(cmdLower, pattern) {
				return true, true, "Befehl pipt Daten an Netzwerk-Tool"
			}
		}
	}

	// Auto-execute check
	if p.AutoExecute {
		if len(p.AllowedCommands) == 0 {
			// AutoExecute but no whitelist = confirm everything
			return true, true, "Kein Befehl in der Whitelist"
		}
		firstWord := strings.Fields(command)[0]
		for _, allowed := range p.AllowedCommands {
			if firstWord == allowed {
				return true, false, ""
			}
		}
		return true, true, fmt.Sprintf("'%s' nicht in der Whitelist", firstWord)
	}

	// Default: allowed but needs confirmation
	return true, true, ""
}

// FilterContext applies permission filters to ShellContext before sending to KI
func (p *Permissions) FilterContext(ctx ShellContext) ShellContext {
	filtered := ShellContext{}

	if p.SendContext.SendCwd {
		filtered.Cwd = ctx.Cwd
	}
	if p.SendContext.SendGitBranch {
		filtered.GitBranch = ctx.GitBranch
	}
	if p.SendContext.SendProjectType {
		filtered.ProjectType = ctx.ProjectType
	}
	if p.SendContext.SendEnvVars {
		filtered.EnvVars = ctx.EnvVars
	}
	if p.SendContext.SendCommandHistory && p.SendContext.MaxHistoryCommands > 0 {
		limit := p.SendContext.MaxHistoryCommands
		if limit > len(ctx.LastCommands) {
			limit = len(ctx.LastCommands)
		}
		for i := 0; i < limit; i++ {
			cmd := ctx.LastCommands[i]
			if !p.SendContext.SendCommandOutput {
				cmd.Stdout = ""
				cmd.Stderr = ""
			}
			filtered.LastCommands = append(filtered.LastCommands, cmd)
		}
	}

	return filtered
}

func expandHomePath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

// resolveProtectedPaths returns all protected paths with tilde expanded and realpath resolved
func resolveProtectedPaths() []string {
	var resolved []string
	for _, p := range ProtectedPaths {
		expanded := expandHomePath(p)
		// Add both the expanded and realpath-resolved version
		resolved = append(resolved, expanded)
		if real, err := filepath.EvalSymlinks(expanded); err == nil && real != expanded {
			resolved = append(resolved, real)
		}
	}
	return resolved
}

// commandTouchesSelfModifyPath checks if a command targets kish's own config files.
// This is the ONLY hardcoded block — prevents KI from escalating its own privileges.
func commandTouchesSelfModifyPath(command string) (bool, string) {
	var resolved []string
	for _, p := range SelfModifyPaths {
		expanded := expandHomePath(p)
		resolved = append(resolved, expanded)
		if real, err := filepath.EvalSymlinks(expanded); err == nil && real != expanded {
			resolved = append(resolved, real)
		}
	}
	fields := strings.Fields(command)
	for _, arg := range fields[1:] {
		arg = expandHomePath(arg)
		absPath := arg
		if !filepath.IsAbs(arg) {
			cwd, _ := os.Getwd()
			absPath = filepath.Join(cwd, arg)
		}
		absPath = filepath.Clean(absPath)
		candidates := []string{absPath}
		if real, err := filepath.EvalSymlinks(absPath); err == nil {
			candidates = append(candidates, real)
		}
		for _, candidate := range candidates {
			for _, selfPath := range resolved {
				if candidate == selfPath || strings.HasPrefix(candidate, selfPath) {
					return true, selfPath
				}
			}
		}
	}
	return false, ""
}

// commandTouchesProtectedPath checks if any argument in a command refers to a protected path.
// Resolves symlinks, relative paths (..), and tilde expansion.
func commandTouchesProtectedPath(command string) (bool, string) {
	protected := resolveProtectedPaths()
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false, ""
	}

	// Check each argument (skip the command itself for read-only check)
	for _, arg := range fields[1:] {
		// Expand tilde
		arg = expandHomePath(arg)

		// Try to resolve to absolute + real path
		absPath := arg
		if !filepath.IsAbs(arg) {
			cwd, _ := os.Getwd()
			absPath = filepath.Join(cwd, arg)
		}
		absPath = filepath.Clean(absPath)

		// Also try resolving symlinks
		candidates := []string{absPath}
		if real, err := filepath.EvalSymlinks(absPath); err == nil {
			candidates = append(candidates, real)
		}

		for _, candidate := range candidates {
			for _, prot := range protected {
				// Exact match or is inside protected directory
				if candidate == prot || strings.HasPrefix(candidate, prot) {
					return true, prot
				}
			}
		}
	}
	return false, ""
}

// LoadPermissions reads permissions from ~/.kish/permissions.yaml or returns defaults
func LoadPermissions() Permissions {
	perms := DefaultPermissions()
	path := filepath.Join(kishDir(), "permissions.yaml")

	data, err := os.ReadFile(path)
	if err != nil {
		WriteDefaultPermissions()
		return perms
	}

	if err := yaml.Unmarshal(data, &perms); err != nil {
		fmt.Fprintf(os.Stderr, "kish: permissions error: %s\n", err)
		return DefaultPermissions()
	}
	return perms
}

// WriteDefaultPermissions creates a default permissions.yaml if none exists
func WriteDefaultPermissions() {
	path := filepath.Join(kishDir(), "permissions.yaml")
	if fileExists(path) {
		return
	}

	content := `# kish permissions — controls what the KI is allowed to do
# Defaults are paranoid: nothing auto-executes, destructive commands need extra confirmation.

# auto_execute: if true, KI-suggested commands run without asking.
# DANGEROUS. Leave false unless you know what you're doing.
auto_execute: false

# confirm_destructive: extra confirmation for dangerous commands, even with auto_execute.
confirm_destructive: true

# allowed_commands: whitelist for auto-execution (only with auto_execute: true)
# Example: ["ls", "cat", "echo", "git status"]
allowed_commands: []

# blocked_commands: these commands are NEVER suggested or executed by the KI
blocked_commands:
  - "rm -rf /"
  - "rm -rf /*"
  - "mkfs"
  - "dd if="
  - ":(){ :|:& };:"
  - "> /dev/sda"
  - "chmod -R 777 /"
  - "curl | sh"
  - "curl | bash"

# destructive_patterns: trigger extra confirmation (substring match)
destructive_patterns:
  - "rm -rf"
  - "rm -r"
  - "mkfs"
  - "dd "
  - "kill -9"
  - "killall"
  - "shutdown"
  - "reboot"
  - "git push --force"
  - "git reset --hard"
  - "DROP TABLE"
  - "DROP DATABASE"
  - "docker system prune"

# What data is sent to the KI API
send_context:
  send_cwd: true
  send_git_branch: true
  send_command_history: true
  send_command_output: false    # CAUTION: can leak passwords/tokens from logs
  send_env_vars: true
  send_project_type: true
  send_memory: true
  max_history_commands: 5

# require_confirmation: show what would be sent before every KI query
# Set true for maximum privacy
require_confirmation: false
`
	os.WriteFile(path, []byte(content), 0644)
}
