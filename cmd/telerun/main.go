// Program telerun is the CLI client to send jobs to teleworker.
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/kkloberdanz/teleworker/auth"
	"github.com/kkloberdanz/teleworker/client"
	"github.com/kkloberdanz/teleworker/job"
	"github.com/kkloberdanz/teleworker/logging"
)

var (
	address  string
	caPath   string
	certPath string
	keyPath  string
)

func main() {
	logging.Init()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rootCmd := &cobra.Command{
		Use:   "telerun",
		Short: "Run commands via telerun",
	}
	rootCmd.SetContext(ctx)

	rootCmd.PersistentFlags().StringVar(&address, "addr", "127.0.0.1:50051", "Server address")
	rootCmd.PersistentFlags().StringVar(&caPath, "ca", "certs/ca.crt", "Path to CA certificate PEM")

	// We default to running `telerun` as the user alice using the alice key and cert.
	rootCmd.PersistentFlags().StringVar(&certPath, "cert", "certs/alice.crt", "Path to client certificate PEM")
	rootCmd.PersistentFlags().StringVar(&keyPath, "key", "certs/alice.key", "Path to client private key PEM")

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

	logsCmd := &cobra.Command{
		Use:   "logs <job_id>",
		Short: "Stream the output of a job",
		Args:  cobra.ExactArgs(1),
		RunE:  cmdLogs,
	}

	rootCmd.AddCommand(startCmd, statusCmd, stopCmd, logsCmd)

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

	teleClient, err := newTLSClient()
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
	teleClient, err := newTLSClient()
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

func cmdLogs(cmd *cobra.Command, args []string) error {
	teleClient, err := newTLSClient()
	if err != nil {
		return err
	}
	defer teleClient.Close()

	return teleClient.StreamOutput(cmd.Context(), args[0], os.Stdout)
}

func cmdStop(cmd *cobra.Command, args []string) error {
	teleClient, err := newTLSClient()
	if err != nil {
		return err
	}
	defer teleClient.Close()

	return teleClient.StopJob(cmd.Context(), args[0])
}

func newTLSClient() (*client.Client, error) {
	caCert, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load client certificate: %w", err)
	}

	tlsConf, err := auth.ClientTLSConfig(caCert, cert, "teleworker")
	if err != nil {
		return nil, fmt.Errorf("failed to build TLS config: %w", err)
	}

	return client.New(address, tlsConf)
}

func statusString(s job.Status) string {
	switch s {
	case job.StatusUnspecified:
		return "unspecified"
	case job.StatusSubmitted:
		return "submitted"
	case job.StatusRunning:
		return "running"
	case job.StatusSuccess:
		return "success"
	case job.StatusFailed:
		return "failed"
	case job.StatusKilled:
		return "killed"
	default:
		return "unknown"
	}
}
