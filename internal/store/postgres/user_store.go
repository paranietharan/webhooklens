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

type userStore struct {
	db *sql.DB
}

func NewUserStore(db *sql.DB) store.UserStore {
	return &userStore{db: db}
}

func (s *userStore) Create(ctx context.Context, user *models.User) error {
	query := `
		INSERT INTO users (id, email, password_hash, created_at)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at;
	`
	if user.ID == uuid.Nil {
		user.ID = uuid.New()
	}

	err := s.db.QueryRowContext(ctx, query, user.ID, strings.ToLower(user.Email), user.PasswordHash, user.CreatedAt).
		Scan(&user.ID, &user.CreatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "users_email_key") {
			return store.ErrDuplicateEmail
		}
		return err
	}
	return nil
}

func (s *userStore) GetByEmail(ctx context.Context, email string) (*models.User, error) {
	query := `
		SELECT id, email, password_hash, created_at
		FROM users
		WHERE email = $1;
	`
	var user models.User
	err := s.db.QueryRowContext(ctx, query, strings.ToLower(email)).
		Scan(&user.ID, &user.Email, &user.PasswordHash, &user.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

func (s *userStore) GetByID(ctx context.Context, id uuid.UUID) (*models.User, error) {
	query := `
		SELECT id, email, password_hash, created_at
		FROM users
		WHERE id = $1;
	`
	var user models.User
	err := s.db.QueryRowContext(ctx, query, id).
		Scan(&user.ID, &user.Email, &user.PasswordHash, &user.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}
