// Package client provides a gRPC client for the teleworker service.
package client

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/kkloberdanz/teleworker/proto/teleworker/v1"
)

// StartJob connects to the teleworker gRPC server at provided address and
// starts a job. It returns the job ID created by the server.
func StartJob(ctx context.Context, address, command string, args []string) (string, error) {
	// TODO: Will replace with TLS in issue 5:
	// https://github.com/kkloberdanz/teleport-challenge/issues/5
	creds := insecure.NewCredentials()

	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(creds))
	if err != nil {
		return "", fmt.Errorf("failed to connect: %w", err)
	}
	defer conn.Close()

	c := pb.NewTeleWorkerClient(conn)

	resp, err := c.StartJob(ctx, &pb.StartJobRequest{
		Command: command,
		Args:    args,
	})
	if err != nil {
		return "", fmt.Errorf("failed to start job: %w", err)
	}

	return resp.GetJobId(), nil
}
