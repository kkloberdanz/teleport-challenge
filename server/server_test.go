package server_test

import (
	"net"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/goleak"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	pb "github.com/kkloberdanz/teleworker/proto/teleworker/v1"
	"github.com/kkloberdanz/teleworker/server"
)

// Enable goleak to ensure no goroutines have been leaked.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// Note: There is some redundancy between the server tests and the client tests,
// however, as this project progresses, these redundancies will disapear, as
// they will be testing different aspects of the implementation, i.e., client
// specific functionality vs server specific functionality.

// startTestServer starts a gRPC server and returns a connected client.
func startTestServer(t *testing.T) pb.TeleWorkerClient {
	t.Helper()

	listen, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterTeleWorkerServer(grpcServer, &server.Server{})

	// Server listens in the background.
	go func() {
		if err := grpcServer.Serve(listen); err != nil {
			t.Logf("server exited: %v", err)
		}
	}()

	conn, err := grpc.NewClient(
		listen.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		grpcServer.Stop()
		t.Fatalf("failed to connect: %v", err)
	}

	t.Cleanup(func() {
		conn.Close()
		grpcServer.Stop()
	})
	return pb.NewTeleWorkerClient(conn)
}

func TestStartJobReturnsValidUUID(t *testing.T) {
	client := startTestServer(t)

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
	client := startTestServer(t)

	_, err := client.StartJob(t.Context(), &pb.StartJobRequest{})
	if err == nil {
		t.Fatal("expected error for empty command, got nil")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", err)
	}
}

// Job status is not yet implemented, ensure the server returns a correct response.
func TestGetJobStatusUnimplemented(t *testing.T) {
	client := startTestServer(t)

	_, err := client.GetJobStatus(t.Context(), &pb.GetJobStatusRequest{
		JobId: "test-job",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.Unimplemented {
		t.Fatalf("expected Unimplemented, got %v", err)
	}
}

// Job stop is not yet implemented, ensure the server returns a correct response.
func TestStopJobUnimplemented(t *testing.T) {
	client := startTestServer(t)

	_, err := client.StopJob(t.Context(), &pb.StopJobRequest{
		JobId: "test-job",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.Unimplemented {
		t.Fatalf("expected Unimplemented, got %v", err)
	}
}

// Job output is not yet implemented, ensure the server returns a correct response.
func TestStreamOutputUnimplemented(t *testing.T) {
	client := startTestServer(t)

	stream, err := client.StreamOutput(t.Context(), &pb.StreamOutputRequest{
		JobId: "test-job",
	})
	if err != nil {
		t.Fatalf("failed to open stream: %v", err)
	}

	// The error surfaces on the first Recv for server-streaming RPCs.
	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.Unimplemented {
		t.Fatalf("expected Unimplemented, got %v", err)
	}
}
