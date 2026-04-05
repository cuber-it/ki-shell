# kish — the KI shell

A bash-compatible shell with native AI integration. Not a chatbot with shell access — a real shell that understands you.

```
$ ls -la                              → runs immediately (shell)
$ git push origin main                → runs immediately (shell)
$ ki what's wrong with the build      → asks AI, shows answer
$ cat error.log | ki "summarize"      → pipes output to AI
$ ki check what's running on server   → AI runs ssh, docker ps, analyzes
```

## Features

**Shell** — full bash compatibility via kish-sh (fork of mvdan/sh):
- Pipes, redirects, loops, functions, aliases, arrays, brace expansion
- Real subshells via re-exec (process isolation)
- Job control (Ctrl+Z, fg, bg, jobs, disown)
- Tab completion with YAML specs (git, docker, ssh, variables)
- Readline with persistent timestamped history and bang expansion (!!, !n, !string)
- PS1-compatible prompt (`PS1` supported, `KISH_PS1` overrides, default: git branch + exit code)
- Bash-conformant startup (`.bashrc`, `.kishrc`, `/etc/profile`)
- Shebang support (`#!/usr/bin/env kish`)

**AI** — powered by OpenAI + Anthropic (via heinzel provider library):
- `ki` prefix for AI queries (explicit, configurable, no guessing)
- `?` shortcut for quick context queries
- `ki start` / `ki stop` — continuous dialog mode
- Agent mode: AI runs read-only commands autonomously, confirms writes
- Pre-thinking: decomposes complex tasks before generating commands
- Skills: predefined YAML scripts the AI prefers over improvising
- Multi-turn conversation history
- Shell context: cwd, git branch, project type, recent commands + output
- Persistent memory with tags and decay (`remember` / `recall` / `forget`)
- Prompt A/B testing (`ki:variant`)
- Cost tracking (`ki:costs`)
- MCP client support
- Streaming responses

**Security** — paranoid by default:
- AI only activates on `ki` — never intercepts shell commands
- 5 action levels: Blocked → Confirm → AutoRead → AutoWrite → AutoExec
- AI cannot modify its own config (only hardcoded block)
- Destructive commands (rm, kill, sudo) need red confirmation
- Interactive commands (vim, htop, visudo) blocked from agent auto-execution
- Secret scrubbing in logs (API keys, passwords, JWTs, SSH keys)
- Audit log for every AI action
- Rate limiting (20/min, 200/hour)
- Context filtering (no stdout/stderr sent to API by default)
- Toggleable logging (`log on` / `log off`)

**Web UI** — browser-based terminal for remote administration:
- `web start` from within a running shell (shared session, same permissions)
- Full terminal (xterm.js) with KI panel alongside
- REST API for status, KI queries, command execution, history, costs, memory
- Token authentication, self-signed TLS
- Themes: Dark, Sepia, White, Terminal

## Install

```bash
git clone https://github.com/cuber-it/ki-shell
cd ki-shell
make install
```

## Setup

```bash
export OPENAI_API_KEY=sk-...
vi ~/.kish/config.yaml
```

```yaml
ki:
  provider: "openai"       # or "anthropic"
  model: "gpt-4o-mini"
  prefix: "ki"              # configurable: "ai", "hey", "ask", ...
```

> **Note on the `ki` prefix:** "KI" is the German word for AI (*Künstliche Intelligenz*) — that's where kish gets its name. The prefix is freely configurable, but choose carefully: any word you pick will be intercepted and sent to the AI instead of being executed as a shell command.

## Usage

