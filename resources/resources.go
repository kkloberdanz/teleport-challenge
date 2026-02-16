// Package resources provides cgroup v2 resource controls for jobs.
package resources

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

const parentDir = "/sys/fs/cgroup/teleworker"

// Manager is used to create cgroups.
type Manager struct {
	parentPath string
}

// Cgroup represents a single job's cgroup.
type Cgroup struct {
	path string
	fd   int
}

// NewManager creates the parent cgroup directory and enables controllers.
// Returns an error if cgroup v2 is not available or permissions are insufficient.
func NewManager() (*Manager, error) {
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err != nil {
		return nil, fmt.Errorf("cgroup v2 not available: %w", err)
	}

	// Kill any stale processes and remove the directory left over from a
	// previous run (e.g. if teleworker was killed with SIGKILL).
	cleanupStaleDir(parentDir)

	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create parent cgroup: %w", err)
	}

	// We'll be enabling CPU, Memory, and Disk IO controllers.
	if err := os.WriteFile(
		filepath.Join(parentDir, "cgroup.subtree_control"),
		[]byte("+cpu +memory +io"),
		0644,
	); err != nil {
		return nil, fmt.Errorf("failed to enable cgroup controllers: %w", err)
	}

	return &Manager{parentPath: parentDir}, nil
}

// CreateCgroup creates a cgroup for the given job ID, writes resource limits,
// and opens a directory fd for use with SysProcAttr.CgroupFD.
func (m *Manager) CreateCgroup(jobID string) (*Cgroup, error) {
	path := filepath.Join(m.parentPath, jobID)
	if err := os.Mkdir(path, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cgroup directory: %w", err)
	}

	// CPU: 1 core (100ms quota per 100ms period).
	if err := os.WriteFile(filepath.Join(path, "cpu.max"), []byte("100000 100000"), 0644); err != nil {
		if rmErr := os.Remove(path); rmErr != nil {
			slog.Warn(
				"failed to remove cgroup directory",
				"path", path,
				"error", rmErr,
			)
		}
		return nil, fmt.Errorf("failed to set cpu.max: %w", err)
	}

	// Memory: 500 MiB.
	if err := os.WriteFile(filepath.Join(path, "memory.max"), []byte("524288000"), 0644); err != nil {
		if rmErr := os.Remove(path); rmErr != nil {
			slog.Warn(
				"failed to remove cgroup directory",
				"path", path,
				"error", rmErr,
			)
		}
		return nil, fmt.Errorf("failed to set memory.max: %w", err)
	}

	// TODO: I tested setting disk io on my machine, but different disk
	// configurations may have different behavior. I'm going to make this a best
	// effort configuration in case this runs on a machine with a disk
	// configuration that I have not been able to test. If this fails, then I
	// will warn instead of failing to configure io cgroups.
	//
	// IO: 5 MB/s read and write on root filesystem block device.
	ioMax, err := rootIOMax()
	if err != nil {
		slog.Warn(
			"failed to get io.max config",
			"error", err,
		)
	} else if err := os.WriteFile(filepath.Join(path, "io.max"), []byte(ioMax), 0644); err != nil {
		// Setting io.max with an incorrect major:minor configuration results in
		// an error. While this works on my machine, I have not been able to
		// test it on other disk configurations (e.g. RAID). I will not make
		// this a failure condition, but instead log a warning.
		slog.Warn(
			"failed to set io.max",
			"error", err,
		)
	}

	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		if rmErr := os.Remove(path); rmErr != nil {
			slog.Warn(
				"failed to remove cgroup directory",
				"path", path,
				"error", rmErr,
			)
		}
		return nil, fmt.Errorf("failed to open cgroup directory fd: %w", err)
	}

	return &Cgroup{path: path, fd: fd}, nil
}

// FD returns the cgroup directory file descriptor for SysProcAttr.CgroupFD.
func (c *Cgroup) FD() int {
	return c.fd
}

// Kill writes "1" to cgroup.kill, terminating all processes in this cgroup.
func (c *Cgroup) Kill() error {
	return os.WriteFile(filepath.Join(c.path, "cgroup.kill"), []byte("1"), 0644)
}

// Cleanup closes the directory fd and removes the cgroup directory.
func (c *Cgroup) Cleanup() error {
	if err := unix.Close(c.fd); err != nil {
		return fmt.Errorf("failed to close cgroup fd: %w", err)
	}
	return os.Remove(c.path)
}

// cleanupStaleDir kills any processes in child cgroups and removes the
// directory tree. Errors are logged as warnings since this is best-effort.
func cleanupStaleDir(dir string) {
	// Kill all processes in this cgroup and its children.
	if err := os.WriteFile(filepath.Join(dir, "cgroup.kill"), []byte("1"), 0644); err != nil {
		// Directory doesn't exist yet. Nothing to clean up.
		return
	}

	// Remove child cgroup directories, then the parent.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			slog.Warn(
				"failed to remove child cgroup",
				"path", entry.Name(),
				"error", err,
			)
		}
	}
	if err := os.Remove(dir); err != nil {
		slog.Warn(
			"failed to remove parent cgroup",
			"path", dir,
			"error", err,
		)
	}
}

// rootIOMax returns the io.max string for the root filesystem's block device.
//
// TODO: For simplicity, this hard-codes the path to the root directory, and
// finds which device is mapped to that directory. This could be extended to
// allow configuration of which disks have which limits.
func rootIOMax() (string, error) {
	var stat syscall.Stat_t
	if err := syscall.Stat("/", &stat); err != nil {
		return "", err
	}
	major := unix.Major(stat.Dev)
	return fmt.Sprintf("%d:0 rbps=5242880 wbps=5242880", major), nil
}
