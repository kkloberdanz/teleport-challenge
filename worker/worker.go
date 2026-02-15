// Package worker manages job execution and lifecycle.
package worker

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"

	"github.com/kkloberdanz/teleworker/job"
	"github.com/kkloberdanz/teleworker/resources"
)

// ErrJobNotFound is returned when a job ID does not exist.
var ErrJobNotFound = errors.New("job not found")

// Worker manages a set of running jobs.
//
// TODO: Finished jobs are never removed from the map. For a long-running
// server, consider adding a cleanup mechanism to avoid unbounded memory growth.
type Worker struct {
	mu        sync.RWMutex
	jobs      map[string]job.Job // TODO: This would ideally be stored in a database. Using a Map for simplicity.
	cgroupMgr *resources.Manager // nil if cgroups unavailable
	noCleanup bool
}

// Options configures a Worker.
type Options struct {
	CgroupMgr *resources.Manager // nil if cgroups unavailable
	NoCleanup bool               // If true, skip cgroup cleanup when jobs exit. Used for testing so we can inspect the cgroup directory after a job finishes.
}

// New creates a Worker.
func New(opts Options) *Worker {
	return &Worker{
		jobs:      make(map[string]job.Job),
		cgroupMgr: opts.CgroupMgr,
		noCleanup: opts.NoCleanup,
	}
}

// trackJob adds the job to the map so we can track it.
func (w *Worker) trackJob(jobID string, j job.Job) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.jobs[jobID] = j
}

// StartJob starts a command and returns the job ID.
func (w *Worker) StartJob(jobType job.JobType, command string, args []string) (string, error) {
	jobID := uuid.New().String()

	var cg *resources.Cgroup
	if w.cgroupMgr != nil {
		var err error
		cg, err = w.cgroupMgr.CreateCgroup(jobID)
		if err != nil {
			return "", fmt.Errorf("failed to create cgroup: %w", err)
		}
	}

	j, err := job.NewJob(jobType, jobID, command, args, job.Options{NoCleanup: w.noCleanup, Cgroup: cg})
	if err != nil {
		if cg != nil {
			cg.Cleanup()
		}
		return "", err
	}

	if err := j.Start(); err != nil {
		return "", err
	}

	w.trackJob(jobID, j)

	go j.Wait()

	return jobID, nil
}

func (w *Worker) getJob(jobID string) (job.Job, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	j, ok := w.jobs[jobID]
	return j, ok
}

// GetJobStatus returns the status and exit code for a job.
func (w *Worker) GetJobStatus(jobID string) (job.StatusResult, error) {
	j, ok := w.getJob(jobID)
	if !ok {
		return job.StatusResult{}, ErrJobNotFound
	}

	return j.Status(), nil
}

// StopJob kills a running job. Returns ErrJobNotFound or job.ErrJobNotRunning on failure.
func (w *Worker) StopJob(jobID string) error {
	j, ok := w.getJob(jobID)
	if !ok {
		return ErrJobNotFound
	}

	slog.Info(
		"stopping job",
		"jobID", jobID,
	)
	return j.Stop()
}
