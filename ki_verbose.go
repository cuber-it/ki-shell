// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"fmt"
	"os"
	"strings"
)

// VerboseLevel controls how much the KI shows about its internal process
// -v0 (default): minimal — only KI response text, auto-read actions as dim "→ cmd"
// -v1: show thinking — actions requested, step count, what KI is doing
// -v2: full debug — system prompt, KI request, KI response, action results
var verboseLevel int

// vPrint prints a message at the given verbose level
func vPrint(level int, format string, args ...interface{}) {
	if verboseLevel < level {
		return
	}
	prefix := ""
	color := "\033[2m" // dim
	switch level {
	case 1:
		prefix = "[v1] "
		color = "\033[2;36m" // dim cyan
	case 2:
		prefix = "[v2] "
		color = "\033[2;35m" // dim magenta
	}
	msg := fmt.Sprintf(format, args...)
	for _, line := range strings.Split(msg, "\n") {
		fmt.Fprintf(os.Stderr, "%s%s%s\033[0m\n", color, prefix, line)
	}
}

// vAction logs an action at v1+ level (v0 output is handled inline by the caller)
func vAction(action string, level ActionLevel, step int, maxSteps int) {
	if verboseLevel < 1 {
		return
	}
	levelName := ""
	color := ""
	switch level {
	case ActionAutoRead:
		color = "\033[2m"
		levelName = "read"
	case ActionAutoWrite:
		color = "\033[33m"
		levelName = "write"
	case ActionAutoExec:
		color = "\033[35m"
		levelName = "exec"
	case ActionConfirm:
		color = "\033[1;33m"
		levelName = "confirm"
	case ActionBlocked:
		color = "\033[1;31m"
		levelName = "BLOCKED"
	}
	fmt.Fprintf(os.Stderr, "%s[step %d/%d] [%s] → %s\033[0m\n", color, step, maxSteps, levelName, action)
}

// vSystemPrompt logs the system prompt at v2 level
func vSystemPrompt(prompt string) {
	if verboseLevel < 2 {
		return
	}
	vPrint(2, "=== SYSTEM PROMPT ===")
	if len(prompt) > 500 {
		vPrint(2, "%s...", prompt[:500])
	} else {
		vPrint(2, "%s", prompt)
	}
	vPrint(2, "=== END SYSTEM PROMPT ===")
}

// vKIRequest logs the user input sent to the KI
func vKIRequest(input string) {
	if verboseLevel < 2 {
		return
	}
	vPrint(2, "=== KI REQUEST ===")
	vPrint(2, "%s", input)
	vPrint(2, "=== END KI REQUEST ===")
}

// vKIResponse logs the raw KI response
func vKIResponse(response string) {
	if verboseLevel < 2 {
		return
	}
	vPrint(2, "=== KI RESPONSE ===")
	if len(response) > 1000 {
		vPrint(2, "%s...", response[:1000])
	} else {
		vPrint(2, "%s", response)
	}
	vPrint(2, "=== END KI RESPONSE ===")
}

// vActionResult logs the output of an executed action
func vActionResult(action string, exitCode int, stdout, stderr string) {
	if verboseLevel < 2 {
		return
	}
	vPrint(2, "=== ACTION RESULT: %s (exit:%d) ===", action, exitCode)
	if stdout != "" {
		out := stdout
		if len(out) > 500 {
			out = out[:500] + "..."
		}
		vPrint(2, "stdout: %s", out)
	}
	if stderr != "" {
		vPrint(2, "stderr: %s", stderr)
	}
	vPrint(2, "=== END ACTION RESULT ===")
}

// vStep logs an agent loop iteration
func vStep(step, maxSteps int, input string) {
	if verboseLevel < 1 {
		return
	}
	vPrint(1, "--- Agent Step %d/%d ---", step+1, maxSteps)
	if verboseLevel >= 2 {
		vPrint(2, "Input: %s", input)
	}
}
