# NLServer

NetherLink backend foundation, built from `backend-requirements.md`.

Current scope is intentionally small:

- Go module bootstrap under `cmd/` and `internal/`.
- Environment-based config loading under `internal/config`.
- Thin HTTP server under `internal/server`.
- PostgreSQL connection pool setup under `internal/database`.
- Redis client setup under `internal/cache`.
- PostgreSQL repositories for users, user preferences, user sessions, files, chat, friends, groups, and realtime events under `internal/repository`.
- Shared repository infrastructure: aggregate repository construction, transaction helper, and operation receipt repository for idempotent writes.
- P0 auth service under `internal/auth`: password hashing, access token signing/validation, refresh token hashing/rotation, PostgreSQL-backed session checks, current-user profile updates, public ID checks/changes, and device session management.
- Application runtime assembly under `internal/app` for PostgreSQL, Redis, repositories, auth service, and future storage provider injection.
- Object storage interface and MinIO/S3 provider under `internal/storage`.
- PostgreSQL schema bootstrap under `db/`.
- Redis service bootstrap under `docker-compose.yml`.
- MinIO object storage bootstrap under `minio/`.
- Health endpoints, API v1 root, request IDs, panic recovery, structured access logs, JSON 404/405 responses, request body helper functions, readiness checks for PostgreSQL and Redis, and Bearer access-token validation are available.
- P0 auth HTTP handlers are available: register, login, refresh, logout, session listing/revoke, `/me`, profile editing, public ID availability/change, `GET /me/preferences`, and `PATCH /me/preferences`.
- User/friend/group HTTP handlers are available: user search/detail/batch, friend list/group list, friend group creation, friend settings update/delete, friend request create/list/accept/reject, and group list/detail/member list.
- File upload-session HTTP handlers are available for presigned single and multipart MinIO/S3 uploads.
- File upload completion now queues image, video, and audio processing jobs; the runtime starts a background worker that reads uploaded media from MinIO, extracts backend-side image dimensions, generates 512px JPEG image thumbnails, probes video/audio duration with `ffprobe`, extracts 512px video cover JPEGs with `ffmpeg`, retries stale processing jobs, records derived image/video variants in `file_variants`, and moves file status from `processing` to `ready` or `failed`.
- Chat conversation list/open/detail/settings/read/unread endpoints are available, along with message send, history, sender recall, and current-user hidden-history clearing; image attachments require and return `width` and `height` for frontend preview placeholders.
- WebSocket realtime foundation is available at `GET /api/v1/ws`: access-token auth, per-user connection management, PostgreSQL-backed global event IDs, durable replay, Redis pub/sub fanout, `ping`/`realtime.pong`, `chat.message.created`, `chat.message.recalled`, `chat.conversation.sync.updated`, hidden-history sync, and `chat.message.send`.
- No file cleanup worker, group write handlers, post/AI API handlers, realtime cleanup policy, or fanout operational metrics yet.
- Redis is used for auth rate limiting and realtime fanout today. It is not used as the source of truth for users, refresh token sessions, messages, notifications, realtime events, file metadata, or durable business state.

## Start Local Dependencies

```sh
cp .env.example .env
# Fill NETHERLINK_AUTH_TOKEN_SECRET, NETHERLINK_POSTGRES_PASSWORD,
# NETHERLINK_OBJECT_STORAGE_ACCESS_KEY, and NETHERLINK_OBJECT_STORAGE_SECRET_KEY.
docker compose up -d postgres redis minio minio-init
docker compose ps
```

## Run The API Server

```sh
set -a
. ./.env
set +a
go run ./cmd/nlserver
```

Default server address:

- HTTP: http://localhost:8080
- Liveness: `GET /healthz`
- Readiness: `GET /readyz`
- API root: `GET /api/v1`

`/healthz` only verifies that the process is serving HTTP. `/readyz` pings PostgreSQL and Redis, and returns `503 SERVICE_NOT_READY` until both dependencies are reachable.
Unknown routes and unsupported methods return the standard JSON error shape with `code`, `message`, and `requestId`.

Auth configuration:

