# NLServer

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
- File upload governance is available: fixed `targetType` allowlists, MIME and extension checks, per-target file size limits, per-user storage quota checks, final object size/type validation after direct upload, multipart missing-part and part-conflict checks before completion, MinIO invalid-part and stale-upload error mapping, final SHA-256 verification when `contentHash` is supplied, and a pluggable malicious-content scanning hook before files become bindable.
- File upload completion now queues image, video, and audio processing jobs; the runtime starts a background worker that reads uploaded media from MinIO, extracts backend-side image dimensions, generates 512px JPEG image thumbnails, probes video/audio duration with `ffprobe`, extracts 512px video cover JPEGs with `ffmpeg`, retries stale processing jobs, records derived image/video variants in `file_variants`, and moves file status from `processing` to `ready` or `failed`.
- File cleanup worker is available for expired upload sessions, expired temporary AI file bindings, unbound files, and files whose `file_usages` are all deleted; it releases expired AI file usages/chunks, deletes original and variant objects from MinIO, and soft-deletes cleaned file metadata.
- File metadata and download URLs now enforce business-aware access checks: chat attachments require visible conversation participation, group avatars require group membership or upload ownership, and post media follows public/author visibility.
- Chat conversation list/open/detail/settings/read/unread endpoints are available, along with message send, history, sender recall, and current-device hidden-history clearing; conversation detail/open responses include business `etag`, `GET /conversations/{conversationId}` honors `If-None-Match`, and settings/history-hidden writes can reject stale `If-Match` or `expectedVersion`; image attachments require and return `width` and `height` for frontend preview placeholders.
- Profile, preference, group, conversation, file, and post detail resources now return HTTP `ETag` headers; GET detail endpoints honor `If-None-Match` with `304 Not Modified`, and mutable profile/group/conversation/file/post writes can reject stale `If-Match` or `expectedVersion` inputs with `409 VERSION_CONFLICT`.
- WebSocket realtime foundation is available at `GET /api/v1/ws`: access-token auth, per-user connection management, PostgreSQL-backed global event IDs, durable replay, `resume` / `event.replay`, Redis pub/sub fanout, replay metrics, fanout operational counters, `ping`/`realtime.pong`, chat events, `chat.conversation.updated`, `notification.read.updated`, friend/group change events, `group.notification.created`, `profile.updated`, `preference.updated`, post count/version update events, in-process presence state backed by a persisted user presence preference, and `chat.message.send`, `chat.message.recall`, `chat.read`, and `presence.update`. `GET /me/preferences` reads do not emit preference update events.
- Realtime event cleanup is available: `realtime_events` are retained for the configured window, old events are deleted in batches, and `/readyz` exposes hub, fanout, and cleanup snapshots.
- Post HTTP handlers are available for feed/detail/create/edit/delete, media binding, likes, follows, comments, replies, comment/reply likes, and comment/reply deletion.
- Notification HTTP handlers are available for listing notifications, marking all notifications read, and marking one notification read.
- Operation receipt HTTP handler is available for idempotent client-operation result lookup and creation. Creation, deletion, and key update HTTP writes for friend groups, friend settings/deletion, friend requests, groups, group members, group owner transfer, current-user group settings, group join requests, posts, post comments/replies, AI conversations, non-streaming AI message sends, AI streaming message sends, AI file binding/deletion, and AI message feedback now accept `Idempotency-Key` or `clientOperationId`/`clientMessageId`. The server claims a receipt before executing and replays the saved HTTP or stream completion result on retry; versioned group and post writes perform `If-Match` / `expectedVersion` checks inside the protected operation so successful retries replay instead of failing on the advanced version.
- AI repositories, service, shared Agent model gateway/provider layer, OpenAI-compatible provider adapter, and AI HTTP handlers are available for conversations, messages, uploaded-file binding, context metadata, feedback, title generation, and conversation deletion cleanup. REST message generation is synchronous by default and supports SSE streaming with `Accept: text/event-stream` or `?stream=true`; WebSocket `ai.message.send` streams provider chunks and supports `ai.stream.cancel`; streaming retries with the same `clientMessageId` return `OPERATION_IN_PROGRESS` while pending or replay `ai.stream.done` after completion. Uploaded text-like, PDF, docx, pptx, and xlsx AI files are parsed into `ai_file_chunks`, complete extracted text is stored as private managed file metadata through `ai_uploaded_files.extracted_text_file_id`, and BM25-style local retrieval injects relevant chunks plus neighboring context into provider prompts. PDF extraction uses optional `pdftotext` first and can OCR scanned pages with optional `pdftoppm` + `tesseract`; docx extraction includes supplemental document parts such as headers, footers, footnotes, endnotes, and comments. AI files using `retentionPolicy=source_deleted_after_parse` delete the original object and soft-delete source file metadata after successful parsing when no non-AI business reference still needs the file; `retentionPolicy=temporary` expires after 24 hours and cleanup releases AI usages/chunks before object cleanup proceeds through the unused-file path. Deleting an AI conversation soft-deletes its messages and file bindings, releases AI file usages, deletes chunks, and leaves object deletion to the unused-file cleanup path.
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

One-command local startup:

```sh
./scripts/dev-start.sh
```

The script creates `.env` from `.env.example` when needed, appends newly added template keys to an existing `.env` without overwriting filled values, fills missing local development secrets, starts PostgreSQL, Redis, and MinIO, initializes MinIO buckets, then runs the API server. Non-empty variables exported in the shell that launches the script take precedence over `.env` values for the API server, and the script prints a redacted AI/LangSmith configuration summary before `go run`.

