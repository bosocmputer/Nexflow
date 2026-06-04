# Nexflow — Deploy Info

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
- `/import/shopee` shows Open API status and active shop.
- `/sale-invoices` lists sent SI documents.
- Open one sent bill detail and verify the route badge says `ขาย -> ขายสินค้าและบริการ`.
- `/logs`, `/settings/instance`, `/settings/channels`, `/settings/email`, `/settings/catalog`.
- Do not confirm imports, send SML, delete/purge, reset data, or save settings during visual QA unless explicitly approved.

---

## sml-api-bybos (Shared SML Gateway)

```text
Location:  ~/sml-api-bybos/
Port:      8200
```

Nexflow backend calls `http://172.24.0.1:8200` (Docker gateway IP) with header `x-tenant: aoy` to route to the Aoy customer SML DB.

| Header | Value | DB |
| --- | --- | --- |
| `x-tenant` | `aoy` | `demserver.3bbddns.com` |

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
VITE_ENABLE_LAZADA_EXCEL=true
VITE_ENABLE_TIKTOK_EXCEL=true
VITE_ENABLE_CHAT=false          # LINE chat disabled
```

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

Last updated: 2026-05-31
