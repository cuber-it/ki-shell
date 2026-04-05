// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"fmt"
	"os"
	"os/user"
	"strings"
	"time"
)

var lastExitCode int

func buildPrompt() string {
	ps1 := os.Getenv("KISH_PS1")
	if ps1 == "" {
		return defaultPrompt()
	}
	return expandPS1(ps1)
}

// lightBackground returns true if COLORFGBG or KISH_THEME suggest a light background.
func lightBackground() bool {
	// KISH_THEME set by web UI theme switcher
	switch os.Getenv("KISH_THEME") {
	case "white", "sepia":
		return true
	}
	// Standard terminal hint
	if bg := os.Getenv("COLORFGBG"); bg != "" {
		parts := strings.Split(bg, ";")
		if len(parts) >= 2 && (parts[len(parts)-1] == "15" || parts[len(parts)-1] == "7") {
			return true
		}
	}
	return false
}

func defaultPrompt() string {
	cwd := shortCwd()
	light := lightBackground()

	// Colors: dark bg uses bright colors, light bg uses dark colors
	var cOK, cErr, cName, cGit string
	if light {
		cOK = "\033[32m"      // dark green
		cErr = "\033[31m"     // dark red
		cName = "\033[34m"    // dark blue
		cGit = "\033[35m"     // dark magenta
	} else {
		cOK = "\033[32m"      // green
		cErr = "\033[31m"     // red
		cName = "\033[1;36m"  // bold cyan
		cGit = "\033[1;33m"   // bold yellow
	}
	reset := "\033[0m"

	var exitIndicator string
	if lastExitCode == 0 {
		exitIndicator = cOK + "✓" + reset
	} else {
		exitIndicator = fmt.Sprintf("%s%d%s", cErr, lastExitCode, reset)
	}

	branch := detectGitBranch()
	var gitPart string
	if branch != "" {
		gitPart = fmt.Sprintf(" %s(%s)%s", cGit, branch, reset)
	}

	return fmt.Sprintf("%s %skish%s %s%s $ ", exitIndicator, cName, reset, cwd, gitPart)
}

func expandPS1(ps1 string) string {
	var result strings.Builder
	for i := 0; i < len(ps1); i++ {
		if ps1[i] != '\\' || i+1 >= len(ps1) {
			result.WriteByte(ps1[i])
			continue
		}
		i++
		switch ps1[i] {
		case 'u':
			if usr, err := user.Current(); err == nil {
				result.WriteString(usr.Username)
			}
		case 'h':
			if name, err := os.Hostname(); err == nil {
				if idx := strings.IndexByte(name, '.'); idx >= 0 {
					name = name[:idx]
				}
				result.WriteString(name)
			}
		case 'H':
			if name, err := os.Hostname(); err == nil {
				result.WriteString(name)
			}
		case 'w':
			result.WriteString(shortCwd())
		case 'W':
			cwd, _ := os.Getwd()
			if idx := strings.LastIndexByte(cwd, '/'); idx >= 0 && idx < len(cwd)-1 {
				result.WriteString(cwd[idx+1:])
			} else {
				result.WriteString(cwd)
			}
		case '$':
			if os.Getuid() == 0 {
				result.WriteByte('#')
			} else {
				result.WriteByte('$')
			}
		case 'n':
			result.WriteByte('\n')
		case 't':
			result.WriteString(time.Now().Format("15:04:05"))
		case 'A':
			result.WriteString(time.Now().Format("15:04"))
		case 'd':
			result.WriteString(time.Now().Format("Mon Jan 02"))
		case 'j':
			result.WriteString(fmt.Sprintf("%d", len(jobTable.List())))
		case '?':
			result.WriteString(fmt.Sprintf("%d", lastExitCode))
		case '[':
			result.WriteString("\001")
		case ']':
			result.WriteString("\002")
		case 'e':
			result.WriteByte(0x1b)
		case '\\':
			result.WriteByte('\\')
		default:
			result.WriteByte('\\')
			result.WriteByte(ps1[i])
		}
	}
	return result.String()
}

func buildPS2() string {
	ps2 := os.Getenv("KISH_PS2")
	if ps2 != "" {
		return expandPS1(ps2)
	}
	return "\033[2m...\033[0m "
}

func shortCwd() string {
	cwd, _ := os.Getwd()
	home := os.Getenv("HOME")
	if home != "" && strings.HasPrefix(cwd, home) {
		cwd = "~" + cwd[len(home):]
	}
	return cwd
}
