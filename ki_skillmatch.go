// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
//
// Local skill matching — the n-th-time, zero-token path.
//
// When a natural-language KI request locally matches a learned skill, aish can
// run the skill's script WITHOUT calling the model. Matching is conservative
// (keyword overlap, must be unambiguous); on any doubt it falls through to the
// model. Per the user's choice it PROPOSES rather than auto-runs: the human
// confirms (j), declines (n), or escalates to the model (k) — propose-don't-
// dispose, the same principle as the cost and learn guards.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"
)

// matchStopwords are command verbs, articles and fillers (DE + EN) that carry no
// skill-identifying signal, so they are ignored when comparing a query to a skill.
var matchStopwords = map[string]bool{
	"der": true, "die": true, "das": true, "ein": true, "eine": true, "einen": true,
	"und": true, "oder": true, "ich": true, "mir": true, "mal": true, "bitte": true,
	"zeig": true, "zeige": true, "gib": true, "mach": true, "bau": true, "baue": true,
	"erstell": true, "erstelle": true, "liste": true, "alle": true, "den": true,
	"the": true, "and": true, "for": true, "give": true, "show": true, "make": true,
	"build": true, "list": true, "create": true, "with": true, "from": true,
}

// significantWords lowercases text, splits on non-alphanumerics, and drops
// stopwords and very short tokens, returning the deduplicated remainder.
func significantWords(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	var out []string
	seen := map[string]bool{}
	for _, w := range fields {
		if len(w) < 3 || matchStopwords[w] || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	return out
}

// matchSkillForQuery returns the single skill that unambiguously matches the
// query, or nil. Conservative: requires at least 2 shared significant words,
// majority coverage of the skill's keywords, and a clear margin over any runner-
// up — otherwise nil (caller then uses the model).
func matchSkillForQuery(query string) *Skill {
	qWords := significantWords(query)
	if len(qWords) == 0 {
		return nil
	}
	qSet := map[string]bool{}
	for _, w := range qWords {
		qSet[w] = true
	}

	var best *Skill
	bestScore, secondScore := 0.0, 0.0
	for i := range loadedSkills {
		s := &loadedSkills[i]
		kw := significantWords(s.Name + " " + s.Description)
		if len(kw) == 0 {
			continue
		}
		hits := 0
		for _, w := range kw {
			if qSet[w] {
				hits++
			}
		}
		score := float64(hits) / float64(len(kw))
		if hits < 2 || score < 0.5 {
			continue // too weak to consider
		}
		if score > bestScore {
			bestScore, secondScore, best = score, bestScore, s
		} else if score > secondScore {
			secondScore = score
		}
	}
	if best != nil && bestScore-secondScore >= 0.25 {
		return best
	}
	return nil // ambiguous or weak → let the model handle it
}

// skillMatchChoice is the user's decision on a proposed skill match.
type skillMatchChoice int

const (
	skillRun    skillMatchChoice = iota // run the skill (no API call)
	skillToKI                           // escalate to the model
	skillCancel                         // do nothing
)

// parseSkillChoice maps a raw input line to a choice. Enter (empty) defaults to
// running, since the user explicitly learned this skill. Pure for testability.
func parseSkillChoice(input string) skillMatchChoice {
	switch strings.TrimSpace(strings.ToLower(input)) {
	case "j", "y", "ja", "yes", "":
		return skillRun
	case "k", "ki":
		return skillToKI
	default:
		return skillCancel
	}
}

// askSkillMatch proposes a matched skill and reads the user's choice. It reads
// os.Stdin via bufio the same way Confirm/ConfirmSimple do (works at the tty;
// the REPL's readline is idle while handleKI runs).
func askSkillMatch(skill *Skill) skillMatchChoice {
	fmt.Fprintf(os.Stderr, "\033[2mPasst zu skill:%s — %s (kein KI-Call).\033[0m\n", skill.Name, skill.Description)
	fmt.Fprint(os.Stderr, "Ausführen? [j=ja / n=nein / k=KI fragen] ")
	input, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return parseSkillChoice(input)
}

// runSkillDirect executes a matched skill's script with zero token cost and
// records it in the action log (audit + learning history).
func runSkillDirect(skill *Skill) {
	fmt.Fprintf(os.Stderr, "\033[2m[skill:%s — kein KI-Call]\033[0m\n", skill.Name)
	stdout, stderr, code := ExecuteAction(context.Background(), skill.Script, 30*time.Second)
	if stdout != "" {
		fmt.Fprint(os.Stdout, stdout)
	}
	if stderr != "" {
		fmt.Fprintf(os.Stderr, "\033[31m%s\033[0m", stderr)
	}
	status := "ok"
	if code != 0 {
		status = "error"
	}
	cwd, _ := os.Getwd()
	logKIAction(KIActionRecord{
		Event:            "skill_run",
		Cwd:              cwd,
		Prompt:           "skill:" + skill.Name,
		SuggestedCommand: skill.Script,
		Status:           status,
	})
}
