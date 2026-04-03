// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"fmt"
	"sync"
	"time"
)

// RateLimiter tracks KI query frequency and enforces limits.
type RateLimiter struct {
	mu             sync.Mutex
	timestamps     []time.Time
	maxPerMinute   int
	maxPerHour     int
	maxAgentSteps  int
}

func newRateLimiter(perMinute, perHour, agentSteps int) *RateLimiter {
	if perMinute == 0 {
		perMinute = 20
	}
	if perHour == 0 {
		perHour = 200
	}
	if agentSteps == 0 {
		agentSteps = 10
	}
	return &RateLimiter{
		maxPerMinute:  perMinute,
		maxPerHour:    perHour,
		maxAgentSteps: agentSteps,
	}
}

// Allow checks if a query is allowed. Returns (allowed, warning, error).
// warning is set at 80% of limit.
func (rl *RateLimiter) Allow() (bool, string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	rl.cleanup(now)

	minuteCount := rl.countSince(now.Add(-1 * time.Minute))
	hourCount := rl.countSince(now.Add(-1 * time.Hour))

	// Check minute limit
	if minuteCount >= rl.maxPerMinute {
		return false, fmt.Sprintf("Rate-Limit: %d/%d Anfragen pro Minute", minuteCount, rl.maxPerMinute)
	}

	// Check hour limit
	if hourCount >= rl.maxPerHour {
		return false, fmt.Sprintf("Rate-Limit: %d/%d Anfragen pro Stunde", hourCount, rl.maxPerHour)
	}

	// Record this query
	rl.timestamps = append(rl.timestamps, now)

	// Warnings at 80%
	if minuteCount >= rl.maxPerMinute*80/100 {
		return true, fmt.Sprintf("Rate-Limit Warnung: %d/%d pro Minute", minuteCount+1, rl.maxPerMinute)
	}
	if hourCount >= rl.maxPerHour*80/100 {
		return true, fmt.Sprintf("Rate-Limit Warnung: %d/%d pro Stunde", hourCount+1, rl.maxPerHour)
	}

	return true, ""
}

func (rl *RateLimiter) countSince(since time.Time) int {
	count := 0
	for _, ts := range rl.timestamps {
		if ts.After(since) {
			count++
		}
	}
	return count
}

func (rl *RateLimiter) cleanup(now time.Time) {
	cutoff := now.Add(-1 * time.Hour)
	var kept []time.Time
	for _, ts := range rl.timestamps {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	rl.timestamps = kept
}

// MaxAgentSteps returns the configured max steps for agent loops
func (rl *RateLimiter) MaxAgentSteps() int {
	return rl.maxAgentSteps
}