Manual startup:

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

Storage configuration:

- `NETHERLINK_FILE_USER_QUOTA_BYTES`: default `10737418240` (10 GiB); set `0` to disable the per-user quota check.

AI configuration:

- `NETHERLINK_AI_PROVIDER`: default `disabled`; supported configured values are `openai` and `openai-compatible`.
- `NETHERLINK_AI_API_KEY`: required when the AI provider is enabled; if empty, `OPENAI_API_KEY` is used as a fallback for OpenAI-style local environments.
- `NETHERLINK_AI_BASE_URL`: optional provider base URL, mainly for OpenAI-compatible providers.
- `NETHERLINK_AI_DEFAULT_MODEL`: required when the AI provider is enabled.
- `NETHERLINK_AI_TITLE_MODEL`: optional separate title-generation model.
- `NETHERLINK_AI_THINKING_TYPE`: optional OpenAI-compatible reasoning extension; supported values are `enabled` and `disabled`.
- `NETHERLINK_AI_REASONING_EFFORT`: optional OpenAI-compatible reasoning extension; supported values are `low`, `medium`, `high`, and `max`.
- `NETHERLINK_AI_MAX_CONTEXT_TOKENS`: default `0`, meaning no configured backend cap yet.
- `NETHERLINK_AI_MAX_OUTPUT_TOKENS`: default `4096`; maximum final-answer output tokens sent to the model.
- `NETHERLINK_AI_REQUEST_TIMEOUT`: default `1m`.
- `NETHERLINK_TAVILY_API_KEY`: optional Tavily API key. When set, AI conversations can call backend live web search for current web/news/finance information.
- `NETHERLINK_BRAVE_SEARCH_API_KEY`: optional Brave Search API key. When set, web search merges Brave's independent, fresh web index and available rich results with Tavily results.
- `NETHERLINK_BROWSERLESS_API_KEY`: optional Browserless API key. When set, `web_read` can execute JavaScript and read fully rendered dynamic pages before falling back to direct HTTP.
- `NETHERLINK_BROWSERLESS_BASE_URL`: Browserless service URL, default `https://production-sfo.browserless.io`; may point to a self-hosted Browserless deployment.
- `NETHERLINK_AI_OCR_LANGS`: optional Tesseract language list for scanned PDF OCR when `pdftoppm` and `tesseract` are installed; default `eng`.
- `LANGSMITH_TRACING`: optional LangSmith tracing switch. Set to `true` to report model gateway calls as LangSmith `llm` runs.
- `LANGSMITH_ENDPOINT`: LangSmith API endpoint, default `https://api.smith.langchain.com`.
- `LANGSMITH_API_KEY`: required when `LANGSMITH_TRACING=true`.
- `LANGSMITH_PROJECT`: optional LangSmith project name; set to `NLServer` to group traces under that project.
- `LANGSMITH_WORKSPACE_ID`: optional workspace/tenant ID for API keys that can access multiple workspaces.
- `LANGSMITH_REQUEST_TIMEOUT`: timeout for each LangSmith API request, default `5s`. LangSmith reporting runs asynchronously and never blocks model generation.

DeepSeek v4 flash local example:

```sh
NETHERLINK_AI_PROVIDER=openai-compatible
NETHERLINK_AI_BASE_URL=https://api.deepseek.com
NETHERLINK_AI_DEFAULT_MODEL=deepseek-v4-flash
NETHERLINK_AI_TITLE_MODEL=deepseek-v4-flash
NETHERLINK_AI_THINKING_TYPE=enabled
NETHERLINK_AI_REASONING_EFFORT=high
NETHERLINK_AI_MAX_OUTPUT_TOKENS=4096
NETHERLINK_AI_API_KEY=...
```

Keep the real API key in `.env`, shell environment variables, or a secret manager. `.env` is ignored by Git and must not be committed.

LangSmith local tracing example:

```sh
LANGSMITH_TRACING=true
LANGSMITH_ENDPOINT=https://api.smith.langchain.com
LANGSMITH_API_KEY=...
LANGSMITH_PROJECT=NLServer
LANGSMITH_REQUEST_TIMEOUT=10s
```

## API Foundation

All HTTP responses written by the shared helpers use JSON and preserve `X-Request-Id`.
The current API foundation includes:

