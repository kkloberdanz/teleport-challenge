// Program teleworker manages jobs sent by the telerun client.
package main

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"

	"github.com/kkloberdanz/teleworker/logging"
	pb "github.com/kkloberdanz/teleworker/proto/teleworker/v1"
	"github.com/kkloberdanz/teleworker/resources"
	"github.com/kkloberdanz/teleworker/server"
	"github.com/kkloberdanz/teleworker/worker"
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
	cgroupMgr, err := resources.NewManager("/sys/fs/cgroup/teleworker")
	if err != nil {
		return fmt.Errorf("failed to configure cgroups (requires root): %w", err)
	}

	w := worker.New(worker.Options{CgroupMgr: *cgroupMgr})
	srv := server.New(w)

	listen, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterTeleWorkerServer(grpcServer, srv)

	// Handle shutdown on SIGINT/SIGTERM.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		slog.Info(
			"received signal, shutting down",
			"signal", sig,
		)
		grpcServer.GracefulStop()
	}()

	slog.Info(
		"server listening",
		"addr", address,
	)
	if err := grpcServer.Serve(listen); err != nil {
		return fmt.Errorf("failed to serve: %w", err)
	}

	slog.Info("server finished")
	return nil
}
