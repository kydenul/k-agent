package postgres

import (
	"github.com/uptrace/bun"

	_ "github.com/lib/pq" // PostgreSQL driver
)

// Client wraps a PostgreSQL database connection for session persistence.
type PostgresClient interface {
	// DB returns the underlying database connection.
	DB() *bun.DB
}
