// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"context"
	"io"
	"os"
)

// KIEngine is the interface that any KI backend must implement.
// heinzel-core implements this, but a simple Ollama client can too.
type KIEngine interface {
	// Query sends a natural language query to the KI and returns a response.
	// The response is streamed to the writer as it arrives.
	// ShellContext provides the current shell state for context-aware answers.
	Query(ctx context.Context, input string, shellCtx ShellContext, out io.Writer) (*KIResponse, error)

	// Available returns true if the KI engine is configured and ready.
	Available() bool

	// Name returns the engine name (e.g. "heinzel", "ollama", "openai")
	Name() string
}

// ShellContext captures the current shell state for KI context.
// This is what the KI "sees" when answering questions.
type ShellContext struct {
	// Cwd is the current working directory
	Cwd string

	// LastCommands are the most recent shell commands (newest first)
	LastCommands []CommandRecord

	// EnvVars are selected environment variables (filtered, not all)
	EnvVars map[string]string

	// GitBranch is the current git branch (empty if not in a repo)
	GitBranch string

	// ProjectType detected from files (go.mod, package.json, Cargo.toml, etc.)
	ProjectType string
}

// CommandRecord stores a command and its result for context
type CommandRecord struct {
	Input    string
	ExitCode int
	Stdout   string // truncated to last N lines
	Stderr   string // truncated to last N lines
}

// KIResponse is what the KI engine returns after processing a query
type KIResponse struct {
	// Text is the full response text
	Text string

	// SuggestedCommand is a shell command the KI suggests to run (may be empty)
	SuggestedCommand string

	// Confidence indicates how sure the KI is (0.0 - 1.0, -1 if unknown)
	Confidence float64

	// TokensUsed for cost tracking
	TokensUsed int
}

// ShellContextCollector gathers the current shell context for KI queries
type ShellContextCollector struct {
	history    []CommandRecord
	maxHistory int
}

func newShellContextCollector() *ShellContextCollector {
	return &ShellContextCollector{
		maxHistory: 10,
	}
}

// Record adds a command and its result to the history
func (sc *ShellContextCollector) Record(input string, exitCode int, stdout, stderr string) {
	// Truncate output to last 50 lines
	stdout = truncateLines(stdout, 50)
	stderr = truncateLines(stderr, 20)

	record := CommandRecord{
		Input:    input,
		ExitCode: exitCode,
		Stdout:   stdout,
		Stderr:   stderr,
	}

	// Prepend (newest first)
	sc.history = append([]CommandRecord{record}, sc.history...)
	if len(sc.history) > sc.maxHistory {
		sc.history = sc.history[:sc.maxHistory]
	}
}

// Collect builds the current ShellContext
func (sc *ShellContextCollector) Collect() ShellContext {
	ctx := ShellContext{
		LastCommands: sc.history,
		EnvVars:      collectEnvVars(),
	}
	ctx.Cwd, _ = os.Getwd()
	ctx.GitBranch = detectGitBranch()
	ctx.ProjectType = detectProjectType()
	return ctx
}
