// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import "testing"

func TestSignificantWords(t *testing.T) {
	got := significantWords("Zeig mir die groessten Dateien!")
	want := map[string]bool{"groessten": true, "dateien": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want keys %v", got, want)
	}
	for _, w := range got {
		if !want[w] {
			t.Errorf("unexpected significant word %q", w)
		}
	}
}

func withSkills(t *testing.T, skills ...Skill) {
	t.Helper()
	prev := loadedSkills
	loadedSkills = skills
	t.Cleanup(func() { loadedSkills = prev })
}

func TestMatchSkillForQuery_Match(t *testing.T) {
	withSkills(t, Skill{Name: "bigfiles", Description: "zeig die groessten dateien", Script: "du -ah"})
	if s := matchSkillForQuery("zeig mir die groessten dateien"); s == nil || s.Name != "bigfiles" {
		t.Fatalf("expected match on bigfiles, got %v", s)
	}
}

func TestMatchSkillForQuery_NoMatch(t *testing.T) {
	withSkills(t, Skill{Name: "bigfiles", Description: "zeig die groessten dateien", Script: "du -ah"})
	if s := matchSkillForQuery("wie spät ist es"); s != nil {
		t.Errorf("unrelated query must not match, got %v", s)
	}
}

func TestMatchSkillForQuery_TooWeak(t *testing.T) {
	// Only one shared significant word → below the 2-hit floor.
	withSkills(t, Skill{Name: "bigfiles", Description: "zeig die groessten dateien", Script: "du -ah"})
	if s := matchSkillForQuery("zeig die uhrzeit dateien"); s != nil {
		// "dateien" matches but "groessten" does not → 1 hit only
		if s != nil {
			t.Errorf("single-word overlap must not match, got %v", s)
		}
	}
}

func TestMatchSkillForQuery_Ambiguous(t *testing.T) {
	// Two skills with identical keywords → ambiguous → no match (use model).
	withSkills(t,
		Skill{Name: "a", Description: "groessten dateien anzeigen", Script: "x"},
		Skill{Name: "b", Description: "groessten dateien loeschen", Script: "y"},
	)
	if s := matchSkillForQuery("groessten dateien"); s != nil {
		t.Errorf("ambiguous match must fall through to model, got %v", s)
	}
}

func TestMatchSkillForQuery_NoSkills(t *testing.T) {
	withSkills(t)
	if s := matchSkillForQuery("irgendwas"); s != nil {
		t.Errorf("no skills → no match, got %v", s)
	}
}

func TestParseSkillChoice(t *testing.T) {
	cases := map[string]skillMatchChoice{
		"j": skillRun, "ja\n": skillRun, "y": skillRun, "": skillRun, "  \n": skillRun,
		"k": skillToKI, "ki\n": skillToKI, "KI": skillToKI,
		"n": skillCancel, "nein": skillCancel, "x": skillCancel,
	}
	for in, want := range cases {
		if got := parseSkillChoice(in); got != want {
			t.Errorf("parseSkillChoice(%q) = %v, want %v", in, got, want)
		}
	}
}