- `GET /api/v1` as the versioned API root.
- Structured request logs with method, path, status, response size, duration, request ID, remote address, and user agent.
- Panic recovery mapped to `INTERNAL_ERROR`.
- JSON `NOT_FOUND` and `METHOD_NOT_ALLOWED` fallback responses.
- JSON body decoding helpers with unknown-field rejection and a 1 MiB request body limit.
- Bearer auth middleware validates access token signature, expiry, active PostgreSQL session, active user status, and `tokenVersion`.
- Detail resources use version-based HTTP `ETag` headers. `GET /me`, `GET /me/preferences`, `GET /users/{userUuid}`, `GET /users/by-id/{userId}`, `GET /groups/{groupId}`, `GET /conversations/{conversationId}`, `GET /files/{fileId}`, and `GET /posts/{postId}` return `304 Not Modified` when `If-None-Match` matches the current resource version. Mutable profile, preference, group, conversation settings/history-hidden, file delete, and post update/delete requests accept `If-Match`; JSON PATCH bodies also accept `expectedVersion`, while DELETE/avatar upload accept `expectedVersion` as a query parameter.
- WebSocket endpoint: `GET /api/v1/ws` accepts `Authorization: Bearer <access_token>`, `?accessToken=`, or `?token=`; optional `lastEventSeq` / `lastEventId=evt_N` enables durable replay when PostgreSQL is available.
- WebSocket command: send `{"type":"chat.message.send","payload":{...}}` with `conversationId` plus the same message fields as REST `POST /conversations/{conversationId}/messages`; success emits durable `chat.message.created` plus non-durable `chat.message.sent` ACK to the sender.
- Auth endpoints: `POST /api/v1/auth/register`, `POST /api/v1/auth/login`, `POST /api/v1/auth/refresh`, `POST /api/v1/auth/logout`, `GET /api/v1/auth/sessions`, `POST /api/v1/auth/sessions/{sessionId}/revoke`, `GET /api/v1/me`, `PATCH /api/v1/me`, `GET /api/v1/me/user-id/check`, `PATCH /api/v1/me/user-id`, `POST /api/v1/me/avatar`, `GET /api/v1/me/preferences`, and `PATCH /api/v1/me/preferences`.
- Directory endpoints: `GET /api/v1/users`, `GET /api/v1/users/{userUuid}`, `GET /api/v1/users/by-id/{userId}`, `POST /api/v1/users/batch`, `POST /api/v1/presence/batch`, `GET /api/v1/friends`, `PATCH /api/v1/friends/{friendUserUuid}`, `DELETE /api/v1/friends/{friendUserUuid}`, `GET /api/v1/friend-groups`, `POST /api/v1/friend-groups`, `PATCH /api/v1/friend-groups/{friendGroupId}`, `DELETE /api/v1/friend-groups/{friendGroupId}`, `POST /api/v1/friend-requests`, `GET /api/v1/friend-requests`, `POST /api/v1/friend-requests/{requestId}/accept`, `POST /api/v1/friend-requests/{requestId}/reject`, `GET /api/v1/groups`, `POST /api/v1/groups`, `GET /api/v1/groups/by-id`, `GET /api/v1/groups/{groupId}`, `PATCH /api/v1/groups/{groupId}`, `DELETE /api/v1/groups/{groupId}`, `GET /api/v1/groups/{groupId}/members`, `POST /api/v1/groups/{groupId}/members`, `DELETE /api/v1/groups/{groupId}/members/{userUuid}`, `PATCH /api/v1/groups/{groupId}/members/{userUuid}`, `POST /api/v1/groups/{groupId}/transfer-owner`, `PATCH /api/v1/groups/{groupId}/my-settings`, `POST /api/v1/group-join-requests`, `GET /api/v1/group-notifications`, `POST /api/v1/group-join-requests/{requestId}/accept`, and `POST /api/v1/group-join-requests/{requestId}/reject`.
- Groups have both `groupId` and `groupPublicId`. `groupId` is the internal UUID for existing group management routes; `groupPublicId` is a unique 10-digit numeric group number for display, search, and join-request entry. Group list/detail/conversation-summary responses include `groupPublicId`; UUID values are not matched by group search.
- `GET /api/v1/groups` supports `keyword`, `categoryId`, and `role` filters. `keyword` matches the readable group number `groupPublicId`, group name, or the current user's group remark, not the UUID `groupId`. `role` may be `joined`, `created`/`owner`, `managed`, `admin`, or `member`; group list responses include per-user `role`, `remark`, `listGroupId`, and `listGroupName` so clients can build system sections and custom/default list groups.
- `GET /api/v1/groups/by-id?groupPublicId=1000000001` looks up a group by exact numeric group number and returns `{ "group": ... }`. `POST /api/v1/group-join-requests` accepts either legacy `groupId` or the preferred `groupPublicId`; send only one of them.
- Group `version` and `avatarVersion` are separate contracts. `version` is the group metadata version and may change when name, introduction, announcement, owner, or other group metadata changes. `avatarVersion` changes only when the persisted group avatar file reference changes; name/introduction/announcement/member/settings changes keep `avatarUrl`, `avatarVersion`, and `avatarContentHash` stable. Omitted `avatarFileId` leaves the avatar untouched; `avatarFileId: null` or an empty string explicitly resets the group to a persisted default avatar. `PATCH /api/v1/groups/{groupId}` returns `changedFields` and `avatarChanged`:
  ```json
  {
    "group": {
      "groupId": "0f4f4a43-987c-40f0-b818-dc07505c44c5",
      "name": "New group name",
      "avatarUrl": "/api/v1/avatar-files/f1420000-0000-0000-0000-000000000000?variant=thumb",
      "avatarVersion": 5,
      "avatarContentHash": null,
      "version": 9,
      "etag": "\"group:0f4f4a43-987c-40f0-b818-dc07505c44c5:9\""
    },
    "changedFields": ["name"],
    "avatarChanged": false
  }
  ```
