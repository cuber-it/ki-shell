// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
//
// CostGuard — fail-closed cost control for every LLM call.
// Go port of uc_llm_cost.cost_guard (qataki). Pure stdlib.
//
// Levels:
//   1. Hard limit per run (tokens + USD)  -> call refused (error)
//   2. Soft limit per run (80%)           -> warning, call proceeds
//   3. Sparmode (90%)                      -> reduce max_tokens / shorter prompt
//   4. Daily / monthly budget              -> call refused (error)
//   5. Killswitch                          -> hard-stop everything
//   Audit: every check + every block written to ~/.aish/cost_audit.jsonl
//
// FAIL-CLOSED: the pre-call budget check runs BEFORE the API call. On any
// limit breach OR any error reading usage/budget, the call is refused. We never
// "log and continue" — that is the core rule.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// CostLimitError signals a hard stop. The caller MUST abort the LLM call.
type CostLimitError struct {
	Reason string
}

func (e *CostLimitError) Error() string { return e.Reason }

// CostWarning is non-fatal: the run continues, the UI is informed.
type CostWarning struct {
	Reason string
	Level  string // "warn" | "sparmode"
}

// UsageProvider supplies the already-consumed amounts. Methods return an error
// so the guard can fail closed when the underlying store is unreadable.
type UsageProvider interface {
	TodayUsd() (float64, error)
	TodayTokens() (int64, error)
	MonthUsd() (float64, error)
}

// CostLimits holds all configured ceilings. Defaults mirror the Python port.
type CostLimits struct {
	HardTokensPerRun int     `json:"max_tokens_per_run"`
	SoftTokensPerRun int     `json:"soft_tokens_per_run"`
	SparmodeTokens   int     `json:"sparmode_tokens"`
	HardUsdPerRun    float64 `json:"max_usd_per_run"`
	HardTokensPerDay int64   `json:"max_tokens_per_day"`
	HardUsdPerDay    float64 `json:"max_usd_per_day"`
	HardUsdPerMonth  float64 `json:"max_usd_per_month"`
	ConfirmAboveUsd  float64 `json:"confirm_above_usd"`
}

func defaultCostLimits() CostLimits {
	return CostLimits{
		HardTokensPerRun: 50000,
		SoftTokensPerRun: 40000,
		SparmodeTokens:   45000,
		HardUsdPerRun:    1.00,
		HardTokensPerDay: 500000,
		HardUsdPerDay:    5.00,
		HardUsdPerMonth:  50.00,
		ConfirmAboveUsd:  0.10,
	}
}

// budgetOverrides is the on-disk shape of ~/.aish/budget.json. All limit fields
// are pointers so "unset" is distinguishable from "set to zero". Killswitch is
// stored here too, so the kill state survives restarts.
type budgetOverrides struct {
	MaxTokensPerRun  *int     `json:"max_tokens_per_run,omitempty"`
	SoftTokensPerRun *int     `json:"soft_tokens_per_run,omitempty"`
	SparmodeTokens   *int     `json:"sparmode_tokens,omitempty"`
	MaxUsdPerRun     *float64 `json:"max_usd_per_run,omitempty"`
	MaxTokensPerDay  *int64   `json:"max_tokens_per_day,omitempty"`
	MaxUsdPerDay     *float64 `json:"max_usd_per_day,omitempty"`
	MaxUsdPerMonth   *float64 `json:"max_usd_per_month,omitempty"`
	ConfirmAboveUsd  *float64 `json:"confirm_above_usd,omitempty"`
	Killswitch       *bool    `json:"killswitch,omitempty"`
}

func budgetStorePath() string {
	return filepath.Join(aishDir(), "budget.json")
}

func costAuditPath() string {
	return filepath.Join(aishDir(), "cost_audit.jsonl")
}

// loadBudgetOverrides reads ~/.aish/budget.json. A missing file is fine (returns
// empty overrides, no error). A present-but-unreadable/corrupt file IS an error
// so the guard can fail closed.
func loadBudgetOverrides() (budgetOverrides, error) {
	var ov budgetOverrides
	path := budgetStorePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ov, nil
		}
		return ov, fmt.Errorf("budget.json unreadable: %w", err)
	}
	if err := json.Unmarshal(data, &ov); err != nil {
		return ov, fmt.Errorf("budget.json corrupt: %w", err)
	}
	return ov, nil
}

