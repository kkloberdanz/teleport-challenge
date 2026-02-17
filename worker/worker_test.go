package worker_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/goleak"

	"github.com/kkloberdanz/teleworker/job"
	"github.com/kkloberdanz/teleworker/testutil"
	"github.com/kkloberdanz/teleworker/worker"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// newTestWorker creates a Worker backed by a real cgroup manager.
// Tests are skipped if cgroups are unavailable.
func newTestWorker(t *testing.T) *worker.Worker {
	t.Helper()
	mgr := testutil.RequireManager(t)
	return worker.New(worker.Options{CgroupMgr: mgr})
}

// TODO: Can we make a test if killing teleworker with -9 also kills the child
// procs? This has been tested and verified manually, and a unit test should be
// feasible (perhaps using /proc/self/exe?) but will be a little tricky.

func TestStartJobReturnsUUID(t *testing.T) {
	w := newTestWorker(t)

	jobID, err := w.StartJob(job.JobTypeLocal, "echo", []string{"hello"})
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}
	if _, err := uuid.Parse(jobID); err != nil {
		t.Fatalf("expected valid UUID, got %q: %v", jobID, err)
	}

	// Wait for process to finish so goroutine exits.
	waitForStatus(t, w, jobID, job.StatusSuccess)
}

func TestStartJobBadCommand(t *testing.T) {
	w := newTestWorker(t)

	_, err := w.StartJob(job.JobTypeLocal, "nonexistent-command-that-does-not-exist", nil)
	if err == nil {
		t.Fatal("expected error for bad command, got nil")
	}
}

func TestJobRunsToSuccess(t *testing.T) {
	w := newTestWorker(t)

	jobID, err := w.StartJob(job.JobTypeLocal, "true", nil)
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}

	st := waitForStatus(t, w, jobID, job.StatusSuccess)
	if st != job.StatusSuccess {
		t.Fatalf("expected StatusSuccess, got %v", st)
	}

	result, err := w.GetJobStatus(jobID)
	if err != nil {
		t.Fatalf("GetJobStatus failed: %v", err)
	}
	if result.ExitCode == nil || *result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %v", result.ExitCode)
	}
}

func TestJobRunsToFailed(t *testing.T) {
	w := newTestWorker(t)

	jobID, err := w.StartJob(job.JobTypeLocal, "false", nil)
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}

	st := waitForStatus(t, w, jobID, job.StatusFailed)
	if st != job.StatusFailed {
		t.Fatalf("expected StatusFailed, got %v", st)
	}

	result, err := w.GetJobStatus(jobID)
	if err != nil {
		t.Fatalf("GetJobStatus failed: %v", err)
	}
	if result.ExitCode == nil || *result.ExitCode != 1 {
		t.Fatalf("expected exit code 1, got %v", result.ExitCode)
	}
}

func TestStopRunningJob(t *testing.T) {
	w := newTestWorker(t)

	jobID, err := w.StartJob(job.JobTypeLocal, "sleep", []string{"60"})
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}

	// Verify it's running.
	result, err := w.GetJobStatus(jobID)
	if err != nil {
		t.Fatalf("GetJobStatus failed: %v", err)
	}
	if result.Status != job.StatusRunning {
		t.Fatalf("expected StatusRunning, got %v", result.Status)
	}

	if err := w.StopJob(jobID); err != nil {
		t.Fatalf("StopJob failed: %v", err)
	}

	// Wait for the killed status to settle (waitForJob goroutine needs to finish).
	waitForNonRunning(t, w, jobID)

	result, err = w.GetJobStatus(jobID)
	if err != nil {
		t.Fatalf("GetJobStatus failed: %v", err)
	}
	if result.Status != job.StatusKilled {
		t.Fatalf("expected StatusKilled, got %v", result.Status)
	}
}

func TestStopFinishedJob(t *testing.T) {
	w := newTestWorker(t)

	jobID, err := w.StartJob(job.JobTypeLocal, "true", nil)
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}

	waitForStatus(t, w, jobID, job.StatusSuccess)

	err = w.StopJob(jobID)
	if err == nil {
		t.Fatal("expected error stopping finished job, got nil")
	}
	if !errors.Is(err, job.ErrJobNotRunning) {
		t.Fatalf("expected ErrJobNotRunning, got %v", err)
	}
}

func TestGetStatusNotFound(t *testing.T) {
	w := newTestWorker(t)

	_, err := w.GetJobStatus("nonexistent-job-id")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, worker.ErrJobNotFound) {
		t.Fatalf("expected ErrJobNotFound, got %v", err)
	}
}

