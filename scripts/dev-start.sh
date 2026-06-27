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
  awk -v key="$key" '
    $0 ~ "^(export[[:space:]]+)?" key "=" {
      line = $0
      sub(/^export[[:space:]]+/, "", line)
      sub("^" key "=", "", line)
      print line
      found = 1
    }
    END { if (!found) exit 0 }
  ' .env
}

env_has_key() {
  local key="$1"
  if [[ ! -f .env ]]; then
    return 1
  fi
  awk -v key="$key" '$0 ~ "^(export[[:space:]]+)?" key "=" {found=1} END {exit found ? 0 : 1}' .env
}

env_keys_from_file() {
  local file="$1"
  awk '
    /^(export[[:space:]]+)?[A-Za-z_][A-Za-z0-9_]*=/ {
      line = $0
      sub(/^export[[:space:]]+/, "", line)
      split(line, parts, "=")
      print parts[1]
    }
  ' "$file"
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
    $0 ~ "^(export[[:space:]]+)?" key "=" {
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

sync_env_template_defaults() {
  if [[ ! -f .env.example ]]; then
    return
  fi

  local tmp
  tmp="$(mktemp)"
  local added=()
  local line key
  while IFS= read -r line; do
    if [[ "$line" =~ ^([A-Za-z_][A-Za-z0-9_]*)= ]]; then
      key="${BASH_REMATCH[1]}"
      if ! env_has_key "$key"; then
        printf '%s\n' "$line" >> "$tmp"
        added+=("$key")
      fi
    fi
  done < .env.example

  if (( ${#added[@]} > 0 )); then
    {
      printf '\n# Added by scripts/dev-start.sh from .env.example on %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
      cat "$tmp"
    } >> .env
    echo "updated .env with missing keys from .env.example: ${added[*]}"
  fi
  rm -f "$tmp"
}

source_env_preserving_exports() {
  local keys=()
  local values=()
  local key current
  while IFS= read -r key; do
    current="${!key:-}"
    if [[ -n "$current" ]]; then
      keys+=("$key")
      values+=("$current")
    fi
  done < <(env_keys_from_file .env)

  set -a
  # shellcheck disable=SC1091
  . ./.env
  set +a

  local i
  for i in "${!keys[@]}"; do
    printf -v "${keys[$i]}" '%s' "${values[$i]}"
    export "${keys[$i]}"
  done
}

print_effective_config_summary() {
  local ai_key_state="<empty>"
  local openai_key_state="<empty>"
  local langsmith_key_state="<empty>"
  if [[ -n "${NETHERLINK_AI_API_KEY:-}" ]]; then
    ai_key_state="<set>"
  fi
  if [[ -n "${OPENAI_API_KEY:-}" ]]; then
    openai_key_state="<set>"
  fi
  if [[ -n "${LANGSMITH_API_KEY:-}" ]]; then
    langsmith_key_state="<set>"
  fi

  echo "effective AI config: provider=${NETHERLINK_AI_PROVIDER:-disabled} model=${NETHERLINK_AI_DEFAULT_MODEL:-} ai_key=${ai_key_state} openai_key=${openai_key_state} langsmith_tracing=${LANGSMITH_TRACING:-false} langsmith_project=${LANGSMITH_PROJECT:-} langsmith_timeout=${LANGSMITH_REQUEST_TIMEOUT:-5s} langsmith_key=${langsmith_key_state}"
}

warn_langsmith_config() {
  if [[ "${LANGSMITH_TRACING:-false}" == "true" && -z "${LANGSMITH_API_KEY:-}" ]]; then
    echo "warning: LANGSMITH_TRACING=true but LANGSMITH_API_KEY is empty; LangSmith tracing will fail validation." >&2
  elif [[ "${LANGSMITH_TRACING:-false}" != "true" && -n "${LANGSMITH_API_KEY:-}" ]]; then
    echo "warning: LANGSMITH_API_KEY is set but LANGSMITH_TRACING is not true; LangSmith tracing is disabled." >&2
  fi
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

sync_env_template_defaults

ensure_env NETHERLINK_AUTH_TOKEN_SECRET "$(random_hex 32)"
ensure_env NETHERLINK_POSTGRES_PASSWORD "$(random_hex 24)"
ensure_env NETHERLINK_OBJECT_STORAGE_ACCESS_KEY "netherlink_dev_minio_admin"
ensure_env NETHERLINK_OBJECT_STORAGE_SECRET_KEY "$(random_hex 24)"

echo "starting local dependencies..."
docker compose up -d postgres redis minio

echo "initializing MinIO buckets..."
docker compose up --force-recreate minio-init

echo "starting API server..."
source_env_preserving_exports
print_effective_config_summary
warn_langsmith_config

exec go run ./cmd/nlserver
