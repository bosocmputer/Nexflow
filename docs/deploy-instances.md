# Nexflow — Deploy Info

> Current production moved to the NextStep server on 2026-07-08. Use
> [nextstep-server-deploy-flow.md](nextstep-server-deploy-flow.md) for production.
> The old `192.168.2.109` / ngrok deployment below is DEV/legacy only.

## Current Production Summary

| Instance | Public URL | Server folder | Frontend | Backend | Postgres |
| --- | --- | --- | --- | --- | --- |
| demo | `https://nexflow.nextstep-soft.com` | `/mnt/data/nextstep-node-2/nexflow` | edge `6323`, debug `127.0.0.1:16323` | `8110` | `5440` |
| aoy | `https://nexflow-aoy.nextstep-soft.com` | `/mnt/data/nextstep-node-2/nexflow-aoy` | edge `6323`, debug `127.0.0.1:16324` | `8111` | `5441` |
| lanboon | `https://nextflow-lanboon.nextstep-soft.com` | `/mnt/data/nextstep-node-2/nexflow-lanboon` | edge `6323`, debug `127.0.0.1:16325` | `8112` | `5442` |

Standard code deploy:

```bash
NX_PASS='<server-password>' python scripts/deploy_nextstep_instances.py --target all
```

The script SSHes to the server, updates
`/mnt/data/nextstep-node-2/nexflow-release` from GitHub, rebuilds production
instances from that release clone, updates `nexflow-edge`, and preserves each
instance `.env`, `docker-compose.yml`, database volume, `backups/`, and
`artifacts/`.

---

## Legacy DEV Deploy Info

```text
Instance:  nexflow  (billflow-henna on server)
Folder:    /home/bosscatdog/billflow-henna
Frontend:  nexflow-frontend  →  port 3030
Backend:   nexflow-backend   →  port 8110
Postgres:  nexflow-postgres  →  port 5440
Public:    https://animal-galvanize-tameness.ngrok-free.dev  (ngrok fixed domain)
```

---

## Quick Commands

```bash
# Health check
curl http://192.168.2.109:8110/health

# Running containers
docker ps --format '{{.Names}} {{.Ports}}' | grep nexflow

# Logs
docker logs nexflow-backend --tail=50
docker logs nexflow-frontend --tail=20

# Restart
cd ~/billflow-henna && docker compose up -d

# Rebuild + restart
cd ~/billflow-henna && docker compose build backend frontend && docker compose up -d
```

---

## Production-Safe Deploy Checklist

The server folder has no `.git` metadata. Treat deploys as a file replacement
operation and record the state before rebuilding.

```bash
# 1) Record current state
cd ~/billflow-henna
docker ps --format '{{.Names}} {{.Status}} {{.Ports}}' | grep -E 'nexflow|sml-api-bybos'
curl -s http://localhost:8110/health

# 2) Preserve local-only server files
cp -p .env ".env.bak.$(date +%Y%m%d-%H%M%S)"

# 3) Backup DB before production UX deploys
mkdir -p backups
docker exec nexflow-postgres pg_dump -U nexflow -d nexflow | gzip > "backups/pre-ux-redesign-$(date +%Y%m%d-%H%M%S).sql.gz"
```

Recommended upload from local workspace:

```bash
NX_PASS='<server-password>' python scripts/deploy.py
```

The deploy script now:

- runs the local Nexflow brand guard before upload
- backs up `.env` and PostgreSQL to `~/nexflow-backups` on the server
- syncs `frontend/` and `backend/` with `rsync --delete` so stale BillFlow/Henna files cannot remain
- rebuilds backend/frontend, checks health at port `8110`, and verifies ngrok/container branding

Manual deploy fallback:

```bash
rsync -az --delete \
  --exclude '.git' \
  --exclude '.env' \
  --exclude 'node_modules' \
  --exclude 'dist' \
  --exclude 'backups' \
  --exclude 'artifacts' \
  /Users/nontawatwongnuk/dev_bos/Nexflow/frontend/ \
  bosscatdog@192.168.2.109:/home/bosscatdog/billflow-henna/frontend/
rsync -az --delete \
  --exclude '.git' \
  --exclude '.env' \
  --exclude 'node_modules' \
  --exclude 'dist' \
  --exclude 'backups' \
  --exclude 'artifacts' \
  /Users/nontawatwongnuk/dev_bos/Nexflow/backend/ \
  bosscatdog@192.168.2.109:/home/bosscatdog/billflow-henna/backend/
cd ~/billflow-henna
docker compose build backend frontend
docker compose up -d
curl -s http://localhost:8110/health
curl -I http://localhost:3030/
docker logs nexflow-backend --tail=80
```

