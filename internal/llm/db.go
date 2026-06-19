// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
//
// SQLite-backed usage logging. Pure-Go driver (modernc.org/sqlite), schema
// identical to the previous costs.db so existing databases keep working.
package llm

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// DB handles cost/usage logging to SQLite.
type DB struct {
	db           *sql.DB
	providerName string
}

// NewDB opens or creates the database. dsn is a sqlite DSN, e.g. "file:costs.db".
func NewDB(dsn string, providerName string) (*DB, error) {
	if dsn == "" {
		dsn = "file:data/costs.db"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("db open: %w", err)
	}
	d := &DB{db: db, providerName: providerName}
	d.migrate()
	return d, nil
}

func (d *DB) migrate() {
	d.db.Exec(`CREATE TABLE IF NOT EXISTS usage_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp TEXT NOT NULL,
		provider TEXT NOT NULL,
		model TEXT NOT NULL,
		input_tokens INTEGER NOT NULL DEFAULT 0,
		output_tokens INTEGER NOT NULL DEFAULT 0,
		latency_ms INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'ok',
		error TEXT DEFAULT '',
		session_id TEXT DEFAULT '',
		cost_usd REAL DEFAULT 0
	)`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_usage_timestamp ON usage_log(timestamp)`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_usage_provider ON usage_log(provider)`)
}

// LogUsage records a usage entry.
func (d *DB) LogUsage(model string, inputTokens, outputTokens int, latencyMs int64, status, errMsg, sessionID string, costUSD float64) {
	d.db.Exec(`INSERT INTO usage_log (timestamp, provider, model, input_tokens, output_tokens, latency_ms, status, error, session_id, cost_usd)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		time.Now().UTC().Format(time.RFC3339), d.providerName, model,
		inputTokens, outputTokens, latencyMs, status, errMsg, sessionID, costUSD)
}

// Stats returns total usage summary for this provider.
func (d *DB) Stats() (totalRequests int, totalInputTokens, totalOutputTokens int64, totalCost float64) {
	d.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0), COALESCE(SUM(cost_usd),0) FROM usage_log WHERE provider = ?`, d.providerName).Scan(
		&totalRequests, &totalInputTokens, &totalOutputTokens, &totalCost)
	return
}

// UsageSummary is a structured usage report.
type UsageSummary struct {
	Requests     int     `json:"requests"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	Cost         float64 `json:"cost_usd"`
	AvgLatency   float64 `json:"avg_latency_ms"`
}

// TodayStats returns usage for today (UTC).
func (d *DB) TodayStats() UsageSummary {
	var summary UsageSummary
	today := time.Now().UTC().Format("2006-01-02")
	d.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0), COALESCE(SUM(cost_usd),0), COALESCE(AVG(latency_ms),0) FROM usage_log WHERE provider = ? AND timestamp >= ?`,
		d.providerName, today).Scan(&summary.Requests, &summary.InputTokens, &summary.OutputTokens, &summary.Cost, &summary.AvgLatency)
	return summary
}

// RecentRequests returns the last N requests for this provider.
func (d *DB) RecentRequests(limit int) []map[string]interface{} {
	rows, err := d.db.Query(`SELECT timestamp, model, input_tokens, output_tokens, latency_ms, status, session_id, cost_usd FROM usage_log WHERE provider = ? ORDER BY id DESC LIMIT ?`,
		d.providerName, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []map[string]interface{}
	for rows.Next() {
		var ts, model, status, sessionID string
		var tokIn, tokOut int
		var latency int64
		var cost float64
		rows.Scan(&ts, &model, &tokIn, &tokOut, &latency, &status, &sessionID, &cost)
		result = append(result, map[string]interface{}{
			"timestamp": ts, "model": model, "input_tokens": tokIn, "output_tokens": tokOut,
			"latency_ms": latency, "status": status, "session_id": sessionID, "cost_usd": cost,
		})
	}
	return result
}

// Close closes the database.
func (d *DB) Close() {
	d.db.Close()
}
