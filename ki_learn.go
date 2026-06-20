// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
//
// ki:learn — turn the last successful KI run into a reusable skill.
//
// This closes the learning loop: "build me a script for X" produces a one-off
// result; ki:learn promotes it to a named skill so the n-th time the shell
// reuses it (it is injected into the prompt, and can be run via skill:<name>)
// instead of asking the model to build it again — saving tokens.
//
// Propose-don't-dispose: promotion is an explicit, user-initiated step. The raw
// run is never auto-promoted (a run may have failed, and learning failures would
// teach mistakes). ki:learn is deterministic and makes NO API call.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// firstParagraph returns the text up to the first blank line, trimmed. It strips
// enrichments the agent appends to the user's prompt (e.g. preThink's
// "Voranalyse…" block) so a learned skill's default description stays clean.
func firstParagraph(s string) string {
	if i := strings.Index(s, "\n\n"); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// extractScriptBlock returns the contents of the first fenced code block in text
// (```bash / ```sh / ```shell / ```action / plain ```), trimmed. Empty if none.
func extractScriptBlock(text string) string {
	for _, marker := range []string{"```bash\n", "```sh\n", "```shell\n", "```action\n", "```\n"} {
		if i := strings.Index(text, marker); i >= 0 {
			rest := text[i+len(marker):]
			if end := strings.Index(rest, "```"); end >= 0 {
				return strings.TrimSpace(rest[:end])
			}
		}
	}
	return ""
}

// saveSkill writes a skill to ~/.aish/skills/<name>.yaml. Returns an error the
// caller must handle — never a silent best-effort write.
func saveSkill(skill Skill) error {
	dir := filepath.Join(aishDir(), "skills")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("skills-Verzeichnis: %w", err)
	}
	data, err := yaml.Marshal(skill)
	if err != nil {
		return fmt.Errorf("yaml: %w", err)
	}
	path := filepath.Join(dir, sanitizeFilename(skill.Name)+".yaml")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("schreiben: %w", err)
	}
	return nil
}

// reloadSkills re-reads ~/.aish/skills so a freshly learned skill is available
// immediately (initSkills appends, so reset first to avoid duplicates).
func reloadSkills() {
	loadedSkills = nil
	initSkills()
}

// lastScriptInConversation searches the conversation backwards for the most
// recent turn whose response contains a fenced script, skipping scriptless
// agent-loop follow-ups. Returns the script and the prompt that produced it.
func lastScriptInConversation() (script, prompt string) {
	turns := kiConversation.Recent()
	for i := len(turns) - 1; i >= 0; i-- {
		if s := extractScriptBlock(turns[i].Response); s != "" {
			return s, turns[i].UserInput
		}
	}
	return "", ""
}

// alreadyASkill reports whether a script is already saved as a skill, so the
// learn hint is not shown for something the user already promoted.
func alreadyASkill(script string) bool {
	script = strings.TrimSpace(script)
	for _, s := range loadedSkills {
		if strings.TrimSpace(s.Script) == script {
			return true
		}
	}
	return false
}

// suggestLearn prints a discreet one-line hint when the run that just finished
// produced a fresh, not-yet-learned script — so the user need not remember
// ki:learn. turnsBefore is the conversation length before the run, so only newly
// added turns are considered. No-op outside interactive mode.
func suggestLearn(turnsBefore int) {
	if !isInteractiveMode {
		return
	}
	turns := kiConversation.Recent()
	if turnsBefore < 0 || turnsBefore > len(turns) {
		turnsBefore = 0 // sliding window shifted; fall back to scanning all
	}
	for i := len(turns) - 1; i >= turnsBefore; i-- {
		if s := extractScriptBlock(turns[i].Response); s != "" {
			if alreadyASkill(s) {
				return
			}
			fmt.Fprintln(os.Stderr, "\033[2mTipp: ki:learn <name> merkt dieses Skript als wiederverwendbaren Skill.\033[0m")
			return
		}
	}
}

// handleLearnCmd implements: ki:learn <name> [description].
func handleLearnCmd(fields []string) {
	if len(fields) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: ki:learn <name> [beschreibung]")
		fmt.Fprintln(os.Stderr, "  Merkt das Skript der letzten KI-Antwort als wiederverwendbaren Skill.")
		return
	}
	name := fields[1]
	desc := strings.TrimSpace(strings.Join(fields[2:], " "))

	script, sourcePrompt := lastScriptInConversation()
	if script == "" {
		fmt.Fprintln(os.Stderr, "ki:learn: kein Skript in der letzten KI-Antwort gefunden.")
		fmt.Fprintln(os.Stderr, "  Erst etwas bauen lassen, z.B.: ki bau mir ein skript das ...")
		return
	}
	if desc == "" {
		desc = firstParagraph(sourcePrompt)
	}
	if findSkill(name) != nil {
		fmt.Fprintf(os.Stderr, "ki:learn: Skill '%s' existiert bereits — wird überschrieben.\n", name)
	}

	if err := saveSkill(Skill{Name: name, Description: desc, Script: script}); err != nil {
		fmt.Fprintf(os.Stderr, "ki:learn: %s\n", err)
		return
	}
	reloadSkills()
	fmt.Fprintf(os.Stdout, "Gelernt: skill:%s — %s\n", name, desc)
	fmt.Fprintf(os.Stdout, "Beim nächsten Mal nutzbar mit: skill:%s\n", name)
}
