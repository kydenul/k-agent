package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/spf13/cast"
)

func parseQuery[T int | int32](c *gin.Context, key string, defaultVal T) T {
	raw := c.Query(key)
	if raw == "" {
		return defaultVal
	}

	val, err := cast.ToE[T](raw)
	if err != nil {
		return defaultVal
	}

	return val
}
