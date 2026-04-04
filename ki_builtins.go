// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cuber-it/ki-shell/kish-sh/v3/interp"
)

func pageOutput(content string) {
	pager := os.Getenv("PAGER")
	if pager == "" {
		pager = "less"
	}
	cmd := exec.Command(pager)
	cmd.Stdin = strings.NewReader(content)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprint(os.Stdout, content) // fallback: dump directly
	}
}

func kishBuiltinsMiddleware(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(ctx context.Context, args []string) error {
		hc := interp.HandlerCtx(ctx)

		switch args[0] {
		case "merke", "remember", "ki:remember":
			if len(args) < 3 {
				fmt.Fprintln(hc.Stderr, "Usage: merke <key> <value...>")
				return interp.ExitStatus(1)
			}
			key := args[1]
			value := strings.Join(args[2:], " ")
			if err := kiMemory.Store(key, value, "fact", nil); err != nil {
				fmt.Fprintf(hc.Stderr, "kish: memory error: %s\n", err)
				return interp.ExitStatus(1)
			}
			fmt.Fprintf(hc.Stderr, "Remembered: %s\n", key)
			return nil

		case "erinnere", "recall", "ki:recall":
			if len(args) < 2 {
				fmt.Fprintln(hc.Stderr, "Usage: erinnere <query>")
				return interp.ExitStatus(1)
			}
			query := strings.Join(args[1:], " ")
			results := kiMemory.Search(query)
			if len(results) == 0 {
				fmt.Fprintln(hc.Stderr, "No memories found.")
				return interp.ExitStatus(1)
			}
			for _, entry := range results {
				fmt.Fprintf(hc.Stdout, "%s [%s]: %s\n", entry.Key, entry.Category, entry.Value)
			}
			return nil

		case "vergiss", "forget", "ki:forget":
			if len(args) < 2 {
				fmt.Fprintln(hc.Stderr, "Usage: vergiss <key>")
				return interp.ExitStatus(1)
			}
			key := args[1]
			for _, cat := range []string{"fact", "session", "scratch"} {
				path := filepath.Join(kishDir(), "vault", cat, sanitizeFilename(key)+".yaml")
				os.Remove(path)
			}
			fmt.Fprintf(hc.Stderr, "Forgotten: %s\n", key)
			return nil

		case "showmemory", "ki:memory":
			filter := ""
			if len(args) > 1 {
				filter = args[1]
			}
			var buf bytes.Buffer
			if filter == "" || filter == "facts" {
				facts := kiMemory.AllFacts()
				if len(facts) > 0 {
					fmt.Fprintln(&buf, "=== Facts ===")
					for _, f := range facts {
						tags := ""
						if len(f.Tags) > 0 {
							tags = " #" + strings.Join(f.Tags, " #")
						}
						fmt.Fprintf(&buf, "  %-20s %s%s\n", f.Key, f.Value, tags)
					}
					fmt.Fprintln(&buf)
				}
			}
			if filter == "" || filter == "sessions" {
				sessions := kiMemory.RecentSessions(10)
				if len(sessions) > 0 {
					fmt.Fprintln(&buf, "=== Sessions ===")
					for _, s := range sessions {
						fmt.Fprintf(&buf, "  %s  %s\n", s.Created.Format("2006-01-02 15:04"), s.Value)
					}
					fmt.Fprintln(&buf)
				}
			}
			if filter == "" || filter == "scratch" {
				scratch := kiMemory.listCategory("scratch")
				if len(scratch) > 0 {
					fmt.Fprintln(&buf, "=== Scratch ===")
					for _, s := range scratch {
						fmt.Fprintf(&buf, "  %-20s %s (expires %s)\n", s.Key, s.Value, s.ExpiresAt.Format("2006-01-02"))
					}
					fmt.Fprintln(&buf)
				}
			}
			if filter == "" {
				total := len(kiMemory.AllFacts()) + len(kiMemory.RecentSessions(100)) + len(kiMemory.listCategory("scratch"))
				fmt.Fprintf(&buf, "%d entries in ~/.kish/vault/\n", total)
			}
			pageOutput(buf.String())
			return nil

		case "showlogs", "ki:showlogs":
			filter := ""
			n := 20
			for _, arg := range args[1:] {
				switch arg {
				case "shell", "audit", "conversation":
					filter = arg
				default:
					fmt.Sscanf(arg, "%d", &n)
				}
			}
			var buf bytes.Buffer
			if filter == "" || filter == "shell" {
				fmt.Fprintln(&buf, "=== Shell Log ===")
				if shellLog != nil {
					for _, entry := range shellLog.Recent(n) {
						fmt.Fprintln(&buf, entry)
						fmt.Fprintln(&buf)
					}
				}
			}
			if filter == "" || filter == "audit" {
				fmt.Fprintln(&buf, "=== Audit Log ===")
				if audit != nil {
					data, _ := os.ReadFile(audit.filePath)
					lines := strings.Split(strings.TrimSpace(string(data)), "\n")
					start := 0
					if len(lines) > n {
						start = len(lines) - n
					}
					for _, line := range lines[start:] {
						if line != "" {
							fmt.Fprintln(&buf, line)
						}
					}
				}
				fmt.Fprintln(&buf)
			}
			if filter == "" || filter == "conversation" {
				fmt.Fprintln(&buf, "=== Conversation ===")
				for _, turn := range kiConversation.Recent() {
					fmt.Fprintf(&buf, "  User: %s\n", truncateLines(turn.UserInput, 3))
					fmt.Fprintf(&buf, "  KI:   %s\n\n", truncateLines(turn.Response, 5))
				}
			}
			pageOutput(buf.String())
			return nil

		case "ki:disk":
			fmt.Fprintln(hc.Stdout, DiskUsage())
			return nil

		case "ki:clear":
			kiConversation.Clear()
			fmt.Fprintln(hc.Stderr, "Conversation cleared.")
			return nil

		case "ki:log":
			n := 20
			if len(args) > 1 {
				fmt.Sscanf(args[1], "%d", &n)
			}
			if shellLog != nil {
				entries := shellLog.Recent(n)
				for _, entry := range entries {
					fmt.Fprintln(hc.Stdout, entry)
					fmt.Fprintln(hc.Stdout)
				}
			}
			return nil

		case "ki:search":
			if len(args) < 2 {
				fmt.Fprintln(hc.Stderr, "Usage: ki:search <query>")
				return interp.ExitStatus(1)
			}
			query := strings.Join(args[1:], " ")
			if shellLog != nil {
				entries := shellLog.Search(query, 10)
				if len(entries) == 0 {
					fmt.Fprintln(hc.Stderr, "No matches found.")
				}
				for _, entry := range entries {
					fmt.Fprintln(hc.Stdout, entry)
					fmt.Fprintln(hc.Stdout)
				}
			}
			return nil

		case "ki:costs":
			if pe, ok := kiEngine.(*ProviderEngine); ok {
				today := pe.TodayStats()
				reqs, tokIn, tokOut, totalCost := pe.TotalStats()
				fmt.Fprintf(hc.Stdout, "Provider:  %s\n", kiEngine.Name())
				if today != nil {
					fmt.Fprintf(hc.Stdout, "\nHeute:\n")
					fmt.Fprintf(hc.Stdout, "  Requests:  %d\n", today.Requests)
					fmt.Fprintf(hc.Stdout, "  Tokens:    %d in / %d out\n", today.InputTokens, today.OutputTokens)
					fmt.Fprintf(hc.Stdout, "  Kosten:    $%.4f\n", today.Cost)
					fmt.Fprintf(hc.Stdout, "  Latenz:    %.0fms avg\n", today.AvgLatency)
				}
				fmt.Fprintf(hc.Stdout, "\nGesamt:\n")
				fmt.Fprintf(hc.Stdout, "  Requests:  %d\n", reqs)
				fmt.Fprintf(hc.Stdout, "  Tokens:    %d in / %d out\n", tokIn, tokOut)
				fmt.Fprintf(hc.Stdout, "  Kosten:    $%.4f\n", totalCost)

				recent := pe.RecentRequests(5)
				if len(recent) > 0 {
					fmt.Fprintf(hc.Stdout, "\nLetzte Requests:\n")
					for _, r := range recent {
						fmt.Fprintf(hc.Stdout, "  %s  %s  %d/%d tok  %dms  $%.4f\n",
							r["timestamp"], r["model"],
							r["input_tokens"], r["output_tokens"],
							r["latency_ms"], r["cost_usd"])
					}
				}
			} else {
				fmt.Fprintln(hc.Stderr, "Cost tracking requires heinzel provider")
			}
			return nil

		case "ki:variant":
			if len(args) < 2 {
				fmt.Fprintln(hc.Stdout, ListVariants())
				return nil
			}
			if err := SwitchVariant(args[1]); err != nil {
				fmt.Fprintf(hc.Stderr, "kish: %s\n", err)
				return interp.ExitStatus(1)
			}
			fmt.Fprintf(hc.Stderr, "Prompt variant switched to: %s\n", args[1])
			return nil

		case "ki:prompt":
			sctx := shellContext.Collect()
			filteredCtx := kiPermissions.FilterContext(sctx)
			prompt := buildSystemPrompt(filteredCtx, kiMemory, "")
			fmt.Fprintln(hc.Stdout, prompt)
			return nil

		case "ki:audit":
			n := 20
			if len(args) > 1 {
				fmt.Sscanf(args[1], "%d", &n)
			}
			if audit != nil {
				audit.PrintRecent(n)
			} else {
				fmt.Fprintln(hc.Stderr, "Audit log not initialized")
			}
			return nil

		case "ki:mcp":
			if mcpClient == nil {
				fmt.Fprintln(hc.Stderr, "No MCP servers configured. See ~/.kish/config.yaml")
				return nil
			}
			if len(args) > 1 {
				switch args[1] {
				case "start":
					if len(args) > 2 {
						if err := mcpClient.Start(args[2]); err != nil {
							fmt.Fprintf(hc.Stderr, "kish: %s\n", err)
						}
					}
				case "stop":
					if len(args) > 2 {
						mcpClient.Stop(args[2])
					}
				}
				return nil
			}
			tools := mcpClient.ListTools()
			for _, t := range tools {
				fmt.Fprintln(hc.Stdout, t)
			}
			return nil

		case "ki:status":
			fmt.Fprintf(hc.Stdout, "KI Engine:    %s\n", kiEngine.Name())
			fmt.Fprintf(hc.Stdout, "Available:    %v\n", kiEngine.Available())
			fmt.Fprintf(hc.Stdout, "Memories:     %d facts\n", len(kiMemory.AllFacts()))
			fmt.Fprintf(hc.Stdout, "History:      %d turns\n", len(kiConversation.Recent()))
			fmt.Fprintf(hc.Stdout, "AutoExecute:  %v\n", kiPermissions.AutoExecute)
			fmt.Fprintf(hc.Stdout, "Blocked:      %d patterns\n", len(kiPermissions.BlockedCommands))
			fmt.Fprintf(hc.Stdout, "Destructive:  %d patterns\n", len(kiPermissions.DestructivePatterns))
			fmt.Fprintf(hc.Stdout, "SendOutput:   %v\n", kiPermissions.SendContext.SendCommandOutput)
			fmt.Fprintf(hc.Stdout, "ConfirmQuery: %v\n", kiPermissions.RequireConfirmation)
			sctx := shellContext.Collect()
			fmt.Fprintf(hc.Stdout, "Context:      %s", sctx.Cwd)
			if sctx.GitBranch != "" {
				fmt.Fprintf(hc.Stdout, " [%s]", sctx.GitBranch)
			}
			if sctx.ProjectType != "" {
				fmt.Fprintf(hc.Stdout, " (%s)", sctx.ProjectType)
			}
			fmt.Fprintln(hc.Stdout)
			return nil
		}

		return next(ctx, args)
	}
}
