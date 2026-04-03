// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.

package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ergochat/readline"

	"github.com/cuber-it/ki-shell/kish-sh/v3/interp"
	"github.com/cuber-it/ki-shell/kish-sh/v3/syntax"
)

func runInteractive(runner *interp.Runner, stdoutTee, stderrTee *TeeWriter) error {
	isInteractiveMode = true
	defer func() { isInteractiveMode = false }()
	// Setup signal handling — shared channel, context-aware
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTSTP)
	var kiCancelFunc context.CancelFunc // set during KI operations
	go func() {
		for sig := range sigChan {
			// If KI operation is running, cancel it
			if kiCancelFunc != nil {
				kiCancelFunc()
				kiCancelFunc = nil
				continue
			}
			// Otherwise forward to foreground process
			pid := foregroundPID.Load()
			if pid > 0 {
				syscall.Kill(-int(pid), sig.(syscall.Signal))
			}
		}
	}()
	defer signal.Stop(sigChan)

	historyFile := filepath.Join(kishDir(), "history")
	completer := newCompleter()

	rl, err := readline.NewFromConfig(&readline.Config{
		Prompt:          buildPrompt(),
		HistoryFile:     historyFile,
		HistoryLimit:    10000,
		AutoComplete:    completer,
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})
	if err != nil {
		return err
	}
	defer rl.Close()

	parser := syntax.NewParser(syntax.Variant(syntax.LangBash))
	var multiLine strings.Builder

	for {
		// Check for finished background jobs
		jobTable.UpdateStatus()
		jobTable.CleanDone()

		line, err := rl.Readline()
		if err != nil {
			if err == readline.ErrInterrupt {
				multiLine.Reset()
				rl.SetPrompt(buildPrompt())
				continue
			}
			if err == io.EOF {
				fmt.Fprintln(os.Stdout, "exit")
				saveSessionOnExit()
				return nil
			}
			return err
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Check: is this a KI request? (@ki/ki prefix or ? shortcut)
		if isKIRequest(line) {
			query := stripKIPrefix(line)

			// Continuous mode
			if query == "start" || query == "continuous" || query == "chat" {
				kiCtx, kiCancel := context.WithCancel(context.Background())
				kiCancelFunc = kiCancel
				ContinuousMode(kiCtx, runner, stdoutTee, stderrTee)
				kiCancelFunc = nil
				kiCancel()
				rl.SetPrompt(buildPrompt())
				continue
			}

			kiCtx, kiCancel := context.WithCancel(context.Background())
			kiCancelFunc = kiCancel
			handleKI(kiCtx, query)
			kiCancelFunc = nil
			kiCancel()
			rl.SetPrompt(buildPrompt())
			continue
		}

		// Bang expansion: !!, !n, !string
		if strings.HasPrefix(line, "!") && line != "!" {
			expanded := expandBang(line, rl)
			if expanded != "" {
				fmt.Fprintf(os.Stderr, "\033[2m%s\033[0m\n", expanded)
				line = expanded
			}
		}

		// Handle exit/quit directly
		trimmed := strings.TrimSpace(line)
		if trimmed == "exit" || trimmed == "quit" || trimmed == "logout" {
			saveSessionOnExit()
			fmt.Fprintln(os.Stdout, "exit")
			return nil
		}
		// Handle exit with code: "exit 1"
		if strings.HasPrefix(trimmed, "exit ") {
			saveSessionOnExit()
			var code int
			fmt.Sscanf(trimmed[5:], "%d", &code)
			return interp.ExitStatus(code)
		}

		// Handle kish builtins that bypass the parser
		if handled := handleBuiltin(line); handled {
			rl.SetPrompt(buildPrompt())
			continue
		}

		// Multi-line support: accumulate if incomplete
		if multiLine.Len() > 0 {
			multiLine.WriteString("\n")
		}
		multiLine.WriteString(line)
		fullInput := multiLine.String()

		// Try to parse — if incomplete, continue reading
		prog, parseErr := parser.Parse(strings.NewReader(fullInput), "")
		if parseErr != nil && isIncomplete(parseErr) {
			rl.SetPrompt(buildPS2())
			continue
		}

		// Reset multi-line state
		multiLine.Reset()

		if parseErr != nil {
			fmt.Fprintf(os.Stderr, "kish: %s\n", parseErr)
			rl.SetPrompt(buildPrompt())
			continue
		}

		// Execute shell statements
		ctx := context.Background()
		for _, stmt := range prog.Stmts {
			input := nodeToString(stmt)

			// Reset capture buffers before each command
			stdoutTee.Reset()
			stderrTee.Reset()

			err := runner.Run(ctx, stmt)
			lastExitCode = 0
			if err != nil {
				if es, ok := err.(interp.ExitStatus); ok {
					lastExitCode = int(es)
				} else {
					lastExitCode = 1
				}
				if runner.Exited() {
					return nil
				}
			}

			// Capture output for context and logging
			capturedOut := stdoutTee.String()
			capturedErr := stderrTee.String()
			shellContext.Record(input, lastExitCode, capturedOut, capturedErr)
			if shellLog != nil {
				shellLog.Record(input, lastExitCode, capturedOut, capturedErr)
			}
			if kishHistory != nil {
				kishHistory.Add(input)
			}
		}

		// PROMPT_COMMAND equivalent
		if promptCmd := os.Getenv("PROMPT_COMMAND"); promptCmd != "" {
			prog, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(promptCmd), "")
			if err == nil {
				runner.Run(context.Background(), prog)
			}
		}
		rl.SetPrompt(buildPrompt())
	}
}

