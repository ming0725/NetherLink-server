# NLServer

NetherLink backend foundation, built from `backend-requirements.md`.

Current scope is intentionally small:

- Go module bootstrap under `cmd/` and `internal/`.
- Environment-based config loading under `internal/config`.
- Thin HTTP server under `internal/server`.
- PostgreSQL connection pool setup under `internal/database`.
- Redis client setup under `internal/cache`.
- PostgreSQL repositories for users, user preferences, user sessions, files, chat, friends, groups, notifications, realtime events, and operation receipts under `internal/repository`.
- Shared repository infrastructure: aggregate repository construction, transaction helper, and operation receipt repository for idempotent writes.
- P0 auth service under `internal/auth`: password hashing, access token signing/validation, refresh token hashing/rotation, PostgreSQL-backed session checks, current-user profile updates, public ID checks/changes, and device session management.
- Application runtime assembly under `internal/app` for PostgreSQL, Redis, repositories, auth service, file workers, realtime fanout, realtime cleanup, and object storage provider injection.
- Object storage interface and MinIO/S3 provider under `internal/storage`.
- PostgreSQL schema bootstrap under `db/`.
- Redis service bootstrap under `docker-compose.yml`.
- MinIO object storage bootstrap under `minio/`.
- Health endpoints, API v1 root, request IDs, panic recovery, structured access logs, JSON 404/405 responses, request body helper functions, readiness checks for PostgreSQL and Redis, and Bearer access-token validation are available.
- P0 auth HTTP handlers are available: register, login, refresh, logout, session listing/revoke, `/me`, profile editing, public ID availability/change, avatar upload, `GET /me/preferences`, and `PATCH /me/preferences`.
- User/friend/group HTTP handlers are available: user search/detail/batch, friend list/group list, friend group creation, friend settings update/delete, friend request create/list/accept/reject, group list/detail/member list, group create/edit/delete, membership management, owner transfer, member settings, current-user group settings, and join-request handling.
- File HTTP handlers are available for avatar upload, proxy upload, presigned single and multipart MinIO/S3 upload sessions, file metadata, controlled download URLs, and owner-side deletion of unreferenced files.
- File upload completion now queues image, video, and audio processing jobs; the runtime starts a background worker that reads uploaded media from MinIO, extracts backend-side image dimensions, generates 512px JPEG image thumbnails, probes video/audio duration with `ffprobe`, extracts 512px video cover JPEGs with `ffmpeg`, retries stale processing jobs, records derived image/video variants in `file_variants`, and moves file status from `processing` to `ready` or `failed`.
- File cleanup worker is available for expired upload sessions, unbound files, and files whose `file_usages` are all deleted; it deletes original and variant objects from MinIO and soft-deletes cleaned file metadata.
- File metadata and download URLs now enforce business-aware access checks: chat attachments require visible conversation participation, group avatars require group membership or upload ownership, and post media follows public/author visibility.
- Chat conversation list/open/detail/settings/read/unread endpoints are available, along with message send, history, sender recall, and current-user hidden-history clearing; image attachments require and return `width` and `height` for frontend preview placeholders.
- Profile, preference, group, file, and post detail resources now return HTTP `ETag` headers; GET detail endpoints honor `If-None-Match` with `304 Not Modified`, and mutable profile/group/file/post writes can reject stale `If-Match` or `expectedVersion` inputs with `409 VERSION_CONFLICT`.
- WebSocket realtime foundation is available at `GET /api/v1/ws`: access-token auth, per-user connection management, PostgreSQL-backed global event IDs, durable replay, Redis pub/sub fanout, replay metrics, fanout operational counters, `ping`/`realtime.pong`, chat events, notification read sync, friend/group change events, profile/preference change events, post count/version update events, hidden-history sync, and `chat.message.send`.
- Realtime event cleanup is available: `realtime_events` are retained for the configured window, old events are deleted in batches, and `/readyz` exposes hub, fanout, and cleanup snapshots.
- Post HTTP handlers are available for feed/detail/create/edit/delete, media binding, likes, follows, comments, replies, comment/reply likes, and comment/reply deletion.
- Notification HTTP handlers are available for listing notifications, marking all notifications read, and marking one notification read.
- Operation receipt HTTP handler is available for idempotent client-operation result lookup and creation.
- AI repositories, service, OpenAI-compatible provider client, and AI HTTP handlers are available for conversations, messages, uploaded-file binding, context metadata, feedback, and title generation. REST message generation is synchronous by default and supports SSE streaming with `Accept: text/event-stream` or `?stream=true`; WebSocket `ai.message.send` streams provider chunks and supports `ai.stream.cancel`. Uploaded text-like, PDF, docx, pptx, and xlsx AI files are parsed into `ai_file_chunks` and relevant chunks are injected into provider prompts. PDF extraction uses optional `pdftotext` first and can OCR scanned pages with optional `pdftoppm` + `tesseract`; docx extraction includes supplemental document parts such as headers, footers, footnotes, endnotes, and comments. AI files using `retentionPolicy=source_deleted_after_parse` delete the original object and soft-delete source file metadata after successful parsing when no non-AI business reference still needs the file.
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

