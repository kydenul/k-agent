package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kydenul/k-agent/internal/models"
	usersvc "github.com/kydenul/k-agent/internal/service/user"
)

type SessionHandler struct {
	userSvc *usersvc.UserService
}

func NewSessionHandler(usersvc *usersvc.UserService) *SessionHandler {
	return &SessionHandler{userSvc: usersvc}
}

// ListSessionsHandler lists all sessions for a user.
//
// GET /apps/:app_name/users/:user_id/sessions
func (h *SessionHandler) ListSessionsHandler(c *gin.Context) {
	// NOTE: handle the parameters in the request
	req := &models.ListSessionsRequest{
		AppName: c.Param("app_name"),
		UserID:  c.Param("user_id"),
	}

	if err := req.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// NOTE: Call service to handle the request
	events, code, err := h.userSvc.ListSessions(c.Request.Context(), req)
	if err != nil {
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}

	// NOTE: Return the response
	c.JSON(code, events)
}

// CreateSessionHandler handles session creation.
//
// POST /apps/:app_name/users/:user_id/sessions
//
// POST /apps/:app_name/users/:user_id/sessions/:session_id
func (h *SessionHandler) CreateSessionHandler(c *gin.Context) {
	// NOTE: handle the parameters in the request
	req := &models.CreateSessionRequest{
		AppName:   c.Param("app_name"),
		UserID:    c.Param("user_id"),
		SessionID: c.Param("session_id"),
	}

	if err := req.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var body models.CreateSessionBody
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	// NOTE: Call service to handle the request
	session, code, err := h.userSvc.CreateSession(c.Request.Context(), req, &body)
	if err != nil {
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}

	// NOTE: Return the response
	c.JSON(code, session)
}

// GetSessionHandler retrieves a specific session.
// GET /apps/:app_name/users/:user_id/sessions/:session_id
func (h *SessionHandler) GetSessionHandler(c *gin.Context) {
	// NOTE: handle the parameters in the request
	req := &models.GetSessionRequest{
		AppName:   c.Param("app_name"),
		UserID:    c.Param("user_id"),
		SessionID: c.Param("session_id"),
	}

	if err := req.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// NOTE: Call service to handle the request
	session, code, err := h.userSvc.GetSession(c.Request.Context(), req)
	if err != nil {
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}

	c.JSON(code, session)
}

// DeleteSessionHandler deletes a specific session.
// DELETE /apps/:app_name/users/:user_id/sessions/:session_id
func (h *SessionHandler) DeleteSessionHandler(c *gin.Context) {
	// NOTE: handle the parameters in the request
	req := &models.DeleteSessionRequest{
		AppName:   c.Param("app_name"),
		UserID:    c.Param("user_id"),
		SessionID: c.Param("session_id"),
	}

	if err := req.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// NOTE: Call service to handle the request
	code, err := h.userSvc.DeleteSession(c.Request.Context(), req)
	if err != nil {
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusNoContent)
}
