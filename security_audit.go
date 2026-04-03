// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AuditLog records every KI action for security review.
// Append-only. The KI cannot delete or modify this file (Protected Path).
type AuditLog struct {
	mu       sync.Mutex
	file     *os.File
	filePath string
}

// AuditEntry represents one logged action
type AuditEntry struct {
	Timestamp time.Time
	Level     string // BLOCKED, CONFIRM, AUTO_READ, AUTO_WRITE, AUTO_EXEC, QUERY
	Command   string
	Decision  string // allowed, denied, auto, blocked
	Detail    string
}

var audit *AuditLog

func initAudit() {
	logPath := filepath.Join(kishDir(), "audit.log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kish: audit log error: %s\n", err)
		return
	}
	audit = &AuditLog{file: file, filePath: logPath}

	// Add to protected paths
	ProtectedPaths = append(ProtectedPaths, "~/.kish/audit.log")

	// Rotate if needed
	audit.rotateIfNeeded()
}

func closeAudit() {
	if audit != nil && audit.file != nil {
		audit.file.Close()
	}
}

// Log writes an audit entry
func (a *AuditLog) Log(level, command, decision, detail string) {
	if a == nil || a.file == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	entry := fmt.Sprintf("%s [%s] cmd=%q decision=%s %s\n",
		time.Now().Format("2006-01-02 15:04:05"),
		level,
		command,
		decision,
		detail,
	)
	a.file.WriteString(entry)
}

// LogQuery logs a KI query (what was sent to the API)
func (a *AuditLog) LogQuery(input string, provider string) {
	a.Log("QUERY", input, "sent", fmt.Sprintf("provider=%s", provider))
}

// LogAction logs a KI-requested action and its result
func (a *AuditLog) LogAction(command string, level ActionLevel, decision string, detail string) {
	levelStr := "UNKNOWN"
	switch level {
	case ActionBlocked:
		levelStr = "BLOCKED"
	case ActionConfirm:
		levelStr = "CONFIRM"
	case ActionAutoRead:
		levelStr = "AUTO_READ"
	case ActionAutoWrite:
		levelStr = "AUTO_WRITE"
	case ActionAutoExec:
		levelStr = "AUTO_EXEC"
	}
	a.Log(levelStr, command, decision, detail)
}

// rotateIfNeeded rotates the log file if it exceeds 10MB
func (a *AuditLog) rotateIfNeeded() {
	info, err := a.file.Stat()
	if err != nil {
		return
	}
	maxSize := int64(10 * 1024 * 1024) // 10 MB
	if info.Size() < maxSize {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	a.file.Close()

	// Rotate: audit.log.4 → delete, .3 → .4, .2 → .3, .1 → .2, .log → .1
	for i := 4; i >= 1; i-- {
		old := fmt.Sprintf("%s.%d", a.filePath, i)
		new := fmt.Sprintf("%s.%d", a.filePath, i+1)
		if i == 4 {
			os.Remove(old)
		} else {
			os.Rename(old, new)
		}
	}
	os.Rename(a.filePath, a.filePath+".1")

	file, err := os.OpenFile(a.filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		a.file = file
	}
}

// PrintRecent shows the last N audit entries (for ki:audit builtin)
func (a *AuditLog) PrintRecent(n int) {
	if a == nil {
		fmt.Fprintln(os.Stderr, "Audit-Log nicht initialisiert")
		return
	}

	data, err := os.ReadFile(a.filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kish: %s\n", err)
		return
	}

	lines := splitLines(string(data))
	start := 0
	if len(lines) > n {
		start = len(lines) - n
	}
	for _, line := range lines[start:] {
		fmt.Fprintln(os.Stdout, line)
	}
}

func splitLines(text string) []string {
	var lines []string
	for _, line := range splitString(text, '\n') {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func splitString(s string, sep byte) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		result = append(result, s[start:])
	}
	return result
}
