#!/bin/sh
set -eu

ALIAS_NAME="${MINIO_ALIAS:-nlserver}"
ENDPOINT="${MINIO_INTERNAL_ENDPOINT:-http://minio:9000}"

mc alias set "$ALIAS_NAME" "$ENDPOINT" "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD"

mc mb --ignore-existing "$ALIAS_NAME/${MINIO_PUBLIC_BUCKET:-netherlink-public}"
mc mb --ignore-existing "$ALIAS_NAME/${MINIO_PROTECTED_BUCKET:-netherlink-protected}"
mc mb --ignore-existing "$ALIAS_NAME/${MINIO_PRIVATE_BUCKET:-netherlink-private}"
mc mb --ignore-existing "$ALIAS_NAME/${MINIO_TMP_BUCKET:-netherlink-tmp}"

# Local development only: public assets can be fetched directly.
# Protected/private/tmp buckets stay private and should be accessed through backend-issued URLs.
mc anonymous set download "$ALIAS_NAME/${MINIO_PUBLIC_BUCKET:-netherlink-public}"

mc ls "$ALIAS_NAME"
