package agent

import (
	"context"
	"fmt"
	"iter"
	"net/http"

	"github.com/kydenul/k-agent/internal/models"
	"github.com/kydenul/log"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/memory"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
)

// Server holds the dependencies for HTTP handlers.
type AgentService struct {
	loader     agent.Loader
	memorySvc  memory.Service
	sessionSvc session.Service
}

// ============================================================================
// Server implementation
// ============================================================================

// NewServer creates a new Server with the given agent loader and session service.
func NewServer(
	agentLoader agent.Loader,
	sessSvc session.Service,
	memSvc memory.Service,
) *AgentService {
	return &AgentService{
		loader:     agentLoader,
		memorySvc:  memSvc,
		sessionSvc: sessSvc,
	}
}

// prepareRunner validates the session, loads the agent, and creates a runner.
func (svc *AgentService) prepareRunner(
	ctx context.Context,
	req *models.RunAgentRequest,
) (*runner.Runner, int, error) {
	// Validate session exists
	_, err := svc.sessionSvc.Get(ctx, &session.GetRequest{
		AppName:   req.AppName,
		UserID:    req.UserID,
		SessionID: req.SessionID,
	})
	if err != nil {
		log.Error("session not found", err)

		return nil, http.StatusNotFound, fmt.Errorf("session not found: %w", err)
	}

	// Load agent
	curAgent, err := svc.loader.LoadAgent(req.AppName)
	if err != nil {
		log.Error("failed to load agent", err)

		return nil, http.StatusInternalServerError, fmt.Errorf("failed to load agent: %w", err)
	}

	// Create runner
	r, err := runner.New(runner.Config{
		AppName:        req.AppName,
		Agent:          curAgent,
		MemoryService:  svc.memorySvc,
		SessionService: svc.sessionSvc,
	})
	if err != nil {
		log.Error("failed to create runner", err)

		return nil, http.StatusInternalServerError, fmt.Errorf("failed to create runner: %w", err)
	}

	return r, http.StatusOK, nil
}

// Run executes the agent and collects all events synchronously.
func (svc *AgentService) Run(
	ctx context.Context,
	req *models.RunAgentRequest,
) ([]*models.Event, int, error) {
	r, code, err := svc.prepareRunner(ctx, req)
	if err != nil {
		return nil, code, err
	}

	// Determine streaming mode
	streamingMode := agent.StreamingModeNone
	if req.Streaming {
		streamingMode = agent.StreamingModeSSE
	}

	// Run and collect events
	var events []*models.Event
	for event, err := range r.Run(ctx, req.UserID, req.SessionID, &req.NewMessage,
		agent.RunConfig{StreamingMode: streamingMode}) {
		if err != nil {
			log.Error("runner error", err)

			return nil, http.StatusInternalServerError, fmt.Errorf("runner error: %w", err)
		}
		events = append(events, models.FromSessionEvent(event))
	}

	// Persist session to memory for cross-session search
	if err := svc.addSessionToMemory(ctx, req.AppName, req.UserID, req.SessionID); err != nil {
		log.Errorf("failed to persist session to memory: %v", err)
	}

	return events, http.StatusOK, nil
}

// RunSSE prepares the runner and returns an event iterator for SSE streaming.
// The caller is responsible for iterating and writing SSE frames.
func (svc *AgentService) RunSSE(
	ctx context.Context,
	req *models.RunAgentRequest,
) (iter.Seq2[*models.Event, error], int, error) {
	r, code, err := svc.prepareRunner(ctx, req)
	if err != nil {
		return nil, code, err
	}

	eventIter := func(yield func(*models.Event, error) bool) {
		for event, err := range r.Run(
			ctx, req.UserID, req.SessionID, &req.NewMessage,
			agent.RunConfig{StreamingMode: agent.StreamingModeSSE},
		) {
			if err != nil {
				log.Error("runner SSE error", err)
				if !yield(&models.Event{}, err) {
					return
				}

				continue
			}

			if !yield(models.FromSessionEvent(event), nil) {
				return
			}
		}
	}

	return eventIter, http.StatusOK, nil
}

// PostRun persists session to memory. Should be called after streaming completes.
func (svc *AgentService) PostRun(ctx context.Context, req *models.RunAgentRequest) error {
	log.Infof("post-run: %s/%s/%s", req.AppName, req.UserID, req.SessionID)

	return svc.addSessionToMemory(ctx, req.AppName, req.UserID, req.SessionID)
}

// addSessionToMemory re-fetches the session (which now includes the latest events)
// and persists it to the memory service for cross-session search.
func (svc *AgentService) addSessionToMemory(
	ctx context.Context,
	appName, userID, sessionID string,
) error {
	if svc.memorySvc == nil {
		return nil
	}

	resp, err := svc.sessionSvc.Get(ctx, &session.GetRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err != nil {
		log.Errorf("failed to get session for memory: %v", err)
		return fmt.Errorf("failed to get session for memory: %w", err)
	}

	if err := svc.memorySvc.AddSession(ctx, resp.Session); err != nil {
		log.Errorf("failed to add session to memory: %v", err)
		return fmt.Errorf("failed to add session to memory: %w", err)
	}

	return nil
}
