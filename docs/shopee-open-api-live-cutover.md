# Shopee Open API Direct-Mode Rollback (Legacy)

> Updated: 2026-07-15
> The production default is now the central Shopee gateway. This document is
> retained only for direct-mode rollback. See `docs/shopee-gateway-runbook.md`.

## Current State

- Existing shared app: `Nexflow` / App ID `231867` / live Partner ID `2034838`.
- Shared app status: `Online`; keep it as demo or temporary fallback only.
- Production server: `10.121.20.83`, root `/mnt/data/nextstep-node-2`, edge port `6323`.
- Production customer URLs:
  - demo: `https://nexflow.nextstep-soft.com`
  - AOY: `https://nexflow-aoy.nextstep-soft.com`
  - Lanboon: `https://nextflow-lanboon.nextstep-soft.com`
- Production rule: one central Shopee Open Platform App routes shops to isolated
  Nexflow tenants through `nexflow-shopee-gateway`.
- Shopee Console stores one live redirect domain and one live push callback per app. Nexflow now routes those central callbacks through `nexflow-shopee-gateway`; tenant domains must not be entered into the central Shopee App.
- AOY currently works on the shared direct app as a fallback. Keep it in direct mode until the central gateway canary passes OAuth, token, latest sync, order preview, and real push checks.
- Nexflow has OAuth callback, token tables, preview-only import, readiness status, multi-shop connection management, user-facing error UX, Shopee API preview hardening, realtime operations, and push webhook handling deployed.
- Current shared-app IP whitelist includes `58.136.190.202` and the observed Shopee-reported source IP `210.1.1.52`.
- Shopee live OAuth has been observed returning `code` and `shop_id` without `state`. Nexflow now allows a guarded fallback only when exactly one unconsumed, unexpired OAuth state exists for the current live environment and redirect URL.

## Readiness Gate

`GET /api/settings/shopee-api/status` returns a checklist used by `/import/shopee`:

- Open API enabled on the server.
- Partner ID and Partner Key configured.
- Redirect URL is HTTPS and ends with `/api/shopee-api/callback`.
- Base URL matches environment:
  - `sandbox` -> `https://partner.test-stable.shopeemobile.com`
  - `live` -> `https://partner.shopeemobile.com`
- Live mode is approved before letting the admin connect a real shop.
- OAuth shop connection exists before allowing API fetch.
- Token state is usable, refreshable, or blocked.
- Last sync error is shown as a warning, not hidden in logs.

The UI derives two separate gates:

- `can_connect`: admin can start Shopee OAuth.
- `can_fetch`: admin can fetch order preview from Shopee API.

This keeps the system safe after cutover: admins can connect a real shop, but API fetch stays blocked until a shop authorization token exists.

## Error UX Contract

Shopee API failures return structured JSON:

```json
{
  "error": "อ่านง่ายสำหรับ admin",
  "error_code": "RATE_LIMIT",
  "retryable": true
}
```

Known mapped cases include token expiry, rate limit, duplicate/in-flight request, timeout, and Shopee business errors. The frontend displays the action the admin should take instead of a raw Go/Shopee error.

## Preconditions Before Customer Cutover

1. Customer Nexflow hostname is live over HTTPS and routes to the correct instance.
2. Shopee Console has a customer-specific app, such as `Nexflow AOY`.
3. Customer app status is `Online` or otherwise approved for the needed live APIs.
4. Shopee Console live redirect domain equals the customer Nexflow domain.
5. Shopee Console business product URL equals the customer Nexflow domain.
6. Shopee Console IP whitelist contains `58.136.190.202` plus any IP from `source_ip_undeclared` errors.
7. Shopee Console push callback is `https://<customer-domain>/webhook/shopee?token=<customer-push-key>`.
8. Keep Excel import and the previous `.env` backup as rollback paths.

## Verified

