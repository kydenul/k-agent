package handler

import (
	"net/http"

	"github.com/bytedance/sonic"
	"github.com/gin-gonic/gin"
	"github.com/kydenul/k-agent/internal/models"
	agentsvc "github.com/kydenul/k-agent/internal/service/agent"
	"github.com/kydenul/log"
)

type AgentHandler struct {
	agentSvc *agentsvc.AgentService
}

func NewAgentHandler(agentSvc *agentsvc.AgentService) *AgentHandler {
	return &AgentHandler{agentSvc: agentSvc}
}

// handleRun handles the /run endpoint (compatible with ADK REST API).
// POST /run
// Request: RunAgentRequest
// Response: []Event
func (h *AgentHandler) HandleRun(c *gin.Context) {
	// NOTE: handle the parameters in the request
	var req models.RunAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.AppName == "" || req.UserID == "" || req.SessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "appName, userId, and sessionId are required"})
		return
	}

	ctx := c.Request.Context()

	// NOTE: Call service to handle the request
	events, code, err := h.agentSvc.Run(ctx, &req)
	if err != nil {
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}

	// NOTE: Return the response
	c.JSON(code, events)
}

// HandleRunSSE handles the /run_sse endpoint with Server-Sent Events.
// POST /run_sse
// Request: RunAgentRequest
// Response: SSE stream of Event objects
func (h *AgentHandler) HandleRunSSE(c *gin.Context) {
	// NOTE: handle the parameters in the request
	var req models.RunAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.AppName == "" || req.UserID == "" || req.SessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "appName, userId, and sessionId are required"})
		return
	}

	ctx := c.Request.Context()

	// NOTE: Call service to get event iterator
	eventIter, code, err := h.agentSvc.RunSSE(ctx, &req)
	if err != nil {
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}

	// NOTE: Return response via SSE stream
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(http.StatusOK)

	for event, err := range eventIter {
		// Check if client disconnected
		if ctx.Err() != nil {
			break
		}

		// Non-Normal events
		if err != nil {
			errJSON, _ := sonic.Marshal(gin.H{"error": err.Error()})
			frame := "data: " + string(errJSON) + "\n\n"
			if _, writeErr := c.Writer.Write([]byte(frame)); writeErr != nil {
				break
			}
			c.Writer.Flush()

			continue
		}

		// Normal events
		eventJSON, err := sonic.Marshal(event)
		if err != nil {
			errJSON, _ := sonic.Marshal(gin.H{"error": err.Error()})
			frame := "data: " + string(errJSON) + "\n\n"
			if _, writeErr := c.Writer.Write([]byte(frame)); writeErr != nil {
				break
			}
			c.Writer.Flush()

			continue
		}

		frame := "data: " + string(eventJSON) + "\n\n"
		if _, writeErr := c.Writer.Write([]byte(frame)); writeErr != nil {
			break
		}
		c.Writer.Flush()
	}

	// NOTE: Post-processing after stream completes
	if err := h.agentSvc.PostRun(ctx, &req); err != nil {
		log.Errorf("post-run failed: %v", err)
	}
}
