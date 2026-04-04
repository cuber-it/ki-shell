// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"fmt"
	"sync"
	"time"
)

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

func (rl *RateLimiter) Allow() (bool, string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	rl.cleanup(now)

	minuteCount := rl.countSince(now.Add(-1 * time.Minute))
	hourCount := rl.countSince(now.Add(-1 * time.Hour))

	if minuteCount >= rl.maxPerMinute {
		return false, fmt.Sprintf("rate limit: %d/%d requests per minute", minuteCount, rl.maxPerMinute)
	}
	if hourCount >= rl.maxPerHour {
		return false, fmt.Sprintf("rate limit: %d/%d requests per hour", hourCount, rl.maxPerHour)
	}

	rl.timestamps = append(rl.timestamps, now)

	if minuteCount >= rl.maxPerMinute*80/100 {
		return true, fmt.Sprintf("rate limit warning: %d/%d per minute", minuteCount+1, rl.maxPerMinute)
	}
	if hourCount >= rl.maxPerHour*80/100 {
		return true, fmt.Sprintf("rate limit warning: %d/%d per hour", hourCount+1, rl.maxPerHour)
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
