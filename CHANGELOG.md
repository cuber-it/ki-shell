# Changelog

## v0.2.0 — 2026-04-05

### Web UI
- Browser-based terminal via xterm.js + WebSocket
- KI panel alongside terminal for AI queries
- REST API: /api/status, /api/ki, /api/exec, /api/history, /api/costs, /api/memory
- Token authentication (auto-generated or custom)
- Self-signed TLS (auto-generated)
- Session IDs + client IP in audit log

### KI improvements
- Pre-thinking: decomposes complex tasks before generating commands (makes mini smarter)
- Skills: predefined YAML scripts the KI prefers over improvised commands
- KI self-awareness: knows its own builtins, capabilities, and files
- Multi-line scripts executed as single action (not line-by-line)
- Interactive commands (vim, htop, visudo) blocked from agent auto-execution
- Action deduplication

### Shell
- Kill-safe history with fsync, TTY name, PID per entry
- Bang expansion reads from timestamped history (not readline)
- Log format: == timestamp ==> command, <== timestamp == response
- Log rotation with gzip compression at startup
- Clean display: no arrows on screen, structured format in logs only

### Builtins
- showlogs [shell|audit|conversation] [n] — paged output via $PAGER
- showmemory [facts|sessions|scratch] — paged output
- ki:skills — list loaded skills
- ki:disk — show ~/.kish/ disk usage
- ki:costs — API cost tracking with pricing

### Infrastructure
- heinzel-ai-core-go provider library (replaces custom OpenAI code)
- OpenAI + Anthropic with retry, streaming, cost tracking (SQLite)
- Pricing tables for all current models
- kish-sh as subdirectory (not separate repo)
- File structure: shell_*, ki_*, security_* prefixes
- Clean code pass: removed 800+ LOC dead code and verbose comments
- English UI strings throughout

## v0.1.0 — 2026-04-03

Initial release. See git history for details.
- 95/95 bash compatibility
- @ki prefix for AI queries
- Agent mode, memory, permissions, audit
- OpenAI provider with SSE streaming
- Man page, README, SECURITY.md
