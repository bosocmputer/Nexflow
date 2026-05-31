#!/usr/bin/env bash
set -euo pipefail

# Nexflow main production preflight.
#
# Defaults target the main LAN instance. Override with env vars when needed:
#   BF_HOST=192.168.2.109 BACKEND_PORT=8090 FRONTEND_PORT=3010 SML_API_PORT=8200 scripts/preflight-main.sh
# Optional deeper checks via SSH:
#   BF_SSH='bosscatdog@192.168.2.109' BF_SSH_PREFIX='sshpass -p *** ssh -o StrictHostKeyChecking=no' scripts/preflight-main.sh

BF_HOST="${BF_HOST:-192.168.2.109}"
BACKEND_PORT="${BACKEND_PORT:-8090}"
FRONTEND_PORT="${FRONTEND_PORT:-3010}"
SML_API_PORT="${SML_API_PORT:-8200}"
SML_TENANT="${SML_TENANT:-SML1_2026}"

BACKEND_URL="${BACKEND_URL:-http://${BF_HOST}:${BACKEND_PORT}}"
FRONTEND_URL="${FRONTEND_URL:-http://${BF_HOST}:${FRONTEND_PORT}}"
SML_API_URL="${SML_API_URL:-http://${BF_HOST}:${SML_API_PORT}}"

ok() { printf 'ok   %s\n' "$*"; }
fail() { printf 'fail %s\n' "$*" >&2; exit 1; }

http_check() {
  local label="$1"
  local url="$2"
  local expect="${3:-}"
  local body status attempt
  for attempt in 1 2 3 4 5; do
    if body="$(curl -fsS --max-time 8 -w '\n%{http_code}' "$url")"; then
      break
    fi
    [ "$attempt" = "5" ] && fail "$label: cannot reach $url"
    sleep 1
  done
  status="$(printf '%s' "$body" | tail -n 1)"
  body="$(printf '%s' "$body" | sed '$d')"
  [ "$status" = "200" ] || fail "$label: HTTP $status from $url"
  if [ -n "$expect" ]; then
    grep -Fq "$expect" <<<"$body" || fail "$label: response missing '$expect'"
  fi
  ok "$label"
}

http_check_tenant() {
  local label="$1"
  local url="$2"
  local expect="${3:-}"
  local body status attempt
  for attempt in 1 2 3 4 5; do
    if body="$(curl -fsS --max-time 8 -H "X-Tenant: ${SML_TENANT}" -w '\n%{http_code}' "$url")"; then
      break
    fi
    [ "$attempt" = "5" ] && fail "$label: cannot reach $url"
    sleep 1
  done
  status="$(printf '%s' "$body" | tail -n 1)"
  body="$(printf '%s' "$body" | sed '$d')"
  [ "$status" = "200" ] || fail "$label: HTTP $status from $url"
  if [ -n "$expect" ]; then
    grep -Fq "$expect" <<<"$body" || fail "$label: response missing '$expect'"
  fi
  ok "$label"
}

printf 'Nexflow main preflight (%s)\n' "$BF_HOST"

http_check "backend /health" "${BACKEND_URL}/health" '"status":"ok"'
http_check "frontend /login" "${FRONTEND_URL}/login" '<div id="root">'
http_check "sml-api /health" "${SML_API_URL}/health" '"status":"ok"'
http_check_tenant "sml-api /health/ready" "${SML_API_URL}/health/ready" '"status":"ok"'
http_check "sml-api /openapi.json" "${SML_API_URL}/openapi.json" '"openapi"'

if [ -n "${BF_SSH:-}" ]; then
  SSH_PREFIX="${BF_SSH_PREFIX:-ssh -o StrictHostKeyChecking=no}"
  remote() {
    # shellcheck disable=SC2086
    $SSH_PREFIX "$BF_SSH" "$1"
  }
  remote "docker ps --filter name=nexflow --format '{{.Names}} {{.Status}}' && docker ps --filter name=sml-api-bybos --format '{{.Names}} {{.Status}}'"
  remote "docker exec nexflow-postgres psql -U nexflow -d nexflow -c \"SELECT COUNT(*) AS total, COUNT(*) FILTER (WHERE embedding_status='done') AS embedded, COUNT(*) FILTER (WHERE embedding_status='pending') AS pending, COUNT(*) FILTER (WHERE embedding_status='error') AS error FROM sml_catalog;\""
  ok "remote docker/catalog checks"
fi

printf 'preflight complete\n'
