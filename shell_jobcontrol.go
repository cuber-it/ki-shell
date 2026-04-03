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

	"github.com/cuber-it/ki-shell/kish-sh/v3/interp"
)

var foregroundPID atomic.Int32
var shellPGID int
var isInteractiveMode bool

func initJobControl() {
	shellPGID = syscall.Getpgrp()

	sigchld := make(chan os.Signal, 10)
	signal.Notify(sigchld, syscall.SIGCHLD)
	go func() {
		for range sigchld {
			jobTable.UpdateStatus()
			jobTable.CleanDone()
		}
	}()
}

// jobControlMiddleware wraps the exec handler for job control:
// process groups, terminal control, SIGTSTP detection, foreground PID tracking.
func jobControlMiddleware(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(ctx context.Context, args []string) error {
		hc := interp.HandlerCtx(ctx)
		path, err := interp.LookPathDir(hc.Dir, hc.Env, args[0])
		if err != nil {
			fmt.Fprintln(hc.Stderr, err)
			return interp.ExitStatus(127)
		}

		// Use real file descriptors so interactive programs (vim, htop)
		// detect a proper terminal via isatty().
		stdout := hc.Stdout
		stderr := hc.Stderr
		if tw, ok := stdout.(*TeeWriter); ok && tw.file != nil {
			stdout = tw.file
		}
		if tw, ok := stderr.(*TeeWriter); ok && tw.file != nil {
			stderr = tw.file
		}

		cmd := exec.Cmd{
			Path:   path,
			Args:   args,
			Env:    execEnv(hc.Env),
			Dir:    hc.Dir,
			Stdin:  hc.Stdin,
			Stdout: stdout,
			Stderr: stderr,
		}

		if err = cmd.Start(); err != nil {
			fmt.Fprintln(hc.Stderr, err)
			return interp.ExitStatus(127)
		}

		pid := int32(cmd.Process.Pid)
		foregroundPID.Store(pid)
		defer foregroundPID.Store(0)

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
