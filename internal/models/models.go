package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID           uuid.UUID `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

type Endpoint struct {
	ID          string    `json:"id"`
	UserID      uuid.UUID `json:"user_id"`
	Name        string    `json:"name"`
	RequireAuth bool      `json:"require_auth"`
	AuthToken   *string   `json:"auth_token,omitempty"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
}

type EndpointConfig struct {
	ID          string    `json:"id"`
	UserID      uuid.UUID `json:"user_id"`
	RequireAuth bool      `json:"require_auth"`
	AuthToken   *string   `json:"auth_token,omitempty"`
	IsActive    bool      `json:"is_active"`
}

func (e *Endpoint) ToConfig() *EndpointConfig {
	return &EndpointConfig{
		ID:          e.ID,
		UserID:      e.UserID,
		RequireAuth: e.RequireAuth,
		AuthToken:   e.AuthToken,
		IsActive:    e.IsActive,
	}
}

type WebhookLog struct {
	ID          uuid.UUID       `json:"id"`
	EndpointID  string          `json:"endpoint_id"`
	Method      string          `json:"method"`
	Headers     json.RawMessage `json:"headers"`
	QueryParams json.RawMessage `json:"query_params"`
	Body        []byte          `json:"-"`
	BodyRaw     string          `json:"body,omitempty"`
	IPAddress   string          `json:"ip_address"`
	ReceivedAt  time.Time       `json:"received_at"`
}

type WebhookEvent struct {
	ID          uuid.UUID       `json:"id"`
	EndpointID  string          `json:"endpoint_id"`
	Method      string          `json:"method"`
	Headers     json.RawMessage `json:"headers"`
	QueryParams json.RawMessage `json:"query_params"`
	Body        string          `json:"body"`
	BodySize    int             `json:"body_size"`
	ContentType string          `json:"content_type"`
	IPAddress   string          `json:"ip_address"`
	ReceivedAt  time.Time       `json:"received_at"`
}
