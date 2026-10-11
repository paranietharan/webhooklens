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

	if err := ps.ensureSchema(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run schema migration: %w", err)
	}

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

func (s *PostgresStore) ensureSchema(ctx context.Context) error {
	schema := `
	CREATE EXTENSION IF NOT EXISTS "pgcrypto";

	CREATE TABLE IF NOT EXISTS users (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		email VARCHAR(255) NOT NULL UNIQUE,
		password_hash VARCHAR(255) NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS endpoints (
		id VARCHAR(32) PRIMARY KEY,
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name VARCHAR(255) NOT NULL,
		require_auth BOOLEAN NOT NULL DEFAULT FALSE,
		auth_token VARCHAR(255) UNIQUE,
		is_active BOOLEAN NOT NULL DEFAULT TRUE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_endpoints_user_id ON endpoints(user_id);
	CREATE INDEX IF NOT EXISTS idx_endpoints_auth_token ON endpoints(auth_token);

	CREATE TABLE IF NOT EXISTS webhook_logs (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		endpoint_id VARCHAR(32) NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
		method VARCHAR(16) NOT NULL,
		headers JSONB NOT NULL DEFAULT '{}'::jsonb,
		query_params JSONB NOT NULL DEFAULT '{}'::jsonb,
		body BYTEA,
		ip_address VARCHAR(45) NOT NULL,
		received_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_webhook_logs_endpoint_received ON webhook_logs(endpoint_id, received_at DESC);
	`
	_, err := s.db.ExecContext(ctx, schema)
	return err
}
