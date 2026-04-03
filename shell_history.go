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
	"time"
)

type HistoryEntry struct {
	Num     int
	Time    time.Time
	Command string
}

type KishHistory struct {
	mu      sync.Mutex
	entries []HistoryEntry
	file    *os.File
	path    string
}

var kishHistory *KishHistory

func initHistory() {
	path := filepath.Join(kishDir(), "history_ts")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0600)
	if err != nil {
		return
	}
	kh := &KishHistory{file: file, path: path}
	kh.load()
	kishHistory = kh
}

func closeHistory() {
	if kishHistory != nil && kishHistory.file != nil {
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
	}
	kh.entries = append(kh.entries, entry)
	fmt.Fprintf(kh.file, "%d\t%s\n", entry.Time.Unix(), command)
}

func (kh *KishHistory) load() {
	data, err := os.ReadFile(kh.path)
	if err != nil {
		return
	}
	for i, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		var ts time.Time
		var cmd string
		if len(parts) == 2 {
			if unix, err := strconv.ParseInt(parts[0], 10, 64); err == nil {
				ts = time.Unix(unix, 0)
			}
			cmd = parts[1]
		} else {
			cmd = line // legacy format without timestamp
		}
		kh.entries = append(kh.entries, HistoryEntry{
			Num:     i + 1,
			Time:    ts,
			Command: cmd,
		})
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
		data, err := os.ReadFile(filepath.Join(kishDir(), "history"))
		if err != nil {
			return
		}
		for i, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			fmt.Fprintf(os.Stdout, " %4d  %s\n", i+1, line)
		}
		return
	}

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

	for _, entry := range entries {
		ts := ""
		if !entry.Time.IsZero() {
			ts = entry.Time.Format("2006-01-02 15:04")
		}
		fmt.Fprintf(os.Stdout, " %4d  %s  %s\n", entry.Num, ts, entry.Command)
	}
}
