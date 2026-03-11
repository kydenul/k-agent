package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	memorytypes "github.com/kydenul/k-adk/memory/types"
	"github.com/kydenul/log"
	"google.golang.org/adk/memory"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// EmbeddingModel is an interface for generating embeddings from text.
type EmbeddingModel interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	Dimension() int
}

var (
	_ memory.Service                    = (*PostgresMemoryService)(nil)
	_ memorytypes.ExtendedMemoryService = (*PostgresMemoryService)(nil)
)

// Default configuration values.
const (
	defaultAsyncBufferSize = 1000
	defaultAsyncOpTimeout  = 30 * time.Second

	operationAddSession   = "add_session"
	operationUpdateMemory = "update_memory"
	operationDeleteMemory = "delete_memory"
)

// PostgresMemoryService implements memory.Service using PostgreSQL with pgvector.
type PostgresMemoryService struct {
	client         PostgresClient
	embeddingModel EmbeddingModel
	embeddingDim   int

	asyncBufferSize int
	asyncChan       chan asyncOperation // Channel for async operations
	wg              sync.WaitGroup
	closed          bool
	mu              sync.Mutex
}

type asyncOperation struct {
	operationType string
	sess          session.Session
	appName       string
	userID        string
	entryID       int
	newContent    string
}

// NewPostgresMemoryService creates a new PostgreSQL-backed memory service.
func NewPostgresMemoryService(
	ctx context.Context,
	client PostgresClient,
	opts ...MemoryOption,
) (*PostgresMemoryService, error) {
	if client == nil {
		return nil, errors.New("postgres client cannot be nil")
	}

	svc := &PostgresMemoryService{client: client}

	// Apply options
	for _, opt := range opts {
		opt(svc)
	}

	// Async mode: positive = enabled, zero defaults, negative = disabled
	if svc.asyncBufferSize > 0 {
		svc.asyncChan = make(chan asyncOperation, svc.asyncBufferSize)
	} else if svc.asyncBufferSize == 0 {
		svc.asyncBufferSize = defaultAsyncBufferSize
		svc.asyncChan = make(chan asyncOperation, svc.asyncBufferSize)
	}
	// negative asyncBufferSize = explicitly disabled, asyncChan stays nil

	// Detect embedding dimension
	if svc.embeddingModel != nil {
		svc.embeddingDim = svc.embeddingModel.Dimension()

		if svc.embeddingDim == 0 {
			embedding, err := svc.embeddingModel.Embed(ctx, "dimension probe")
			if err != nil {
				log.Errorf("failed to probe embedding dimension: %v", err)
				return nil, fmt.Errorf("failed to probe embedding dimension: %w", err)
			}

			svc.embeddingDim = len(embedding)
		}
	}

	// Initialize database schema
	if err := svc.initSchema(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Start async worker if async mode is enabled
	if svc.asyncChan != nil {
		svc.wg.Add(1)
		go svc.asyncWorker() //nolint:contextcheck // async worker manages its own context
	}

	log.Info("PostgreSQL memory service initialized")

	return svc, nil
}

// initSchema creates the necessary tables and extensions.
func (s *PostgresMemoryService) initSchema(ctx context.Context) error {
	// Base schema without vector column
	baseSchema := `
		CREATE TABLE IF NOT EXISTS memory_entries (
			id SERIAL PRIMARY KEY,
			app_name VARCHAR(255) NOT NULL,
			user_id VARCHAR(255) NOT NULL,
			session_id VARCHAR(255) NOT NULL,
			event_id VARCHAR(255) NOT NULL,
			author VARCHAR(255),
			content JSONB NOT NULL,
			content_text TEXT NOT NULL,
			timestamp TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE(app_name, user_id, session_id, event_id)
		);

		CREATE INDEX IF NOT EXISTS idx_memory_app_user ON memory_entries(app_name, user_id);
		CREATE INDEX IF NOT EXISTS idx_memory_session ON memory_entries(session_id);
		CREATE INDEX IF NOT EXISTS idx_memory_timestamp ON memory_entries(timestamp);
		CREATE INDEX IF NOT EXISTS idx_memory_content_text ON memory_entries USING gin(to_tsvector('english', content_text));
	`

	log.Infof("Init Memory Schema SQL: %s", baseSchema)

	if _, err := s.client.DB().ExecContext(ctx, baseSchema); err != nil {
		log.Errorf("failed to create base schema: %v", err)
		return fmt.Errorf("failed to create base schema: %w", err)
	}

	// Add vector column if embedding model is configured
	if s.embeddingDim > 0 {
		vectorSchema := fmt.Sprintf(`
			CREATE EXTENSION IF NOT EXISTS vector;

			DO $$
			BEGIN
				IF NOT EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_name = 'memory_entries' AND column_name = 'embedding'
				) THEN
					ALTER TABLE memory_entries ADD COLUMN embedding vector(%d);
				END IF;
			END $$;

			DO $$
			BEGIN
				IF NOT EXISTS (
					SELECT 1 FROM pg_indexes WHERE indexname = 'idx_memory_embedding'
				) THEN
					CREATE INDEX idx_memory_embedding ON memory_entries
					USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);
				END IF;
			END $$;
		`, s.embeddingDim)

		if _, err := s.client.DB().ExecContext(ctx, vectorSchema); err != nil {
			log.Errorf("failed to create vector schema: %v", err)
			return fmt.Errorf("failed to create vector schema: %w", err)
		}
	}

	return nil
}

// asyncWorker processes async operations from the channel.
func (s *PostgresMemoryService) asyncWorker() {
	defer s.wg.Done()

	for op := range s.asyncChan {
		s.processAsyncOp(op)
	}
}

// processAsyncOp processes a single async operation with proper context management.
func (s *PostgresMemoryService) processAsyncOp(op asyncOperation) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultAsyncOpTimeout)
	defer cancel()

	var err error
	switch op.operationType {
	case operationAddSession:
		err = s.addSessionSync(ctx, op.sess)
	case operationUpdateMemory:
		err = s.updateMemorySync(ctx, op.appName, op.userID, op.entryID, op.newContent)
	case operationDeleteMemory:
		err = s.deleteMemorySync(ctx, op.appName, op.userID, op.entryID)
	}

	if err != nil {
		log.Errorf("async %s operation failed: %v", op.operationType, err)
	}
}

