package config

import (
	"flag"
	"fmt"
	"path/filepath"
	"time"

	"github.com/kydenul/log"
	"github.com/spf13/viper"
)

var conf = flag.String("conf", "./k-agent.yaml", "config file path")

// Config holds all application configuration
type Config struct {
	HTTP     HTTP
	GRPC     GRPC
	Postgres Postgres
	Redis    Redis
}

type HTTP struct {
	// HTTP gateway port, e.g. ":5568"
	Port string `mapstructure:"Port"`
	// gin.DebugMode | gin.ReleaseMode
	GinMode string `mapstructure:"gin_mode"`
	// CORS configuration
	CORS CORS `mapstructure:"cors"`
}

type CORS struct {
	// Allowed origins, e.g. ["http://localhost:3000", "https://example.com"]
	AllowOrigins []string `mapstructure:"allow_origins"`
}

type GRPC struct {
	// e.g. "localhost:5567"
	SvrAddr string `mapstructure:"svr_addr"`
}

// PostgresConfig holds PostgreSQL connection configuration.
type Postgres struct {
	// DSN is the PostgreSQL data source name.
	//
	// e.g., "postgres://user:pass@localhost:5432/dbname?sslmode=disable"
	DSN string `mapstructure:"dsn"`

	// Connection pool settings
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxIdleTime time.Duration `mapstructure:"conn_max_idle_time"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`

	// PingRetries is the number of ping retries during connection validation.
	PingRetries int `mapstructure:"ping_retries"`
	// PingTimeout is the timeout for each ping attempt.
	PingTimeout time.Duration `mapstructure:"ping_timeout"`
}

func (c *Postgres) String() string {
	maskedDSN := "[REDACTED]"
	if c.DSN == "" {
		maskedDSN = "(empty)"
	}

	return fmt.Sprintf("PostgresConfig ==> DSN: %s, MaxOpenConns: %d, MaxIdleConns: %d, "+
		"ConnMaxIdleTime: %s, ConnMaxLifetime: %s, PingRetries: %d, PingTimeout: %s",
		maskedDSN, c.MaxOpenConns, c.MaxIdleConns, c.ConnMaxIdleTime,
		c.ConnMaxLifetime, c.PingRetries, c.PingTimeout)
}

// RedisConfig holds Redis connection configuration.
type Redis struct {
	Host     string `mapstructure:"host"`
	Port     uint16 `mapstructure:"port"`
	Password string `mapstructure:"password"` //nolint:gosec

	PoolSize        int           `mapstructure:"pool_size"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	MinIdleConns    int           `mapstructure:"min_idle_conns"`
	ConnMaxIdleTime time.Duration `mapstructure:"conn_max_idle_time"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`

	// PingRetries is the number of ping retries during connection validation.
	PingRetries int `mapstructure:"ping_retries"`
	// PingTimeout is the timeout for each ping attempt.
	PingTimeout time.Duration `mapstructure:"ping_timeout"`

	// EnablePoolMonitor enables pool statistics monitoring.
	// When enabled, pool stats will be logged at PoolMonitorInterval.
	EnablePoolMonitor bool `mapstructure:"enable_pool_monitor"`
	// PoolMonitorInterval is the interval for logging pool statistics.
	// Only used when EnablePoolMonitor is true.
	PoolMonitorInterval time.Duration `mapstructure:"pool_monitor_interval"`
}

func (c *Redis) String() string {
	maskedPassword := "[REDACTED]"
	if c.Password == "" {
		maskedPassword = "(empty)"
	}

	return fmt.Sprintf("RedisConfig ==> Host: %s, Port: %d, Password: %s, PoolSize: %d, "+
		"MaxIdleConns: %d, MinIdleConns: %d, ConnMaxIdleTime: %s, ConnMaxLifetime: %s, "+
		"PingRetries: %d, PingTimeout: %s, EnablePoolMonitor: %v, PoolMonitorInterval: %s",
		c.Host, c.Port, maskedPassword, c.PoolSize,
		c.MaxIdleConns, c.MinIdleConns, c.ConnMaxIdleTime, c.ConnMaxLifetime,
		c.PingRetries, c.PingTimeout, c.EnablePoolMonitor, c.PoolMonitorInterval)
}

func init() {
	flag.Parse()
	if *conf == "" {
		panic("config file path is empty")
	}

	opt, err := log.LoadFromFile(*conf)
	if err != nil {
		panic(fmt.Sprintf("Failed to load log config from file: %v", err))
	}
	log.NewLog(opt)
}

func Load() *Config {
	viper.SetConfigFile(*conf)

	viper.SetEnvPrefix("KAGENT")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		log.Fatalf("failed to read config: %v", err)
	}

	var cfg Config
	if err := viper.UnmarshalKey("Application", &cfg); err != nil {
		log.Fatalf("failed to unmarshal config: %v", err)
	}

	log.Debugf("using config: %s", filepath.Clean(viper.ConfigFileUsed()))

	return &cfg
}
