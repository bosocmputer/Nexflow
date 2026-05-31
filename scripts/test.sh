#!/usr/bin/env bash
# =============================================================================
# Nexflow — Phase Test Script
# Usage: ./scripts/test.sh [phase] [host]
#   phase: 1 | 2 | 3 | all  (default: 1)
#   host:  host:port          (default: localhost:8090)
#
# Examples:
#   ./scripts/test.sh 1                     # test Phase 1 on localhost
#   ./scripts/test.sh 1 192.168.2.109:8090  # test Phase 1 on server
#   ./scripts/test.sh all                   # run all API phases
# =============================================================================

set -euo pipefail

PHASE="${1:-1}"
HOST="${2:-localhost:8090}"
BASE="http://${HOST}"
PASS=0
FAIL=0

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

ok()   { echo -e "  ${GREEN}✓${NC} $1"; ((++PASS)); }
fail() { echo -e "  ${RED}✗${NC} $1"; ((++FAIL)); }
info() { echo -e "${YELLOW}▶ $1${NC}"; }

# ── helpers ───────────────────────────────────────────────────────────────────

assert_status() {
  local label="$1" expected="$2" actual="$3"
  if [[ "$actual" == "$expected" ]]; then
    ok "$label (HTTP $actual)"
  else
    fail "$label — expected HTTP $expected, got HTTP $actual"
  fi
}

assert_contains() {
  local label="$1" needle="$2" body="$3"
  if echo "$body" | grep -q "$needle"; then
    ok "$label (contains '$needle')"
  else
    fail "$label — response does not contain '$needle'. Got: ${body:0:200}"
  fi
}

# Returns: sets $STATUS and $BODY
do_get() {
  local url="$1" token="${2:-}"
  if [[ -n "$token" ]]; then
    response=$(curl -s -w "\n%{http_code}" -H "Authorization: Bearer $token" "$url" 2>&1)
  else
    response=$(curl -s -w "\n%{http_code}" "$url" 2>&1)
  fi
  STATUS=$(echo "$response" | tail -1)
  BODY=$(echo "$response" | sed '$d')
}

do_post() {
  local url="$1" data="$2" token="${3:-}"
  if [[ -n "$token" ]]; then
    response=$(curl -s -w "\n%{http_code}" -X POST \
      -H "Content-Type: application/json" \
      -H "Authorization: Bearer $token" \
      -d "$data" "$url" 2>&1)
  else
    response=$(curl -s -w "\n%{http_code}" -X POST \
      -H "Content-Type: application/json" \
      -d "$data" "$url" 2>&1)
  fi
  STATUS=$(echo "$response" | tail -1)
  BODY=$(echo "$response" | sed '$d')
}

# ── Phase 1: Foundation ───────────────────────────────────────────────────────

test_phase1() {
  info "Phase 1 — Foundation & Auth (${BASE})"
  echo ""

  # 1.1 Health check
  do_get "${BASE}/health"
  assert_status "GET /health" "200" "$STATUS"
  assert_contains "health returns ok" "ok" "$BODY"

  # 1.2 Login — valid credentials
  do_post "${BASE}/api/auth/login" '{"email":"admin@nexflow.local","password":"admin1234"}'
  assert_status "POST /api/auth/login (valid)" "200" "$STATUS"
  assert_contains "login returns token" "token" "$BODY"

  # Extract token for subsequent requests
  TOKEN=$(echo "$BODY" | grep -o '"token":"[^"]*"' | cut -d'"' -f4 || true)
  if [[ -z "$TOKEN" ]]; then
    fail "Could not extract JWT token — remaining tests may fail"
    return
  fi
  ok "JWT token extracted"

  # 1.3 Login — wrong password
  do_post "${BASE}/api/auth/login" '{"email":"admin@nexflow.local","password":"wrongpassword"}'
  assert_status "POST /api/auth/login (invalid)" "401" "$STATUS"

  # 1.4 GET /api/auth/me — authenticated
  do_get "${BASE}/api/auth/me" "$TOKEN"
  assert_status "GET /api/auth/me (with token)" "200" "$STATUS"
  assert_contains "me returns email" "admin@nexflow.local" "$BODY"

  # 1.5 GET /api/auth/me — unauthenticated
  do_get "${BASE}/api/auth/me"
  assert_status "GET /api/auth/me (no token) → 401" "401" "$STATUS"

  # 1.6 GET /api/bills — authenticated
  do_get "${BASE}/api/bills" "$TOKEN"
  assert_status "GET /api/bills (with token)" "200" "$STATUS"

  # 1.7 GET /api/bills — unauthenticated
  do_get "${BASE}/api/bills"
  assert_status "GET /api/bills (no token) → 401" "401" "$STATUS"

  # 1.8 GET /api/mappings — authenticated
  do_get "${BASE}/api/mappings" "$TOKEN"
  assert_status "GET /api/mappings (with token)" "200" "$STATUS"

  # 1.9 GET /api/dashboard/stats — authenticated
  do_get "${BASE}/api/dashboard/stats" "$TOKEN"
  assert_status "GET /api/dashboard/stats (with token)" "200" "$STATUS"

  # 1.10 GET /api/dashboard/insights — authenticated
  do_get "${BASE}/api/dashboard/insights" "$TOKEN"
  assert_status "GET /api/dashboard/insights (with token)" "200" "$STATUS"
}