- File endpoints: `POST /api/v1/files`, `POST /api/v1/files/upload-session`, `GET /api/v1/files/upload-session/{uploadSessionId}`, `POST /api/v1/files/upload-session/{uploadSessionId}/parts`, `POST /api/v1/files/upload-session/{uploadSessionId}/complete`, `POST /api/v1/files/upload-session/{uploadSessionId}/abort`, `GET /api/v1/avatar-files/{fileId}`, `GET /api/v1/files/{fileId}`, `GET /api/v1/files/{fileId}/download-url`, and `DELETE /api/v1/files/{fileId}`.
- File upload completion returns `processingStatus` from the file storage status. Image, video, and audio uploads enter `processing` until the background worker records metadata and any supported variants; other file types move directly to `ready` for now.
- File upload creation and proxy upload enforce the backend `targetType` governance table for MIME, extension, per-target size, and per-user quota. Presigned upload completion revalidates final object size and object Content-Type from storage metadata before marking the file uploaded. Multipart completion now rejects missing required parts with `UPLOAD_PART_MISSING` plus `missingPartNumbers`, duplicate/conflicting or MinIO-invalid parts with `UPLOAD_PART_ETAG_MISMATCH`, stale multipart uploads with `UPLOAD_SESSION_INVALID`, and mismatched SHA-256 values with `CONTENT_HASH_MISMATCH` when `contentHash` is supplied. A `MaliciousContentScanner` can be injected into `internal/files.Service`; the default scanner allows the object, and scanner rejections return `FILE_SCAN_REJECTED`.
- Chat endpoints: `GET /api/v1/conversations`, `POST /api/v1/conversations/direct`, `POST /api/v1/conversations/group`, `GET /api/v1/conversations/{conversationId}`, `PATCH /api/v1/conversations/{conversationId}/settings`, `POST /api/v1/conversations/{conversationId}/read`, `POST /api/v1/conversations/{conversationId}/unread`, `POST /api/v1/conversations/{conversationId}/messages`, `GET /api/v1/conversations/{conversationId}/messages`, `POST /api/v1/conversations/{conversationId}/messages/{messageId}/recall`, and `DELETE /api/v1/conversations/{conversationId}/messages`. Image messages must include attachment `width` and `height`; message responses include those fields on each attachment.
- Post endpoints: `GET /api/v1/posts`, `POST /api/v1/posts`, `GET /api/v1/posts/{postId}`, `PATCH /api/v1/posts/{postId}`, `DELETE /api/v1/posts/{postId}`, `POST /api/v1/posts/{postId}/like`, `POST /api/v1/users/{authorUuid}/follow`, `GET /api/v1/posts/{postId}/comments`, `POST /api/v1/posts/{postId}/comments`, `POST /api/v1/comments/{commentId}/like`, `POST /api/v1/comments/{commentId}/replies`, `POST /api/v1/replies/{replyId}/like`, `DELETE /api/v1/comments/{commentId}`, and `DELETE /api/v1/replies/{replyId}`.
- Notification and operation endpoints: `GET /api/v1/notifications`, `POST /api/v1/notifications/read-all`, `POST /api/v1/notifications/{notificationId}/read`, and `POST /api/v1/operations/receipt`.
- AI endpoints: `GET /api/v1/ai/conversations`, `POST /api/v1/ai/conversations`, `PATCH /api/v1/ai/conversations/{conversationId}`, `DELETE /api/v1/ai/conversations/{conversationId}`, `GET /api/v1/ai/conversations/{conversationId}/messages`, `POST /api/v1/ai/conversations/{conversationId}/messages`, `POST /api/v1/ai/conversations/{conversationId}/files`, `GET /api/v1/ai/conversations/{conversationId}/files`, `GET /api/v1/ai/files/{aiFileId}`, `DELETE /api/v1/ai/files/{aiFileId}`, `GET /api/v1/ai/conversations/{conversationId}/context`, `POST /api/v1/ai/messages/{messageId}/feedback`, and `POST /api/v1/ai/title`. REST message generation uses the configured provider synchronously by default; `Accept: text/event-stream` or `?stream=true` switches the same endpoint to SSE events (`ai.stream.started`, `ai.stream.thinking`, `ai.stream.chunk`, `ai.stream.done`, `ai.stream.cancelled`, and `client.error`). WebSocket `ai.message.send` streams chunks from the same OpenAI-compatible provider, and `ai.stream.cancel` cancels in-flight provider requests. Message sends may override `model`, `thinkingType`, and `reasoningEffort`, and may include `timezone` for relative-date interpretation; provider API keys remain server-side only. SSE and WebSocket streaming sends use `operation_receipts` keyed by `clientMessageId`; duplicate completed requests replay `ai.stream.done` or `ai.stream.cancelled` without a second provider call. First-round message completion returns and stores a generated conversation title when the title is still automatic. File binding creates `ai_parse` jobs for text-like, PDF, docx, pptx, and xlsx files, the worker writes private extracted-text file metadata plus `ai_file_chunks`, and message sending uses BM25-style local retrieval for the model input.

PostgreSQL:

- Host: `localhost`
- Port: `5432`
- Database: `netherlink`
- User: `netherlink_app`
- Existing development databases created before the group avatar split and persisted presence preference need these one-time schema upgrades: `ALTER TABLE groups ADD COLUMN IF NOT EXISTS avatar_version INTEGER NOT NULL DEFAULT 1;` and `ALTER TABLE users ADD COLUMN IF NOT EXISTS presence_status TEXT NOT NULL DEFAULT 'online';`.

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

Avatar URLs are stable same-origin image URLs. `GET /api/v1/avatar-files/{fileId}?variant=thumb` validates that the file is a public/avatar file, proxies the original or `thumbnail_512` object from MinIO, and returns cacheable image bytes instead of redirecting clients to MinIO presigned URLs.

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
- `groups`: generate and enforce unique numeric group public IDs, list current-user groups with keyword/category filters, read group details only for members, read public group cards by group public ID, page group members only when the viewer belongs to the group, create/update/soft-delete groups, maintain group-avatar `file_usages`, add/remove/update members, transfer ownership, upsert current-user group settings, create/list/accept/reject join requests, and record group events.
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

