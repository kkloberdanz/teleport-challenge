package client_test

import (
	"bytes"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/goleak"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/kkloberdanz/teleworker/auth"
	"github.com/kkloberdanz/teleworker/client"
	"github.com/kkloberdanz/teleworker/job"
	pb "github.com/kkloberdanz/teleworker/proto/teleworker/v1"
	"github.com/kkloberdanz/teleworker/server"
	"github.com/kkloberdanz/teleworker/testutil"
	"github.com/kkloberdanz/teleworker/worker"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// startTestServer starts a gRPC server with mTLS and returns its address.
func startTestServer(t *testing.T) string {
	t.Helper()

	listen, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	mgr := testutil.RequireManager(t)
	w := worker.New(worker.Options{CgroupMgr: mgr})
	srv := server.New(w)

	grpcServer := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(testutil.ServerTLSConfig(t))),
		grpc.UnaryInterceptor(auth.UnaryInterceptor),
		grpc.StreamInterceptor(auth.StreamInterceptor),
	)
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

	c, err := client.New(addr, testutil.ClientTLSConfig(t, "alice"))
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
	c, err := client.New("127.0.0.1:0", testutil.ClientTLSConfig(t, "alice"))
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

	c, err := client.New(addr, testutil.ClientTLSConfig(t, "alice"))
	if err != nil {
		t.Fatalf("client.New failed: %v", err)
	}
	t.Cleanup(func() { c.Close() })

	jobID, err := c.StartJob(t.Context(), "true", nil)
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}

	var st job.Status
	testutil.PollUntil(t, "job to finish", func() bool {
		var err error
		st, _, err = c.GetJobStatus(t.Context(), jobID)
		if err != nil {
			t.Fatalf("GetJobStatus failed: %v", err)
		}
		return st != job.StatusRunning
	})
	if st != job.StatusSuccess {
		t.Fatalf("expected StatusSuccess, got %v", st)
	}
}

func TestStreamOutput(t *testing.T) {
	addr := startTestServer(t)

	c, err := client.New(addr, testutil.ClientTLSConfig(t, "alice"))
	if err != nil {
		t.Fatalf("client.New failed: %v", err)
	}
	t.Cleanup(func() { c.Close() })

	jobID, err := c.StartJob(t.Context(), "echo", []string{"client-stream"})
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}

	var buf bytes.Buffer
	if err := c.StreamOutput(t.Context(), jobID, &buf); err != nil {
		t.Fatalf("StreamOutput failed: %v", err)
	}

	if !strings.Contains(buf.String(), "client-stream") {
		t.Fatalf("expected output to contain %q, got %q", "client-stream", buf.String())
	}
}

// TestStreamOutputIncremental verifies that output arrives at the client
// incrementally while the job is still running, not all at once after exit.
func TestStreamOutputIncremental(t *testing.T) {
	addr := startTestServer(t)

	c, err := client.New(addr, testutil.ClientTLSConfig(t, "alice"))
	if err != nil {
		t.Fatalf("client.New failed: %v", err)
	}
	t.Cleanup(func() { c.Close() })

	// Start a job that prints "first", sleeps, then prints "second".
	jobID, err := c.StartJob(t.Context(), "sh", []string{
		"-c", "echo first; sleep 2; echo second",
	})
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}

	// Use an io.Pipe so we can observe each chunk as it arrives.
	pr, pw := io.Pipe()

	streamDone := make(chan error, 1)
	go func() {
		streamDone <- c.StreamOutput(t.Context(), jobID, pw)
		pw.Close()
	}()

	// Wait for the first chunk which should contain "first".
	buf := make([]byte, 4096)
	n, err := pr.Read(buf)
	if err != nil {
		t.Fatalf("failed to read first chunk: %v", err)
	}
	first := string(buf[:n])
	if !strings.Contains(first, "first") {
		t.Fatalf("expected first chunk to contain %q, got %q", "first", first)
	}

	// The job should still be running because of the sleep.
	st, _, err := c.GetJobStatus(t.Context(), jobID)
	if err != nil {
		t.Fatalf("GetJobStatus failed: %v", err)
	}
	if st != job.StatusRunning {
		t.Fatalf("expected job to still be running after first chunk, got %v", st)
	}

	// Read remaining output until EOF.
	rest, err := io.ReadAll(pr)
	if err != nil {
		t.Fatalf("failed to read remaining output: %v", err)
	}
	if err := <-streamDone; err != nil {
		t.Fatalf("StreamOutput failed: %v", err)
	}
	if !strings.Contains(string(rest), "second") {
		t.Fatalf("expected remaining output to contain %q, got %q", "second", string(rest))
	}
}

func TestStopJob(t *testing.T) {
	addr := startTestServer(t)

	c, err := client.New(addr, testutil.ClientTLSConfig(t, "alice"))
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

	var st job.Status
	testutil.PollUntil(t, "job to be killed", func() bool {
		var err error
		st, _, err = c.GetJobStatus(t.Context(), jobID)
		if err != nil {
			t.Fatalf("GetJobStatus failed: %v", err)
		}
		return st != job.StatusRunning
	})
	if st != job.StatusKilled {
		t.Fatalf("expected StatusKilled, got %v", st)
	}
}
