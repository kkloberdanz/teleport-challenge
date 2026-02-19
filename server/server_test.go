package server_test

import (
	"io"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/goleak"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	pb "github.com/kkloberdanz/teleworker/proto/teleworker/v1"
	"github.com/kkloberdanz/teleworker/server"
	"github.com/kkloberdanz/teleworker/testutil"
	"github.com/kkloberdanz/teleworker/worker"
)

// Enable goleak to ensure no goroutines have been leaked.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// testEnv holds the mTLS test infrastructure.
type testEnv struct {
	addr string
}

// newTestEnv starts a gRPC server with mTLS and returns the test environment.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	listen, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	mgr := testutil.RequireManager(t)
	w := worker.New(worker.Options{CgroupMgr: mgr})
	srv := server.New(w)

	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(testutil.ServerTLSConfig(t))))
	pb.RegisterTeleWorkerServer(grpcServer, srv)

	go func() {
		if err := grpcServer.Serve(listen); err != nil {
			t.Logf("server exited: %v", err)
		}
	}()

	t.Cleanup(func() { grpcServer.Stop() })

	return &testEnv{addr: listen.Addr().String()}
}

// clientAs creates a gRPC client using the certificate certs/<name>.crt/.key.
func (e *testEnv) clientAs(t *testing.T, name string) pb.TeleWorkerClient {
	t.Helper()

	conn, err := grpc.NewClient(
		e.addr,
		grpc.WithTransportCredentials(credentials.NewTLS(testutil.ClientTLSConfig(t, name))),
	)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return pb.NewTeleWorkerClient(conn)
}

func TestStartJobReturnsValidUUID(t *testing.T) {
	env := newTestEnv(t)
	client := env.clientAs(t, "alice")

	resp, err := client.StartJob(t.Context(), &pb.StartJobRequest{
		Command: "echo",
		Args:    []string{"hello"},
	})
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}
	if _, err := uuid.Parse(resp.GetJobId()); err != nil {
		t.Fatalf("expected valid UUID, got %q: %v", resp.GetJobId(), err)
	}
}

func TestStartJobEmptyCommand(t *testing.T) {
	env := newTestEnv(t)
	client := env.clientAs(t, "alice")

	_, err := client.StartJob(t.Context(), &pb.StartJobRequest{})
	if err == nil {
		t.Fatal("expected error for empty command, got nil")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", err)
	}
}

func TestGetJobStatus(t *testing.T) {
	env := newTestEnv(t)
	client := env.clientAs(t, "alice")

	resp, err := client.StartJob(t.Context(), &pb.StartJobRequest{
		Command: "true",
	})
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}

	var statusResp *pb.GetJobStatusResponse
	testutil.PollUntil(t, "job to finish", func() bool {
		var err error
		statusResp, err = client.GetJobStatus(t.Context(), &pb.GetJobStatusRequest{JobId: resp.GetJobId()})
		if err != nil {
			t.Fatalf("GetJobStatus failed: %v", err)
		}
		return statusResp.GetStatus() != pb.JobStatus_JOB_STATUS_RUNNING
	})
	if statusResp.GetStatus() != pb.JobStatus_JOB_STATUS_SUCCESS {
		t.Fatalf("expected JOB_STATUS_SUCCESS, got %v", statusResp.GetStatus())
	}
	if statusResp.GetExitCode() != 0 {
		t.Fatalf("expected exit code 0, got %d", statusResp.GetExitCode())
	}
}