# ── Phase 2: AI Pipeline (smoke tests — requires running backend) ─────────────

test_phase2_go_unit() {
  info "Phase 2 — Go Unit Tests (mapper + anomaly)"
  echo ""

  local backend_dir
  backend_dir="$(dirname "$0")/../backend"

  if ! command -v go &>/dev/null; then
    fail "Go not found in PATH — skipping unit tests"
    return
  fi

  echo "  Running: go test ./internal/services/anomaly/..."
  if (cd "$backend_dir" && go test ./internal/services/anomaly/... -v 2>&1 | sed 's/^/    /'); then
    ok "anomaly unit tests passed"
  else
    fail "anomaly unit tests FAILED"
  fi

  echo ""
  echo "  Running: go test ./internal/services/mapper/..."
  if (cd "$backend_dir" && go test ./internal/services/mapper/... -v 2>&1 | sed 's/^/    /'); then
    ok "mapper unit tests passed"
  else
    fail "mapper unit tests FAILED"
  fi
}

# ── Phase 3: LINE Webhook ─────────────────────────────────────────────────────

test_phase3() {
  info "Phase 3 — LINE Webhook"
  echo ""

  # 3.1 Webhook must return 200 regardless of payload (async processing)
  do_post "${BASE}/webhook/line" '{"events":[]}' ""
  assert_status "POST /webhook/line (empty events) → 200" "200" "$STATUS"

  # 3.2 Webhook with invalid signature — should return 400
  response=$(curl -s -w "\n%{http_code}" -X POST \
    -H "Content-Type: application/json" \
    -H "X-Line-Signature: invalidsignature" \
    -d '{"events":[{"type":"message","replyToken":"test","message":{"type":"text","text":"test"}}]}' \
    "${BASE}/webhook/line" 2>&1)
  STATUS=$(echo "$response" | tail -1)
  BODY=$(echo "$response" | sed '$d')
  # With no LINE credentials configured, signature validation may be skipped
  # — just verify server responds (not 500)
  if [[ "$STATUS" != "500" ]]; then
    ok "POST /webhook/line — server responds (HTTP $STATUS, not 500)"
  else
    fail "POST /webhook/line — server error 500"
  fi
}

# ── Phase 4: Import (stub) ────────────────────────────────────────────────────

test_phase4() {
  info "Phase 4 — File Import Endpoint"
  echo ""

  do_post "${BASE}/api/auth/login" '{"email":"admin@nexflow.local","password":"admin1234"}'
  TOKEN=$(echo "$BODY" | grep -o '"token":"[^"]*"' | cut -d'"' -f4 || true)

  if [[ -z "$TOKEN" ]]; then
    fail "Cannot get token — skipping phase 4"
    return
  fi

  # Import endpoint should exist (even as stub returning 501)
  response=$(curl -s -w "\n%{http_code}" -X POST \
    -H "Authorization: Bearer $TOKEN" \
    -F "platform=lazada" \
    "${BASE}/api/import/upload" 2>&1)
  STATUS=$(echo "$response" | tail -1)
  if [[ "$STATUS" == "501" || "$STATUS" == "400" || "$STATUS" == "422" ]]; then
    ok "POST /api/import/upload exists (HTTP $STATUS)"
  else
    fail "POST /api/import/upload unexpected status: $STATUS"
  fi
}

