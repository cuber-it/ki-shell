# Security Model — aish

(Formerly kish. `kish` remains a backward-compatible alias; `~/.kish` is migrated to `~/.aish` on first start.)

## Overview

aish is a bash-compatible shell with an integrated AI engine. The AI is **only active when explicitly invoked** via `@ki` or `ki` prefix. Without that prefix, aish behaves exactly like bash — no data is sent anywhere.

## Threat Model

### What we protect against

1. **AI acting autonomously** — The AI cannot execute commands without user involvement. `@ki` must be typed explicitly.
2. **AI self-modification** — The AI cannot modify its own config, permissions, or startup files. This is the only hardcoded block that cannot be overridden.
3. **Data exfiltration** — Command output is not sent to the AI API by default. Secrets in logs are scrubbed.
4. **Privilege escalation** — Destructive commands (sudo, rm -rf, chmod +s) require explicit user confirmation with red warning.
5. **Runaway costs** — A fail-closed cost guard runs before every LLM call (per-run, daily, and monthly token/USD limits, plus a killswitch). On any breach or unreadable usage/budget the call is refused. Plus rate limiting (20/min, 200/hour).

### What we do NOT protect against

1. **User confirming dangerous commands** — If the AI suggests `rm -rf /` and the user types `j`, it runs. The user is sovereign.
2. **Compromised API keys** — aish stores API keys in `~/.aish/config.yaml` (0644). Protect your home directory.
3. **Malicious AI responses** — If the AI provider is compromised, it could suggest harmful commands. The permission system catches known patterns, but cannot catch everything.
4. **Side-channel attacks** — The AI sees cwd, git branch, project type, and recent commands. This is necessary for context but leaks information to the API provider.

## Permission Layers

### Layer 1: Explicit Activation

The AI is dormant until `@ki`, `ki`, or `?` is typed. Normal shell commands never touch the AI.

### Layer 2: Self-Modification Block (hardcoded)

The AI cannot suggest commands that modify:
- `~/.aish/config.yaml`
- `~/.aish/permissions.yaml`
- `~/.aish/aishrc`

(The legacy `~/.kish/*` equivalents stay protected too, for migrated installs.)

This prevents the AI from escalating its own privileges. This block cannot be disabled — not via config, not via environment variable, not via god mode. Only by changing the source code.

### Layer 3: Action Levels

Every AI-suggested command is classified:

| Level | Behavior | Example |
|-------|----------|---------|
| **AutoRead** | Runs silently | `ls`, `cat`, `git status`, `docker ps` |
| **Confirm** | Yellow/red warning, user confirms | `rm -rf`, `git push --force`, `sudo` |
| **Blocked** | Rejected (user-configurable) | Fork bombs, `curl | bash` |

### Layer 4: Context Filtering

What is sent to the AI API (configurable in `permissions.yaml`):

| Data | Default | Setting |
|------|---------|---------|
| Current directory | Sent | `send_cwd: true` |
| Git branch | Sent | `send_git_branch: true` |
| Recent commands | Sent (no output) | `send_command_history: true` |
| Command stdout/stderr | **NOT sent** | `send_command_output: false` |
| Environment variables | Filtered subset | `send_env_vars: true` |
| Memory/vault | Sent | `send_memory: true` |

### Layer 5: Secret Scrubbing

The shell log (`~/.aish/shell.log`) scrubs known secret patterns before writing:
- API keys (OpenAI `sk-`, GitHub `ghp_`, AWS `AKIA`, GitLab `glpat-`, Slack `xox`)
- JWTs
- Passwords in URLs (`https://user:pass@host`)
- Authorization headers
- SSH private key markers
- Generic `password=`, `token=`, `secret=` patterns

### Layer 6: Audit Log

Every AI action is logged to `~/.aish/audit.log`:
- Timestamp, action level, command, user decision (allowed/denied/auto)
- Append-only, rotation at 10MB
- The AI cannot delete this file (protected path)

### Layer 7: Rate Limiting

- 20 queries per minute
- 200 queries per hour
- Warning at 80%, block at 100%

### Layer 8: Cost Guard (fail-closed)

- Pre-call check before every LLM call: per-run (tokens + USD), daily, and monthly limits, plus a killswitch
- On any breach OR any unreadable usage/budget, the call is **refused** — never logged-and-made-anyway
- Limits layered: defaults < `config.yaml` < `~/.aish/budget.json` (UI- and `ki:budget`-editable)
- Usage in SQLite at `~/.aish/costs.db`; guard audit trail in `~/.aish/cost_audit.jsonl`
- Controlled via `ki:budget`, `ki:killswitch`, `ki:costs`, the prompt indicator, and the web budget panel

## God Mode

Setting `KISH_GOD_MODE=yes` as environment variable disables Layer 3 action classification.
(The environment variable keeps its `KISH_` name for backward compatibility.) The AI can suggest any command, including sudo and destructive operations. User confirmation is still required unless `auto_execute: true` is set in permissions.

**Layer 2 (self-modification block) is NOT affected by god mode.**

## File Permissions

| File | Permissions | Why |
|------|-------------|-----|
| `~/.aish/config.yaml` | 0644 | Contains API keys — consider 0600 |
| `~/.aish/permissions.yaml` | 0644 | Security settings |
| `~/.aish/shell.log` | 0600 | May contain sensitive command output |
| `~/.aish/audit.log` | 0644 | Audit trail |
| `~/.aish/costs.db` | 0644 | Usage data |
| `~/.aish/vault/` | 0755 | Persistent memories |
| `~/.aish/history` | 0600 | Command history |

## Reporting Vulnerabilities

Report security issues at: https://github.com/cuber-it/ki-shell/issues

For sensitive vulnerabilities, contact: security@cuber-it.de
