package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"iter"
	"os"
	"testing"
	"time"

	"github.com/bytedance/sonic"
	_ "github.com/lib/pq" // PostgreSQL driver
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"google.golang.org/adk/session"
)

func getTestConnString() string {
	/*
	  docker run -d \
	    --name postgres-test \
	    -e POSTGRES_PASSWORD=postgres \
	    -p 5432:5432 \
	    postgres:latest
	*/

	if connStr := os.Getenv("TEST_POSTGRES_CONN_STRING"); connStr != "" {
		return connStr
	}
	return "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"
}

// testClient implements PostgresClient for integration tests.
type testClient struct {
	db *bun.DB
}

func newTestClient(connStr string) (*testClient, error) {
	sqldb, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db := bun.NewDB(sqldb, pgdialect.New())
	if err := db.Ping(); err != nil {
		_ = sqldb.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &testClient{db: db}, nil
}

func (c *testClient) DB() *bun.DB {
	return c.db
}

func (c *testClient) Close() error {
	return c.db.Close()
}

func setupTestDB(t *testing.T) (*SessionPersister, *testClient) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := newTestClient(getTestConnString())
	if err != nil {
		t.Skipf("PostgreSQL not available, skipping test: %v", err)
		return nil, nil
	}

	persister, err := NewSessionPersister(ctx, client, WithShardCount(4))
	if err != nil {
		_ = client.Close()
		t.Fatalf("Failed to create persister: %v", err)
	}

	// Clean up test data
	_, err = client.db.NewRaw("DELETE FROM sessions WHERE app_name LIKE 'test_%'").Exec(ctx)
	if err != nil {
		t.Logf("Warning: failed to clean up sessions: %v", err)
	}

	// Clean up all event shards
	for i := range persister.shardCount {
		query := fmt.Sprintf("DELETE FROM session_events_%d WHERE app_name LIKE 'test_%%'", i)
		_, _ = client.db.NewRaw(query).Exec(ctx)
	}

	return persister, client
}

// mockSession implements session.Session for testing
type mockSession struct {
	id             string
	appName        string
	userID         string
	state          session.State
	events         *mockEvents
	lastUpdateTime time.Time
}

func (s *mockSession) ID() string                { return s.id }
func (s *mockSession) AppName() string           { return s.appName }
func (s *mockSession) UserID() string            { return s.userID }
func (s *mockSession) State() session.State      { return s.state }
func (s *mockSession) Events() session.Events    { return s.events }
func (s *mockSession) LastUpdateTime() time.Time { return s.lastUpdateTime }

type mockEvents struct {
	events []*session.Event
}

func (e *mockEvents) All() iter.Seq[*session.Event] {
	return func(yield func(*session.Event) bool) {
		for _, evt := range e.events {
			if !yield(evt) {
				return
			}
		}
	}
}

func (e *mockEvents) Len() int {
	return len(e.events)
}

func (e *mockEvents) At(i int) *session.Event {
	if i < 0 || i >= len(e.events) {
		return nil
	}
	return e.events[i]
}

// mockState implements session.State for testing
type mockState struct {
	data map[string]any
}

func (s *mockState) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		for k, v := range s.data {
			if !yield(k, v) {
				return
			}
		}
	}
}

func (s *mockState) Get(key string) (any, error) {
	v, ok := s.data[key]
	if !ok {
		return nil, nil // Return nil error for simplicity in tests
	}
	return v, nil
}

func (s *mockState) Set(key string, value any) error {
	s.data[key] = value
	return nil
}

func createTestSession(id, appName, userID string) *mockSession {
	return &mockSession{
		id:             id,
		appName:        appName,
		userID:         userID,
		events:         &mockEvents{events: nil},
		lastUpdateTime: time.Now(),
	}
}

func createTestSessionWithState(id, appName, userID string, state map[string]any) *mockSession {
	return &mockSession{
		id:             id,
		appName:        appName,
		userID:         userID,
		state:          &mockState{data: state},
		events:         &mockEvents{events: nil},
		lastUpdateTime: time.Now(),
	}
}

