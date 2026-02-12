package client_test

import (
	"context"
	"net"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/goleak"
	"google.golang.org/grpc"

	"github.com/kkloberdanz/teleworker/client"
	pb "github.com/kkloberdanz/teleworker/proto/teleworker/v1"
	"github.com/kkloberdanz/teleworker/server"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// Note: There is some redundancy between the server tests and the client tests,
// however, as this project progresses, these redundancies will disapear, as
// they will be testing different aspects of the implementation, i.e., client
// specific functionality vs server specific functionality.

// startTestServer starts a gRPC server and returns its address and a cleanup
// function.
func startTestServer(t *testing.T) (addr string, cleanup func()) {
	t.Helper()

	listen, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterTeleWorkerServer(grpcServer, &server.Server{})

	go func() {
		if err := grpcServer.Serve(listen); err != nil {
			t.Logf("server exited: %v", err)
		}
	}()

	return listen.Addr().String(), func() { grpcServer.Stop() }
}

func TestStartJobReturnsValidUUID(t *testing.T) {
	addr, cleanup := startTestServer(t)
	defer cleanup()

	jobID, err := client.StartJob(context.Background(), addr, "echo", []string{"hello"})
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}
	if _, err := uuid.Parse(jobID); err != nil {
		t.Fatalf("expected valid UUID, got %q: %v", jobID, err)
	}
}

func TestStartJobBadAddress(t *testing.T) {
	_, err := client.StartJob(context.Background(), "127.0.0.1:0", "echo", []string{"hello"})
	if err == nil {
		t.Fatal("expected error for bad address, got nil")
	}
}
