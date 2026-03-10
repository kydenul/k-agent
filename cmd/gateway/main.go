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
	"github.com/kydenul/k-agent/internal/handler"
	"github.com/kydenul/k-agent/internal/router"
	"github.com/kydenul/k-agent/internal/service"
	"github.com/kydenul/k-agent/internal/stores"
	"github.com/kydenul/log"
)

func main() {
	// NOTE: 1. Load Config
	cfg := config.Load()
	gin.SetMode(cfg.HTTP.GinMode)

	// NOTE: 2. Initialize PostgreSQL
	pgClient, err := stores.NewPostgresClient(context.Background(), &cfg.Postgres)
	if err != nil {
		log.Fatalf("failed to init postgres: %v", err)
	}
	defer pgClient.Close()

	stores.EnsureExistApplicationTable(pgClient, stores.CreateTableUser)

	// NOTE: 3. Initialize Redis
	rdb, err := stores.NewRedisClient(&cfg.Redis)
	if err != nil {
		log.Fatalf("failed to init redis: %v", err)
	}
	defer rdb.Close()

	// NOTE: 4. Wire Services, Handlers, and Router
	userService := service.NewUserService(pgClient, rdb)
	userHandler := handler.NewUserHandler(userService)
	r := router.New(&cfg.HTTP, userHandler)

	// NOTE: 5. Start HTTP Server with Graceful Shutdown
	srv := &http.Server{
		Addr:         cfg.HTTP.Port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Infof("HTTP server listening on %s", cfg.HTTP.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Infoln("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("server forced to shutdown: %v", err)
	}
	log.Infoln("server exited gracefully")
}
