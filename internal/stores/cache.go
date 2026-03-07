package stores

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kydenul/k-agent/config"
	"github.com/kydenul/log"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/cast"
)

const (
	defaultPoolSize             = 100
	defaultRedisMaxIdleConns    = 30
	defaultMinIdleConns         = 15
	defaultRedisConnMaxIdleTime = 10 * time.Minute
	defaultRedisConnMaxLifetime = 30 * time.Minute
	defaultRedisPingRetries     = 3
	defaultRedisPingTimeout     = 3 * time.Second
	defaultPoolMonitor          = false
	defaultPoolMonitorInterval  = 1 * time.Minute
)

// RedisClient wraps a Redis client with optional pool monitoring.
type RedisClient struct {
	redis.UniversalClient

	cancelMonitor context.CancelFunc
}

// NewRedisClient creates a new Redis client with the given configuration.
// The caller is responsible for closing the client when done.
func NewRedisClient(cfg *config.Redis) (*RedisClient, error) {
	if cfg == nil {
		return nil, errors.New("redis config cannot be nil")
	}

	// Apply defaults for zero values
	pingRetries := cfg.PingRetries
	if pingRetries <= 0 {
		pingRetries = defaultRedisPingRetries
	}

	pingTimeout := cfg.PingTimeout
	if pingTimeout <= 0 {
		pingTimeout = defaultRedisPingTimeout
	}

	poolMonitorInterval := cfg.PoolMonitorInterval
	if poolMonitorInterval <= 0 {
		poolMonitorInterval = defaultPoolMonitorInterval
	}

	poolSize := cfg.PoolSize
	if poolSize <= 0 {
		poolSize = defaultPoolSize
	}

	maxIdleConns := cfg.MaxIdleConns
	if maxIdleConns <= 0 {
		maxIdleConns = defaultRedisMaxIdleConns
	}

	minIdleConns := cfg.MinIdleConns
	if minIdleConns <= 0 {
		minIdleConns = defaultMinIdleConns
	}

	connMaxIdleTime := cfg.ConnMaxIdleTime
	if connMaxIdleTime <= 0 {
		connMaxIdleTime = defaultRedisConnMaxIdleTime
	}

	connMaxLifetime := cfg.ConnMaxLifetime
	if connMaxLifetime <= 0 {
		connMaxLifetime = defaultRedisConnMaxLifetime
	}

	rdb := redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs:           []string{cfg.Host + ":" + cast.ToString(cfg.Port)},
		Password:        cfg.Password,
		PoolSize:        poolSize,
		MaxIdleConns:    maxIdleConns,
		MinIdleConns:    minIdleConns,
		ConnMaxIdleTime: connMaxIdleTime,
		ConnMaxLifetime: connMaxLifetime,
	})

	// NOTE: Validate connection with retries
	var pingErr error
	for i := range pingRetries {
		ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
		pingErr = rdb.Ping(ctx).Err()
		cancel()

		if pingErr == nil {
			break
		}

		log.Errorf("redis ping failed (attempt %d/%d): %s",
			i+1, pingRetries, pingErr.Error())

		if i < pingRetries-1 {
			// Exponential backoff
			time.Sleep(time.Duration(i+1) * time.Second)
		}
	}

	if pingErr != nil {
		if closeErr := rdb.Close(); closeErr != nil {
			log.Errorf("failed to close redis client: %s", closeErr.Error())
		}
		return nil, fmt.Errorf("redis ping failed after %d retries: %w",
			pingRetries, pingErr)
	}

	log.Info("redis client initialized successfully")

	client := &RedisClient{UniversalClient: rdb}

	// NOTE: Start pool monitor if enabled
	if cfg.EnablePoolMonitor {
		ctx, cancel := context.WithCancel(context.Background())
		client.cancelMonitor = cancel

		go client.runPoolMonitor(ctx, poolMonitorInterval)
	}

	return client, nil
}

// Client returns the underlying Redis client.
// The returned client shares the same connection pool and should not be closed separately.
func (c *RedisClient) Client() redis.UniversalClient { return c.UniversalClient }

// Close closes the Redis client and stops the pool monitor if running.
func (c *RedisClient) Close() error {
	if c.cancelMonitor != nil {
		c.cancelMonitor()
		c.cancelMonitor = nil
	}

	if c.UniversalClient == nil {
		return nil
	}

	return c.UniversalClient.Close()
}

// runPoolMonitor logs pool statistics at the specified interval.
func (c *RedisClient) runPoolMonitor(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("redis pool monitor stopped")
			return

		case <-ticker.C:
			stats := c.PoolStats()
			log.Infof("Redis Pool: Hits=%d Misses=%d Timeouts=%d "+
				"TotalConns=%d IdleConns=%d StaleConns=%d",
				stats.Hits, stats.Misses, stats.Timeouts,
				stats.TotalConns, stats.IdleConns, stats.StaleConns)
		}
	}
}
