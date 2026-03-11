package memory

// MemoryOption configures the PostgresMemoryService.
type MemoryOption func(*PostgresMemoryService)

// WithAsyncBufferSize sets the buffer size for async operations.
// Default is 1000. Set to a negative value to disable async mode (all operations are synchronous).
func WithAsyncBufferSize(size int) MemoryOption {
	return func(s *PostgresMemoryService) {
		s.asyncBufferSize = size
	}
}

// WithEmbeddingModel sets the embedding model for semantic search.
func WithEmbeddingModel(model EmbeddingModel) MemoryOption {
	return func(s *PostgresMemoryService) {
		s.embeddingModel = model
	}
}
