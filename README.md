# WebHookLens

WebHookLens is a Go backend service for capturing, inspecting, and streaming incoming webhooks in real time. Built with **Go (`chi/v5`)**, **PostgreSQL (`pgx/v5`)**, and **Redis**, it accepts webhook requests with `HTTP 202 Accepted` and processes persistence and live browser notifications in the background.

---

## How It Works

```
Webhook Sender ──► ANY /w/{endpoint_id}
                        │
                        ├─► 1. Lookup endpoint config in Redis (fallback to PostgreSQL)
                        ├─► 2. Validate active status & optional secret token
                        ├─► 3. Enqueue payload into buffered goroutine worker pool
                        └─► 4. Return HTTP 202 Accepted immediately
                                     │
                                     ▼ (Background Worker Pool)
                        ┌─────────────────────────────────────────┐
                        │ • Save request headers/body in Postgres │
                        │ • Publish event to Redis Pub/Sub        │
                        └────────────────────┬────────────────────┘
                                             │
                                             ▼
Connected Browsers ◄── GET /api/endpoints/{endpoint_id}/stream (SSE + 15s keep-alive ping)
```

### Key Implementation Details
- **Non-Blocking Webhook Ingestion (`internal/api/handlers/ingest.go`)**: Incoming webhooks on `/w/{endpoint_id}` are validated, pushed to an in-memory buffered job queue (`WORKER_BUFFER_SIZE`), and acknowledged with `202 Accepted` so database writes do not block the request path.
- **Worker Pool with Graceful Drain (`internal/worker/dispatcher.go`)**: A configurable pool of worker goroutines (`WORKER_POOL_SIZE`) writes webhook logs to PostgreSQL and publishes events to Redis. On `SIGINT`/`SIGTERM`, the server stops accepting new HTTP requests and drains in-flight jobs before exiting.
- **Live Server-Sent Events (`internal/api/handlers/stream.go`)**: Clients subscribe to `/api/endpoints/{endpoint_id}/stream` over SSE (`text/event-stream`). The handler subscribes to the endpoint's Redis Pub/Sub channel and sends `: ping` heartbeats every 15 seconds to keep connections open through proxies.
- **Endpoint Caching (`internal/cache/redis.go`)**: Endpoint configurations and secret tokens are cached in Redis (`ENDPOINT_CACHE_TTL_MINUTES`) with automatic fallback to PostgreSQL on cache misses, and invalidated whenever an endpoint is updated or deleted.

---

## Quick Start

### 1. Start PostgreSQL, Redis, and Mailpit
```bash
cp .env.example .env
docker compose up -d
```

### 2. Run Database Migrations
```bash
# Apply all pending migrations
go run cmd/migration/main.go up

# Check current migration version
go run cmd/migration/main.go status

# Rollback the last migration
go run cmd/migration/main.go down
```

### 3. Start the API Server
```bash
go run cmd/server/main.go
```
The API listens on `http://localhost:8080` (Mailpit web inbox is at `http://localhost:8025`).

### 4. Run Tests
```bash
go test -v ./...
```

---

## API Endpoints

### Ingestion & Real-Time Stream
- `ANY /w/{endpoint_id}` — Capture an incoming webhook (supports any HTTP method; returns `202 Accepted`)
- `GET /api/endpoints/{endpoint_id}/stream` — Subscribe to live webhook events via Server-Sent Events (SSE)
- `GET /healthz` — Check PostgreSQL and Redis connectivity

### Authentication
- `POST /api/auth/register` — Register a user account and send verification email
- `GET /api/auth/verify-email` — Verify email address token
- `POST /api/auth/resend-verification` — Resend verification email
- `POST /api/auth/login` — Authenticate and receive a JWT
- `GET /api/auth/me` — Get current authenticated user profile

### Webhook Endpoint & Log Management (Bearer Auth)
- `POST /api/endpoints` — Create a new webhook endpoint
- `GET /api/endpoints` — List all endpoints for the authenticated user
- `GET /api/endpoints/{endpoint_id}` — Get endpoint details
- `PATCH /api/endpoints/{endpoint_id}` — Update endpoint name or description
- `DELETE /api/endpoints/{endpoint_id}` — Delete an endpoint and evict cache
- `POST /api/endpoints/{endpoint_id}/toggle-active` — Enable or pause webhook capture
- `POST /api/endpoints/{endpoint_id}/toggle-auth` — Enable or disable secret token requirement on `/w/{endpoint_id}`
- `POST /api/endpoints/{endpoint_id}/regenerate-token` — Rotate the endpoint ingestion secret token
- `GET /api/endpoints/{endpoint_id}/logs` — List captured webhook requests (paginated)
- `GET /api/endpoints/{endpoint_id}/logs/{log_id}` — Inspect full headers, query params, and body of a captured webhook
- `DELETE /api/endpoints/{endpoint_id}/logs` — Clear captured logs for an endpoint

---

## Configuration (`.env`)

| Variable | Example | Description |
| :--- | :--- | :--- |
| `PORT` | `8080` | HTTP server port |
| `DATABASE_URL` | `postgres://webhooklens:webhooklens_secret@localhost:5432/webhooklens?sslmode=disable` | PostgreSQL connection string |
| `REDIS_URL` | `redis://localhost:6379/0` | Redis connection URL for caching and Pub/Sub |
| `JWT_SECRET` | `replace-with-a-secure-random-32-byte-secret-key!` | Secret key used to sign HMAC-SHA256 JWTs |
| `JWT_EXPIRATION_HOURS` | `24` | JWT token lifetime in hours |
| `SMTP_HOST` | `localhost` | SMTP server host (`Mailpit` in local dev) |
| `SMTP_PORT` | `1025` | SMTP server port |
| `MAILPIT_UI_PORT` | `8025` | Mailpit web UI port (`http://localhost:8025`) |
| `WORKER_POOL_SIZE` | `10` | Number of background worker goroutines |
| `WORKER_BUFFER_SIZE` | `5000` | Capacity of the buffered webhook ingestion channel |
| `MAX_REQUEST_BODY_MB` | `5` | Maximum webhook request body size in MB |
| `ENDPOINT_CACHE_TTL_MINUTES` | `5` | Redis cache TTL for endpoint configurations |
