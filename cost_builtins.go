// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
//
// Shell control for the cost guard: ki:budget and ki:killswitch.
package main

import (
	"fmt"
	"io"

	"github.com/cuber-it/ki-shell/kish-sh/v3/interp"
)

// budgetLevel classifies the current budget state for the prompt indicator.
type budgetLevel int

const (
	budgetOK    budgetLevel = iota // below 80% — nothing to show
	budgetWarn                     // >= 80% monthly budget — gentle warning
	budgetBlock                    // killswitch on OR a hard limit reached — loud warning
)

// budgetStatus inspects budget.json + current usage and returns the prompt-level
// state. It is intentionally best-effort and side-effect-free: it must never
// block the prompt. On any error reading budget/usage it returns budgetBlock,
// because an unreadable budget means the cost guard is failing closed and the
// next KI call would be refused — the user should see that in the prompt.
func budgetStatus() budgetLevel {
	ov, err := loadBudgetOverrides()
	if err != nil {
		return budgetBlock
	}
	if ov.Killswitch != nil && *ov.Killswitch {
		return budgetBlock
	}

	limits := effectiveLimits(kiConfig, ov)

	pe, ok := kiEngine.(*ProviderEngine)
	if !ok {
		return budgetOK
	}

	// Month (lifetime total per usageAdapter convention) vs monthly USD ceiling.
	_, _, _, monthCost := pe.TotalStats()
	if limits.HardUsdPerMonth > 0 {
		if monthCost >= limits.HardUsdPerMonth {
			return budgetBlock
		}
		if monthCost >= limits.HardUsdPerMonth*0.8 {
			return budgetWarn
		}
	}

	// Daily ceilings can also hard-stop the next call.
	if today := pe.TodayStats(); today != nil {
		if limits.HardUsdPerDay > 0 && today.Cost >= limits.HardUsdPerDay {
			return budgetBlock
		}
		dayTokens := today.InputTokens + today.OutputTokens
		if limits.HardTokensPerDay > 0 && dayTokens >= limits.HardTokensPerDay {
			return budgetBlock
		}
	}

	return budgetOK
}

func budgetPct(used, limit float64) int {
	if limit <= 0 {
		return 0
	}
	pct := int(used * 100 / limit)
	if pct > 100 {
		pct = 100
	}
	return pct
}

func budgetPctInt(used, limit int64) int {
	if limit <= 0 {
		return 0
	}
	pct := int(used * 100 / limit)
	if pct > 100 {
		pct = 100
	}
	return pct
}

func remaining(limit, used float64) float64 {
	rest := limit - used
	if rest < 0 {
		return 0
	}
	return rest
}

// printBudget shows the configured limits plus current consumption vs. budget.
func printBudget(out io.Writer) {
	ov, err := loadBudgetOverrides()
	if err != nil {
		fmt.Fprintf(out, "Budget: WARNUNG — budget.json nicht lesbar: %s\n", err)
		fmt.Fprintln(out, "Fail-closed: KI-Calls werden verweigert bis behoben.")
		return
	}
	limits := effectiveLimits(kiConfig, ov)

	kill := ov.Killswitch != nil && *ov.Killswitch
	fmt.Fprintf(out, "Killswitch:        %s\n", onOff(kill))
	fmt.Fprintf(out, "Monatslimit:       $%.2f\n", limits.HardUsdPerMonth)
	fmt.Fprintf(out, "Tageslimit:        $%.2f\n", limits.HardUsdPerDay)
	fmt.Fprintf(out, "Token-Tageslimit:  %d\n", limits.HardTokensPerDay)
	fmt.Fprintf(out, "Limit pro Run:     $%.2f\n", limits.HardUsdPerRun)
	fmt.Fprintf(out, "Token pro Run:     %d (soft %d, sparmode %d)\n",
		limits.HardTokensPerRun, limits.SoftTokensPerRun, limits.SparmodeTokens)
	fmt.Fprintf(out, "Rueckfrage ab:     $%.2f\n", limits.ConfirmAboveUsd)

	if pe, ok := kiEngine.(*ProviderEngine); ok {
		today := pe.TodayStats()
		_, _, _, totalCost := pe.TotalStats()
		if today != nil {
			fmt.Fprintf(out, "\nVerbrauch heute:   $%.4f (%d%% Tagesbudget, $%.4f verbleibend)\n",
				today.Cost, budgetPct(today.Cost, limits.HardUsdPerDay),
				remaining(limits.HardUsdPerDay, today.Cost))
		}
		fmt.Fprintf(out, "Verbrauch Monat:   $%.4f (%d%% Monatsbudget, $%.4f verbleibend)\n",
			totalCost, budgetPct(totalCost, limits.HardUsdPerMonth),
			remaining(limits.HardUsdPerMonth, totalCost))
	}
}

func onOff(value bool) string {
	if value {
		return "on"
	}
	return "off"
}

