package postgres

// PersisterOption configures the SessionPersister.
type PersisterOption func(*SessionPersister)

// WithAsyncBufferSize sets the buffer size for async operations.
// Default is 1000. Set to a negative value to disable async mode (all operations are synchronous).
func WithAsyncBufferSize(size int) PersisterOption {
	return func(p *SessionPersister) {
		p.asyncBufferSize = size
	}
}

// WithShardCount sets the number of event shards.
// Must be a power of 2. Invalid values are ignored.
func WithShardCount(count int) PersisterOption {
	return func(p *SessionPersister) {
		if count > 0 && (count&(count-1)) == 0 {
			p.shardCount = count
		}
	}
}