func createTestEvent(id, author string) *session.Event {
	return &session.Event{
		ID:        id,
		Author:    author,
		Timestamp: time.Now(),
	}
}

func TestNewSessionPersister(t *testing.T) {
	ctx := context.Background()

	t.Run("nil client", func(t *testing.T) {
		_, err := NewSessionPersister(ctx, nil)
		if err == nil {
			t.Fatal("Expected error for nil client")
		}
		t.Logf("nil client: correctly returned error: %v", err)
	})

	t.Run("valid client", func(t *testing.T) {
		persister, client := setupTestDB(t)
		if persister == nil {
			return
		}
		defer persister.Close()
		defer client.Close()

		if persister.client == nil {
			t.Error("persister should have a non-nil client")
		}
		t.Logf("valid client: persister created successfully")
	})
}

func TestPersistSession(t *testing.T) {
	persister, client := setupTestDB(t)
	if persister == nil {
		return
	}
	defer persister.Close()
	defer client.Close()

	ctx := context.Background()

	t.Run("basic persist", func(t *testing.T) {
		sess := createTestSession("sess-1", "test_app", "user-1")

		err := persister.PersistSession(ctx, sess)
		if err != nil {
			t.Fatalf("PersistSession failed: %v", err)
		}

		// Wait for async operation
		time.Sleep(100 * time.Millisecond)

		// Verify session was saved
		var count int
		err = client.db.NewRaw(
			"SELECT COUNT(*) FROM sessions WHERE id = ? AND app_name = ? AND user_id = ?",
			"sess-1", "test_app", "user-1",
		).Scan(ctx, &count)
		if err != nil {
			t.Fatalf("Failed to verify session: %v", err)
		}
		if count != 1 {
			t.Errorf("Expected 1 session, got %d", count)
		}

		t.Logf("basic persist: session saved successfully")
	})

	t.Run("persist with state", func(t *testing.T) {
		state := map[string]any{
			"key1": "value1",
			"key2": 123,
		}
		sess := createTestSessionWithState("sess-state", "test_app", "user-1", state)

		err := persister.PersistSession(ctx, sess)
		if err != nil {
			t.Fatalf("PersistSession failed: %v", err)
		}

		// Wait for async operation
		time.Sleep(100 * time.Millisecond)

		// Verify state was saved
		var stateJSON []byte
		err = client.db.NewRaw(
			"SELECT state FROM sessions WHERE id = ?", "sess-state",
		).Scan(ctx, &stateJSON)
		if err != nil {
			t.Fatalf("Failed to verify state: %v", err)
		}

		var savedState map[string]any
		if err := sonic.Unmarshal(stateJSON, &savedState); err != nil {
			t.Fatalf("Failed to unmarshal state: %v", err)
		}

		if savedState["key1"] != "value1" {
			t.Errorf("Expected key1='value1', got %v", savedState["key1"])
		}

		t.Logf("persist with state: state saved correctly")
	})

	t.Run("upsert session", func(t *testing.T) {
		sess1 := createTestSessionWithState("sess-upsert", "test_app", "user-1",
			map[string]any{"version": 1})

		err := persister.PersistSession(ctx, sess1)
		if err != nil {
			t.Fatalf("First PersistSession failed: %v", err)
		}

		// Wait for async operation
		time.Sleep(100 * time.Millisecond)

		sess2 := createTestSessionWithState("sess-upsert", "test_app", "user-1",
			map[string]any{"version": 2})

		err = persister.PersistSession(ctx, sess2)
		if err != nil {
			t.Fatalf("Second PersistSession failed: %v", err)
		}

		// Wait for async operation
		time.Sleep(100 * time.Millisecond)

		// Should still have only 1 session
		var count int
		err = client.db.NewRaw(
			"SELECT COUNT(*) FROM sessions WHERE id = ?", "sess-upsert",
		).Scan(ctx, &count)
		if err != nil {
			t.Fatalf("Failed to count sessions: %v", err)
		}
		if count != 1 {
			t.Errorf("Expected 1 session after upsert, got %d", count)
		}

		// Verify state was updated
		var stateJSON []byte
		err = client.db.NewRaw(
			"SELECT state FROM sessions WHERE id = ?", "sess-upsert",
		).Scan(ctx, &stateJSON)
		if err != nil {
			t.Fatalf("Failed to get state: %v", err)
		}

		var state map[string]any
		if err := sonic.Unmarshal(stateJSON, &state); err != nil {
			t.Fatalf("Failed to unmarshal state: %v", err)
		}

		if state["version"] != float64(2) {
			t.Errorf("Expected version=2, got %v", state["version"])
		}

		t.Logf("upsert session: state updated correctly")
	})
}

