# MinIO module

This module prepares the local object storage foundation required by `backend-requirements.md`.

## Buckets

| Bucket | Purpose | Local policy |
| --- | --- | --- |
| `netherlink-public` | Public avatars and public post thumbnails. | Anonymous download enabled for local development. |
| `netherlink-protected` | Chat files, private post media, group avatars. | Private; backend should issue short-lived URLs. |
| `netherlink-private` | AI uploads, parsed text, audit or internal processing output. | Private; backend service account only. |
| `netherlink-tmp` | Unconfirmed uploads and processing intermediates. | Private; should be cleaned by later background jobs. |

## Start

```sh
docker compose up -d minio minio-init
docker compose ps
```

Console: http://localhost:9001

Default local credentials come from `.env.example`.

To rerun bucket initialization after changing bucket names:

```sh
docker compose up --force-recreate minio-init
```

## Notes

- API code should persist only `files.id`, `bucket`, `object_key`, `etag`, `content_hash`, `storage_status`, and visibility metadata internally.
- Public APIs should return public URLs or short-lived signed URLs, not MinIO endpoint, bucket, object key, or credentials.
- Lifecycle cleanup for `netherlink-tmp` is intentionally left for the later background worker phase.
