package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	memorytypes "github.com/kydenul/k-adk/memory/types"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/memory"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/toolconfirmation"
	"google.golang.org/genai"
)

// ---------------------------------------------------------------------------
// Mock: tool.Context
// ---------------------------------------------------------------------------

type mockToolContext struct {
	userID string
}

func (m *mockToolContext) Deadline() (time.Time, bool)                          { return time.Time{}, false }
func (m *mockToolContext) Done() <-chan struct{}                                { return nil }
func (m *mockToolContext) Err() error                                           { return nil }
func (m *mockToolContext) Value(_ any) any                                      { return nil }
func (m *mockToolContext) UserID() string                                       { return m.userID }
func (m *mockToolContext) AppName() string                                      { return "test-app" }
func (m *mockToolContext) SessionID() string                                    { return "sess-1" }
func (m *mockToolContext) InvocationID() string                                 { return "inv-1" }
func (m *mockToolContext) AgentName() string                                    { return "agent" }
func (m *mockToolContext) Branch() string                                       { return "" }
func (m *mockToolContext) UserContent() *genai.Content                          { return nil }
func (m *mockToolContext) ReadonlyState() session.ReadonlyState                 { return nil }
func (m *mockToolContext) Artifacts() agent.Artifacts                           { return nil }
func (m *mockToolContext) State() session.State                                 { return nil }
func (m *mockToolContext) FunctionCallID() string                               { return "fc-1" }
func (m *mockToolContext) Actions() *session.EventActions                       { return &session.EventActions{} }
func (m *mockToolContext) ToolConfirmation() *toolconfirmation.ToolConfirmation { return nil }
func (m *mockToolContext) RequestConfirmation(string, any) error                { return nil }

func (m *mockToolContext) SearchMemory(_ context.Context, _ string) (*memory.SearchResponse, error) {
	return &memory.SearchResponse{}, nil
}

var _ tool.Context = (*mockToolContext)(nil)

func newMockCtx(userID string) *mockToolContext {
	return &mockToolContext{userID: userID}
}

// ---------------------------------------------------------------------------
// Mock: MemoryService (basic)
// ---------------------------------------------------------------------------

type mockMemoryService struct {
	addSessionFn func(ctx context.Context, s session.Session) error
	searchFn     func(ctx context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error)
}

func (m *mockMemoryService) AddSession(ctx context.Context, s session.Session) error {
	if m.addSessionFn != nil {
		return m.addSessionFn(ctx, s)
	}
	return nil
}

func (m *mockMemoryService) Search(ctx context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error) {
	if m.searchFn != nil {
		return m.searchFn(ctx, req)
	}
	return &memory.SearchResponse{}, nil
}

var _ memorytypes.MemoryService = (*mockMemoryService)(nil)

// ---------------------------------------------------------------------------
// Mock: ExtendedMemoryService
// ---------------------------------------------------------------------------

type mockExtendedMemoryService struct {
	mockMemoryService

	searchWithIDFn func(ctx context.Context, req *memory.SearchRequest) ([]memorytypes.EntryWithID, error)
	updateMemoryFn func(ctx context.Context, appName, userID string, entryID int, newContent string) error
	deleteMemoryFn func(ctx context.Context, appName, userID string, entryID int) error
}

func (m *mockExtendedMemoryService) SearchWithID(ctx context.Context, req *memory.SearchRequest) ([]memorytypes.EntryWithID, error) {
	if m.searchWithIDFn != nil {
		return m.searchWithIDFn(ctx, req)
	}
	return nil, nil
}

func (m *mockExtendedMemoryService) UpdateMemory(ctx context.Context, appName, userID string, entryID int, newContent string) error {
	if m.updateMemoryFn != nil {
		return m.updateMemoryFn(ctx, appName, userID, entryID, newContent)
	}
	return nil
}

func (m *mockExtendedMemoryService) DeleteMemory(ctx context.Context, appName, userID string, entryID int) error {
	if m.deleteMemoryFn != nil {
		return m.deleteMemoryFn(ctx, appName, userID, entryID)
	}
	return nil
}

