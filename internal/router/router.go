package router

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kydenul/k-agent/config"
	"github.com/kydenul/k-agent/internal/handler"
	"github.com/kydenul/k-agent/internal/middleware"
)

func New(
	cfg *config.HTTP,
	sessionHandler *handler.SessionHandler,
	agentHandler *handler.AgentHandler,
) *gin.Engine {
	r := gin.New()

	// NOTE: Global Middleware
	r.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		SkipPaths: []string{
			"/health",
		},
	}))
	r.Use(middleware.CORS(cfg.CORS.AllowOrigins))
	r.Use(middleware.Recovery())

	// NOTE: Health check
	r.GET("/health", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// NOTE: Session API
	r.GET("/apps/:app_name/users/:user_id/sessions/:session_id",
		sessionHandler.GetSessionHandler)
	r.GET("/apps/:app_name/users/:user_id/sessions",
		sessionHandler.ListSessionsHandler)
	r.POST("/apps/:app_name/users/:user_id/sessions",
		sessionHandler.CreateSessionHandler)
	r.POST("/apps/:app_name/users/:user_id/sessions/:session_id",
		sessionHandler.CreateSessionHandler)
	r.DELETE("/apps/:app_name/users/:user_id/sessions/:session_id",
		sessionHandler.DeleteSessionHandler)

	// Agent Runtime API

	// POST /run
	r.POST("/run", agentHandler.HandleRun)
	r.OPTIONS("/run", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	// POST /run_sse
	r.POST("/run_sse", agentHandler.HandleRunSSE)
	r.OPTIONS("/run_sse", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	return r
}