```bash
# Shell — everything works like bash
$ for f in *.log; do wc -l "$f"; done
$ git status
$ docker ps

# Ask the AI
$ ki what does this error mean
$ ki: how do I find files larger than 100MB
$ ? why did that fail

# Pipe to AI
$ cat error.log | ki "summarize the errors"
$ docker logs app | ki "what went wrong"

# Continuous dialog mode
$ ki start
ki> Hi, what's in this project?
ki> ls -la
ki> what do you notice about the file sizes?
ki> stop

# Agent mode — gathers info autonomously
$ ki check what's running on the server and if anything is unhealthy

# Memory
$ remember editor "I use vim #tools"
$ recall editor
$ forget editor

# Web UI (starts within the running shell)
$ web start                            # auto-token, port 12080
$ web start --port :8080 --token abc   # custom
$ web start --notoken                  # no auth (intranet only!)
$ web stop

# Skills
$ ki:skills                            # list available skills

# Logs & memory
$ showlogs                             # all logs (paged)
$ showlogs shell 10                    # shell log only
$ showmemory                           # vault contents
$ log on / log off                     # toggle logging

# Status & debug
$ ki:status                            # engine, memory, permissions
$ ki:costs                             # API cost tracking
$ ki:disk                              # ~/.kish/ disk usage
$ ki:prompt                            # current system prompt
$ ki:variant                           # prompt A/B testing
$ ki:audit 10                          # last 10 AI actions
$ history 20                           # timestamped, with TTY + PID
$ kish -v 1                            # show AI thinking
$ kish -v 2                            # full debug output
```

## Security model

```
Without ki:  Direct shell. No AI. No checks. Your responsibility.
With ki:     Everything goes through the permission system.

Action levels:
  AutoRead    ls, cat, grep, docker ps    → runs silently
  Confirm     rm, mv, git push            → asks you first [y/n/e]
  Blocked     vi ~/.kish/config.yaml      → AI can't modify itself

Hardcoded (cannot be disabled):
  AI cannot modify ~/.kish/config.yaml, permissions.yaml, or kishrc.
  This prevents the AI from escalating its own privileges.

Everything else is configurable in ~/.kish/permissions.yaml.
```

## Files

```
~/.kish/
├── config.yaml         AI provider, model, prefix
├── permissions.yaml    Action levels, blocked patterns, context settings
├── kishrc              Shell startup (aliases, functions)
├── prompts.yaml        Prompt A/B testing variants
├── skills/             Predefined scripts (YAML)
├── completions/        Tab completion specs (YAML)
├── history             Timestamped command history (kill-safe, TTY + PID)
├── shell.log           Activity log (secret-scrubbed, gzip rotation)
├── audit.log           AI action audit trail (append-only)
├── costs.db            API cost tracking (SQLite)
└── vault/              Persistent AI memory
    ├── fact/            Long-term knowledge
    ├── session/         Session summaries
    └── scratch/         Temporary (7 days, auto-cleanup)
```

## Flags

```
-c string    execute command (supports ki prefix)
-i           force interactive mode
-l           login shell
-v int       verbose: 0=quiet, 1=actions, 2=debug
--norc       skip ~/.kishrc / ~/.bashrc
--noprofile  skip /etc/profile / ~/.profile
--version    print version
--help       show help
--web addr   start web UI as standalone (e.g. --web :12080)
--token str  auth token for web UI
--insecure   disable TLS for web UI
```

## Architecture

- **kish-sh/**: Fork of [mvdan/sh](https://github.com/mvdan/sh) with SubshellHandler API and bash compat fixes
- **heinzel provider**: OpenAI + Anthropic with retry, streaming, cost tracking (SQLite)
- **Permission system**: 5 action levels, secret scrubbing, audit log, rate limiting
- **Memory**: YAML vault with facts, sessions, scratch, tags, decay
- **Web UI**: Embedded xterm.js + WebSocket + REST API, runs as goroutine in-process
- **Skills**: YAML-defined scripts the AI invokes instead of improvising

~6000 LOC Go · 18 tests · 95/95 bash compatibility · 12MB binary

## What kish is not

kish is not a coding agent. Tools like Claude Code, Gemini CLI, or Codex are AI-first agents that happen to run in a terminal — they read codebases, write files, plan multi-step refactors.

kish is the opposite: a real shell that happens to have AI. You work in it like bash. You run scripts, pipe commands, manage jobs. When you need AI, you say `ki` — and it's there, with full context of what you've been doing. When you don't, it's silent and costs nothing.

Think of it as: Claude Code is your AI pair programmer. kish is your terminal, with a brain.

## License

Apache 2.0

## Credits

- Shell engine based on [mvdan/sh](https://github.com/mvdan/sh) by Daniel Martí (MIT license)
- AI providers via [heinzel-ai-core-go](https://github.com/cuber-it/heinzel-ai-core-go)

## Author

Ulrich Cuber / cuber IT service — built with AI assistance (Claude)