func TestPersistEvent(t *testing.T) {
	persister, client := setupTestDB(t)
	if persister == nil {
		return
	}
	defer persister.Close()
	defer client.Close()

	ctx := context.Background()

	t.Run("basic event persist", func(t *testing.T) {
		sess := createTestSession("sess-evt-1", "test_app", "user-evt")
		evt := createTestEvent("evt-1", "user")

		// First persist the session
		err := persister.PersistSession(ctx, sess)
		if err != nil {
			t.Fatalf("PersistSession failed: %v", err)
		}
		time.Sleep(100 * time.Millisecond)

		// Then persist the event
		err = persister.PersistEvent(ctx, sess, evt)
		if err != nil {
			t.Fatalf("PersistEvent failed: %v", err)
		}
		time.Sleep(100 * time.Millisecond)

		// Verify event was saved
		tableName := persister.eventsTableName("user-evt")
		var count int
		err = client.db.NewRaw(
			fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE session_id = ? AND id = ?", tableName),
			"sess-evt-1", "evt-1",
		).Scan(ctx, &count)
		if err != nil {
			t.Fatalf("Failed to verify event: %v", err)
		}
		if count != 1 {
			t.Errorf("Expected 1 event, got %d", count)
		}

		t.Logf("basic event persist: event saved to shard table %s", tableName)
	})

	t.Run("multiple events ordering", func(t *testing.T) {
		sess := createTestSession("sess-evt-order", "test_app", "user-evt-order")

		// Persist session first
		err := persister.PersistSession(ctx, sess)
		if err != nil {
			t.Fatalf("PersistSession failed: %v", err)
		}
		time.Sleep(100 * time.Millisecond)

		// Persist multiple events sequentially (wait for each to complete)
		for i := range 3 {
			evt := createTestEvent(fmt.Sprintf("evt-order-%d", i), "user")
			err = persister.PersistEvent(ctx, sess, evt)
			if err != nil {
				t.Fatalf("PersistEvent %d failed: %v", i, err)
			}
			time.Sleep(100 * time.Millisecond)
		}

		// Verify event ordering
		tableName := persister.eventsTableName("user-evt-order")
		var orders []int
		err = client.db.NewRaw(
			fmt.Sprintf("SELECT event_order FROM %s WHERE session_id = ? ORDER BY event_order", tableName),
			"sess-evt-order",
		).Scan(ctx, &orders)
		if err != nil {
			t.Fatalf("Failed to query events: %v", err)
		}

		if len(orders) != 3 {
			t.Errorf("Expected 3 events, got %d", len(orders))
		}

		for i, order := range orders {
			if order != i {
				t.Errorf("Expected order %d at position %d, got %d", i, i, order)
			}
		}

		t.Logf("multiple events ordering: events ordered correctly %v", orders)
	})
}

