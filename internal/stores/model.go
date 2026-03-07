package stores

import (
	"context"

	"github.com/kydenul/log"
	"github.com/uptrace/bun"
)

type User struct {
	bun.BaseModel `bun:"table:users"`

	ID        string `bun:",pk,type:varchar(36)"`
	Name      string `bun:",notnull,type:varchar(255)"`
	Email     string `bun:",notnull,unique,type:varchar(255)"`
	CreatedAt int64  `bun:",nullzero,notnull,default:extract(epoch from now())::bigint"`
}

type CreateTableFunc func(*PostgresClient) error

// EnsureExistApplicationTable ensures the application's database tables exist. It will panic if any error occurs.
func EnsureExistApplicationTable(pg *PostgresClient, funcs ...CreateTableFunc) {
	for _, f := range funcs {
		if err := f(pg); err != nil {
			log.Fatalf("failed to create users table: %v", err)
		}
	}

	log.Info("✅ ensured tables")
}

func CreateTableUser(pgClient *PostgresClient) error {
	// Auto-create tables
	if _, err := pgClient.DB().NewCreateTable().
		Model((*User)(nil)).
		IfNotExists().
		Exec(context.Background()); err != nil {
		log.Errorf("failed to create users table: %v", err)
		return err
	}

	return nil
}