// handleBuiltin handles kish-specific builtins that need to bypass the shell parser.
// Returns true if the input was handled.
func handleBuiltin(line string) bool {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false
	}

	switch fields[0] {
	case "history":
		printHistory(fields)
		return true
	case "ki:clear":
		kiConversation.Clear()
		fmt.Fprintln(os.Stderr, "Konversation zurückgesetzt.")
		return true
	case "jobs":
		jobTable.PrintJobs()
		return true
	case "fg":
		jobID := 0
		if len(fields) > 1 {
			fmt.Sscanf(strings.TrimPrefix(fields[1], "%"), "%d", &jobID)
		}
		if jobID == 0 {
			if job := jobTable.Last(); job != nil {
				jobID = job.ID
			}
		}
		if jobID > 0 {
			if err := jobTable.ContinueFg(jobID); err != nil {
				fmt.Fprintln(os.Stderr, "kish:", err)
			}
		} else {
			fmt.Fprintln(os.Stderr, "kish: fg: no current job")
		}
		return true
	case "disown":
		jobID := 0
		if len(fields) > 1 {
			fmt.Sscanf(strings.TrimPrefix(fields[1], "%"), "%d", &jobID)
		}
		if jobID == 0 {
			if job := jobTable.Last(); job != nil {
				jobID = job.ID
			}
		}
		if jobID > 0 {
			jobTable.Remove(jobID)
			fmt.Fprintf(os.Stderr, "Job %%%d disowned\n", jobID)
		} else {
			fmt.Fprintln(os.Stderr, "kish: disown: no current job")
		}
		return true
	case "bg":
		jobID := 0
		if len(fields) > 1 {
			fmt.Sscanf(strings.TrimPrefix(fields[1], "%"), "%d", &jobID)
		}
		if jobID == 0 {
			if job := jobTable.Last(); job != nil {
				jobID = job.ID
			}
		}
		if jobID > 0 {
			if err := jobTable.ContinueBg(jobID); err != nil {
				fmt.Fprintln(os.Stderr, "kish:", err)
			}
		} else {
			fmt.Fprintln(os.Stderr, "kish: bg: no current job")
		}
		return true
	}
	return false
}

// ---------- Session ----------

// saveSessionOnExit stores a session summary in memory
func saveSessionOnExit() {
	if len(shellContext.history) == 0 {
		return
	}
	cwd, _ := os.Getwd()
	summary := fmt.Sprintf("%d Befehle ausgeführt", len(shellContext.history))
	if len(shellContext.history) > 0 {
		last := shellContext.history[0]
		summary += fmt.Sprintf(", letzter: %s", last.Input)
	}
	kiMemory.SaveSessionSummary(summary, cwd, len(shellContext.history))
}

