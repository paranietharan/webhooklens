package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	gonanoid "github.com/matoous/go-nanoid/v2"
	"webhooklens/internal/api/middleware"
	"webhooklens/internal/cache"
	"webhooklens/internal/config"
	"webhooklens/internal/models"
	"webhooklens/internal/store"
)

type EndpointHandler struct {
	store store.Store
	cache cache.Cache
	cfg   *config.Config
}

func NewEndpointHandler(store store.Store, cache cache.Cache, cfg *config.Config) *EndpointHandler {
	return &EndpointHandler{
		store: store,
		cache: cache,
		cfg:   cfg,
	}
}

func GenerateSecureToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

type createEndpointRequest struct {
	Name        string `json:"name"`
	RequireAuth bool   `json:"require_auth"`
}

type updateEndpointRequest struct {
	Name     *string `json:"name"`
	IsActive *bool   `json:"is_active"`
}

type toggleAuthRequest struct {
	RequireAuth bool `json:"require_auth"`
}

func (h *EndpointHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req createEndpointRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		respondError(w, http.StatusBadRequest, "name is required")
		return
	}

	id, err := gonanoid.New(21)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to generate endpoint ID")
		return
	}

	var authToken *string
	if req.RequireAuth {
		token, err := GenerateSecureToken()
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to generate auth token")
			return
		}
		authToken = &token
	}

	endpoint := &models.Endpoint{
		ID:          id,
		UserID:      userID,
		Name:        req.Name,
		RequireAuth: req.RequireAuth,
		AuthToken:   authToken,
		IsActive:    true,
		CreatedAt:   time.Now().UTC(),
	}

	if err := h.store.Endpoints().Create(r.Context(), endpoint); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create endpoint")
		return
	}

	// Warm cache
	_ = h.cache.SetEndpointConfig(r.Context(), endpoint.ID, endpoint.ToConfig(), h.cfg.CacheTTL)

	respondJSON(w, http.StatusCreated, endpoint)
}

func (h *EndpointHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	endpoints, err := h.store.Endpoints().ListByUserID(r.Context(), userID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list endpoints")
		return
	}
	if endpoints == nil {
		endpoints = []*models.Endpoint{}
	}

	respondJSON(w, http.StatusOK, endpoints)
}

func (h *EndpointHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	endpointID := chi.URLParam(r, "endpoint_id")
	endpoint, err := h.store.Endpoints().GetByID(r.Context(), endpointID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			respondError(w, http.StatusNotFound, "endpoint not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to retrieve endpoint")
		return
	}

	if endpoint.UserID != userID {
		respondError(w, http.StatusForbidden, "access denied")
		return
	}

	respondJSON(w, http.StatusOK, endpoint)
}

func (h *EndpointHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	endpointID := chi.URLParam(r, "endpoint_id")
	endpoint, err := h.store.Endpoints().GetByID(r.Context(), endpointID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			respondError(w, http.StatusNotFound, "endpoint not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to retrieve endpoint")
		return
	}

	if endpoint.UserID != userID {
		respondError(w, http.StatusForbidden, "access denied")
		return
	}

	var req updateEndpointRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name != nil {
		if *req.Name == "" {
			respondError(w, http.StatusBadRequest, "name cannot be empty")
			return
		}
		endpoint.Name = *req.Name
	}
	if req.IsActive != nil {
		endpoint.IsActive = *req.IsActive
	}

	if err := h.store.Endpoints().Update(r.Context(), endpoint); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update endpoint")
		return
	}

	// Invalidate and refresh cache
	_ = h.cache.SetEndpointConfig(r.Context(), endpoint.ID, endpoint.ToConfig(), h.cfg.CacheTTL)

	respondJSON(w, http.StatusOK, endpoint)
}

func (h *EndpointHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	endpointID := chi.URLParam(r, "endpoint_id")
	if err := h.store.Endpoints().Delete(r.Context(), endpointID, userID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			respondError(w, http.StatusNotFound, "endpoint not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to delete endpoint")
		return
	}

	_ = h.cache.InvalidateEndpointConfig(r.Context(), endpointID)

	respondJSON(w, http.StatusOK, map[string]string{"message": "endpoint deleted successfully"})
}