func TestDeleteSession(t *testing.T) {
	persister, client := setupTestDB(t)
	if persister == nil {
		return
	}
	defer persister.Close()
	defer client.Close()

	ctx := context.Background()

	t.Run("delete existing session", func(t *testing.T) {
		sess := createTestSession("sess-del-1", "test_app", "user-del")

		// Create session
		err := persister.PersistSession(ctx, sess)
		if err != nil {
			t.Fatalf("PersistSession failed: %v", err)
		}
		time.Sleep(100 * time.Millisecond)

		// Add some events
		for i := range 2 {
			evt := createTestEvent(fmt.Sprintf("evt-del-%d", i), "user")
			err = persister.PersistEvent(ctx, sess, evt)
			if err != nil {
				t.Fatalf("PersistEvent failed: %v", err)
			}
		}
		time.Sleep(200 * time.Millisecond)

		// Delete session
		err = persister.DeleteSession(ctx, "test_app", "user-del", "sess-del-1")
		if err != nil {
			t.Fatalf("DeleteSession failed: %v", err)
		}
		time.Sleep(100 * time.Millisecond)

		// Verify session was deleted
		var count int
		err = client.db.NewRaw(
			"SELECT COUNT(*) FROM sessions WHERE id = ?", "sess-del-1",
		).Scan(ctx, &count)
		if err != nil {
			t.Fatalf("Failed to count sessions: %v", err)
		}
		if count != 0 {
			t.Errorf("Expected 0 sessions after delete, got %d", count)
		}

		// Verify events were deleted
		tableName := persister.eventsTableName("user-del")
		err = client.db.NewRaw(
			fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE session_id = ?", tableName),
			"sess-del-1",
		).Scan(ctx, &count)
		if err != nil {
			t.Fatalf("Failed to count events: %v", err)
		}
		if count != 0 {
			t.Errorf("Expected 0 events after delete, got %d", count)
		}

		t.Logf("delete existing session: session and events deleted")
	})

	t.Run("delete non-existent session", func(t *testing.T) {
		err := persister.DeleteSession(ctx, "test_app", "user-none", "sess-none")
		if err != nil {
			t.Fatalf("DeleteSession for non-existent session failed: %v", err)
		}
		time.Sleep(100 * time.Millisecond)

		t.Logf("delete non-existent session: no error returned")
	})
}

func TestClose(t *testing.T) {
	persister, client := setupTestDB(t)
	if persister == nil {
		return
	}
	defer client.Close()

	ctx := context.Background()

	// Add some operations before closing
	sess := createTestSession("sess-close", "test_app", "user-close")
	_ = persister.PersistSession(ctx, sess)

	err := persister.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Should not accept new operations after close
	err = persister.PersistSession(ctx, sess)
	if err == nil {
		t.Error("Expected error after Close")
	}

	// Double close should be safe
	err = persister.Close()
	if err != nil {
		t.Errorf("Double Close should be safe, got: %v", err)
	}

	t.Logf("Close: persister closed correctly")
}

func TestAsyncBufferSize(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := newTestClient(getTestConnString())
	if err != nil {
		t.Skipf("PostgreSQL not available, skipping test: %v", err)
		return
	}
	defer client.Close()

	t.Run("custom buffer size", func(t *testing.T) {
		persister, err := NewSessionPersister(ctx, client,
			WithAsyncBufferSize(100))
		if err != nil {
			t.Fatalf("Failed to create persister: %v", err)
		}
		defer persister.Close()

		t.Logf("custom buffer size: persister created with buffer size 100")
	})

	t.Run("default async mode", func(t *testing.T) {
		persister, err := NewSessionPersister(ctx, client)
		if err != nil {
			t.Fatalf("Failed to create persister: %v", err)
		}
		defer persister.Close()

		// Operations should complete via async worker
		sess := createTestSession("sess-async", "test_app", "user-async")
		err = persister.PersistSession(ctx, sess)
		if err != nil {
			t.Fatalf("PersistSession failed: %v", err)
		}

		// Wait for async operation to complete
		time.Sleep(100 * time.Millisecond)

		var count int
		err = client.db.NewRaw(
			"SELECT COUNT(*) FROM sessions WHERE id = ?", "sess-async",
		).Scan(ctx, &count)
		if err != nil {
			t.Fatalf("Failed to verify session: %v", err)
		}
		if count != 1 {
			t.Errorf("Expected 1 session, got %d", count)
		}

		t.Logf("default async mode: operations completed via async worker")
	})
}

