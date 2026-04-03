// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cuber-it/kish-sh/v3/interp"
)

// foregroundPID tracks the currently running foreground process group.
var foregroundPID atomic.Int32

// shellPGID is the shell's own process group (for terminal control)
var shellPGID int

func initJobControl() {
	// Get shell's process group
	shellPGID = syscall.Getpgrp()

	// Start SIGCHLD handler for background job notifications
	sigchld := make(chan os.Signal, 10)
	signal.Notify(sigchld, syscall.SIGCHLD)
	go func() {
		for range sigchld {
			jobTable.UpdateStatus()
			jobTable.CleanDone()
		}
	}()
}

// giveTerminal gives terminal control to a process group
func giveTerminal(pgid int) {
	// Best-effort: tcsetpgrp via ioctl
	// This may fail if not a real terminal — that's OK
	syscall.Syscall(syscall.SYS_IOCTL,
		uintptr(syscall.Stdin),
		uintptr(syscall.TIOCSPGRP),
		uintptr(unsafePointer(&pgid)))
}

// takeTerminal gives terminal control back to the shell
func takeTerminal() {
	if shellPGID > 0 {
		giveTerminal(shellPGID)
	}
}

// unsafePointer converts an *int to an unsafe.Pointer for ioctl
// Inlined to avoid importing unsafe in multiple files
func unsafePointer(p *int) uintptr {
	return uintptr(*p)
}

// jobControlMiddleware wraps the exec handler for full job control:
// 1. Process groups (setpgid) for each command
// 2. Terminal control (tcsetpgrp) for foreground jobs
// 3. SIGTSTP detection (Ctrl+Z → stopped)
// 4. Foreground PID tracking for signal forwarding
func jobControlMiddleware(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(ctx context.Context, args []string) error {
		hc := interp.HandlerCtx(ctx)
		path, err := interp.LookPathDir(hc.Dir, hc.Env, args[0])
		if err != nil {
			fmt.Fprintln(hc.Stderr, err)
			return interp.ExitStatus(127)
		}

		cmd := exec.Cmd{
			Path:   path,
			Args:   args,
			Env:    execEnv(hc.Env),
			Dir:    hc.Dir,
			Stdin:  hc.Stdin,
			Stdout: hc.Stdout,
			Stderr: hc.Stderr,
			SysProcAttr: &syscall.SysProcAttr{
				Setpgid: true,
			},
		}

		err = cmd.Start()
		if err != nil {
			fmt.Fprintln(hc.Stderr, err)
			return interp.ExitStatus(127)
		}

		pid := int32(cmd.Process.Pid)
		foregroundPID.Store(pid)
		defer func() {
			foregroundPID.Store(0)
			takeTerminal() // always reclaim terminal
		}()

		// Give terminal to child process group
		giveTerminal(int(pid))

		// Context cancellation handler
		stopf := context.AfterFunc(ctx, func() {
			_ = cmd.Process.Signal(os.Interrupt)
			time.Sleep(2 * time.Second)
			_ = cmd.Process.Signal(os.Kill)
		})
		defer stopf()

		err = cmd.Wait()

		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
					if status.Stopped() {
						cmdStr := strings.Join(args, " ")
						jobID := jobTable.Add(int(pid), cmdStr, JobStopped, cmd.Process)
						fmt.Fprintf(os.Stderr, "\n[%d]  Stopped\t\t%s\n", jobID, cmdStr)
						return interp.ExitStatus(128 + int(status.StopSignal()))
					}
					if status.Signaled() {
						return interp.ExitStatus(128 + int(status.Signal()))
					}
					return interp.ExitStatus(uint8(status.ExitStatus()))
				}
			}
			if exitErr, ok := err.(*exec.ExitError); ok {
				return interp.ExitStatus(uint8(exitErr.ExitCode()))
			}
			return err
		}
		return nil
	}
}