// REMOVED: everything below was moved to app.go
// runSubshell, kishSubshellHandler, execEnv, kishDir, expandHome, etc.
var _ = "" // end of file marker
func handleKI(ctx context.Context, input string) {
	ensureKIEngine()

	// Strip prefix
	input = strings.TrimPrefix(input, "? ")
	input = strings.TrimPrefix(input, "?")
	input = strings.TrimSpace(input)

	// "?" alone = analyze last command
	if input == "" && len(shellContext.history) > 0 {
		last := shellContext.history[0]
		input = fmt.Sprintf("Erkläre was passiert ist: Befehl '%s' mit Exit-Code %d", last.Input, last.ExitCode)
		if last.Stderr != "" {
			input += "\nStderr: " + last.Stderr
		}
	}

	if input == "" {
		fmt.Fprintln(os.Stderr, "kish: nothing to ask")
		return
	}

	// Collect and FILTER context through permissions
	rawCtx := shellContext.Collect()
	filteredCtx := kiPermissions.FilterContext(rawCtx)

	// If RequireConfirmation: show what would be sent
	if kiPermissions.RequireConfirmation {
		msg := fmt.Sprintf("KI-Query senden? (cwd=%s, %d cmds, %d env vars)",
			filteredCtx.Cwd, len(filteredCtx.LastCommands), len(filteredCtx.EnvVars))
		if !ConfirmSimple(msg) {
			fmt.Fprintln(os.Stderr, "Abgebrochen.")
			return
		}
	}

	// Rate limiting
	allowed, warning := rateLimiter.Allow()
	if warning != "" {
		fmt.Fprintf(os.Stderr, "\033[1;33m%s\033[0m\n", warning)
	}
	if !allowed {
		fmt.Fprintf(os.Stderr, "\033[1;31m%s\033[0m\n", warning)
		return
	}

	// Audit: log what we're sending
	if audit != nil {
		audit.LogQuery(input, kiEngine.Name())
	}

	// Agent mode: KI can execute actions to gather info
	if kiPermissions.AgentMode {
		_, err := RunAgentLoop(ctx, kiEngine, input, filteredCtx, kiMemory, rateLimiter.MaxAgentSteps())
		if err != nil {
			fmt.Fprintf(os.Stderr, "kish: ki error: %s\n", err)
		}
		return
	}

	// Simple mode: one query, one answer
	resp, err := kiEngine.Query(ctx, input, filteredCtx, os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kish: ki error: %s\n", err)
		return
	}

	// If the KI suggests a command, check permissions and ask for confirmation
	if resp != nil && resp.SuggestedCommand != "" {
		executeWithPermissions(resp.SuggestedCommand)
	}
}

// executeWithPermissions checks a KI-suggested command against the permission system
// and handles confirmation before execution.
func executeWithPermissions(command string) {
	allowed, needsConfirm, reason := kiPermissions.CheckCommand(command)

	if !allowed {
		fmt.Fprintf(os.Stderr, "\033[1;31m[BLOCKIERT]\033[0m %s\n", reason)
		if audit != nil {
			audit.Log("BLOCKED", command, "blocked", reason)
		}
		return
	}

	if needsConfirm {
		level := ConfirmNormal
		if reason != "" && (strings.Contains(reason, "Destruktiv") || strings.Contains(reason, "destruktiv")) {
			level = ConfirmDestructive
		}

		result := Confirm(command, reason, level)
		switch result {
		case ConfirmNo:
			fmt.Fprintln(os.Stderr, "Nicht ausgeführt.")
			if audit != nil {
				audit.Log("CONFIRM", command, "denied", reason)
			}
			return
		case ConfirmEdit:
			fmt.Fprintf(os.Stderr, "Befehl editieren: ")
			reader := bufio.NewReader(os.Stdin)
			edited, _ := reader.ReadString('\n')
			command = strings.TrimSpace(edited)
			if command == "" {
				fmt.Fprintln(os.Stderr, "Abgebrochen.")
				return
			}
			// Re-check the edited command
			executeWithPermissions(command)
			return
		case ConfirmYes:
			if audit != nil {
				audit.Log("CONFIRM", command, "allowed", reason)
			}
		}
	}

	// Execute the command
	fmt.Fprintf(os.Stderr, "\033[2m→ %s\033[0m\n", command)
	runner, err := interp.New(
		interp.StdIO(os.Stdin, os.Stdout, os.Stderr),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kish: %s\n", err)
		return
	}
	prog, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(command), "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "kish: %s\n", err)
		return
	}
	runner.Reset()
	runner.Run(context.Background(), prog)
}

// expandBang handles bash-style bang expansion: !!, !n, !string
func expandBang(line string, rl *readline.Instance) string {
	histFile := filepath.Join(kishDir(), "history")
	data, err := os.ReadFile(histFile)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		return ""
	}

	bang := line[1:] // strip leading !

	// !! = last command
	if bang == "!" {
		return lines[len(lines)-1]
	}

	// !n = command number n
	var n int
	if cnt, _ := fmt.Sscanf(bang, "%d", &n); cnt == 1 && n > 0 && n <= len(lines) {
		return lines[n-1]
	}

	// !-n = n-th last command
	if strings.HasPrefix(bang, "-") {
		var neg int
		if cnt, _ := fmt.Sscanf(bang[1:], "%d", &neg); cnt == 1 && neg > 0 && neg <= len(lines) {
			return lines[len(lines)-neg]
		}
	}

	// !string = last command starting with string
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], bang) {
			return lines[i]
		}
	}

	return ""
}

// saveSessionOnExit stores a session summary in memory
