// Package client provides a gRPC client for the teleworker service.
package client

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/kkloberdanz/teleworker/job"
	pb "github.com/kkloberdanz/teleworker/proto/teleworker/v1"
)

// Client wraps a gRPC connection to the teleworker service.
type Client struct {
	conn   *grpc.ClientConn
	client pb.TeleWorkerClient
}

// New creates a new Client connected to the teleworker gRPC server at address
// using the provided TLS configuration for mutual TLS authentication.
func New(address string, tlsConf *tls.Config) (*Client, error) {
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(credentials.NewTLS(tlsConf)))
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

// GetJobStatus returns the job's status and optional exit code.
func (c *Client) GetJobStatus(ctx context.Context, jobID string) (job.Status, *int32, error) {
	resp, err := c.client.GetJobStatus(ctx, &pb.GetJobStatusRequest{
		JobId: jobID,
	})
	if err != nil {
		return job.StatusUnspecified, nil, fmt.Errorf("failed to get job status: %w", err)
	}

	return mapStatus(resp.GetStatus()), resp.ExitCode, nil
}

func mapStatus(s pb.JobStatus) job.Status {
	switch s {
	case pb.JobStatus_JOB_STATUS_SUBMITTED:
		return job.StatusSubmitted
	case pb.JobStatus_JOB_STATUS_RUNNING:
		return job.StatusRunning
	case pb.JobStatus_JOB_STATUS_SUCCESS:
		return job.StatusSuccess
	case pb.JobStatus_JOB_STATUS_FAILED:
		return job.StatusFailed
	case pb.JobStatus_JOB_STATUS_KILLED:
		return job.StatusKilled
	default:
		return job.StatusUnspecified
	}
}

// StreamOutput streams the combined stdout/stderr of a job into w.
// It returns nil on EOF (job finished), or an error on failure.
func (c *Client) StreamOutput(ctx context.Context, jobID string, w io.Writer) error {
	stream, err := c.client.StreamOutput(ctx, &pb.StreamOutputRequest{
		JobId: jobID,
	})
	if err != nil {
		return fmt.Errorf("failed to open output stream: %w", err)
	}
	if err := stream.CloseSend(); err != nil {
		return fmt.Errorf("failed to close send: %w", err)
	}
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("stream recv error: %w", err)
		}
		if _, err := w.Write(resp.GetData()); err != nil {
			return fmt.Errorf("write error: %w", err)
		}
	}
}

// StopJob stops a running job.
func (c *Client) StopJob(ctx context.Context, jobID string) error {
	_, err := c.client.StopJob(ctx, &pb.StopJobRequest{
		JobId: jobID,
	})
	if err != nil {
		return fmt.Errorf("failed to stop job: %w", err)
	}
	return nil
}