The processing worker supports image metadata and `thumbnail_512` JPEG variants, video metadata plus `video_cover_512` JPEG variants through `ffprobe` / `ffmpeg`, and audio duration probing through `ffprobe`. Image variants are re-encoded as JPEG without EXIF metadata and are not upscaled. The cleanup worker claims expired upload sessions, expired temporary AI file bindings, and unused files with `FOR UPDATE SKIP LOCKED`; it aborts stale multipart uploads, releases expired AI usages/chunks, deletes original and variant objects, and marks cleaned file metadata as `deleted`.

AI file binding creates `file_usages(usage_type=ai_upload)` and a pending `file_processing_jobs(job_type=ai_parse)` row. The processing worker now claims `ai_parse` jobs for UTF-8 text-like files such as plain text, Markdown, JSON, CSV, logs, XML, HTML, code, and common config files, plus PDF, docx, pptx, and xlsx text extraction. PDF extraction uses optional `pdftotext` before the built-in parser and can OCR the first scanned pages with optional `pdftoppm` + `tesseract`; set `NETHERLINK_AI_OCR_LANGS` to choose OCR languages. DOCX extraction includes body text plus headers, footers, footnotes, endnotes, and comments; PPTX extraction includes slides and speaker notes. It reads the original object, extracts text, stores the complete text as a private managed `files` row referenced by `ai_uploaded_files.extracted_text_file_id`, stores bounded chunks in `ai_file_chunks`, records the parser in job/chunk metadata, and marks `ai_uploaded_files.parse_status` as `ready` or `failed`. Sending a message ranks chunks with a local BM25-style scorer using query terms, phrase matches, and document frequency, then includes neighboring chunks from the same AI file for context.

When every active AI binding for the parsed source file uses `retentionPolicy=source_deleted_after_parse`, and repository checks confirm that no avatar, chat, post, group, or other non-AI reference still points at the file, the worker deletes the original MinIO object and soft-deletes the source `files` row after storing the extracted text object and chunks. The AI binding, extracted text file, and chunks remain available for later messages.

When an AI binding uses `retentionPolicy=temporary`, the service sets a default 24 hour `expires_at`. The cleanup worker soft-deletes expired bindings, releases `ai_upload` and `ai_extracted_text` usages, deletes the binding chunks, and leaves object deletion to the existing unused-file cleanup path. Deleting an AI conversation follows the same reference-release model for all bindings in that conversation.

## AI Boundary

`internal/agent/model` owns the shared ModelGateway/ModelProvider layer, the OpenAI-compatible `/chat/completions` adapter, and optional LangSmith tracing around model calls. `internal/agent/conversation` owns AI conversation service logic and keeps the `ChatClient` wrapper over the shared gateway. The service stores user messages before model calls, stores assistant messages after successful provider responses, rejects unparsed AI files with `AI_FILE_NOT_READY`, and maps failed parses to `AI_FILE_PARSE_FAILED`. Provider credentials are read from `NETHERLINK_AI_API_KEY`, falling back to `OPENAI_API_KEY` when the NetherLink-specific variable is empty; code and docs must not contain real API keys.

AI conversations expose backend tools to the model:

- `local_time`: returns the current date/time, timezone, UTC offset, and Unix timestamps for a requested IANA timezone; AI message requests can provide the user's timezone for relative-date routing.
- `calculate`: evaluates deterministic arithmetic, scientific, aggregate, comparison, boolean, percentage, factorial, variable-based, and long-form expressions with bounded parsing and big-float precision.
- `calendar`: calculates date info, date/time add/subtract, date differences, ISO week/weekday metadata, period bounds, nth weekday, date ranges, and business-day add/diff with configurable weekends and holidays.
- `web_search`: uses Tavily Search and optionally Brave Search. When both keys are configured, results from independent indexes are merged and deduplicated; Brave rich results are fetched when available. Search is optimized as a fast discovery step that returns fresh URLs/snippets with explicit date bounds, target-date evidence checks, domain filters, and exact matching; `web_read` performs the heavier page/content extraction afterward.
- `web_read`: opens up to five public HTTP/HTTPS URLs and returns bounded page content. With Browserless configured it renders JavaScript and captures bounded public XHR/fetch responses used by dynamic pages. The direct HTTP fallback also discovers likely public JSON, JSONP, API, quote, and data resources referenced by static HTML and fetches a bounded subset with the page Referer. Private/local addresses and non-web URL schemes are rejected.

Tool calls are resolved server-side before the final assistant message is stored. The routing pass receives an authoritative request-start clock in the request timezone, so relative dates such as "yesterday" or "last night" are resolved before search instead of being left to search-provider or source-site timezones. Search snippets are used for source discovery; precise, disputed, page-specific, or strongly asserted claims require reading relevant source pages with `web_read`. The verification pass automatically opens the top search sources before revising a risky draft. Streaming requests keep the same SSE/WebSocket event shape; requests that use live tools are generated and fact-checked before the verified final answer is emitted as normal answer chunk content, while requests that do not use tools retain provider streaming. During live-tool work, the backend may also send `ai.stream.chunk` events with `transient=true`; these are user-visible progress narrations and are not persisted as the final assistant message.

