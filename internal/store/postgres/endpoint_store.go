package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"
	"webhooklens/internal/models"
	"webhooklens/internal/store"
)

type endpointStore struct {
	db *sql.DB
}

func NewEndpointStore(db *sql.DB) store.EndpointStore {
	return &endpointStore{db: db}
}

func (s *endpointStore) Create(ctx context.Context, endpoint *models.Endpoint) error {
	query := `
		INSERT INTO endpoints (id, user_id, name, require_auth, auth_token, is_active, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at;
	`
	err := s.db.QueryRowContext(
		ctx,
		query,
		endpoint.ID,
		endpoint.UserID,
		endpoint.Name,
		endpoint.RequireAuth,
		endpoint.AuthToken,
		endpoint.IsActive,
		endpoint.CreatedAt,
	).Scan(&endpoint.CreatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") && strings.Contains(err.Error(), "auth_token") {
			return store.ErrDuplicateToken
		}
		return err
	}
	return nil
}

func (s *endpointStore) GetByID(ctx context.Context, id string) (*models.Endpoint, error) {
	query := `
		SELECT id, user_id, name, require_auth, auth_token, is_active, created_at
		FROM endpoints
		WHERE id = $1;
	`
	var ep models.Endpoint
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&ep.ID,
		&ep.UserID,
		&ep.Name,
		&ep.RequireAuth,
		&ep.AuthToken,
		&ep.IsActive,
		&ep.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	return &ep, nil
}

func (s *endpointStore) ListByUserID(ctx context.Context, userID uuid.UUID) ([]*models.Endpoint, error) {
	query := `
		SELECT id, user_id, name, require_auth, auth_token, is_active, created_at
		FROM endpoints
		WHERE user_id = $1
		ORDER BY created_at DESC;
	`
	rows, err := s.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var endpoints []*models.Endpoint
	for rows.Next() {
		var ep models.Endpoint
		if err := rows.Scan(
			&ep.ID,
			&ep.UserID,
			&ep.Name,
			&ep.RequireAuth,
			&ep.AuthToken,
			&ep.IsActive,
			&ep.CreatedAt,
		); err != nil {
			return nil, err
		}
		endpoints = append(endpoints, &ep)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return endpoints, nil
}

func (s *endpointStore) Update(ctx context.Context, endpoint *models.Endpoint) error {
	query := `
		UPDATE endpoints
		SET name = $1, is_active = $2
		WHERE id = $3 AND user_id = $4;
	`
	result, err := s.db.ExecContext(ctx, query, endpoint.Name, endpoint.IsActive, endpoint.ID, endpoint.UserID)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *endpointStore) Delete(ctx context.Context, id string, userID uuid.UUID) error {
	query := `
		DELETE FROM endpoints
		WHERE id = $1 AND user_id = $2;
	`
	result, err := s.db.ExecContext(ctx, query, id, userID)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *endpointStore) ToggleRequireAuth(ctx context.Context, id string, userID uuid.UUID, requireAuth bool) (*models.Endpoint, error) {
	query := `
		UPDATE endpoints
		SET require_auth = $1
		WHERE id = $2 AND user_id = $3
		RETURNING id, user_id, name, require_auth, auth_token, is_active, created_at;
	`
	var ep models.Endpoint
	err := s.db.QueryRowContext(ctx, query, requireAuth, id, userID).Scan(
		&ep.ID,
		&ep.UserID,
		&ep.Name,
		&ep.RequireAuth,
		&ep.AuthToken,
		&ep.IsActive,
		&ep.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	return &ep, nil
}

func (s *endpointStore) ToggleActive(ctx context.Context, id string, userID uuid.UUID, isActive *bool) (*models.Endpoint, error) {
	var query string
	var args []any

	if isActive != nil {
		query = `
			UPDATE endpoints
			SET is_active = $1
			WHERE id = $2 AND user_id = $3
			RETURNING id, user_id, name, require_auth, auth_token, is_active, created_at;
		`
		args = []any{*isActive, id, userID}
	} else {
		query = `
			UPDATE endpoints
			SET is_active = NOT is_active
			WHERE id = $1 AND user_id = $2
			RETURNING id, user_id, name, require_auth, auth_token, is_active, created_at;
		`
		args = []any{id, userID}
	}

	var ep models.Endpoint
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&ep.ID,
		&ep.UserID,
		&ep.Name,
		&ep.RequireAuth,
		&ep.AuthToken,
		&ep.IsActive,
		&ep.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	return &ep, nil
}

func (s *endpointStore) UpdateAuthToken(ctx context.Context, id string, userID uuid.UUID, authToken *string) (*models.Endpoint, error) {
	query := `
		UPDATE endpoints
		SET auth_token = $1
		WHERE id = $2 AND user_id = $3
		RETURNING id, user_id, name, require_auth, auth_token, is_active, created_at;
	`
	var ep models.Endpoint
	err := s.db.QueryRowContext(ctx, query, authToken, id, userID).Scan(
		&ep.ID,
		&ep.UserID,
		&ep.Name,
		&ep.RequireAuth,
		&ep.AuthToken,
		&ep.IsActive,
		&ep.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		if strings.Contains(err.Error(), "duplicate key") && strings.Contains(err.Error(), "auth_token") {
			return nil, store.ErrDuplicateToken
		}
		return nil, err
	}
	return &ep, nil
}
