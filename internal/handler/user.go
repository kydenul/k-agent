package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kydenul/k-agent/internal/service"
	"github.com/kydenul/log"
)

type UserHandler struct {
	userService *service.UserService
}

func NewUserHandler(userService *service.UserService) *UserHandler {
	return &UserHandler{userService: userService}
}

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

	user, err := h.userService.GetUser(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, service.ErrUserNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}

		log.Errorf("failed to get user: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	log.Infof("got user: %v", user)

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

	user, err := h.userService.CreateUser(c.Request.Context(), req.Name, req.Email)
	if err != nil {
		log.Errorf("failed to create user: %v", err)

		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	log.Infof("created user: %v", user)

	c.JSON(http.StatusCreated, gin.H{"data": user})
}

// ListUsers fetches a list of users with pagination.
//
// @Params: page, page_size
//
// @Router: GET /api/v1/users
func (h *UserHandler) ListUsers(c *gin.Context) {
	page := parseQuery[int](c, "page", 1)
	pageSize := parseQuery[int](c, "page_size", 10)

	resp, err := h.userService.ListUsers(c.Request.Context(), page, pageSize)
	if err != nil {
		log.Errorf("failed to list users: %v", err)

		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	log.Infof("listed %d users", len(resp.Users))

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"users":     resp.Users,
			"total":     resp.Total,
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

	ok, err := h.userService.DeleteUser(c.Request.Context(), id)
	if err != nil {
		log.Errorf("failed to delete user: %v", err)

		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	log.Infof("deleted user: %v", id)

	c.JSON(http.StatusOK, gin.H{"success": ok})
}
