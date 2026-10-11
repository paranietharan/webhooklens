package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"webhooklens/internal/store"
)

type PostgresStore struct {
	db          *sql.DB
	users       store.UserStore
	endpoints   store.EndpointStore
	webhookLogs store.WebhookLogStore
}

func New(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to open database connection: %w", err)
	}

	db.SetMaxOpenConns(30)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(2 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	ps := &PostgresStore{
		db: db,
	}
	ps.users = NewUserStore(db)
	ps.endpoints = NewEndpointStore(db)
	ps.webhookLogs = NewWebhookLogStore(db)

	return ps, nil
}

func (s *PostgresStore) Users() store.UserStore {
	return s.users
}

func (s *PostgresStore) Endpoints() store.EndpointStore {
	return s.endpoints
}

func (s *PostgresStore) WebhookLogs() store.WebhookLogStore {
	return s.webhookLogs
}

func (s *PostgresStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *PostgresStore) Close() error {
	return s.db.Close()
}