var _ memorytypes.ExtendedMemoryService = (*mockExtendedMemoryService)(nil)

// ===========================================================================
// Tests: NewToolset
// ===========================================================================

func TestNewToolset_NilMemoryService(t *testing.T) {
	_, err := NewToolset(ToolsetConfig{AppName: "app"})
	if err == nil {
		t.Fatal("expected error for nil MemoryService")
	}
}

func TestNewToolset_EmptyAppName(t *testing.T) {
	_, err := NewToolset(ToolsetConfig{MemoryService: &mockMemoryService{}})
	if err == nil {
		t.Fatal("expected error for empty AppName")
	}
}

func TestNewToolset_BasicService_HasTwoTools(t *testing.T) {
	ts, err := NewToolset(ToolsetConfig{
		MemoryService: &mockMemoryService{},
		AppName:       "app",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tools, err := ts.Tools(nil)
	if err != nil {
		t.Fatalf("Tools() error: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools (search, save), got %d", len(tools))
	}

	names := map[string]bool{}
	for _, tt := range tools {
		names[tt.Name()] = true
	}
	for _, want := range []string{"search_memory", "save_to_memory"} {
		if !names[want] {
			t.Errorf("missing tool %q", want)
		}
	}
}

func TestNewToolset_ExtendedService_HasFourTools(t *testing.T) {
	ts, err := NewToolset(ToolsetConfig{
		MemoryService: &mockExtendedMemoryService{},
		AppName:       "app",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tools, _ := ts.Tools(nil)
	if len(tools) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(tools))
	}

	names := map[string]bool{}
	for _, tt := range tools {
		names[tt.Name()] = true
	}
	for _, want := range []string{"search_memory", "save_to_memory", "update_memory", "delete_memory"} {
		if !names[want] {
			t.Errorf("missing tool %q", want)
		}
	}
}

func TestNewToolset_DisableExtendedTools(t *testing.T) {
	ts, err := NewToolset(ToolsetConfig{
		MemoryService:        &mockExtendedMemoryService{},
		AppName:              "app",
		DisableExtendedTools: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tools, _ := ts.Tools(nil)
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools when extended disabled, got %d", len(tools))
	}
}

func TestToolset_Name(t *testing.T) {
	ts, _ := NewToolset(ToolsetConfig{
		MemoryService: &mockMemoryService{},
		AppName:       "app",
	})
	if got := ts.Name(); got != "memory_toolset" {
		t.Errorf("Name() = %q, want %q", got, "memory_toolset")
	}
}

// ===========================================================================
// Tests: searchMemory
// ===========================================================================

func TestSearchMemory_EmptyQuery(t *testing.T) {
	ts := &Toolset{memoryService: &mockMemoryService{}, appName: "app"}

	_, err := ts.searchMemory(newMockCtx("user-1"), SearchArgs{Query: ""})
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestSearchMemory_BasicService_Success(t *testing.T) {
	now := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	svc := &mockMemoryService{
		searchFn: func(_ context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error) {
			if req.AppName != "test-app" {
				t.Errorf("AppName = %q, want %q", req.AppName, "test-app")
			}
			if req.UserID != "user-1" {
				t.Errorf("UserID = %q, want %q", req.UserID, "user-1")
			}
			if req.Query != "hello" {
				t.Errorf("Query = %q, want %q", req.Query, "hello")
			}
			return &memory.SearchResponse{
				Memories: []memory.Entry{
					{
						Content:   &genai.Content{Parts: []*genai.Part{genai.NewPartFromText("memory-1")}},
						Author:    "agent",
						Timestamp: now,
					},
					{
						Content:   &genai.Content{Parts: []*genai.Part{genai.NewPartFromText("memory-2")}},
						Author:    "user",
						Timestamp: now.Add(time.Hour),
					},
				},
			}, nil
		},
	}

	ts := &Toolset{memoryService: svc, appName: "test-app"}
	result, err := ts.searchMemory(newMockCtx("user-1"), SearchArgs{Query: "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Count != 2 {
		t.Fatalf("Count = %d, want 2", result.Count)
	}
	if result.Memories[0].Text != "memory-1" {
		t.Errorf("Memories[0].Text = %q, want %q", result.Memories[0].Text, "memory-1")
	}
	if result.Memories[0].Author != "agent" {
		t.Errorf("Memories[0].Author = %q, want %q", result.Memories[0].Author, "agent")
	}
	if result.Memories[1].Text != "memory-2" {
		t.Errorf("Memories[1].Text = %q, want %q", result.Memories[1].Text, "memory-2")
	}
}

func TestSearchMemory_BasicService_Error(t *testing.T) {
	svc := &mockMemoryService{
		searchFn: func(_ context.Context, _ *memory.SearchRequest) (*memory.SearchResponse, error) {
			return nil, errors.New("db down")
		},
	}

	ts := &Toolset{memoryService: svc, appName: "app"}
	_, err := ts.searchMemory(newMockCtx("u"), SearchArgs{Query: "q"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSearchMemory_BasicService_NilContent(t *testing.T) {
	svc := &mockMemoryService{
		searchFn: func(_ context.Context, _ *memory.SearchRequest) (*memory.SearchResponse, error) {
			return &memory.SearchResponse{
				Memories: []memory.Entry{
					{Content: nil, Author: "a", Timestamp: time.Now()},
					{Content: &genai.Content{Parts: nil}, Author: "b", Timestamp: time.Now()},
					{Content: &genai.Content{Parts: []*genai.Part{}}, Author: "c", Timestamp: time.Now()},
				},
			}, nil
		},
	}

	ts := &Toolset{memoryService: svc, appName: "app"}
	result, err := ts.searchMemory(newMockCtx("u"), SearchArgs{Query: "q"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Count != 3 {
		t.Fatalf("Count = %d, want 3", result.Count)
	}
	for i, e := range result.Memories {
		if e.Text != "" {
			t.Errorf("Memories[%d].Text = %q, want empty", i, e.Text)
		}
	}
}

func TestSearchMemory_BasicService_EmptyResponse(t *testing.T) {
	svc := &mockMemoryService{
		searchFn: func(_ context.Context, _ *memory.SearchRequest) (*memory.SearchResponse, error) {
			return &memory.SearchResponse{Memories: nil}, nil
		},
	}

	ts := &Toolset{memoryService: svc, appName: "app"}
	result, err := ts.searchMemory(newMockCtx("u"), SearchArgs{Query: "q"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Count != 0 {
		t.Errorf("Count = %d, want 0", result.Count)
	}
}

func TestSearchMemory_ExtendedService_Success(t *testing.T) {
	now := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	extSvc := &mockExtendedMemoryService{
		searchWithIDFn: func(_ context.Context, req *memory.SearchRequest) ([]memorytypes.EntryWithID, error) {
			return []memorytypes.EntryWithID{
				{
					ID:        42,
					Content:   &genai.Content{Parts: []*genai.Part{genai.NewPartFromText("found it")}},
					Author:    "agent",
					Timestamp: now,
				},
			}, nil
		},
	}

	ts := &Toolset{
		memoryService:    extSvc,
		extMemoryService: extSvc,
		appName:          "app",
	}

	result, err := ts.searchMemory(newMockCtx("u"), SearchArgs{Query: "q"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Count != 1 {
		t.Fatalf("Count = %d, want 1", result.Count)
	}
	if result.Memories[0].ID != 42 {
		t.Errorf("ID = %d, want 42", result.Memories[0].ID)
	}
	if result.Memories[0].Text != "found it" {
		t.Errorf("Text = %q, want %q", result.Memories[0].Text, "found it")
	}
	if result.Memories[0].Timestamp != "2025-06-01 12:00:00" {
		t.Errorf("Timestamp = %q, want %q", result.Memories[0].Timestamp, "2025-06-01 12:00:00")
	}
}

func TestSearchMemory_ExtendedService_Error(t *testing.T) {
	extSvc := &mockExtendedMemoryService{
		searchWithIDFn: func(_ context.Context, _ *memory.SearchRequest) ([]memorytypes.EntryWithID, error) {
			return nil, errors.New("search failed")
		},
	}

	ts := &Toolset{
		memoryService:    extSvc,
		extMemoryService: extSvc,
		appName:          "app",
	}

	_, err := ts.searchMemory(newMockCtx("u"), SearchArgs{Query: "q"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSearchMemory_ExtendedService_NilContent(t *testing.T) {
	extSvc := &mockExtendedMemoryService{
		searchWithIDFn: func(_ context.Context, _ *memory.SearchRequest) ([]memorytypes.EntryWithID, error) {
			return []memorytypes.EntryWithID{
				{ID: 1, Content: nil, Author: "a", Timestamp: time.Now()},
				{ID: 2, Content: &genai.Content{Parts: nil}, Author: "b", Timestamp: time.Now()},
			}, nil
		},
	}

	ts := &Toolset{memoryService: extSvc, extMemoryService: extSvc, appName: "app"}
	result, err := ts.searchMemory(newMockCtx("u"), SearchArgs{Query: "q"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, e := range result.Memories {
		if e.Text != "" {
			t.Errorf("Memories[%d].Text = %q, want empty", i, e.Text)
		}
	}
}

// ===========================================================================
// Tests: saveToMemory
// ===========================================================================

func TestSaveToMemory_EmptyContent(t *testing.T) {
	ts := &Toolset{memoryService: &mockMemoryService{}, appName: "app"}
	result, err := ts.saveToMemory(newMockCtx("u"), SaveArgs{Content: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected Success=false for empty content")
	}
}

func TestSaveToMemory_Success(t *testing.T) {
	var capturedSession session.Session
	svc := &mockMemoryService{
		addSessionFn: func(_ context.Context, s session.Session) error {
			capturedSession = s
			return nil
		},
	}

	ts := &Toolset{memoryService: svc, appName: "test-app"}
	result, err := ts.saveToMemory(newMockCtx("user-1"), SaveArgs{
		Content:  "remember this",
		Category: "fact",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected Success=true")
	}

	if capturedSession == nil {
		t.Fatal("AddSession was not called")
	}
	if capturedSession.AppName() != "test-app" {
		t.Errorf("AppName = %q, want %q", capturedSession.AppName(), "test-app")
	}
	if capturedSession.UserID() != "user-1" {
		t.Errorf("UserID = %q, want %q", capturedSession.UserID(), "user-1")
	}
}

func TestSaveToMemory_ServiceError(t *testing.T) {
	svc := &mockMemoryService{
		addSessionFn: func(_ context.Context, _ session.Session) error {
			return errors.New("write error")
		},
	}

	ts := &Toolset{memoryService: svc, appName: "app"}
	result, err := ts.saveToMemory(newMockCtx("u"), SaveArgs{Content: "data"})
	if err != nil {
		t.Fatalf("unexpected error (should be in result): %v", err)
	}
	if result.Success {
		t.Error("expected Success=false on service error")
	}
}

func TestSaveToMemory_WithoutCategory(t *testing.T) {
	svc := &mockMemoryService{
		addSessionFn: func(_ context.Context, _ session.Session) error { return nil },
	}
	ts := &Toolset{memoryService: svc, appName: "app"}
	result, err := ts.saveToMemory(newMockCtx("u"), SaveArgs{Content: "data"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected Success=true")
	}
}

// ===========================================================================
// Tests: updateMemory
// ===========================================================================

func TestUpdateMemory_ZeroID(t *testing.T) {
	extSvc := &mockExtendedMemoryService{}
	ts := &Toolset{extMemoryService: extSvc}

	result, err := ts.updateMemory(newMockCtx("u"), UpdateArgs{ID: 0, Content: "new"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected Success=false for zero ID")
	}
}

func TestUpdateMemory_EmptyContent(t *testing.T) {
	extSvc := &mockExtendedMemoryService{}
	ts := &Toolset{extMemoryService: extSvc}

	result, err := ts.updateMemory(newMockCtx("u"), UpdateArgs{ID: 1, Content: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected Success=false for empty content")
	}
}

func TestUpdateMemory_Success(t *testing.T) {
	var (
		gotAppName string
		gotUserID  string
		gotID      int
		gotContent string
	)
	extSvc := &mockExtendedMemoryService{
		updateMemoryFn: func(_ context.Context, appName, userID string, entryID int, newContent string) error {
			gotAppName = appName
			gotUserID = userID
			gotID = entryID
			gotContent = newContent
			return nil
		},
	}

	ts := &Toolset{extMemoryService: extSvc, appName: "my-app"}
	result, err := ts.updateMemory(newMockCtx("alice"), UpdateArgs{ID: 99, Content: "updated text"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected Success=true")
	}
	if gotAppName != "my-app" {
		t.Errorf("appName = %q, want %q", gotAppName, "my-app")
	}
	if gotUserID != "alice" {
		t.Errorf("userID = %q, want %q", gotUserID, "alice")
	}
	if gotID != 99 {
		t.Errorf("entryID = %d, want 99", gotID)
	}
	if gotContent != "updated text" {
		t.Errorf("content = %q, want %q", gotContent, "updated text")
	}
}

func TestUpdateMemory_ServiceError(t *testing.T) {
	extSvc := &mockExtendedMemoryService{
		updateMemoryFn: func(_ context.Context, _, _ string, _ int, _ string) error {
			return errors.New("update failed")
		},
	}

	ts := &Toolset{extMemoryService: extSvc, appName: "app"}
	result, err := ts.updateMemory(newMockCtx("u"), UpdateArgs{ID: 1, Content: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected Success=false on service error")
	}
}

// ===========================================================================
// Tests: deleteMemory
// ===========================================================================

func TestDeleteMemory_ZeroID(t *testing.T) {
	extSvc := &mockExtendedMemoryService{}
	ts := &Toolset{extMemoryService: extSvc}

	result, err := ts.deleteMemory(newMockCtx("u"), DeleteArgs{ID: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected Success=false for zero ID")
	}
}

func TestDeleteMemory_Success(t *testing.T) {
	var (
		gotAppName string
		gotUserID  string
		gotID      int
	)
	extSvc := &mockExtendedMemoryService{
		deleteMemoryFn: func(_ context.Context, appName, userID string, entryID int) error {
			gotAppName = appName
			gotUserID = userID
			gotID = entryID
			return nil
		},
	}

	ts := &Toolset{extMemoryService: extSvc, appName: "my-app"}
	result, err := ts.deleteMemory(newMockCtx("bob"), DeleteArgs{ID: 7})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected Success=true")
	}
	if gotAppName != "my-app" {
		t.Errorf("appName = %q, want %q", gotAppName, "my-app")
	}
	if gotUserID != "bob" {
		t.Errorf("userID = %q, want %q", gotUserID, "bob")
	}
	if gotID != 7 {
		t.Errorf("entryID = %d, want 7", gotID)
	}
}

func TestDeleteMemory_ServiceError(t *testing.T) {
	extSvc := &mockExtendedMemoryService{
		deleteMemoryFn: func(_ context.Context, _, _ string, _ int) error {
			return errors.New("delete failed")
		},
	}

	ts := &Toolset{extMemoryService: extSvc, appName: "app"}
	result, err := ts.deleteMemory(newMockCtx("u"), DeleteArgs{ID: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected Success=false on service error")
	}
}

// ===========================================================================
// Tests: singleEntrySession / singleEntryEvents
// ===========================================================================

func TestSingleEntrySession_Fields(t *testing.T) {
	s := &singleEntrySession{
		id:       "mem-123",
		appName:  "app",
		userID:   "user-1",
		content:  "hello",
		category: "greeting",
	}

	if s.ID() != "mem-123" {
		t.Errorf("ID() = %q", s.ID())
	}
	if s.AppName() != "app" {
		t.Errorf("AppName() = %q", s.AppName())
	}
	if s.UserID() != "user-1" {
		t.Errorf("UserID() = %q", s.UserID())
	}
	if s.State() != nil {
		t.Error("State() should be nil")
	}
	if s.LastUpdateTime().IsZero() {
		t.Error("LastUpdateTime() should not be zero")
	}
}

func TestSingleEntryEvents_All(t *testing.T) {
	e := &singleEntryEvents{content: "test content", category: "note"}

	count := 0
	for ev := range e.All() {
		count++
		if ev == nil {
			t.Fatal("event is nil")
		}
		if ev.Content == nil || len(ev.Content.Parts) == 0 {
			t.Fatal("event Content is empty")
		}
		text := ev.Content.Parts[0].Text
		if text != "[note] test content" {
			t.Errorf("Text = %q, want %q", text, "[note] test content")
		}
		if ev.Author != "agent" {
			t.Errorf("Author = %q, want %q", ev.Author, "agent")
		}
		if ev.Content.Role != "assistant" {
			t.Errorf("Role = %q, want %q", ev.Content.Role, "assistant")
		}
	}
	if count != 1 {
		t.Errorf("All() yielded %d events, want 1", count)
	}
}

func TestSingleEntryEvents_AllWithoutCategory(t *testing.T) {
	e := &singleEntryEvents{content: "plain", category: ""}

	for ev := range e.All() {
		text := ev.Content.Parts[0].Text
		if text != "plain" {
			t.Errorf("Text = %q, want %q", text, "plain")
		}
	}
}

func TestSingleEntryEvents_Len(t *testing.T) {
	e := &singleEntryEvents{content: "x"}
	if e.Len() != 1 {
		t.Errorf("Len() = %d, want 1", e.Len())
	}
}

func TestSingleEntryEvents_At(t *testing.T) {
	e := &singleEntryEvents{content: "data", category: ""}

	ev := e.At(0)
	if ev == nil {
		t.Fatal("At(0) returned nil")
	}
	if ev.Content.Parts[0].Text != "data" {
		t.Errorf("At(0) Text = %q", ev.Content.Parts[0].Text)
	}

	if e.At(1) != nil {
		t.Error("At(1) should be nil")
	}
	if e.At(-1) != nil {
		t.Error("At(-1) should be nil")
	}
}

func TestSingleEntrySession_Events(t *testing.T) {
	s := &singleEntrySession{
		id:       "id",
		appName:  "app",
		userID:   "u",
		content:  "hello",
		category: "",
	}

	events := s.Events()
	if events.Len() != 1 {
		t.Fatalf("Events().Len() = %d, want 1", events.Len())
	}

	ev := events.At(0)
	if ev == nil {
		t.Fatal("At(0) nil")
	}
	if ev.Content.Parts[0].Text != "hello" {
		t.Errorf("Text = %q", ev.Content.Parts[0].Text)
	}
}

func TestSingleEntrySession_EventsWithCategory(t *testing.T) {
	s := &singleEntrySession{
		id:       "id",
		appName:  "app",
		userID:   "u",
		content:  "data",
		category: "pref",
	}

	events := s.Events()
	ev := events.At(0)
	if ev.Content.Parts[0].Text != "[pref] data" {
		t.Errorf("Text = %q, want %q", ev.Content.Parts[0].Text, "[pref] data")
	}
}

// ===========================================================================
// Tests: Verify singleEntrySession implements session.Session
// ===========================================================================

func TestSingleEntrySession_ImplementsSession(t *testing.T) {
	var _ session.Session = (*singleEntrySession)(nil)
}

// ===========================================================================
// Tests: Verify singleEntryEvents implements session.Events
// ===========================================================================

func TestSingleEntryEvents_ImplementsEvents(t *testing.T) {
	var _ session.Events = (*singleEntryEvents)(nil)
}

// ===========================================================================
// Tests: saveToMemory session content verification
// ===========================================================================

func TestSaveToMemory_SessionContentWithCategory(t *testing.T) {
	var capturedEvents session.Events
	svc := &mockMemoryService{
		addSessionFn: func(_ context.Context, s session.Session) error {
			capturedEvents = s.Events()
			return nil
		},
	}

	ts := &Toolset{memoryService: svc, appName: "app"}
	_, err := ts.saveToMemory(newMockCtx("u"), SaveArgs{Content: "my fact", Category: "fact"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedEvents.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", capturedEvents.Len())
	}
	ev := capturedEvents.At(0)
	text := ev.Content.Parts[0].Text
	if text != "[fact] my fact" {
		t.Errorf("Text = %q, want %q", text, "[fact] my fact")
	}
}

func TestSaveToMemory_SessionContentWithoutCategory(t *testing.T) {
	var capturedEvents session.Events
	svc := &mockMemoryService{
		addSessionFn: func(_ context.Context, s session.Session) error {
			capturedEvents = s.Events()
			return nil
		},
	}

	ts := &Toolset{memoryService: svc, appName: "app"}
	_, err := ts.saveToMemory(newMockCtx("u"), SaveArgs{Content: "bare content"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ev := capturedEvents.At(0)
	text := ev.Content.Parts[0].Text
	if text != "bare content" {
		t.Errorf("Text = %q, want %q", text, "bare content")
	}
}

// ===========================================================================
// Tests: searchMemory uses the correct context user ID
// ===========================================================================

func TestSearchMemory_PassesCorrectUserID(t *testing.T) {
	var gotUserID string
	svc := &mockMemoryService{
		searchFn: func(_ context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error) {
			gotUserID = req.UserID
			return &memory.SearchResponse{}, nil
		},
	}

	ts := &Toolset{memoryService: svc, appName: "app"}
	_, _ = ts.searchMemory(newMockCtx("specific-user"), SearchArgs{Query: "q"})

	if gotUserID != "specific-user" {
		t.Errorf("UserID = %q, want %q", gotUserID, "specific-user")
	}
}

// ===========================================================================
// Tests: Extended search prefers SearchWithID over Search
// ===========================================================================

func TestSearchMemory_ExtendedPreferred(t *testing.T) {
	searchCalled := false
	searchWithIDCalled := false

	extSvc := &mockExtendedMemoryService{
		mockMemoryService: mockMemoryService{
			searchFn: func(_ context.Context, _ *memory.SearchRequest) (*memory.SearchResponse, error) {
				searchCalled = true
				return &memory.SearchResponse{}, nil
			},
		},
		searchWithIDFn: func(_ context.Context, _ *memory.SearchRequest) ([]memorytypes.EntryWithID, error) {
			searchWithIDCalled = true
			return nil, nil
		},
	}

	ts := &Toolset{memoryService: extSvc, extMemoryService: extSvc, appName: "app"}
	_, _ = ts.searchMemory(newMockCtx("u"), SearchArgs{Query: "q"})

	if !searchWithIDCalled {
		t.Error("SearchWithID was not called")
	}
	if searchCalled {
		t.Error("basic Search should NOT be called when extMemoryService is set")
	}
}

// ===========================================================================
// Tests: singleEntryEvents All() early-stop
// ===========================================================================

func TestSingleEntryEvents_AllEarlyStop(t *testing.T) {
	e := &singleEntryEvents{content: "x"}

	count := 0
	e.All()(func(_ *session.Event) bool {
		count++
		return false // stop immediately
	})
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
}

// ===========================================================================
// Tests: singleEntryEvents createEvent timestamp
// ===========================================================================

func TestSingleEntryEvents_CreateEventTimestamp(t *testing.T) {
	e := &singleEntryEvents{content: "x"}
	before := time.Now()
	ev := e.createEvent()
	after := time.Now()

	if ev.Timestamp.Before(before) || ev.Timestamp.After(after) {
		t.Errorf("Timestamp %v not between %v and %v", ev.Timestamp, before, after)
	}
}

// ===========================================================================
// Tests: Toolset implements tool.Toolset interface
// ===========================================================================

func TestToolset_ImplementsInterface(t *testing.T) {
	var _ tool.Toolset = (*Toolset)(nil)
}

// ===========================================================================
// Tests: singleEntryEvents implements iter.Seq
// ===========================================================================

func TestSingleEntryEvents_AllIterator(t *testing.T) {
	e := &singleEntryEvents{content: "hello"}

	// Verify we can use it as an iter.Seq
	seq := e.All()
	count := 0
	seq(func(ev *session.Event) bool {
		count++
		return true
	})
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
}