// handleBudgetCmd implements: ki:budget [set <key> <value> | confirm <value>].
func handleBudgetCmd(stdout, stderr io.Writer, args []string) error {
	if len(args) == 0 {
		printBudget(stdout)
		return nil
	}

	switch args[0] {
	case "set":
		if len(args) < 3 {
			fmt.Fprintln(stderr, "Usage: ki:budget set month|day|run|tokens-run|tokens-day <wert>")
			return interp.ExitStatus(1)
		}
		return setBudgetValue(stdout, stderr, args[1], args[2])

	case "confirm":
		if len(args) < 2 {
			fmt.Fprintln(stderr, "Usage: ki:budget confirm <wert>")
			return interp.ExitStatus(1)
		}
		return setBudgetValue(stdout, stderr, "confirm", args[1])

	default:
		fmt.Fprintln(stderr, "Usage: ki:budget [set month|day|run|tokens-run|tokens-day <wert> | confirm <wert>]")
		return interp.ExitStatus(1)
	}
}

func setBudgetValue(stdout, stderr io.Writer, key, value string) error {
	ov, err := loadBudgetOverrides()
	if err != nil {
		fmt.Fprintf(stderr, "kish: budget.json nicht lesbar: %s\n", err)
		return interp.ExitStatus(1)
	}

	switch key {
	case "month":
		val, perr := parseBudgetFloat(value)
		if perr != nil {
			fmt.Fprintf(stderr, "kish: ungueltiger Wert: %s\n", value)
			return interp.ExitStatus(1)
		}
		ov.MaxUsdPerMonth = &val
	case "day":
		val, perr := parseBudgetFloat(value)
		if perr != nil {
			fmt.Fprintf(stderr, "kish: ungueltiger Wert: %s\n", value)
			return interp.ExitStatus(1)
		}
		ov.MaxUsdPerDay = &val
	case "run":
		val, perr := parseBudgetFloat(value)
		if perr != nil {
			fmt.Fprintf(stderr, "kish: ungueltiger Wert: %s\n", value)
			return interp.ExitStatus(1)
		}
		ov.MaxUsdPerRun = &val
	case "confirm":
		val, perr := parseBudgetFloat(value)
		if perr != nil {
			fmt.Fprintf(stderr, "kish: ungueltiger Wert: %s\n", value)
			return interp.ExitStatus(1)
		}
		ov.ConfirmAboveUsd = &val
	case "tokens-run":
		val, perr := parseBudgetInt(value)
		if perr != nil {
			fmt.Fprintf(stderr, "kish: ungueltiger Wert: %s\n", value)
			return interp.ExitStatus(1)
		}
		intVal := int(val)
		ov.MaxTokensPerRun = &intVal
		soft := intVal * 80 / 100
		spar := intVal * 90 / 100
		ov.SoftTokensPerRun = &soft
		ov.SparmodeTokens = &spar
	case "tokens-day":
		val, perr := parseBudgetInt(value)
		if perr != nil {
			fmt.Fprintf(stderr, "kish: ungueltiger Wert: %s\n", value)
			return interp.ExitStatus(1)
		}
		ov.MaxTokensPerDay = &val
	default:
		fmt.Fprintf(stderr, "kish: unbekannter Schluessel: %s (month|day|run|tokens-run|tokens-day|confirm)\n", key)
		return interp.ExitStatus(1)
	}

	if err := saveBudgetOverrides(ov); err != nil {
		fmt.Fprintf(stderr, "kish: budget.json schreiben fehlgeschlagen: %s\n", err)
		return interp.ExitStatus(1)
	}
	fmt.Fprintf(stdout, "Budget gesetzt: %s = %s\n", key, value)
	return nil
}

// handleKillswitchCmd implements: ki:killswitch on|off.
func handleKillswitchCmd(stdout, stderr io.Writer, args []string) error {
	if len(args) == 0 {
		ov, err := loadBudgetOverrides()
		if err != nil {
			fmt.Fprintf(stderr, "kish: budget.json nicht lesbar: %s\n", err)
			return interp.ExitStatus(1)
		}
		kill := ov.Killswitch != nil && *ov.Killswitch
		fmt.Fprintf(stdout, "Killswitch: %s\n", onOff(kill))
		return nil
	}

	var newState bool
	switch args[0] {
	case "on":
		newState = true
	case "off":
		newState = false
	default:
		fmt.Fprintln(stderr, "Usage: ki:killswitch on|off")
		return interp.ExitStatus(1)
	}

	ov, err := loadBudgetOverrides()
	if err != nil {
		fmt.Fprintf(stderr, "kish: budget.json nicht lesbar: %s\n", err)
		return interp.ExitStatus(1)
	}
	ov.Killswitch = &newState
	if err := saveBudgetOverrides(ov); err != nil {
		fmt.Fprintf(stderr, "kish: budget.json schreiben fehlgeschlagen: %s\n", err)
		return interp.ExitStatus(1)
	}
	fmt.Fprintf(stdout, "Killswitch: %s\n", onOff(newState))
	return nil
}