`.env` is the local file that supplies environment variables to Docker Compose and the API server. `NETHERLINK_ENV` is only one variable inside that file; it selects the runtime mode, defaults to `local`, and enables stricter validation when set to `production`.

`/healthz` only verifies that the process is serving HTTP. `/readyz` pings PostgreSQL and Redis, returns `503 SERVICE_NOT_READY` until both dependencies are reachable, and includes realtime hub, Redis fanout, replay, and cleanup counters when ready.
Unknown routes and unsupported methods return the standard JSON error shape with `code`, `message`, and `requestId`.

Auth configuration:

- `NETHERLINK_AUTH_TOKEN_SECRET`: HMAC signing secret for access tokens. Required; at least 32 characters.
- `NETHERLINK_AUTH_ACCESS_TOKEN_TTL`: default `15m`.
- `NETHERLINK_AUTH_REFRESH_TOKEN_TTL`: default `720h`.

Realtime configuration:

- `NETHERLINK_REALTIME_EVENT_RETENTION`: default `168h`.
- `NETHERLINK_REALTIME_EVENT_CLEANUP_INTERVAL`: default `1h`.
- `NETHERLINK_REALTIME_EVENT_CLEANUP_BATCH`: default `5000`.

AI configuration:

- `NETHERLINK_AI_PROVIDER`: default `disabled`; supported configured values are `openai` and `openai-compatible`.
- `NETHERLINK_AI_API_KEY`: required when the AI provider is enabled.
- `NETHERLINK_AI_BASE_URL`: optional provider base URL, mainly for OpenAI-compatible providers.
- `NETHERLINK_AI_DEFAULT_MODEL`: required when the AI provider is enabled.
- `NETHERLINK_AI_TITLE_MODEL`: optional separate title-generation model.
- `NETHERLINK_AI_THINKING_TYPE`: optional OpenAI-compatible reasoning extension; supported values are `enabled` and `disabled`.
- `NETHERLINK_AI_REASONING_EFFORT`: optional OpenAI-compatible reasoning extension; supported values are `low`, `medium`, and `high`.
- `NETHERLINK_AI_MAX_CONTEXT_TOKENS`: default `0`, meaning no configured backend cap yet.
- `NETHERLINK_AI_REQUEST_TIMEOUT`: default `1m`.
- `NETHERLINK_AI_OCR_LANGS`: optional Tesseract language list for scanned PDF OCR when `pdftoppm` and `tesseract` are installed; default `eng`.

DeepSeek v4 flash local example:

