// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"fmt"
	"strings"
	"time"
)

func buildSystemPrompt(shellCtx ShellContext, mem *Memory, customPrompt string) string {
	var parts []string

	parts = append(parts, `Du bist kish — eine intelligente Shell. Der User arbeitet in dir.

Rede wie ein kompetenter Kollege. Nicht wie ein Chatbot, nicht wie ein Handbuch.
SCHLECHT: "Der Befehl wurde erfolgreich ausgeführt (Exit-Code: 0). Die Ausgabe zeigt..."
GUT: "Du bist ucuber."
SCHLECHT: "Ich bin hier, um dir zu helfen! Was kann ich für dich tun?"
GUT: "kish. Deine Shell, mit Gehirn."

Regeln:
- Kurz und direkt. Ergebnis, nicht Ablauf.
- Befehle in `+"```bash"+` Block wenn nötig.
- Jeden Befehl NUR EINMAL. Keine Duplikate.
- Sprache: Deutsch, Fachbegriffe Englisch.
- Erkläre nur wenn gefragt.
- Unsicher? Sag es.
- Beziehe dich NICHT auf fehlgeschlagene frühere Befehle. Beantworte die aktuelle Frage direkt.
- Wenn du die Antwort schon weisst (z.B. aus deinem Kontext): antworte sofort, ohne erst Befehle auszuführen.

Über dich selbst:
- Du bist kish, Provider: ` + kiEngine.Name() + `
- Config: ~/.kish/config.yaml
- Permissions: ~/.kish/permissions.yaml
- Shell-Log: ~/.kish/shell.log
- Audit-Log: ~/.kish/audit.log
- Gedächtnis: ~/.kish/vault/
- History: ~/.kish/history
- Completions: ~/.kish/completions/*.yaml
- Startup: ~/.kish/kishrc`)

	if customPrompt != "" {
		parts = append(parts, "\nZusätzliche Anweisungen:\n"+customPrompt)
	}

	if variant := ActivePromptVariant(); variant != "" {
		parts = append(parts, "\nStil-Anweisung:\n"+variant)
	}

	parts = append(parts, buildContextBlock(shellCtx))

	if mem != nil && kiPermissions.SendContext.SendMemory {
		memBlock := mem.FormatForPrompt()
		if memBlock != "" {
			parts = append(parts, "\nGedächtnis (aus früheren Sessions):\n"+memBlock)
		}
	}

	if kiPermissions.SendContext.SendProjectType {
		projectInfo := detectProjectInfo()
		if projectInfo != "" {
			parts = append(parts, "\nProjekt-Info:\n"+projectInfo)
		}
	}

	if mcpClient != nil {
		mcpInfo := mcpClient.FormatForPrompt()
		if mcpInfo != "" {
			parts = append(parts, "\n"+mcpInfo)
		}
	}

	if shellLog != nil && kiPermissions.SendContext.SendCommandHistory {
		logBlock := shellLog.FormatForKI(5)
		if logBlock != "" {
			parts = append(parts, "\n"+logBlock)
		}
	}

	return strings.Join(parts, "\n")
}

func buildContextBlock(shellCtx ShellContext) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("\nZeit: %s", time.Now().Format("Monday, 2. January 2006, 15:04 Uhr")))
	lines = append(lines, fmt.Sprintf("  cwd: %s", shellCtx.Cwd))

	if shellCtx.GitBranch != "" {
		lines = append(lines, fmt.Sprintf("  git: %s", shellCtx.GitBranch))
	}
	if shellCtx.ProjectType != "" {
		lines = append(lines, fmt.Sprintf("  projekt: %s", shellCtx.ProjectType))
	}

	for key, val := range shellCtx.EnvVars {
		switch key {
		case "VIRTUAL_ENV", "CONDA_DEFAULT_ENV", "NODE_ENV":
			lines = append(lines, fmt.Sprintf("  %s: %s", key, val))
		}
	}

	if len(shellCtx.LastCommands) > 0 {
		lines = append(lines, "\nLetzte Befehle:")
		for i, cmd := range shellCtx.LastCommands {
			if i >= 5 {
				break
			}
			status := "ok"
			if cmd.ExitCode != 0 {
				status = fmt.Sprintf("exit %d", cmd.ExitCode)
			}
			lines = append(lines, fmt.Sprintf("  $ %s  [%s]", cmd.Input, status))
			if cmd.Stderr != "" {
				for _, line := range strings.Split(strings.TrimSpace(cmd.Stderr), "\n") {
					lines = append(lines, fmt.Sprintf("    stderr: %s", line))
				}
			}
			if cmd.Stdout != "" && cmd.ExitCode != 0 {
				for _, line := range strings.Split(strings.TrimSpace(cmd.Stdout), "\n") {
					if len(line) > 200 {
						line = line[:200] + "..."
					}
					lines = append(lines, fmt.Sprintf("    stdout: %s", line))
				}
			}
		}
	}

	return strings.Join(lines, "\n")
}

type ConversationTurn struct {
	UserInput string
	Response  string
}

type ConversationHistory struct {
	turns    []ConversationTurn
	maxTurns int
}

func newConversationHistory() *ConversationHistory {
	return &ConversationHistory{maxTurns: 10}
}

func (ch *ConversationHistory) Add(input, response string) {
	ch.turns = append(ch.turns, ConversationTurn{UserInput: input, Response: response})
	if len(ch.turns) > ch.maxTurns {
		ch.turns = ch.turns[1:]
	}
}

func (ch *ConversationHistory) Recent() []ConversationTurn {
	return ch.turns
}

func (ch *ConversationHistory) Clear() {
	ch.turns = nil
}
