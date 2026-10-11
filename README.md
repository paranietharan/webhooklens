# WebHookLens

Headless backend API for inspecting, capturing, and debugging incoming webhooks in real-time.

## Quickstart

### 1. Start Infrastructure

```bash
docker compose up -d
```

### 2. Run Database Migrations

```bash
# Apply all pending migrations
go run cmd/migration/main.go up

# Check migration status
go run cmd/migration/main.go status

# Rollback last migration
go run cmd/migration/main.go down
```

### 3. Run the Server

```bash
go run cmd/server/main.go
```

The server listens on `http://localhost:8080`.

### 3. Run the Test Suite

```bash
go test -v ./...
```

## Configuration

| Configuration | Example | Description |
| :--- | :--- | :--- |
| `PORT` | `8080` | Port for the HTTP API server |
| `DATABASE_URL` | `postgres://webhooklens:webhooklens_secret@localhost:5432/webhooklens?sslmode=disable` | PostgreSQL connection URL |
| `POSTGRES_USER` | `webhooklens` | PostgreSQL username |
| `POSTGRES_PASSWORD` | `webhooklens_secret` | PostgreSQL user password |
| `POSTGRES_DB` | `webhooklens` | PostgreSQL database name |
| `POSTGRES_PORT` | `5432` | Host port exposed for PostgreSQL container |
| `REDIS_URL` | `redis://localhost:6379/0` | Redis connection URL for caching and Pub/Sub |
| `REDIS_PORT` | `6379` | Host port exposed for Redis container |
| `JWT_SECRET` | `replace-with-a-secure-random-32-byte-secret-key!` | Secret key used to sign and verify HMAC-SHA256 JWTs |
| `JWT_EXPIRATION_HOURS` | `24` | Expiration time of issued JWT tokens in hours |
| `SMTP_HOST` | `localhost` | Host of SMTP server for email delivery (Mailpit) |
| `SMTP_PORT` | `1025` | Port of SMTP server for email delivery (Mailpit) |
| `SMTP_FROM` | `noreply@webhooklens.local` | Sender address used in verification emails |
| `MAILPIT_UI_PORT` | `8025` | Port for Mailpit web inbox UI (`http://localhost:8025`) |
| `APP_BASE_URL` | `http://localhost:8080` | Application base URL used in email verification links |
| `WORKER_POOL_SIZE` | `10` | Number of worker goroutines in asynchronous ingestion pool |
| `WORKER_BUFFER_SIZE` | `5000` | Buffer capacity of the worker pool job queue |
| `MAX_REQUEST_BODY_MB` | `5` | Maximum allowed request body size for webhooks in MB |
| `ENDPOINT_CACHE_TTL_MINUTES` | `5` | Time-to-live for endpoint configurations cached in Redis |