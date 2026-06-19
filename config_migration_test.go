// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
//
// Migration tests for the kish->aish config-dir rebrand. The migration is
// failure-mode critical: existing user data under ~/.kish must never be lost
// when the directory moves to ~/.aish.
package main

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// freshHome gives the test an isolated HOME and resets the one-shot migration
// latch so aishDir() re-evaluates against this HOME.
func freshHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	aishDirOnce = sync.Once{}
	t.Cleanup(func() { aishDirOnce = sync.Once{} })
	return tmp
}

// TestMigrationMovesLegacyData: ~/.kish present, ~/.aish absent -> data lands in
// ~/.aish, nothing lost.
func TestMigrationMovesLegacyData(t *testing.T) {
	home := freshHome(t)
	legacy := filepath.Join(home, ".kish")
	if err := os.MkdirAll(filepath.Join(legacy, "vault", "fact"), 0755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"config.yaml":            "ki:\n  provider: anthropic\n",
		"budget.json":            `{"killswitch":true}`,
		"costs.db":               "binary-ish",
		"history":                "ls -la\n",
		"audit.log":              "AUDIT entry\n",
		"vault/fact/hello.yaml":  "key: hello\nvalue: world\n",
	}
	for rel, content := range files {
		if err := os.WriteFile(filepath.Join(legacy, rel), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	dir := aishDir()
	if dir != filepath.Join(home, ".aish") {
		t.Fatalf("aishDir() = %q, want ~/.aish", dir)
	}

	for rel, want := range files {
		got, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			t.Fatalf("migrated file %s missing: %v", rel, err)
		}
		if string(got) != want {
			t.Fatalf("migrated file %s content = %q, want %q", rel, got, want)
		}
	}
}

// TestMigrationSkippedWhenAishExists: if ~/.aish already exists, the live data
// must win and ~/.kish must be left untouched (no clobbering).
func TestMigrationSkippedWhenAishExists(t *testing.T) {
	home := freshHome(t)
	legacy := filepath.Join(home, ".kish")
	target := filepath.Join(home, ".aish")
	if err := os.MkdirAll(legacy, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "budget.json"), []byte(`{"killswitch":true}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "budget.json"), []byte(`{"killswitch":false}`), 0644); err != nil {
		t.Fatal(err)
	}

	aishDir()

	// ~/.aish kept its own data ...
	got, err := os.ReadFile(filepath.Join(target, "budget.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"killswitch":false}` {
		t.Fatalf("~/.aish budget.json was overwritten: %q", got)
	}
	// ... and ~/.kish is still present (not destroyed).
	if _, err := os.Stat(filepath.Join(legacy, "budget.json")); err != nil {
		t.Fatalf("legacy ~/.kish should be left untouched: %v", err)
	}
}

// TestMigrationNoLegacy: neither dir present -> aishDir() just creates ~/.aish.
func TestMigrationNoLegacy(t *testing.T) {
	home := freshHome(t)
	dir := aishDir()
	if dir != filepath.Join(home, ".aish") {
		t.Fatalf("aishDir() = %q, want ~/.aish", dir)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("~/.aish should be created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".kish")); !os.IsNotExist(err) {
		t.Fatalf("~/.kish should not be created spuriously")
	}
}