func TestGetJobStatusNotFound(t *testing.T) {
	env := newTestEnv(t)
	client := env.clientAs(t, "alice")

	_, err := client.GetJobStatus(t.Context(), &pb.GetJobStatusRequest{
		JobId: "nonexistent-job",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.NotFound {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestStopJob(t *testing.T) {
	env := newTestEnv(t)
	client := env.clientAs(t, "alice")

	resp, err := client.StartJob(t.Context(), &pb.StartJobRequest{
		Command: "sleep",
		Args:    []string{"60"},
	})
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}

	_, err = client.StopJob(t.Context(), &pb.StopJobRequest{
		JobId: resp.GetJobId(),
	})
	if err != nil {
		t.Fatalf("StopJob failed: %v", err)
	}

	var statusResp *pb.GetJobStatusResponse
	testutil.PollUntil(t, "job to be killed", func() bool {
		var err error
		statusResp, err = client.GetJobStatus(t.Context(), &pb.GetJobStatusRequest{JobId: resp.GetJobId()})
		if err != nil {
			t.Fatalf("GetJobStatus failed: %v", err)
		}
		return statusResp.GetStatus() != pb.JobStatus_JOB_STATUS_RUNNING
	})
	if statusResp.GetStatus() != pb.JobStatus_JOB_STATUS_KILLED {
		t.Fatalf("expected JOB_STATUS_KILLED, got %v", statusResp.GetStatus())
	}
}

func TestStopJobNotFound(t *testing.T) {
	env := newTestEnv(t)
	client := env.clientAs(t, "alice")

	_, err := client.StopJob(t.Context(), &pb.StopJobRequest{
		JobId: "nonexistent-job",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.NotFound {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestStopFinishedJob(t *testing.T) {
	env := newTestEnv(t)
	client := env.clientAs(t, "alice")

	resp, err := client.StartJob(t.Context(), &pb.StartJobRequest{
		Command: "true",
	})
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}

	// Wait for the job to finish.
	testutil.PollUntil(t, "job to finish", func() bool {
		statusResp, err := client.GetJobStatus(t.Context(), &pb.GetJobStatusRequest{JobId: resp.GetJobId()})
		if err != nil {
			t.Fatalf("GetJobStatus failed: %v", err)
		}
		return statusResp.GetStatus() != pb.JobStatus_JOB_STATUS_RUNNING
	})

	// Stopping a finished job should return FailedPrecondition.
	_, err = client.StopJob(t.Context(), &pb.StopJobRequest{
		JobId: resp.GetJobId(),
	})
	if err == nil {
		t.Fatal("expected error stopping finished job, got nil")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.FailedPrecondition {
		t.Fatalf("expected FailedPrecondition, got %v", err)
	}
}

func recvAll(t *testing.T, stream grpc.ServerStreamingClient[pb.StreamOutputResponse]) string {
	t.Helper()
	var sb strings.Builder
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Recv failed: %v", err)
		}
		sb.Write(resp.GetData())
	}
	return sb.String()
}

func TestStreamOutput(t *testing.T) {
	env := newTestEnv(t)
	client := env.clientAs(t, "alice")

	resp, err := client.StartJob(t.Context(), &pb.StartJobRequest{
		Command: "echo",
		Args:    []string{"hello"},
	})
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}

	stream, err := client.StreamOutput(t.Context(), &pb.StreamOutputRequest{
		JobId: resp.GetJobId(),
	})
	if err != nil {
		t.Fatalf("StreamOutput failed: %v", err)
	}

	got := recvAll(t, stream)
	if !strings.Contains(got, "hello") {
		t.Fatalf("expected output to contain %q, got %q", "hello", got)
	}
}

func TestStreamOutputJobNotFound(t *testing.T) {
	env := newTestEnv(t)
	client := env.clientAs(t, "alice")

	stream, err := client.StreamOutput(t.Context(), &pb.StreamOutputRequest{
		JobId: "nonexistent-job",
	})
	if err != nil {
		t.Fatalf("failed to open stream: %v", err)
	}

	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.NotFound {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestStreamOutputMultipleClients(t *testing.T) {
	env := newTestEnv(t)
	client := env.clientAs(t, "alice")

	resp, err := client.StartJob(t.Context(), &pb.StartJobRequest{
		Command: "echo",
		Args:    []string{"multi"},
	})
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}

	// Open two concurrent streams for the same job.
	stream1, err := client.StreamOutput(t.Context(), &pb.StreamOutputRequest{
		JobId: resp.GetJobId(),
	})
	if err != nil {
		t.Fatalf("StreamOutput 1 failed: %v", err)
	}
	stream2, err := client.StreamOutput(t.Context(), &pb.StreamOutputRequest{
		JobId: resp.GetJobId(),
	})
	if err != nil {
		t.Fatalf("StreamOutput 2 failed: %v", err)
	}

	var wg sync.WaitGroup
	results := make([]string, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		results[0] = recvAll(t, stream1)
	}()
	go func() {
		defer wg.Done()
		results[1] = recvAll(t, stream2)
	}()
	wg.Wait()

	for i, r := range results {
		if !strings.Contains(r, "multi") {
			t.Errorf("stream %d: expected output to contain %q, got %q", i+1, "multi", r)
		}
	}
}

func TestOwnerCanAccessOwnJob(t *testing.T) {
	env := newTestEnv(t)
	alice := env.clientAs(t, "alice")

	resp, err := alice.StartJob(t.Context(), &pb.StartJobRequest{
		Command: "true",
	})
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}

	// Alice should be able to get status of her own job.
	testutil.PollUntil(t, "job to finish", func() bool {
		statusResp, err := alice.GetJobStatus(t.Context(), &pb.GetJobStatusRequest{JobId: resp.GetJobId()})
		if err != nil {
			t.Fatalf("GetJobStatus failed: %v", err)
		}
		return statusResp.GetStatus() != pb.JobStatus_JOB_STATUS_RUNNING
	})
}

func TestNonOwnerCannotGetStatus(t *testing.T) {
	env := newTestEnv(t)
	alice := env.clientAs(t, "alice")
	bob := env.clientAs(t, "bob")

	resp, err := alice.StartJob(t.Context(), &pb.StartJobRequest{
		Command: "sleep",
		Args:    []string{"60"},
	})
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}

	defer alice.StopJob(t.Context(), &pb.StopJobRequest{JobId: resp.GetJobId()})

	_, err = bob.GetJobStatus(t.Context(), &pb.GetJobStatusRequest{
		JobId: resp.GetJobId(),
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.NotFound {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestNonOwnerCannotStopJob(t *testing.T) {
	env := newTestEnv(t)
	alice := env.clientAs(t, "alice")
	bob := env.clientAs(t, "bob")

	resp, err := alice.StartJob(t.Context(), &pb.StartJobRequest{
		Command: "sleep",
		Args:    []string{"60"},
	})
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}

	defer alice.StopJob(t.Context(), &pb.StopJobRequest{JobId: resp.GetJobId()})

	_, err = bob.StopJob(t.Context(), &pb.StopJobRequest{
		JobId: resp.GetJobId(),
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.NotFound {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestNonOwnerCannotStreamOutput(t *testing.T) {
	env := newTestEnv(t)
	alice := env.clientAs(t, "alice")
	bob := env.clientAs(t, "bob")

	resp, err := alice.StartJob(t.Context(), &pb.StartJobRequest{
		Command: "sleep",
		Args:    []string{"60"},
	})
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}

	defer alice.StopJob(t.Context(), &pb.StopJobRequest{JobId: resp.GetJobId()})

	stream, err := bob.StreamOutput(t.Context(), &pb.StreamOutputRequest{
		JobId: resp.GetJobId(),
	})
	if err != nil {
		t.Fatalf("failed to open stream: %v", err)
	}

	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.NotFound {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestAdminCanAccessAnyJob(t *testing.T) {
	env := newTestEnv(t)
	alice := env.clientAs(t, "alice")
	admin := env.clientAs(t, "admin")

	resp, err := alice.StartJob(t.Context(), &pb.StartJobRequest{
		Command: "sleep",
		Args:    []string{"60"},
	})
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}

	// Admin can query alice's job.
	_, err = admin.GetJobStatus(t.Context(), &pb.GetJobStatusRequest{
		JobId: resp.GetJobId(),
	})
	if err != nil {
		t.Fatalf("admin GetJobStatus failed: %v", err)
	}

	// Admin can stop alice's job.
	_, err = admin.StopJob(t.Context(), &pb.StopJobRequest{
		JobId: resp.GetJobId(),
	})
	if err != nil {
		t.Fatalf("admin StopJob failed: %v", err)
	}
}