```sh
NETHERLINK_AI_PROVIDER=openai-compatible
NETHERLINK_AI_BASE_URL=https://api.deepseek.com
NETHERLINK_AI_DEFAULT_MODEL=deepseek-v4-flash
NETHERLINK_AI_TITLE_MODEL=deepseek-v4-flash
NETHERLINK_AI_THINKING_TYPE=enabled
NETHERLINK_AI_REASONING_EFFORT=high
NETHERLINK_AI_API_KEY=...
```

Keep the real API key in `.env`, shell environment variables, or a secret manager. `.env` is ignored by Git and must not be committed.

## API Foundation

All HTTP responses written by the shared helpers use JSON and preserve `X-Request-Id`.
The current API foundation includes:

- `GET /api/v1` as the versioned API root.
- Structured request logs with method, path, status, response size, duration, request ID, remote address, and user agent.
- Panic recovery mapped to `INTERNAL_ERROR`.
- JSON `NOT_FOUND` and `METHOD_NOT_ALLOWED` fallback responses.
- JSON body decoding helpers with unknown-field rejection and a 1 MiB request body limit.
- Bearer auth middleware validates access token signature, expiry, active PostgreSQL session, active user status, and `tokenVersion`.
- Detail resources use version-based HTTP `ETag` headers. `GET /me`, `GET /me/preferences`, `GET /users/{userUuid}`, `GET /users/by-id/{userId}`, `GET /groups/{groupId}`, `GET /files/{fileId}`, and `GET /posts/{postId}` return `304 Not Modified` when `If-None-Match` matches the current resource version. Mutable profile, preference, group, file delete, and post update/delete requests accept `If-Match`; JSON PATCH bodies also accept `expectedVersion`, while DELETE/avatar upload accept `expectedVersion` as a query parameter.
- WebSocket endpoint: `GET /api/v1/ws` accepts `Authorization: Bearer <access_token>`, `?accessToken=`, or `?token=`; optional `lastEventSeq` / `lastEventId=evt_N` enables durable replay when PostgreSQL is available.
- WebSocket command: send `{"type":"chat.message.send","payload":{...}}` with `conversationId` plus the same message fields as REST `POST /conversations/{conversationId}/messages`; success emits durable `chat.message.created` plus non-durable `chat.message.sent` ACK to the sender.
- Auth endpoints: `POST /api/v1/auth/register`, `POST /api/v1/auth/login`, `POST /api/v1/auth/refresh`, `POST /api/v1/auth/logout`, `GET /api/v1/auth/sessions`, `POST /api/v1/auth/sessions/{sessionId}/revoke`, `GET /api/v1/me`, `PATCH /api/v1/me`, `GET /api/v1/me/user-id/check`, `PATCH /api/v1/me/user-id`, `POST /api/v1/me/avatar`, `GET /api/v1/me/preferences`, and `PATCH /api/v1/me/preferences`.
- Directory endpoints: `GET /api/v1/users`, `GET /api/v1/users/{userUuid}`, `GET /api/v1/users/by-id/{userId}`, `POST /api/v1/users/batch`, `GET /api/v1/friends`, `PATCH /api/v1/friends/{friendUserUuid}`, `DELETE /api/v1/friends/{friendUserUuid}`, `GET /api/v1/friend-groups`, `POST /api/v1/friend-groups`, `POST /api/v1/friend-requests`, `GET /api/v1/friend-requests`, `POST /api/v1/friend-requests/{requestId}/accept`, `POST /api/v1/friend-requests/{requestId}/reject`, `GET /api/v1/groups`, `POST /api/v1/groups`, `GET /api/v1/groups/{groupId}`, `PATCH /api/v1/groups/{groupId}`, `DELETE /api/v1/groups/{groupId}`, `GET /api/v1/groups/{groupId}/members`, `POST /api/v1/groups/{groupId}/members`, `DELETE /api/v1/groups/{groupId}/members/{userUuid}`, `PATCH /api/v1/groups/{groupId}/members/{userUuid}`, `POST /api/v1/groups/{groupId}/transfer-owner`, `PATCH /api/v1/groups/{groupId}/my-settings`, `POST /api/v1/group-join-requests`, `GET /api/v1/group-notifications`, `POST /api/v1/group-join-requests/{requestId}/accept`, and `POST /api/v1/group-join-requests/{requestId}/reject`.
- File endpoints: `POST /api/v1/files`, `POST /api/v1/files/upload-session`, `GET /api/v1/files/upload-session/{uploadSessionId}`, `POST /api/v1/files/upload-session/{uploadSessionId}/parts`, `POST /api/v1/files/upload-session/{uploadSessionId}/complete`, `POST /api/v1/files/upload-session/{uploadSessionId}/abort`, `GET /api/v1/files/{fileId}`, `GET /api/v1/files/{fileId}/download-url`, and `DELETE /api/v1/files/{fileId}`.
- File upload completion returns `processingStatus` from the file storage status. Image, video, and audio uploads enter `processing` until the background worker records metadata and any supported variants; other file types move directly to `ready` for now.
- Chat endpoints: `GET /api/v1/conversations`, `POST /api/v1/conversations/direct`, `POST /api/v1/conversations/group`, `GET /api/v1/conversations/{conversationId}`, `PATCH /api/v1/conversations/{conversationId}/settings`, `POST /api/v1/conversations/{conversationId}/read`, `POST /api/v1/conversations/{conversationId}/unread`, `POST /api/v1/conversations/{conversationId}/messages`, `GET /api/v1/conversations/{conversationId}/messages`, `POST /api/v1/conversations/{conversationId}/messages/{messageId}/recall`, and `DELETE /api/v1/conversations/{conversationId}/messages`. Image messages must include attachment `width` and `height`; message responses include those fields on each attachment.
- Post endpoints: `GET /api/v1/posts`, `POST /api/v1/posts`, `GET /api/v1/posts/{postId}`, `PATCH /api/v1/posts/{postId}`, `DELETE /api/v1/posts/{postId}`, `POST /api/v1/posts/{postId}/like`, `POST /api/v1/users/{authorUuid}/follow`, `GET /api/v1/posts/{postId}/comments`, `POST /api/v1/posts/{postId}/comments`, `POST /api/v1/comments/{commentId}/like`, `POST /api/v1/comments/{commentId}/replies`, `POST /api/v1/replies/{replyId}/like`, `DELETE /api/v1/comments/{commentId}`, and `DELETE /api/v1/replies/{replyId}`.
- Notification and operation endpoints: `GET /api/v1/notifications`, `POST /api/v1/notifications/read-all`, `POST /api/v1/notifications/{notificationId}/read`, and `POST /api/v1/operations/receipt`.
- AI endpoints: `GET /api/v1/ai/conversations`, `POST /api/v1/ai/conversations`, `GET /api/v1/ai/conversations/{conversationId}/messages`, `POST /api/v1/ai/conversations/{conversationId}/messages`, `POST /api/v1/ai/conversations/{conversationId}/files`, `GET /api/v1/ai/conversations/{conversationId}/files`, `GET /api/v1/ai/files/{aiFileId}`, `DELETE /api/v1/ai/files/{aiFileId}`, `GET /api/v1/ai/conversations/{conversationId}/context`, `POST /api/v1/ai/messages/{messageId}/feedback`, and `POST /api/v1/ai/title`. REST message generation uses the configured provider synchronously by default; `Accept: text/event-stream` or `?stream=true` switches the same endpoint to SSE events (`ai.stream.started`, `ai.stream.chunk`, `ai.stream.done`, `ai.stream.cancelled`, and `client.error`). WebSocket `ai.message.send` streams chunks from the same OpenAI-compatible provider, and `ai.stream.cancel` cancels in-flight provider requests. File binding creates `ai_parse` jobs for text-like, PDF, docx, pptx, and xlsx files, the worker writes `ai_file_chunks`, and message sending selects relevant chunks for the model input.

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
The project has one environment-variable namespace. `.env` is a local values file for that namespace; `NETHERLINK_ENV` is the runtime-mode value within it, not a separate environment file or duplicate configuration source.

