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

func ContinuousMode(ctx context.Context, runner *interp.Runner, stdoutTee, stderrTee *TeeWriter) {
	ensureKIEngine()

	if pe, ok := kiEngine.(*ProviderEngine); ok {
		pe.SetSystemPromptOverride(continuousSystemPrompt)
		defer pe.SetSystemPromptOverride("")
	}

	fmt.Fprintln(os.Stderr, "\033[1;36m[ki]\033[0m Dialog started. Shell commands are detected and executed.")
	fmt.Fprintln(os.Stderr, "\033[2mEnd with: ki stop\033[0m")

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
	var conversation []ConversationTurn

	for {
		line, err := rl.Readline()
		if err != nil {
			if err == readline.ErrInterrupt || err == io.EOF {
				fmt.Fprintln(os.Stderr, "\033[1;36m[ki]\033[0m Dialog ended.")
			}
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "!") && line != "!" {
			expanded := expandBang(line)
			if expanded != "" {
				fmt.Fprintf(os.Stderr, "\033[2m%s\033[0m\n", expanded)
				line = expanded
			}
		}

		if line == "history" || strings.HasPrefix(line, "history ") {
			printHistory(strings.Fields(line))
			continue
		}

		if line == "stop" || line == "@ki stop" || line == "ki stop" {
			fmt.Fprintln(os.Stderr, "\033[1;36m[ki]\033[0m Dialog ended.")
			return
		}

		lineLower := strings.ToLower(line)
		if memKey, memVal, ok := parseMemoryCommand(lineLower, line); ok {
			if memVal == "" {
				for _, cat := range []string{"fact", "session", "scratch"} {
					path := filepath.Join(kishDir(), "vault", cat, sanitizeFilename(memKey)+".yaml")
					os.Remove(path)
				}
				fmt.Fprintf(os.Stderr, "\033[2mForgotten: %s\033[0m\n", memKey)
			} else {
				kiMemory.Store(memKey, memVal, "fact", nil)
				fmt.Fprintf(os.Stderr, "\033[2mRemembered: %s → %s\033[0m\n", memKey, memVal)
			}
			continue
		}

		isShell := looksLikeShellCommand(line, parser)

		if isShell {
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

			shellContext.Record(line, 0, capturedOut, capturedErr)
			if shellLog != nil {
				shellLog.Record(line, 0, capturedOut, capturedErr)
			}

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
				Response:  "(command executed, output shown above)",
			})
		} else {
			rawCtx := shellContext.Collect()
			filteredCtx := kiPermissions.FilterContext(rawCtx)

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
			fmt.Fprintln(os.Stdout)

			if err != nil {
				if ctx.Err() != nil {
					fmt.Fprintln(os.Stderr, "\n[ki] Cancelled.")
					return
				}
				fmt.Fprintf(os.Stderr, "kish: ki error: %s\n", err)
				continue
			}

			responseText := output.String()

			actions := extractActions(responseText)
			if len(actions) > 0 {
				for _, action := range actions {
					level, reason := ClassifyAction(action, &kiPermissions)
					if level == ActionBlocked {
						fmt.Fprintf(os.Stderr, "\033[1;31m[BLOCKIERT]\033[0m %s — %s\n", action, reason)
						continue
					}
					if level == ActionAutoRead {
						fmt.Fprintf(os.Stderr, "\033[2m$ %s\033[0m\n", action)
						stdout, stderr, exitCode := ExecuteAction(ctx, action, 30*1e9)
						if stdout != "" {
							fmt.Fprint(os.Stdout, stdout)
						}
						logAction(action, stdout, stderr, exitCode)
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

			conversation = append(conversation, ConversationTurn{
				UserInput: line,
				Response:  responseText,
			})

			if len(conversation) > 20 {
				conversation = conversation[len(conversation)-20:]
			}
		}
	}
}

// looksLikeShellCommand uses heuristics to detect shell commands.
// Conservative: when in doubt, treat as natural language.
func looksLikeShellCommand(input string, parser *syntax.Parser) bool {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return false
	}
	firstWord := fields[0]

	for _, op := range []string{"|", ">", "<", "&&", "||", ";", "$(", "`"} {
		if strings.Contains(input, op) {
			return true
		}
	}

	if parts := strings.SplitN(input, "=", 2); len(parts) == 2 && !strings.Contains(parts[0], " ") {
		return true
	}

	if strings.HasPrefix(input, "./") || strings.HasPrefix(input, "/") {
		return true
	}

	if shellKeywords[firstWord] {
		return true
	}

	if commandInPath(firstWord) {
		return true
	}

	return false
}

func parseMemoryCommand(lower, original string) (string, string, bool) {
	for _, prefix := range []string{"vergiss ", "forget "} {
		if strings.HasPrefix(lower, prefix) {
			key := strings.TrimSpace(original[len(prefix):])
			return sanitizeFilename(key), "", true
		}
	}

	for _, prefix := range []string{"merk dir: ", "merk dir:", "merke: ", "merke:", "remember: "} {
		if strings.HasPrefix(lower, prefix) {
			fact := strings.TrimSpace(original[len(prefix):])
			if fact == "" {
				return "", "", false
			}
			return sanitizeFilename(fact), fact, true
		}
	}

	for _, prefix := range []string{"merk dir ", "merke "} {
		if strings.HasPrefix(lower, prefix) {
			rest := strings.TrimSpace(original[len(prefix):])
			if rest == "" {
				return "", "", false
			}
			parts := strings.SplitN(rest, " ", 2)
			if len(parts) == 2 {
				if len(parts[0]) <= 20 && !strings.Contains(parts[0], " ") {
					return sanitizeFilename(parts[0]), parts[1], true
				}
			}
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
