package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/example/pci-infra/internal/config"
	"github.com/example/pci-infra/pkg/audit"
	pb "github.com/example/pci-infra/pkg/audit/proto"
)

type server struct {
	pb.UnimplementedAuditServiceServer
	chainLogger *audit.ChainLogger
}

func (s *server) Ingest(ctx context.Context, req *pb.IngestRequest) (*pb.IngestResponse, error) {
	for _, entry := range req.Entries {
		s.chainLogger.Append(entry.Payload)
	}

	// In a real implementation, we would sign the batch using KMS (cfg.KMSSigner)
	// and forward to CloudWatch (cfg.AuditSink).
	// For now, we simulate success.

	return &pb.IngestResponse{
		Success: true,
		BatchId: "batch-" + time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func main() {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	log.Printf("Starting auditd in %s environment", cfg.Environment)

	lis, err := net.Listen("tcp", ":50052")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	auditServer := &server{
		chainLogger: audit.NewChainLogger(),
	}

	pb.RegisterAuditServiceServer(s, auditServer)
	reflection.Register(s)

	log.Printf("server listening at %v", lis.Addr())

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	
	go func() {
		<-sigCh
		log.Println("Shutting down...")
		s.GracefulStop()
	}()

	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
