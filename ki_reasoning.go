// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
//
// Agent-level reasoning: makes dumb models smarter by decomposing
// the task before generating commands. Based on heinzel-ai reasoning addon.
package main

import (
	"context"
	"fmt"
	"strings"
)

// preThink runs a decomposition step before the main KI query.
// Returns an enriched input that includes the analysis.
// Only runs for complex queries (more than 5 words).
func preThink(ctx context.Context, input string, shellCtx ShellContext) string {
	words := strings.Fields(input)
	if len(words) <= 5 {
		return input // simple query, no pre-thinking needed
	}

	ensureKIEngine()
	if !kiEngine.Available() {
		return input
	}

	vPrint(1, "Pre-thinking: decomposing task...")

	filteredCtx := kiPermissions.FilterContext(shellCtx)

	thinkPrompt := `Du bist ein analytischer Planer. Der User will etwas auf der Shell erledigen.
Zerlege die Aufgabe in 2-4 konkrete Schritte. Für jeden Schritt: welcher Shell-Befehl?
WICHTIG:
- stderr immer mit 2>/dev/null unterdrücken
- pro Repo: git -C "$repo" remote -v (NICHT im Home-Dir)
- Mehrzeilige Scripts in EINEN Block
Antworte NUR mit der Schritt-Liste, kein Smalltalk.`

	// Temporarily override system prompt for the think query
	if pe, ok := kiEngine.(*ProviderEngine); ok {
		orig := pe.sysPromptOverride
		pe.SetSystemPromptOverride(thinkPrompt)
		defer pe.SetSystemPromptOverride(orig)
	}

	var thinkOutput strings.Builder
	_, err := kiEngine.Query(ctx, input, filteredCtx, &thinkOutput)
	if err != nil {
		vPrint(1, "Pre-thinking failed: %s", err)
		return input
	}

	plan := strings.TrimSpace(thinkOutput.String())
	if plan == "" {
		return input
	}

	vPrint(1, "Plan:\n%s", plan)

	// Return enriched input: original question + plan
	return fmt.Sprintf("%s\n\nVoranalyse (nutze diese als Grundlage):\n%s", input, plan)
}

// shouldPreThink decides whether pre-thinking would help.
// Skipped for simple commands, ? queries, and when verbose is off.
func shouldPreThink(input string) bool {
	if len(strings.Fields(input)) <= 5 {
		return false
	}
	// Skip for simple patterns
	if strings.HasPrefix(input, "was ist") || strings.HasPrefix(input, "erkläre") ||
		strings.HasPrefix(input, "what is") || strings.HasPrefix(input, "explain") {
		return false
	}
	return true
}
