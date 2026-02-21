package resources_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/goleak"

	"github.com/kkloberdanz/teleworker/testutil"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestCreateAndCleanupCgroup(t *testing.T) {
	mgr := testutil.RequireManager(t)

	cg, err := mgr.CreateCgroup("test-job-1")
	if err != nil {
		t.Fatalf("CreateCgroup failed: %v", err)
	}

	cgPath := filepath.Join(mgr.ParentPath(), "test-job-1")
	if _, err := os.Stat(cgPath); err != nil {
		t.Fatalf("cgroup directory does not exist: %v", err)
	}

	if err := cg.Cleanup(); err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	if _, err := os.Stat(cgPath); !os.IsNotExist(err) {
		t.Fatalf("cgroup directory still exists after cleanup")
	}
}

func TestResourceLimitsWritten(t *testing.T) {
	mgr := testutil.RequireManager(t)

	cg, err := mgr.CreateCgroup("test-job-2")
	if err != nil {
		t.Fatalf("CreateCgroup failed: %v", err)
	}
	t.Cleanup(func() { cg.Cleanup() })

	cgPath := filepath.Join(mgr.ParentPath(), "test-job-2")

	cpuMax, err := os.ReadFile(filepath.Join(cgPath, "cpu.max"))
	if err != nil {
		t.Fatalf("failed to read cpu.max: %v", err)
	}
	if got := strings.TrimSpace(string(cpuMax)); got != "100000 100000" {
		t.Fatalf("expected cpu.max = %q, got %q", "100000 100000", got)
	}

	memMax, err := os.ReadFile(filepath.Join(cgPath, "memory.max"))
	if err != nil {
		t.Fatalf("failed to read memory.max: %v", err)
	}
	if got := strings.TrimSpace(string(memMax)); got != "524288000" {
		t.Fatalf("expected memory.max = %q, got %q", "524288000", got)
	}
}

func TestKillCgroup(t *testing.T) {
	mgr := testutil.RequireManager(t)

	cg, err := mgr.CreateCgroup("test-job-3")
	if err != nil {
		t.Fatalf("CreateCgroup failed: %v", err)
	}
	t.Cleanup(func() { cg.Cleanup() })

	// Kill should succeed even with no processes in the cgroup.
	if err := cg.Kill(); err != nil {
		t.Fatalf("Kill failed: %v", err)
	}
}