func TestShardDistribution(t *testing.T) {
	persister, client := setupTestDB(t)
	if persister == nil {
		return
	}
	defer persister.Close()
	defer client.Close()

	ctx := context.Background()

	// Create sessions for different users
	users := []string{"user-shard-a", "user-shard-b", "user-shard-c", "user-shard-d"}
	shardCounts := make(map[int]int)

	for _, userID := range users {
		sess := createTestSession("sess-shard-"+userID, "test_app", userID)
		err := persister.PersistSession(ctx, sess)
		if err != nil {
			t.Fatalf("PersistSession failed: %v", err)
		}

		evt := createTestEvent("evt-shard-"+userID, "user")
		err = persister.PersistEvent(ctx, sess, evt)
		if err != nil {
			t.Fatalf("PersistEvent failed: %v", err)
		}

		shardIdx := persister.shardIndex(userID)
		shardCounts[shardIdx]++
	}

	// Wait for async operations
	time.Sleep(500 * time.Millisecond)

	t.Logf("shard distribution: events distributed across shards: %v", shardCounts)
}

func TestIsolation(t *testing.T) {
	persister, client := setupTestDB(t)
	if persister == nil {
		return
	}
	defer persister.Close()
	defer client.Close()

	ctx := context.Background()

	t.Run("app isolation", func(t *testing.T) {
		// Create sessions for different apps
		sess1 := createTestSession("sess-iso-1", "test_app_1", "user-iso")
		sess2 := createTestSession("sess-iso-1", "test_app_2", "user-iso")

		err := persister.PersistSession(ctx, sess1)
		if err != nil {
			t.Fatalf("PersistSession app1 failed: %v", err)
		}

		err = persister.PersistSession(ctx, sess2)
		if err != nil {
			t.Fatalf("PersistSession app2 failed: %v", err)
		}

		time.Sleep(200 * time.Millisecond)

		// Both should exist (same session ID, different apps)
		var count int
		err = client.db.NewRaw(
			"SELECT COUNT(*) FROM sessions WHERE id = ?", "sess-iso-1",
		).Scan(ctx, &count)
		if err != nil {
			t.Fatalf("Failed to count sessions: %v", err)
		}
		if count != 2 {
			t.Errorf("Expected 2 sessions (different apps), got %d", count)
		}

		t.Logf("app isolation: same session ID allowed across different apps")
	})

	t.Run("user isolation", func(t *testing.T) {
		// Create sessions for different users
		sess1 := createTestSession("sess-user-iso", "test_app", "user-a")
		sess2 := createTestSession("sess-user-iso", "test_app", "user-b")

		err := persister.PersistSession(ctx, sess1)
		if err != nil {
			t.Fatalf("PersistSession user-a failed: %v", err)
		}

		err = persister.PersistSession(ctx, sess2)
		if err != nil {
			t.Fatalf("PersistSession user-b failed: %v", err)
		}

		time.Sleep(200 * time.Millisecond)

		// Both should exist (same session ID, different users)
		var count int
		err = client.db.NewRaw(
			"SELECT COUNT(*) FROM sessions WHERE id = ?", "sess-user-iso",
		).Scan(ctx, &count)
		if err != nil {
			t.Fatalf("Failed to count sessions: %v", err)
		}
		if count != 2 {
			t.Errorf("Expected 2 sessions (different users), got %d", count)
		}

		t.Logf("user isolation: same session ID allowed across different users")
	})
}

func TestSchemaCreation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := newTestClient(getTestConnString())
	if err != nil {
		t.Skipf("PostgreSQL not available, skipping test: %v", err)
		return
	}
	defer client.Close()

	persister, err := NewSessionPersister(ctx, client, WithShardCount(4))
	if err != nil {
		t.Fatalf("Failed to create persister: %v", err)
	}
	defer persister.Close()

	// Verify sessions table exists
	var tableName string
	err = client.db.NewRaw(
		"SELECT table_name FROM information_schema.tables WHERE table_name = ?", "sessions",
	).Scan(ctx, &tableName)
	if err != nil {
		t.Fatalf("Sessions table should exist: %v", err)
	}

	// Verify sharded event tables exist
	for i := range persister.shardCount {
		expectedTable := fmt.Sprintf("session_events_%d", i)
		err = client.db.NewRaw(
			"SELECT table_name FROM information_schema.tables WHERE table_name = ?",
			expectedTable,
		).Scan(ctx, &tableName)
		if err != nil {
			t.Errorf("Event shard table %s should exist: %v", expectedTable, err)
		}
	}

	t.Logf("schema creation: sessions table and %d event shard tables created",
		persister.shardCount)
}
