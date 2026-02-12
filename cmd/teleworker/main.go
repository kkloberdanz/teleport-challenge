// Program teleworker manages jobs sent by the telerun client.
package main

import (
	"fmt"
	"log/slog"
	"net"
	"os"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"

	"github.com/kkloberdanz/teleworker/logging"
	pb "github.com/kkloberdanz/teleworker/proto/teleworker/v1"
	"github.com/kkloberdanz/teleworker/server"
)

var address string

func main() {
	logging.Init()

	rootCmd := &cobra.Command{
		Use:   "teleworker",
		Short: "teleworker gRPC server",
		RunE:  runServer,
	}

	rootCmd.PersistentFlags().StringVar(&address, "addr", ":50051", "Server address")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runServer(cmd *cobra.Command, args []string) error {
	listen, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterTeleWorkerServer(grpcServer, &server.Server{})

	slog.Info("server listening", "addr", address)
	if err := grpcServer.Serve(listen); err != nil {
		return fmt.Errorf("failed to serve: %w", err)
	}
	return nil
}
