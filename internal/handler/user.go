package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kydenul/k-agent/internal/client"
	"github.com/kydenul/log"
)

// UserHandler holds the gRPC client dependency.
type UserHandler struct {
	userClient *client.UserClient
}

// NewUserHandler constructs a handler with an injected gRPC client.
func NewUserHandler(userClient *client.UserClient) *UserHandler {
	return &UserHandler{userClient: userClient}
}

// rpcCtx returns a context with a reasonable RPC deadline.
func rpcCtx() (context.Context, context.CancelFunc) {
	//nolint:gosec
	return context.WithTimeout(context.Background(), 5*time.Second)
}

// ========== Handlers ==========

// GetUser fetches a user by ID.
//
// @Router: GET /api/v1/users/{id}
func (h *UserHandler) GetUser(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		log.Errorf("id is required")

		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	ctx, cancel := rpcCtx()
	defer cancel()

	user, err := h.userClient.GetUser(ctx, id)
	if err != nil {
		log.Errorf("failed to get user: %v", err)

		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	if user == nil {
		log.Errorf("user not found")

		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	log.Infof("✅ got user: %v", user)

	c.JSON(http.StatusOK, gin.H{"data": user})
}

type createUserReq struct {
	Name  string `binding:"required,min=2" json:"name"`
	Email string `binding:"required,email" json:"email"`
}

// CreateUser creates a new user.
//
// @Router: POST /api/v1/users
func (h *UserHandler) CreateUser(c *gin.Context) {
	var req createUserReq
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Errorf("failed to bind request: %v", err)

		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := rpcCtx()
	defer cancel()

	user, err := h.userClient.CreateUser(ctx, req.Name, req.Email)
	if err != nil {
		log.Errorf("failed to create user: %v", err)

		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	log.Infof("✅ created user: %v", user)

	c.JSON(http.StatusCreated, gin.H{"data": user})
}

// ListUsers fetches a list of users with pagination.
//
// @Params: page, page_size
//
// @Router: GET /api/v1/users
func (h *UserHandler) ListUsers(c *gin.Context) {
	page := parseQuery[int32](c, "page", 1)
	pageSize := parseQuery[int32](c, "page_size", 10)

	ctx, cancel := rpcCtx()
	defer cancel()

	users, total, err := h.userClient.ListUsers(ctx, page, pageSize)
	if err != nil {
		log.Errorf("failed to list users: %v", err)

		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	log.Infof("✅ listed users: %v", users)

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"users":     users,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
	})
}

// DeleteUser deletes a user by ID.
//
// @Router: DELETE /api/v1/users/{id}
func (h *UserHandler) DeleteUser(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		log.Errorf("id is required")

		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	ctx, cancel := rpcCtx()
	defer cancel()

	ok, err := h.userClient.DeleteUser(ctx, id)
	if err != nil {
		log.Errorf("failed to delete user: %v", err)

		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	log.Infof("✅ deleted user: %v", id)

	c.JSON(http.StatusOK, gin.H{"success": ok})
}
