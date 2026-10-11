# WebHookLens

Headless backend API for inspecting, capturing, and debugging incoming webhooks in real-time.

## Architecture

* **Language:** Go 1.21+
* **Routing:** `go-chi/chi/v5`
* **Storage:** PostgreSQL 16 (via `pgx/v5` and `database/sql`)
* **Cache & Pub/Sub:** Redis 7 (`redis/go-redis/v9`)
* **Real-time Engine:** Server-Sent Events (SSE)
* **Ingestion Worker:** Asynchronous, non-blocking bounded worker pool

---

## Project Structure

```
.
├── cmd/
│   └── server/
│       └── main.go                  # Application entry point & graceful shutdown
├── internal/
│   ├── api/
│   │   ├── router.go                # Chi router, middleware, and route mounting
│   │   ├── middleware/
│   │   │   └── auth.go              # JWT stateless authentication middleware
│   │   └── handlers/
│   │       ├── auth.go              # User registration and login handlers
│   │       ├── endpoints.go         # Endpoint CRUD, token regeneration, logs inspection
│   │       ├── ingest.go            # High-throughput webhook ingestion (/w/{endpoint_id})
│   │       ├── response.go          # JSON response helpers
│   │       └── stream.go            # Server-Sent Events (SSE) real-time streaming
│   ├── cache/
│   │   └── cache.go                 # Redis cache & Pub/Sub broker implementation
│   ├── config/
│   │   └── config.go                # Environment variable loader
│   ├── models/
│   │   └── models.go                # Core domain and DTO models
│   ├── store/
│   │   ├── store.go                 # Database repository interfaces
│   │   └── postgres/
│   │       ├── postgres.go          # Database connection pool and schema auto-migration
│   │       ├── user_store.go        # User PostgreSQL repository
│   │       ├── endpoint_store.go    # Webhook endpoint PostgreSQL repository
│   │       └── webhook_log_store.go # Webhook logs PostgreSQL repository
│   └── worker/
│       └── dispatcher.go            # Asynchronous ingestion worker pool & drain logic
├── migrations/                      # PostgreSQL migration scripts
├── docker-compose.yml               # PostgreSQL & Redis infrastructure
└── Dockerfile                       # Multi-stage production container build
```

---

## Quickstart

### 1. Start Infrastructure

```bash
docker compose up -d
```

### 2. Run the Server

```bash
go run cmd/server/main.go
```

The server listens on `http://localhost:8080`.

### 3. Run the Test Suite

```bash
go test -v ./...
```

---

## API Reference

### Authentication
* `POST /api/auth/register` - Create developer account (`email`, `password`)
* `POST /api/auth/login` - Authenticate and obtain JWT token
* `GET /api/auth/me` - Get current authenticated user profile

### Endpoint Management
All endpoints require `Authorization: Bearer <JWT_TOKEN>`.
* `POST /api/endpoints` - Create webhook endpoint (`name`, `require_auth`)
* `GET /api/endpoints` - List developer's endpoints
* `GET /api/endpoints/{endpoint_id}` - Get endpoint details
* `PATCH /api/endpoints/{endpoint_id}` - Update endpoint (`name`, `is_active`)
* `DELETE /api/endpoints/{endpoint_id}` - Delete endpoint
* `POST /api/endpoints/{endpoint_id}/toggle-auth` - Toggle `require_auth`
* `POST /api/endpoints/{endpoint_id}/regenerate-token` - Generate a new 32-byte secure URL-safe base64 token
* `GET /api/endpoints/{endpoint_id}/logs` - Inspect captured webhook logs (`limit`, `offset`)
* `GET /api/endpoints/{endpoint_id}/logs/{log_id}` - Inspect single webhook log
* `DELETE /api/endpoints/{endpoint_id}/logs` - Clear logs for an endpoint

### Webhook Ingestion (`/w/{endpoint_id}`)
* **Supported Methods:** `POST`, `GET`, `PUT`, `PATCH`, `DELETE`, etc.
* **Payload Limit:** Enforces 5MB via `http.MaxBytesReader`
* **Cache Check:** Fast endpoint verification via Redis cache
* **Bearer Validation:** When `require_auth: true`, validates `Authorization: Bearer <token>` in constant time (`crypto/subtle.ConstantTimeCompare`)
* **Response:** Returns `202 Accepted` immediately, delegating persistence and Pub/Sub publishing to the asynchronous worker pool.

### Real-time SSE Stream (`/api/endpoints/{endpoint_id}/stream`)
* **Headers:** `text/event-stream`, `Cache-Control: no-cache`
* **Authentication:** `Authorization: Bearer <JWT_TOKEN>` header or `?token=<JWT_TOKEN>` query parameter (for standard browser `EventSource`)
* **Events:**
  * Initial handshake: `event: connected`
  * Heartbeat: `: ping` (every 15s)
  * Webhook payload: `event: webhook` with full JSON payload