Brand/release guard:

```bash
python scripts/release_guard.py --local
NX_PASS='<server-password>' python scripts/release_guard.py --production
```

`scripts/wipe_and_deploy.py` is test-only and destructive. Do not use it for production release. It refuses to run unless `NX_WIPE_CONFIRM=WIPE_NEXFLOW_TEST_DATA` is set.

Post-deploy smoke for the UX redesign:

- Login.
- `/dashboard` first screen.
- `/setup` still visible and actionable.
- `/shopee-operations` loads daily Shopee orders; open one Timeline drawer and
  verify order summary/payment card renders without calling Shopee during page load.
- `/import/shopee` shows Open API status and active shop.
- `/sale-invoices` lists sent SI documents.
- Open one sent bill detail and verify the route badge says `ขาย -> ขายสินค้าและบริการ`.
- `/settings/line-notifications` loads senders/recipients, recent deliveries,
  and the Shopee rich Flex fallback sample.
- `/settings/line-myshop` loads empty/account state, can show/copy generated
  webhook URLs, and `/settings/channels` shows `LINE MyShop / sale` routing when
  MyShop is enabled.
- `/logs`, `/settings/instance`, `/settings/channels`, `/settings/email`, `/settings/catalog`.
- Do not confirm imports, send SML, delete/purge, reset data, or save settings during visual QA unless explicitly approved.

---

## sml-api-bybos (Shared SML Gateway)

```text
Location:  ~/sml-api-bybos/
Port:      8200
```

Nexflow backend calls `http://172.17.0.1:8200` (Docker host gateway) with header
`x-tenant` from the resolved tenant runtime. The deployment `.env` supplies the
default; a pre-existing `app_settings.sml.database` row overrides it at startup,
so migrations must update or remove any stale override explicitly.

| Header | Value | DB |
| --- | --- | --- |
| `x-tenant` | `aoy` | `sml-api-bybos` tenant `aoy` |
| `x-tenant` | `lbk63` | `chk562595.totddns.com:12831/lbk63` |
| `x-tenant` | `ploy_test` | internal PostgreSQL database `ploy_test` |

Current Aoy tenant config:

```text
SML_DB_HOST_AOY=nextstep.iszai.com
SML_DB_PORT_AOY=6843
sml.provider=NEXT
sml.config_file=SMLConfigNEXT.xml
sml.database=aoy
sml.rest_base_url=http://172.24.0.1:8200
sml.stock_request_url=http://nextstep.iszai.com:8093
```

`nextstep.iszai.com` resolves to the server public IP. The production host does
not support hairpin access back to port 8093, so the Demo and AOY backends
receive tenant-scoped Docker `extra_hosts` entries from
`deploy/nextstep-instances.json`:

```text
nextstep.iszai.com -> 10.121.20.83
```

Keep the application setting as the public service URL. The internal DNS
override preserves the expected HTTP Host and final request URL while routing
traffic directly to the SML Java service inside the customer network.

Readiness checks:

```bash
curl -s -H 'x-tenant: aoy' http://localhost:8200/health/ready
# {"database":"aoy","status":"ok"}

curl -s -H 'x-tenant: lbk63' http://localhost:8200/health/ready
# {"database":"lbk63","status":"ok"}

curl -s -H 'x-tenant: ploy_test' http://localhost:8200/health/ready
# {"database":"ploy_test","status":"ok"}

curl -s -o /dev/null -w '%{http_code}' http://nextstep.iszai.com:8093/
# 200
```

To add a new tenant or restart after `.env` change:

```bash
cd ~/sml-api-bybos
docker compose up -d --force-recreate   # must use --force-recreate, not restart
```

---

## ngrok Tunnel

ngrok runs as a persistent background process:

