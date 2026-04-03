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
	mu            sync.Mutex
	timestamps    []time.Time
	maxPerMinute  int
	maxPerHour    int
	maxAgentSteps int
}

func newRateLimiter(perMinute, perHour, agentSteps int) *RateLimiter {
	return &RateLimiter{
		maxPerMinute:  perMinute,
		maxPerHour:    perHour,
		maxAgentSteps: agentSteps,
	}
}

// Allow checks if a query is allowed. Warning is set at 80% of limit.
func (rl *RateLimiter) Allow() (bool, string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	rl.cleanup(now)

	minuteCount := rl.countSince(now.Add(-1 * time.Minute))
	hourCount := rl.countSince(now.Add(-1 * time.Hour))

	if minuteCount >= rl.maxPerMinute {
		return false, fmt.Sprintf("Rate-Limit: %d/%d Anfragen pro Minute", minuteCount, rl.maxPerMinute)
	}
	if hourCount >= rl.maxPerHour {
		return false, fmt.Sprintf("Rate-Limit: %d/%d Anfragen pro Stunde", hourCount, rl.maxPerHour)
	}

	rl.timestamps = append(rl.timestamps, now)

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

func (rl *RateLimiter) MaxAgentSteps() int {
	return rl.maxAgentSteps
}
