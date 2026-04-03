// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

var audit *AuditLog

func initAudit() {
	logPath := filepath.Join(kishDir(), "audit.log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kish: audit log error: %s\n", err)
		return
	}
	audit = &AuditLog{file: file, filePath: logPath}
	ProtectedPaths = append(ProtectedPaths, "~/.kish/audit.log")
	audit.rotateIfNeeded()
}

func closeAudit() {
	if audit != nil && audit.file != nil {
		audit.file.Close()
	}
}

func (a *AuditLog) Log(level, command, decision, detail string) {
	if a == nil || a.file == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	entry := fmt.Sprintf("%s [%s] cmd=%q decision=%s %s\n",
		time.Now().Format("2006-01-02 15:04:05"),
		level, command, decision, detail,
	)
	a.file.WriteString(entry)
}

func (a *AuditLog) LogQuery(input string, provider string) {
	a.Log("QUERY", input, "sent", fmt.Sprintf("provider=%s", provider))
}

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

func (a *AuditLog) rotateIfNeeded() {
	info, err := a.file.Stat()
	if err != nil {
		return
	}
	maxSize := int64(10 * 1024 * 1024)
	if info.Size() < maxSize {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	a.file.Close()
	for i := 4; i >= 1; i-- {
		old := fmt.Sprintf("%s.%d", a.filePath, i)
		newPath := fmt.Sprintf("%s.%d", a.filePath, i+1)
		if i == 4 {
			os.Remove(old)
		} else {
			os.Rename(old, newPath)
		}
	}
	os.Rename(a.filePath, a.filePath+".1")

	file, err := os.OpenFile(a.filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		a.file = file
	}
}

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

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	start := 0
	if len(lines) > n {
		start = len(lines) - n
	}
	for _, line := range lines[start:] {
		fmt.Fprintln(os.Stdout, line)
	}
}