## Configuration And Secrets

`.env.example` is not a production environment file. It is a committed template that documents required variable names. Real passwords, token signing secrets, object-storage credentials, and production endpoints belong in `.env`, deployment environment variables, or a secret manager.

The API server intentionally fails during startup when required secret variables are missing or malformed. Non-secret local defaults are kept only for developer ergonomics, such as `localhost` hosts and default ports.

## Storage Boundary

The backend should treat MinIO as an internal S3-compatible provider. Public API responses should expose only stable file IDs, public URLs, or short-lived signed URLs. Bucket names, object keys, internal endpoints, and credentials should stay inside backend configuration and database metadata.

`protected` object storage is not a blanket authenticated-user permission. `GET /api/v1/files/{fileId}` and `GET /api/v1/files/{fileId}/download-url` authorize against PostgreSQL ownership and business references before signing object URLs.

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
- `files`: create file metadata, upload sessions, multipart part tracking, upload completion, status transitions, object-storage metadata bookkeeping, file processing job claims/results, file variant records, file usage records, and business-aware access checks for signing downloads, including AI uploaded files and extracted-text files.
- `chat`: list/open/read/update conversations, persist participant sync state, persist messages, assign per-conversation `message_seq`, persist message attachments and `chat_attachment` file usages, recall sender messages, hide current-user history with `hidden_at`, return attachment file metadata, and preserve image `width` and `height`.
- `friends`: create and list current-user friend groups, list active friends with keyword/group filters, read current-user friendship relation metadata, update/delete friendship settings, create/list/accept/reject friend requests, and upsert bidirectional friendships when accepting requests.
- `groups`: list current-user groups with keyword/category filters, read group details only for members, page group members only when the viewer belongs to the group, create/update/soft-delete groups, maintain group-avatar `file_usages`, add/remove/update members, transfer ownership, upsert current-user group settings, create/list/accept/reject join requests, and record group events.
- `posts`: list feed entries with viewer state, read post details, create/update/soft-delete posts, replace post media, record `file_usages` for post media and cover files with visibility matching the post, set post likes, set follows, list/create/delete comments and replies, and maintain persisted like/comment/reply counts.
- `ai`: list/create AI conversations, store user/assistant messages, bind uploaded files to AI conversations, track parse status and retention metadata, list/delete AI file bindings, expose context token counters, and store AI message feedback.
- `realtime_events`: persist globally sequenced WebSocket business events, record per-user recipients, list durable replay events after a client cursor, and report the latest sequence.
- `operation_receipts`: record and replay idempotent operation results by `user_id + client_operation_id + operation_type`.

