package memory

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

// MemoryEntry represents a memory entry stored in PostgreSQL.
type MemoryEntry struct {
	bun.BaseModel `bun:"table:memory_entries"`

	ID          int             `bun:",pk,autoincrement"                                json:"id"`
	AppName     string          `bun:",notnull,type:varchar(255)"                       json:"app_name"`
	UserID      string          `bun:",notnull,type:varchar(255)"                       json:"user_id"`
	SessionID   string          `bun:",notnull,type:varchar(255)"                       json:"session_id"`
	EventID     string          `bun:",notnull,type:varchar(255)"                       json:"event_id"`
	Author      string          `bun:",type:varchar(255)"                               json:"author,omitempty"`
	Content     json.RawMessage `bun:",notnull,type:jsonb"                              json:"content"`
	ContentText string          `bun:",notnull,type:text"                               json:"content_text"`
	Timestamp   time.Time       `bun:",notnull,type:timestamptz"                        json:"timestamp"`
	CreatedAt   time.Time       `bun:",nullzero,notnull,default:now(),type:timestamptz" json:"created_at"`
}