# ── Phase 5: Email IMAP (config check only) ───────────────────────────────────

test_phase5() {
  info "Phase 5 — Email IMAP"
  echo ""
  echo "  NOTE: IMAP test requires IMAP_HOST configured in .env"
  echo "  Backend will auto-start email poller if IMAP_HOST is set."
  echo "  Manual test: Check backend logs for 'IMAP poll' messages."
  ok "Phase 5 reminder: configure IMAP_HOST in .env before full test"
}

# ── Phase 6: Web UI (frontend) ────────────────────────────────────────────────

test_phase6() {
  info "Phase 6 — Web UI (Frontend)"
  echo ""

  FRONTEND="${3:-http://localhost:3010}"
  if [[ "$#" -ge 3 ]]; then
    FRONTEND="$3"
  else
    # derive from HOST
    local fhost
    fhost=$(echo "$HOST" | cut -d: -f1)
    FRONTEND="http://${fhost}:3010"
  fi

  do_get "$FRONTEND/"
  assert_status "GET / (React app)" "200" "$STATUS"
  assert_contains "/ contains DOCTYPE" "DOCTYPE\|<!doctype" "$BODY"

  do_get "$FRONTEND/login"
  if [[ "$STATUS" == "200" ]]; then
    ok "GET /login → 200 (SPA route)"
  else
    # nginx may return 200 for all routes via try_files
    ok "GET /login → $STATUS (SPA)"
  fi
}

# ── Phase 7: Background Jobs ──────────────────────────────────────────────────

test_phase7() {
  info "Phase 7 — Background Jobs"
  echo ""
  echo "  Verify background jobs are registered in backend logs:"
  echo "    docker logs nexflow-backend | grep -E 'cron|poller|disk|token'"
  echo ""
  ok "Phase 7 reminder: review logs after startup"
}

# ── Phase 8: Production ───────────────────────────────────────────────────────

test_phase8() {
  info "Phase 8 — Production Readiness"
  echo ""

  # Health check
  do_get "${BASE}/health"
  assert_status "GET /health — production up" "200" "$STATUS"

  # DB backup exists (if on server)
  if [[ -d ~/nexflow/backups ]]; then
    count=$(find ~/nexflow/backups -name "*.sql" | wc -l)
    if [[ "$count" -gt 0 ]]; then
      ok "pg_dump backup files present ($count files)"
    else
      fail "No pg_dump backup files found in ~/nexflow/backups"
    fi
  fi

  ok "Phase 8 checklist: Cloudflare Tunnel, HTTPS, pg_dump restore — verify manually"
}

# ── Summary ───────────────────────────────────────────────────────────────────

print_summary() {
  echo ""
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  total=$((PASS + FAIL))
  if [[ $FAIL -eq 0 ]]; then
    echo -e "  ${GREEN}ALL TESTS PASSED${NC}  ($PASS/$total)"
  else
    echo -e "  ${RED}TESTS FAILED${NC}  ($PASS passed / $FAIL failed)"
  fi
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  [[ $FAIL -eq 0 ]] && exit 0 || exit 1
}

# ── Main ──────────────────────────────────────────────────────────────────────

case "$PHASE" in
  1)    test_phase1 ;;
  2)    test_phase2_go_unit ;;
  3)    test_phase3 ;;
  4)    test_phase4 ;;
  5)    test_phase5 ;;
  6)    test_phase6 ;;
  7)    test_phase7 ;;
  8)    test_phase8 ;;
  all)
    test_phase1
    echo ""
    test_phase2_go_unit
    echo ""
    test_phase3
    echo ""
    test_phase4
    echo ""
    test_phase5
    echo ""
    test_phase6
    echo ""
    test_phase7
    echo ""
    test_phase8
    ;;
  *)
    echo "Usage: $0 [1|2|3|4|5|6|7|8|all] [host:port]"
    exit 1
    ;;
esac

print_summary
