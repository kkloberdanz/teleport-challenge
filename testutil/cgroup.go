// Package testutil provides shared test helpers.
package testutil

import (
	"os"
	"testing"
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