These repositories are persistence primitives only. They do not hash passwords, mint tokens, enforce auth policy, or write HTTP responses.

Repository infrastructure:

- `repository.WithTx` and `repository.WithTxOptions` provide a single transaction boundary for service-layer operations that need to update multiple tables.
- `repository.Repositories` is the dependency aggregate passed into services, so new repositories should be added there before handlers depend on them directly.

## Runtime Boundary

`internal/app` owns runtime dependency assembly and lifecycle for PostgreSQL, Redis, repository aggregates, auth service, AI service/provider client, friend service, group service, post service, file service, file processing worker, file cleanup worker, realtime hub, Redis fanout, realtime cleanup worker, and object storage provider injection. `cmd/nlserver` should stay thin: load configuration, create the runtime, pass dependencies into `server.NewWithDependencies`, and start HTTP serving.

The MinIO/S3 provider implements object PUT/GET/HEAD/DELETE, presigned GET/PUT URLs, multipart upload creation, part presigning, completion, and abort. File metadata remains in PostgreSQL; public API responses do not expose bucket names or object keys.

## File Processing Boundary

`internal/files` now includes the upload-session service, a background processing worker, and a background cleanup worker. Completing an uploaded image, video, or audio file creates a `file_processing_jobs` row and moves the file to `processing`. The processing worker claims pending jobs with `FOR UPDATE SKIP LOCKED`, resets jobs stuck in `processing` for more than 15 minutes, reads the original object through the storage provider, records the job result JSON, and marks the file `ready`. Processing failures are recorded on the job and mark the file `failed`.

