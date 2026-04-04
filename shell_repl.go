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

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTSTP)
	var kiCancelFunc context.CancelFunc
	go func() {
		for sig := range sigChan {
			if kiCancelFunc != nil {
				kiCancelFunc()
				kiCancelFunc = nil
				continue
			}
			if pid := foregroundPID.Load(); pid > 0 {
				syscall.Kill(-int(pid), sig.(syscall.Signal))
			}
		}
	}()
	defer signal.Stop(sigChan)

	rl, err := readline.NewFromConfig(&readline.Config{
		Prompt:          buildPrompt(),
		HistoryFile:     filepath.Join(kishDir(), "history"),
		HistoryLimit:    10000,
		AutoComplete:    newCompleter(),
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
				saveSessionOnExit()
				return nil
			}
			return err
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if isKIRequest(line) {
			query := stripKIPrefix(line)
			if query == "start" || query == "continuous" || query == "chat" {
				kiCtx, kiCancel := context.WithCancel(context.Background())
				kiCancelFunc = kiCancel
				ContinuousMode(kiCtx, runner, stdoutTee, stderrTee)
				kiCancelFunc = nil
				kiCancel()
			} else {
				kiCtx, kiCancel := context.WithCancel(context.Background())
				kiCancelFunc = kiCancel
				handleKI(kiCtx, query)
				kiCancelFunc = nil
				kiCancel()
			}
			rl.SetPrompt(buildPrompt())
			continue
		}

		if strings.HasPrefix(line, "!") && line != "!" {
			if expanded := expandBang(line); expanded != "" {
				fmt.Fprintf(os.Stderr, "\033[2m%s\033[0m\n", expanded)
				line = expanded
			}
		}

		if line == "exit" || line == "quit" || line == "logout" {
			saveSessionOnExit()
			return nil
		}
		if strings.HasPrefix(line, "exit ") {
			saveSessionOnExit()
			var code int
			fmt.Sscanf(line[5:], "%d", &code)
			return interp.ExitStatus(code)
		}

		if handleBuiltin(line) {
			rl.SetPrompt(buildPrompt())
			continue
		}

		if multiLine.Len() > 0 {
			multiLine.WriteString("\n")
		}
		multiLine.WriteString(line)

		prog, parseErr := parser.Parse(strings.NewReader(multiLine.String()), "")
		if parseErr != nil && isIncomplete(parseErr) {
			rl.SetPrompt(buildPS2())
			continue
		}
		multiLine.Reset()

		if parseErr != nil {
			fmt.Fprintf(os.Stderr, "kish: %s\n", parseErr)
			rl.SetPrompt(buildPrompt())
			continue
		}

		ctx := context.Background()
		for _, stmt := range prog.Stmts {
			input := nodeToString(stmt)
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

			out, serr := stdoutTee.String(), stderrTee.String()
			shellContext.Record(input, lastExitCode, out, serr)
			if shellLog != nil {
				shellLog.Record(input, lastExitCode, out, serr)
			}
			if kishHistory != nil {
				kishHistory.Add(input)
			}
		}

		if promptCmd := os.Getenv("PROMPT_COMMAND"); promptCmd != "" {
			if prog, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(promptCmd), ""); err == nil {
				runner.Run(context.Background(), prog)
			}
		}
		rl.SetPrompt(buildPrompt())
	}
}

func handleBuiltin(line string) bool {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "history":
		printHistory(fields)
	case "ki:clear":
		kiConversation.Clear()
		fmt.Fprintln(os.Stderr, "Conversation cleared.")
	case "ki:disk":
		fmt.Fprintln(os.Stdout, DiskUsage())
	case "ki:status", "ki:costs", "ki:prompt", "ki:variant", "ki:audit", "ki:log",
		"ki:search", "ki:mcp",
		"remember", "recall", "forget", "merke", "erinnere", "vergiss":
		dispatchBuiltin(fields)
	case "jobs":
		jobTable.PrintJobs()
	case "fg":
		resolveJobCmd(fields, jobTable.ContinueFg)
	case "bg":
		resolveJobCmd(fields, jobTable.ContinueBg)
	case "disown":
		if id := resolveJobID(fields); id > 0 {
			jobTable.Remove(id)
			fmt.Fprintf(os.Stderr, "Job %%%d disowned\n", id)
		}
	default:
		return false
	}
	return true
}

