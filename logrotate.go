// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// rotateAllLogs compresses logs older than today and removes very old ones.
// Called once at startup.
func rotateAllLogs() {
	dir := kishDir()
	today := time.Now().Format("2006-01-02")

	// Rotate shell.log → shell.log.2026-04-03.gz
	rotateLog(filepath.Join(dir, "shell.log"), today, 30)
	// Rotate audit.log → audit.log.2026-04-03.gz
	rotateLog(filepath.Join(dir, "audit.log"), today, 90)
	// Rotate history_ts → keep as-is but trim to last 50k lines
	trimFile(filepath.Join(dir, "history_ts"), 50000)
	// Clean old compressed logs
	cleanOldGzips(dir, 90)
}

// rotateLog moves content older than today into a dated gzip file.
func rotateLog(path string, today string, maxDays int) {
	info, err := os.Stat(path)
	if err != nil || info.Size() < 1024 { // don't bother with tiny files
		return
	}

	// If last modified before today, compress the whole file
	if info.ModTime().Format("2006-01-02") < today {
		gzPath := fmt.Sprintf("%s.%s.gz", path, info.ModTime().Format("2006-01-02"))
		if fileExists(gzPath) {
			return // already rotated
		}
		if compressFile(path, gzPath) {
			os.Truncate(path, 0) // clear original
		}
	}
}

func compressFile(src, dst string) bool {
	in, err := os.Open(src)
	if err != nil {
		return false
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return false
	}
	defer out.Close()

	gz := gzip.NewWriter(out)
	gz.Name = filepath.Base(src)
	gz.ModTime = time.Now()
	defer gz.Close()

	_, err = io.Copy(gz, in)
	return err == nil
}

// trimFile keeps only the last N lines of a file.
func trimFile(path string, maxLines int) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) <= maxLines {
		return
	}
	kept := lines[len(lines)-maxLines:]
	os.WriteFile(path, []byte(strings.Join(kept, "\n")), 0600)
}

// cleanOldGzips removes .gz files older than maxDays.
func cleanOldGzips(dir string, maxDays int) {
	cutoff := time.Now().AddDate(0, 0, -maxDays)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".gz") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
}

// DiskUsage returns total size of ~/.kish/ in bytes and a formatted summary.
func DiskUsage() string {
	dir := kishDir()
	var total int64
	var details []struct {
		name string
		size int64
	}

	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		total += info.Size()
		details = append(details, struct {
			name string
			size int64
		}{rel, info.Size()})
		return nil
	})

	sort.Slice(details, func(i, j int) bool {
		return details[i].size > details[j].size
	})

	var lines []string
	lines = append(lines, fmt.Sprintf("~/.kish/ total: %s", humanSize(total)))
	for _, d := range details {
		if d.size > 1024 { // only show files > 1KB
			lines = append(lines, fmt.Sprintf("  %-30s %s", d.name, humanSize(d.size)))
		}
	}
	return strings.Join(lines, "\n")
}

func humanSize(b int64) string {
	switch {
	case b >= 1024*1024*1024:
		return fmt.Sprintf("%.1f GB", float64(b)/(1024*1024*1024))
	case b >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
	case b >= 1024:
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	default:
		return fmt.Sprintf("%d B", b)
	}
}
