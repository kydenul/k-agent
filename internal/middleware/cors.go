package middleware

// 跨域资源共享

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// CORS returns a middleware function that sets CORS headers.
// Restrict AllowOrigins in production.
func CORS() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Header("Access-Control-Allow-Origin", "*")
		ctx.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		ctx.Header("Access-Control-Allow-Methods", "POST, GET, DELETE, PUT, OPTIONS")

		if ctx.Request.Method == http.MethodOptions {
			ctx.AbortWithStatus(http.StatusNoContent)
			return
		}

		ctx.Next()
	}
}
