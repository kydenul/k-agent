package user

import (
	"context"
	"fmt"
	"net/http"

	"github.com/kydenul/k-agent/internal/models"
	"github.com/kydenul/log"
	"google.golang.org/adk/session"
)

// UserService holds the session and memory stores
type UserService struct {
	sessionSvc session.Service
}

func NewUserService(sessionSvc session.Service) *UserService {
	return &UserService{
		sessionSvc: sessionSvc,
	}
}

// handleListSessions lists all sessions for a user.
// GET /apps/:app_name/users/:user_id/sessions
func (svc *UserService) ListSessions(
	ctx context.Context,
	req *models.ListSessionsRequest,
) ([]*models.Session, int, error) {
	resp, err := svc.sessionSvc.List(ctx, &session.ListRequest{
		AppName: req.AppName,
		UserID:  req.UserID,
	})
	if err != nil {
		log.Errorf("failed to list sessions: %v", err)

		return nil, http.StatusInternalServerError, fmt.Errorf("failed to list sessions: %w", err)
	}

	sessions := make([]*models.Session, 0, len(resp.Sessions))
	for _, sess := range resp.Sessions {
		sessions = append(sessions, models.FromSession(sess))
	}

	log.Infof("list sessions, count: %d, sessions: %v", len(sessions), sessions)

	return sessions, http.StatusOK, nil
}

func (svc *UserService) CreateSession(
	ctx context.Context,
	req *models.CreateSessionRequest,
	body *models.CreateSessionBody,
) (*models.Session, int, error) {
	resp, err := svc.sessionSvc.Create(ctx, &session.CreateRequest{
		AppName:   req.AppName,
		UserID:    req.UserID,
		SessionID: req.SessionID,

		State: body.State,
	})
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}

	// Append initial events if provided
	for _, event := range body.Events {
		if err := svc.sessionSvc.AppendEvent(
			ctx,
			resp.Session,
			models.ToSessionEvent(event),
		); err != nil {
			return nil, http.StatusInternalServerError,
				fmt.Errorf("failed to append event: %w", err)
		}
	}

	return models.FromSession(resp.Session), http.StatusOK, nil
}
