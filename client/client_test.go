package client_test

import (
	"net"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/goleak"
	"google.golang.org/grpc"

	"github.com/kkloberdanz/teleworker/client"
	pb "github.com/kkloberdanz/teleworker/proto/teleworker/v1"
	"github.com/kkloberdanz/teleworker/server"
	"github.com/kkloberdanz/teleworker/testutil"
	"github.com/kkloberdanz/teleworker/worker"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// Note: There is some redundancy between the server tests and the client tests,
// however, as this project progresses, these redundancies will disappear, as
// they will be testing different aspects of the implementation, i.e., client
// specific functionality vs server specific functionality.

// startTestServer starts a gRPC server and returns its address.
func startTestServer(t *testing.T) string {
	t.Helper()

	listen, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	w := worker.New(worker.Options{})
	srv := server.New(w)

	grpcServer := grpc.NewServer()
	pb.RegisterTeleWorkerServer(grpcServer, srv)

	go func() {
		if err := grpcServer.Serve(listen); err != nil {
			t.Logf("server exited: %v", err)
		}
	}()

	t.Cleanup(func() { grpcServer.Stop() })
	return listen.Addr().String()
}

func TestStartJobReturnsValidUUID(t *testing.T) {
	addr := startTestServer(t)

	c, err := client.New(addr)
	if err != nil {
		t.Fatalf("client.New failed: %v", err)
	}
	t.Cleanup(func() { c.Close() })

	jobID, err := c.StartJob(t.Context(), "echo", []string{"hello"})
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}
	if _, err := uuid.Parse(jobID); err != nil {
		t.Fatalf("expected valid UUID, got %q: %v", jobID, err)
	}
}

func TestStartJobBadAddress(t *testing.T) {
	c, err := client.New("127.0.0.1:0")
	if err != nil {
		t.Fatalf("client.New failed: %v", err)
	}
	t.Cleanup(func() { c.Close() })

	_, err = c.StartJob(t.Context(), "echo", []string{"hello"})
	if err == nil {
		t.Fatal("expected error for bad address, got nil")
	}
}

func TestGetJobStatus(t *testing.T) {
	addr := startTestServer(t)

	c, err := client.New(addr)
	if err != nil {
		t.Fatalf("client.New failed: %v", err)
	}
	t.Cleanup(func() { c.Close() })

	jobID, err := c.StartJob(t.Context(), "true", nil)
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}

	var st pb.JobStatus
	testutil.PollUntil(t, "job to finish", func() bool {
		var err error
		st, _, err = c.GetJobStatus(t.Context(), jobID)
		if err != nil {
			t.Fatalf("GetJobStatus failed: %v", err)
		}
		return st != pb.JobStatus_JOB_STATUS_RUNNING
	})
	if st != pb.JobStatus_JOB_STATUS_SUCCESS {
		t.Fatalf("expected JOB_STATUS_SUCCESS, got %v", st)
	}
}

func TestStopJob(t *testing.T) {
	addr := startTestServer(t)

	c, err := client.New(addr)
	if err != nil {
		t.Fatalf("client.New failed: %v", err)
	}
	t.Cleanup(func() { c.Close() })

	jobID, err := c.StartJob(t.Context(), "sleep", []string{"60"})
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}

	if err := c.StopJob(t.Context(), jobID); err != nil {
		t.Fatalf("StopJob failed: %v", err)
	}

	var st pb.JobStatus
	testutil.PollUntil(t, "job to be killed", func() bool {
		var err error
		st, _, err = c.GetJobStatus(t.Context(), jobID)
		if err != nil {
			t.Fatalf("GetJobStatus failed: %v", err)
		}
		return st != pb.JobStatus_JOB_STATUS_RUNNING
	})
	if st != pb.JobStatus_JOB_STATUS_KILLED {
		t.Fatalf("expected JOB_STATUS_KILLED, got %v", st)
	}
}