// AddSession extracts memory entries from a session and stores them.
// If async mode is enabled, the operation is queued and returns immediately.
func (s *PostgresMemoryService) AddSession(ctx context.Context, sess session.Session) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("memory service is closed")
	}
	s.mu.Unlock()

	if s.asyncChan != nil {
		select {
		case s.asyncChan <- asyncOperation{operationType: operationAddSession, sess: sess}:
			return nil

		default:
			log.Warn("async channel full, falling back to sync add session")
		}
	}

	return s.addSessionSync(ctx, sess)
}

func (s *PostgresMemoryService) addSessionSync(ctx context.Context, sess session.Session) error {
	events := sess.Events()
	if events == nil || events.Len() == 0 {
		log.Warn("no events found in session")
		return nil
	}

	log.Debugf("adding session to memory: app=%s, user=%s, session=%s, events=%d",
		sess.AppName(), sess.UserID(), sess.ID(), events.Len())

	tx, err := s.client.DB().BeginTx(ctx, nil)
	if err != nil {
		log.Errorf("failed to begin transaction: %v", err)
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	insertedCount := 0
	skippedCount := 0
	errorCount := 0

	for event := range events.All() {
		if event.Content == nil || len(event.Content.Parts) == 0 {
			skippedCount++
			continue
		}

		// Extract text content
		text := extractTextFromContent(event.Content)
		if text == "" {
			skippedCount++
			continue
		}

		// Serialize content to JSON
		contentJSON, err := sonic.Marshal(event.Content)
		if err != nil {
			log.Warnf("failed to marshal event content for event %s: %v", event.ID, err)
			errorCount++
			continue
		}

		timestamp := event.Timestamp
		if timestamp.IsZero() {
			timestamp = time.Now()
		}

		eventID := event.ID
		if eventID == "" {
			eventID = fmt.Sprintf("%s-%d", event.InvocationID, timestamp.UnixNano())
		}

		if s.embeddingModel != nil {
			// Generate embedding
			var embeddingStr *string
			embedding, embErr := s.embeddingModel.Embed(ctx, text)
			if embErr == nil && len(embedding) > 0 {
				embStr := vectorToString(embedding)
				embeddingStr = &embStr
			} else if embErr != nil {
				log.Debugf("failed to generate embedding for event %s: %v", eventID, embErr)
			}

			_, err = tx.NewRaw(`
				INSERT INTO memory_entries
				(app_name, user_id, session_id, event_id, author, content, content_text, embedding, timestamp)
				VALUES (?, ?, ?, ?, ?, ?::jsonb, ?, ?, ?)
				ON CONFLICT (app_name, user_id, session_id, event_id) DO UPDATE
				SET content = EXCLUDED.content, content_text = EXCLUDED.content_text, embedding = EXCLUDED.embedding
			`,
				sess.AppName(),
				sess.UserID(),
				sess.ID(),
				eventID,
				event.Author,
				string(contentJSON),
				text,
				embeddingStr,
				timestamp,
			).Exec(ctx)
		} else {
			_, err = tx.NewRaw(`
				INSERT INTO memory_entries
				(app_name, user_id, session_id, event_id, author, content, content_text, timestamp)
				VALUES (?, ?, ?, ?, ?, ?::jsonb, ?, ?)
				ON CONFLICT (app_name, user_id, session_id, event_id) DO UPDATE
				SET content = EXCLUDED.content, content_text = EXCLUDED.content_text
			`,
				sess.AppName(),
				sess.UserID(),
				sess.ID(),
				eventID,
				event.Author,
				string(contentJSON),
				text,
				timestamp,
			).Exec(ctx)
		}
		if err != nil {
			log.Errorf("failed to insert memory entry: %v", err)
			errorCount++
			continue
		}
		insertedCount++
	}

	if err := tx.Commit(); err != nil {
		log.Errorf("failed to commit transaction: %v", err)
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Infof("session added to memory: session=%s, inserted=%d, skipped=%d, errors=%d",
		sess.ID(), insertedCount, skippedCount, errorCount)

	return nil
}

// Close closes the memory service and releases resources.
// It waits for all pending async operations to complete before returning.
func (s *PostgresMemoryService) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	if s.asyncChan != nil {
		close(s.asyncChan)
		s.wg.Wait() // Wait for all async operations to complete
	}

	log.Info("PostgreSQL memory service closed")
	return nil
}

// Search finds relevant memory entries for a query.
func (s *PostgresMemoryService) Search(
	ctx context.Context,
	req *memory.SearchRequest,
) (*memory.SearchResponse, error) {
	log.Debugf("searching memories: app=%s, user=%s, query=%q",
		req.AppName, req.UserID, req.Query)

	var (
		memories   []memory.Entry
		err        error
		searchType string
	)

	// NOTE: If we have an embedding model and a query, try vector search first
	if s.embeddingModel != nil && req.Query != "" {
		embedding, embErr := s.embeddingModel.Embed(ctx, req.Query)
		if embErr != nil {
			log.Warnf("embedding failed for search query, falling back to text search: %v", embErr)
		} else if len(embedding) > 0 {
			memories, err = s.searchByVector(ctx, req, embedding)
			if err != nil {
				log.Errorf("failed to search by vector: %v", err)
				return nil, err
			}
			searchType = "vector"
		}
	}

	// NOTE: Fallback to text search if no results or no embedding model
	if len(memories) == 0 && req.Query != "" {
		memories, err = s.searchByText(ctx, req)
		if err != nil {
			log.Errorf("failed to search by text: %v", err)
			return nil, err
		}
		searchType = "text"
	}

	// NOTE: If still no results and query is empty, return recent entries
	if len(memories) == 0 {
		memories, err = s.searchRecent(ctx, req)
		if err != nil {
			log.Errorf("failed to search recent: %v", err)
			return nil, err
		}
		searchType = "recent"
	}

	log.Debugf("search completed: type=%s, results=%d", searchType, len(memories))

	return &memory.SearchResponse{Memories: memories}, nil
}

// searchByVector performs semantic similarity search.
func (s *PostgresMemoryService) searchByVector(
	ctx context.Context,
	req *memory.SearchRequest,
	embedding []float32,
) ([]memory.Entry, error) {
	log.Debugf("searching by vector: app=%s, user=%s, embedding_dim=%d",
		req.AppName, req.UserID, len(embedding))

	embeddingStr := vectorToString(embedding)

	rows, err := s.client.DB().QueryContext(ctx, `
		SELECT content, author, timestamp
		FROM memory_entries
		WHERE app_name = ? AND user_id = ? AND embedding IS NOT NULL
		ORDER BY embedding <=> ?
		LIMIT 10
	`, req.AppName, req.UserID, embeddingStr)
	if err != nil {
		log.Errorf("failed to search by vector: %v", err)
		return nil, fmt.Errorf("failed to search by vector: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return s.scanMemories(rows)
}

// searchByText performs full-text search using PostgreSQL's tsvector.
func (s *PostgresMemoryService) searchByText(
	ctx context.Context,
	req *memory.SearchRequest,
) ([]memory.Entry, error) {
	log.Debugf("searching by text: app=%s, user=%s, query=%q",
		req.AppName, req.UserID, req.Query)

	rows, err := s.client.DB().QueryContext(ctx, `
		SELECT content, author, timestamp
		FROM memory_entries
		WHERE app_name = ? AND user_id = ?
		AND to_tsvector('english', content_text) @@ plainto_tsquery('english', ?)
		ORDER BY ts_rank(to_tsvector('english', content_text), plainto_tsquery('english', ?)) DESC,
		         timestamp DESC
		LIMIT 10
	`, req.AppName, req.UserID, req.Query, req.Query)
	if err != nil {
		log.Errorf("failed to search by text: %v", err)
		return nil, fmt.Errorf("failed to search by text: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return s.scanMemories(rows)
}

// searchRecent returns the most recent memory entries.
func (s *PostgresMemoryService) searchRecent(
	ctx context.Context,
	req *memory.SearchRequest,
) ([]memory.Entry, error) {
	log.Debugf("searching recent entries: app=%s, user=%s", req.AppName, req.UserID)

	rows, err := s.client.DB().QueryContext(ctx, `
		SELECT content, author, timestamp
		FROM memory_entries
		WHERE app_name = ? AND user_id = ?
		ORDER BY timestamp DESC
		LIMIT 10
	`, req.AppName, req.UserID)
	if err != nil {
		log.Errorf("failed to search recent: %v", err)
		return nil, fmt.Errorf("failed to search recent: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return s.scanMemories(rows)
}

// scanMemories converts database rows to memory entries.
func (*PostgresMemoryService) scanMemories(rows *sql.Rows) ([]memory.Entry, error) {
	var memories []memory.Entry

	for rows.Next() {
		var contentJSON []byte
		var author sql.NullString
		var timestamp time.Time

		if err := rows.Scan(&contentJSON, &author, &timestamp); err != nil {
			log.Warnf("failed to scan memory row: %v", err)
			continue
		}

		var content genai.Content
		if err := sonic.Unmarshal(contentJSON, &content); err != nil {
			log.Warnf("failed to unmarshal memory content: %v", err)
			continue
		}

		entry := memory.Entry{
			Content:   &content,
			Timestamp: timestamp,
		}
		if author.Valid {
			entry.Author = author.String
		}

		memories = append(memories, entry)
	}

	return memories, rows.Err()
}

// ------------------- ExtendedMemoryService -------------------

// scanMemoriesWithID converts database rows to memory entries with database row IDs.
func (*PostgresMemoryService) scanMemoriesWithID(
	rows *sql.Rows,
) ([]memorytypes.EntryWithID, error) {
	var memories []memorytypes.EntryWithID

	for rows.Next() {
		var id int
		var contentJSON []byte
		var author sql.NullString
		var timestamp time.Time

		if err := rows.Scan(&id, &contentJSON, &author, &timestamp); err != nil {
			log.Warnf("failed to scan memory row with ID: %v", err)
			continue
		}

		var content genai.Content
		if err := sonic.Unmarshal(contentJSON, &content); err != nil {
			log.Warnf("failed to unmarshal memory content for id=%d: %v", id, err)
			continue
		}

		entry := memorytypes.EntryWithID{
			ID:        id,
			Content:   &content,
			Timestamp: timestamp,
		}
		if author.Valid {
			entry.Author = author.String
		}

		memories = append(memories, entry)
	}

	return memories, rows.Err()
}

// SearchWithID finds relevant memory entries for a query and returns them with database row IDs.
func (s *PostgresMemoryService) SearchWithID(
	ctx context.Context,
	req *memory.SearchRequest,
) ([]memorytypes.EntryWithID, error) {
	log.Debugf("searching memories with ID: app=%s, user=%s, query=%q",
		req.AppName, req.UserID, req.Query)

	var (
		memories   []memorytypes.EntryWithID
		err        error
		searchType string
	)

	// NOTE: If we have an embedding model and a query, try vector search first
	if s.embeddingModel != nil && req.Query != "" {
		embedding, embErr := s.embeddingModel.Embed(ctx, req.Query)
		if embErr != nil {
			log.Warnf(
				"embedding failed for search with ID, falling back to text search: %v",
				embErr,
			)
		} else if len(embedding) > 0 {
			memories, err = s.searchByVectorWithID(ctx, req, embedding)
			if err != nil {
				log.Errorf("failed to search by vector with ID: %v", err)
				return nil, err
			}
			searchType = "vector"
		}
	}

	// NOTE: Fallback to text search if no results or no embedding model
	if len(memories) == 0 && req.Query != "" {
		memories, err = s.searchByTextWithID(ctx, req)
		if err != nil {
			log.Errorf("failed to search by text with ID: %v", err)
			return nil, err
		}
		searchType = "text"
	}

	// NOTE: If still no results and query is empty, return recent entries
	if len(memories) == 0 {
		memories, err = s.searchRecentWithID(ctx, req)
		if err != nil {
			log.Errorf("failed to search recent with ID: %v", err)
			return nil, err
		}
		searchType = "recent"
	}

	log.Debugf("search with ID completed: type=%s, results=%d", searchType, len(memories))

	return memories, nil
}

// searchByVectorWithID performs semantic similarity search and returns entries with IDs.
func (s *PostgresMemoryService) searchByVectorWithID(
	ctx context.Context,
	req *memory.SearchRequest,
	embedding []float32,
) ([]memorytypes.EntryWithID, error) {
	log.Debugf("searching by vector with ID: app=%s, user=%s, embedding_dim=%d",
		req.AppName, req.UserID, len(embedding))

	embeddingStr := vectorToString(embedding)
	rows, err := s.client.DB().QueryContext(ctx, `
		SELECT id, content, author, timestamp
		FROM memory_entries
		WHERE app_name = ? AND user_id = ? AND embedding IS NOT NULL
		ORDER BY embedding <=> ?
		LIMIT 10
	`, req.AppName, req.UserID, embeddingStr)
	if err != nil {
		log.Errorf("failed to search by vector with ID: %v", err)
		return nil, fmt.Errorf("failed to search by vector with ID: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return s.scanMemoriesWithID(rows)
}

// searchByTextWithID performs full-text search and returns entries with IDs.
func (s *PostgresMemoryService) searchByTextWithID(
	ctx context.Context,
	req *memory.SearchRequest,
) ([]memorytypes.EntryWithID, error) {
	log.Debugf("searching by text with ID: app=%s, user=%s, query=%q",
		req.AppName, req.UserID, req.Query)

	rows, err := s.client.DB().QueryContext(ctx, `
		SELECT id, content, author, timestamp
		FROM memory_entries
		WHERE app_name = ? AND user_id = ?
		AND to_tsvector('english', content_text) @@ plainto_tsquery('english', ?)
		ORDER BY ts_rank(to_tsvector('english', content_text), plainto_tsquery('english', ?)) DESC,
		         timestamp DESC
		LIMIT 10
	`, req.AppName, req.UserID, req.Query, req.Query)
	if err != nil {
		log.Errorf("failed to search by text with ID: %v", err)
		return nil, fmt.Errorf("failed to search by text with ID: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return s.scanMemoriesWithID(rows)
}

// searchRecentWithID returns the most recent memory entries with IDs.
func (s *PostgresMemoryService) searchRecentWithID(
	ctx context.Context,
	req *memory.SearchRequest,
) ([]memorytypes.EntryWithID, error) {
	log.Debugf("searching recent entries with ID: app=%s, user=%s", req.AppName, req.UserID)

	rows, err := s.client.DB().QueryContext(ctx, `
		SELECT id, content, author, timestamp
		FROM memory_entries
		WHERE app_name = ? AND user_id = ?
		ORDER BY timestamp DESC
		LIMIT 10
	`, req.AppName, req.UserID)
	if err != nil {
		log.Errorf("failed to search recent with ID: %v", err)
		return nil, fmt.Errorf("failed to search recent with ID: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return s.scanMemoriesWithID(rows)
}

// UpdateMemory updates the content of a memory entry by ID, scoped by app_name and user_id.
// If async mode is enabled, the operation is queued and returns immediately.
// If an embedding model is available, the embedding is regenerated for the new content.
func (s *PostgresMemoryService) UpdateMemory(
	ctx context.Context,
	appName, userID string,
	entryID int,
	newContent string,
) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("memory service is closed")
	}
	s.mu.Unlock()

	if s.asyncChan != nil {
		select {
		case s.asyncChan <- asyncOperation{
			operationType: operationUpdateMemory,
			appName:       appName,
			userID:        userID,
			entryID:       entryID,
			newContent:    newContent,
		}:
			return nil

		default:
			log.Warn("async channel full, falling back to sync update")
		}
	}

	return s.updateMemorySync(ctx, appName, userID, entryID, newContent)
}

func (s *PostgresMemoryService) updateMemorySync(
	ctx context.Context,
	appName, userID string,
	entryID int,
	newContent string,
) error {
	log.Debugf("updating memory entry: app=%s, user=%s, entry_id=%d", appName, userID, entryID)

	// Build the updated genai.Content with the new text
	content := &genai.Content{
		Parts: []*genai.Part{
			{Text: newContent},
		},
	}

	contentJSON, err := sonic.Marshal(content)
	if err != nil {
		log.Errorf("failed to marshal updated content: %v", err)
		return fmt.Errorf("failed to marshal updated content: %w", err)
	}

	var result sql.Result
	if s.embeddingModel != nil {
		// Re-generate embedding for the new content
		embedding, embErr := s.embeddingModel.Embed(ctx, newContent)
		if embErr != nil {
			log.Errorf("failed to generate embedding for updated content: %v", embErr)
			return fmt.Errorf("failed to generate embedding for updated content: %w", embErr)
		}

		embeddingStr := vectorToString(embedding)
		result, err = s.client.DB().NewRaw(`
			UPDATE memory_entries
			SET content_text = ?, content = ?::jsonb, embedding = ?
			WHERE id = ? AND app_name = ? AND user_id = ?
		`, newContent, string(contentJSON), embeddingStr, entryID, appName, userID).Exec(ctx)
	} else {
		result, err = s.client.DB().NewRaw(`
			UPDATE memory_entries
			SET content_text = ?, content = ?::jsonb
			WHERE id = ? AND app_name = ? AND user_id = ?
		`, newContent, string(contentJSON), entryID, appName, userID).Exec(ctx)
	}

	if err != nil {
		log.Errorf("failed to update memory entry: %v", err)
		return fmt.Errorf("failed to update memory entry: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		log.Errorf("failed to get rows affected: %v", err)
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		log.Debugf(
			"no memory entry found to update: app=%s, user=%s, entry_id=%d",
			appName, userID, entryID,
		)
		return fmt.Errorf(
			"memory entry not found: app=%s, user=%s, id=%d",
			appName, userID, entryID,
		)
	}

	log.Infof("memory entry updated: app=%s, user=%s, entry_id=%d", appName, userID, entryID)

	return nil
}

// DeleteMemory deletes a memory entry by ID, scoped by app_name and user_id.
// If async mode is enabled, the operation is queued and returns immediately.
func (s *PostgresMemoryService) DeleteMemory(
	ctx context.Context,
	appName, userID string,
	entryID int,
) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("memory service is closed")
	}
	s.mu.Unlock()

	if s.asyncChan != nil {
		select {
		case s.asyncChan <- asyncOperation{
			operationType: operationDeleteMemory,
			appName:       appName,
			userID:        userID,
			entryID:       entryID,
		}:
			return nil

		default:
			log.Warn("async channel full, falling back to sync delete")
		}
	}

	return s.deleteMemorySync(ctx, appName, userID, entryID)
}

func (s *PostgresMemoryService) deleteMemorySync(
	ctx context.Context,
	appName, userID string,
	entryID int,
) error {
	log.Debugf("deleting memory entry: app=%s, user=%s, entry_id=%d", appName, userID, entryID)

	result, err := s.client.DB().NewRaw(`
		DELETE FROM memory_entries
		WHERE id = ? AND app_name = ? AND user_id = ?
	`, entryID, appName, userID).Exec(ctx)
	if err != nil {
		log.Errorf("failed to delete memory entry: %v", err)
		return fmt.Errorf("failed to delete memory entry: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		log.Errorf("failed to get rows affected: %v", err)
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		log.Debugf(
			"no memory entry found to delete: app=%s, user=%s, entry_id=%d",
			appName, userID, entryID,
		)
		return fmt.Errorf(
			"memory entry not found: app=%s, user=%s, id=%d",
			appName, userID, entryID,
		)
	}

	log.Infof("memory entry deleted: app=%s, user=%s, entry_id=%d", appName, userID, entryID)

	return nil
}
