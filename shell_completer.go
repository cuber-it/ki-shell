// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// kishCompleter implements readline.AutoCompleter with dynamic, context-aware completions.
// Tool-specific completions are loaded from YAML specs in ~/.kish/completions/
type kishCompleter struct {
	pathCommands []string
	pathCacheDir string
	specs        map[string]*CompletionSpec
}

// CompletionSpec defines completions for a command, loaded from YAML.
type CompletionSpec struct {
	Subcommands []string                      `yaml:"subcommands"`
	Args        map[string]interface{}         `yaml:"args"` // string = shell command, map = nested spec
}

// shellKeywords for tab completion
var shellKeywords = map[string]bool{
	"if": true, "then": true, "else": true, "elif": true, "fi": true,
	"for": true, "while": true, "until": true, "do": true, "done": true,
	"case": true, "esac": true, "in": true,
	"function": true, "select": true, "time": true,
	"cd": true, "pwd": true, "echo": true, "printf": true,
	"export": true, "unset": true, "set": true, "shopt": true,
	"source": true, "eval": true, "exec": true,
	"exit": true, "return": true, "break": true, "continue": true,
	"read": true, "test": true, "alias": true, "unalias": true,
	"type": true, "command": true, "builtin": true, "trap": true, "wait": true,
	"pushd": true, "popd": true, "dirs": true,
	"true": true, "false": true,
	"jobs": true, "fg": true, "bg": true, "disown": true,
}

func newCompleter() *kishCompleter {
	comp := &kishCompleter{
		specs: make(map[string]*CompletionSpec),
	}
	comp.refreshPathCache()
	comp.loadSpecs()
	return comp
}

// loadSpecs reads all YAML completion specs from ~/.kish/completions/
func (c *kishCompleter) loadSpecs() {
	dir := filepath.Join(kishDir(), "completions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var spec CompletionSpec
		if err := yaml.Unmarshal(data, &spec); err != nil {
			continue
		}
		cmdName := strings.TrimSuffix(entry.Name(), ".yaml")
		c.specs[cmdName] = &spec
	}
}

func (c *kishCompleter) Do(line []rune, pos int) (newLine [][]rune, length int) {
	lineStr := string(line[:pos])

	// Find current word
	wordStart := pos
	for wordStart > 0 && line[wordStart-1] != ' ' && line[wordStart-1] != '|' && line[wordStart-1] != ';' && line[wordStart-1] != '&' {
		wordStart--
	}
	for wordStart < pos && line[wordStart] == ' ' {
		wordStart++
	}
	prefix := string(line[wordStart:pos])

	words := strings.Fields(lineStr)
	isFirstWord := len(words) == 0 || (len(words) == 1 && !strings.HasSuffix(lineStr, " "))

	var candidates []string

	if isFirstWord {
		// Complete commands
		if strings.HasPrefix(kiPrefix, prefix) {
			candidates = append(candidates, kiPrefix)
		}
		for kw := range shellKeywords {
			if strings.HasPrefix(kw, prefix) {
				candidates = append(candidates, kw)
			}
		}
		for _, builtin := range []string{"ki:status", "ki:audit", "ki:log", "ki:search", "ki:clear", "merke", "erinnere", "vergiss"} {
			if strings.HasPrefix(builtin, prefix) {
				candidates = append(candidates, builtin)
			}
		}
		for _, cmd := range c.pathCommands {
			if strings.HasPrefix(cmd, prefix) {
				candidates = append(candidates, cmd)
			}
		}
	} else {
		// Sub-completion from YAML specs
		cmdWord := words[0]
		if spec, ok := c.specs[cmdWord]; ok {
			candidates = append(candidates, c.completeFromSpec(spec, words[1:], prefix)...)
		}

		// cd = only directories
		if cmdWord == "cd" {
			return c.buildResult(completeDirectories(prefix), prefix)
		}

		// Variable completion
		if strings.HasPrefix(prefix, "$") {
			candidates = append(candidates, completeVariables(prefix)...)
		}

		// Memory completion for erinnere/ki:search
		if cmdWord == "erinnere" || cmdWord == "ki:search" {
			candidates = append(candidates, completeMemory(prefix)...)
		}
	}

	// File completion as fallback
	fileCandidates := completeFiles(prefix)
	candidates = append(candidates, fileCandidates...)

	return c.buildResult(candidates, prefix)
}

