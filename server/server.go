// Package server implements the teleworker gRPC service.
package server

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/kkloberdanz/teleworker/proto/teleworker/v1"
)

type Server struct {
	pb.UnimplementedTeleWorkerServer
}

func (s *Server) StartJob(ctx context.Context, req *pb.StartJobRequest) (*pb.StartJobResponse, error) {
	if req.GetCommand() == "" {
		return nil, status.Error(codes.InvalidArgument, "command must not be empty")
	}

	jobID := uuid.New().String()

	// TODO: Start the job, handle job lifecycle
	// See this issue for implementation:
	// https://github.com/kkloberdanz/teleport-challenge/issues/3
	slog.Info(
		"starting job",
		"jobID", jobID,
		"command", req.GetCommand(),
		"args", req.GetArgs(),
	)

	return &pb.StartJobResponse{
		JobId: jobID,
	}, nil
}
