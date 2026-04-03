// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"fmt"
	"os"
	"sync"
	"syscall"
)

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

func (jt *JobTable) List() []*Job {
	jt.mu.Lock()
	defer jt.mu.Unlock()
	var result []*Job
	for _, job := range jt.jobs {
		result = append(result, job)
	}
	return result
}

func (jt *JobTable) Get(jobID int) *Job {
	jt.mu.Lock()
	defer jt.mu.Unlock()
	return jt.jobs[jobID]
}

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

func (jt *JobTable) Remove(jobID int) {
	jt.mu.Lock()
	defer jt.mu.Unlock()
	delete(jt.jobs, jobID)
}

func (jt *JobTable) UpdateStatus() {
	jt.mu.Lock()
	defer jt.mu.Unlock()
	for id, job := range jt.jobs {
		if job.Status == JobRunning {
			var ws syscall.WaitStatus
			pid, err := syscall.Wait4(job.PID, &ws, syscall.WNOHANG, nil)
			if err != nil || pid == 0 {
				continue
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

func (jt *JobTable) CleanDone() {
	jt.mu.Lock()
	defer jt.mu.Unlock()
	for id, job := range jt.jobs {
		if job.Status == JobDone {
			delete(jt.jobs, id)
		}
	}
}

func (jt *JobTable) PrintJobs() {
	jt.mu.Lock()
	defer jt.mu.Unlock()
	for _, job := range jt.jobs {
		fmt.Fprintf(os.Stdout, "[%d]  %s\t\t%s\n", job.ID, job.Status, job.Command)
	}
}

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
	var ws syscall.WaitStatus
	_, err := syscall.Wait4(job.PID, &ws, 0, nil)
	jt.Remove(jobID)
	return err
}

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
