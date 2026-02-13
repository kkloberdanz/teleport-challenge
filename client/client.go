// Package client provides a gRPC client for the teleworker service.
package client

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/kkloberdanz/teleworker/proto/teleworker/v1"
)

// Client wraps a gRPC connection to the teleworker service.
type Client struct {
	conn   *grpc.ClientConn
	client pb.TeleWorkerClient
}

// New creates a new Client connected to the teleworker gRPC server at address.
// If no dial options are provided, insecure credentials are used by default.
func New(address string, opts ...grpc.DialOption) (*Client, error) {
	if len(opts) == 0 {
		// TODO: Will replace with TLS in issue 5:
		// https://github.com/kkloberdanz/teleport-challenge/issues/5
		opts = []grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		}
	}

	conn, err := grpc.NewClient(address, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	return &Client{
		conn:   conn,
		client: pb.NewTeleWorkerClient(conn),
	}, nil
}

// Close the underlying gRPC connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// StartJob starts a job on the teleworker server and returns the job ID.
func (c *Client) StartJob(ctx context.Context, command string, args []string) (string, error) {
	resp, err := c.client.StartJob(ctx, &pb.StartJobRequest{
		Command: command,
		Args:    args,
	})
	if err != nil {
		return "", fmt.Errorf("failed to start job: %w", err)
	}

	return resp.GetJobId(), nil
}