REST generation is synchronous by default. `POST /api/v1/ai/conversations/{conversationId}/messages` returns the stored user message, created assistant message ID, an empty `streamId`, used files, and a generated `title` after the first round completes. With `Accept: text/event-stream` or `?stream=true`, the same endpoint streams `ai.stream.started`, `ai.stream.thinking`, `ai.stream.chunk`, `ai.stream.done`, `ai.stream.cancelled`, and `client.error` SSE events, stores the final assistant message on successful provider completion, returns the generated first-round title on `ai.stream.done`, and cancels the provider request when the HTTP client disconnects. WebSocket `ai.message.send` returns `ai.stream.started`, streams `ai.stream.thinking` and `ai.stream.chunk` control events, stores the final assistant message on successful provider completion, and returns `ai.stream.done`; `ai.stream.cancel` cancels an in-flight stream by `streamId` and returns `ai.stream.cancelled`. If a stream is cancelled after partial output, the service stores the partial assistant message and includes it in `ai.stream.cancelled` so clients can replace local temporary text with the persisted message. Streaming sends claim `operation_receipts` by `clientMessageId`; duplicate pending requests return `OPERATION_IN_PROGRESS`, while duplicate completed requests emit a new `ai.stream.started` and replay the saved `ai.stream.done` or `ai.stream.cancelled` without calling the provider again. When `aiFileIds` are supplied, the service rejects pending/failed parses and injects a bounded set of BM25-ranked `ai_file_chunks` into the provider prompt, including adjacent chunks for local context. `source_deleted_after_parse` keeps extracted text and chunks while removing the original source object after successful parsing, provided the file is not shared with any non-AI business reference; `temporary` bindings expire after 24 hours and are removed by the file cleanup worker.

Frontend AI send request fields:

```json
{
  "message": "Explain this file",
  "clientMessageId": "client-generated-id",
  "aiFileIds": [],
  "model": "deepseek-v4-flash",
  "thinkingType": "enabled",
  "reasoningEffort": "high",
  "timezone": "Asia/Shanghai"
}
```

`model`, `thinkingType`, `reasoningEffort`, and `timezone` are optional per-message overrides/context. Empty values fall back to the conversation/default backend configuration or server-local timezone. `timezone` should be an IANA name such as `Asia/Shanghai` from the client runtime. `thinkingType` accepts `enabled` or `disabled`; `reasoningEffort` accepts `low`, `medium`, `high`, or `max`. Clients never send provider API keys; the backend always uses `NETHERLINK_AI_API_KEY`.

HTTP JSON mode:

```http
POST /api/v1/ai/conversations/{conversationId}/messages
Authorization: Bearer <accessToken>
Content-Type: application/json
```

Response:

```json
{
  "userMessage": { "role": "user", "text": "Explain this file" },
  "assistantMessageId": "uuid",
  "streamId": "",
  "usedFiles": [],
  "title": "Optional generated title"
}
```

SSE mode:

```http
POST /api/v1/ai/conversations/{conversationId}/messages?stream=true
Authorization: Bearer <accessToken>
Accept: text/event-stream
Content-Type: application/json
```

Events:

```text
event: ai.stream.started
data: {"streamId":"ais_...","conversationId":"...","clientMessageId":"..."}

event: ai.stream.chunk
data: {"streamId":"ais_...","delta":"先核对","kind":"progress","transient":true,"stepId":"step-1","segmentId":"progress-1","segmentStart":true}

event: ai.stream.chunk
data: {"streamId":"ais_...","delta":"京能电","kind":"progress","transient":true,"stepId":"step-1","segmentId":"progress-1"}

event: ai.stream.chunk
data: {"streamId":"ais_...","delta":"力财报。\n","kind":"progress","transient":true,"stepId":"step-1","segmentId":"progress-1","segmentEnd":true}

event: ai.stream.thinking
data: {"streamId":"ais_...","active":true,"userVisible":false,"stepId":"step-1","sequence":1,"phase":"searching","status":"running","message":"正在搜索相关来源","tool":"web_search","query":"京能电力 2026 一季报","urls":[]}

event: ai.stream.thinking
data: {"streamId":"ais_...","active":true,"userVisible":false,"stepId":"step-1","sequence":1,"phase":"searching","status":"completed","message":"搜索返回 3 个候选来源，正在筛选","tool":"web_search","query":"京能电力 2026 一季报","urls":["https://example.com/report"]}

event: ai.stream.chunk
data: {"streamId":"ais_...","delta":"根据","kind":"answer","transient":false}

event: ai.stream.thinking
data: {"streamId":"ais_...","active":false,"userVisible":false}

event: ai.stream.done
data: {"streamId":"ais_...","userMessage":{},"assistantMessage":{},"usedFiles":[],"finishReason":"stop","title":"..."}
```

The backend never forwards private provider chain-of-thought. `ai.stream.thinking` always has `userVisible=false`: it is machine-readable state for spinners, tool panels, and diagnostics, and must not be appended to the assistant chat text. A concrete user-facing action narration is sent before the matching tool starts. `stepId` and `sequence` pair that narration with the tool state; the same `stepId` transitions from `running` to terminal `completed` or `failed`. For a search, `status=completed` is the successful end marker for that specific search, not for the whole Agent run. `active=false` hides the global activity indicator.

Every `ai.stream.chunk.delta` is user-renderable text. `kind=progress` and `transient=true` identify temporary TAO progress narration. Progress chunks sharing a `segmentId` form one sentence; `segmentStart` resets the temporary progress buffer and `segmentEnd` marks that sentence complete. `kind=answer` and `transient=false` identify persistent final-answer text. Final provider deltas are forwarded directly without an answer-sized validation buffer, and each backend answer event is bounded to a small text block. Clients should keep separate progress and answer buffers and animate queued deltas themselves. `ai.stream.done.assistantMessage.text` remains the authoritative persisted answer and must replace any local answer buffer.

After completion, AI conversation assistant messages may include a persisted collapsible trace:

