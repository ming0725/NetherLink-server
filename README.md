# NLServer

NetherLink backend foundation, built from `backend-requirements.md`.

Current scope is intentionally small:

- Go module bootstrap under `cmd/` and `internal/`.
- Environment-based config loading under `internal/config`.
- Thin HTTP server under `internal/server`.
- PostgreSQL connection pool setup under `internal/database`.
- Redis client setup under `internal/cache`.
- PostgreSQL repositories for users, user preferences, and user sessions under `internal/repository`.
- Object storage interface skeleton under `internal/storage`.
- PostgreSQL schema bootstrap under `db/`.
- Redis service bootstrap under `docker-compose.yml`.
- MinIO object storage bootstrap under `minio/`.
- Health endpoints, API v1 root, request IDs, panic recovery, structured access logs, JSON 404/405 responses, request body helper functions, readiness checks for PostgreSQL and Redis, and a Bearer auth middleware skeleton are available.
- No business API handlers, auth token validation, MinIO SDK implementation, or file processing workers yet.
- Redis is connected as infrastructure only. It is not yet used for login rate limits, online presence, token/session acceleration, WebSocket fanout, or temporary workflow state.

## Start Local Dependencies

```sh
cp .env.example .env
docker compose up -d postgres redis minio minio-init
docker compose ps
```

## Run The API Server

```sh
go run ./cmd/nlserver
```

Default server address:

- HTTP: http://localhost:8080
- Liveness: `GET /healthz`
- Readiness: `GET /readyz`
- API root: `GET /api/v1`

`/healthz` only verifies that the process is serving HTTP. `/readyz` pings PostgreSQL and Redis, and returns `503 SERVICE_NOT_READY` until both dependencies are reachable.
Unknown routes and unsupported methods return the standard JSON error shape with `code`, `message`, and `requestId`.

## API Foundation

All HTTP responses written by the shared helpers use JSON and preserve `X-Request-Id`.
The current API foundation includes:

- `GET /api/v1` as the versioned API root.
- Structured request logs with method, path, status, response size, duration, request ID, remote address, and user agent.
- Panic recovery mapped to `INTERNAL_ERROR`.
- JSON `NOT_FOUND` and `METHOD_NOT_ALLOWED` fallback responses.
- JSON body decoding helpers with unknown-field rejection and a 1 MiB request body limit.
- Bearer auth middleware skeleton. It only extracts a token today; actual token validation belongs to the P0 auth implementation.

PostgreSQL:

- Host: `localhost`
- Port: `5432`
- Database: `netherlink`
- User: `netherlink_app`

Redis:

- Host: `localhost`
- Port: `6379`
- Database: `0`
- Password: empty by default for local development

MinIO:

- S3 API: http://localhost:9000
- Console: http://localhost:9001
- Default local credentials are defined in `.env.example`.

## Configuration Naming

Project-level configuration uses the `NETHERLINK_` prefix, for example `NETHERLINK_POSTGRES_PASSWORD` and `NETHERLINK_OBJECT_STORAGE_SECRET_KEY`.
Docker Compose maps those values to the official container variables required by PostgreSQL and MinIO.

## Storage Boundary

The backend should treat MinIO as an internal S3-compatible provider. Public API responses should expose only stable file IDs, public URLs, or short-lived signed URLs. Bucket names, object keys, internal endpoints, and credentials should stay inside backend configuration and database metadata.

Prepared buckets:

- `netherlink-public`
- `netherlink-protected`
- `netherlink-private`
- `netherlink-tmp`

## Redis Boundary

Redis is an auxiliary runtime dependency. PostgreSQL remains the source of truth for users, preferences, sessions, messages, notifications, file metadata, and durable business state.

Initial intended Redis uses:

- Login/register/IP rate limiting.
- Short-lived verification or workflow state.
- WebSocket online presence and later fanout support.
- Optional access-token/session lookup acceleration after PostgreSQL-backed auth is implemented.

Redis keys should be namespaced with `nl:` and should have explicit TTLs unless they represent process-owned online state.

## Repository Boundary

The first PostgreSQL repositories cover:

- `users`: create account records, read by UUID/email/public ID/account, update profile fields, update public ID with history, increment token version, and update last-seen time.
- `user_preferences`: ensure defaults, upsert full preferences, read by user UUID, and patch preference fields.
- `user_sessions`: create refresh-token sessions, read active sessions, list user sessions, rotate refresh token hash, revoke sessions, and update session last-seen time.

These repositories are persistence primitives only. They do not hash passwords, mint tokens, enforce auth policy, or write HTTP responses.

## Testing

Repository tests live under `tests/` instead of the production `internal/` package directories.
Run all current tests with:

```sh
go test ./...
```

The former `internal/database/postgres_test.go` was a single DSN unit test, not a duplicate or repeated test file. It now lives at `tests/postgres_dsn_test.go` with the rest of the external test suite.

## Next Build Steps

1. Implement P0 auth service and handlers: register, login, refresh token rotation, logout, `/me`, `/me/preferences`, and access token validation.
2. Use Redis for auth rate limits and short-lived token/session acceleration only after PostgreSQL-backed auth is correct.
3. Add a MinIO-backed implementation of `ObjectStorageProvider`.
4. Add only the file upload-session API skeleton before implementing full chat, posts, or AI workflows.
