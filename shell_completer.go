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

type kishCompleter struct {
	pathCommands []string
	pathCacheDir string
	specs        map[string]*CompletionSpec
}

type CompletionSpec struct {
	Subcommands []string              `yaml:"subcommands"`
	Args        map[string]interface{} `yaml:"args"`
}

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
		cmdWord := words[0]
		if spec, ok := c.specs[cmdWord]; ok {
			candidates = append(candidates, c.completeFromSpec(spec, words[1:], prefix)...)
		}

		if cmdWord == "cd" {
			return c.buildResult(completeDirectories(prefix), prefix)
		}

		if strings.HasPrefix(prefix, "$") {
			candidates = append(candidates, completeVariables(prefix)...)
		}

		if cmdWord == "erinnere" || cmdWord == "ki:search" {
			candidates = append(candidates, completeMemory(prefix)...)
		}
	}

	fileCandidates := completeFiles(prefix)
	candidates = append(candidates, fileCandidates...)

	return c.buildResult(candidates, prefix)
}

func (c *kishCompleter) completeFromSpec(spec *CompletionSpec, args []string, prefix string) []string {
	if len(args) == 0 {
		var result []string
		for _, sc := range spec.Subcommands {
			if strings.HasPrefix(sc, prefix) {
				result = append(result, sc)
			}
		}
		return result
	}

	subCmd := args[0]
	if argSpec, ok := spec.Args[subCmd]; ok {
		switch val := argSpec.(type) {
		case string:
			return execCompletionCommand(val, prefix)
		case map[string]interface{}:
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

	if defSpec, ok := spec.Args["_default"]; ok {
		if cmd, ok := defSpec.(string); ok {
			return execCompletionCommand(cmd, prefix)
		}
	}

	return nil
}

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