func (h *EndpointHandler) ToggleAuth(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	endpointID := chi.URLParam(r, "endpoint_id")
	endpoint, err := h.store.Endpoints().GetByID(r.Context(), endpointID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			respondError(w, http.StatusNotFound, "endpoint not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to retrieve endpoint")
		return
	}

	if endpoint.UserID != userID {
		respondError(w, http.StatusForbidden, "access denied")
		return
	}

	var req toggleAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// If enabling auth and no auth token exists yet, generate one
	if req.RequireAuth && (endpoint.AuthToken == nil || *endpoint.AuthToken == "") {
		token, err := GenerateSecureToken()
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to generate auth token")
			return
		}
		endpoint, err = h.store.Endpoints().UpdateAuthToken(r.Context(), endpointID, userID, &token)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to set auth token")
			return
		}
	}

	updated, err := h.store.Endpoints().ToggleRequireAuth(r.Context(), endpointID, userID, req.RequireAuth)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to toggle auth")
		return
	}

	_ = h.cache.SetEndpointConfig(r.Context(), updated.ID, updated.ToConfig(), h.cfg.CacheTTL)

	respondJSON(w, http.StatusOK, updated)
}

func (h *EndpointHandler) RegenerateToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	endpointID := chi.URLParam(r, "endpoint_id")
	endpoint, err := h.store.Endpoints().GetByID(r.Context(), endpointID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			respondError(w, http.StatusNotFound, "endpoint not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to retrieve endpoint")
		return
	}

	if endpoint.UserID != userID {
		respondError(w, http.StatusForbidden, "access denied")
		return
	}

	newToken, err := GenerateSecureToken()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to generate secure token")
		return
	}

	updated, err := h.store.Endpoints().UpdateAuthToken(r.Context(), endpointID, userID, &newToken)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update auth token")
		return
	}

	_ = h.cache.SetEndpointConfig(r.Context(), updated.ID, updated.ToConfig(), h.cfg.CacheTTL)

	respondJSON(w, http.StatusOK, updated)
}

func (h *EndpointHandler) ListLogs(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	endpointID := chi.URLParam(r, "endpoint_id")
	endpoint, err := h.store.Endpoints().GetByID(r.Context(), endpointID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			respondError(w, http.StatusNotFound, "endpoint not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to retrieve endpoint")
		return
	}

	if endpoint.UserID != userID {
		respondError(w, http.StatusForbidden, "access denied")
		return
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil && val > 0 {
			limit = val
		}
	}

	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		if val, err := strconv.Atoi(o); err == nil && val >= 0 {
			offset = val
		}
	}

	logs, err := h.store.WebhookLogs().ListByEndpointID(r.Context(), endpointID, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to retrieve webhook logs")
		return
	}
	if logs == nil {
		logs = []*models.WebhookLog{}
	}

	respondJSON(w, http.StatusOK, logs)
}

func (h *EndpointHandler) GetLog(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	endpointID := chi.URLParam(r, "endpoint_id")
	endpoint, err := h.store.Endpoints().GetByID(r.Context(), endpointID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			respondError(w, http.StatusNotFound, "endpoint not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to retrieve endpoint")
		return
	}

	if endpoint.UserID != userID {
		respondError(w, http.StatusForbidden, "access denied")
		return
	}

	logIDStr := chi.URLParam(r, "log_id")
	logID, err := uuid.Parse(logIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid log ID")
		return
	}

	logEntry, err := h.store.WebhookLogs().GetByID(r.Context(), logID, endpointID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			respondError(w, http.StatusNotFound, "webhook log not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to retrieve webhook log")
		return
	}

	respondJSON(w, http.StatusOK, logEntry)
}

func (h *EndpointHandler) ClearLogs(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	endpointID := chi.URLParam(r, "endpoint_id")
	endpoint, err := h.store.Endpoints().GetByID(r.Context(), endpointID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			respondError(w, http.StatusNotFound, "endpoint not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to retrieve endpoint")
		return
	}

	if endpoint.UserID != userID {
		respondError(w, http.StatusForbidden, "access denied")
		return
	}

	if err := h.store.WebhookLogs().DeleteByEndpointID(r.Context(), endpointID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to clear webhook logs")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "logs cleared successfully"})
}
