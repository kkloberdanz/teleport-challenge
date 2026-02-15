// Package job defines job types and provides a factory for constructing them.
package job

import (
	"errors"
	"fmt"

	"github.com/kkloberdanz/teleworker/resources"
)

// ErrJobNotRunning is returned when attempting to stop a non-running job.
var ErrJobNotRunning = errors.New("job not running")

// JobStatus represents the current state of a job.
type JobStatus int

const (
	// Job statuses. A job should never have StatusUnspecified. This would
	// indicate a bug. This was included as the zero value so we can have a
	// mechanism to detect a bug in setting the status, since a status of 0
	// would indicate that an unexpected bug happened.
	StatusUnspecified JobStatus = iota
	StatusSubmitted
	StatusRunning
	StatusSuccess
	StatusFailed
	StatusKilled
)

// JobType identifies the kind of job to run.
type JobType int

const (
	// Currently only local jobs are accepted. This can be extended later to
	// allow launching Docker jobs.
	JobTypeLocal  JobType = 1
	JobTypeDocker JobType = 2
)

// StatusResult holds the status and optional exit code for a job.
type StatusResult struct {
	Status   JobStatus
	ExitCode *int
}

// Job is the interface that all job types must implement.
type Job interface {
	ID() string
	Start() error
	Status() StatusResult
	Stop() error
	Wait()
}

// Options configures job construction.
type Options struct {
	NoCleanup bool              // If true, skip cgroup cleanup when the job exits. This is used for testing purposes.
	Cgroup    *resources.Cgroup // Resource limits for the job. nil if running without cgroups.
}

// NewJob will return a job type that implements the Job interface. Currently,
// only local jobs are supported, but this can be extended to support Docker
// jobs.
func NewJob(jobType JobType, id, command string, args []string, opts Options) (Job, error) {
	switch jobType {
	case JobTypeLocal:
		return &localJob{
			id:        id,
			command:   command,
			args:      args,
			status:    StatusSubmitted,
			cgroup:    opts.Cgroup,
			noCleanup: opts.NoCleanup,
		}, nil
	default:
		return nil, fmt.Errorf("unknown job type: %d", jobType)
	}
}
