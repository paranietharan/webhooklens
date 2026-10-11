package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"webhooklens/internal/api/middleware"
	"webhooklens/internal/cache"
	"webhooklens/internal/config"
	"webhooklens/internal/store"
)

type StreamHandler struct {
	store store.Store
	cache cache.Cache
	cfg   *config.Config
}

func NewStreamHandler(store store.Store, cache cache.Cache, cfg *config.Config) *StreamHandler {
	return &StreamHandler{
		store: store,
		cache: cache,
		cfg:   cfg,
	}
}

func (h *StreamHandler) Stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		respondError(w, http.StatusInternalServerError, "streaming unsupported by server")
		return
	}

	// Authenticate client: check context (if middleware applied) or Bearer header or query param
	userID, authenticated := middleware.GetUserID(r.Context())
	if !authenticated {
		var tokenStr string
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
				tokenStr = parts[1]
			}
		}
		if tokenStr == "" {
			tokenStr = r.URL.Query().Get("token")
		}

		if tokenStr == "" {
			respondError(w, http.StatusUnauthorized, "unauthorized: authentication token required")
			return
		}

		claims, err := middleware.ValidateToken(tokenStr, h.cfg.JWTSecret)
		if err != nil {
			respondError(w, http.StatusUnauthorized, "unauthorized: invalid or expired token")
			return
		}
		userID = claims.UserID
	}

	endpointID := chi.URLParam(r, "endpoint_id")
	if endpointID == "" {
		respondError(w, http.StatusBadRequest, "endpoint ID is required")
		return
	}

	// Verify endpoint existence and ownership
	endpoint, err := h.store.Endpoints().GetByID(r.Context(), endpointID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			respondError(w, http.StatusNotFound, "endpoint not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to verify endpoint")
		return
	}

	if endpoint.UserID != userID && userID != uuid.Nil {
		respondError(w, http.StatusForbidden, "access denied")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// Send initial connected handshake
	_, _ = fmt.Fprintf(w, "event: connected\ndata: {\"endpoint_id\":\"%s\",\"status\":\"listening\"}\n\n", endpointID)
	flusher.Flush()

	// Subscribe to Redis pub/sub channel for endpoint:{endpoint_id}
	pubsub := h.cache.Subscribe(r.Context(), endpointID)
	defer pubsub.Close()

	ch := pubsub.Channel()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	notify := r.Context().Done()

	for {
		select {
		case <-notify:
			return

		case <-ticker.C:
			// Heartbeat comment to prevent client / proxy timeouts
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()

		case msg, ok := <-ch:
			if !ok {
				return
			}
			// Stream incoming webhook payload event to the client connection
			if _, err := fmt.Fprintf(w, "event: webhook\ndata: %s\n\n", msg.Payload); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
