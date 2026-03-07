package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kydenul/log"
)

// LoggerConfig defines the config for Logger middleware.
type LoggerConfig struct {
	SkipPaths []string
}

// Logger returns a middleware function that logs HTTP requests.
func Logger() gin.HandlerFunc { return LoggerWithConfig(LoggerConfig{}) }

// LoggerWithConfig returns a middleware function that logs HTTP requests with given config.
func LoggerWithConfig(config LoggerConfig) gin.HandlerFunc {
	skipPaths := make(map[string]struct{}, len(config.SkipPaths))
	for _, path := range config.SkipPaths {
		skipPaths[path] = struct{}{}
	}

	return func(ctx *gin.Context) {
		start := time.Now()
		path := ctx.Request.URL.Path
		raw := ctx.Request.URL.RawQuery

		ctx.Next()

		// Skip logging for skipped paths
		if _, ok := skipPaths[path]; ok {
			return
		}

		latency := time.Since(start)
		clientIP := ctx.ClientIP()
		method := ctx.Request.Method
		statusCode := ctx.Writer.Status()
		bodySize := ctx.Writer.Size()
		errorMessage := ctx.Errors.ByType(gin.ErrorTypePrivate).String()

		if raw != "" {
			path = path + "?" + raw
		}

		switch {
		case statusCode >= 500:
			log.Errorf("[GIN] %3d | %13v | %15s | %-7s | %7d | %s | %s",
				statusCode, latency, clientIP, method, bodySize, path, errorMessage)

		case statusCode >= 400:
			log.Warnf("[GIN] %3d | %13v | %15s | %-7s | %7d | %s | %s",
				statusCode, latency, clientIP, method, bodySize, path, errorMessage)

		default:
			log.Infof("[GIN] %3d | %13v | %15s | %-7s | %7d | %s",
				statusCode, latency, clientIP, method, bodySize, path)
		}
	}
}
