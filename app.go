// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.

package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/cuber-it/ki-shell/kish-sh/v3/expand"
	"github.com/cuber-it/ki-shell/kish-sh/v3/interp"
	"github.com/cuber-it/ki-shell/kish-sh/v3/syntax"
)

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
	loggingEnabled          = true
)

func ensureKIEngine() {
	if kiInitialized {
		return
	}
	kiInitialized = true
	if kiConfig != nil {
		kiEngine = initKIEngine(kiConfig)
	}
}

func runAll() error {
	verboseLevel = *flagVerbose

	WriteDefaultConfig()
	WriteDefaultPromptVariants()
	cfg := LoadConfig()
	kiConfig = cfg
	initKIPrefix(cfg)
	kiPermissions = LoadPermissions()
	initAudit()
	defer closeAudit()
	initJobControl()
	shellLog = newShellLog()
	defer shellLog.Close()
	rotateAllLogs()
	initSkills()
	initHistory()
	defer closeHistory()
	if len(cfg.MCP) > 0 {
		initMCP(cfg.MCP)
		defer mcpClient.StopAll()
	}

	stdoutTee := newTeeWriter(os.Stdout, 64*1024)
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

	interactive := *flagInteractive || (term.IsTerminal(int(os.Stdin.Fd())) && *flagCommand == "" && flag.NArg() == 0)
	login := *flagLogin || isLoginShell()

	loadStartupFiles(runner, stderrFilter, interactive, login)

	if *flagCommand != "" {
		if isKIRequest(*flagCommand) {
			handleKI(context.Background(), stripKIPrefix(*flagCommand))
			return nil
		}
		return runSource(runner, strings.NewReader(*flagCommand), "")
	}

	if flag.NArg() > 0 {
		runner.Params = flag.Args()[1:]
		return runFile(runner, flag.Arg(0))
	}

	if interactive {
		return runInteractive(runner, stdoutTee, stderrTee)
	}

	return runSource(runner, os.Stdin, "")
}

func loadStartupFiles(runner *interp.Runner, stderrFilter *FilterWriter, interactive, login bool) {
	if login && !*flagNoProfile {
		sourceIfExists(runner, "/etc/profile")
		if fileExists(expandHome("~/.bash_profile")) {
			sourceIfExists(runner, expandHome("~/.bash_profile"))
		} else if fileExists(expandHome("~/.bash_login")) {
			sourceIfExists(runner, expandHome("~/.bash_login"))
		} else {
			sourceIfExists(runner, expandHome("~/.profile"))
		}
		sourceIfExists(runner, expandHome("~/.kish/profile"))
	}

	if interactive && !*flagNoRC {
		stderrFilter.SetSuppress(true)

		sourceIfExists(runner, "/etc/kish.kishrc")
		if !fileExists("/etc/kish.kishrc") {
			sourceIfExists(runner, "/etc/bash.bashrc")
		}
		if fileExists(expandHome("~/.kishrc")) {
			sourceIfExists(runner, expandHome("~/.kishrc"))
		} else {
			sourceIfExists(runner, expandHome("~/.bashrc"))
		}
		cwd, _ := os.Getwd()
		localRC := filepath.Join(cwd, ".kishrc")
		if fileExists(localRC) && localRC != expandHome("~/.kishrc") {
			sourceIfExists(runner, localRC)
		}

		stderrFilter.SetSuppress(false)
	}
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

func runSubshell(code string) int {
	runner, err := interp.New(
		interp.StdIO(os.Stdin, os.Stdout, os.Stderr),
	)
	if err != nil {
		return 1
	}
	prog, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(code), "")
	if err != nil {
		return 1
	}
	runner.Reset()
	if err = runner.Run(context.Background(), prog); err != nil {
		var exitStatus interp.ExitStatus
		if errors.As(err, &exitStatus) {
			return int(exitStatus)
		}
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
		strings.Contains(msg, "incomplete") ||
		strings.Contains(msg, "must be followed by") ||
		strings.Contains(msg, "must be closed") ||
		strings.Contains(msg, "must end with") ||
		strings.Contains(msg, "expected") ||
		strings.Contains(msg, "not terminated")
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
		return
	}
	func() {
		defer func() { recover() }()
		runner.Run(context.Background(), prog)
	}()
}

func isLoginShell() bool {
	return len(os.Args) > 0 && strings.HasPrefix(os.Args[0], "-")
}
