# kish — the KI shell

A bash-compatible shell with native AI integration. Not a chatbot with shell access — a real shell that understands you.

```
$ ls -la                              → runs immediately (shell)
$ git push origin main                → runs immediately (shell)
$ @ki what's wrong with the build     → asks AI, shows answer
$ cat error.log | ki "summarize"      → pipes output to AI
$ @ki check what's running on server  → AI runs ssh, docker ps, analyzes
```

## Features

**Shell** — full bash compatibility via kish-sh (fork of mvdan/sh):
- Pipes, redirects, loops, functions, aliases, arrays
- Real subshells via re-exec (process isolation)
- Job control (Ctrl+Z, fg, bg, jobs, disown)
- Tab completion with YAML specs (git, docker, ssh, variables)
- Readline with persistent timestamped history
- Bang expansion (!!, !n, !string)
- PS1-compatible prompt with git branch + exit code
- `.kishrc` + `.bashrc` fallback
- Shebang support (`#!/usr/bin/env kish`)

**AI** — powered by OpenAI + Anthropic (via heinzel provider library):
- `@ki` / `ki` prefix for AI queries (explicit, no guessing)
- `?` shortcut for quick context queries
- `ki start` / `ki stop` — continuous dialog mode
- Agent mode: AI runs read-only commands autonomously, confirms writes
- Multi-turn conversation history
- Shell context: cwd, git branch, project type, recent commands + output
- Persistent memory with tags and decay
- Prompt A/B testing (`ki:variant`)
- Cost tracking (`ki:costs`)
- MCP client support
- Streaming responses

**Security** — paranoid by default:
- AI only activates on `@ki` — never intercepts shell commands
- 5 action levels: Blocked → Confirm → AutoRead → AutoWrite → AutoExec
- AI cannot modify its own config (only hardcoded block)
- Destructive commands (rm, kill, sudo) need red confirmation
- Secret scrubbing in logs (API keys, passwords, JWTs, SSH keys)
- Audit log for every AI action
- Rate limiting (20/min, 200/hour)
- Context filtering (no stdout/stderr sent to API by default)

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
# ~/.kish/config.yaml
ki:
  provider: "openai"       # or "anthropic"
  model: "gpt-4o-mini"
  prefix: "@ki"
```

## Usage

```bash
# Shell — everything works like bash
$ for f in *.log; do wc -l "$f"; done
$ git status
$ docker ps

# Ask the AI
$ @ki what does this error mean
$ ki how do I find files larger than 100MB
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
$ @ki check what's running on the server and if anything is unhealthy

# Memory
$ remember editor "I use vim"           # or: merke editor "I use vim"
$ recall editor                         # or: erinnere editor
$ forget editor                         # or: vergiss editor

# Continuous dialog mode
$ ki start                              # start dialog (ki> prompt)
$ ki stop                               # back to normal shell

# Status & debug
$ ki:status                             # engine, memory, permissions
$ ki:costs                              # API cost tracking
$ ki:prompt                             # show current system prompt
$ ki:variant                            # list/switch prompt variants
$ ki:audit 10                           # last 10 AI actions
$ ki:log 5                              # last 5 shell log entries
$ ki:search "docker"                    # search shell log
$ history 20                            # last 20 commands with timestamps
$ kish -v 1                             # show AI thinking
$ kish -v 2                             # full debug output
```

## Security model

```
Without @ki:  Direct shell. No AI. No checks. Your responsibility.
With @ki:     Everything goes through the permission system.

Action levels:
  AutoRead    ls, cat, grep, docker ps    → runs silently
  Confirm     rm, mv, git push            → asks you first [j/n/e]
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
├── completions/        YAML tab completion specs (git, docker, ssh)
├── history             Command history (readline)
├── history_ts          Timestamped history
├── shell.log           Activity log (secret-scrubbed, rotation)
├── audit.log           AI action audit trail (append-only)
├── costs.db            API cost tracking (SQLite)
└── vault/              Persistent AI memory
    ├── fact/            Long-term knowledge
    ├── session/         Session summaries
    └── scratch/         Temporary (7 days, auto-cleanup)
```

## Flags

```
-c string    execute command (supports @ki prefix)
-i           force interactive mode
-l           login shell
-v int       verbose: 0=quiet, 1=actions, 2=debug
--norc       skip ~/.kishrc / ~/.bashrc
--noprofile  skip /etc/profile / ~/.profile
--version    print version
--help       show help
```

## Architecture

- **kish-sh/**: Fork of [mvdan/sh](https://github.com/mvdan/sh) with SubshellHandler API and bash compat fixes
- **heinzel provider**: OpenAI + Anthropic with retry, streaming, cost tracking
- **Permission system**: 5 action levels, secret scrubbing, audit log, rate limiting
- **Memory**: YAML vault with facts, sessions, scratch, tags, decay

~5600 LOC Go · 18 tests · 95/95 bash compatibility · 12MB binary

## License

Apache 2.0

## Author

Ulrich Cuber / cuber IT service — built with AI assistance (Claude)