```json
{
  "messageId": "uuid",
  "role": "assistant",
  "text": "final answer",
  "trace": {
    "traceId": "uuid",
    "status": "completed",
    "summary": "检索了：京能电力 2026 一季报；读取并核对了：京能电力 2026 一季报",
    "steps": [
      {
        "sequence": 1,
        "stepId": "step-1",
        "phase": "searching",
        "status": "completed",
        "text": "先核对京能电力财报的公开来源。",
        "tool": "web_search",
        "query": "京能电力 2026 一季报",
        "urls": ["https://example.com/report"],
        "createdAt": "2026-06-24T20:42:36+08:00"
      }
    ],
    "sourceRefs": ["https://example.com/report"],
    "createdAt": "2026-06-24T20:43:33+08:00",
    "updatedAt": "2026-06-24T20:43:33+08:00"
  }
}
```

The full trace is returned for UI history and folding. Receipt of `ai.stream.done` with `assistantMessage.trace` is the client trigger to attach the live work rows to the final assistant message and collapse them once; no separate collapse event is required. Later turns in the same AI conversation receive only bounded `trace.summary` continuity context. Full display steps and raw tool output are not injected into later prompts. This storage and context behavior is specific to `/api/v1/ai/conversations`; group and private chat AI are unchanged.

WebSocket mode:

```json
{
  "type": "ai.message.send",
  "payload": {
    "conversationId": "uuid",
    "clientMessageId": "client-generated-id",
    "message": "Explain this file",
    "aiFileIds": [],
    "model": "deepseek-v4-flash",
    "thinkingType": "enabled",
    "reasoningEffort": "high"
  }
}
```

The connection receives the same event names as SSE: `ai.stream.started`, `ai.stream.thinking`, `ai.stream.chunk`, `ai.stream.done`, `ai.stream.cancelled`, and `client.error`.

## Chat Boundary

Chat conversation support includes listing visible conversations, opening or creating direct conversations, opening group conversations for existing group members, reading conversation metadata, updating pin/DND/hidden settings, marking conversations read or unread, sender-side message recall, and current-device hidden-history clearing. Conversation detail/open responses include `version`, `etag`, and `updatedAt`; `GET /api/v1/conversations/{conversationId}` supports `If-None-Match`, and settings/history-hidden writes support stale `If-Match` or `expectedVersion` rejection. Sending a message validates that the sender is a participant, assigns the next `messageSeq` in a transaction, stores attachments in `message_attachments`, updates the conversation last-message fields, increments unread counts for other participants, and brings hidden conversations back into the visible list.

Image message attachments must include positive `width` and `height`. The same fields are returned in send and history responses so clients can allocate preview placeholders before the signed image URL finishes loading. Recalled message responses keep message metadata and `recalledAt` but omit content and attachment details. WebSocket broadcasts reuse the REST `message` response shape for `chat.message.created` and `chat.message.recalled` payloads. The `chat.message.send` WebSocket command reuses the same chat service validation and idempotency path as REST.

## Realtime Boundary

`internal/realtime` is the WebSocket hub plus Redis fanout transport and realtime event cleanup worker. PostgreSQL `realtime_events` assigns globally increasing `evt_N` IDs and stores per-user recipients for durable replay. The in-process hub still tracks online connections by user/session/device, keeps a short per-user replay window as fallback, and stores process-local presence state; the user's selected presence status is persisted on `users.presence_status` and restored on the next WebSocket connect before broadcasting presence. Connection-control events such as `realtime.ready`, `realtime.pong`, `event.replay`, `chat.message.sent`, and `client.error` use `eventSeq: 0` and are not replayed.

The realtime cleanup worker deletes `realtime_events` older than `NETHERLINK_REALTIME_EVENT_RETENTION` in batches of `NETHERLINK_REALTIME_EVENT_CLEANUP_BATCH` every `NETHERLINK_REALTIME_EVENT_CLEANUP_INTERVAL`; recipient rows are removed by cascade. `/readyz` includes hub online counts, memory/durable replay counters, Redis fanout publish/receive/error counters, and cleanup last-run state.

Current pushed events:

- `realtime.ready` after successful WebSocket auth.
- `realtime.pong` when the client sends `{"type":"ping"}`.
- `event.replay` after a successful `resume` command when the event backlog is available.
- `sync.required` when initial replay or `resume` cannot cover the requested backlog window.
- `chat.message.created` after REST or WebSocket message send succeeds.
- `chat.message.recalled` after REST or WebSocket sender recall succeeds.
- `chat.conversation.sync.updated` after read/unread/settings changes and after new messages update participant unread state.
- `chat.conversation.updated` after message creation, sender recall, or settings changes; payloads include participant-view `summary`, `syncState`, and last-message metadata.
- `chat.read.updated` after REST or WebSocket read-state updates.
- `profile.updated` after profile, avatar, or public ID changes; payloads include `userUuid`, `userId`, profile or avatar fields, and avatar version/hash fields.
- `preference.updated` after preference changes; payloads include `preferences` and `version`.
- `friend.group.created`, `friend.group.updated`, `friend.group.deleted`, `friend.updated`, `friend.deleted`, and `friend.request.*` after friend list and request changes; friend request creation payloads include both `request` and `notification`.
- `group.created`, `group.updated`, `group.deleted`, `group.members.added`, `group.member.removed`, `group.member.updated`, `group.owner.transferred`, `group.my_settings.updated`, `group.join_request.*`, and `group.notification.created` after group changes. `group.updated` uses the same shape as `PATCH /api/v1/groups/{groupId}` and includes `changedFields` plus `avatarChanged`; clients should reload avatar media only when `avatarChanged` is true or `group.avatarVersion` changes.
- `notification.read.updated` after single-notification or read-all changes; payloads include `type` when scoped and the remaining `unreadCount`.
- `post.created`, `post.updated`, `post.deleted`, `post.like.updated`, `post.comment.created`, `post.comment.deleted`, `post.reply.created`, `post.reply.deleted`, `post.comment.like.updated`, `post.reply.like.updated`, and `post.author.follow.updated` after post/count/viewer-state changes.
- `presence.updated` after WebSocket connect/disconnect and `presence.update`; recipients are the user and active friends. The user receives the real status, while friends see `invisible` as `offline`.
- `ai.stream.started`, `ai.stream.thinking`, `ai.stream.chunk`, `ai.stream.done`, and `ai.stream.cancelled` as connection-control events for WebSocket AI generation.

