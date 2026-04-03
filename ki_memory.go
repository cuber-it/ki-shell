// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Memory provides persistent storage across kish sessions.
// Stored as YAML files in ~/.kish/vault/
//
// Three layers:
// - facts:    Long-term knowledge (user preferences, project info, learned patterns)
// - session:  Session summaries (what was done, when, where)
// - scratch:  Temporary notes (cleared after 7 days)

type Memory struct {
	vaultDir string
}

type MemoryEntry struct {
	Key       string    `yaml:"key"`
	Value     string    `yaml:"value"`
	Category  string    `yaml:"category"` // fact, session, scratch
	Tags      []string  `yaml:"tags,omitempty"`
	Created   time.Time `yaml:"created"`
	Updated   time.Time `yaml:"updated"`
	ExpiresAt time.Time `yaml:"expires_at,omitempty"`
	AccessCnt int       `yaml:"access_count,omitempty"` // how often recalled
}

func newMemory() *Memory {
	dir := filepath.Join(kishDir(), "vault")
	os.MkdirAll(dir, 0755)
	m := &Memory{vaultDir: dir}
	m.cleanExpired()
	return m
}

// cleanExpired removes expired scratch entries
func (m *Memory) cleanExpired() {
	dir := filepath.Join(m.vaultDir, "scratch")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, file := range entries {
		if !strings.HasSuffix(file.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, file.Name()))
		if err != nil {
			continue
		}
		var entry MemoryEntry
		if err := yaml.Unmarshal(data, &entry); err != nil {
			continue
		}
		if !entry.ExpiresAt.IsZero() && time.Now().After(entry.ExpiresAt) {
			os.Remove(filepath.Join(dir, file.Name()))
		}
	}
}

// Store saves a memory entry. Tags can be passed explicitly or extracted from value (#tag syntax).
func (m *Memory) Store(key, value, category string, tags []string) error {
	// Extract #tags from value
	extractedTags, cleanValue := extractTags(value)
	if len(extractedTags) > 0 {
		tags = append(tags, extractedTags...)
		value = cleanValue
	}

	entry := MemoryEntry{
		Key:      key,
		Value:    value,
		Category: category,
		Tags:     tags,
		Created:  time.Now(),
		Updated:  time.Now(),
	}
	if category == "scratch" {
		entry.ExpiresAt = time.Now().Add(7 * 24 * time.Hour)
	}

	data, err := yaml.Marshal(entry)
	if err != nil {
		return err
	}

	filename := sanitizeFilename(key) + ".yaml"
	path := filepath.Join(m.vaultDir, category)
	os.MkdirAll(path, 0755)
	return os.WriteFile(filepath.Join(path, filename), data, 0644)
}

// Recall retrieves a memory entry by key
func (m *Memory) Recall(key string) (*MemoryEntry, error) {
	for _, category := range []string{"fact", "session", "scratch"} {
		filename := sanitizeFilename(key) + ".yaml"
		path := filepath.Join(m.vaultDir, category, filename)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var entry MemoryEntry
		if err := yaml.Unmarshal(data, &entry); err != nil {
			continue
		}
		if !entry.ExpiresAt.IsZero() && time.Now().After(entry.ExpiresAt) {
			os.Remove(path)
			continue
		}
		return &entry, nil
	}
	return nil, fmt.Errorf("memory not found: %s", key)
}

// Search finds all memories matching a query string (searches keys and values)
func (m *Memory) Search(query string) []MemoryEntry {
	query = strings.ToLower(query)
	var results []MemoryEntry

	for _, category := range []string{"fact", "session", "scratch"} {
		dir := filepath.Join(m.vaultDir, category)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, file := range entries {
			if !strings.HasSuffix(file.Name(), ".yaml") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, file.Name()))
			if err != nil {
				continue
			}
			var entry MemoryEntry
			if err := yaml.Unmarshal(data, &entry); err != nil {
				continue
			}
			// Skip expired
			if !entry.ExpiresAt.IsZero() && time.Now().After(entry.ExpiresAt) {
				continue
			}
			// Match
			if strings.Contains(strings.ToLower(entry.Key), query) ||
				strings.Contains(strings.ToLower(entry.Value), query) ||
				matchTags(entry.Tags, query) {
				results = append(results, entry)
			}
		}
	}
	return results
}

