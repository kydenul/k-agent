package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kydenul/k-agent/config"
	"github.com/kydenul/k-agent/internal/client"
	"github.com/kydenul/k-agent/internal/handler"
	"github.com/kydenul/k-agent/internal/router"
	"github.com/kydenul/log"
)

func main() {
	// NOTE: 1. Load Config
	cfg := config.Load()
	gin.SetMode(cfg.HTTP.GinMode)

	// NOTE: 2. Build gRPC Clients
	userClient, err := client.NewUserClient(cfg.GRPC.SvrAddr)
	if err != nil {
		log.Fatalf("failed to create user gRPC client: %v", err)
	}
	defer userClient.Close()

	log.Infof("✅ connected to gRPC service @ %s", cfg.GRPC.SvrAddr)

	// NOTE: 3. Wire Handlers and Router
	userHandler := handler.NewUserHandler(userClient)
	r := router.New(userHandler)

	// NOTE: 4. Start HTTP Server with Gra
	srv := &http.Server{
		Addr:         cfg.HTTP.Port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Infof("🚀 HTTP gateway listening on %s", cfg.HTTP.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Infoln("🛑 shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("server forced to shutdown: %v", err)
	}
	log.Infoln("✅ server exited gracefully")
}