Incoming WebSocket command:

- `resume` with `lastEventSeq` or `lastEventId`; it prefers PostgreSQL durable replay and falls back to the in-process replay buffer.
- `chat.message.send` with `conversationId`, `clientMessageId`, `type`, `content`, `attachments`, `clientSentAt`, and `referencedMessageId`; it reuses the REST chat service validation and idempotency path.
- `chat.message.recall` with `conversationId` and `messageId`; it reuses REST sender-only recall validation and broadcasts `chat.message.recalled`.
- `chat.read` with `conversationId`, `lastReadMessageId`, and `lastReadAt`; it marks the conversation read and publishes both sync-state and read-state events.
- `presence.update` with `status` (`online`, `mining`, `airplane`, or `invisible`; legacy `busy`/`dnd` map to `mining`, legacy `away` maps to `airplane`). It persists the selected status on `users.presence_status`, updates process-local presence, and publishes `presence.updated`; `offline` is a server-only runtime state used for disconnects and external visibility. Visible statuses touch `users.last_seen_at`; `invisible` does not update public last-seen time. Disconnect broadcasts `offline` to friends without overwriting the persisted selection; reconnecting while invisible only syncs the user's own client and does not emit a friend-visible presence event. HTTP user responses include viewer-visible `user.presence`, so non-friend group members can actively query profile/member endpoints instead of receiving realtime presence fanout. In user JSON, `presence.status` is the online presence value; `accountStatus` is the account lifecycle value. The legacy top-level `status` is still emitted as the account lifecycle value for backward compatibility.
- `ai.message.send` with `conversationId`, `clientMessageId`, `message`, `aiFileIds`, optional `model`, optional `thinkingType`, optional `reasoningEffort`, and optional IANA `timezone`; it stores the user message, streams OpenAI-compatible provider thinking state and answer chunks, stores the final assistant message on success, and replays completed stream results for duplicate `clientMessageId` requests.
- `ai.stream.cancel` with `streamId`; it cancels a same-user in-flight AI stream.

PostgreSQL remains the source of truth. Redis only fans out already-persisted events between API instances; replay after reconnect reads from PostgreSQL when available.

## Auth Boundary

`internal/auth` owns password hashing, refresh token hashing, access-token signing, refresh token rotation, access-token validation, current-user profile validation, public ID policy, and user session management. PostgreSQL remains authoritative for users and refresh token sessions. Redis keys used by auth are namespaced as `nl:rate:auth:*`, have TTLs, and are only rate-limit counters.

Public IDs must match `[A-Za-z0-9_]{4,32}`. `PATCH /api/v1/me/user-id` requires `clientOperationId`, records successful changes in `user_id_changes`, and enforces a 30-day cooldown after each successful public ID change. Self-service `PATCH /api/v1/me` accepts `status` only as `active`; online presence states belong to the later realtime presence layer, not the account-active status used by token validation. User responses expose this account lifecycle value as `accountStatus` and keep top-level `status` as a compatibility alias.

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

The current implementation is roughly 99.3% of `backend-requirements.md`. The remaining work is contract closure, consistency, and lifecycle coverage. Do not add optional expansion work until the items below are closed.

1. Audit mutable-resource version contracts. Profile, preferences, files, groups, conversations, posts, and deletion-style writes should consistently expose `version`, `etag`, and `updatedAt` where required, and reject stale `If-Match` or `expectedVersion` inputs with `VERSION_CONFLICT`. Conversation detail/open responses, settings writes, hidden-history deletion, and versioned group/post idempotent writes are now covered; remaining work is a final audit for other update/delete edges.
2. Finish remaining WebSocket event-contract edges. Profile/preference event names, notification read updates, notification creation payloads, group notification naming, and conversation summary/update events are now aligned with `backend-requirements.md`; remaining work is recipient fanout auditing and final `sync.required` replay edge checks.
3. Normalize REST response and error contracts. Use the required pagination shape, make file/post/AI response fields match the documented models, and ensure partial paths return exact error codes such as `AI_FILE_NOT_READY`, `AI_FILE_PARSE_FAILED`, upload-session errors, and version conflicts.
4. Finish media-processing coverage beyond the current thumbnail/cover path. Add target-specific post image variants, video preview or HLS/MP4 transcode outputs where required, live-photo playable metadata/variants, and consistent media metadata in post/chat/file responses.
5. Finish AI retention-policy lifecycle semantics. `temporary` expiry cleanup and AI conversation deletion now soft-delete bindings, release AI usages, and remove chunks; remaining work is `none` handling for small text-only request-lifetime parsing, deeper extracted-text object cleanup, and broader signed URL revocation through metadata state.
6. Add account/session lifecycle cleanup required by file storage. Soft-delete related metadata, revoke future signed URL access by state, and queue asynchronous object cleanup according to active `file_usages` and retention policy.