- Backend readiness blocks misconfigured live base URL.
- Backend allows `refresh_required` token state when refresh token is still valid.
- Shopee API error mapper returns friendly rate-limit messaging.
- OAuth callback accepts normal `state` flow and has a tested no-state fallback that rejects missing/ambiguous sessions.
- `/import/shopee` lists connected shops, supports label edit + soft-disable, and requires a selected shop when more than one active connection exists.
- Imported Shopee bills keep `shopee_shop_id`, `shopee_connection_id`, and `shopee_shop_label`; duplicate protection is scoped to `(shop_id, order_id)`.
- Shopee client signs shop API requests correctly.
- Shopee client handles business errors and malformed token response.
- Shopee client rejects order detail requests over Shopee's 50-order limit before making HTTP calls.
- Frontend build and browser smoke test confirm the readiness checklist renders live mode and keeps fetch blocked until shop authorization exists.
- Server live cutover health check passed after writing a timestamped `.env` backup.
- Browser OAuth retry after deploy succeeded even though Shopee omitted `state`; API preview smoke for `2026-05-20` to `2026-05-21` returned HTTP 200 with zero orders.
- Real-data direct API discovery for `Henna.milkford` (`2026-05-07` to `2026-05-21`) found `create_time=28` orders and `update_time=38` orders. Shopee rejects `pay_time` for `get_order_list`, so Nexflow now exposes only `create_time` and `update_time`.
- Preview defaults to the ready-to-bill status group (`SHIPPED`, `TO_CONFIRM_RECEIVE`, `COMPLETED`), fetches detail batches of 50, maps shipping/package/COD fields, and blocks confirm when Shopee indicates additional pages.

## Server Cutover

Run this on `10.121.20.83` after approval:

```bash
cd /mnt/data/nextstep-node-2/nexflow-release
python3 scripts/shopee-live-cutover.py \
  --target aoy \
  --partner-id LIVE_PARTNER_ID \
  --enable-realtime \
  --prompt-push-secret

cd /mnt/data/nextstep-node-2/nexflow-aoy
docker compose up -d --build backend frontend
```

The script reads the Live Partner Key via hidden prompt, writes a timestamped `.env` backup, and sets:

```dotenv
SHOPEE_OPEN_API_ENABLED=true
SHOPEE_OPEN_API_ENV=live
SHOPEE_OPEN_API_BASE_URL=https://partner.shopeemobile.com
SHOPEE_OPEN_API_PARTNER_ID=<live partner id>
SHOPEE_OPEN_API_PARTNER_KEY=<live partner key>
SHOPEE_OPEN_API_REDIRECT_URL=<PUBLIC_BASE_URL>/api/shopee-api/callback
ENABLE_SHOPEE_REALTIME_OPS=true
VITE_ENABLE_SHOPEE_REALTIME_OPS=true
SHOPEE_REALTIME_WEBHOOK_SECRET=<push partner key>
```

The helper resolves the target `.env` from `deploy/nextstep-instances.json`.
It never prints Partner Key, Push Partner Key, access token, refresh token, or
webhook secret.

## Validation

1. Backend health for the target instance:

```bash
curl -sS http://localhost:8111/health
```

Expected:

```json
{"database":"ok","env":"production","status":"ok"}
```

2. In Nexflow `/settings/shopee-connections`, Shopee Open API card should show:

- Environment: `live`
- Configured: complete
- Connected: not yet connected

3. Click `เชื่อมต่อ Shopee API`, login/authorize the real shop, then return to Nexflow.

4. Fetch a small date range first, preferably 1 day. Use `วันที่สร้าง order` or `วันที่อัปเดต order`; `pay_time` is intentionally not supported for order list search.

5. Create Nexflow bills only after preview looks correct and Shopee does not report more pages. Do not enable auto-send to SML for the first live run.

6. For realtime push, verify `/settings/shopee-connections` push readiness and
confirm a real Shopee event updates `/shopee-operations` and the notification
badge.

## Rollback

If live OAuth or API fetch fails:

```bash
cd /mnt/data/nextstep-node-2/nexflow-aoy
cp .env.bak.YYYYMMDD-HHMMSS .env
docker compose up -d --build backend frontend
```

Fast disable without restoring everything:

```dotenv
SHOPEE_OPEN_API_ENABLED=false
```

Then restart backend. Shopee Excel/email flows are independent and should keep working.

## Production Notes

- Do not paste Partner Key into chat, tickets, screenshots, or command history.
- OAuth tokens are stored in `shopee_api_connections`; treat the DB backup as sensitive.
- The first live import is preview-only by design. Bill creation still requires explicit confirmation.
- If public URL changes, old OAuth links and Shopee redirect validation will fail until Console and `.env` match again.
- SML routing is still shared in v1. Multi-shop support currently affects OAuth connection selection, source traceability, filters, and duplicate prevention; per-shop SML defaults can be added later if operations need it.
