package stores

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/kydenul/k-agent/config"
	"github.com/kydenul/log"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

const (
	defaultConnMaxIdleTime = 10 * time.Minute
	defaultConnMaxLifetime = 30 * time.Minute
	defaultMaxOpenConns    = 25
	defaultMaxIdleConns    = 10
	defaultPingRetries     = 3
	defaultPingTimeout     = 3 * time.Second
)

// PostgresClient wraps a Bun database connection.
type PostgresClient struct {
	db *bun.DB
}

// NewPostgresClient creates a new PostgreSQL client backed by Bun ORM.
// The caller is responsible for closing the client when done.
func NewPostgresClient(ctx context.Context, cfg *config.Postgres) (*PostgresClient, error) {
	if cfg == nil {
		return nil, errors.New("postgres config cannot be nil")
	}

	if cfg.DSN == "" {
		return nil, errors.New("postgres DSN cannot be empty")
	}

	// Apply defaults for zero values
	pingRetries := cfg.PingRetries
	if pingRetries <= 0 {
		pingRetries = defaultPingRetries
	}

	pingTimeout := cfg.PingTimeout
	if pingTimeout <= 0 {
		pingTimeout = defaultPingTimeout
	}

	// Open underlying sql.DB via pgdriver
	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(cfg.DSN)))

	// Configure connection pool
	maxOpenConns := cfg.MaxOpenConns
	if maxOpenConns <= 0 {
		maxOpenConns = defaultMaxOpenConns
	}
	sqldb.SetMaxOpenConns(maxOpenConns)

	maxIdleConns := cfg.MaxIdleConns
	if maxIdleConns <= 0 {
		maxIdleConns = defaultMaxIdleConns
	}
	sqldb.SetMaxIdleConns(maxIdleConns)

	connMaxIdleTime := cfg.ConnMaxIdleTime
	if connMaxIdleTime <= 0 {
		connMaxIdleTime = defaultConnMaxIdleTime
	}
	sqldb.SetConnMaxIdleTime(connMaxIdleTime)

	connMaxLifetime := cfg.ConnMaxLifetime
	if connMaxLifetime <= 0 {
		connMaxLifetime = defaultConnMaxLifetime
	}
	sqldb.SetConnMaxLifetime(connMaxLifetime)

	// Create Bun DB instance
	db := bun.NewDB(sqldb, pgdialect.New())

	// Validate connection with retries
	var pingErr error
	for i := range pingRetries {
		pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
		pingErr = db.PingContext(pingCtx)
		cancel()

		if pingErr == nil {
			break
		}

		log.Errorf("postgres ping failed (attempt %d/%d): %v",
			i+1, pingRetries, pingErr)

		if i < pingRetries-1 {
			time.Sleep(time.Duration(i+1) * time.Second)
		}
	}

	if pingErr != nil {
		if closeErr := db.Close(); closeErr != nil {
			log.Errorf("failed to close postgres connection: %v", closeErr)
		}
		return nil, fmt.Errorf("postgres ping failed after %d retries: %w",
			pingRetries, pingErr)
	}

	log.Info("postgres client initialized successfully")

	return &PostgresClient{db: db}, nil
}

// DB returns the underlying Bun database instance.
func (c *PostgresClient) DB() *bun.DB { return c.db }

// Close closes the database connection.
func (c *PostgresClient) Close() error {
	if c.db == nil {
		return nil
	}
	return c.db.Close()
}