func TestStopJobKillsChildProcesses(t *testing.T) {
	if _, err := exec.LookPath("flock"); err != nil {
		t.Skip("skipping: flock not available")
	}

	w := newTestWorker(t)
	tmpDir := t.TempDir()

	// Each child acquires an exclusive flock, writes a ready marker, then
	// sleeps. When the process is killed, the kernel closes its file
	// descriptors, releasing the lock. This lets us verify child processes
	// are dead without relying on PIDs (which don't work across PID
	// namespaces).
	script := fmt.Sprintf(
		`for i in 1 2 3; do flock -x %s/lock_$i sh -c "touch %s/ready_$i; sleep 60" & done; wait`,
		tmpDir, tmpDir,
	)

	jobID, err := w.StartJob(job.JobTypeLocal, "sh", []string{"-c", script})
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}

	// Wait for all 3 children to signal readiness.
	waitForFiles(t, tmpDir, "ready_", 3)

	// Verify locks are held (children are alive).
	for i := 1; i <= 3; i++ {
		lockPath := filepath.Join(tmpDir, fmt.Sprintf("lock_%d", i))
		if tryFlock(lockPath) {
			t.Fatalf("expected lock_%d to be held, but it wasn't", i)
		}
	}

	if err := w.StopJob(jobID); err != nil {
		t.Fatalf("StopJob failed: %v", err)
	}

	waitForStatus(t, w, jobID, job.StatusKilled)

	// Verify all child processes are dead by checking their locks are released.
	testutil.PollUntil(t, "child locks to be released", func() bool {
		for i := 1; i <= 3; i++ {
			if !tryFlock(filepath.Join(tmpDir, fmt.Sprintf("lock_%d", i))) {
				return false
			}
		}
		return true
	})
}

func skipIfNoPython3(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("skipping: python3 not available")
	}
}

func TestCgroupOOMKillsJob(t *testing.T) {
	skipIfNoPython3(t)

	mgr := testutil.RequireManager(t)
	w := worker.New(worker.Options{CgroupMgr: mgr, NoCleanup: true})

	// Allocate 600 MiB, which exceeds the 500 MiB memory limit.
	// The cgroup OOM killer should terminate the process.
	jobID, err := w.StartJob(job.JobTypeLocal, "python3", []string{
		"-c", "x = bytearray(600_000_000); import time; time.sleep(60)",
	})
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}

	waitForStatus(t, w, jobID, job.StatusFailed)

	// Verify the OOM killer was triggered by checking memory.events.

	// If a job is OOM killed in a cgroup, then the memory.events file will
	// look like so:
	//
	// $ cat memory.events
	// low 0
	// high 0
	// max 38
	// oom 1
	// oom_kill 1
	// oom_group_kill 0
	cgPath := filepath.Join(mgr.ParentPath(), jobID)
	data, err := os.ReadFile(filepath.Join(cgPath, "memory.events"))
	if err != nil {
		t.Fatalf("failed to read memory.events: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "oom_kill ") {
			count, _ := strconv.Atoi(strings.TrimPrefix(line, "oom_kill "))
			if count > 0 {
				return
			}
		}
	}
	t.Fatalf("expected oom_kill > 0 in memory.events, got:\n%s", data)
}

// waitForStatus polls until the job reaches the expected status or times out.
func waitForStatus(t *testing.T, w *worker.Worker, jobID string, expected job.Status) job.Status {
	t.Helper()
	var st job.Status
	testutil.PollUntil(t, fmt.Sprintf("status %v", expected), func() bool {
		result, err := w.GetJobStatus(jobID)
		if err != nil {
			t.Fatalf("GetJobStatus failed: %v", err)
		}
		st = result.Status
		return st == expected
	})
	return st
}

// waitForNonRunning polls until the job is no longer running.
func waitForNonRunning(t *testing.T, w *worker.Worker, jobID string) {
	t.Helper()
	testutil.PollUntil(t, "job to stop running", func() bool {
		result, err := w.GetJobStatus(jobID)
		if err != nil {
			t.Fatalf("GetJobStatus failed: %v", err)
		}
		return result.Status != job.StatusRunning
	})
}

// waitForFiles polls until n files with the given prefix (prefix1 … prefixN)
// appear in dir.
func waitForFiles(t *testing.T, dir, prefix string, n int) {
	t.Helper()
	testutil.PollUntil(t, "files to appear", func() bool {
		for i := 1; i <= n; i++ {
			if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf("%s%d", prefix, i))); err != nil {
				return false
			}
		}
		return true
	})
}

// TestConcurrentStartQueryStop exercises concurrent access to the worker by
// starting, polling, and stopping jobs from many goroutines simultaneously.
// Run with -race to detect data races.
func TestConcurrentStartQueryStop(t *testing.T) {
	w := newTestWorker(t)

	const n = 20
	jobIDs := make([]string, n)

	// Start n jobs concurrently.
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := w.StartJob(job.JobTypeLocal, "sleep", []string{"60"})
			if err != nil {
				t.Errorf("StartJob failed: %v", err)
				return
			}
			jobIDs[i] = id
		}()
	}
	wg.Wait()

	// Concurrently query and stop all jobs.
	for _, id := range jobIDs {
		wg.Add(3)
		// Reader 1: poll status.
		go func() {
			defer wg.Done()
			for range 50 {
				w.GetJobStatus(id)
			}
		}()
		// Reader 2: poll status.
		go func() {
			defer wg.Done()
			for range 50 {
				w.GetJobStatus(id)
			}
		}()
		// Writer: stop the job.
		go func() {
			defer wg.Done()
			w.StopJob(id)
		}()
	}
	wg.Wait()

	// Wait for every job to reach a terminal state. Polling ensures the
	// Wait() goroutine has finished (including cgroup cleanup) before
	// t.Cleanup removes the parent cgroup directory.
	for _, id := range jobIDs {
		waitForNonRunning(t, w, id)
	}
}

// tryFlock attempts a non-blocking exclusive flock on path. Returns true if the
// lock was acquired (meaning no other process holds it), false otherwise.
func tryFlock(path string) bool {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return false
	}
	defer f.Close()
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		return false
	}
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return true
}
