# NLServer

NetherLink backend foundation, built from `backend-requirements.md`.

Current scope is intentionally small:

- Go module bootstrap under `cmd/` and `internal/`.
- Environment-based config loading under `internal/config`.
- Object storage interface skeleton under `internal/storage`.
- PostgreSQL schema bootstrap under `db/`.
- MinIO object storage bootstrap under `minio/`.
- No API handlers, auth, database repositories, MinIO SDK implementation, or file processing workers yet.

## Start Local Dependencies

```sh
cp .env.example .env
docker compose up -d postgres minio minio-init
docker compose ps
```

PostgreSQL:

- Host: `localhost`
- Port: `5432`
- Database: `netherlink`
- User: `netherlink_app`

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

## Next Build Steps

1. Add a thin HTTP server and health endpoint.
2. Add a MinIO-backed implementation of `ObjectStorageProvider`.
3. Add only the file upload-session API skeleton before implementing full auth, chat, posts, or AI workflows.
