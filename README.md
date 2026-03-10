# k-agent

A Go REST API service built with Gin, PostgreSQL, and Redis.

## Architecture

```
HTTP Client → Gin Router → Handler → Service → PostgreSQL / Redis
```

Single-process HTTP server with layered architecture:

| Layer | Package | Responsibility |
|-------|---------|----------------|
| Router | `internal/router/` | Gin engine, middleware, route registration |
| Handler | `internal/handler/` | HTTP request/response, validation |
| Service | `internal/service/` | Business logic, caching strategy |
| Store | `internal/stores/` | Database & cache clients, data models |

## Quick Start

```bash
# Prerequisites: Go 1.25+, PostgreSQL, Redis

# Copy and edit config
cp k-agent.yaml.example k-agent.yaml  # or edit k-agent.yaml directly

# Build
make compile

# Run
make run CMD=gateway
```

The server listens on the port configured in `k-agent.yaml` (default `:5568`).

## API

All endpoints are under `/api/v1`.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Health check |
| `GET` | `/api/v1/users` | List users (query: `page`, `page_size`) |
| `POST` | `/api/v1/users` | Create user (body: `name`, `email`) |
| `GET` | `/api/v1/users/:id` | Get user by ID |
| `DELETE` | `/api/v1/users/:id` | Delete user by ID |

## Configuration

Config file: `k-agent.yaml` (override with `-conf` flag). Environment variables prefixed with `KAGENT_`.

```yaml
Application:
  HTTP:
    port: ":5568"
    gin_mode: "debug"
    cors:
      allow_origins:
        - "http://localhost:3000"

  Postgres:
    dsn: "postgres://user:pass@localhost:5432/kagent?sslmode=disable"
    max_open_conns: 25
    max_idle_conns: 10

  Redis:
    host: 127.0.0.1
    port: 6379
```

### PostgreSQL DSN format

```
postgres://username:password@host:port/dbname?param1=value1
└──┬───┘   └───┬──┘ └──┬──┘  └─┬─┘ └┬┘ └──┬─┘ └──┬──┘
   │           |       |       │    |     |      |
  协议       用户名   密码    主机 端口 数据库名 查询参数
```

## Development

```bash
make                   # Full build: clean → tidy → fumpt → lint → compile
make compile           # Quick build without linting
make debug             # Build with debug symbols
make test              # Run all tests
make lint              # Run golangci-lint
make fumpt             # Format with gofumpt
make tidy              # go mod tidy + verify
make clean             # Remove binaries
```