The processing worker supports image metadata and `thumbnail_512` JPEG variants, video metadata plus `video_cover_512` JPEG variants through `ffprobe` / `ffmpeg`, and audio duration probing through `ffprobe`. The cleanup worker claims expired upload sessions and unused files with `FOR UPDATE SKIP LOCKED`, aborts stale multipart uploads, deletes original and variant objects, and marks cleaned file metadata as `deleted`.

AI file binding creates `file_usages(usage_type=ai_upload)` and a pending `file_processing_jobs(job_type=ai_parse)` row. The processing worker now claims `ai_parse` jobs for UTF-8 text-like files such as plain text, Markdown, JSON, CSV, logs, XML, HTML, code, and common config files, plus PDF, docx, pptx, and xlsx text extraction. PDF extraction uses optional `pdftotext` before the built-in parser and can OCR the first scanned pages with optional `pdftoppm` + `tesseract`; set `NETHERLINK_AI_OCR_LANGS` to choose OCR languages. DOCX extraction includes body text plus headers, footers, footnotes, endnotes, and comments; PPTX extraction includes slides and speaker notes. It reads the original object, extracts text, stores bounded chunks in `ai_file_chunks`, records the parser in job/chunk metadata, and marks `ai_uploaded_files.parse_status` as `ready` or `failed`.

When every active AI binding for the parsed source file uses `retentionPolicy=source_deleted_after_parse`, and repository checks confirm that no avatar, chat, post, group, or other non-AI reference still points at the file, the worker deletes the original MinIO object and soft-deletes the source `files` row after storing chunks. The AI binding and chunks remain available for later messages.

## AI Boundary

`internal/ai` owns AI conversation service logic and the OpenAI-compatible `/chat/completions` provider client. The service stores user messages before model calls, stores assistant messages after successful provider responses, rejects unparsed AI files with `AI_FILE_NOT_READY`, and maps failed parses to `AI_FILE_PARSE_FAILED`. Provider credentials are read only from `NETHERLINK_AI_API_KEY`; code and docs must not contain real API keys.

REST generation is synchronous by default. `POST /api/v1/ai/conversations/{conversationId}/messages` returns the stored user message, created assistant message ID, an empty `streamId`, and used files. With `Accept: text/event-stream` or `?stream=true`, the same endpoint streams `ai.stream.started`, `ai.stream.chunk`, `ai.stream.done`, `ai.stream.cancelled`, and `client.error` SSE events, stores the final assistant message on successful provider completion, and cancels the provider request when the HTTP client disconnects. WebSocket `ai.message.send` returns `ai.stream.started`, streams `ai.stream.chunk` control events, stores the final assistant message on successful provider completion, and returns `ai.stream.done`; `ai.stream.cancel` cancels an in-flight stream by `streamId` and returns `ai.stream.cancelled`. When `aiFileIds` are supplied, the service rejects pending/failed parses and injects a bounded set of relevant `ai_file_chunks` into the provider prompt. `source_deleted_after_parse` keeps those chunks while removing the original source object after successful parsing, provided the file is not shared with any non-AI business reference.

## Chat Boundary

Chat conversation support includes listing visible conversations, opening or creating direct conversations, opening group conversations for existing group members, reading conversation metadata, updating pin/DND/hidden settings, marking conversations read or unread, sender-side message recall, and current-user hidden-history clearing. Sending a message validates that the sender is a participant, assigns the next `messageSeq` in a transaction, stores attachments in `message_attachments`, updates the conversation last-message fields, increments unread counts for other participants, and brings hidden conversations back into the visible list.

