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

**Shell** — full bash compatibility via [kish-sh](https://github.com/cuber-it/kish-sh) (fork of mvdan/sh):
- Pipes, redirects, loops, functions, aliases, arrays
- Real subshells via re-exec (process isolation)
- Job control (Ctrl+Z, fg, bg, jobs, disown)
- Tab completion (commands, files, @ki builtins)
- Readline with persistent history
- PS1-compatible prompt with git branch + exit code
- `.kishrc` + `.bashrc` fallback
- Shebang support (`#!/usr/bin/env kish`)

**AI** — powered by OpenAI (Anthropic/Ollama planned):
- `@ki` prefix for AI queries (explicit, no guessing)
- `?` shortcut for quick context queries
- Agent mode: AI can run read-only commands to gather info
- Multi-turn conversation history
- Shell context: cwd, git branch, project type, recent commands
- Persistent memory (`merke`/`erinnere`/`vergiss`)
- Streaming responses

**Security** — paranoid by default:
- AI only activates on `@ki` — never intercepts shell commands
- 5 action levels: Blocked → Confirm → AutoRead → AutoWrite → AutoExec
- AI cannot modify its own config (only hardcoded block)
- Destructive commands (rm, kill, sudo) need red confirmation
- Secret scrubbing in logs (API keys, passwords, JWTs)
- Audit log for every AI action
- Rate limiting (20/min, 200/hour)
- Context filtering (no stdout/stderr sent to API by default)

## Install

```bash
# Build from source
git clone https://github.com/cuber-it/kish
cd kish
make install

# Or via go install (once published)
go install github.com/cuber-it/kish@latest
```

## Setup

```bash
# Set OpenAI API key
export OPENAI_API_KEY=sk-...

# Edit config
vi ~/.kish/config.yaml
```

```yaml
# ~/.kish/config.yaml
ki:
  provider: "openai"
  model: "gpt-4o-mini"
  prefix: "@ki"
```

## Usage

```bash
# Normal shell — everything works like bash
$ for f in *.log; do wc -l "$f"; done
$ git status
$ docker ps

# Ask the AI
$ @ki what does this error mean
$ @ki how do I find files larger than 100MB
$ ? why did that fail                    # ? = quick context query

# Pipe to AI
$ cat error.log | ki "summarize the errors"
$ docker logs app | ki "what went wrong"

# AI agent mode — gathers info autonomously
$ @ki check what's running on the server and if anything is unhealthy

# Memory
$ merke editor "I use vim"
$ erinnere editor
$ vergiss editor

# Status & debug
$ ki:status
$ ki:audit 10
$ ki:log 5
$ ki:search "docker"
$ kish -v 1    # show AI thinking
$ kish -v 2    # full debug output
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
├── config.yaml       AI provider, model, prefix
├── permissions.yaml  Action levels, blocked patterns, context settings
├── kishrc            Shell startup (aliases, functions)
├── history           Command history
├── shell.log         Persistent activity log (secret-scrubbed)
├── audit.log         AI action audit trail
└── vault/            Persistent AI memory
    ├── fact/          Long-term knowledge
    ├── session/       Session summaries
    └── scratch/       Temporary (7 days)
```

## Architecture

- **kish-sh**: Fork of [mvdan/sh](https://github.com/mvdan/sh) with SubshellHandler API and shopt fixes
- **OpenAI**: SSE streaming, conversation history, agent loop
- **Permission system**: 5 action levels, secret scrubbing, audit log, rate limiting
- **Memory**: YAML-based vault with facts, sessions, scratch entries

~4800 LOC Go. 17 tests. 7.6MB stripped binary.

## Bash flags

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

## License

Apache 2.0

## Author

Ulrich Cuber / cuber IT service — built with AI assistance (Claude)
