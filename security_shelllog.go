// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ShellLog is a persistent, human-readable log of all shell activity.
// Secrets are scrubbed before writing.
//
// Format:
//
//	=== 2026-04-03 07:45:12 [exit:0] cwd:/home/user/project ===
//	$ ls -la
//	total 42
//	drwxr-xr-x ...
//	===
type ShellLog struct {
	mu       sync.Mutex
	file     *os.File
	filePath string
	maxSize  int64
	maxFiles int
}

func newShellLog() *ShellLog {
	logPath := filepath.Join(kishDir(), "shell.log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kish: shell.log error: %s\n", err)
		return nil
	}

	sl := &ShellLog{
		file:     file,
		filePath: logPath,
		maxSize:  5 * 1024 * 1024,
		maxFiles: 3,
	}
	sl.rotateIfNeeded()
	return sl
}

func (sl *ShellLog) Record(command string, exitCode int, stdout, stderr string) {
	if sl == nil || sl.file == nil {
		return
	}
	sl.mu.Lock()
	defer sl.mu.Unlock()

	cwd, _ := os.Getwd()
	ts := time.Now().Format("2006-01-02 15:04:05")

	var entry strings.Builder
	entry.WriteString(fmt.Sprintf("=== %s [exit:%d] cwd:%s ===\n", ts, exitCode, cwd))
	entry.WriteString(fmt.Sprintf("$ %s\n", command))

	if stdout != "" {
		scrubbed := scrubSecrets(stdout)
		lines := strings.Split(scrubbed, "\n")
		if len(lines) > 50 {
			for _, line := range lines[:25] {
				entry.WriteString(line + "\n")
			}
			entry.WriteString(fmt.Sprintf("... (%d Zeilen gekürzt) ...\n", len(lines)-50))
			for _, line := range lines[len(lines)-25:] {
				entry.WriteString(line + "\n")
			}
		} else {
			entry.WriteString(scrubbed)
			if !strings.HasSuffix(scrubbed, "\n") {
				entry.WriteString("\n")
			}
		}
	}

	if stderr != "" {
		scrubbed := scrubSecrets(stderr)
		entry.WriteString("[stderr]\n")
		entry.WriteString(scrubbed)
		if !strings.HasSuffix(scrubbed, "\n") {
			entry.WriteString("\n")
		}
	}

	entry.WriteString("===\n\n")
	sl.file.WriteString(entry.String())
}

func (sl *ShellLog) Search(query string, maxResults int) []string {
	if sl == nil {
		return nil
	}
	data, err := os.ReadFile(sl.filePath)
	if err != nil {
		return nil
	}

	query = strings.ToLower(query)
	entries := splitLogEntries(string(data))

	var matches []string
	for _, entry := range entries {
		if strings.Contains(strings.ToLower(entry), query) {
			matches = append(matches, entry)
		}
	}
	if len(matches) > maxResults {
		matches = matches[len(matches)-maxResults:]
	}
	return matches
}

func (sl *ShellLog) Recent(count int) []string {
	if sl == nil {
		return nil
	}
	data, err := os.ReadFile(sl.filePath)
	if err != nil {
		return nil
	}
	entries := splitLogEntries(string(data))
	if len(entries) > count {
		entries = entries[len(entries)-count:]
	}
	return entries
}

func (sl *ShellLog) FormatForKI(maxEntries int) string {
	entries := sl.Recent(maxEntries)
	if len(entries) == 0 {
		return ""
	}
	return "Shell-Log (letzte Aktivität):\n" + strings.Join(entries, "\n")
}

func (sl *ShellLog) Close() {
	if sl != nil && sl.file != nil {
		sl.file.Close()
	}
}

func (sl *ShellLog) rotateIfNeeded() {
	info, err := sl.file.Stat()
	if err != nil || info.Size() < sl.maxSize {
		return
	}

	sl.mu.Lock()
	defer sl.mu.Unlock()

	sl.file.Close()
	for i := sl.maxFiles; i >= 1; i-- {
		old := fmt.Sprintf("%s.%d", sl.filePath, i)
		if i == sl.maxFiles {
			os.Remove(old)
		} else {
			os.Rename(old, fmt.Sprintf("%s.%d", sl.filePath, i+1))
		}
	}
	os.Rename(sl.filePath, sl.filePath+".1")

	file, err := os.OpenFile(sl.filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err == nil {
		sl.file = file
	}
}

func splitLogEntries(text string) []string {
	var entries []string
	var current strings.Builder
	inEntry := false

	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "=== ") && strings.Contains(line, "[exit:") {
			if inEntry && current.Len() > 0 {
				entries = append(entries, strings.TrimSpace(current.String()))
			}
			current.Reset()
			current.WriteString(line + "\n")
			inEntry = true
		} else if inEntry {
			current.WriteString(line + "\n")
			if line == "===" {
				entries = append(entries, strings.TrimSpace(current.String()))
				current.Reset()
				inEntry = false
			}
		}
	}
	if inEntry && current.Len() > 0 {
		entries = append(entries, strings.TrimSpace(current.String()))
	}
	return entries
}

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(sk-[a-zA-Z0-9_-]{20,})`),
	regexp.MustCompile(`(?i)(ghp_[a-zA-Z0-9]{36,})`),
	regexp.MustCompile(`(?i)(gho_[a-zA-Z0-9]{36,})`),
	regexp.MustCompile(`(?i)(github_pat_[a-zA-Z0-9_]{20,})`),
	regexp.MustCompile(`(?i)(glpat-[a-zA-Z0-9_-]{20,})`),
	regexp.MustCompile(`(?i)(xox[bpsa]-[a-zA-Z0-9-]{10,})`),
	regexp.MustCompile(`(?i)(AKIA[A-Z0-9]{16})`),
	regexp.MustCompile(`(?i)(eyJ[a-zA-Z0-9_-]{20,}\.[a-zA-Z0-9_-]{20,}\.[a-zA-Z0-9_-]{20,})`),
	regexp.MustCompile(`(://[^:]+:)[^@]+(@)`),
	regexp.MustCompile(`(?i)(authorization:\s*bearer\s+)\S+`),
	regexp.MustCompile(`(?i)(authorization:\s*basic\s+)\S+`),
	regexp.MustCompile(`(?i)(authorization:\s*token\s+)\S+`),
	regexp.MustCompile(`(?i)(password|passwd|pwd|secret|token|api_key|apikey|api-key)[\s:=]+\S+`),
	regexp.MustCompile(`(?i)(-----BEGIN\s+(RSA\s+)?PRIVATE KEY-----)`),
}

func scrubSecrets(text string) string {
	for _, pattern := range secretPatterns {
		switch {
		case pattern.String() == `(://[^:]+:)[^@]+(@)`:
			text = pattern.ReplaceAllString(text, "${1}[REDACTED]${2}")
		case strings.Contains(pattern.String(), `(authorization`):
			text = pattern.ReplaceAllString(text, "${1}[REDACTED]")
		case strings.Contains(pattern.String(), `(password|passwd`):
			text = pattern.ReplaceAllString(text, "[REDACTED]")
		default:
			text = pattern.ReplaceAllString(text, "[REDACTED]")
		}
	}
	return text
}
