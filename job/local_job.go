package job

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/kkloberdanz/teleworker/resources"
)

// localJob manages the lifetime of the job, and therefore the job's cgroup.
// Once properly constructed, localJob will be responsible for cleaning up the
// cgroup it was provided.
type localJob struct {
	mu        sync.Mutex        // Guards status and exitCode.
	id        string            // Unique job identifier.
	command   string            // Executable path.
	args      []string          // Command line arguments.
	status    Status            // Current job status.
	exitCode  *int              // Process exit code: `nil` if not yet exited or unknown.
	cmd       *exec.Cmd         // Underlying OS process.
	cgroup    *resources.Cgroup // Resource limits: `nil` if running without cgroups.
	noCleanup bool              // If true, skip cgroup cleanup on exit.
}

// TODO: Ideally we would be running jobs as a different user. For simplicity,
// we will ignore this for now. It would be best to use user namespaces such
// that the job we run does not have permissions to the user that is running the
// teleworker server.

// pidNamespaceFlags returns the Cloneflags and optional UID/GID mappings
// needed to run the child in its own PID namespace.
//
// Note: This looks a little complex, but the rationale is to make it so that if
// the teleworker dies unexpectedly (e.g., gets a SIGKILL), then we want all of
// the jobs to be killed as well. This is setup so that it works regardless if
// the teleworker is running as root or not.
//
// Problem: We need to ensure that if teleworker dies (e.g., if it gets killed
// with a SIGKILL signal), then we want all of its child processes to die.
// We can leverage cgroups to achieve this, but what if cgroups are not
// available? (e.g., if the server is run as a non-root user, then we cannot
// configure cgroups.)
//
// Solution: For the non-cgroups use case, we can launch the child processes in
// a new PID namespace. This way the new process will get launched under a new
// PID 1. If the process with PID 1 dies, then the kernel will sigkill all
// processes that PID owns. Because we launch the child processes with
// Pdeathsig: syscall.SIGKILL, when teleworker dies, it will send a sigkill to
// this child process. Because this child has the PID of 1 in its process
// namespace, the kernel will then SIGKILL any child processes in this
// namespace.
//
// When running as root, `CLONE_NEWPID` alone suffices. Without root, we also
// create a user namespace (`CLONE_NEWUSER`) and map the current UID/GID into
// it so the child retains file-access permissions.
func pidNamespaceFlags() (uintptr, []syscall.SysProcIDMap, []syscall.SysProcIDMap) {
	uid := os.Getuid()
	if uid == 0 {
		return syscall.CLONE_NEWPID, nil, nil
	}
	gid := os.Getgid()
	return syscall.CLONE_NEWPID | syscall.CLONE_NEWUSER,
		// Map the user ID in the namespace (i.e., ContainerID) to the user ID on the host system.
		[]syscall.SysProcIDMap{{ContainerID: uid, HostID: uid, Size: 1}},
		// Map the group ID in the namespace (i.e., ContainerID) to the group ID on the host system.
		[]syscall.SysProcIDMap{{ContainerID: gid, HostID: gid, Size: 1}}
}

func (l *localJob) buildCmd(usePIDNS bool) *exec.Cmd {
	cmd := exec.Command(l.command, l.args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// Launch the job as a process group so that we can send signals to all
		// child processes launched by this job.
		Setpgid: true,
		// If the teleworker process dies, kill the child process.
		Pdeathsig: syscall.SIGKILL,
	}
	// Use a PID namespace so that when the direct child dies (e.g. via
	// Pdeathsig when teleworker exits), all of its descendants are also
	// killed by the kernel. When PID 1 in a PID namespace exits, the
	// kernel sends SIGKILL to every remaining process in that namespace.
	if usePIDNS {
		flags, uidMap, gidMap := pidNamespaceFlags()
		cmd.SysProcAttr.Cloneflags = flags
		cmd.SysProcAttr.UidMappings = uidMap
		cmd.SysProcAttr.GidMappings = gidMap
	}
	if l.cgroup != nil {
		cmd.SysProcAttr.CgroupFD = l.cgroup.FD() // Ensure the process is added to the cgroup when it is created.
		cmd.SysProcAttr.UseCgroupFD = true
	}
	return cmd
}

