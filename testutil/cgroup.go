// Package testutil provides shared test helpers.
package testutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/kkloberdanz/teleworker/resources"
)

// SkipIfNoCgroupV2 skips the test if cgroup v2 is not available or the
// process is not running as root.
func SkipIfNoCgroupV2(t *testing.T) {
	t.Helper()
	if os.Getuid() != 0 {
		t.Skip("skipping: requires root")
	}
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err != nil {
		t.Skip("skipping: cgroup v2 not available")
	}
}

// RequireManager skips the test if cgroups are unavailable and returns a
// Manager that uses a unique cgroup directory. The directory is cleaned up
// when the test finishes.
func RequireManager(t *testing.T) resources.Manager {
	t.Helper()
	SkipIfNoCgroupV2(t)

	parentPath := filepath.Join("/sys/fs/cgroup", "teleworker-test-"+uuid.New().String())
	mgr, err := resources.NewManager(parentPath)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	t.Cleanup(mgr.Cleanup)
	return *mgr
}
