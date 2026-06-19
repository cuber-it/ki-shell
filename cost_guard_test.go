// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// fakeUsage is a controllable UsageProvider for tests. A non-nil err on any
// field makes that read fail, exercising the fail-closed path.
type fakeUsage struct {
	todayUsd    float64
	todayTokens int64
	monthUsd    float64
	err         error
}

func (f fakeUsage) TodayUsd() (float64, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.todayUsd, nil
}
func (f fakeUsage) TodayTokens() (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.todayTokens, nil
}
func (f fakeUsage) MonthUsd() (float64, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.monthUsd, nil
}

// withTempKishHome points aishDir() at a fresh temp HOME for the test and resets
// kiConfig. Returns the dir so the test can read/write budget.json.
func withTempKishHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	prevConfig := kiConfig
	kiConfig = DefaultConfig()
	t.Cleanup(func() {
		kiConfig = prevConfig
		aishDirOnce = sync.Once{} // reset migration latch for the next test's fresh HOME
	})
	aishDirOnce = sync.Once{} // ensure aishDir() re-evaluates against this temp HOME
	return filepath.Join(tmp, ".aish")
}

// guardWith builds a guard with default limits, the given usage source, and an
// in-memory audit sink so tests can assert on emitted events.
func guardWith(t *testing.T, usage UsageProvider) (*CostGuard, *[]string) {
	t.Helper()
	guard, err := newCostGuard(kiConfig, usage)
	if err != nil {
		t.Fatalf("newCostGuard failed: %v", err)
	}
	var events []string
	guard.auditWriter = func(event string, data map[string]interface{}) {
		events = append(events, event)
	}
	return guard, &events
}

func assertCostLimit(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected CostLimitError, got nil")
	}
	var cle *CostLimitError
	if !errors.As(err, &cle) {
		t.Fatalf("expected *CostLimitError, got %T: %v", err, err)
	}
}

func contains(events []string, want string) bool {
	for _, event := range events {
		if event == want {
			return true
		}
	}
	return false
}

// --- Level 1: hard limit per run -------------------------------------------

func TestHardRunTokenLimit(t *testing.T) {
	withTempKishHome(t)
	guard, events := guardWith(t, fakeUsage{})

	warnings, err := guard.CheckRunTokens(guard.Limits.HardTokensPerRun, 0)
	assertCostLimit(t, err)
	if warnings != nil {
		t.Fatalf("expected no warnings on hard stop, got %v", warnings)
	}
	if !contains(*events, "block") {
		t.Fatalf("expected a block audit event, got %v", *events)
	}
}

func TestHardRunUsdLimit(t *testing.T) {
	withTempKishHome(t)
	guard, _ := guardWith(t, fakeUsage{})

	_, err := guard.CheckRunTokens(0, guard.Limits.HardUsdPerRun+0.01)
	assertCostLimit(t, err)
}

// --- Level 2: soft limit (80%) ---------------------------------------------

func TestSoftRunLimitWarns(t *testing.T) {
	withTempKishHome(t)
	guard, _ := guardWith(t, fakeUsage{})

	warnings, err := guard.CheckRunTokens(guard.Limits.SoftTokensPerRun, 0)
	if err != nil {
		t.Fatalf("soft limit must not error, got %v", err)
	}
	if len(warnings) == 0 || warnings[0].Level != "warn" {
		t.Fatalf("expected a warn-level warning, got %v", warnings)
	}
	if guard.Sparmode() {
		t.Fatal("sparmode must NOT be active at the soft threshold")
	}
}

// --- Level 3: sparmode (90%) -----------------------------------------------

func TestSparmodeActivates(t *testing.T) {
	withTempKishHome(t)
	guard, _ := guardWith(t, fakeUsage{})

	warnings, err := guard.CheckRunTokens(guard.Limits.SparmodeTokens, 0)
	if err != nil {
		t.Fatalf("sparmode threshold must not error, got %v", err)
	}
	if !guard.Sparmode() {
		t.Fatal("sparmode flag must be set")
	}
	foundSparmode := false
	for _, warning := range warnings {
		if warning.Level == "sparmode" {
			foundSparmode = true
		}
	}
	if !foundSparmode {
		t.Fatalf("expected a sparmode warning, got %v", warnings)
	}
	if got := guard.MaxTokensFor(2048); got != 300 {
		t.Fatalf("sparmode should clamp max_tokens to 300, got %d", got)
	}
	if guard.SparmodeSuffix() == "" {
		t.Fatal("sparmode suffix must be non-empty")
	}
}