Image message attachments must include positive `width` and `height`. The same fields are returned in send and history responses so clients can allocate preview placeholders before the signed image URL finishes loading. Recalled message responses keep message metadata and `recalledAt` but omit content and attachment details. WebSocket broadcasts reuse the REST `message` response shape for `chat.message.created` and `chat.message.recalled` payloads. The `chat.message.send` WebSocket command reuses the same chat service validation and idempotency path as REST.

## Realtime Boundary

`internal/realtime` is the WebSocket hub plus Redis fanout transport and realtime event cleanup worker. PostgreSQL `realtime_events` assigns globally increasing `evt_N` IDs and stores per-user recipients for durable replay. The in-process hub still tracks online connections by user/session/device and keeps a short per-user replay window as fallback. Connection-control events such as `realtime.ready`, `realtime.pong`, `chat.message.sent`, and `client.error` use `eventSeq: 0` and are not replayed.

The realtime cleanup worker deletes `realtime_events` older than `NETHERLINK_REALTIME_EVENT_RETENTION` in batches of `NETHERLINK_REALTIME_EVENT_CLEANUP_BATCH` every `NETHERLINK_REALTIME_EVENT_CLEANUP_INTERVAL`; recipient rows are removed by cascade. `/readyz` includes hub online counts, memory/durable replay counters, Redis fanout publish/receive/error counters, and cleanup last-run state.

Current pushed events:

- `realtime.ready` after successful WebSocket auth.
- `realtime.pong` when the client sends `{"type":"ping"}`.
- `chat.message.created` after REST message send succeeds.
- `chat.message.recalled` after REST sender recall succeeds.
- `chat.conversation.sync.updated` after read/unread/settings/hidden-history changes and after new messages update participant unread state.
- `user.profile.updated` and `user.preferences.updated` after profile, avatar, public ID, or preference changes.
- `friend.group.created`, `friend.updated`, `friend.deleted`, and `friend.request.*` after friend list and request changes.
- `group.created`, `group.updated`, `group.deleted`, `group.members.added`, `group.member.removed`, `group.member.updated`, `group.owner.transferred`, `group.my_settings.updated`, and `group.join_request.*` after group changes.
- `notification.read` and `notification.read_all` after notification read-state changes.
- `post.created`, `post.updated`, `post.deleted`, `post.like.updated`, `post.comment.created`, `post.comment.deleted`, `post.reply.created`, `post.reply.deleted`, `post.comment.like.updated`, `post.reply.like.updated`, and `post.author.follow.updated` after post/count/viewer-state changes.
- `ai.stream.started`, `ai.stream.chunk`, `ai.stream.done`, and `ai.stream.cancelled` as connection-control events for WebSocket AI generation.

Incoming WebSocket command:

- `chat.message.send` with `conversationId`, `clientMessageId`, `type`, `content`, `attachments`, `clientSentAt`, and `referencedMessageId`; it reuses the REST chat service validation and idempotency path.
- `ai.message.send` with `conversationId`, `clientMessageId`, `message`, and `aiFileIds`; it stores the user message, streams OpenAI-compatible provider chunks, and stores the final assistant message on success.
- `ai.stream.cancel` with `streamId`; it cancels a same-user in-flight AI stream.

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

Business-aware file access integration coverage is available when a PostgreSQL test DSN is provided:

```sh
NETHERLINK_TEST_POSTGRES_DSN='postgres://...' go test ./tests -run TestFileAccessBusinessReferences
```

Without `NETHERLINK_TEST_POSTGRES_DSN`, that integration test is skipped and the rest of the test suite still runs normally.

The former `internal/database/postgres_test.go` was a single DSN unit test, not a duplicate or repeated test file. It now lives at `tests/postgres_dsn_test.go` with the rest of the external test suite.

## Next Build Steps

1. Store complete extracted text as managed file metadata through `ai_uploaded_files.extracted_text_file_id`.
2. Add optional embeddings or a stronger retrieval strategy for AI chunk search.
