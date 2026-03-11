# adk-util

ADK utility packages providing adapter implementations for integrating [Google's Agent Development Kit (ADK)](https://google.golang.org/adk) with **OpenAI** and **Anthropic** LLM APIs, plus session persistence and memory services.

This is an embedded package within the `k-agent` module. Import via `github.com/kydenul/k-agent/pkg/adk-util/...`.

## Features

- **OpenAI Adapter** — Full support for OpenAI API and compatible providers (Ollama, vLLM, OpenRouter, etc.)
- **Anthropic Adapter** — Native Claude API support with extended thinking and automatic message history repair
- **Multi-Modal Support** — Images, audio (wav/mp3), PDF documents, and text files across both adapters
- **Memory Toolset** — Agent-facing tools for searching, saving, updating, and deleting long-term memories
- **Redis Session Service** — Persistent session management with Redis backend
- **PostgreSQL Session Persister** — Hybrid Redis + PostgreSQL session persistence for durability
- **PostgreSQL Memory Service** — Long-term memory storage with pgvector semantic search, plus CRUD operations
- **Streaming Support** — Real-time streaming responses via Go 1.23+ iterators
- **Tool Calling** — Full function/tool calling support with automatic ID normalization

## Package Structure

```
adk-util/
├── genai/
│   ├── openai/              # OpenAI adapter (model.LLM interface)
│   │   ├── openai.go        # Main adapter implementation
│   │   ├── openai_test.go   # Unit tests
│   │   └── base.go          # Conversion utilities (images, audio, PDF, text)
│   └── anthropic/           # Anthropic adapter (model.LLM interface)
│       ├── anthropic.go     # Main adapter implementation
│       ├── anthropic_test.go# Unit tests
│       └── base.go          # Conversion utilities
├── session/
│   ├── persister.go         # Persister interface for long-term storage
│   ├── redis/               # Redis session service (session.Service)
│   │   ├── service.go       # Service implementation
│   │   ├── service_test.go  # Unit tests
│   │   ├── session.go       # Session struct
│   │   ├── state.go         # State management
│   │   └── events.go        # Event handling
│   └── postgres/            # PostgreSQL session persister
│       ├── client.go        # PostgreSQL client interface
│       ├── models.go        # Database models
│       ├── option.go        # Persister options (buffer size, shard count)
│       ├── persister.go     # Async session/event persistence
│       └── persister_test.go# Unit tests
├── memory/
│   ├── types/               # Memory service interfaces
│   │   └── types.go         # MemoryService, ExtendedMemoryService
│   └── postgres/            # PostgreSQL memory service (pgvector)
│       ├── client.go        # PostgresClient interface
│       ├── models.go        # Database models
│       ├── option.go        # Service options (embedding model, buffer size)
│       ├── memory.go        # memory.Service + ExtendedMemoryService
│       ├── memory_test.go   # Unit tests
│       ├── embedding.go     # Embedding utilities
│       ├── embedding_test.go# Embedding tests
│       └── utils.go         # Shared helpers
└── tools/
    └── memory/              # Agent-facing memory tools (tool.Toolset)
        ├── toolset.go       # search, save, update, delete memory tools
        └── toolset_test.go  # Unit tests
```

## Usage

### LLM Adapters

#### OpenAI

```go
import "github.com/kydenul/k-agent/pkg/adk-util/genai/openai"

model := openai.New(openai.Config{
    ModelName: "gpt-4o",
    APIKey:    os.Getenv("OPENAI_API_KEY"),
    // BaseURL: "http://localhost:11434/v1", // Optional: for Ollama/vLLM
})
```

Compatible with OpenRouter, Ollama, vLLM, and any OpenAI-compatible API via `BaseURL`.

#### Anthropic

```go
import "github.com/kydenul/k-agent/pkg/adk-util/genai/anthropic"

model := anthropic.New(anthropic.Config{
    ModelName: "claude-sonnet-4-20250514",
    APIKey:    os.Getenv("ANTHROPIC_API_KEY"),
})

// With extended thinking
model := anthropic.New(anthropic.Config{
    ModelName:            "claude-sonnet-4-20250514",
    APIKey:               os.Getenv("ANTHROPIC_API_KEY"),
    MaxOutputTokens:      16000,
    ThinkingBudgetTokens: 10000,
})
```

### Redis Session Service

```go
import ksess "github.com/kydenul/k-agent/pkg/adk-util/session/redis"

sessionSrv, _ := ksess.NewRedisSessionService(rdb,
    ksess.WithTTL(7 * 24 * time.Hour),
)
```

> **Redis is the sole read source.** All read operations (`Get`, `List`) only query Redis. The optional PostgreSQL persister is write-only. Once a session's Redis TTL expires, it becomes inaccessible. Recommended TTL: at least 7 days.

### PostgreSQL Session Persister (Hybrid Storage)

```go
import (
    ksess "github.com/kydenul/k-agent/pkg/adk-util/session/redis"
    ksesspg "github.com/kydenul/k-agent/pkg/adk-util/session/postgres"
)

pgPersister, _ := ksesspg.NewSessionPersister(ctx, pgClient)

sessionSrv, _ := ksess.NewRedisSessionService(rdb,
    ksess.WithTTL(7 * 24 * time.Hour),
    ksess.WithPersister(pgPersister),
)
```

### PostgreSQL Memory Service

```go
import memory "github.com/kydenul/k-agent/pkg/adk-util/memory/postgres"

memorySrv, _ := memory.NewPostgresMemoryService(ctx, pgClient,
    memory.WithEmbeddingModel(myEmbeddingModel),
    memory.WithAsyncBufferSize(1000),
)
```

> **Memory requires manual persistence.** ADK `runner.Run()` does not call `memory.Service.AddSession()` automatically. Call `AddSession` yourself after the runner finishes.

### Memory Toolset

```go
import memtools "github.com/kydenul/k-agent/pkg/adk-util/tools/memory"

memoryToolset, _ := memtools.NewToolset(memtools.ToolsetConfig{
    MemoryService: memorySrv,
    AppName:       "myapp",
})
```

Available tools: `search_memory`, `save_to_memory`, `update_memory`, `delete_memory`.

## Architecture

```
google.golang.org/adk/model.LLM (interface)
           │
           ├── genai/openai/    → github.com/openai/openai-go/v3
           └── genai/anthropic/ → github.com/anthropics/anthropic-sdk-go

google.golang.org/adk/session.Service (interface)
           │
           └── session/redis/   → github.com/redis/go-redis/v9
                    │
                    └── (optional) session/postgres/ → Long-term persistence

google.golang.org/adk/memory.Service (interface)
           │
           └── memory/postgres/ → github.com/lib/pq + pgvector

google.golang.org/adk/tool.Toolset (interface)
           │
           └── tools/memory/ → Agent-facing memory tools
```

## Key Implementation Details

- **Streaming**: Uses Go 1.23+ `iter.Seq2[*model.LLMResponse, error]`
- **Multi-Modal**: Images (JPEG, PNG, GIF, WebP), PDFs, text files; OpenAI also supports audio (WAV, MP3)
- **Extended Thinking**: Anthropic adapter via `ThinkingBudgetTokens` config
- **Tool Call ID Normalization**: OpenAI 40-char SHA256 hashing; Anthropic `[a-zA-Z0-9_-]` regex sanitization
- **Message History Repair**: Anthropic `repairMessageHistory()` fixes orphaned `tool_use` blocks
- **Async Persistence**: Session persister and memory service use buffered async channels with sync fallback

## Dependencies

- [google.golang.org/adk](https://google.golang.org/adk) — Google Agent Development Kit
- [github.com/openai/openai-go/v3](https://github.com/openai/openai-go) — Official OpenAI Go SDK
- [github.com/anthropics/anthropic-sdk-go](https://github.com/anthropics/anthropic-sdk-go) — Official Anthropic Go SDK
- [github.com/redis/go-redis/v9](https://github.com/redis/go-redis) — Redis client
- [github.com/lib/pq](https://github.com/lib/pq) — PostgreSQL driver
