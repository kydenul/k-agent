package router

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kydenul/k-agent/config"
	"github.com/kydenul/k-agent/internal/handler"
	"github.com/kydenul/k-agent/internal/middleware"
)

func New(cfg *config.HTTP, userHandler *handler.UserHandler) *gin.Engine {
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

	// NOTE: API v1
	v1 := r.Group("/api/v1")
	{
		user := v1.Group("/users")
		{
			// GET /api/v1/users
			user.GET("", userHandler.ListUsers)

			// POST /api/v1/users
			user.POST("", userHandler.CreateUser)

			// GET /api/v1/users/{id}
			user.GET("/:id", userHandler.GetUser)

			// DELETE /api/v1/users/{id}
			user.DELETE("/:id", userHandler.DeleteUser)
		}
	}

	return r
}
