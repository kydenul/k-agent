package models

import (
	"fmt"
	"maps"
	"time"

	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// RunAgentRequest
type RunAgentRequest struct {
	AppName    string         `json:"appName"`
	UserID     string         `json:"userId"`
	SessionID  string         `json:"sessionId"`
	NewMessage genai.Content  `json:"newMessage"`
	Streaming  bool           `json:"streaming,omitempty"`
	StateDelta map[string]any `json:"stateDelta,omitempty"`
}

// Event
type Event struct {
	ID                 string                   `json:"id"`
	Time               int64                    `json:"time"`
	InvocationID       string                   `json:"invocationId"`
	Branch             string                   `json:"branch"`
	Author             string                   `json:"author"`
	Partial            bool                     `json:"partial"`
	LongRunningToolIDs []string                 `json:"longRunningToolIds"`
	Content            *genai.Content           `json:"content"`
	GroundingMetadata  *genai.GroundingMetadata `json:"groundingMetadata"`
	TurnComplete       bool                     `json:"turnComplete"`
	Interrupted        bool                     `json:"interrupted"`
	ErrorCode          string                   `json:"errorCode"`
	ErrorMessage       string                   `json:"errorMessage"`
	Actions            EventActions             `json:"actions"`
}

// EventActions represents actions performed during an event.
type EventActions struct {
	StateDelta        map[string]any   `json:"stateDelta"`
	ArtifactDelta     map[string]int64 `json:"artifactDelta"`
	SkipSummarization bool             `json:"skipSummarization,omitempty"`
}

// fromSessionEvent converts a session.Event to an API Event.
func FromSessionEvent(e *session.Event) *Event {
	return &Event{
		ID:                 e.ID,
		Time:               e.Timestamp.Unix(),
		InvocationID:       e.InvocationID,
		Branch:             e.Branch,
		Author:             e.Author,
		Partial:            e.Partial,
		LongRunningToolIDs: e.LongRunningToolIDs,
		Content:            e.Content,
		GroundingMetadata:  e.GroundingMetadata,
		TurnComplete:       e.TurnComplete,
		Interrupted:        e.Interrupted,
		ErrorCode:          e.ErrorCode,
		ErrorMessage:       e.ErrorMessage,
		Actions: EventActions{
			StateDelta:    e.Actions.StateDelta,
			ArtifactDelta: e.Actions.ArtifactDelta,
		},
	}
}

// Session represents a session in the API response.
type Session struct {
	ID        string         `json:"id"`
	AppName   string         `json:"appName"`
	UserID    string         `json:"userId"`
	UpdatedAt int64          `json:"lastUpdateTime"`
	Events    []*Event       `json:"events"`
	State     map[string]any `json:"state"`
}

func (s *Session) String() string {
	return fmt.Sprintf("ID: %s, AppName: %s, UserID: %s, UpdatedAt: %d, Events: %v, State: %v",
		s.ID, s.AppName, s.UserID, s.UpdatedAt, s.Events, s.State)
}

// fromSession converts a session.Session to an API Session.
func FromSession(s session.Session) *Session {
	state := maps.Collect(s.State().All())

	events := make([]*Event, 0)
	for e := range s.Events().All() {
		events = append(events, FromSessionEvent(e))
	}

	return &Session{
		ID:        s.ID(),
		AppName:   s.AppName(),
		UserID:    s.UserID(),
		UpdatedAt: s.LastUpdateTime().Unix(),
		Events:    events,
		State:     state,
	}
}

type ListSessionsRequest struct {
	AppName string `json:"app_name"`
	UserID  string `json:"user_id"`
}

func (req *ListSessionsRequest) Validate() error {
	if req.AppName == "" || req.UserID == "" {
		return ErrMissingAppNameOrUserID
	}

	return nil
}

type CreateSessionRequest struct {
	AppName string `json:"app_name"`
	UserID  string `json:"user_id"`

	// Optional
	SessionID string `json:"session_id"`
}

func (req *CreateSessionRequest) Validate() error {
	if req.AppName == "" || req.UserID == "" {
		return ErrMissingAppNameOrUserID
	}

	return nil
}

// CreateSessionBody is the request body for creating a session.
type CreateSessionBody struct {
	State  map[string]any `json:"state,omitempty"`
	Events []Event        `json:"events,omitempty"`
}

// toSessionEvent converts an API Event to a session.Event.
func ToSessionEvent(e Event) *session.Event {
	return &session.Event{
		ID:                 e.ID,
		Timestamp:          time.Unix(e.Time, 0),
		InvocationID:       e.InvocationID,
		Branch:             e.Branch,
		Author:             e.Author,
		LongRunningToolIDs: e.LongRunningToolIDs,
		LLMResponse: model.LLMResponse{
			Content:           e.Content,
			GroundingMetadata: e.GroundingMetadata,
			Partial:           e.Partial,
			TurnComplete:      e.TurnComplete,
			Interrupted:       e.Interrupted,
			ErrorCode:         e.ErrorCode,
			ErrorMessage:      e.ErrorMessage,
		},
		Actions: session.EventActions{
			StateDelta:    e.Actions.StateDelta,
			ArtifactDelta: e.Actions.ArtifactDelta,
		},
	}
}

type GetSessionRequest struct {
	AppName   string `json:"app_name"`
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id"`
}

func (req *GetSessionRequest) Validate() error {
	if req.AppName == "" || req.UserID == "" || req.SessionID == "" {
		return ErrMissingAppNameOrUserIDOrSessionID
	}

	return nil
}

type DeleteSessionRequest struct {
	AppName   string `json:"app_name"`
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id"`
}

func (req *DeleteSessionRequest) Validate() error {
	if req.AppName == "" || req.UserID == "" || req.SessionID == "" {
		return ErrMissingAppNameOrUserIDOrSessionID
	}

	return nil
}