// completeFromSpec generates completions from a YAML spec
func (c *kishCompleter) completeFromSpec(spec *CompletionSpec, args []string, prefix string) []string {
	// No args yet → complete subcommands
	if len(args) == 0 {
		var result []string
		for _, sc := range spec.Subcommands {
			if strings.HasPrefix(sc, prefix) {
				result = append(result, sc)
			}
		}
		return result
	}

	// Has args → check if there's a spec for this subcommand
	subCmd := args[0]
	if argSpec, ok := spec.Args[subCmd]; ok {
		switch val := argSpec.(type) {
		case string:
			// Shell command to execute for dynamic completions
			return execCompletionCommand(val, prefix)
		case map[string]interface{}:
			// Nested spec (e.g. stash → {subcommands: [list, show, ...]})
			if subs, ok := val["subcommands"].([]interface{}); ok {
				var result []string
				for _, sub := range subs {
					s := sub.(string)
					if strings.HasPrefix(s, prefix) {
						result = append(result, s)
					}
				}
				return result
			}
		}
	}

	// Check _default
	if defSpec, ok := spec.Args["_default"]; ok {
		if cmd, ok := defSpec.(string); ok {
			return execCompletionCommand(cmd, prefix)
		}
	}

	return nil
}

// execCompletionCommand runs a shell command and returns its output lines as completions
func execCompletionCommand(shellCmd string, prefix string) []string {
	out, err := exec.Command("sh", "-c", shellCmd).Output()
	if err != nil {
		return nil
	}
	var result []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && strings.HasPrefix(line, prefix) {
			result = append(result, line)
		}
	}
	return result
}

func (c *kishCompleter) buildResult(candidates []string, prefix string) ([][]rune, int) {
	seen := make(map[string]bool)
	var result [][]rune
	for _, cand := range candidates {
		suffix := cand[len(prefix):]
		if suffix == "" || seen[suffix] {
			continue
		}
		seen[suffix] = true
		result = append(result, []rune(suffix))
	}
	return result, len(prefix)
}

func (c *kishCompleter) refreshPathCache() {
	pathEnv := os.Getenv("PATH")
	if pathEnv == c.pathCacheDir {
		return
	}
	c.pathCacheDir = pathEnv
	c.pathCommands = nil
	seen := make(map[string]bool)
	for _, dir := range strings.Split(pathEnv, ":") {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			if !entry.IsDir() && !seen[name] {
				seen[name] = true
				c.pathCommands = append(c.pathCommands, name)
			}
		}
	}
}

// ---------- Static Completions ----------

func completeFiles(prefix string) []string {
	dir := "."
	filePrefix := prefix
	unescaped := strings.ReplaceAll(prefix, "\\ ", " ")
	if unescaped != prefix {
		prefix = unescaped
	}
	if idx := strings.LastIndexByte(prefix, '/'); idx >= 0 {
		dir = prefix[:idx]
		if dir == "" {
			dir = "/"
		}
		filePrefix = prefix[idx+1:]
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var result []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(filePrefix, ".") {
			continue
		}
		if strings.HasPrefix(name, filePrefix) {
			full := name
			if dir != "." {
				full = dir + "/" + name
			}
			full = strings.ReplaceAll(full, " ", "\\ ")
			if entry.IsDir() {
				full += "/"
			}
			result = append(result, full)
		}
	}
	return result
}

func completeDirectories(prefix string) []string {
	all := completeFiles(prefix)
	var dirs []string
	for _, c := range all {
		if strings.HasSuffix(c, "/") {
			dirs = append(dirs, c)
		}
	}
	return dirs
}

func completeVariables(prefix string) []string {
	varPrefix := prefix[1:]
	var result []string
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if strings.HasPrefix(parts[0], varPrefix) {
			result = append(result, "$"+parts[0])
		}
	}
	return result
}

func completeMemory(prefix string) []string {
	if kiMemory == nil {
		return nil
	}
	var result []string
	for _, entry := range kiMemory.AllFacts() {
		if strings.HasPrefix(entry.Key, prefix) {
			result = append(result, entry.Key)
		}
	}
	return result
}
