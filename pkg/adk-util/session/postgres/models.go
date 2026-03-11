package postgres

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

type Session struct {
	bun.BaseModel `bun:"table:sessions"`

	ID             string          `bun:",pk,type:varchar(255)"                            json:"id"`
	AppName        string          `bun:",pk,type:varchar(255)"                            json:"app_name"`
	UserID         string          `bun:",pk,type:varchar(255)"                            json:"user_id"`
	State          json.RawMessage `bun:",notnull,type:jsonb,default:'{}'"                 json:"state"`
	LastUpdateTime time.Time       `bun:",nullzero,notnull,default:now(),type:timestamptz" json:"last_update_time"`
	CreatedAt      time.Time       `bun:",nullzero,notnull,default:now(),type:timestamptz" json:"created_at"`
}

type SessionEvent struct {
	bun.BaseModel `bun:"table:session_events"`

	ID         string          `bun:",type:varchar(255)"                               json:"id"`
	AppName    string          `bun:",pk,type:varchar(255)"                            json:"app_name"`
	UserID     string          `bun:",pk,type:varchar(255)"                            json:"user_id"`
	SessionID  string          `bun:",pk,type:varchar(255)"                            json:"session_id"`
	EventOrder int             `bun:",pk"                                              json:"event_order"`
	Content    json.RawMessage `bun:",notnull,type:jsonb"                              json:"content"`
	Author     string          `bun:",type:varchar(255)"                               json:"author,omitempty"`
	Timestamp  time.Time       `bun:",notnull,type:timestamptz"                        json:"timestamp"`
	CreatedAt  time.Time       `bun:",nullzero,notnull,default:now(),type:timestamptz" json:"created_at"`
}
