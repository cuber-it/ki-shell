// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ergochat/readline"

	"github.com/cuber-it/ki-shell/kish-sh/v3/interp"
	"github.com/cuber-it/ki-shell/kish-sh/v3/syntax"
)

// ContinuousMode runs an interactive dialog where everything goes through the KI.
// Shell commands are detected, executed, and their output is fed back to the KI.
// The KI sees the full conversation including command results.
func ContinuousMode(ctx context.Context, runner *interp.Runner, stdoutTee, stderrTee *TeeWriter) {
	ensureKIEngine()

	// Save and restore original system prompt
	if pe, ok := kiEngine.(*ProviderEngine); ok {
		pe.SetSystemPromptOverride(continuousSystemPrompt)
		defer pe.SetSystemPromptOverride("")
	}

	fmt.Fprintln(os.Stderr, "\033[1;36m[ki]\033[0m Dialog gestartet. Shell-Befehle werden erkannt und ausgeführt.")
	fmt.Fprintln(os.Stderr, "\033[2mBeenden mit: @ki stop\033[0m")

	rl, err := readline.NewFromConfig(&readline.Config{
		Prompt:          "\033[1;36mki>\033[0m ",
		InterruptPrompt: "^C",
		EOFPrompt:       "stop",
		HistoryFile:     filepath.Join(kishDir(), "history"),
		HistoryLimit:    10000,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "kish: readline error: %s\n", err)
		return
	}
	defer rl.Close()

	parser := syntax.NewParser(syntax.Variant(syntax.LangBash))

	// Conversation accumulator — everything the KI sees
	var conversation []ConversationTurn

	for {
		line, err := rl.Readline()
		if err != nil {
			if err == readline.ErrInterrupt {
				fmt.Fprintln(os.Stderr, "\n\033[1;36m[ki]\033[0m Dialog beendet.")
				return
			}
			if err == io.EOF {
				fmt.Fprintln(os.Stderr, "\033[1;36m[ki]\033[0m Dialog beendet.")
				return
			}
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Bang expansion
		if strings.HasPrefix(line, "!") && line != "!" {
			expanded := expandBang(line, rl)
			if expanded != "" {
				fmt.Fprintf(os.Stderr, "\033[2m%s\033[0m\n", expanded)
				line = expanded
			}
		}

		// history builtin
		if line == "history" || strings.HasPrefix(line, "history ") {
			printHistory(strings.Fields(line))
			continue
		}

		// Exit commands
		if line == "stop" || line == "@ki stop" || line == "ki stop" {
			fmt.Fprintln(os.Stderr, "\033[1;36m[ki]\033[0m Dialog beendet.")
			return
		}

		// Memory commands in continuous mode
		// Supports: "merk dir: ich bevorzuge vim", "merke: docker läuft auf port 8080",
		// "merke name ulrich", "vergiss name"
		lineLower := strings.ToLower(line)
		if memKey, memVal, ok := parseMemoryCommand(lineLower, line); ok {
			if memVal == "" {
				// vergiss
				for _, cat := range []string{"fact", "session", "scratch"} {
					path := filepath.Join(kishDir(), "vault", cat, sanitizeFilename(memKey)+".yaml")
					os.Remove(path)
				}
				fmt.Fprintf(os.Stderr, "\033[2mVergessen: %s\033[0m\n", memKey)
			} else {
				kiMemory.Store(memKey, memVal, "fact", nil)
				fmt.Fprintf(os.Stderr, "\033[2mGemerkt: %s → %s\033[0m\n", memKey, memVal)
			}
			continue
		}

		// Check if this looks like a shell command
		isShell := looksLikeShellCommand(line, parser)

		if isShell {
			// Execute the command and capture output
			stdoutTee.Reset()
			stderrTee.Reset()

			prog, parseErr := parser.Parse(strings.NewReader(line), "")
			if parseErr != nil {
				fmt.Fprintf(os.Stderr, "kish: %s\n", parseErr)
				continue
			}

			for _, stmt := range prog.Stmts {
				runner.Run(ctx, stmt)
			}

			capturedOut := stdoutTee.String()
			capturedErr := stderrTee.String()

			// Record in shell log
			shellContext.Record(line, 0, capturedOut, capturedErr)
			if shellLog != nil {
				shellLog.Record(line, 0, capturedOut, capturedErr)
			}

			// Feed command + output to conversation for KI context
			var cmdResult strings.Builder
			cmdResult.WriteString(fmt.Sprintf("User hat ausgeführt: $ %s\n", line))
			if capturedOut != "" {
				cmdResult.WriteString("stdout:\n" + truncateLines(capturedOut, 100) + "\n")
			}
			if capturedErr != "" {
				cmdResult.WriteString("stderr:\n" + truncateLines(capturedErr, 50) + "\n")
			}
			conversation = append(conversation, ConversationTurn{
				UserInput: cmdResult.String(),
				Response:  "(Befehl ausgeführt, Output oben sichtbar)",
			})
		} else {
			// Natural language — send to KI with full conversation context
			rawCtx := shellContext.Collect()
			filteredCtx := kiPermissions.FilterContext(rawCtx)

			// System prompt override already set at mode entry

			// Sync conversation history
			kiConversation.Clear()
			for _, turn := range conversation {
				kiConversation.Add(turn.UserInput, turn.Response)
			}

			if audit != nil {
				audit.LogQuery(line, kiEngine.Name())
			}

			var output strings.Builder
			teeOut := io.MultiWriter(os.Stdout, &output)
			_, err := kiEngine.Query(ctx, line, filteredCtx, teeOut)
			fmt.Fprintln(os.Stdout) // newline after streamed response

			if err != nil {
				if ctx.Err() != nil {
					fmt.Fprintln(os.Stderr, "\n[ki] Abgebrochen.")
					return
				}
				fmt.Fprintf(os.Stderr, "kish: ki error: %s\n", err)
				continue
			}

			responseText := output.String()

			// Check if KI response contains commands to execute
			actions := extractActions(responseText)
			if len(actions) > 0 {
				for _, action := range actions {
					level, reason := ClassifyAction(action, &kiPermissions)
					if level == ActionBlocked {
						fmt.Fprintf(os.Stderr, "\033[1;31m[BLOCKIERT]\033[0m %s — %s\n", action, reason)
						continue
					}
					if level == ActionAutoRead {
						fmt.Fprintf(os.Stderr, "\033[2m→ %s\033[0m\n", action)
						stdout, stderr, exitCode := ExecuteAction(ctx, action, 30*1e9)
						if stdout != "" {
							fmt.Fprint(os.Stdout, stdout)
						}
						conversation = append(conversation, ConversationTurn{
							UserInput: fmt.Sprintf("(auto-executed: %s → exit %d)", action, exitCode),
							Response:  stdout + stderr,
						})
					} else if level == ActionConfirm {
						result := Confirm(action, reason, ConfirmNormal)
						if result == ConfirmYes {
							stdout, stderr, exitCode := ExecuteAction(ctx, action, 30*1e9)
							if stdout != "" {
								fmt.Fprint(os.Stdout, stdout)
							}
							conversation = append(conversation, ConversationTurn{
								UserInput: fmt.Sprintf("(confirmed: %s → exit %d)", action, exitCode),
								Response:  stdout + stderr,
							})
						}
					}
				}
			}

			// Record conversation turn
			conversation = append(conversation, ConversationTurn{
				UserInput: line,
				Response:  responseText,
			})

			// Keep conversation manageable
			if len(conversation) > 20 {
				conversation = conversation[len(conversation)-20:]
			}
		}
	}
}

// looksLikeShellCommand checks if input is likely a shell command.
// Used only in continuous mode. Conservative: when in doubt, it's text.
func looksLikeShellCommand(input string, parser *syntax.Parser) bool {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return false
	}
	firstWord := fields[0]

	// Shell operators → definitely shell
	for _, op := range []string{"|", ">", "<", "&&", "||", ";", "$(", "`"} {
		if strings.Contains(input, op) {
			return true
		}
	}

	// Variable assignment
	if parts := strings.SplitN(input, "=", 2); len(parts) == 2 && !strings.Contains(parts[0], " ") {
		return true
	}

	// Starts with ./ or /
	if strings.HasPrefix(input, "./") || strings.HasPrefix(input, "/") {
		return true
	}

	// Shell keywords
	if shellKeywords[firstWord] {
		return true
	}

	// Known command in PATH
	if commandInPath(firstWord) {
		return true
	}

	// Try to parse — if it parses as valid shell, it's probably shell
	_, err := parser.Parse(strings.NewReader(input), "")
	if err == nil {
		// Parsed successfully, but could still be natural language
		// Only treat as shell if first word is a command
		return false // conservative: if not caught above, it's text
	}

	return false
}

