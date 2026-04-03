// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/cuber-it/ki-shell/kish-sh/v3/interp"
)

// kiExecMiddleware intercepts the KI prefix command in pipes.
// Allows: cat log | @ki "summarize"
// Also keeps "ki" as alias for backwards compatibility.
func kiExecMiddleware(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(ctx context.Context, args []string) error {
		// Match the configured prefix (without @) or "ki"
		cmdName := args[0]
		prefixName := strings.TrimPrefix(kiPrefix, "@")
		if cmdName != prefixName && cmdName != "@"+prefixName && cmdName != "ki" {
			return next(ctx, args)
		}

		hc := interp.HandlerCtx(ctx)

		// Build query from args
		query := strings.Join(args[1:], " ")

		// Read piped stdin if available
		var pipeInput string
		if hc.Stdin != nil {
			data, err := io.ReadAll(hc.Stdin)
			if err == nil && len(data) > 0 {
				pipeInput = string(data)
			}
		}

		// Combine pipe input with query
		fullQuery := query
		if pipeInput != "" {
			if query != "" {
				fullQuery = fmt.Sprintf("%s\n\nInput:\n%s", query, truncateLines(pipeInput, 200))
			} else {
				fullQuery = fmt.Sprintf("Analysiere:\n%s", truncateLines(pipeInput, 200))
			}
		}

		if fullQuery == "" {
			fmt.Fprintln(hc.Stderr, "ki: no query provided")
			return interp.ExitStatus(1)
		}

		rawCtx := shellContext.Collect()
		filteredCtx := kiPermissions.FilterContext(rawCtx)
		_, err := kiEngine.Query(ctx, fullQuery, filteredCtx, hc.Stdout)
		if err != nil {
			fmt.Fprintf(hc.Stderr, "ki: %s\n", err)
			return interp.ExitStatus(1)
		}
		return nil
	}
}
