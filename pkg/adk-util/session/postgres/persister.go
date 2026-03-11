package postgres

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"maps"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	ksess "github.com/kydenul/k-adk/session"
	"github.com/kydenul/log"
	"google.golang.org/adk/session"
)

var _ ksess.Persister = (*SessionPersister)(nil)

// Default configuration values.
const (
	defaultAsyncBufferSize = 1000
	defaultAsyncOpTimeout  = 30 * time.Second
	defaultShardCount      = 16

	operationSession = "session"
	operationEvent   = "event"
	operationDelete  = "delete"
)

// SessionPersister implements Persister for PostgreSQL session persistence.
// It is designed to work alongside RedisSessionService as a long-term storage backend.
type SessionPersister struct {
	client PostgresClient

	asyncBufferSize int
	shardCount      int

	asyncChan chan asyncOperation // Channel for async operations
	wg        sync.WaitGroup
	closed    bool
	mu        sync.Mutex
}

type asyncOperation struct {
	operationType string // "session", "event", "delete"
	sess          session.Session
	evt           *session.Event
	appName       string
	userID        string
	sessionID     string
}

// NewSessionPersister creates a new PostgreSQL session persister.
func NewSessionPersister(
	ctx context.Context,
	client PostgresClient,
	opts ...PersisterOption,
) (*SessionPersister, error) {
	if client == nil {
		return nil, errors.New("postgres client cannot be nil")
	}

	p := &SessionPersister{client: client}

	// Apply options
	for _, opt := range opts {
		opt(p)
	}

	// Default shard count
	if p.shardCount <= 0 {
		p.shardCount = defaultShardCount
	}

	// Async mode: positive = enabled, zero defaults, negative = disabled
	if p.asyncBufferSize > 0 {
		p.asyncChan = make(chan asyncOperation, p.asyncBufferSize)
	} else if p.asyncBufferSize == 0 {
		p.asyncBufferSize = defaultAsyncBufferSize
		p.asyncChan = make(chan asyncOperation, p.asyncBufferSize)
	}
	// negative asyncBufferSize = explicitly disabled, asyncChan stays nil

	// Initialize database schema
	if err := p.initSchema(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Start async worker if async mode is enabled
	if p.asyncChan != nil {
		p.wg.Add(1)
		go p.asyncWorker() //nolint:contextcheck // async worker manages its own context
	}

	log.Info("PostgreSQL session persister initialized")

	return p, nil
}

// initSchema creates the necessary tables and indexes.
func (p *SessionPersister) initSchema(ctx context.Context) error {
	// NOTE: Create sessions table
	const sessionsSchema = `
		CREATE TABLE IF NOT EXISTS sessions (
			id VARCHAR(255) NOT NULL,
			app_name VARCHAR(255) NOT NULL,
			user_id VARCHAR(255) NOT NULL,
			state JSONB NOT NULL DEFAULT '{}',
			last_update_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (app_name, user_id, id)
		);

		CREATE INDEX IF NOT EXISTS idx_sessions_app_user ON sessions(app_name, user_id);
		CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
		CREATE INDEX IF NOT EXISTS idx_sessions_last_update ON sessions(last_update_time);
	`

	log.Infof("Init Session Schema SQL: %s", sessionsSchema)

	if _, err := p.client.DB().ExecContext(ctx, sessionsSchema); err != nil {
		log.Errorf("failed to create sessions table: %v", err)
		return fmt.Errorf("failed to create sessions table: %w", err)
	}

	// NOTE: Create sharded events tables
	for i := range p.shardCount {
		eventsSchema := fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS session_events_%d (
				id VARCHAR(255) NOT NULL,
				app_name VARCHAR(255) NOT NULL,
				user_id VARCHAR(255) NOT NULL,
				session_id VARCHAR(255) NOT NULL,
				event_order INT NOT NULL,
				content JSONB NOT NULL,
				author VARCHAR(255),
				timestamp TIMESTAMPTZ NOT NULL,
				created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
				PRIMARY KEY (app_name, user_id, session_id, event_order)
			);

			CREATE INDEX IF NOT EXISTS idx_events_%d_session ON session_events_%d(app_name, user_id, session_id);
			CREATE INDEX IF NOT EXISTS idx_events_%d_timestamp ON session_events_%d(timestamp);
		`, i, i, i, i, i)

		log.Infof("Init Event Schema SQL: %s", eventsSchema)

		if _, err := p.client.DB().ExecContext(ctx, eventsSchema); err != nil {
			log.Errorf("failed to create events shard table %d: %v", i, err)
			return fmt.Errorf("failed to create events shard table %d: %w", i, err)
		}
	}

	log.Infof("schema initialized with %d event shards", p.shardCount)

	return nil
}

// asyncWorker processes async operations from the channel.
func (p *SessionPersister) asyncWorker() {
	defer p.wg.Done()

	for op := range p.asyncChan {
		p.processAsyncOp(op)
	}
}

// processAsyncOp processes a single async operation with proper context management.
func (p *SessionPersister) processAsyncOp(op asyncOperation) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultAsyncOpTimeout)
	defer cancel()

	var err error
	switch op.operationType {
	case operationSession:
		err = p.persistSessionSync(ctx, op.sess)
	case operationEvent:
		err = p.persistEventSync(ctx, op.sess, op.evt)
	case operationDelete:
		err = p.deleteSessionSync(ctx, op.appName, op.userID, op.sessionID)
	}

	if err != nil {
		log.Errorf("async %s operation failed: %v", op.operationType, err)
	}
}

// PersistSession saves or updates a session in PostgreSQL.
// If async mode is enabled, the operation is queued and returns immediately.
func (p *SessionPersister) PersistSession(ctx context.Context, sess session.Session) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return errors.New("persister is closed")
	}
	p.mu.Unlock()

	if p.asyncChan != nil {
		select {
		case p.asyncChan <- asyncOperation{operationType: operationSession, sess: sess}:
			return nil

		default:
			log.Warn("async channel full, falling back to sync persist")
		}
	}

	return p.persistSessionSync(ctx, sess)
}

func (p *SessionPersister) persistSessionSync(ctx context.Context, sess session.Session) error {
	// Collect state
	var stateJSON []byte
	if state := sess.State(); state != nil {
		stateMap := maps.Collect(state.All())
		var err error
		stateJSON, err = sonic.Marshal(stateMap)
		if err != nil {
			log.Errorf("failed to marshal session state for %s: %v", sess.ID(), err)
			return fmt.Errorf("failed to marshal session state: %w", err)
		}
	} else {
		stateJSON = []byte("{}")
	}

	model := &Session{
		ID:             sess.ID(),
		AppName:        sess.AppName(),
		UserID:         sess.UserID(),
		State:          stateJSON,
		LastUpdateTime: sess.LastUpdateTime(),
	}

	_, err := p.client.DB().NewInsert().
		Model(model).
		On("CONFLICT (app_name, user_id, id) DO UPDATE").
		Set("state = EXCLUDED.state").
		Set("last_update_time = EXCLUDED.last_update_time").
		Exec(ctx)
	if err != nil {
		log.Errorf("failed to persist session %s: %v", sess.ID(), err)
		return fmt.Errorf("failed to persist session: %w", err)
	}

	log.Infof("session persisted: %s", sess.ID())

	return nil
}

// PersistEvent saves a single event to PostgreSQL (real-time sync).
// If async mode is enabled, the operation is queued and returns immediately.
func (p *SessionPersister) PersistEvent(
	ctx context.Context,
	sess session.Session,
	evt *session.Event,
) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return errors.New("persister is closed")
	}
	p.mu.Unlock()

	if p.asyncChan != nil {
		select {
		case p.asyncChan <- asyncOperation{operationType: operationEvent, sess: sess, evt: evt}:
			return nil

		default:
			log.Warn("async channel full, falling back to sync persist")
		}
	}

	return p.persistEventSync(ctx, sess, evt)
}

func (p *SessionPersister) persistEventSync(
	ctx context.Context,
	sess session.Session,
	evt *session.Event,
) error {
	// Serialize event
	evtData, err := sonic.Marshal(evt)
	if err != nil {
		log.Errorf("failed to marshal event %s: %v", evt.ID, err)
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	tableName := p.eventsTableName(sess.UserID())

	// Use transaction to ensure atomicity when getting next order and inserting
	tx, err := p.client.DB().BeginTx(ctx, nil)
	if err != nil {
		log.Errorf("failed to begin transaction: %v", err)
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Lock the session row to serialize event inserts for this session
	// Ignore error - session may not exist yet, but we still need the order
	var lockedID string
	_ = tx.NewSelect().Model((*Session)(nil)).
		Column("id").
		Where("app_name = ?", sess.AppName()).
		Where("user_id = ?", sess.UserID()).
		Where("id = ?", sess.ID()).
		For("UPDATE").
		Scan(ctx, &lockedID)

	// Get next event order
	var nextOrder int
	err = tx.NewSelect().ModelTableExpr(tableName).
		ColumnExpr("COALESCE(MAX(event_order), -1) + 1").
		Where("app_name = ?", sess.AppName()).
		Where("user_id = ?", sess.UserID()).
		Where("session_id = ?", sess.ID()).
		Scan(ctx, &nextOrder)
	if err != nil {
		log.Errorf("failed to get next event order: %v", err)
		return fmt.Errorf("failed to get next event order: %w", err)
	}

	// Insert event
	eventModel := &SessionEvent{
		ID:         evt.ID,
		AppName:    sess.AppName(),
		UserID:     sess.UserID(),
		SessionID:  sess.ID(),
		EventOrder: nextOrder,
		Content:    evtData,
		Author:     evt.Author,
		Timestamp:  evt.Timestamp,
	}
	_, err = tx.NewInsert().
		Model(eventModel).
		ModelTableExpr(tableName).
		Exec(ctx)
	if err != nil {
		log.Errorf("failed to insert event %s: %v", evt.ID, err)
		return fmt.Errorf("failed to insert event: %w", err)
	}

	// Also update session's last_update_time
	_, err = tx.NewUpdate().
		Model((*Session)(nil)).
		Set("last_update_time = ?", evt.Timestamp).
		Where("app_name = ?", sess.AppName()).
		Where("user_id = ?", sess.UserID()).
		Where("id = ?", sess.ID()).
		Exec(ctx)
	if err != nil {
		log.Warnf("failed to update session last_update_time: %v", err)
		// Don't fail the whole operation for this
	}

	if err := tx.Commit(); err != nil {
		log.Errorf("failed to commit transaction: %v", err)
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Debugf("event persisted: session=%s, event=%s, shard=%s",
		sess.ID(), evt.ID, tableName)
	return nil
}

// DeleteSession removes a session and all its events from PostgreSQL.
// If async mode is enabled, the operation is queued and returns immediately.
func (p *SessionPersister) DeleteSession(
	ctx context.Context,
	appName, userID, sessionID string,
) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return errors.New("persister is closed")
	}
	p.mu.Unlock()

	if p.asyncChan != nil {
		select {
		case p.asyncChan <- asyncOperation{
			operationType: operationDelete,
			appName:       appName,
			userID:        userID,
			sessionID:     sessionID,
		}:
			return nil

		default:
			log.Warn("async channel full, falling back to sync delete")
		}
	}

	return p.deleteSessionSync(ctx, appName, userID, sessionID)
}

func (p *SessionPersister) deleteSessionSync(
	ctx context.Context,
	appName, userID, sessionID string,
) error {
	tx, err := p.client.DB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Delete events from sharded table
	tableName := p.eventsTableName(userID)
	_, err = tx.NewDelete().
		ModelTableExpr(tableName).
		Where("app_name = ?", appName).
		Where("user_id = ?", userID).
		Where("session_id = ?", sessionID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete events: %w", err)
	}

	// Delete session
	_, err = tx.NewDelete().
		Model((*Session)(nil)).
		Where("app_name = ?", appName).
		Where("user_id = ?", userID).
		Where("id = ?", sessionID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Debugf("session deleted from postgres: %s", sessionID)
	return nil
}

// Close closes the persister and releases resources.
// It waits for all pending async operations to complete before returning.
func (p *SessionPersister) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()

	if p.asyncChan != nil {
		close(p.asyncChan)
		p.wg.Wait() // Wait for all async operations to complete
	}

	log.Info("PostgreSQL session persister closed")
	return nil
}

// shardIndex calculates the shard index for a given user ID.
// Uses FNV-1a hash for consistent distribution.
func (p *SessionPersister) shardIndex(userID string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(userID))
	return int(h.Sum32()) % p.shardCount
}

// eventsTableName returns the sharded events table name for a user.
func (p *SessionPersister) eventsTableName(userID string) string {
	shardIdx := p.shardIndex(userID)
	return fmt.Sprintf("session_events_%d", shardIdx)
}