// Start starts the local process. It transitions the job from StatusSubmitted
// to StatusRunning.
func (l *localJob) Start() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.status != StatusSubmitted {
		return errors.New("job already started")
	}

	// Try to start with a PID namespace. If that fails (e.g. user namespaces
	// are disabled), fall back to starting without one.
	cmd := l.buildCmd(true)
	if err := cmd.Start(); err != nil {
		cmd = l.buildCmd(false)
		if err := cmd.Start(); err != nil {
			if l.cgroup != nil {
				l.cgroup.Cleanup()
			}
			return fmt.Errorf("failed to start command: %w", err)
		}
		slog.Warn("PID namespace unavailable, job descendants may survive if teleworker dies")
	}

	l.cmd = cmd
	l.status = StatusRunning
	return nil
}

// ID returns the unique job identifier.
func (l *localJob) ID() string {
	return l.id
}

// Status returns the current job status and exit code. The exit code is nil
// while the job is still running or if the exit code could not be determined.
func (l *localJob) Status() StatusResult {
	l.mu.Lock()
	defer l.mu.Unlock()

	return StatusResult{Status: l.status, ExitCode: l.exitCode}
}

// Stop kills the job and all of its child processes. Returns ErrJobNotRunning
// if the job has already exited.
func (l *localJob) Stop() error {
	var cgroupErr error

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.status != StatusRunning {
		// This would happen if job.Start() is not called before calling
		// job.Stop()
		return ErrJobNotRunning
	}

	// If cgroups are available, then use the `cgroup.kill` file to terminate
	// jobs. This should be the least error prone way to do this given that
	// this was the recommended approach for service managers such as systemd
	// in the Linux Kernel Mailing list. See: https://lwn.net/Articles/855924/
	//
	// Please see the section `cgroup.kill` in the documentation below for
	// further explanation.
	// See: https://www.kernel.org/doc/html/latest/admin-guide/cgroup-v2.html
	if l.cgroup != nil {
		// This is unlikely to fail, but in case it does, fallback to killing
		// the process group and log a warning.
		if err := l.cgroup.Kill(); err != nil {
			cgroupErr = fmt.Errorf("failed to write to cgroups.kill file: %w", err)
			slog.Warn(
				"failed to kill job using cgroups",
				"error", cgroupErr,
			)
		}
	}

	// If cgroups are not available or if writing to cgroups.kill failed, then
	// fall back to sending SIGKILL to the process group. Notice that we need to
	// send the signal to the negative of the PID in order to send this signal
	// to the group.
	if l.cgroup == nil || cgroupErr != nil {
		// See the following doc for a description of when `kill` can fail:
		// https://pubs.opengroup.org/onlinepubs/9699919799/functions/kill.html
		//
		// From the doc:
		//    The kill() function shall fail if:
		//    [EINVAL]
		//        The value of the sig argument is an invalid or unsupported signal number.
		//    [EPERM]
		//        The process does not have permission to send the signal to any receiving process.
		//    [ESRCH]
		//        No process or process group can be found corresponding to that specified by pid.
		//
		// Note: There is a potential for a race condition. Suppose the process
		// exits before we have a chance to kill it. In that case, we will get
		// the errno: `ESRCH`. If we get `ESRCH`, then we can ignore the failure
		// since it would be due to this race condition.
		if err := syscall.Kill(-l.cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			if cgroupErr != nil {
				err = fmt.Errorf("%w: %w", err, cgroupErr)
			}

			// If this happens, then something is seriously wrong. It would
			// probably be best to reboot the host this is running on.
			return fmt.Errorf("failed to kill process group: %w", err)
		}
	}
	l.status = StatusKilled
	ec := 128 + int(syscall.SIGKILL)
	l.exitCode = &ec

	return nil
}

// Wait blocks until the process exits, then updates the job status and exit
// code and cleans up cgroup resources. This function invokes Cmd.Wait,
// therefore it can only be called once.
func (l *localJob) Wait() {
	err := l.cmd.Wait()

	l.mu.Lock()
	defer l.mu.Unlock()

	defer func() {
		if l.cgroup != nil && !l.noCleanup {
			l.cgroup.Cleanup()
		}
	}()

	if err != nil {
		// If Stop already set killed, leave status as killed.
		if l.status != StatusKilled {
			l.status = StatusFailed
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// Check if the process was terminated by a signal. If so, then
			// calculate the correct exit code by applying 128 + <signal_number>
			if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
				ec := 128 + int(ws.Signal())
				l.exitCode = &ec
			} else {
				ec := exitErr.ExitCode()
				l.exitCode = &ec
			}
		}
	} else {
		if l.status != StatusKilled {
			l.status = StatusSuccess
		}
		ec := 0
		l.exitCode = &ec
	}
}
