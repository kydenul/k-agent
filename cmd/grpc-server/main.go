package main

import (
	"context"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/kydenul/k-agent/config"
	"github.com/kydenul/k-agent/internal/server"
	"github.com/kydenul/k-agent/internal/stores"
	"github.com/kydenul/k-agent/pb/user"
	"github.com/kydenul/log"
	"google.golang.org/grpc"
)

func main() {
	// NOTE: 1. Load Config
	cfg := config.Load()

	// NOTE: 2. Initialize PostgreSQL via Bun
	pgClient, err := stores.NewPostgresClient(context.Background(), &cfg.Postgres)
	if err != nil {
		log.Fatalf("failed to init postgres: %v", err)
	}
	defer pgClient.Close()

	// XXX: Ensure tables exist
	stores.EnsureExistApplicationTable(pgClient, stores.CreateTableUser)

	// NOTE: 3. Redis
	rdb, err := stores.NewRedisClient(&cfg.Redis)
	if err != nil {
		log.Fatalf("failed to init redis: %v", err)
	}
	defer rdb.Close()

	// NOTE: 4. Start gRPC server
	lis, err := net.Listen("tcp", cfg.GRPC.SvrAddr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// NOTE: 5. Wire gRPC Server
	srv := grpc.NewServer()
	user.RegisterUserServiceServer(srv, server.NewUserServer(pgClient, rdb))

	go func() {
		log.Infof("gRPC server listening on %s", cfg.GRPC.SvrAddr)
		if err := srv.Serve(lis); err != nil {
			log.Fatalf("grpc serve error: %v", err)
		}
	}()

	// NOTE: 6. Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Infoln("shutting down gRPC server...")
	srv.GracefulStop()
	log.Infoln("gRPC server stopped")
}
