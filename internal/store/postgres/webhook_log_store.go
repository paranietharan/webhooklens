package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"webhooklens/internal/models"
	"webhooklens/internal/store"
)

type webhookLogStore struct {
	db *sql.DB
}

func NewWebhookLogStore(db *sql.DB) store.WebhookLogStore {
	return &webhookLogStore{db: db}
}

func (s *webhookLogStore) Create(ctx context.Context, log *models.WebhookLog) error {
	query := `
		INSERT INTO webhook_logs (id, endpoint_id, method, headers, query_params, body, ip_address, received_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, received_at;
	`
	if log.ID == uuid.Nil {
		log.ID = uuid.New()
	}

	err := s.db.QueryRowContext(
		ctx,
		query,
		log.ID,
		log.EndpointID,
		log.Method,
		log.Headers,
		log.QueryParams,
		log.Body,
		log.IPAddress,
		log.ReceivedAt,
	).Scan(&log.ID, &log.ReceivedAt)
	return err
}

func (s *webhookLogStore) GetByID(ctx context.Context, id uuid.UUID, endpointID string) (*models.WebhookLog, error) {
	query := `
		SELECT id, endpoint_id, method, headers, query_params, body, ip_address, received_at
		FROM webhook_logs
		WHERE id = $1 AND endpoint_id = $2;
	`
	var log models.WebhookLog
	err := s.db.QueryRowContext(ctx, query, id, endpointID).Scan(
		&log.ID,
		&log.EndpointID,
		&log.Method,
		&log.Headers,
		&log.QueryParams,
		&log.Body,
		&log.IPAddress,
		&log.ReceivedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	if len(log.Body) > 0 {
		log.BodyRaw = string(log.Body)
	}
	return &log, nil
}

func (s *webhookLogStore) ListByEndpointID(ctx context.Context, endpointID string, limit, offset int) ([]*models.WebhookLog, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	query := `
		SELECT id, endpoint_id, method, headers, query_params, body, ip_address, received_at
		FROM webhook_logs
		WHERE endpoint_id = $1
		ORDER BY received_at DESC
		LIMIT $2 OFFSET $3;
	`
	rows, err := s.db.QueryContext(ctx, query, endpointID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []*models.WebhookLog
	for rows.Next() {
		var log models.WebhookLog
		if err := rows.Scan(
			&log.ID,
			&log.EndpointID,
			&log.Method,
			&log.Headers,
			&log.QueryParams,
			&log.Body,
			&log.IPAddress,
			&log.ReceivedAt,
		); err != nil {
			return nil, err
		}
		if len(log.Body) > 0 {
			log.BodyRaw = string(log.Body)
		}
		logs = append(logs, &log)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return logs, nil
}

func (s *webhookLogStore) DeleteByEndpointID(ctx context.Context, endpointID string) error {
	query := `
		DELETE FROM webhook_logs
		WHERE endpoint_id = $1;
	`
	_, err := s.db.ExecContext(ctx, query, endpointID)
	return err
}
