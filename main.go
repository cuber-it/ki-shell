// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
// kish — KI-first Shell with bash compatibility
// Built on kish-sh (fork of mvdan/sh) + OpenAI/Anthropic/Ollama
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ergochat/readline"
	"golang.org/x/term"

	"github.com/cuber-it/kish-sh/v3/expand"
	"github.com/cuber-it/kish-sh/v3/interp"
	"github.com/cuber-it/kish-sh/v3/syntax"
)

const version = "0.0.1"

var (
	flagCommand     = flag.String("c", "", "command to be executed")
	flagVersion     = flag.Bool("version", false, "print version")
	flagLogin       = flag.Bool("l", false, "act as login shell")
	flagInteractive = flag.Bool("i", false, "force interactive mode")
	flagSubshell    = flag.String("subshell", "", "run code as subshell (internal use)")
	flagVerbose     = flag.Int("v", 0, "verbose level: 0=quiet, 1=actions, 2=full debug")
	flagNoRC        = flag.Bool("norc", false, "do not read startup files")
	flagNoProfile   = flag.Bool("noprofile", false, "do not read login files")
	flagRestricted  = flag.Bool("r", false, "restricted shell (not implemented)")
	flagHelp        = flag.Bool("help", false, "show help")
)

func printHelp() {
	fmt.Printf(`kish %s — the KI shell

Usage:
  kish                     interactive shell
  kish script.sh [args]    run script
  kish -c 'command'        run command
  kish -c '@ki query'      run KI query non-interactively

Flags:
  -c string    command to execute
  -i           force interactive mode
  -l           act as login shell
  -v int       verbose: 0=quiet (default), 1=actions, 2=full debug
  --norc       do not read ~/.kishrc or ~/.bashrc
  --noprofile  do not read /etc/profile or ~/.profile
  --version    print version
  --help       show this help

KI Commands (interactive):
  @ki <query>        ask the KI (configurable prefix)
  ? [query]          quick context query (last command)
  ki:status          show KI engine status
  ki:log [n]         show last n shell log entries
  ki:search <query>  search shell log
  ki:audit [n]       show last n audit entries
  ki:clear           reset conversation history
  merke <key> <val>  store a fact in memory
  erinnere <query>   recall from memory
  vergiss <key>      forget a fact

Config:
  ~/.kish/config.yaml        KI provider, model, prefix
  ~/.kish/permissions.yaml   security settings
  ~/.kish/kishrc             shell startup (aliases, functions)
  ~/.kish/completions/*.yaml tab completion specs
  ~/.kish/vault/             persistent KI memory

More: https://github.com/cuber-it/kish
`, version)
}


// Global state
var (
	jobTable       = newJobTable()
	kiEngine       KIEngine = &StubKIEngine{}
	kiConfig       *KishConfig
	kiInitialized  bool
	shellContext            = newShellContextCollector()
	kiMemory                = newMemory()
	kiConversation          = newConversationHistory()
	kiPermissions           = DefaultPermissions()
	rateLimiter             = newRateLimiter(20, 200, 10)
	shellLog                *ShellLog
)

// ensureKIEngine initializes the KI engine on first use (lazy loading)
func ensureKIEngine() {
	if kiInitialized {
		return
	}
	kiInitialized = true
	if kiConfig != nil {
		kiEngine = initKIEngine(kiConfig)
	}
}

func main() {
	flag.Parse()

	if *flagHelp {
		printHelp()
		os.Exit(0)
	}
	if *flagVersion {
		fmt.Printf("kish %s\n", version)
		os.Exit(0)
	}

	// Internal: subshell re-exec mode
	if *flagSubshell != "" {
		os.Exit(runSubshell(*flagSubshell))
	}

	err := runAll()
	var exitStatus interp.ExitStatus
	if errors.As(err, &exitStatus) {
		os.Exit(int(exitStatus))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "kish:", err)
		os.Exit(1)
	}
}

// ---------- Core ----------

