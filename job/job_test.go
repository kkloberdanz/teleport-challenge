package job

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestNewJobLocal(t *testing.T) {
	j, err := NewJob(JobTypeLocal, "test-id", "echo", []string{"hello"}, Options{})
	if err != nil {
		t.Fatalf("NewJob failed: %v", err)
	}
	if j.ID() != "test-id" {
		t.Fatalf("expected id %q, got %q", "test-id", j.ID())
	}
	st := j.Status()
	if st.Status != StatusSubmitted {
		t.Fatalf("expected StatusSubmitted, got %v", st.Status)
	}
	if st.ExitCode != nil {
		t.Fatalf("expected nil exit code, got %v", *st.ExitCode)
	}
}

func TestStartCalledTwice(t *testing.T) {
	j, err := NewJob(JobTypeLocal, "test-id", "echo", []string{"hello"}, Options{})
	if err != nil {
		t.Fatalf("NewJob failed: %v", err)
	}
	if err := j.Start(); err != nil {
		t.Fatalf("first Start failed: %v", err)
	}
	// Second Start should fail.
	if err := j.Start(); err == nil {
		t.Fatal("expected error on second Start, got nil")
	}
	// Let the process finish so we don't leak a goroutine.
	j.Wait()
}

func TestNewJobUnknownType(t *testing.T) {
	_, err := NewJob(JobType(999), "test-id", "echo", nil, Options{})
	if err == nil {
		t.Fatal("expected error for unknown job type, got nil")
	}
}