```bash
# Check status
ps aux | grep ngrok | grep -v grep

# Get current URL
curl -s http://localhost:4040/api/tunnels | python3 -c \
  "import sys,json; [print(t['public_url']) for t in json.load(sys.stdin)['tunnels']]"
```

Current URL: `https://animal-galvanize-tameness.ngrok-free.dev` (fixed, does not change on restart)

If URL changes: update `PUBLIC_BASE_URL` in `.env` and rebuild frontend.

---

## Feature Flags

```bash
VITE_PHASE=2
VITE_ENABLE_SALES_ORDERS=true
VITE_ENABLE_SHOPEE_EXCEL=true
VITE_ENABLE_SHOPEE_REALTIME_OPS=true
VITE_ENABLE_LAZADA_EXCEL=true
VITE_ENABLE_TIKTOK_EXCEL=true
VITE_ENABLE_LINE_MYSHOP=true
VITE_ENABLE_CHAT=false          # LINE chat disabled

ENABLE_SHOPEE_REALTIME_OPS=true
ENABLE_SHOPEE_CANCEL_AFTER_SML_ALERTS=true
ENABLE_SHOPEE_SML_CANCEL_DOCUMENTS=true
ENABLE_SHOPEE_RICH_LINE_FLEX=true
ENABLE_SHOPEE_SETTLEMENT_LINE_ALERTS=true
ENABLE_SHOPEE_ORDER_ESCROW_ENRICHMENT=true
ENABLE_LINE_MYSHOP=true
```

`ENABLE_SHOPEE_SML_CANCEL_DOCUMENTS=true` allows staff/admin to confirm creation
of the configured Shopee cancelled-after-SML document: TRANS_FLAG 45 sale
cancellation or TRANS_FLAG 48 sale return / credit note. Set it back to `false`
for immediate rollback; the backend still blocks the action when SML readiness
is not OK.

`ENABLE_SHOPEE_RICH_LINE_FLEX=true` sends structured LINE Flex messages from the
notification outbox for Shopee order alerts. `ENABLE_SHOPEE_SETTLEMENT_LINE_ALERTS=true`
sends one deduped LINE alert per Shopee settlement run when escrow/payout data is ready.
`ENABLE_SHOPEE_ORDER_ESCROW_ENRICHMENT=true` caches order-level
`get_escrow_detail` payment breakdowns for `/shopee-operations` and enriches new-order
LINE Flex messages when the snapshot is ready. Set it to `false` to stop Shopee
escrow calls; existing order and settlement notifications continue with fallback data.
These three rich LINE/escrow flags default to `true` in backend config; add
explicit `false` values only for rollback.

`ENABLE_LINE_MYSHOP=true` enables the backend webhook/settings routes for LINE
MyShop, and `VITE_ENABLE_LINE_MYSHOP=true` shows the admin UI. Both default to
enabled in code; set either explicitly to `false` for rollback.

LINE MyShop production setup after deploy:

- Open `/settings/line-myshop`, add one row per OA Plus / MyShop account, and
  use the field guidance to locate the OA Plus API key/shop identifiers. Edit
  mode can clear a saved webhook secret to return to API key based signature
  verification. Copy the generated webhook URL after saving.
- Register that webhook URL in OA Plus for the matching account. The public route
  is `/webhook/line-myshop/:connection_id` behind the fixed ngrok domain.
- Open `/settings/channels` and configure `LINE MyShop / sale` before trying to
  send a MyShop bill to SML. The migration does not seed a default route.
- Use the per-account `ซิงก์ย้อนหลัง 48 ชม.` button only as a bounded reconciliation or
  backfill action after confirming the API key. It calls LINE SHOPPING API,
  fetches order detail, and may create local bills plus LINE notifications.
- Existing `/settings/line-notifications` recipients will receive MyShop alerts
  when enabled by recipient filters. MyShop alerts are PII-redacted and use
  `source=line_myshop`, separate from Shopee Flex notifications.

---

## Other Projects on Server (do not touch)

| Project | Ports |
| --- | --- |
| billflow (main) | 8090, 3010, 5438 |
| billflow-thaisunsport | 8100, 3020, 5448 |
| openclaw-admin | 3000, 5432 |
| centrix | 3002, 5001, 5434, 6380 |
| ledgioai | 3004, 5436, 6381 |
| sml-api-bybos | 8200 |

---

Last updated: 2026-06-22
