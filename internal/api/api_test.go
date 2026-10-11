package api_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"webhooklens/internal/api"
	"webhooklens/internal/cache"
	"webhooklens/internal/config"
	"webhooklens/internal/mailer"
	"webhooklens/internal/models"
	"webhooklens/internal/store/postgres"
	"webhooklens/internal/worker"
)

func setupTestApp(t *testing.T) (http.Handler, *postgres.PostgresStore, *cache.RedisCache, *worker.Pool, *config.Config) {
	cfg := config.Load()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pgStore, err := postgres.New(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("failed to connect to test postgres: %v", err)
	}

	redisCache, err := cache.NewRedisCache(ctx, cfg.RedisURL)
	if err != nil {
		t.Fatalf("failed to connect to test redis: %v", err)
	}

	workerPool := worker.NewPool(pgStore, redisCache, 5, 100)
	workerPool.Start()

	mailService := mailer.New(cfg)

	router := api.NewRouter(api.RouterDependencies{
		Store:      pgStore,
		Cache:      redisCache,
		WorkerPool: workerPool,
		Config:     cfg,
		Mailer:     mailService,
	})

	return router, pgStore, redisCache, workerPool, cfg
}

func createVerifiedUser(t *testing.T, router http.Handler, pgStore *postgres.PostgresStore, email, password string) string {
	regBody, _ := json.Marshal(map[string]string{
		"first_name": "Test",
		"last_name":  "User",
		"email":      email,
		"password":   password,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(regBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("failed to register user: %d %s", rec.Code, rec.Body.String())
	}

	var regResp struct {
		User *models.User `json:"user"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &regResp)
	if err := pgStore.Users().SetVerified(context.Background(), regResp.User.ID); err != nil {
		t.Fatalf("failed to set verified: %v", err)
	}

	loginBody, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": password,
	})
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("failed to login: %d %s", rec.Code, rec.Body.String())
	}

	var loginResp struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &loginResp)
	return loginResp.Token
}

func TestHealthCheck(t *testing.T) {
	router, pgStore, redisCache, workerPool, _ := setupTestApp(t)
	defer pgStore.Close()
	defer redisCache.Close()
	defer workerPool.Stop(context.Background())

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUserRegistrationAndEmailVerification(t *testing.T) {
	router, pgStore, redisCache, workerPool, _ := setupTestApp(t)
	defer pgStore.Close()
	defer redisCache.Close()
	defer workerPool.Stop(context.Background())

	testEmail := fmt.Sprintf("dev_%d@example.com", time.Now().UnixNano())
	testPassword := "securePassword123!"

	// 1. Register with first name, last name, email, password
	regBody, _ := json.Marshal(map[string]string{
		"first_name": "Alice",
		"last_name":  "Smith",
		"email":      testEmail,
		"password":   testPassword,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(regBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("register failed: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var regResp struct {
		Message string       `json:"message"`
		User    *models.User `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &regResp); err != nil {
		t.Fatalf("failed to unmarshal register response: %v", err)
	}

	if regResp.User.FirstName != "Alice" || regResp.User.LastName != "Smith" {
		t.Fatalf("expected name Alice Smith, got %s %s", regResp.User.FirstName, regResp.User.LastName)
	}
	if regResp.User.IsVerified {
		t.Fatal("expected newly registered user to be unverified")
	}

	// 2. Attempt login before email verification -> expect 403 Forbidden
	loginBody, _ := json.Marshal(map[string]string{
		"email":    testEmail,
		"password": testPassword,
	})
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for unverified login, got %d: %s", rec.Code, rec.Body.String())
	}

	// Resend verification
	resendBody, _ := json.Marshal(map[string]string{"email": testEmail})
	req = httptest.NewRequest(http.MethodPost, "/api/auth/resend-verification", bytes.NewReader(resendBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("resend verification failed: %d %s", rec.Code, rec.Body.String())
	}

	// Mark verified
	if err := pgStore.Users().SetVerified(context.Background(), regResp.User.ID); err != nil {
		t.Fatalf("failed to verify user: %v", err)
	}

	// 4. Login after verification -> expect 200 OK
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("login failed after verification: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var loginResp struct {
		Token string       `json:"token"`
		User  *models.User `json:"user"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &loginResp)
	if loginResp.Token == "" {
		t.Fatal("expected non-empty token")
	}

	// 5. /api/auth/me
	req = httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+loginResp.Token)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("me endpoint failed: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var me models.User
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if me.FirstName != "Alice" || me.LastName != "Smith" || !me.IsVerified {
		t.Fatalf("expected verified Alice Smith, got %+v", me)
	}
}

func TestEndpointManagementAndTokenRegeneration(t *testing.T) {
	router, pgStore, redisCache, workerPool, _ := setupTestApp(t)
	defer pgStore.Close()
	defer redisCache.Close()
	defer workerPool.Stop(context.Background())

	testEmail := fmt.Sprintf("ep_dev_%d@example.com", time.Now().UnixNano())
	token := createVerifiedUser(t, router, pgStore, testEmail, "strongPassword123!")

	// Create Endpoint
	createBody, _ := json.Marshal(map[string]any{
		"name":         "GitHub Webhooks",
		"require_auth": true,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/endpoints", bytes.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create endpoint failed: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var createdEndpoint models.Endpoint
	if err := json.Unmarshal(rec.Body.Bytes(), &createdEndpoint); err != nil {
		t.Fatalf("failed to decode endpoint: %v", err)
	}

	if createdEndpoint.ID == "" {
		t.Fatal("endpoint ID must not be empty")
	}
	if !createdEndpoint.RequireAuth || createdEndpoint.AuthToken == nil || *createdEndpoint.AuthToken == "" {
		t.Fatal("endpoint must have require_auth true and non-empty auth_token")
	}

	originalAuthToken := *createdEndpoint.AuthToken

	// Regenerate Token
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/endpoints/%s/regenerate-token", createdEndpoint.ID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("regenerate token failed: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var updatedEndpoint models.Endpoint
	_ = json.Unmarshal(rec.Body.Bytes(), &updatedEndpoint)

	if *updatedEndpoint.AuthToken == originalAuthToken {
		t.Fatal("regenerated auth token should be different from original token")
	}

	// Toggle require_auth to false
	toggleBody, _ := json.Marshal(map[string]bool{"require_auth": false})
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/endpoints/%s/toggle-auth", createdEndpoint.ID), bytes.NewReader(toggleBody))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("toggle auth failed: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var toggledEndpoint models.Endpoint
	_ = json.Unmarshal(rec.Body.Bytes(), &toggledEndpoint)
	if toggledEndpoint.RequireAuth {
		t.Fatal("expected require_auth to be false")
	}
}

func TestWebhookIngestionAndWorkerProcessing(t *testing.T) {
	router, pgStore, redisCache, workerPool, _ := setupTestApp(t)
	defer pgStore.Close()
	defer redisCache.Close()
	defer workerPool.Stop(context.Background())

	testEmail := fmt.Sprintf("ingest_%d@example.com", time.Now().UnixNano())
	token := createVerifiedUser(t, router, pgStore, testEmail, "strongPassword123!")

	// Create endpoint with require_auth = true
	createBody, _ := json.Marshal(map[string]any{
		"name":         "Stripe Webhook Receiver",
		"require_auth": true,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/endpoints", bytes.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var ep models.Endpoint
	_ = json.Unmarshal(rec.Body.Bytes(), &ep)
	authToken := *ep.AuthToken

	webhookPayload := `{"event":"payment.succeeded","amount":4900,"currency":"usd"}`

	// Unauthorized request (missing header)
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/w/%s", ep.ID), strings.NewReader(webhookPayload))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for missing auth, got %d", rec.Code)
	}

	// Unauthorized request (wrong token)
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/w/%s", ep.ID), strings.NewReader(webhookPayload))
	req.Header.Set("Authorization", "Bearer invalid-token-xyz")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for invalid token, got %d", rec.Code)
	}

	// Authorized request -> 202 Accepted
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/w/%s?source=test", ep.ID), strings.NewReader(webhookPayload))
	req.Header.Set("Authorization", "Bearer "+authToken)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d: %s", rec.Code, rec.Body.String())
	}

	var acceptResp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &acceptResp)
	if acceptResp["status"] != "accepted" {
		t.Fatalf("expected accepted status, got %v", acceptResp["status"])
	}

	// Wait for worker pool to persist to PostgreSQL
	time.Sleep(200 * time.Millisecond)

	// Verify log persisted in DB via /api/endpoints/{endpoint_id}/logs
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/endpoints/%s/logs", ep.ID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from logs, got %d: %s", rec.Code, rec.Body.String())
	}

	var logs []*models.WebhookLog
	_ = json.Unmarshal(rec.Body.Bytes(), &logs)
	if len(logs) == 0 {
		t.Fatal("expected at least 1 persisted webhook log")
	}

	if logs[0].Method != http.MethodPost {
		t.Fatalf("expected method POST, got %s", logs[0].Method)
	}
	if logs[0].BodyRaw != webhookPayload {
		t.Fatalf("expected body %s, got %s", webhookPayload, logs[0].BodyRaw)
	}
}

func TestMaxBytesLimitEnforcement(t *testing.T) {
	router, pgStore, redisCache, workerPool, _ := setupTestApp(t)
	defer pgStore.Close()
	defer redisCache.Close()
	defer workerPool.Stop(context.Background())

	testEmail := fmt.Sprintf("limit_%d@example.com", time.Now().UnixNano())
	token := createVerifiedUser(t, router, pgStore, testEmail, "strongPassword123!")

	createBody, _ := json.Marshal(map[string]any{
		"name":         "Large Payload Endpoint",
		"require_auth": false,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/endpoints", bytes.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var ep models.Endpoint
	_ = json.Unmarshal(rec.Body.Bytes(), &ep)

	// Send payload > 5MB (e.g. 5.5MB)
	oversizedData := bytes.Repeat([]byte("A"), (5<<20)+512)
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/w/%s", ep.ID), bytes.NewReader(oversizedData))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 Payload Too Large, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRealTimeSSEStream(t *testing.T) {
	router, pgStore, redisCache, workerPool, _ := setupTestApp(t)
	defer pgStore.Close()
	defer redisCache.Close()
	defer workerPool.Stop(context.Background())

	testEmail := fmt.Sprintf("sse_%d@example.com", time.Now().UnixNano())
	token := createVerifiedUser(t, router, pgStore, testEmail, "strongPassword123!")

	// Create endpoint
	createBody, _ := json.Marshal(map[string]any{
		"name":         "SSE Stream Test",
		"require_auth": false,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/endpoints", bytes.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var ep models.Endpoint
	_ = json.Unmarshal(rec.Body.Bytes(), &ep)

	// Start test HTTP server for SSE connection
	server := httptest.NewServer(router)
	defer server.Close()

	// Connect SSE client with ?token= query parameter
	streamCtx, cancelStream := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelStream()

	streamReq, err := http.NewRequestWithContext(streamCtx, http.MethodGet, fmt.Sprintf("%s/api/endpoints/%s/stream?token=%s", server.URL, ep.ID, token), nil)
	if err != nil {
		t.Fatalf("failed to create stream request: %v", err)
	}

	streamResp, err := http.DefaultClient.Do(streamReq)
	if err != nil {
		t.Fatalf("failed to connect to stream: %v", err)
	}
	defer streamResp.Body.Close()

	if streamResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for SSE stream, got %d", streamResp.StatusCode)
	}
	if !strings.Contains(streamResp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("expected Content-Type text/event-stream, got %s", streamResp.Header.Get("Content-Type"))
	}

	reader := bufio.NewReader(streamResp.Body)

	// Read initial connected handshake
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read connected event: %v", err)
	}
	if !strings.Contains(line, "event: connected") {
		t.Fatalf("expected connected event line, got: %s", line)
	}

	// Trigger a webhook ingestion
	payload := `{"test":"sse_stream_message"}`
	ingestReq, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/w/%s", server.URL, ep.ID), strings.NewReader(payload))
	if err != nil {
		t.Fatalf("failed to create ingest request: %v", err)
	}
	ingestReq.Header.Set("Content-Type", "application/json")
	ingestResp, err := http.DefaultClient.Do(ingestReq)
	if err != nil {
		t.Fatalf("failed to execute ingest request: %v", err)
	}
	io.Copy(io.Discard, ingestResp.Body)
	ingestResp.Body.Close()

	if ingestResp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 from ingest, got %d", ingestResp.StatusCode)
	}

	// Read SSE events until we receive the webhook event
	receivedWebhook := false
	for i := 0; i < 20; i++ {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		if strings.Contains(line, "event: webhook") {
			receivedWebhook = true
			break
		}
	}

	if !receivedWebhook {
		t.Fatal("did not receive 'event: webhook' via SSE stream")
	}
}