func runAll() error {
	// Set verbose level
	verboseLevel = *flagVerbose

	// Load config, permissions, audit, and initialize KI engine
	WriteDefaultConfig()
	WriteDefaultPromptVariants()
	cfg := LoadConfig()
	kiConfig = cfg
	// KI engine is lazy-loaded on first @ki call — saves startup time
	initKIPrefix(cfg)
	kiPermissions = LoadPermissions()
	initAudit()
	defer closeAudit()
	initJobControl()
	shellLog = newShellLog()
	defer shellLog.Close()
	initHistory()
	defer closeHistory()
	if len(cfg.MCP) > 0 {
		initMCP(cfg.MCP)
		defer mcpClient.StopAll()
	}

	// TeeWriters capture stdout/stderr for the shell log
	stdoutTee := newTeeWriter(os.Stdout, 64*1024)
	// FilterWriter suppresses known bashrc warnings during startup
	stderrFilter := newFilterWriter(os.Stderr, false)
	stderrTee := newTeeWriter(stderrFilter, 16*1024)

	runner, err := interp.New(
		interp.Interactive(true),
		interp.StdIO(os.Stdin, stdoutTee, stderrTee),
		interp.SubshellHandler(kishSubshellHandler),
		interp.ExecHandlers(kishBuiltinsMiddleware, kiExecMiddleware, jobControlMiddleware),
	)
	if err != nil {
		return err
	}

	// Determine modes
	interactive := *flagInteractive || (term.IsTerminal(int(os.Stdin.Fd())) && *flagCommand == "" && flag.NArg() == 0)
	login := *flagLogin || isLoginShell()

	// Load startup files — bash-conformant order
	if login && !*flagNoProfile {
		sourceIfExists(runner, "/etc/profile")
		if fileExists(expandHome("~/.bash_profile")) {
			sourceQuietly(runner, expandHome("~/.bash_profile"))
		} else if fileExists(expandHome("~/.bash_login")) {
			sourceQuietly(runner, expandHome("~/.bash_login"))
		} else {
			sourceIfExists(runner, expandHome("~/.profile"))
		}
		sourceIfExists(runner, expandHome("~/.kish/profile"))
	}

	if interactive && !*flagNoRC {
		// Suppress known mvdan/sh warnings during rc loading
		stderrFilter.SetSuppress(true)

		sourceIfExists(runner, "/etc/kish.kishrc")
		if !fileExists("/etc/kish.kishrc") {
			sourceQuietly(runner, "/etc/bash.bashrc")
		}
		if fileExists(expandHome("~/.kishrc")) {
			sourceIfExists(runner, expandHome("~/.kishrc"))
		} else {
			sourceQuietly(runner, expandHome("~/.bashrc"))
		}
		cwd, _ := os.Getwd()
		localRC := filepath.Join(cwd, ".kishrc")
		if fileExists(localRC) && localRC != expandHome("~/.kishrc") {
			sourceIfExists(runner, localRC)
		}

		// Stop suppressing after RC loading
		stderrFilter.SetSuppress(false)
	}

	// -c "command" mode — supports @ki prefix
	if *flagCommand != "" {
		cmd := *flagCommand
		if isKIRequest(cmd) {
			query := stripKIPrefix(cmd)
			handleKI(context.Background(), query)
			return nil
		}
		return runSource(runner, strings.NewReader(cmd), "")
	}

	// Script mode: kish script.sh [args...]
	// First arg = script path, rest = script arguments ($1, $2, ...)
	if flag.NArg() > 0 {
		scriptPath := flag.Arg(0)
		runner.Params = flag.Args()[1:] // $1, $2, ... for the script
		if err := runFile(runner, scriptPath); err != nil {
			return err
		}
		return nil
	}

	// Interactive mode
	if interactive {
		return runInteractive(runner, stdoutTee, stderrTee)
	}

	// Pipe mode: echo "ls" | kish
	return runSource(runner, os.Stdin, "")
}

func runSource(runner *interp.Runner, reader io.Reader, name string) error {
	prog, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(reader, name)
	if err != nil {
		return err
	}
	runner.Reset()
	return runner.Run(context.Background(), prog)
}

func runFile(runner *interp.Runner, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return runSource(runner, file, path)
}

// ---------- Interactive REPL ----------

func runInteractive(runner *interp.Runner, stdoutTee, stderrTee *TeeWriter) error {
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

// ---------- Subshell Re-exec ----------

func runSubshell(code string) int {
	runner, err := interp.New(
		interp.StdIO(os.Stdin, os.Stdout, os.Stderr),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "kish: subshell:", err)
		return 1
	}
	prog, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(code), "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "kish: subshell parse:", err)
		return 1
	}
	runner.Reset()
	err = runner.Run(context.Background(), prog)
	var exitStatus interp.ExitStatus
	if errors.As(err, &exitStatus) {
		return int(exitStatus)
	}
	if err != nil {
		return 1
	}
	return 0
}

func kishSubshellHandler(ctx context.Context, code string, hc interp.HandlerContext) (uint8, error) {
	self, err := os.Executable()
	if err != nil {
		return 1, err
	}
	cmd := exec.CommandContext(ctx, self, "--subshell", code)
	cmd.Dir = hc.Dir
	cmd.Env = execEnv(hc.Env)
	cmd.Stdin = hc.Stdin
	cmd.Stdout = hc.Stdout
	cmd.Stderr = hc.Stderr
	err = cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return uint8(exitErr.ExitCode()), nil
		}
		return 1, nil
	}
	return 0, nil
}

func execEnv(env expand.Environ) []string {
	var result []string
	env.Each(func(name string, vr expand.Variable) bool {
		if vr.Exported {
			result = append(result, name+"="+vr.String())
		}
		return true
	})
	return result
}

// ---------- KI Engine ----------

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

// ---------- Helpers ----------

func kishDir() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".kish")
	os.MkdirAll(dir, 0755)
	return dir
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func nodeToString(node syntax.Node) string {
	var buf strings.Builder
	syntax.NewPrinter().Print(&buf, node)
	return strings.TrimSpace(buf.String())
}

func isIncomplete(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "reached EOF") ||
		strings.Contains(msg, "incomplete")
}

func sourceQuietly(runner *interp.Runner, path string) {
	if !fileExists(path) {
		return
	}
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()
	prog, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(file, path)
	if err != nil {
		return
	}
	origStderr := os.Stderr
	devNull, err := os.Open(os.DevNull)
	if err == nil {
		os.Stderr = devNull
	}
	func() {
		defer func() {
			os.Stderr = origStderr
			if devNull != nil {
				devNull.Close()
			}
			if r := recover(); r != nil {
				fmt.Fprintf(origStderr, "kish: .bashrc partially loaded (some features unsupported)\n")
			}
		}()
		runner.Run(context.Background(), prog)
	}()
}

func sourceIfExists(runner *interp.Runner, path string) {
	if !fileExists(path) {
		return
	}
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()
	prog, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(file, path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kish: parse error in %s: %s\n", path, err)
		return
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(os.Stderr, "kish: skipped incompatible code in %s: %v\n", path, r)
			}
		}()
		runner.Run(context.Background(), prog)
	}()
}

func isLoginShell() bool {
	return len(os.Args) > 0 && strings.HasPrefix(os.Args[0], "-")
}
