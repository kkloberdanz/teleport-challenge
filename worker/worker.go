// Package worker manages job execution and lifecycle.
package worker

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"

	"github.com/kkloberdanz/teleworker/auth"
	"github.com/kkloberdanz/teleworker/job"
	"github.com/kkloberdanz/teleworker/output"
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
	jobs      map[string]job.Job       // TODO: This would ideally be stored in a database. Using a Map for simplicity.
	owners    map[string]auth.Identity // Map jobID to owner identity.
	cgroupMgr resources.Manager
	noCleanup bool
}

// Options configures a Worker.
type Options struct {
	CgroupMgr resources.Manager
	NoCleanup bool // If true, skip cgroup cleanup when jobs exit. Used for testing so we can inspect the cgroup directory after a job finishes.
}

// New creates a Worker.
func New(opts Options) *Worker {
	return &Worker{
		jobs:      make(map[string]job.Job),
		owners:    make(map[string]auth.Identity),
		cgroupMgr: opts.CgroupMgr,
		noCleanup: opts.NoCleanup,
	}
}

// trackJob adds the job and its owner to the map so we can track it.
func (w *Worker) trackJob(jobID string, j job.Job, owner auth.Identity) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.jobs[jobID] = j
	w.owners[jobID] = owner
}

// StartJob starts a command and returns the job ID. The owner is recorded for
// authorization checks.
func (w *Worker) StartJob(jobType job.JobType, command string, args []string, owner auth.Identity) (string, error) {
	jobID := uuid.New().String()

	cg, err := w.cgroupMgr.CreateCgroup(jobID)
	if err != nil {
		return "", fmt.Errorf("failed to create cgroup: %w", err)
	}

	j, err := job.NewJob(jobType, jobID, command, args, job.Options{NoCleanup: w.noCleanup, Cgroup: cg})
	if err != nil {
		cg.Cleanup()
		return "", err
	}

	if err := j.Start(); err != nil {
		return "", err
	}

	w.trackJob(jobID, j, owner)

	go j.Wait()

	return jobID, nil
}

// GetJobOwner returns the identity of the job's owner, or ErrJobNotFound.
func (w *Worker) GetJobOwner(jobID string) (auth.Identity, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	owner, ok := w.owners[jobID]
	if !ok {
		return auth.Identity{}, ErrJobNotFound
	}
	return owner, nil
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

// StreamOutput returns a subscriber for the job's combined stdout/stderr.
func (w *Worker) StreamOutput(jobID string) (output.Subscriber, error) {
	j, ok := w.getJob(jobID)
	if !ok {
		return nil, ErrJobNotFound
	}
	return j.Output().Subscribe(), nil
}

// Shutdown closes all job output buffers, unblocking any StreamOutput
// subscribers so that in-flight streaming RPCs can return cleanly during
// graceful shutdown.
func (w *Worker) Shutdown() {
	w.mu.Lock()
	defer w.mu.Unlock()

	for _, j := range w.jobs {
		j.Stop()
		j.Output().Close()
	}
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
