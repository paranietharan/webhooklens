package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"webhooklens/internal/models"
)

var (
	ErrNotFound            = errors.New("resource not found")
	ErrDuplicateEmail      = errors.New("email already registered")
	ErrDuplicateToken      = errors.New("token already exists")
	ErrTokenExpired        = errors.New("verification token has expired")
	ErrUserAlreadyVerified = errors.New("user is already verified")
)

type Store interface {
	Users() UserStore
	Endpoints() EndpointStore
	WebhookLogs() WebhookLogStore
	Ping(ctx context.Context) error
	Close() error
}

type UserStore interface {
	Create(ctx context.Context, user *models.User) error
	GetByEmail(ctx context.Context, email string) (*models.User, error)
	GetByID(ctx context.Context, id uuid.UUID) (*models.User, error)
	SetVerified(ctx context.Context, userID uuid.UUID) error
	CreateEmailVerification(ctx context.Context, ev *models.EmailVerification) error
	GetEmailVerificationByToken(ctx context.Context, token string) (*models.EmailVerification, error)
	DeleteEmailVerification(ctx context.Context, token string) error
	DeleteEmailVerificationsByUserID(ctx context.Context, userID uuid.UUID) error
}

type EndpointStore interface {
	Create(ctx context.Context, endpoint *models.Endpoint) error
	GetByID(ctx context.Context, id string) (*models.Endpoint, error)
	ListByUserID(ctx context.Context, userID uuid.UUID) ([]*models.Endpoint, error)
	Update(ctx context.Context, endpoint *models.Endpoint) error
	Delete(ctx context.Context, id string, userID uuid.UUID) error
	ToggleRequireAuth(ctx context.Context, id string, userID uuid.UUID, requireAuth bool) (*models.Endpoint, error)
	ToggleActive(ctx context.Context, id string, userID uuid.UUID, isActive *bool) (*models.Endpoint, error)
	UpdateAuthToken(ctx context.Context, id string, userID uuid.UUID, authToken *string) (*models.Endpoint, error)
}

type WebhookLogStore interface {
	Create(ctx context.Context, log *models.WebhookLog) error
	GetByID(ctx context.Context, id uuid.UUID, endpointID string) (*models.WebhookLog, error)
	ListByEndpointID(ctx context.Context, endpointID string, limit, offset int) ([]*models.WebhookLog, error)
	DeleteByEndpointID(ctx context.Context, endpointID string) error
}
