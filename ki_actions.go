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

type ActionLevel int

const (
	ActionBlocked   ActionLevel = iota
	ActionConfirm
	ActionAutoRead
	ActionAutoWrite
	ActionAutoExec
)

var interactiveCommands = map[string]bool{
	"vi": true, "vim": true, "nvim": true, "nano": true, "emacs": true,
	"visudo": true, "vipw": true, "vigr": true, "crontab": true,
	"less": true, "more": true, "top": true, "htop": true, "btop": true,
	"tmux": true, "screen": true, "mc": true,
	"python": true, "python3": true, "node": true, "irb": true,
	"mysql": true, "psql": true, "sqlite3": true, "redis-cli": true,
	"ssh": true,
}

var DefaultReadOnlyCommands = []string{
	"ls", "cat", "head", "tail", "wc", "file", "stat", "find", "locate",
	"du", "df", "tree", "realpath", "basename", "dirname",
	"grep", "awk", "sed", "sort", "uniq", "cut", "tr", "diff", "comm",
	"uname", "hostname", "whoami", "id", "date", "uptime", "free",
	"lsb_release", "arch", "nproc", "lscpu", "lsblk",
	"ps", "top", "htop", "pgrep",
	"ping", "host", "dig", "nslookup", "curl", "wget",
	"ip", "ifconfig", "ss", "netstat",
	"git status", "git log", "git diff", "git branch", "git remote",
	"git show", "git blame", "git tag", "git stash list",
	"docker ps", "docker images", "docker logs", "docker inspect",
	"docker stats", "docker top", "docker port", "docker network ls",
	"docker volume ls", "docker compose ps", "docker compose logs",
	// SSH is handled separately in ClassifyAction (recursive check)
	"dpkg -l", "apt list", "rpm -qa", "pip list", "npm list", "go list",
	"kubectl get", "kubectl describe", "kubectl logs", "kubectl top",
}

func ClassifyAction(command string, perms *Permissions) (ActionLevel, string) {
	allowed, _, reason := perms.CheckCommand(command)
	if !allowed {
		return ActionBlocked, reason
	}

	firstWord := strings.Fields(command)[0]
	if firstWord == "ssh" {
		// ssh with remote command is OK, ssh without is interactive
		if extractSSHCommand(command) == "" {
			return ActionConfirm, "Interactive: ssh session needs terminal"
		}
	} else if interactiveCommands[firstWord] {
		return ActionConfirm, "Interactive: needs terminal"
	} else if firstWord == "sudo" && len(strings.Fields(command)) > 1 {
		if interactiveCommands[strings.Fields(command)[1]] {
			return ActionConfirm, "Interactive: needs terminal"
		}
	}

	cmdLower := strings.ToLower(strings.TrimSpace(command))

	if perms.AutoExecute {
		if perms.ConfirmDestructive {
			for _, pattern := range perms.DestructivePatterns {
				if strings.Contains(cmdLower, strings.ToLower(pattern)) {
					return ActionConfirm, fmt.Sprintf("Destructive: '%s'", pattern)
				}
			}
		}
		return ActionAutoExec, ""
	}

	readOnlyList := DefaultReadOnlyCommands
	if len(perms.AllowedCommands) > 0 {
		readOnlyList = append(readOnlyList, perms.AllowedCommands...)
	}
	for _, pattern := range readOnlyList {
		patLower := strings.ToLower(pattern)
		if strings.HasPrefix(cmdLower, patLower) {
			rest := cmdLower[len(patLower):]
			if rest == "" || rest[0] == ' ' {
				return ActionAutoRead, ""
			}
		}
	}

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
		return ActionConfirm, "SSH connection needs confirmation"
	}

	return ActionConfirm, ""
}

func extractSSHCommand(command string) string {
	parts := strings.Fields(command)
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
			continue
		}
		return strings.Join(parts[i:], " ")
	}
	return ""
}

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

	stdoutStr := truncateLines(stdout.String(), 500)
	stderrStr := truncateLines(stderr.String(), 100)
	return stdoutStr, stderrStr, exitCode
}

