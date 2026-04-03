// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ActionLevel defines what the KI is allowed to do with a command.
// Ordered from most restricted to least restricted.
type ActionLevel int

const (
	ActionBlocked   ActionLevel = iota // never execute, hard stop
	ActionConfirm                      // show command, ask user [j/n]
	ActionAutoRead                     // execute silently, read-only (ls, cat, grep, docker ps)
	ActionAutoWrite                    // execute silently, may modify state (git commit, mv)
	ActionAutoExec                     // execute silently, anything goes (sudo, rm — god mode)
)

// ReadOnlyCommands are commands the KI may execute without asking.
// They cannot modify system state. Configurable via permissions.yaml.
var DefaultReadOnlyCommands = []string{
	// Filesystem (read)
	"ls", "cat", "head", "tail", "wc", "file", "stat", "find", "locate",
	"du", "df", "tree", "less", "more", "realpath", "basename", "dirname",
	// Text processing
	"grep", "awk", "sed", "sort", "uniq", "cut", "tr", "diff", "comm",
	// System info
	"uname", "hostname", "whoami", "id", "date", "uptime", "free",
	"lsb_release", "arch", "nproc", "lscpu", "lsblk",
	// Process info
	"ps", "top", "htop", "pgrep",
	// Network info (read-only)
	"ping", "host", "dig", "nslookup", "curl", "wget",
	"ip", "ifconfig", "ss", "netstat",
	// Git (read)
	"git status", "git log", "git diff", "git branch", "git remote",
	"git show", "git blame", "git tag", "git stash list",
	// Docker (read)
	"docker ps", "docker images", "docker logs", "docker inspect",
	"docker stats", "docker top", "docker port", "docker network ls",
	"docker volume ls", "docker compose ps", "docker compose logs",
	// SSH is handled separately in ClassifyAction (recursive check)
	// Package info
	"dpkg -l", "apt list", "rpm -qa", "pip list", "npm list", "go list",
	// Kubernetes (read)
	"kubectl get", "kubectl describe", "kubectl logs", "kubectl top",
}

// ClassifyAction determines the action level for a command.
// Chain: Blocked → AutoExec → AutoWrite → AutoRead → Confirm
func ClassifyAction(command string, perms *Permissions) (ActionLevel, string) {
	// Step 1: Hardcoded blocks — ALWAYS checked, even in god mode the config blocks apply
	allowed, _, reason := perms.CheckCommand(command)
	if !allowed {
		return ActionBlocked, reason
	}

	cmdLower := strings.ToLower(strings.TrimSpace(command))

	// Step 2: God mode / AutoExecute — if enabled, everything that's not blocked runs
	if perms.AutoExecute {
		// Even with AutoExecute, destructive commands need confirmation unless overridden
		if perms.ConfirmDestructive {
			for _, pattern := range perms.DestructivePatterns {
				if strings.Contains(cmdLower, strings.ToLower(pattern)) {
					return ActionConfirm, fmt.Sprintf("Destruktiv: '%s'", pattern)
				}
			}
		}
		return ActionAutoExec, ""
	}

	// Step 3: Read-only whitelist — auto-execute without asking
	readOnlyList := DefaultReadOnlyCommands
	if len(perms.AllowedCommands) > 0 {
		readOnlyList = append(readOnlyList, perms.AllowedCommands...)
	}
	for _, pattern := range readOnlyList {
		patLower := strings.ToLower(pattern)
		if strings.HasPrefix(cmdLower, patLower) {
			// Ensure word boundary: pattern must be followed by space, EOF, or nothing
			rest := cmdLower[len(patLower):]
			if rest == "" || rest[0] == ' ' {
				return ActionAutoRead, ""
			}
		}
	}

	// Step 4: SSH — check the remote command recursively
	if strings.HasPrefix(cmdLower, "ssh ") {
		remoteCmd := extractSSHCommand(command)
		if remoteCmd != "" {
			remoteLevel, remoteReason := ClassifyAction(remoteCmd, perms)
			if remoteLevel == ActionBlocked {
				return ActionBlocked, "Remote: " + remoteReason
			}
			if remoteLevel == ActionAutoRead {
				return ActionAutoRead, ""
			}
		}
		return ActionConfirm, "SSH-Verbindung braucht Bestätigung"
	}

	// Step 5: Everything else → Confirm
	return ActionConfirm, ""
}

// extractSSHCommand extracts the remote command from an ssh command.
// Handles: ssh user@host cmd, ssh -p 22 host cmd, ssh -J jump host cmd
// e.g. "ssh user@host docker ps" → "docker ps"
func extractSSHCommand(command string) string {
	parts := strings.Fields(command)
	// Flags that take an argument
	flagsWithArg := map[string]bool{
		"-p": true, "-i": true, "-l": true, "-o": true,
		"-F": true, "-J": true, "-W": true, "-w": true,
		"-b": true, "-c": true, "-D": true, "-E": true,
		"-e": true, "-I": true, "-L": true, "-m": true,
		"-O": true, "-Q": true, "-R": true, "-S": true,
	}

	hostSeen := false
	for i := 1; i < len(parts); i++ {
		if strings.HasPrefix(parts[i], "-") {
			if flagsWithArg[parts[i]] && i+1 < len(parts) {
				i++ // skip argument
			}
			continue
		}
		if !hostSeen {
			hostSeen = true
			continue // this is the host
		}
		// Everything after host is the remote command
		return strings.Join(parts[i:], " ")
	}
	return ""
}