- `NETHERLINK_AUTH_TOKEN_SECRET`: HMAC signing secret for access tokens. Required; at least 32 characters.
- `NETHERLINK_AUTH_ACCESS_TOKEN_TTL`: default `15m`.
- `NETHERLINK_AUTH_REFRESH_TOKEN_TTL`: default `720h`.

## API Foundation

All HTTP responses written by the shared helpers use JSON and preserve `X-Request-Id`.
The current API foundation includes:

- `GET /api/v1` as the versioned API root.
- Structured request logs with method, path, status, response size, duration, request ID, remote address, and user agent.
- Panic recovery mapped to `INTERNAL_ERROR`.
- JSON `NOT_FOUND` and `METHOD_NOT_ALLOWED` fallback responses.
- JSON body decoding helpers with unknown-field rejection and a 1 MiB request body limit.
- Bearer auth middleware validates access token signature, expiry, active PostgreSQL session, active user status, and `tokenVersion`.
- WebSocket endpoint: `GET /api/v1/ws` accepts `Authorization: Bearer <access_token>`, `?accessToken=`, or `?token=`; optional `lastEventSeq` / `lastEventId=evt_N` enables durable replay when PostgreSQL is available.
- WebSocket command: send `{"type":"chat.message.send","payload":{...}}` with `conversationId` plus the same message fields as REST `POST /conversations/{conversationId}/messages`; success emits durable `chat.message.created` plus non-durable `chat.message.sent` ACK to the sender.
- Auth endpoints: `POST /api/v1/auth/register`, `POST /api/v1/auth/login`, `POST /api/v1/auth/refresh`, `POST /api/v1/auth/logout`, `GET /api/v1/auth/sessions`, `POST /api/v1/auth/sessions/{sessionId}/revoke`, `GET /api/v1/me`, `PATCH /api/v1/me`, `GET /api/v1/me/user-id/check`, `PATCH /api/v1/me/user-id`, `GET /api/v1/me/preferences`, and `PATCH /api/v1/me/preferences`.
- Directory endpoints: `GET /api/v1/users`, `GET /api/v1/users/{userUuid}`, `GET /api/v1/users/by-id/{userId}`, `POST /api/v1/users/batch`, `GET /api/v1/friends`, `PATCH /api/v1/friends/{friendUserUuid}`, `DELETE /api/v1/friends/{friendUserUuid}`, `GET /api/v1/friend-groups`, `POST /api/v1/friend-groups`, `POST /api/v1/friend-requests`, `GET /api/v1/friend-requests`, `POST /api/v1/friend-requests/{requestId}/accept`, `POST /api/v1/friend-requests/{requestId}/reject`, `GET /api/v1/groups`, `GET /api/v1/groups/{groupId}`, and `GET /api/v1/groups/{groupId}/members`.
- File endpoints: `POST /api/v1/files/upload-session`, `GET /api/v1/files/upload-session/{uploadSessionId}`, `POST /api/v1/files/upload-session/{uploadSessionId}/parts`, `POST /api/v1/files/upload-session/{uploadSessionId}/complete`, and `POST /api/v1/files/upload-session/{uploadSessionId}/abort`.
- File upload completion returns `processingStatus` from the file storage status. Image, video, and audio uploads enter `processing` until the background worker records metadata and any supported variants; other file types move directly to `ready` for now.
- Chat endpoints: `GET /api/v1/conversations`, `POST /api/v1/conversations/direct`, `POST /api/v1/conversations/group`, `GET /api/v1/conversations/{conversationId}`, `PATCH /api/v1/conversations/{conversationId}/settings`, `POST /api/v1/conversations/{conversationId}/read`, `POST /api/v1/conversations/{conversationId}/unread`, `POST /api/v1/conversations/{conversationId}/messages`, `GET /api/v1/conversations/{conversationId}/messages`, `POST /api/v1/conversations/{conversationId}/messages/{messageId}/recall`, and `DELETE /api/v1/conversations/{conversationId}/messages`. Image messages must include attachment `width` and `height`; message responses include those fields on each attachment.

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
- Local credentials must be filled in `.env`; `.env.example` is only a committed template.

## Configuration Naming

