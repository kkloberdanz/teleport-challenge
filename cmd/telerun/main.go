// Program telerun is the CLI client to send jobs to teleworker.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/kkloberdanz/teleworker/client"
	"github.com/kkloberdanz/teleworker/logging"
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

	rootCmd.AddCommand(startCmd)

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

	command := args[0]
	commandArgs := args[1:]
	slog.Info(
		"starting job",
		"command", command,
		"arguments", commandArgs,
	)

	jobID, err := client.StartJob(context.Background(), address, command, commandArgs)
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
