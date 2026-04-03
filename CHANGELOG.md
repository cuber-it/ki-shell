# Changelog

## v0.1.0 — 2026-04-03

Initial release. Built in one session.

### Shell
- Full bash compatibility (arrays, brace expansion, here docs, process substitution, regex, trap, set -e, pipefail, extglob, globstar, mapfile, select, nameref)
- Real subshells via re-exec (process isolation, not goroutines)
- Job control (Ctrl+Z, fg, bg, jobs, disown)
- Tab completion with YAML specs (~/.kish/completions/) for git, docker, ssh
- Variable completion ($HOME, $PATH, etc.)
- Readline with persistent history (10k entries)
- PS1-compatible prompt (git branch, exit code indicator, \u \h \w \t \d \j \?)
- Bash-conformant startup: /etc/profile, ~/.bash_profile, ~/.bashrc, ~/.kishrc
- Project-local .kishrc
- Script arguments ($1, $2, $@, getopts)
- Shebang support (#!/usr/bin/env kish)

### KI
- @ki prefix for explicit KI activation (no auto-classifier)
- ? shortcut for quick context queries
- OpenAI provider with SSE streaming
- Agent mode: KI executes read-only commands autonomously, confirms writes
- 5 action levels: Blocked → Confirm → AutoRead → AutoWrite → AutoExec
- Shell context: cwd, git branch, project type, recent commands + output
- Multi-turn conversation history
- Persistent memory (merke/erinnere/vergiss) in ~/.kish/vault/
- Project awareness (reads README.md/CLAUDE.md)
- Configurable prefix (ki.prefix in config.yaml)
- Pipe to KI: `cat log | ki "summarize"`

### Security
- KI only active on @ki — never intercepts shell input
- Only hardcoded block: KI cannot modify its own config
- Destructive commands (sudo, rm -rf, kill) need red confirmation
- Self-modification protection (permissions.yaml, config.yaml, kishrc)
- Secret scrubbing in logs (API keys, JWTs, passwords, SSH keys)
- Audit log (append-only, rotation)
- Rate limiting (20/min, 200/hour)
- Context filtering (no stdout/stderr to API by default)
- Injection detection (LD_PRELOAD, command substitution, pipe exfiltration)
- SSH command recursive classification
- God mode only via KISH_GOD_MODE=yes env var

### Infrastructure
- Shell log (~/.kish/shell.log) with secret scrubbing and rotation
- YAML-based completion specs
- Verbose levels (-v 0/1/2)
- Makefile (build, test, install, release)
- 17 tests (permissions, actions, classifier, secret scrubbing)
- ~4800 LOC Go, 7.6MB stripped binary

### Based on
- kish-sh: fork of mvdan/sh v3.13.0 with SubshellHandler API and shopt fixes
- OpenAI Chat Completions API (gpt-4o-mini default)
