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

type ConfirmLevel int

const (
	ConfirmNormal      ConfirmLevel = iota
	ConfirmDestructive
)

type ConfirmResult int

const (
	ConfirmYes  ConfirmResult = iota
	ConfirmNo
	ConfirmEdit
)

// confirmedCommands tracks commands already confirmed in this session
// to prevent asking the same question twice for repeated patterns.
var (
	confirmedMu       sync.Mutex
	confirmedCommands = make(map[string]bool)
)

func Confirm(command string, reason string, level ConfirmLevel) ConfirmResult {
	confirmedMu.Lock()
	if confirmedCommands[command] {
		confirmedMu.Unlock()
		fmt.Fprintf(os.Stderr, "\033[2m→ %s (bereits bestätigt)\033[0m\n", command)
		return ConfirmYes
	}
	confirmedMu.Unlock()

	var color, label string
	switch level {
	case ConfirmDestructive:
		color = "\033[1;31m"
		label = "ACHTUNG"
	default:
		color = "\033[1;33m"
		label = "Vorschlag"
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

func ConfirmSimple(message string) bool {
	fmt.Fprintf(os.Stderr, "%s [j/n] ", message)
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))
	return input == "j" || input == "y" || input == "ja" || input == "yes"
}
