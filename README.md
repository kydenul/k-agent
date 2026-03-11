# k-agent

A Go REST API service built with Gin, PostgreSQL, and Redis, with integrated ADK (Agent Development Kit) support for AI agent runtime. Supports multiple LLM providers (Gemini, OpenAI, Anthropic) with session persistence and semantic memory.

## Architecture

```
HTTP Client → Gin Router → Handler → Service → PostgreSQL / Redis
                   │
                   ├── SessionHandler → UserService → Session Service (Redis + PG)
                   │
                   └── AgentHandler → AgentService → ADK Runner → LLM
                                          ↓
                                Session (Redis + PG) / Memory (PG + pgvector)
```

Single-process HTTP server with layered architecture:

| Layer | Package | Responsibility |
|-------|---------|----------------|
| Router | `internal/router/` | Gin engine, middleware, route registration |
| Handler | `internal/handler/` | HTTP request/response, validation (`AgentHandler`, `SessionHandler`) |
| Models | `internal/models/` | Request/response models and ADK event conversion |
| Service | `internal/service/user/` | Session management via ADK session service |
| Service/Agent | `internal/service/agent/` | ADK agent runner orchestration (sync, SSE, memory) |
| Store | `internal/stores/` | PostgreSQL (Bun ORM) & Redis clients with connection pooling |
| ADK Util | `pkg/adk-util/` | ADK adapters: LLM providers, session, memory, tools |

## Quick Start

```bash
# Prerequisites: Go 1.26+, PostgreSQL (with pgvector), Redis

# Copy and edit config
cp k-agent.yaml.example k-agent.yaml

# Build
make compile

# Run
make run CMD=gateway
```

The server listens on the port configured in `k-agent.yaml` (default `:5568`).

## API

### Agent Runtime (ADK-compatible)

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/run` | Run agent synchronously, returns collected events |
| `POST` | `/run_sse` | Run agent with Server-Sent Events streaming |

Request body for both endpoints:

```json
{
  "appName": "my_agent",
  "userId": "user1",
  "sessionId": "session-uuid",
  "newMessage": { "role": "user", "parts": [{"text": "Hello"}] },
  "streaming": false,
  "stateDelta": {}
}
```

### Session Management

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/apps/:app_name/users/:user_id/sessions` | List all sessions for a user |
| `POST` | `/apps/:app_name/users/:user_id/sessions` | Create a new session |

### Health

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Health check |

## ADK Util (`pkg/adk-util/`)

Reusable ADK adapter library for LLM providers, session persistence, and semantic memory.

### LLM Providers

| Provider | Package | Features |
|----------|---------|----------|
| OpenAI | `genai/openai/` | OpenAI API + compatible providers (Ollama, vLLM, OpenRouter). Multi-modal (image, audio, PDF). Tool calling with ID normalization. |
| Anthropic | `genai/anthropic/` | Native Claude API. Extended thinking support. Multi-modal (image, PDF). Auto message history repair. |

Both implement `google.golang.org/adk/model.LLM` with streaming via `iter.Seq2`.

### Session

| Backend | Package | Description |
|---------|---------|-------------|
| Redis | `session/redis/` | Primary session store with TTL expiration. Implements `adk/session.Service`. |
| PostgreSQL | `session/postgres/` | Async persister for long-term session storage. Pairs with Redis for hybrid read/write. |

### Memory

| Package | Description |
|---------|-------------|
| `memory/postgres/` | Semantic memory with pgvector. Implements `adk/memory.Service`. Async embedding generation. |
| `tools/memory/` | Agent-facing memory toolset (`search_memory`, `save_to_memory`, `update_memory`, `delete_memory`). Implements `adk/tool.Toolset`. |

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
    conn_max_idle_time: 10m
    conn_max_lifetime: 30m

  Redis:
    host: 127.0.0.1
    port: 6379
    pool_size: 100
    max_idle_conns: 10
```

### PostgreSQL DSN format

```
postgres://username:password@host:port/dbname?param1=value1
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
