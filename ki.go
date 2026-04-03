// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"context"
	"io"
	"os"
)

// KIEngine is the interface that any KI backend must implement.
type KIEngine interface {
	Query(ctx context.Context, input string, shellCtx ShellContext, out io.Writer) (*KIResponse, error)
	Available() bool
	Name() string
}

// ShellContext captures the current shell state for KI context.
type ShellContext struct {
	Cwd          string
	LastCommands []CommandRecord
	EnvVars      map[string]string
	GitBranch    string
	ProjectType  string
}

type CommandRecord struct {
	Input    string
	ExitCode int
	Stdout   string
	Stderr   string
}

type KIResponse struct {
	Text             string
	SuggestedCommand string
	Confidence       float64 // 0.0 - 1.0, -1 if unknown
	TokensUsed       int
}

type ShellContextCollector struct {
	history    []CommandRecord
	maxHistory int
}

func newShellContextCollector() *ShellContextCollector {
	return &ShellContextCollector{
		maxHistory: 10,
	}
}

func (sc *ShellContextCollector) Record(input string, exitCode int, stdout, stderr string) {
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