Project-level configuration uses the `NETHERLINK_` prefix, for example `NETHERLINK_POSTGRES_PASSWORD` and `NETHERLINK_OBJECT_STORAGE_SECRET_KEY`.
Docker Compose maps those values to the official container variables required by PostgreSQL and MinIO.

## Configuration And Secrets

`.env.example` is not a production environment file. It is a committed template that documents required variable names. Real passwords, token signing secrets, object-storage credentials, and production endpoints belong in `.env`, deployment environment variables, or a secret manager.

The API server intentionally fails during startup when required secret variables are missing or malformed. Non-secret local defaults are kept only for developer ergonomics, such as `localhost` hosts and default ports.

## Storage Boundary

The backend should treat MinIO as an internal S3-compatible provider. Public API responses should expose only stable file IDs, public URLs, or short-lived signed URLs. Bucket names, object keys, internal endpoints, and credentials should stay inside backend configuration and database metadata.

Prepared buckets:

- `netherlink-public`
- `netherlink-protected`
- `netherlink-private`
- `netherlink-tmp`

## Redis Boundary

Redis is an auxiliary runtime dependency. PostgreSQL remains the source of truth for users, preferences, sessions, messages, notifications, realtime events, file metadata, and durable business state.

Initial intended Redis uses:

- Login/register/IP rate limiting.
- Short-lived verification or workflow state.
- WebSocket realtime pub/sub fanout on `nl:realtime:events`.
- WebSocket online presence support.
- Optional access-token/session lookup acceleration after PostgreSQL-backed auth is implemented.

Redis keys should be namespaced with `nl:` and should have explicit TTLs unless they represent process-owned online state.

## Repository Boundary

The first PostgreSQL repositories cover:

- `users`: create account records, read by UUID/email/public ID/account, update profile fields, check and update public ID with history, enforce public ID change idempotency, increment token version, and update last-seen time.
- `user_preferences`: ensure defaults, upsert full preferences, read by user UUID, and patch preference fields.
- `user_sessions`: create refresh-token sessions, read active sessions, list user sessions, rotate refresh token hash, revoke sessions, and update session last-seen time.
- `files`: create file metadata, upload sessions, multipart part tracking, upload completion, status transitions, object-storage metadata bookkeeping, file processing job claims/results, and file variant records.
- `chat`: list/open/read/update conversations, persist participant sync state, persist messages, assign per-conversation `message_seq`, persist message attachments, recall sender messages, hide current-user history with `hidden_at`, return attachment file metadata, and preserve image `width` and `height`.
- `friends`: create and list current-user friend groups, list active friends with keyword/group filters, read current-user friendship relation metadata, update/delete friendship settings, create/list/accept/reject friend requests, and upsert bidirectional friendships when accepting requests.
- `groups`: list current-user groups with keyword/category filters, read group details only for members, and page group members only when the viewer belongs to the group.
- `realtime_events`: persist globally sequenced WebSocket business events, record per-user recipients, list durable replay events after a client cursor, and report the latest sequence.
- `operation_receipts`: record and replay idempotent operation results by `user_id + client_operation_id + operation_type`.

These repositories are persistence primitives only. They do not hash passwords, mint tokens, enforce auth policy, or write HTTP responses.

Repository infrastructure:

- `repository.WithTx` and `repository.WithTxOptions` provide a single transaction boundary for service-layer operations that need to update multiple tables.
- `repository.Repositories` is the dependency aggregate passed into services, so new repositories should be added there before handlers depend on them directly.

## Runtime Boundary

`internal/app` owns runtime dependency assembly and lifecycle for PostgreSQL, Redis, repository aggregates, auth service, file service, file processing worker, and object storage provider injection. `cmd/nlserver` should stay thin: load configuration, create the runtime, pass dependencies into `server.NewWithDependencies`, and start HTTP serving.

The MinIO/S3 provider implements object PUT/GET/HEAD/DELETE, presigned GET/PUT URLs, multipart upload creation, part presigning, completion, and abort. File metadata remains in PostgreSQL; public API responses do not expose bucket names or object keys.

## File Processing Boundary

