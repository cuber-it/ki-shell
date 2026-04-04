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

type Permissions struct {
	AutoExecute        bool              `yaml:"auto_execute"`
	ConfirmDestructive bool              `yaml:"confirm_destructive"`
	AllowedCommands    []string          `yaml:"allowed_commands,omitempty"`
	AgentMode          bool              `yaml:"agent_mode"`
	MaxAgentSteps      int               `yaml:"max_agent_steps"`
	BlockedCommands    []string          `yaml:"blocked_commands"`
	DestructivePatterns []string         `yaml:"destructive_patterns"`
	SendContext        ContextPermissions `yaml:"send_context"`
	MaxTokensPerQuery  int               `yaml:"max_tokens_per_query,omitempty"`
	RequireConfirmation bool             `yaml:"require_confirmation"`
}

type ContextPermissions struct {
	SendCwd            bool `yaml:"send_cwd"`
	SendGitBranch      bool `yaml:"send_git_branch"`
	SendCommandHistory bool `yaml:"send_command_history"`
	SendCommandOutput  bool `yaml:"send_command_output"`
	SendEnvVars        bool `yaml:"send_env_vars"`
	SendProjectType    bool `yaml:"send_project_type"`
	SendMemory         bool `yaml:"send_memory"`
	MaxHistoryCommands int  `yaml:"max_history_commands"`
}

func DefaultPermissions() Permissions {
	return Permissions{
		AutoExecute:        false,
		ConfirmDestructive: true,
		AgentMode:          true,
		MaxAgentSteps:      5,
		BlockedCommands: []string{
			":(){ :|:& };:",
			"> /dev/sda",
			"curl | sh", "curl | bash", "wget | sh", "wget | bash",
		},
		DestructivePatterns: []string{
			"rm -rf", "rm -r", "rmdir", "mkfs", "dd ", "format", "> /dev/",
			"kill -9", "killall", "pkill",
			"shutdown", "reboot", "halt",
			"systemctl stop", "systemctl disable",
			"chmod -R", "chown -R",
			"DROP TABLE", "DROP DATABASE", "DELETE FROM",
			"git push --force", "git reset --hard",
			"docker rm", "docker rmi", "docker system prune",
		},
		SendContext: ContextPermissions{
			SendCwd: true, SendGitBranch: true, SendCommandHistory: true,
			SendCommandOutput: false, SendEnvVars: true, SendProjectType: true,
			SendMemory: true, MaxHistoryCommands: 5,
		},
	}
}

var SelfModifyPaths = []string{
	"~/.kish/config.yaml",
	"~/.kish/permissions.yaml",
	"~/.kish/kishrc",
}

var ProtectedPaths = []string{
	"~/.kish/vault/",
	"/etc/shells", "/etc/passwd", "/etc/shadow", "/etc/sudoers",
	"~/.ssh/config", "~/.ssh/authorized_keys", "~/.ssh/known_hosts", "~/.ssh/id_",
}

var readOnlyCmds = map[string]bool{
	"cat": true, "less": true, "more": true, "head": true, "tail": true,
	"grep": true, "wc": true, "file": true, "stat": true, "ls": true,
	"diff": true, "md5sum": true,
}

func (p *Permissions) CheckCommand(command string) (bool, bool, string) {
	cmdLower := strings.ToLower(command)
	firstWord := ""
	if fields := strings.Fields(command); len(fields) > 0 {
		firstWord = strings.ToLower(fields[0])
	}

	if !readOnlyCmds[firstWord] {
		if path, ok := commandTouchesPath(command, SelfModifyPaths); ok {
			return false, false, fmt.Sprintf("AI cannot modify its own config: %s", path)
		}
	}

	for _, blocked := range p.BlockedCommands {
		if strings.Contains(cmdLower, strings.ToLower(blocked)) {
			return false, false, fmt.Sprintf("Blocked: '%s'", blocked)
		}
	}

	for _, pattern := range p.DestructivePatterns {
		if strings.Contains(cmdLower, strings.ToLower(pattern)) {
			return true, true, fmt.Sprintf("Destructive: '%s'", pattern)
		}
	}

	if !readOnlyCmds[firstWord] {
		if path, ok := commandTouchesPath(command, ProtectedPaths); ok {
			return true, true, fmt.Sprintf("Protected file: %s", path)
		}
	}

	for _, pattern := range []string{
		"sudo", "su ", "passwd", "visudo",
		"chmod +s", "chmod u+s", "chmod g+s", "crontab",
		"ld_preload=", "ld_library_path=",
	} {
		if strings.Contains(cmdLower, pattern) {
			return true, true, fmt.Sprintf("Elevated: '%s'", pattern)
		}
	}

	if !readOnlyCmds[firstWord] {
		if strings.Contains(command, "$(") || strings.Contains(command, "`") {
			return true, true, "Command substitution"
		}
		for _, p := range []string{"| nc ", "| ncat ", "| netcat ", "| curl ", "| wget "} {
			if strings.Contains(cmdLower, p) {
				return true, true, "Pipe to network tool"
			}
		}
	}

	if p.AutoExecute {
		if len(p.AllowedCommands) == 0 {
			return true, true, ""
		}
		for _, allowed := range p.AllowedCommands {
			if firstWord == allowed {
				return true, false, ""
			}
		}
		return true, true, fmt.Sprintf("'%s' not in whitelist", firstWord)
	}

	return true, true, ""
}

func (p *Permissions) FilterContext(ctx ShellContext) ShellContext {
	var filtered ShellContext
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
	if p.SendContext.SendCommandHistory {
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

func commandTouchesPath(command string, paths []string) (string, bool) {
	resolved := resolvePaths(paths)
	for _, arg := range strings.Fields(command)[1:] {
		arg = expandHome(arg)
		if !filepath.IsAbs(arg) {
			cwd, _ := os.Getwd()
			arg = filepath.Join(cwd, arg)
		}
		arg = filepath.Clean(arg)
		check := arg
		if real, err := filepath.EvalSymlinks(arg); err == nil && real != arg {
			check = real
		}
		for _, p := range resolved {
			if check == p || strings.HasPrefix(check, p) {
				return p, true
			}
		}
	}
	return "", false
}

func resolvePaths(paths []string) []string {
	var resolved []string
	for _, p := range paths {
		expanded := expandHome(p)
		resolved = append(resolved, expanded)
		if real, err := filepath.EvalSymlinks(expanded); err == nil && real != expanded {
			resolved = append(resolved, real)
		}
	}
	return resolved
}

func LoadPermissions() Permissions {
	perms := DefaultPermissions()
	data, err := os.ReadFile(filepath.Join(kishDir(), "permissions.yaml"))
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

func WriteDefaultPermissions() {
	path := filepath.Join(kishDir(), "permissions.yaml")
	if fileExists(path) {
		return
	}
	content := `# kish permissions
auto_execute: false
confirm_destructive: true
blocked_commands:
  - ":(){ :|:& };:"
  - "> /dev/sda"
  - "curl | sh"
  - "curl | bash"
destructive_patterns:
  - "rm -rf"
  - "rm -r"
  - "mkfs"
  - "kill -9"
  - "killall"
  - "shutdown"
  - "reboot"
  - "git push --force"
  - "git reset --hard"
  - "docker system prune"
send_context:
  send_cwd: true
  send_git_branch: true
  send_command_history: true
  send_command_output: false
  send_env_vars: true
  send_project_type: true
  send_memory: true
  max_history_commands: 5
require_confirmation: false
`
	os.WriteFile(path, []byte(content), 0644)
}
