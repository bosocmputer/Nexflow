#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKEND="$ROOT/backend"
BINARY="${TMPDIR:-/tmp}/nexflow-sales-only-release-guard"

fail() {
  printf "sales-only release guard failed: %s\n" "$1" >&2
  exit 1
}

if rg -n 'services/(ai|mistral|insight)|NewInsightCron|NewEmailHandler|NewChatInboxHandler' \
  "$BACKEND/cmd/server/main.go" >/dev/null; then
  fail "server startup references a disabled AI, insight, email, or chat service"
fi

if rg -n 'catalogH\.(EmbedAll|EmbedOne|ReloadIndex)' "$BACKEND/cmd/server/main.go" >/dev/null; then
  fail "server routes a disabled embedding handler"
fi

if rg -n '/api/dashboard/insights|generateInsight|/api/settings/imap-accounts' \
  "$ROOT/frontend/src/App.tsx" \
  "$ROOT/frontend/src/components" \
  "$ROOT/frontend/src/pages/Bills.tsx" >/dev/null; then
  fail "active frontend entry points reference a disabled runtime feature"
fi

if rg -n 'OPENROUTER_|MISTRAL_API_KEY|IMAP_|AUTO_CONFIRM_THRESHOLD|INSIGHT_CRON_HOUR' \
  "$ROOT/.env.example" >/dev/null; then
  fail ".env.example contains disabled runtime configuration"
fi

(cd "$BACKEND" && go build -trimpath -o "$BINARY" ./cmd/server)

if strings "$BINARY" | rg -i 'openrouter\.ai|api\.mistral\.ai' >/dev/null; then
  fail "server binary contains an AI provider endpoint"
fi

if go tool nm "$BINARY" 2>/dev/null | rg 'nexflow/internal/services/(ai|mistral|insight)' >/dev/null; then
  fail "server binary links a disabled AI or insight package"
fi

if find "$ROOT/frontend/dist/assets" -type f -name '*.js' -print0 2>/dev/null \
  | xargs -0 rg -i 'openrouter|api\.mistral\.ai|/api/dashboard/insights|/api/settings/imap-accounts' >/dev/null; then
  fail "frontend production bundle contains a disabled runtime integration"
fi

printf "sales-only release guard ok: ai=false purchase=false binary=%s\n" "$BINARY"
