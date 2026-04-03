// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
)

// ConfirmLevel indicates the severity of the confirmation request
type ConfirmLevel int

const (
	ConfirmNormal      ConfirmLevel = iota // yellow — standard confirmation
	ConfirmDestructive                     // red — dangerous operation
	ConfirmInfo                            // cyan — informational, low risk
)

// ConfirmResult is what the user chose
type ConfirmResult int

const (
	ConfirmYes  ConfirmResult = iota
	ConfirmNo
	ConfirmEdit // user wants to modify the command before executing
)

// confirmedCommands tracks commands the user already confirmed in this session.
// Prevents asking the same question twice for repeated patterns.
var (
	confirmedMu       sync.Mutex
	confirmedCommands = make(map[string]bool)
)

// Confirm asks the user for confirmation with colored output.
// Returns the user's choice.
func Confirm(command string, reason string, level ConfirmLevel) ConfirmResult {
	// Check if already confirmed in this session
	confirmedMu.Lock()
	if confirmedCommands[command] {
		confirmedMu.Unlock()
		fmt.Fprintf(os.Stderr, "\033[2m→ %s (bereits bestätigt)\033[0m\n", command)
		return ConfirmYes
	}
	confirmedMu.Unlock()

	// Color and label based on level
	var color, label string
	switch level {
	case ConfirmDestructive:
		color = "\033[1;31m" // bold red
		label = "ACHTUNG"
	case ConfirmNormal:
		color = "\033[1;33m" // bold yellow
		label = "Vorschlag"
	case ConfirmInfo:
		color = "\033[1;36m" // bold cyan
		label = "Aktion"
	}

	fmt.Fprintf(os.Stderr, "%s[%s]\033[0m %s\n", color, label, command)
	if reason != "" {
		fmt.Fprintf(os.Stderr, "\033[2m%s\033[0m\n", reason)
	}
	fmt.Fprintf(os.Stderr, "[j]a / [n]ein / [e]ditieren: ")

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))

	switch input {
	case "j", "y", "ja", "yes":
		// Remember for this session
		confirmedMu.Lock()
		confirmedCommands[command] = true
		confirmedMu.Unlock()
		return ConfirmYes
	case "e", "edit":
		return ConfirmEdit
	default:
		return ConfirmNo
	}
}

// ConfirmSimple asks a simple yes/no question without edit option.
func ConfirmSimple(message string) bool {
	fmt.Fprintf(os.Stderr, "%s [j/n] ", message)
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))
	return input == "j" || input == "y" || input == "ja" || input == "yes"
}

// ResetConfirmCache clears the session-confirmed commands
func ResetConfirmCache() {
	confirmedMu.Lock()
	confirmedCommands = make(map[string]bool)
	confirmedMu.Unlock()
}
