// Program telerun is the CLI client to send jobs to teleworker.
package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/kkloberdanz/teleworker/client"
	"github.com/kkloberdanz/teleworker/logging"
	pb "github.com/kkloberdanz/teleworker/proto/teleworker/v1"
)

var address string

func main() {
	logging.Init()

	rootCmd := &cobra.Command{
		Use:   "telerun",
		Short: "Run commands via telerun",
	}

	rootCmd.PersistentFlags().StringVar(&address, "addr", "127.0.0.1:50051", "Server address")

	startCmd := &cobra.Command{
		Use:   "start -- <command> [args...]",
		Short: "Run a command via telerun",
		Args:  cobra.MinimumNArgs(1),
		RunE:  cmdStart,
	}

	statusCmd := &cobra.Command{
		Use:   "status <job_id>",
		Short: "Get the status of a job",
		Args:  cobra.ExactArgs(1),
		RunE:  cmdStatus,
	}

	stopCmd := &cobra.Command{
		Use:   "stop <job_id>",
		Short: "Stop a running job",
		Args:  cobra.ExactArgs(1),
		RunE:  cmdStop,
	}

	rootCmd.AddCommand(startCmd, statusCmd, stopCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// cmdStart sends the command to the gRPC server.
func cmdStart(cmd *cobra.Command, args []string) error {
	slog.Info(
		"connecting",
		"addr", address,
	)

	teleClient, err := client.New(address)
	if err != nil {
		return err
	}
	defer teleClient.Close()

	command := args[0]
	commandArgs := args[1:]
	slog.Info(
		"starting job",
		"command", command,
		"arguments", commandArgs,
	)

	jobID, err := teleClient.StartJob(cmd.Context(), command, commandArgs)
	if err != nil {
		return err
	}

	slog.Info(
		"job started",
		"job_id", jobID,
	)

	output := struct {
		JobID string `json:"job_id"`
	}{JobID: jobID}

	b, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal job ID: %w", err)
	}
	fmt.Println(string(b))

	return nil
}

func cmdStatus(cmd *cobra.Command, args []string) error {
	teleClient, err := client.New(address)
	if err != nil {
		return err
	}
	defer teleClient.Close()

	jobStatus, exitCode, err := teleClient.GetJobStatus(cmd.Context(), args[0])
	if err != nil {
		return err
	}

	output := struct {
		JobID    string `json:"job_id"`
		Status   string `json:"status"`
		ExitCode *int32 `json:"exit_code,omitempty"`
	}{
		JobID:    args[0],
		Status:   statusString(jobStatus),
		ExitCode: exitCode,
	}

	b, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal status: %w", err)
	}
	fmt.Println(string(b))

	return nil
}

func cmdStop(cmd *cobra.Command, args []string) error {
	teleClient, err := client.New(address)
	if err != nil {
		return err
	}
	defer teleClient.Close()

	return teleClient.StopJob(cmd.Context(), args[0])
}

func statusString(s pb.JobStatus) string {
	switch s {
	case pb.JobStatus_JOB_STATUS_UNSPECIFIED:
		return "unspecified"
	case pb.JobStatus_JOB_STATUS_SUBMITTED:
		return "submitted"
	case pb.JobStatus_JOB_STATUS_RUNNING:
		return "running"
	case pb.JobStatus_JOB_STATUS_SUCCESS:
		return "success"
	case pb.JobStatus_JOB_STATUS_FAILED:
		return "failed"
	case pb.JobStatus_JOB_STATUS_KILLED:
		return "killed"
	default:
		return "unknown"
	}
}