// parseMemoryCommand detects memory commands in natural language.
// Returns (key, value, matched). Empty value = forget command.
//
// Supported patterns:
//   "merk dir: ich bevorzuge vim"           → key=ich-bevorzuge-vim, val=ich bevorzuge vim
//   "merk dir ich bevorzuge vim"            → same
//   "merke: docker läuft auf port 8080"     → key=docker-laeuft-auf-port-8080, val=docker läuft auf port 8080
//   "merke name ulrich"                     → key=name, val=ulrich
//   "vergiss name"                          → key=name, val="" (delete)
//   "erinnere name"                         → not handled here (search, not store)
func parseMemoryCommand(lower, original string) (string, string, bool) {
	// Forget
	for _, prefix := range []string{"vergiss ", "forget "} {
		if strings.HasPrefix(lower, prefix) {
			key := strings.TrimSpace(original[len(prefix):])
			return sanitizeFilename(key), "", true
		}
	}

	// Remember with colon: "merk dir: ..." or "merke: ..."
	for _, prefix := range []string{"merk dir: ", "merk dir:", "merke: ", "merke:", "remember: "} {
		if strings.HasPrefix(lower, prefix) {
			fact := strings.TrimSpace(original[len(prefix):])
			if fact == "" {
				return "", "", false
			}
			key := sanitizeFilename(fact)
			return key, fact, true
		}
	}

	// Remember without colon: "merk dir ich mag vim"
	for _, prefix := range []string{"merk dir ", "merke "} {
		if strings.HasPrefix(lower, prefix) {
			rest := strings.TrimSpace(original[len(prefix):])
			if rest == "" {
				return "", "", false
			}
			// "merke name ulrich" → key=name, val=ulrich
			// "merke ich mag vim" → key=ich-mag-vim, val=ich mag vim
			parts := strings.SplitN(rest, " ", 2)
			if len(parts) == 2 {
				// Could be "merke key value" or "merke natural sentence"
				// Heuristic: if first word is short and second is longer, treat as key/value
				if len(parts[0]) <= 20 && !strings.Contains(parts[0], " ") {
					return sanitizeFilename(parts[0]), parts[1], true
				}
			}
			// Treat whole thing as the fact
			key := sanitizeFilename(rest)
			return key, rest, true
		}
	}

	return "", "", false
}

var continuousSystemPrompt = `Du bist kish, eine KI-Shell. Dialog-Modus.

WICHTIG — So redest du NICHT:
- "Ich bin hier, um dir zu helfen!" ← Kundenservice-Müll
- "Konnte ich dir irgendwie behilflich sein?" ← Nein.
- "Was kann ich für dich tun?" ← Langweilig.

So redest du:
- Direkt, knapp, mit Persönlichkeit. Wie ein kompetenter Kollege.
- "wer bist du?" → "kish. Deine Shell, mit Gehirn."
- "was kannst du?" → "Befehle ausführen, Code analysieren, Fehler finden. Frag einfach."
- Humor ist OK. Steifheit nicht.
- Wenn der User Smalltalk macht: kurz drauf eingehen, nicht abwürgen.

Shell-Befehle die der User tippt werden direkt ausgeführt. Du siehst den Output.
Wenn du einen Befehl vorschlägst, pack ihn in einen ` + "```bash" + ` Block.`
