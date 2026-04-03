// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
//
// kish — the KI shell. Bash-compatible with native AI integration.
// https://github.com/cuber-it/ki-shell
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/cuber-it/ki-shell/kish-sh/v3/interp"
)

const version = "0.1.0"

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
  -v int       verbose: 0=quiet (default), 1=actions, 2=debug
  --norc       do not read ~/.kishrc or ~/.bashrc
  --noprofile  do not read /etc/profile or ~/.profile
  --version    print version
  --help       show this help

KI Commands (interactive):
  @ki <query>        ask the KI (configurable prefix)
  ? [query]          quick context query (last command)
  ki start           start continuous dialog mode
  ki:status          show KI engine status
  ki:costs           show API cost tracking
  ki:log [n]         show last n shell log entries
  ki:search <query>  search shell log
  ki:audit [n]       show last n audit entries
  ki:variant [name]  show/switch prompt variants
  ki:prompt          show current system prompt
  ki:mcp             show MCP server status
  ki:clear           reset conversation history
  merke <key> <val>  store a fact in memory
  erinnere <query>   recall from memory
  vergiss <key>      forget a fact
  history [n]        show command history with timestamps

More: https://github.com/cuber-it/ki-shell
`, version)
}
