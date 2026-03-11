package memory

import (
	"github.com/uptrace/bun"

	_ "github.com/lib/pq" // PostgreSQL driver
)

// PostgresClient wraps a PostgreSQL database connection for memory persistence.
type PostgresClient interface {
	// DB returns the underlying database connection.
	DB() *bun.DB
}
