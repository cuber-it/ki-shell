# Changelog

## Unreleased

### Learning loop (ki:learn)
- New `ki:learn <name> [description]` promotes the script from the last KI answer into a reusable skill (`~/.aish/skills/<name>.yaml`). The n-th time, the skill is in the prompt and gets reused (run via `skill:<name>`) instead of asking the model to rebuild it — saving tokens
- Deterministic, no API call: classified as a local builtin (not routed to the model). Searches the conversation backwards for the most recent turn containing a fenced script, skipping scriptless agent-loop follow-ups
- Propose-don't-dispose: promotion is an explicit, user-initiated step — a raw run (which may have failed) is never auto-learned. The default description is the clean first paragraph of the prompt (preThink enrichment stripped)
- Fail-closed write: `saveSkill` returns an error the caller handles; a freshly learned skill is reloaded immediately
- After a KI run that produced a fresh, not-yet-learned script, a discreet one-line hint suggests `ki:learn <name>` (interactive only; suppressed once the script is already a skill)
- Zero-token reuse: a natural-language request that unambiguously matches a learned skill is detected locally (keyword overlap, ≥2 shared significant words, clear margin) and proposed — `[j=run / n=no / k=ask model]` — running the skill with no API call. Conservative by design; on any ambiguity it falls through to the model. Propose-don't-dispose: the human confirms, the shell never auto-runs on a fuzzy match. Skill runs are recorded in the action log
- Tests: script-block extraction (bash/sh/shell/action/plain), skill round-trip, learn-from-conversation, no-script-fails, scriptless-followup-skipped, description-enrichment-stripped, not-a-KI-request

### KI action log (audit + learning source)
- New append-only `~/.aish/actions.jsonl`: every KI interaction is recorded with the task (prompt), the answer, the suggested command, model, status, token usage and cost. Written from the single `ProviderEngine.Query` choke point, so all callers are covered
- Two purposes at once: an audit trail (what was the AI asked, what did it answer) and a learning source (a task solved once need not be re-explored — raw material for later consolidation into reusable skills)
- Distinct from the existing logs: the shell log records executed commands (the outcome), `cost_audit.jsonl` records guard decisions; this log captures the task→solution pair
- Discipline: a raw record is a SOURCE, not a confirmed recipe (a task may have failed). Promotion to a reusable skill stays a separate, confirmed step — propose-don't-dispose. This change only writes the raw log
- Fail-safe: logging never blocks or breaks the shell flow; a write error is reported and swallowed. Failed KI attempts are logged too (part of the audit, and a "this did not work" signal)

### Resilience (API retries)
- `internal/llm` now retries transient API failures (HTTP 429 and 5xx, plus connection errors) with exponential backoff (1/2/4s, capped at 8s) and honors a numeric `Retry-After` header
- Safe by construction: a request is only retried *before* its response stream begins, so no tokens have been produced yet — retrying can never cause double cost or duplicated output. Once streaming starts there is no retry
- Non-retryable responses (4xx: bad request, auth, not found) surface immediately; after retries are exhausted the error is returned so callers fail closed
- Configurable per provider via `max_retries` (0 = default 3, negative = disabled)
- Retry tests cover 429/5xx-then-success, no-retry-on-4xx, exhaustion, disabled, backoff, and Retry-After

### Rebrand kish -> aish
- Product renamed kish -> aish ("KI" was too German). Binary build target is now `aish`; `kish` is kept as a backward-compatible alias/symlink (`make install` creates it)
- Config dir `~/.kish` -> `~/.aish` (`kishDir()` -> `aishDir()`), with automatic, lossless migration on first start: an existing `~/.kish` is moved to `~/.aish` (atomic rename, recursive-copy fallback across filesystems, legacy dir kept on copy). If only `~/.aish` exists it is used as-is and `~/.kish` is never overwritten
- Startup files: `~/.aishrc`, `/etc/aish.aishrc`, `.aishrc`, `~/.aish/profile` are preferred, with the legacy `~/.kish*` names honored as fallback
- Self-modification block and protected-path list cover both the new `~/.aish/*` and legacy `~/.kish/*` paths
- Strings/docs updated (README, SECURITY.md, man page renamed `kish.1` -> `aish.1`, help text, default prompt name, web UI title, error prefixes, About). The `KISH_PS1`/`KISH_GOD_MODE`/`KISH_THEME` env var names are kept for compatibility
- Unchanged on purpose: the shell fork `kish-sh` (internal dependency, module path) and the `ki` AI-command prefix
- Migration unit tests (move / skip-when-target-exists / no-legacy)

### Provider layer (self-contained)
- New `internal/llm` package: kish's own slim OpenAI + Anthropic layer (chat with SSE streaming + prompt/completion token usage) and SQLite usage DB (`modernc.org/sqlite`, pure Go) — replaces the external core provider dependency
- Removed the `heinzel-ai-core-go` require and the `../heinzel` replace directive; kish now builds and runs anywhere (e.g. Acer) without a sibling repo
- DB schema and `LogUsage`/`Stats`/`TodayStats`/`RecentRequests` kept identical, so existing `costs.db` files and the cost guard keep working unchanged
- `KIEngine` interface and the fail-closed cost-guard pre-check are unchanged

### Cost-Guard (fail-closed)
- New `cost_guard.go`: fail-closed cost control with hard per-run limit (tokens + USD), soft warning (80%), sparmode (90%, reduces max_tokens + shortens prompt), daily/monthly budget, and killswitch
- Pre-call budget check in `ki_provider.go` Query: refuses the API call on any limit breach or unreadable usage/budget — never logs and continues
- Audit trail of every check and block in `~/.kish/cost_audit.jsonl`
- Budget overrides + killswitch persisted in `~/.kish/budget.json`; layered defaults < config.yaml < budget.json
- New builtins `ki:budget` (show / set month|day|run|tokens-run|tokens-day / confirm) and `ki:killswitch on|off`
- `ki:costs` extended: consumption vs. budget (% used, remaining) for day and month
- Unit tests for every guard level plus fail-closed failure paths
- Prompt budget indicator: dezent at >= 80% monthly budget (`⚠$`), loud on killswitch or a reached hard limit (`⛔budget`), nothing below 80%
- Web budget panel: `/api/costs` now also returns limits + killswitch and accepts POST to edit them (writes `~/.kish/budget.json`); editable panel in the web UI with live usage

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
- PS1 support (bash-compatible), KISH_PS1 overrides, default with git branch + exit code
- History reload from disk (parallel sessions see each other)
- Toggleable logging (log on/off)
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