func RunAgentLoop(ctx context.Context, engine KIEngine, input string, shellCtx ShellContext, mem *Memory, maxSteps int) (string, error) {
	if maxSteps == 0 {
		maxSteps = 5
	}

	var conversation []ConversationTurn
	currentInput := input

	if pe, ok := engine.(*ProviderEngine); ok {
		pe.SetSystemPromptOverride(buildAgentSystemPrompt(shellCtx, mem))
		defer pe.SetSystemPromptOverride("")
	}

	for step := 0; step < maxSteps; step++ {
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "\n[kish/ki] Cancelled.")
			return "", ctx.Err()
		default:
		}

		vStep(step, maxSteps, currentInput)

		var output bytes.Buffer
		resp, err := engine.Query(ctx, currentInput, shellCtx, &output)
		if err != nil {
			if ctx.Err() != nil {
				fmt.Fprintln(os.Stderr, "\n[kish/ki] Cancelled.")
				return "", ctx.Err()
			}
			return "", err
		}

		responseText := output.String()
		vKIResponse(responseText)

		actions := extractActions(responseText)
		if len(actions) == 0 {
			vPrint(1, "--- Final answer (no more actions) ---")
			fmt.Fprint(os.Stdout, responseText)
			fmt.Fprintln(os.Stdout)
			return resp.Text, nil
		}

		seen := make(map[string]bool)
		var unique []string
		for _, a := range actions {
			if !seen[a] {
				seen[a] = true
				unique = append(unique, a)
			}
		}
		actions = unique

		vPrint(1, "KI requested %d action(s)", len(actions))

		var actionResults strings.Builder
		for i, action := range actions {
			level, reason := ClassifyAction(action, &kiPermissions)
			vAction(action, level, i+1, len(actions))

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
					fmt.Fprintf(os.Stderr, "\033[2m$ %s\033[0m\n", action)
					stdout, stderr, exitCode := ExecuteAction(ctx, action, 30*time.Second)
					if stdout != "" {
						fmt.Fprint(os.Stdout, stdout)
					}
					logAction(action, stdout, stderr, exitCode)
					actionResults.WriteString(fmt.Sprintf("$ %s\nexit: %d\nstdout:\n%sstderr:\n%s\n", action, exitCode, stdout, stderr))
				} else {
					actionResults.WriteString(fmt.Sprintf("ABGELEHNT: %s (User hat abgelehnt)\n", action))
				}

			case ActionAutoRead, ActionAutoWrite, ActionAutoExec:
				fmt.Fprintf(os.Stderr, "\033[2m$ %s\033[0m\n", action)
				stdout, stderr, exitCode := ExecuteAction(ctx, action, 30*time.Second)
				if stdout != "" {
					fmt.Fprint(os.Stdout, stdout)
				}
				if stderr != "" {
					fmt.Fprintf(os.Stderr, "\033[31m%s\033[0m", stderr)
				}
				logAction(action, stdout, stderr, exitCode)
				vActionResult(action, exitCode, stdout, stderr)
				actionResults.WriteString(fmt.Sprintf("$ %s\nexit: %d\nstdout:\n%sstderr:\n%s\n", action, exitCode, stdout, stderr))
			}
		}

		textPart := stripActions(responseText)
		if strings.TrimSpace(textPart) != "" {
			fmt.Fprint(os.Stdout, textPart)
		}

		conversation = append(conversation, ConversationTurn{
			UserInput: currentInput,
			Response:  responseText,
		})
		currentInput = "Results:\n" + actionResults.String() + "\nSummarize the results briefly. Don't repeat technical details — the user already saw them."
	}

	return "", fmt.Errorf("agent loop: max steps (%d) reached", maxSteps)
}

// logAction writes ===> / <=== with timestamps to the shell log (not to screen).
func logAction(command, stdout, stderr string, exitCode int) {
	if shellLog == nil {
		return
	}
	ts := time.Now().Format("2006-01-02 15:04:05")
	var entry strings.Builder
	entry.WriteString(fmt.Sprintf("== %s ==>\n%s\n", ts, command))
	if stdout != "" {
		entry.WriteString(fmt.Sprintf("<== %s ==\n%s", ts, stdout))
		if !strings.HasSuffix(stdout, "\n") {
			entry.WriteString("\n")
		}
	}
	if stderr != "" {
		entry.WriteString(fmt.Sprintf("[stderr] %s", stderr))
		if !strings.HasSuffix(stderr, "\n") {
			entry.WriteString("\n")
		}
	}
	entry.WriteString(fmt.Sprintf("exit: %d\n", exitCode))
	shellLog.Record(command, exitCode, stdout, stderr)
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

func buildAgentSystemPrompt(shellCtx ShellContext, mem *Memory) string {
	base := buildSystemPrompt(shellCtx, mem, "")

	var autoCtx strings.Builder
	if shellCtx.Cwd != "" {
		if out, _, exitCode := ExecuteAction(context.Background(), "ls -la", 5*time.Second); exitCode == 0 {
			autoCtx.WriteString("\nFiles in current directory:\n" + truncateLines(out, 30))
		}
	}
	if shellCtx.GitBranch != "" {
		if out, _, exitCode := ExecuteAction(context.Background(), "git status --short", 5*time.Second); exitCode == 0 && out != "" {
			autoCtx.WriteString("\nGit Status:\n" + truncateLines(out, 20))
		}
	}

	agentInstructions := autoCtx.String() + `

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
- Mehrzeilige Scripts in EINEN Code-Block (nicht aufteilen!)
- stderr immer unterdrücken: 2>/dev/null
- Wenn du genug Informationen hast: antworte DIREKT ohne Code-Block
- Antworte MENSCHLICH, nicht technisch. "Du bist ucuber" statt "Exit-Code 0, stdout: ucuber"`

	return base + agentInstructions
}

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
			// Multi-line scripts stay as one action (executed via sh -c)
			// Single-line commands are added individually
			lines := strings.Split(action, "\n")
			nonEmpty := 0
			for _, l := range lines {
				if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
					nonEmpty++
				}
			}
			if nonEmpty > 1 {
				actions = append(actions, action) // keep as multi-line script
			} else {
				for _, l := range lines {
					if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
						actions = append(actions, l)
					}
				}
			}
		}
		remaining = remaining[start+end+3:]
	}
	return actions
}

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
