// Package server implements the teleworker gRPC service.
package server

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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

// StartJob starts a new job and returns its ID.
func (s *Server) StartJob(ctx context.Context, req *pb.StartJobRequest) (*pb.StartJobResponse, error) {
	if req.GetCommand() == "" {
		return nil, status.Error(codes.InvalidArgument, "command must not be empty")
	}

	// TODO: We can support other job types, such as Docker by extending the
	// protobuf to include which job type we want to launch. Currently, we will
	// hard-code JobTypeLocal for simplicity.
	jobID, err := s.worker.StartJob(job.JobTypeLocal, req.GetCommand(), req.GetArgs())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to start job: %v", err)
	}

	slog.Info(
		"started job",
		"jobID", jobID,
		"command", req.GetCommand(),
		"args", req.GetArgs(),
	)

	return &pb.StartJobResponse{
		JobId: jobID,
	}, nil
}

// GetJobStatus returns the current status and exit code for a job.
func (s *Server) GetJobStatus(ctx context.Context, req *pb.GetJobStatusRequest) (*pb.GetJobStatusResponse, error) {
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

func mapJobStatus(s job.JobStatus) pb.JobStatus {
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
