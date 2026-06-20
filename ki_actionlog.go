// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
//
// KI action log: an append-only record of every KI interaction — the task it was
// given, what it answered, the command it suggested, and the cost.
//
// This is distinct from two existing logs:
//   - the shell log / history records executed commands (see logAction in
//     ki_actions.go) — the execution OUTCOME;
//   - cost_audit.jsonl records the cost guard's decisions.
//
// The KI action log captures the TASK -> SOLUTION pair, serving two purposes at
// once:
//   - Audit trail (Safety): what was the AI asked to do, and what did it answer.
//   - Learning source (Memory): a task solved once need not be re-explored — the
//     raw record is the material a later consolidation step turns into skills.
//
// Crucial discipline (propose-don't-dispose): a raw record is a SOURCE, not a
// confirmed recipe. A task may have FAILED; replaying a raw record blindly would
// teach mistakes. Promotion of a raw record into a reusable skill must be
// confirmed, never automatic. This file only writes the raw log.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// kiActionLogPath is the append-only KI interaction log, alongside cost_audit.jsonl.
func kiActionLogPath() string {
	return filepath.Join(aishDir(), "actions.jsonl")
}

// KIActionRecord is one line in actions.jsonl. Fields are kept stable so the
// file stays grep-able and a future consolidation worker can rely on the schema.
type KIActionRecord struct {
	Timestamp        string  `json:"ts"`
	ID               string  `json:"id"`
	Event            string  `json:"event"` // ki_query (more types later, e.g. executed)
	Cwd              string  `json:"cwd,omitempty"`
	Prompt           string  `json:"prompt"`
	Model            string  `json:"model,omitempty"`
	Response         string  `json:"response,omitempty"`
	SuggestedCommand string  `json:"suggested_command,omitempty"`
	Status           string  `json:"status"` // ok | error
	Error            string  `json:"error,omitempty"`
	InputTokens      int     `json:"input_tokens"`
	OutputTokens     int     `json:"output_tokens"`
	CostUSD          float64 `json:"cost_usd"`
}

var (
	kiActionLogMu  sync.Mutex
	kiActionLogSeq uint64
	// kiActionLogWriter is indirected so tests can capture records without disk I/O.
	kiActionLogWriter = writeKIActionLog
)

// newKIActionID returns a sortable, unique id for one interaction (nanosecond
// timestamp + a process-local sequence to break ties on fast successive calls).
func newKIActionID() string {
	n := atomic.AddUint64(&kiActionLogSeq, 1)
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), n)
}

// logKIAction records one KI interaction. It NEVER blocks or fails the shell
// flow: the interaction already happened, so losing a log line must not break
// the user's session — any error is reported to stderr and swallowed.
func logKIAction(rec KIActionRecord) {
	if rec.Timestamp == "" {
		rec.Timestamp = time.Now().Format(time.RFC3339)
	}
	if rec.ID == "" {
		rec.ID = newKIActionID()
	}
	kiActionLogWriter(rec)
}

func writeKIActionLog(rec KIActionRecord) {
	kiActionLogMu.Lock()
	defer kiActionLogMu.Unlock()
	line, err := json.Marshal(rec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aish: action log marshal failed: %s\n", err)
		return
	}
	file, err := os.OpenFile(kiActionLogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aish: action log failed: %s\n", err)
		return
	}
	defer file.Close()
	if _, err := file.Write(append(line, '\n')); err != nil {
		fmt.Fprintf(os.Stderr, "aish: action log write failed: %s\n", err)
	}
}
