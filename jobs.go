// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"fmt"
	"os"
	"sync"
	"syscall"
)

// Job represents a background or suspended process
type Job struct {
	ID      int
	PID     int
	Command string
	Status  JobStatus
	Process *os.Process
}

type JobStatus int

const (
	JobRunning JobStatus = iota
	JobStopped
	JobDone
)

func (s JobStatus) String() string {
	switch s {
	case JobRunning:
		return "Running"
	case JobStopped:
		return "Stopped"
	case JobDone:
		return "Done"
	default:
		return "Unknown"
	}
}

// JobTable tracks background and suspended jobs
type JobTable struct {
	mu     sync.Mutex
	jobs   map[int]*Job
	nextID int
}

func newJobTable() *JobTable {
	return &JobTable{
		jobs:   make(map[int]*Job),
		nextID: 1,
	}
}

// Add registers a new job and returns its ID
func (jt *JobTable) Add(pid int, command string, status JobStatus, proc *os.Process) int {
	jt.mu.Lock()
	defer jt.mu.Unlock()
	jobID := jt.nextID
	jt.nextID++
	jt.jobs[jobID] = &Job{
		ID:      jobID,
		PID:     pid,
		Command: command,
		Status:  status,
		Process: proc,
	}
	return jobID
}

// List returns all active jobs
func (jt *JobTable) List() []*Job {
	jt.mu.Lock()
	defer jt.mu.Unlock()
	var result []*Job
	for _, job := range jt.jobs {
		result = append(result, job)
	}
	return result
}

// Get returns a job by ID
func (jt *JobTable) Get(jobID int) *Job {
	jt.mu.Lock()
	defer jt.mu.Unlock()
	return jt.jobs[jobID]
}

// Last returns the most recently added job
func (jt *JobTable) Last() *Job {
	jt.mu.Lock()
	defer jt.mu.Unlock()
	var latest *Job
	for _, job := range jt.jobs {
		if latest == nil || job.ID > latest.ID {
			latest = job
		}
	}
	return latest
}

// Remove deletes a job from the table
func (jt *JobTable) Remove(jobID int) {
	jt.mu.Lock()
	defer jt.mu.Unlock()
	delete(jt.jobs, jobID)
}

// UpdateStatus checks if jobs have finished and updates their status
func (jt *JobTable) UpdateStatus() {
	jt.mu.Lock()
	defer jt.mu.Unlock()
	for id, job := range jt.jobs {
		if job.Status == JobRunning {
			var ws syscall.WaitStatus
			pid, err := syscall.Wait4(job.PID, &ws, syscall.WNOHANG, nil)
			if err != nil || pid == 0 {
				continue // still running or error
			}
			if ws.Exited() || ws.Signaled() {
				job.Status = JobDone
				fmt.Fprintf(os.Stderr, "[%d]  Done\t\t%s\n", id, job.Command)
			} else if ws.Stopped() {
				job.Status = JobStopped
			}
		}
	}
}

// CleanDone removes finished jobs from the table
func (jt *JobTable) CleanDone() {
	jt.mu.Lock()
	defer jt.mu.Unlock()
	for id, job := range jt.jobs {
		if job.Status == JobDone {
			delete(jt.jobs, id)
		}
	}
}

// PrintJobs outputs the job table (for the `jobs` builtin)
func (jt *JobTable) PrintJobs() {
	jt.mu.Lock()
	defer jt.mu.Unlock()
	for _, job := range jt.jobs {
		fmt.Fprintf(os.Stdout, "[%d]  %s\t\t%s\n", job.ID, job.Status, job.Command)
	}
}

// ContinueFg brings a job to the foreground (sends SIGCONT)
func (jt *JobTable) ContinueFg(jobID int) error {
	jt.mu.Lock()
	job := jt.jobs[jobID]
	jt.mu.Unlock()
	if job == nil {
		return fmt.Errorf("fg: %%%d: no such job", jobID)
	}
	if job.Status == JobStopped {
		syscall.Kill(job.PID, syscall.SIGCONT)
	}
	// Wait for the process
	var ws syscall.WaitStatus
	_, err := syscall.Wait4(job.PID, &ws, 0, nil)
	jt.Remove(jobID)
	if err != nil {
		return err
	}
	return nil
}

// ContinueBg resumes a stopped job in the background (sends SIGCONT)
func (jt *JobTable) ContinueBg(jobID int) error {
	jt.mu.Lock()
	job := jt.jobs[jobID]
	jt.mu.Unlock()
	if job == nil {
		return fmt.Errorf("bg: %%%d: no such job", jobID)
	}
	if job.Status != JobStopped {
		return fmt.Errorf("bg: job %%%d is not stopped", jobID)
	}
	job.Status = JobRunning
	syscall.Kill(job.PID, syscall.SIGCONT)
	fmt.Fprintf(os.Stderr, "[%d]  %s &\n", jobID, job.Command)
	return nil
}
