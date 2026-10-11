package handlers

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"webhooklens/internal/cache"
	"webhooklens/internal/config"
	"webhooklens/internal/models"
	"webhooklens/internal/store"
	"webhooklens/internal/worker"
)

type IngestionHandler struct {
	store      store.Store
	cache      cache.Cache
	workerPool *worker.Pool
	cfg        *config.Config
}

func NewIngestionHandler(store store.Store, cache cache.Cache, workerPool *worker.Pool, cfg *config.Config) *IngestionHandler {
	return &IngestionHandler{
		store:      store,
		cache:      cache,
		workerPool: workerPool,
		cfg:        cfg,
	}
}

func (h *IngestionHandler) Ingest(w http.ResponseWriter, r *http.Request) {
	endpointID := chi.URLParam(r, "endpoint_id")
	if endpointID == "" {
		respondError(w, http.StatusBadRequest, "endpoint ID is required")
		return
	}

	// Check endpoint validity and configuration via Redis cache / DB fallback
	epConfig, err := h.getEndpointConfig(r.Context(), endpointID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			respondError(w, http.StatusNotFound, "webhook endpoint not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to verify endpoint")
		return
	}

	if !epConfig.IsActive {
		respondError(w, http.StatusNotFound, "webhook endpoint is inactive")
		return
	}

	// Validate Authorization if require_auth is enabled
	if epConfig.RequireAuth {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			respondError(w, http.StatusUnauthorized, "authorization required: missing Authorization header")
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			respondError(w, http.StatusUnauthorized, "invalid authorization header format, expected 'Bearer <token>'")
			return
		}

		providedToken := parts[1]
		if epConfig.AuthToken == nil || subtle.ConstantTimeCompare([]byte(providedToken), []byte(*epConfig.AuthToken)) != 1 {
			respondError(w, http.StatusUnauthorized, "invalid webhook auth token")
			return
		}
	}

	// Enforce MaxBytesReader (5 MB limit)
	maxBytes := h.cfg.MaxRequestBodyMB << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) || strings.Contains(err.Error(), "request body too large") {
			respondError(w, http.StatusRequestEntityTooLarge, "request body exceeds 5MB limit")
			return
		}
		respondError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	// Capture metadata
	headersJSON, _ := json.Marshal(extractHeaders(r.Header))
	queryParamsJSON, _ := json.Marshal(extractQueryParams(r.URL.Query()))
	clientIP := getClientIP(r)
	now := time.Now().UTC()
	logID := uuid.New()

	webhookLog := &models.WebhookLog{
		ID:          logID,
		EndpointID:  endpointID,
		Method:      r.Method,
		Headers:     headersJSON,
		QueryParams: queryParamsJSON,
		Body:        bodyBytes,
		IPAddress:   clientIP,
		ReceivedAt:  now,
	}

	webhookEvent := &models.WebhookEvent{
		ID:          logID,
		EndpointID:  endpointID,
		Method:      r.Method,
		Headers:     headersJSON,
		QueryParams: queryParamsJSON,
		Body:        string(bodyBytes),
		BodySize:    len(bodyBytes),
		ContentType: r.Header.Get("Content-Type"),
		IPAddress:   clientIP,
		ReceivedAt:  now,
	}

	// Pass payload to asynchronous worker pool
	enqueued := h.workerPool.Enqueue(&worker.IngestionJob{
		Log:   webhookLog,
		Event: webhookEvent,
	})
	if !enqueued {
		respondError(w, http.StatusServiceUnavailable, "ingestion queue busy, try again later")
		return
	}

	// Return 202 Accepted immediately upon successful validation
	respondJSON(w, http.StatusAccepted, map[string]any{
		"status":      "accepted",
		"id":          logID,
		"endpoint_id": endpointID,
		"received_at": now,
	})
}

func (h *IngestionHandler) getEndpointConfig(ctx context.Context, endpointID string) (*models.EndpointConfig, error) {
	// Try Redis cache first
	cfg, err := h.cache.GetEndpointConfig(ctx, endpointID)
	if err == nil && cfg != nil {
		return cfg, nil
	}

	// Cache miss: query database
	endpoint, err := h.store.Endpoints().GetByID(ctx, endpointID)
	if err != nil {
		return nil, err
	}

	cfg = endpoint.ToConfig()
	// Warm cache in background
	_ = h.cache.SetEndpointConfig(ctx, endpointID, cfg, h.cfg.CacheTTL)

	return cfg, nil
}

func extractHeaders(header http.Header) map[string]any {
	result := make(map[string]any, len(header))
	for k, v := range header {
		if len(v) == 1 {
			result[k] = v[0]
		} else {
			result[k] = v
		}
	}
	return result
}

func extractQueryParams(values map[string][]string) map[string]any {
	result := make(map[string]any, len(values))
	for k, v := range values {
		if len(v) == 1 {
			result[k] = v[0]
		} else {
			result[k] = v
		}
	}
	return result
}

func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xrip := r.Header.Get("X-Real-IP"); xrip != "" {
		return strings.TrimSpace(xrip)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