// AllFacts returns all fact memories (for KI system prompt context)
func (m *Memory) AllFacts() []MemoryEntry {
	return m.listCategory("fact")
}

// RecentSessions returns the last N session summaries
func (m *Memory) RecentSessions(limit int) []MemoryEntry {
	sessions := m.listCategory("session")
	if len(sessions) > limit {
		sessions = sessions[len(sessions)-limit:]
	}
	return sessions
}

func (m *Memory) listCategory(category string) []MemoryEntry {
	dir := filepath.Join(m.vaultDir, category)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var results []MemoryEntry
	for _, file := range entries {
		if !strings.HasSuffix(file.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, file.Name()))
		if err != nil {
			continue
		}
		var entry MemoryEntry
		if err := yaml.Unmarshal(data, &entry); err != nil {
			continue
		}
		if !entry.ExpiresAt.IsZero() && time.Now().After(entry.ExpiresAt) {
			continue
		}
		results = append(results, entry)
	}
	return results
}

// SaveSessionSummary stores a summary of the current session
func (m *Memory) SaveSessionSummary(summary string, cwd string, commands int) error {
	key := fmt.Sprintf("session_%s", time.Now().Format("2006-01-02_15-04"))
	value := fmt.Sprintf("Verzeichnis: %s\nBefehle: %d\n%s", cwd, commands, summary)
	return m.Store(key, value, "session", []string{"session"})
}

// FormatForPrompt creates a text block of relevant memories for the KI system prompt
func (m *Memory) FormatForPrompt() string {
	var parts []string

	facts := m.AllFacts()
	if len(facts) > 0 {
		parts = append(parts, "WICHTIG — Der User hat dir folgendes beigebracht. Berücksichtige das IMMER:")
		for _, fact := range facts {
			parts = append(parts, fmt.Sprintf("- %s: %s", fact.Key, fact.Value))
		}
	}

	sessions := m.RecentSessions(3)
	if len(sessions) > 0 {
		parts = append(parts, "\nLetzte Sessions:")
		for _, session := range sessions {
			parts = append(parts, fmt.Sprintf("- %s", session.Value))
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
}

// ---------- Helpers ----------

// extractTags pulls #hashtags from a string and returns (tags, clean text)
func extractTags(text string) ([]string, string) {
	var tags []string
	var clean []string
	for _, word := range strings.Fields(text) {
		if strings.HasPrefix(word, "#") && len(word) > 1 {
			tags = append(tags, strings.TrimPrefix(word, "#"))
		} else {
			clean = append(clean, word)
		}
	}
	return tags, strings.Join(clean, " ")
}

// RelevantFacts returns facts matching the current context (cwd, project type, tags)
func (m *Memory) RelevantFacts(query string) []MemoryEntry {
	all := m.AllFacts()
	if query == "" {
		return all
	}
	queryLower := strings.ToLower(query)
	var relevant []MemoryEntry
	for _, entry := range all {
		score := 0
		if strings.Contains(strings.ToLower(entry.Value), queryLower) {
			score += 2
		}
		if strings.Contains(strings.ToLower(entry.Key), queryLower) {
			score += 2
		}
		if matchTags(entry.Tags, queryLower) {
			score += 3
		}
		if score > 0 {
			relevant = append(relevant, entry)
		}
	}
	// If no specific matches, return all (better too much context than too little)
	if len(relevant) == 0 {
		return all
	}
	return relevant
}

func sanitizeFilename(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, ":", "_")
	// Keep only alphanumeric, underscore, dash
	var result strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			result.WriteRune(r)
		}
	}
	s := result.String()
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

func matchTags(tags []string, query string) bool {
	for _, tag := range tags {
		if strings.Contains(strings.ToLower(tag), query) {
			return true
		}
	}
	return false
}
