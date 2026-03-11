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
	agentsvc "github.com/kydenul/k-agent/internal/service/agent"
	usersvc "github.com/kydenul/k-agent/internal/service/user"
	"github.com/kydenul/k-agent/internal/stores"
	kmempg "github.com/kydenul/k-agent/pkg/adk-util/memory/postgres"
	ksesspg "github.com/kydenul/k-agent/pkg/adk-util/session/postgres"
	ksess "github.com/kydenul/k-agent/pkg/adk-util/session/redis"
	"github.com/kydenul/log"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/genai"
)

const (
	defaultAppName         = "k-claw"
	defaultRedisSessionTTL = 10 * time.Minute
)

func main() {
	// Create context that cancels on interrupt signal (Ctrl+C)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// NOTE: 1. Load Config
	cfg := config.Load()
	gin.SetMode(cfg.HTTP.GinMode)

	// NOTE: 2. Initialize PostgreSQL
	pgClient, err := stores.NewPostgresClient(ctx, &cfg.Postgres)
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

	// Create Gemini model
	model, err := gemini.NewModel(ctx, "gemini-2.5-flash", &genai.ClientConfig{
		APIKey: os.Getenv("GOOGLE_API_KEY"),
	})
	if err != nil {
		log.Fatalf("Failed to create model: %v", err)
	}

	// Create LLMAgent
	a, err := llmagent.New(llmagent.Config{
		Name:        defaultAppName,
		Model:       model,
		Description: "A helpful assistant powered by Gin and ADK.",
		Instruction: `You are a helpful assistant. You can help users with various tasks.
When asked about weather, use the get_weather tool to get current weather information.
Always be polite and helpful.`,
		// Tools: []tool.Tool{getWeatherTool},
	})
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	// Create Loader
	loader := agent.NewSingleLoader(a)

	// TODO: initialize ADK dependencies (agent.Loader, session.Service, memory.Service)
	// Create session persister (handles async persistence)
	pgPersister, err := ksesspg.NewSessionPersister(ctx, pgClient)
	if err != nil {
		log.Fatalf("Failed to create session persister: %v", err)
	}
	defer pgPersister.Close()

	// Create session service
	sessSvc, err := ksess.NewRedisSessionService(rdb,
		ksess.WithTTL(defaultRedisSessionTTL), ksess.WithPersister(pgPersister))
	if err != nil {
		log.Fatalf("Failed to create session service: %v", err)
	}

	// Create memory service
	memSvc, err := kmempg.NewPostgresMemoryService(ctx, pgClient, kmempg.WithAsyncBufferSize(100))
	if err != nil {
		log.Fatalf("Failed to create memory service: %v", err)
	}
	defer memSvc.Close()

	// NOTE: 4. Wire Services, Handlers, and Router
	r := router.New(&cfg.HTTP,
		handler.NewSessionHandler(usersvc.NewUserService(sessSvc)),
		handler.NewAgentHandler(agentsvc.NewServer(loader, sessSvc, memSvc)),
	)

	// NOTE: 5. Start HTTP Server with Graceful Shutdown
	srv := &http.Server{
		Addr:         cfg.HTTP.Port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 0, // Disabled for SSE; per-route timeouts via middleware
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Infof("HTTP server listening on %s", cfg.HTTP.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	<-ctx.Done()
	log.Info("Shutting down server...")

	log.Infoln("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("server forced to shutdown: %v", err)
	}
	log.Infoln("server exited gracefully")
}
