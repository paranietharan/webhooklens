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
		INSERT INTO users (id, first_name, last_name, email, password_hash, is_verified, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at;
	`
	if user.ID == uuid.Nil {
		user.ID = uuid.New()
	}

	err := s.db.QueryRowContext(
		ctx,
		query,
		user.ID,
		user.FirstName,
		user.LastName,
		strings.ToLower(user.Email),
		user.PasswordHash,
		user.IsVerified,
		user.CreatedAt,
	).Scan(&user.ID, &user.CreatedAt)
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
		SELECT id, first_name, last_name, email, password_hash, is_verified, created_at
		FROM users
		WHERE email = $1;
	`
	var user models.User
	err := s.db.QueryRowContext(ctx, query, strings.ToLower(email)).
		Scan(&user.ID, &user.FirstName, &user.LastName, &user.Email, &user.PasswordHash, &user.IsVerified, &user.CreatedAt)
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
		SELECT id, first_name, last_name, email, password_hash, is_verified, created_at
		FROM users
		WHERE id = $1;
	`
	var user models.User
	err := s.db.QueryRowContext(ctx, query, id).
		Scan(&user.ID, &user.FirstName, &user.LastName, &user.Email, &user.PasswordHash, &user.IsVerified, &user.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

func (s *userStore) SetVerified(ctx context.Context, userID uuid.UUID) error {
	query := `
		UPDATE users
		SET is_verified = TRUE
		WHERE id = $1;
	`
	result, err := s.db.ExecContext(ctx, query, userID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *userStore) CreateEmailVerification(ctx context.Context, ev *models.EmailVerification) error {
	query := `
		INSERT INTO email_verifications (id, user_id, token, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at;
	`
	if ev.ID == uuid.Nil {
		ev.ID = uuid.New()
	}
	return s.db.QueryRowContext(ctx, query, ev.ID, ev.UserID, ev.Token, ev.ExpiresAt, ev.CreatedAt).
		Scan(&ev.ID, &ev.CreatedAt)
}

func (s *userStore) GetEmailVerificationByToken(ctx context.Context, token string) (*models.EmailVerification, error) {
	query := `
		SELECT id, user_id, token, expires_at, created_at
		FROM email_verifications
		WHERE token = $1;
	`
	var ev models.EmailVerification
	err := s.db.QueryRowContext(ctx, query, token).
		Scan(&ev.ID, &ev.UserID, &ev.Token, &ev.ExpiresAt, &ev.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	return &ev, nil
}

func (s *userStore) DeleteEmailVerification(ctx context.Context, token string) error {
	query := `DELETE FROM email_verifications WHERE token = $1;`
	_, err := s.db.ExecContext(ctx, query, token)
	return err
}

func (s *userStore) DeleteEmailVerificationsByUserID(ctx context.Context, userID uuid.UUID) error {
	query := `DELETE FROM email_verifications WHERE user_id = $1;`
	_, err := s.db.ExecContext(ctx, query, userID)
	return err
}
