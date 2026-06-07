#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

random_hex() {
  local bytes="$1"
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex "$bytes"
  else
    echo "openssl is required to generate missing local secrets" >&2
    exit 1
  fi
}

env_value() {
  local key="$1"
  if [[ ! -f .env ]]; then
    return 0
  fi
  awk -F= -v key="$key" '$1 == key {print substr($0, length(key) + 2); found=1} END {if (!found) exit 0}' .env
}

ensure_env() {
  local key="$1"
  local value="$2"
  local current
  current="$(env_value "$key")"
  if [[ -n "$current" ]]; then
    return
  fi

  local tmp
  tmp="$(mktemp)"
  awk -v key="$key" -v value="$value" '
    BEGIN { updated = 0 }
    $0 ~ "^" key "=" {
      print key "=" value
      updated = 1
      next
    }
    { print }
    END {
      if (!updated) {
        print key "=" value
      }
    }
  ' .env > "$tmp"
  mv "$tmp" .env
}

require_cmd docker
require_cmd go

if [[ ! -f .env ]]; then
  if [[ ! -f .env.example ]]; then
    echo ".env is missing and .env.example was not found" >&2
    exit 1
  fi
  cp .env.example .env
  echo "created .env from .env.example"
fi

ensure_env NETHERLINK_AUTH_TOKEN_SECRET "$(random_hex 32)"
ensure_env NETHERLINK_POSTGRES_PASSWORD "$(random_hex 24)"
ensure_env NETHERLINK_OBJECT_STORAGE_ACCESS_KEY "netherlink_dev_minio_admin"
ensure_env NETHERLINK_OBJECT_STORAGE_SECRET_KEY "$(random_hex 24)"

echo "starting local dependencies..."
docker compose up -d postgres redis minio

echo "initializing MinIO buckets..."
docker compose up --force-recreate minio-init

echo "starting API server..."
set -a
# shellcheck disable=SC1091
. ./.env
set +a

exec go run ./cmd/nlserver