func resolveJobID(fields []string) int {
	id := 0
	if len(fields) > 1 {
		fmt.Sscanf(strings.TrimPrefix(fields[1], "%"), "%d", &id)
	}
	if id == 0 {
		if job := jobTable.Last(); job != nil {
			id = job.ID
		}
	}
	return id
}

func resolveJobCmd(fields []string, fn func(int) error) {
	id := resolveJobID(fields)
	if id > 0 {
		if err := fn(id); err != nil {
			fmt.Fprintln(os.Stderr, "kish:", err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "kish: %s: no current job\n", fields[0])
	}
}

// dispatchBuiltin handles ki: commands that the shell parser would reject (colon syntax).
func dispatchBuiltin(fields []string) {
	switch fields[0] {
	case "remember", "merke":
		if len(fields) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: remember <key> <value...>")
			return
		}
		if err := kiMemory.Store(fields[1], strings.Join(fields[2:], " "), "fact", nil); err != nil {
			fmt.Fprintf(os.Stderr, "kish: %s\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "Remembered: %s\n", fields[1])
		}
	case "recall", "erinnere":
		if len(fields) < 2 {
			return
		}
		for _, e := range kiMemory.Search(strings.Join(fields[1:], " ")) {
			fmt.Fprintf(os.Stdout, "%s [%s]: %s\n", e.Key, e.Category, e.Value)
		}
	case "forget", "vergiss":
		if len(fields) < 2 {
			return
		}
		for _, cat := range []string{"fact", "session", "scratch"} {
			os.Remove(filepath.Join(kishDir(), "vault", cat, sanitizeFilename(fields[1])+".yaml"))
		}
		fmt.Fprintf(os.Stderr, "Forgotten: %s\n", fields[1])
	case "ki:status":
		ensureKIEngine()
		fmt.Fprintf(os.Stdout, "Engine: %s\nAvailable: %v\nMemories: %d\nConversation: %d turns\n",
			kiEngine.Name(), kiEngine.Available(), len(kiMemory.AllFacts()), len(kiConversation.Recent()))
	case "ki:costs":
		ensureKIEngine()
		if pe, ok := kiEngine.(*ProviderEngine); ok {
			today := pe.TodayStats()
			reqs, tokIn, tokOut, cost := pe.TotalStats()
			fmt.Fprintf(os.Stdout, "Today: %d req, %d/%d tok, $%.4f\nTotal: %d req, %d/%d tok, $%.4f\n",
				today.Requests, today.InputTokens, today.OutputTokens, today.Cost,
				reqs, tokIn, tokOut, cost)
		}
	case "ki:prompt":
		ensureKIEngine()
		fmt.Fprintln(os.Stdout, buildSystemPrompt(shellContext.Collect(), kiMemory, ""))
	case "ki:variant":
		if len(fields) > 1 {
			SwitchVariant(fields[1])
		} else {
			fmt.Fprintln(os.Stdout, ListVariants())
		}
	case "ki:audit":
		n := 20
		if len(fields) > 1 {
			fmt.Sscanf(fields[1], "%d", &n)
		}
		if audit != nil {
			audit.PrintRecent(n)
		}
	case "ki:log":
		n := 20
		if len(fields) > 1 {
			fmt.Sscanf(fields[1], "%d", &n)
		}
		if shellLog != nil {
			for _, e := range shellLog.Recent(n) {
				fmt.Fprintln(os.Stdout, e)
			}
		}
	case "ki:search":
		if len(fields) > 1 && shellLog != nil {
			for _, e := range shellLog.Search(strings.Join(fields[1:], " "), 10) {
				fmt.Fprintln(os.Stdout, e)
			}
		}
	case "ki:mcp":
		if mcpClient != nil {
			for _, t := range mcpClient.ListTools() {
				fmt.Fprintln(os.Stdout, t)
			}
		}
	}
}

func handleKI(ctx context.Context, input string) {
	ensureKIEngine()

	input = strings.TrimPrefix(input, "? ")
	input = strings.TrimPrefix(input, "?")
	input = strings.TrimSpace(input)

	if input == "" && len(shellContext.history) > 0 {
		last := shellContext.history[0]
		input = fmt.Sprintf("Explain what happened: command '%s' with exit code %d", last.Input, last.ExitCode)
		if last.Stderr != "" {
			input += "\nStderr: " + last.Stderr
		}
	}
	if input == "" {
		fmt.Fprintln(os.Stderr, "kish: nothing to ask")
		return
	}

	filteredCtx := kiPermissions.FilterContext(shellContext.Collect())

	if kiPermissions.RequireConfirmation {
		msg := fmt.Sprintf("KI-Query senden? (cwd=%s, %d cmds, %d env vars)",
			filteredCtx.Cwd, len(filteredCtx.LastCommands), len(filteredCtx.EnvVars))
		if !ConfirmSimple(msg) {
			return
		}
	}

	if allowed, warning := rateLimiter.Allow(); !allowed {
		fmt.Fprintf(os.Stderr, "\033[1;31m%s\033[0m\n", warning)
		return
	} else if warning != "" {
		fmt.Fprintf(os.Stderr, "\033[1;33m%s\033[0m\n", warning)
	}

	if audit != nil {
		audit.LogQuery(input, kiEngine.Name())
	}

	if kiPermissions.AgentMode {
		if _, err := RunAgentLoop(ctx, kiEngine, input, filteredCtx, kiMemory, rateLimiter.MaxAgentSteps()); err != nil {
			fmt.Fprintf(os.Stderr, "kish: ki error: %s\n", err)
		}
		return
	}

	resp, err := kiEngine.Query(ctx, input, filteredCtx, os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kish: ki error: %s\n", err)
		return
	}
	if resp != nil && resp.SuggestedCommand != "" {
		executeWithPermissions(resp.SuggestedCommand)
	}
}

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
		if strings.Contains(strings.ToLower(reason), "destruktiv") {
			level = ConfirmDestructive
		}
		switch Confirm(command, reason, level) {
		case ConfirmNo:
			if audit != nil {
				audit.Log("CONFIRM", command, "denied", reason)
			}
			return
		case ConfirmEdit:
			fmt.Fprintf(os.Stderr, "Edit command: ")
			edited, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			if cmd := strings.TrimSpace(edited); cmd != "" {
				executeWithPermissions(cmd)
			}
			return
		case ConfirmYes:
			if audit != nil {
				audit.Log("CONFIRM", command, "allowed", reason)
			}
		}
	}

	fmt.Fprintf(os.Stderr, "\033[2m===>\n%s\033[0m\n", command)
	runner, err := interp.New(interp.StdIO(os.Stdin, os.Stdout, os.Stderr))
	if err != nil {
		return
	}
	if prog, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(command), ""); err == nil {
		runner.Reset()
		runner.Run(context.Background(), prog)
	}
}

func expandBang(line string) string {
	data, err := os.ReadFile(filepath.Join(kishDir(), "history"))
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		return ""
	}

	bang := line[1:]

	if bang == "!" {
		return lines[len(lines)-1]
	}
	var n int
	if cnt, _ := fmt.Sscanf(bang, "%d", &n); cnt == 1 && n > 0 && n <= len(lines) {
		return lines[n-1]
	}
	if strings.HasPrefix(bang, "-") {
		var neg int
		if cnt, _ := fmt.Sscanf(bang[1:], "%d", &neg); cnt == 1 && neg > 0 && neg <= len(lines) {
			return lines[len(lines)-neg]
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], bang) {
			return lines[i]
		}
	}
	return ""
}

func saveSessionOnExit() {
	if len(shellContext.history) == 0 {
		return
	}
	cwd, _ := os.Getwd()
	summary := fmt.Sprintf("%d commands", len(shellContext.history))
	if last := shellContext.history[0]; last.Input != "" {
		summary += ", last: " + last.Input
	}
	kiMemory.SaveSessionSummary(summary, cwd, len(shellContext.history))
}