func saveBudgetOverrides(ov budgetOverrides) error {
	data, err := json.MarshalIndent(ov, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(budgetStorePath(), data, 0644)
}

// effectiveLimits layers: defaults < config (CostLimit/MaxTokens) < budget.json.
func effectiveLimits(cfg *KishConfig, ov budgetOverrides) CostLimits {
	limits := defaultCostLimits()

	// Config layer (yaml): cost_limit feeds the monthly USD ceiling,
	// max_tokens feeds the per-run token ceiling, when set (>0).
	if cfg != nil {
		if cfg.KI.CostLimit > 0 {
			limits.HardUsdPerMonth = cfg.KI.CostLimit
		}
		if cfg.KI.MaxTokens > 0 {
			limits.HardTokensPerRun = cfg.KI.MaxTokens
			limits.SoftTokensPerRun = cfg.KI.MaxTokens * 80 / 100
			limits.SparmodeTokens = cfg.KI.MaxTokens * 90 / 100
		}
	}

	// budget.json layer (UI / ki:budget) has highest priority.
	if ov.MaxTokensPerRun != nil {
		limits.HardTokensPerRun = *ov.MaxTokensPerRun
	}
	if ov.SoftTokensPerRun != nil {
		limits.SoftTokensPerRun = *ov.SoftTokensPerRun
	}
	if ov.SparmodeTokens != nil {
		limits.SparmodeTokens = *ov.SparmodeTokens
	}
	if ov.MaxUsdPerRun != nil {
		limits.HardUsdPerRun = *ov.MaxUsdPerRun
	}
	if ov.MaxTokensPerDay != nil {
		limits.HardTokensPerDay = *ov.MaxTokensPerDay
	}
	if ov.MaxUsdPerDay != nil {
		limits.HardUsdPerDay = *ov.MaxUsdPerDay
	}
	if ov.MaxUsdPerMonth != nil {
		limits.HardUsdPerMonth = *ov.MaxUsdPerMonth
	}
	if ov.ConfirmAboveUsd != nil {
		limits.ConfirmAboveUsd = *ov.ConfirmAboveUsd
	}
	return limits
}

// CostGuard runs before every LLM call. Constructed fresh per call so it always
// sees the latest budget.json and usage.
type CostGuard struct {
	Limits     CostLimits
	Usage      UsageProvider
	Killswitch bool

	sparmode    bool
	auditWriter func(event string, data map[string]interface{}) // injectable for tests
}

// newCostGuard builds a guard from config + budget.json + usage source. It fails
// closed: if budget.json is present but unreadable, the returned error must
// propagate and the LLM call must be refused.
func newCostGuard(cfg *KishConfig, usage UsageProvider) (*CostGuard, error) {
	ov, err := loadBudgetOverrides()
	if err != nil {
		return nil, &CostLimitError{Reason: "Cost-Guard: " + err.Error() + " (fail-closed)"}
	}
	kill := false
	if ov.Killswitch != nil {
		kill = *ov.Killswitch
	}
	return &CostGuard{
		Limits:      effectiveLimits(cfg, ov),
		Usage:       usage,
		Killswitch:  kill,
		auditWriter: writeCostAudit,
	}, nil
}

func (g *CostGuard) audit(event string, data map[string]interface{}) {
	if g.auditWriter != nil {
		g.auditWriter(event, data)
	}
}

// PreCheck runs BEFORE the API call. It enforces killswitch and daily/monthly
// budget. Returns *CostLimitError on any breach OR on any error reading usage —
// in both cases the caller MUST NOT make the API call (fail-closed).
func (g *CostGuard) PreCheck() error {
	// Level 5: killswitch — absolute priority.
	if g.Killswitch {
		g.audit("block", map[string]interface{}{"reason": "killswitch"})
		return &CostLimitError{Reason: "Killswitch aktiv — alle KI-Calls gesperrt"}
	}

	if g.Usage == nil {
		g.audit("block", map[string]interface{}{"reason": "no usage source"})
		return &CostLimitError{Reason: "Cost-Guard: keine Verbrauchsquelle (fail-closed)"}
	}

	// Level 4: daily / monthly budget. Any read error => fail closed.
	todayUsd, err := g.Usage.TodayUsd()
	if err != nil {
		g.audit("block", map[string]interface{}{"reason": "usage read error", "detail": err.Error()})
		return &CostLimitError{Reason: "Cost-Guard: Verbrauch nicht lesbar (fail-closed): " + err.Error()}
	}
	todayTokens, err := g.Usage.TodayTokens()
	if err != nil {
		g.audit("block", map[string]interface{}{"reason": "usage read error", "detail": err.Error()})
		return &CostLimitError{Reason: "Cost-Guard: Verbrauch nicht lesbar (fail-closed): " + err.Error()}
	}
	monthUsd, err := g.Usage.MonthUsd()
	if err != nil {
		g.audit("block", map[string]interface{}{"reason": "usage read error", "detail": err.Error()})
		return &CostLimitError{Reason: "Cost-Guard: Verbrauch nicht lesbar (fail-closed): " + err.Error()}
	}

	if todayTokens >= g.Limits.HardTokensPerDay {
		reason := fmt.Sprintf("Tages-Token-Limit: %d / %d", todayTokens, g.Limits.HardTokensPerDay)
		g.audit("block", map[string]interface{}{"reason": reason})
		return &CostLimitError{Reason: reason}
	}
	if todayUsd >= g.Limits.HardUsdPerDay {
		reason := fmt.Sprintf("Tages-USD-Limit: $%.4f / $%.2f", todayUsd, g.Limits.HardUsdPerDay)
		g.audit("block", map[string]interface{}{"reason": reason})
		return &CostLimitError{Reason: reason}
	}
	if monthUsd >= g.Limits.HardUsdPerMonth {
		reason := fmt.Sprintf("Monats-USD-Limit: $%.4f / $%.2f", monthUsd, g.Limits.HardUsdPerMonth)
		g.audit("block", map[string]interface{}{"reason": reason})
		return &CostLimitError{Reason: reason}
	}

	g.audit("precheck_ok", map[string]interface{}{
		"today_usd": round6(todayUsd), "today_tokens": todayTokens, "month_usd": round6(monthUsd),
	})
	return nil
}

// CheckRunTokens enforces the per-run token/USD ceilings against an estimate or
// running tally. Hard breach => *CostLimitError. Otherwise it returns soft and
// sparmode warnings (and flips the sparmode flag).
func (g *CostGuard) CheckRunTokens(tokens int, usd float64) ([]CostWarning, error) {
	if tokens >= g.Limits.HardTokensPerRun {
		reason := fmt.Sprintf("Hard-Token-Limit: %d / %d", tokens, g.Limits.HardTokensPerRun)
		g.audit("block", map[string]interface{}{"reason": reason})
		return nil, &CostLimitError{Reason: reason}
	}
	if usd >= g.Limits.HardUsdPerRun {
		reason := fmt.Sprintf("Hard-USD-Limit: $%.4f / $%.2f", usd, g.Limits.HardUsdPerRun)
		g.audit("block", map[string]interface{}{"reason": reason})
		return nil, &CostLimitError{Reason: reason}
	}

	var warnings []CostWarning
	if tokens >= g.Limits.SoftTokensPerRun {
		pct := 0
		if g.Limits.HardTokensPerRun > 0 {
			pct = tokens * 100 / g.Limits.HardTokensPerRun
		}
		warnings = append(warnings, CostWarning{
			Reason: fmt.Sprintf("Naehert sich Token-Limit: %d (%d%%)", tokens, pct),
			Level:  "warn",
		})
	}
	if tokens >= g.Limits.SparmodeTokens {
		g.sparmode = true
		pct := 0
		if g.Limits.HardTokensPerRun > 0 {
			pct = tokens * 100 / g.Limits.HardTokensPerRun
		}
		warnings = append(warnings, CostWarning{
			Reason: fmt.Sprintf("Sparmode aktiv bei %d Tokens (%d%%)", tokens, pct),
			Level:  "sparmode",
		})
	}
	return warnings, nil
}

// Sparmode reports whether economy mode is active.
func (g *CostGuard) Sparmode() bool { return g.sparmode }

// MaxTokensFor returns the max_tokens to send: clamped down in sparmode.
func (g *CostGuard) MaxTokensFor(defaultMax int) int {
	if g.sparmode {
		if defaultMax > 0 && defaultMax < 300 {
			return defaultMax
		}
		return 300
	}
	return defaultMax
}

// SparmodeSuffix is appended to the prompt when economy mode is active.
func (g *CostGuard) SparmodeSuffix() string {
	if !g.sparmode {
		return ""
	}
	return "\n\nKOSTEN-SPARMODUS AKTIV: Antworte extrem kurz und praezise. " +
		"Kein Fliesstext. Nur das Wesentliche."
}

// RecordUsage writes a consumption entry to the audit log after a call.
func (g *CostGuard) RecordUsage(model string, inputTokens, outputTokens int, usd float64) {
	g.audit("llm_call", map[string]interface{}{
		"model":             model,
		"prompt_tokens":     inputTokens,
		"completion_tokens": outputTokens,
		"tokens":            inputTokens + outputTokens,
		"cost_usd":          round6(usd),
	})
}

var costAuditMu sync.Mutex

// writeCostAudit appends one JSON line to ~/.aish/cost_audit.jsonl. Audit
// failures are non-fatal (logged to stderr) — they must not block a call that
// the guard already approved, but also must never let a *block* slip through;
// blocks are decided before this is reached.
func writeCostAudit(event string, data map[string]interface{}) {
	costAuditMu.Lock()
	defer costAuditMu.Unlock()

	entry := map[string]interface{}{
		"ts":    time.Now().UTC().Format(time.RFC3339),
		"event": event,
	}
	for key, val := range data {
		entry[key] = val
	}
	line, err := json.Marshal(entry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aish: cost audit marshal failed: %s\n", err)
		return
	}
	file, err := os.OpenFile(costAuditPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aish: cost audit failed: %s\n", err)
		return
	}
	defer file.Close()
	file.Write(append(line, '\n'))
}

func round6(value float64) float64 {
	return float64(int64(value*1e6+0.5)) / 1e6
}

// parseBudgetFloat / parseBudgetInt are small helpers for the ki:budget builtin.
func parseBudgetFloat(text string) (float64, error) {
	return strconv.ParseFloat(text, 64)
}

func parseBudgetInt(text string) (int64, error) {
	return strconv.ParseInt(text, 10, 64)
}
