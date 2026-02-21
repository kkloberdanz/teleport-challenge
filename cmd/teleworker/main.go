// Program teleworker manages jobs sent by the telerun client.
package main

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/kkloberdanz/teleworker/auth"
	"github.com/kkloberdanz/teleworker/logging"
	pb "github.com/kkloberdanz/teleworker/proto/teleworker/v1"
	"github.com/kkloberdanz/teleworker/resources"
	"github.com/kkloberdanz/teleworker/server"
	"github.com/kkloberdanz/teleworker/worker"
)

var (
	address  string
	caPath   string
	certPath string
	keyPath  string
)

func main() {
	logging.Init()

	rootCmd := &cobra.Command{
		Use:   "teleworker",
		Short: "teleworker gRPC server",
		RunE:  runServer,
	}

	rootCmd.PersistentFlags().StringVar(&address, "addr", ":50051", "Server address")
	rootCmd.PersistentFlags().StringVar(&caPath, "ca", "certs/ca.crt", "Path to CA certificate PEM")
	rootCmd.PersistentFlags().StringVar(&certPath, "cert", "certs/server.crt", "Path to server certificate PEM")
	rootCmd.PersistentFlags().StringVar(&keyPath, "key", "certs/server.key", "Path to server private key PEM")

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

	tlsConf, err := loadServerTLS()
	if err != nil {
		return err
	}

	grpcServer := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(tlsConf)),
		grpc.UnaryInterceptor(auth.UnaryInterceptor),
		grpc.StreamInterceptor(auth.StreamInterceptor),
	)
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
		w.Shutdown()
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

func loadServerTLS() (*tls.Config, error) {
	caCert, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load server certificate: %w", err)
	}

	tlsConf, err := auth.ServerTLSConfig(caCert, cert)
	if err != nil {
		return nil, fmt.Errorf("failed to build TLS config: %w", err)
	}
	return tlsConf, nil
}
