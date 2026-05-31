#!/bin/bash
# Nexflow Cloudflare Quick Tunnel
# - สร้าง quick tunnel ไปยัง backend :8090
# - print URL ที่ได้ + update LINE webhook อัตโนมัติ

set -euo pipefail

BACKEND_PORT=8090
LOGFILE="/tmp/nexflow-tunnel.log"
URLFILE="/tmp/nexflow-tunnel-url.txt"

# Load .env
ENV_FILE="$(dirname "$0")/../.env"
if [ -f "$ENV_FILE" ]; then
  # shellcheck disable=SC2046
  export $(grep -v '^#' "$ENV_FILE" | grep -v '^$' | xargs)
fi

LINE_TOKEN="${LINE_CHANNEL_ACCESS_TOKEN:-}"
API_URL="${1:-http://localhost:${BACKEND_PORT}}"

echo "=== Nexflow Cloudflare Quick Tunnel ==="
echo "Backend: http://localhost:${BACKEND_PORT}"
echo ""

# Start tunnel in background, capture URL
cloudflared tunnel --url "http://localhost:${BACKEND_PORT}" \
  --no-autoupdate \
  --logfile "$LOGFILE" \
  2>&1 &

TUNNEL_PID=$!
echo "Tunnel PID: $TUNNEL_PID"
echo "$TUNNEL_PID" > /tmp/nexflow-tunnel.pid

# Wait for URL to appear in log (max 30s)
echo "รอ URL..."
TUNNEL_URL=""
for i in $(seq 1 30); do
  sleep 1
  TUNNEL_URL=$(grep -oE 'https://[a-z0-9-]+\.trycloudflare\.com' "$LOGFILE" 2>/dev/null | head -1 || true)
  if [ -n "$TUNNEL_URL" ]; then
    break
  fi
done

if [ -z "$TUNNEL_URL" ]; then
  echo "❌ ไม่สามารถรับ Tunnel URL ได้ใน 30 วินาที"
  echo "ดู log: $LOGFILE"
  exit 1
fi

echo "$TUNNEL_URL" > "$URLFILE"
echo ""
echo "✅ Tunnel URL: $TUNNEL_URL"
echo ""
echo "API URL:     ${TUNNEL_URL}/api"
echo "Webhook URL: ${TUNNEL_URL}/webhook/line"
echo ""

# Update LINE webhook if token is set
if [ -n "$LINE_TOKEN" ]; then
  WEBHOOK_URL="${TUNNEL_URL}/webhook/line"
  echo "อัปเดต LINE webhook → $WEBHOOK_URL"
  
  RESPONSE=$(curl -s -X PUT \
    "https://api.line.me/v2/bot/channel/webhook/endpoint" \
    -H "Authorization: Bearer ${LINE_TOKEN}" \
    -H "Content-Type: application/json" \
    -d "{\"endpoint\": \"${WEBHOOK_URL}\"}")
  
  echo "LINE response: $RESPONSE"
  
  # Verify
  VERIFY=$(curl -s \
    "https://api.line.me/v2/bot/channel/webhook/endpoint" \
    -H "Authorization: Bearer ${LINE_TOKEN}")
  echo "Webhook ปัจจุบัน: $VERIFY"
else
  echo "⚠️  LINE_CHANNEL_ACCESS_TOKEN ว่าง — ต้องตั้ง webhook URL ใน LINE Developer Console เอง:"
  echo "   ${TUNNEL_URL}/webhook/line"
fi

echo ""
echo "กด Ctrl+C เพื่อหยุด tunnel"
echo "หรือรัน: kill \$(cat /tmp/nexflow-tunnel.pid)"
echo ""

# Wait for tunnel process
wait $TUNNEL_PID