// --- Level 4: daily budget -------------------------------------------------

func TestDailyUsdBudgetBlocks(t *testing.T) {
	withTempKishHome(t)
	guard, events := guardWith(t, fakeUsage{todayUsd: 5.00, monthUsd: 5.00})

	err := guard.PreCheck()
	assertCostLimit(t, err)
	if !contains(*events, "block") {
		t.Fatalf("expected a block audit event, got %v", *events)
	}
}

func TestDailyTokenBudgetBlocks(t *testing.T) {
	withTempKishHome(t)
	guard, _ := guardWith(t, fakeUsage{todayTokens: 500000})

	assertCostLimit(t, guard.PreCheck())
}

func TestPreCheckPassesUnderBudget(t *testing.T) {
	withTempKishHome(t)
	guard, events := guardWith(t, fakeUsage{todayUsd: 0.10, todayTokens: 100, monthUsd: 0.10})

	if err := guard.PreCheck(); err != nil {
		t.Fatalf("expected pass under budget, got %v", err)
	}
	if !contains(*events, "precheck_ok") {
		t.Fatalf("expected precheck_ok audit event, got %v", *events)
	}
}

// --- Level 4: monthly budget -----------------------------------------------

func TestMonthlyUsdBudgetBlocks(t *testing.T) {
	withTempKishHome(t)
	guard, _ := guardWith(t, fakeUsage{monthUsd: 50.00})

	assertCostLimit(t, guard.PreCheck())
}

// --- Level 5: killswitch ----------------------------------------------------

func TestKillswitchBlocks(t *testing.T) {
	dir := withTempKishHome(t)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	kill := true
	if err := saveBudgetOverrides(budgetOverrides{Killswitch: &kill}); err != nil {
		t.Fatal(err)
	}

	guard, events := guardWith(t, fakeUsage{todayUsd: 0, monthUsd: 0})
	if !guard.Killswitch {
		t.Fatal("killswitch override must be loaded")
	}
	assertCostLimit(t, guard.PreCheck())
	if !contains(*events, "block") {
		t.Fatalf("expected a block audit event, got %v", *events)
	}
}

// --- Failure paths: fail-closed --------------------------------------------

func TestUsageReadErrorFailsClosed(t *testing.T) {
	withTempKishHome(t)
	guard, events := guardWith(t, fakeUsage{err: errors.New("costs.db unavailable")})

	// Even though spent amounts are "unknown", the call must be REFUSED.
	assertCostLimit(t, guard.PreCheck())
	if !contains(*events, "block") {
		t.Fatalf("expected a block audit event on read failure, got %v", *events)
	}
}

func TestNilUsageFailsClosed(t *testing.T) {
	withTempKishHome(t)
	guard, _ := guardWith(t, nil)
	assertCostLimit(t, guard.PreCheck())
}

func TestCorruptBudgetJsonFailsClosed(t *testing.T) {
	dir := withTempKishHome(t)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "budget.json"), []byte("{not valid json"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := newCostGuard(kiConfig, fakeUsage{})
	assertCostLimit(t, err)
}

// --- Persistence: budget.json overrides ------------------------------------

func TestBudgetOverridesApplied(t *testing.T) {
	dir := withTempKishHome(t)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	month := 2.50
	if err := saveBudgetOverrides(budgetOverrides{MaxUsdPerMonth: &month}); err != nil {
		t.Fatal(err)
	}

	guard, _ := guardWith(t, fakeUsage{monthUsd: 2.50})
	if guard.Limits.HardUsdPerMonth != 2.50 {
		t.Fatalf("override not applied: got %.2f", guard.Limits.HardUsdPerMonth)
	}
	// At exactly the (lowered) limit the call must be refused.
	assertCostLimit(t, guard.PreCheck())
}

func TestConfigLayerFeedsLimits(t *testing.T) {
	withTempKishHome(t)
	cfg := DefaultConfig()
	cfg.KI.CostLimit = 7.0
	cfg.KI.MaxTokens = 1000
	limits := effectiveLimits(cfg, budgetOverrides{})
	if limits.HardUsdPerMonth != 7.0 {
		t.Fatalf("cost_limit not mapped to monthly: %.2f", limits.HardUsdPerMonth)
	}
	if limits.HardTokensPerRun != 1000 || limits.SoftTokensPerRun != 800 || limits.SparmodeTokens != 900 {
		t.Fatalf("max_tokens not mapped to run limits: %+v", limits)
	}
}