// ExecuteAction runs a command and captures its output.
// Used by the KI agent loop to gather information.
func ExecuteAction(ctx context.Context, command string, timeout time.Duration) (string, string, int) {
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	// Truncate output to prevent token explosion
	stdoutStr := truncateLines(stdout.String(), 500)
	stderrStr := truncateLines(stderr.String(), 100)
	return stdoutStr, stderrStr, exitCode
}

// RunAgentLoop executes a multi-step KI interaction.
// The KI can request actions (commands to run), kish executes them
// according to permissions, feeds results back, and the KI continues
// until it has a final answer.
//
// Protocol: KI outputs ```action blocks:
//   ```action
//   docker ps
//   ```
// kish executes, feeds output back, KI continues.
// Max iterations to prevent infinite loops.
func RunAgentLoop(ctx context.Context, engine KIEngine, input string, shellCtx ShellContext, mem *Memory, maxSteps int) (string, error) {
	if maxSteps == 0 {
		maxSteps = 5
	}

	conversation := make([]ConversationTurn, 0)
	currentInput := input

	// Temporarily switch to agent system prompt
	if pe, ok := engine.(*ProviderEngine); ok {
		pe.SetSystemPromptOverride(buildAgentSystemPrompt(shellCtx, mem))
		defer pe.SetSystemPromptOverride("")
	}

	for step := 0; step < maxSteps; step++ {
		// Check for cancellation (Ctrl+C)
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "\n[kish/ki] Abgebrochen.")
			return "", ctx.Err()
		default:
		}

		vStep(step, maxSteps, currentInput)

		var output bytes.Buffer
		resp, err := engine.Query(ctx, currentInput, shellCtx, &output)
		if err != nil {
			if ctx.Err() != nil {
				fmt.Fprintln(os.Stderr, "\n[kish/ki] Abgebrochen.")
				return "", ctx.Err()
			}
			return "", err
		}

		responseText := output.String()
		vKIResponse(responseText)

		// Check for action blocks
		actions := extractActions(responseText)
		if len(actions) == 0 {
			// No actions requested — final answer
			vPrint(1, "--- Final answer (no more actions) ---")
			fmt.Fprint(os.Stdout, responseText)
			fmt.Fprintln(os.Stdout)
			return resp.Text, nil
		}

		// Deduplicate
		{
			seen := make(map[string]bool)
			var unique []string
			for _, a := range actions {
				if !seen[a] {
					seen[a] = true
					unique = append(unique, a)
				}
			}
			actions = unique
		}

		vPrint(1, "KI requested %d action(s)", len(actions))

		// Execute actions
		var actionResults strings.Builder
		for i, action := range actions {
			level, reason := ClassifyAction(action, &kiPermissions)
			vAction(action, level, i+1, len(actions))

			// Audit every action
			if audit != nil {
				audit.LogAction(action, level, levelDecision(level), reason)
			}

			switch level {
			case ActionBlocked:
				fmt.Fprintf(os.Stderr, "\033[1;31m[BLOCKIERT]\033[0m %s — %s\n", action, reason)
				actionResults.WriteString(fmt.Sprintf("BLOCKIERT: %s (%s)\n", action, reason))

			case ActionConfirm:
				confirmLevel := ConfirmNormal
				if strings.Contains(strings.ToLower(reason), "destruktiv") {
					confirmLevel = ConfirmDestructive
				}
				result := Confirm(action, reason, confirmLevel)
				if result == ConfirmYes {
					stdout, stderr, exitCode := ExecuteAction(ctx, action, 30*time.Second)
					fmt.Fprintf(os.Stderr, "\033[2m→ exit %d\033[0m\n", exitCode)
					actionResults.WriteString(fmt.Sprintf("$ %s\nexit: %d\nstdout:\n%sstderr:\n%s\n", action, exitCode, stdout, stderr))
				} else {
					actionResults.WriteString(fmt.Sprintf("ABGELEHNT: %s (User hat abgelehnt)\n", action))
				}

			case ActionAutoRead:
				if verboseLevel == 0 {
					fmt.Fprintf(os.Stderr, "\033[2m→ %s\033[0m\n", action)
				}
				stdout, stderr, exitCode := ExecuteAction(ctx, action, 30*time.Second)
				vActionResult(action, exitCode, stdout, stderr)
				actionResults.WriteString(fmt.Sprintf("$ %s\nexit: %d\nstdout:\n%sstderr:\n%s\n", action, exitCode, stdout, stderr))

			case ActionAutoWrite:
				if verboseLevel == 0 {
					fmt.Fprintf(os.Stderr, "\033[1;33m→ %s\033[0m\n", action)
				}
				stdout, stderr, exitCode := ExecuteAction(ctx, action, 30*time.Second)
				vActionResult(action, exitCode, stdout, stderr)
				actionResults.WriteString(fmt.Sprintf("$ %s\nexit: %d\nstdout:\n%sstderr:\n%s\n", action, exitCode, stdout, stderr))

			case ActionAutoExec:
				if verboseLevel == 0 {
					fmt.Fprintf(os.Stderr, "\033[1;35m→ %s\033[0m\n", action)
				}
				stdout, stderr, exitCode := ExecuteAction(ctx, action, 30*time.Second)
				actionResults.WriteString(fmt.Sprintf("$ %s\nexit: %d\nstdout:\n%sstderr:\n%s\n", action, exitCode, stdout, stderr))
			}
		}

		// Print the text part (before/between actions)
		textPart := stripActions(responseText)
		if strings.TrimSpace(textPart) != "" {
			fmt.Fprint(os.Stdout, textPart)
		}

		// Feed results back to KI for next iteration
		conversation = append(conversation, ConversationTurn{
			UserInput: currentInput,
			Response:  responseText,
		})
		currentInput = "Ergebnisse:\n" + actionResults.String() + "\nFasse das Ergebnis kurz und menschlich zusammen. Keine technischen Details (Exit-Codes, Befehle) wiederholen — der User hat sie schon gesehen."
	}

	return "", fmt.Errorf("agent loop: max steps (%d) erreicht", maxSteps)
}

