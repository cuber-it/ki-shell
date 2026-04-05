// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type HistoryEntry struct {
	Num     int
	Time    time.Time
	Command string
	TTY     string
	PID     int
}

type KishHistory struct {
	mu   sync.Mutex
	entries []HistoryEntry
	file *os.File
	path string
	tty  string
	pid  int
}

var kishHistory *KishHistory

func initHistory() {
	path := filepath.Join(kishDir(), "history")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0600)
	if err != nil {
		return
	}

	tty := ttyName()
	kh := &KishHistory{file: file, path: path, tty: tty, pid: os.Getpid()}
	kh.load()
	kishHistory = kh
}

func closeHistory() {
	if kishHistory != nil && kishHistory.file != nil {
		kishHistory.file.Sync()
		kishHistory.file.Close()
	}
}

func (kh *KishHistory) Add(command string) {
	if kh == nil || command == "" {
		return
	}
	kh.mu.Lock()
	defer kh.mu.Unlock()

	entry := HistoryEntry{
		Num:     len(kh.entries) + 1,
		Time:    time.Now(),
		Command: command,
		TTY:     kh.tty,
		PID:     kh.pid,
	}
	kh.entries = append(kh.entries, entry)

	fmt.Fprintf(kh.file, "%d\t%s\t%d\t%s\n", entry.Time.Unix(), kh.tty, kh.pid, command)
	kh.file.Sync()
}

// reload re-reads the history file from disk to pick up entries from other sessions.
func (kh *KishHistory) reload() {
	kh.mu.Lock()
	defer kh.mu.Unlock()
	kh.entries = nil
	kh.loadLocked()
}

func (kh *KishHistory) load() {
	kh.loadLocked()
}

func (kh *KishHistory) loadLocked() {
	data, err := os.ReadFile(kh.path)
	if err != nil {
		return
	}
	for i, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 4)
		var entry HistoryEntry
		entry.Num = i + 1

		switch len(parts) {
		case 4: // new format: timestamp\ttty\tpid\tcommand
			if unix, err := strconv.ParseInt(parts[0], 10, 64); err == nil {
				entry.Time = time.Unix(unix, 0)
			}
			entry.TTY = parts[1]
			entry.PID, _ = strconv.Atoi(parts[2])
			entry.Command = parts[3]
		case 2: // old format: timestamp\tcommand
			if unix, err := strconv.ParseInt(parts[0], 10, 64); err == nil {
				entry.Time = time.Unix(unix, 0)
			}
			entry.Command = parts[1]
		default: // legacy: just command
			entry.Command = line
		}
		kh.entries = append(kh.entries, entry)
	}
}

func (kh *KishHistory) Last(n int) []HistoryEntry {
	if kh == nil {
		return nil
	}
	kh.mu.Lock()
	defer kh.mu.Unlock()
	if n >= len(kh.entries) {
		return kh.entries
	}
	return kh.entries[len(kh.entries)-n:]
}

func (kh *KishHistory) All() []HistoryEntry {
	if kh == nil {
		return nil
	}
	kh.mu.Lock()
	defer kh.mu.Unlock()
	return kh.entries
}

func printHistory(fields []string) {
	if kishHistory == nil {
		return
	}

	// Reload from disk to see entries from other sessions (web, parallel terminals)
	kishHistory.reload()

	n := 0
	if len(fields) > 1 {
		fmt.Sscanf(fields[1], "%d", &n)
	}

	var entries []HistoryEntry
	if n > 0 {
		entries = kishHistory.Last(n)
	} else {
		entries = kishHistory.All()
	}

	for _, e := range entries {
		ts := ""
		if !e.Time.IsZero() {
			ts = e.Time.Format("2006-01-02 15:04")
		}
		tty := ""
		if e.TTY != "" {
			tty = fmt.Sprintf(" [%s:%d]", e.TTY, e.PID)
		}
		fmt.Fprintf(os.Stdout, " %4d  %s%s  %s\n", e.Num, ts, tty, e.Command)
	}
}

func ttyName() string {
	link, err := os.Readlink("/proc/self/fd/0")
	if err == nil && strings.HasPrefix(link, "/dev/") {
		return strings.TrimPrefix(link, "/dev/")
	}
	var stat syscall.Stat_t
	if err := syscall.Fstat(0, &stat); err == nil {
		return fmt.Sprintf("tty:%d", stat.Rdev)
	}
	return ""
}
