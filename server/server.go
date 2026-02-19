// Package server implements the teleworker gRPC service.
package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kkloberdanz/teleworker/auth"
	"github.com/kkloberdanz/teleworker/job"
	pb "github.com/kkloberdanz/teleworker/proto/teleworker/v1"
	"github.com/kkloberdanz/teleworker/worker"
)

// Server implements the TeleWorker gRPC service.
type Server struct {
	pb.UnimplementedTeleWorkerServer
	worker *worker.Worker
}

// New creates a Server backed by the given Worker.
func New(w *worker.Worker) *Server {
	return &Server{worker: w}
}

// authorize checks that the caller is allowed to access the given job. Admins
// may access any job. Regular users may only access their own jobs.
// The caller's identity must already be in the context (set by the auth interceptor).
func (s *Server) authorize(ctx context.Context, jobID string) (auth.Identity, error) {
	id, err := auth.FromContext(ctx)
	if err != nil {
		return auth.Identity{}, err
	}

	if id.IsAdmin() {
		return id, nil
	}

	owner, err := s.worker.GetJobOwner(jobID)
	if err != nil {
		if errors.Is(err, worker.ErrJobNotFound) {
			return auth.Identity{}, status.Error(codes.NotFound, "job not found")
		}
		return auth.Identity{}, status.Errorf(codes.Internal, "failed to check job owner: %v", err)
	}

	if owner.Username != id.Username {
		// We return a NotFound here because if we returned PermissionDenied,
		// this could leak which job IDs are valid and owned by another user.
		// Job IDs currently are UUIDs, which are 128 bits. It would be
		// impractical to brute force a 128 bit key (although only 122 bits are
		// random), however, this is a defense in depth. Suppose if the key type
		// changes from a UUID to something with fewer bits of entropy?
		return auth.Identity{}, status.Error(codes.NotFound, "job not found")
	}
	return id, nil
}

// StartJob starts a new job and returns its ID.
func (s *Server) StartJob(ctx context.Context, req *pb.StartJobRequest) (*pb.StartJobResponse, error) {
	id, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	if req.GetCommand() == "" {
		return nil, status.Error(codes.InvalidArgument, "command must not be empty")
	}

	// TODO: We can support other job types, such as Docker by extending the
	// protobuf to include which job type we want to launch. Currently, we will
	// hard-code JobTypeLocal for simplicity.
	jobID, err := s.worker.StartJob(job.JobTypeLocal, req.GetCommand(), req.GetArgs(), id)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to start job: %v", err)
	}

	slog.Info(
		"started job",
		"jobID", jobID,
		"command", req.GetCommand(),
		"args", req.GetArgs(),
		"user", id.Username,
	)

	return &pb.StartJobResponse{
		JobId: jobID,
	}, nil
}

// GetJobStatus returns the current status and exit code for a job.
func (s *Server) GetJobStatus(ctx context.Context, req *pb.GetJobStatusRequest) (*pb.GetJobStatusResponse, error) {
	if _, err := s.authorize(ctx, req.GetJobId()); err != nil {
		return nil, err
	}

	result, err := s.worker.GetJobStatus(req.GetJobId())
	if err != nil {
		if errors.Is(err, worker.ErrJobNotFound) {
			return nil, status.Error(codes.NotFound, "job not found")
		}
		return nil, status.Errorf(codes.Internal, "failed to get job status: %v", err)
	}

	resp := &pb.GetJobStatusResponse{
		JobId:  req.GetJobId(),
		Status: mapJobStatus(result.Status),
	}

	if result.ExitCode != nil {
		ec := int32(*result.ExitCode)
		resp.ExitCode = &ec
	}

	return resp, nil
}

// StopJob terminates a running job.
func (s *Server) StopJob(ctx context.Context, req *pb.StopJobRequest) (*pb.StopJobResponse, error) {
	if _, err := s.authorize(ctx, req.GetJobId()); err != nil {
		return nil, err
	}

	err := s.worker.StopJob(req.GetJobId())
	if err != nil {
		if errors.Is(err, worker.ErrJobNotFound) {
			return nil, status.Error(codes.NotFound, "job not found")
		}
		if errors.Is(err, job.ErrJobNotRunning) {
			return nil, status.Error(codes.FailedPrecondition, "job is not running")
		}
		return nil, status.Errorf(codes.Internal, "failed to stop job: %v", err)
	}

	return &pb.StopJobResponse{}, nil
}

// StreamOutput streams the combined stdout/stderr of a job to the client.
func (s *Server) StreamOutput(req *pb.StreamOutputRequest, stream grpc.ServerStreamingServer[pb.StreamOutputResponse]) error {
	if _, err := s.authorize(stream.Context(), req.GetJobId()); err != nil {
		return err
	}

	sub, err := s.worker.StreamOutput(req.GetJobId())
	if err != nil {
		if errors.Is(err, worker.ErrJobNotFound) {
			return status.Error(codes.NotFound, "job not found")
		}
		return status.Errorf(codes.Internal, "failed to stream output: %v", err)
	}
	// Ensure we close when either the context is canceled or we exit this
	// function
	closeSub := sync.OnceFunc(func() { sub.Close() })
	stop := context.AfterFunc(stream.Context(), closeSub)
	defer stop()
	defer closeSub()

	buf := make([]byte, 4096) // For simplicity, hard code buffer size.
	for {
		n, err := sub.Read(buf)
		if n > 0 {
			if sendErr := stream.Send(&pb.StreamOutputResponse{Data: buf[:n]}); sendErr != nil {
				return sendErr
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			// If the client disconnected, sub.Close() was called
			// via the AfterFunc above, causing Read to return
			// io.ErrClosedPipe. Map that to a proper gRPC status.
			if errors.Is(err, io.ErrClosedPipe) {
				return status.Error(codes.Canceled, "client disconnected")
			}
			return err
		}
	}
}

func mapJobStatus(s job.Status) pb.JobStatus {
	switch s {
	case job.StatusSubmitted:
		return pb.JobStatus_JOB_STATUS_SUBMITTED
	case job.StatusRunning:
		return pb.JobStatus_JOB_STATUS_RUNNING
	case job.StatusSuccess:
		return pb.JobStatus_JOB_STATUS_SUCCESS
	case job.StatusFailed:
		return pb.JobStatus_JOB_STATUS_FAILED
	case job.StatusKilled:
		return pb.JobStatus_JOB_STATUS_KILLED
	default:
		return pb.JobStatus_JOB_STATUS_UNSPECIFIED
	}
}