func levelDecision(level ActionLevel) string {
	switch level {
	case ActionBlocked:
		return "blocked"
	case ActionConfirm:
		return "pending"
	case ActionAutoRead:
		return "auto_read"
	case ActionAutoWrite:
		return "auto_write"
	case ActionAutoExec:
		return "auto_exec"
	}
	return "unknown"
}

// buildAgentSystemPrompt extends the system prompt with action instructions
func buildAgentSystemPrompt(shellCtx ShellContext, mem *Memory) string {
	base := buildSystemPrompt(shellCtx, mem, "")

	// Auto-gather context for agent
	var autoContext strings.Builder
	if shellCtx.Cwd != "" {
		if out, _, exitCode := ExecuteAction(context.Background(), "ls -la", 5*time.Second); exitCode == 0 {
			autoContext.WriteString("\nDateien im aktuellen Verzeichnis:\n" + truncateLines(out, 30))
		}
	}
	if shellCtx.GitBranch != "" {
		if out, _, exitCode := ExecuteAction(context.Background(), "git status --short", 5*time.Second); exitCode == 0 && out != "" {
			autoContext.WriteString("\nGit Status:\n" + truncateLines(out, 20))
		}
	}

	agentInstructions := autoContext.String() + `

WICHTIG: Du hast die Fähigkeit, Befehle auszuführen um Informationen zu sammeln.
Schreibe Befehle die du ausführen willst in einen ` + "```action" + ` Block (NICHT ` + "```bash" + `!):

` + "```action" + `
ls -la
` + "```" + `

Die Shell führt den Befehl automatisch aus und gibt dir das Ergebnis zurück.
Du kannst dann basierend auf dem Ergebnis weiterarbeiten oder antworten.

REGELN:
- Führe jeden Befehl NUR EINMAL aus. Keine Duplikate.
- NUR lesen, NICHT schreiben (außer der User bittet explizit darum)
- Kurze, gezielte Befehle — maximal 3 pro Antwort
- Wenn du genug Informationen hast: antworte DIREKT ohne Code-Block
- Antworte MENSCHLICH, nicht technisch. "Du bist ucuber" statt "Exit-Code 0, stdout: ucuber"`

	return base + agentInstructions
}

// extractActions finds ```action or ```bash blocks in the response.
// In agent mode, both are treated as executable actions.
func extractActions(text string) []string {
	var actions []string
	prefixes := []string{"```action\n", "```bash\n", "```sh\n"}

	remaining := text
	for {
		bestIdx := -1
		bestPrefix := ""
		for _, prefix := range prefixes {
			idx := strings.Index(remaining, prefix)
			if idx >= 0 && (bestIdx < 0 || idx < bestIdx) {
				bestIdx = idx
				bestPrefix = prefix
			}
		}
		if bestIdx < 0 {
			break
		}
		start := bestIdx + len(bestPrefix)
		end := strings.Index(remaining[start:], "```")
		if end < 0 {
			break
		}
		action := strings.TrimSpace(remaining[start : start+end])
		if action != "" {
			for _, line := range strings.Split(action, "\n") {
				line = strings.TrimSpace(line)
				if line != "" && !strings.HasPrefix(line, "#") {
					actions = append(actions, line)
				}
			}
		}
		remaining = remaining[start+end+3:]
	}
	return actions
}

// stripActions removes ```action/```bash/```sh blocks from text, keeping the rest
func stripActions(text string) string {
	prefixes := []string{"```action\n", "```bash\n", "```sh\n"}
	for _, prefix := range prefixes {
		for {
			start := strings.Index(text, prefix)
			if start < 0 {
				break
			}
			end := strings.Index(text[start+len(prefix):], "```")
			if end < 0 {
				break
			}
			text = text[:start] + text[start+len(prefix)+end+3:]
		}
	}
	return text
}
