// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// captureKIActions swaps the action-log writer for an in-memory slice for the
// duration of a test.
func captureKIActions(t *testing.T) *[]KIActionRecord {
	t.Helper()
	prev := kiActionLogWriter
	recs := &[]KIActionRecord{}
	kiActionLogWriter = func(rec KIActionRecord) { *recs = append(*recs, rec) }
	t.Cleanup(func() { kiActionLogWriter = prev })
	return recs
}

func TestLogActionFillsTimestampAndID(t *testing.T) {
	recs := captureKIActions(t)
	logKIAction(KIActionRecord{Event: "ki_query", Prompt: "p", Status: "ok"})
	if len(*recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(*recs))
	}
	got := (*recs)[0]
	if got.Timestamp == "" {
		t.Error("timestamp should be auto-filled")
	}
	if got.ID == "" {
		t.Error("id should be auto-filled")
	}
}

func TestNewActionIDUnique(t *testing.T) {
	a, b := newKIActionID(), newKIActionID()
	if a == b {
		t.Fatalf("ids must be unique: %s == %s", a, b)
	}
}

func TestWriteActionLogRoundTrip(t *testing.T) {
	// Real disk write into a temp HOME so aishDir() resolves there.
	tmp := withTempKishHome(t)

	rec := KIActionRecord{
		Event:            "ki_query",
		Cwd:              "/work",
		Prompt:           "list big files",
		Model:            "gpt-test",
		Response:         "use find",
		SuggestedCommand: "find . -size +100M",
		Status:           "ok",
		InputTokens:      10,
		OutputTokens:     5,
		CostUSD:          0.001,
	}
	logKIAction(rec)

	path := filepath.Join(tmp, "actions.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading action log: %v", err)
	}
	var back KIActionRecord
	line := strings.TrimSpace(string(data))
	if err := json.Unmarshal([]byte(line), &back); err != nil {
		t.Fatalf("unmarshal logged line: %v", err)
	}
	if back.Prompt != rec.Prompt || back.SuggestedCommand != rec.SuggestedCommand {
		t.Errorf("round-trip mismatch: %+v", back)
	}
	if back.CostUSD != rec.CostUSD || back.InputTokens != 10 {
		t.Errorf("numeric fields lost: %+v", back)
	}
	if back.ID == "" || back.Timestamp == "" {
		t.Errorf("id/timestamp not persisted: %+v", back)
	}
}

func TestWriteActionLogAppends(t *testing.T) {
	tmp := withTempKishHome(t)

	logKIAction(KIActionRecord{Event: "ki_query", Prompt: "first", Status: "ok"})
	logKIAction(KIActionRecord{Event: "ki_query", Prompt: "second", Status: "error"})

	path := filepath.Join(tmp, "actions.jsonl")
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer file.Close()
	var lines int
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			lines++
		}
	}
	if lines != 2 {
		t.Fatalf("expected 2 appended lines, got %d", lines)
	}
}

func TestWriteActionLogFailSafe(t *testing.T) {
	// Point HOME at a path whose .aish cannot be created (a regular file blocks
	// the directory). The write must NOT panic — it fails closed and silent.
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "home")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", blocker) // UserHomeDir is a file → MkdirAll(.aish) fails
	aishDirOnce = sync.Once{}
	t.Cleanup(func() { aishDirOnce = sync.Once{} })

	// Must return normally despite the unwritable target.
	logKIAction(KIActionRecord{Event: "ki_query", Prompt: "p", Status: "ok"})
}
