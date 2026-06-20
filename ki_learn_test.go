// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestExtractScriptBlock(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"bash", "hier:\n```bash\necho hi\nls\n```\nfertig", "echo hi\nls"},
		{"sh", "```sh\npwd\n```", "pwd"},
		{"shell", "```shell\nwhoami\n```", "whoami"},
		{"action", "```action\nmkdir x\n```", "mkdir x"},
		{"plain", "```\ndate\n```", "date"},
		{"none", "kein code hier", ""},
		{"first-of-two", "```bash\nA\n```\nund\n```bash\nB\n```", "A"},
	}
	for _, c := range cases {
		if got := extractScriptBlock(c.in); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestSaveSkillRoundTrip(t *testing.T) {
	tmp := withTempKishHome(t)
	skill := Skill{Name: "mytool", Description: "does X", Script: "echo X\nls -la"}
	if err := saveSkill(skill); err != nil {
		t.Fatalf("saveSkill: %v", err)
	}
	path := filepath.Join(tmp, "skills", "mytool.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read skill: %v", err)
	}
	var back Skill
	if err := yaml.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Name != skill.Name || back.Script != skill.Script || back.Description != skill.Description {
		t.Errorf("round-trip mismatch: %+v", back)
	}
}

func TestLearnFromConversation(t *testing.T) {
	withTempKishHome(t)
	prev := kiConversation
	kiConversation = newConversationHistory()
	t.Cleanup(func() { kiConversation = prev; loadedSkills = nil })

	kiConversation.Add("bau mir ein skript fuer backups",
		"Klar:\n```bash\ntar czf backup.tgz ~/data\n```\nDas sichert ~/data.")

	loadedSkills = nil
	handleLearnCmd([]string{"ki:learn", "backup"})

	// Skill must be persisted and immediately loaded.
	if s := findSkill("backup"); s == nil {
		t.Fatal("skill 'backup' not learned/loaded")
	} else if s.Script != "tar czf backup.tgz ~/data" {
		t.Errorf("wrong script learned: %q", s.Script)
	} else if s.Description != "bau mir ein skript fuer backups" {
		t.Errorf("description should default to the prompt, got %q", s.Description)
	}
}

func TestLearnDescriptionStripsEnrichment(t *testing.T) {
	withTempKishHome(t)
	prev := kiConversation
	kiConversation = newConversationHistory()
	t.Cleanup(func() { kiConversation = prev; loadedSkills = nil })

	// The prompt carries an appended preThink "Voranalyse" block.
	kiConversation.Add("zeig die groessten dateien\n\nVoranalyse (nutze diese als Grundlage):\ndu -ah",
		"```bash\ndu -ah . | sort -hr | head\n```")
	loadedSkills = nil
	handleLearnCmd([]string{"ki:learn", "big"})

	s := findSkill("big")
	if s == nil {
		t.Fatal("skill not learned")
	}
	if s.Description != "zeig die groessten dateien" {
		t.Errorf("description should be the clean first paragraph, got %q", s.Description)
	}
}

func TestLearnNoScriptFails(t *testing.T) {
	withTempKishHome(t)
	prev := kiConversation
	kiConversation = newConversationHistory()
	t.Cleanup(func() { kiConversation = prev; loadedSkills = nil })

	kiConversation.Add("wie spät ist es", "Es ist 12 Uhr.") // no script
	loadedSkills = nil
	handleLearnCmd([]string{"ki:learn", "nichts"})

	if findSkill("nichts") != nil {
		t.Error("must not learn a skill when the last response has no script")
	}
}

func TestLearnSkipsScriptlessFollowups(t *testing.T) {
	// The script is in an earlier turn; later agent-loop turns have none.
	withTempKishHome(t)
	prev := kiConversation
	kiConversation = newConversationHistory()
	t.Cleanup(func() { kiConversation = prev; loadedSkills = nil })

	kiConversation.Add("bau ein skript", "```bash\necho done\n```")
	kiConversation.Add("Results: ...", "Erledigt, das Skript lief durch.") // no script
	loadedSkills = nil
	handleLearnCmd([]string{"ki:learn", "done"})

	if s := findSkill("done"); s == nil || s.Script != "echo done" {
		t.Errorf("must find the script in the earlier turn, got %+v", s)
	}
}

func TestLearnIsNotKIRequest(t *testing.T) {
	// ki:learn must be classified as a local builtin, not routed to the model.
	if isKIRequest("ki:learn backup") {
		t.Error("ki:learn must NOT be a KI request (would cost tokens)")
	}
	// A normal ki query still is one.
	if !isKIRequest("ki bau mir was") {
		t.Error("ki <query> must remain a KI request")
	}
}