`internal/files` now includes the upload-session service and a background processing worker. Completing an uploaded image, video, or audio file creates a `file_processing_jobs` row and moves the file to `processing`. The worker claims pending jobs with `FOR UPDATE SKIP LOCKED`, resets jobs stuck in `processing` for more than 15 minutes, reads the original object through the storage provider, records the job result JSON, and marks the file `ready`. Processing failures are recorded on the job and mark the file `failed`.

The current worker supports image metadata and `thumbnail_512` JPEG variants, video metadata plus `video_cover_512` JPEG variants through `ffprobe` / `ffmpeg`, and audio duration probing through `ffprobe`. Orphan object cleanup and business-level usage cleanup are still future work.

## Chat Boundary

Chat conversation support includes listing visible conversations, opening or creating direct conversations, opening group conversations for existing group members, reading conversation metadata, updating pin/DND/hidden settings, marking conversations read or unread, sender-side message recall, and current-user hidden-history clearing. Sending a message validates that the sender is a participant, assigns the next `messageSeq` in a transaction, stores attachments in `message_attachments`, updates the conversation last-message fields, increments unread counts for other participants, and brings hidden conversations back into the visible list.

Image message attachments must include positive `width` and `height`. The same fields are returned in send and history responses so clients can allocate preview placeholders before the signed image URL finishes loading. Recalled message responses keep message metadata and `recalledAt` but omit content and attachment details. WebSocket broadcasts reuse the REST `message` response shape for `chat.message.created` and `chat.message.recalled` payloads. The `chat.message.send` WebSocket command reuses the same chat service validation and idempotency path as REST.

## Realtime Boundary

`internal/realtime` is the WebSocket hub plus Redis fanout transport. PostgreSQL `realtime_events` assigns globally increasing `evt_N` IDs and stores per-user recipients for durable replay. The in-process hub still tracks online connections by user/session/device and keeps a short per-user replay window as fallback. Connection-control events such as `realtime.ready`, `realtime.pong`, `chat.message.sent`, and `client.error` use `eventSeq: 0` and are not replayed.

Current pushed events:

- `realtime.ready` after successful WebSocket auth.
- `realtime.pong` when the client sends `{"type":"ping"}`.
- `chat.message.created` after REST message send succeeds.
- `chat.message.recalled` after REST sender recall succeeds.
- `chat.conversation.sync.updated` after read/unread/settings/hidden-history changes and after new messages update participant unread state.

Incoming WebSocket command:

- `chat.message.send` with `conversationId`, `clientMessageId`, `type`, `content`, `attachments`, `clientSentAt`, and `referencedMessageId`; it reuses the REST chat service validation and idempotency path.

PostgreSQL remains the source of truth. Redis only fans out already-persisted events between API instances; replay after reconnect reads from PostgreSQL when available.

## Auth Boundary

`internal/auth` owns password hashing, refresh token hashing, access-token signing, refresh token rotation, access-token validation, current-user profile validation, public ID policy, and user session management. PostgreSQL remains authoritative for users and refresh token sessions. Redis keys used by auth are namespaced as `nl:rate:auth:*`, have TTLs, and are only rate-limit counters.

Public IDs must match `[A-Za-z0-9_]{4,32}`. `PATCH /api/v1/me/user-id` requires `clientOperationId`, records successful changes in `user_id_changes`, and enforces a 30-day cooldown after each successful public ID change. Self-service `PATCH /api/v1/me` accepts `status` only as `active`; online presence states belong to the later realtime presence layer, not the account-active status used by token validation.

## Testing

Repository tests live under `tests/` instead of the production `internal/` package directories.
Run all current tests with:

```sh
go test ./...
```

The former `internal/database/postgres_test.go` was a single DSN unit test, not a duplicate or repeated test file. It now lives at `tests/postgres_dsn_test.go` with the rest of the external test suite.

## Next Build Steps

1. Add orphan/unused object cleanup for expired upload sessions, unbound files, and deleted `file_usages`.
2. Add realtime event cleanup policy, replay metrics, and operational health checks for Redis fanout.
3. Add group write APIs, post APIs, and AI workflows after the chat/file metadata paths settle.
