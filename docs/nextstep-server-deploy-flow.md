# Nexflow NextStep Production Deploy Flow

Updated: 2026-07-09

This is the current production server topology. The old `192.168.2.109` /
ngrok deployment is DEV/legacy only and must not be used for production deploys.

## Instances

| Instance | Public URL | Server folder | Frontend | Backend | Postgres | SML tenant |
| --- | --- | --- | --- | --- | --- | --- |
| demo | `https://nexflow.nextstep-soft.com` | `/mnt/data/nextstep-node-2/nexflow` | `127.0.0.1:16323 -> 80` | `8110 -> 8090` | `5440 -> 5432` | `demo` |
| aoy | `https://nexflow-aoy.nextstep-soft.com` | `/mnt/data/nextstep-node-2/nexflow-aoy` | `127.0.0.1:16324 -> 80` | `8111 -> 8090` | `5441 -> 5432` | `aoy` |
| lanboon | `https://nextflow-lanboon.nextstep-soft.com` | `/mnt/data/nextstep-node-2/nexflow-lanboon` | `127.0.0.1:16325 -> 80` | `8112 -> 8090` | `5442 -> 5432` | `lbk63` |

Public entrypoint:

```text
container: nexflow-edge
folder:    /mnt/data/nextstep-node-2/nexflow-edge
port:      6323 -> 80
routing:   by Host header
```

Shared SML gateway:

```text
container: nexflow-sml-api-bybos
port:      8200
tenants:   demo,aoy,lbk63
```

Each Nexflow instance has its own `.env`, Docker Compose file, Postgres volume,
`app_settings`, LINE/Shopee settings, and user data. Code should normally be the
same commit on all production instances.

## Diagram

```mermaid
flowchart TB
  Dev["Local workspace<br/>/Users/nontawatwongnuk/dev_bos/Nexflow"]
  Commit["commit + push to GitHub"]
  Script["scripts/deploy_nextstep_instances.py<br/>SSH orchestration"]

  Dev --> Commit --> Script

  subgraph Server["PROD server 10.121.20.83<br/>/mnt/data/nextstep-node-2"]
    Release["nexflow-release<br/>git clone<br/>checkout origin/main or commit SHA"]
    Edge["nexflow-edge<br/>6323 -> nginx :80<br/>host-based routing"]

    subgraph Demo["demo instance"]
      DemoFE["nexflow-frontend<br/>127.0.0.1:16323 -> nginx :80"]
      DemoBE["nexflow-backend<br/>8110 -> Go :8090"]
      DemoDB["nexflow-postgres<br/>5440 -> PostgreSQL"]
      DemoFE -->|"same-origin /api"| DemoBE
      DemoBE --> DemoDB
    end

    subgraph Aoy["aoy instance"]
      AoyFE["nexflow-aoy-frontend<br/>127.0.0.1:16324 -> nginx :80"]
      AoyBE["nexflow-aoy-backend<br/>8111 -> Go :8090"]
      AoyDB["nexflow-aoy-postgres<br/>5441 -> PostgreSQL"]
      AoyFE -->|"same-origin /api"| AoyBE
      AoyBE --> AoyDB
    end

    subgraph Lanboon["lanboon instance"]
      LanboonFE["nexflow-lanboon-frontend<br/>127.0.0.1:16325 -> nginx :80"]
      LanboonBE["nexflow-lanboon-backend<br/>8112 -> Go :8090"]
      LanboonDB["nexflow-lanboon-postgres<br/>5442 -> PostgreSQL"]
      LanboonFE -->|"same-origin /api"| LanboonBE
      LanboonBE --> LanboonDB
    end

    SMLAPI["nexflow-sml-api-bybos<br/>8200<br/>ALLOWED_TENANTS=demo,aoy,lbk63"]
    SMLDemo["SML database: demo"]
    SMLAoy["SML database: aoy"]
    SMLLanboon["SML database: lbk63<br/>chk562595.totddns.com:12831"]

    DemoBE -->|"app_settings: sml.database=demo"| SMLAPI
    AoyBE -->|"app_settings: sml.database=aoy"| SMLAPI
    LanboonBE -->|"app_settings: sml.database=lbk63"| SMLAPI
    SMLAPI --> SMLDemo
    SMLAPI --> SMLAoy
    SMLAPI --> SMLLanboon
  end

  Cloudflare["Cloudflare proxied DNS / HTTPS"]
  Cloudflare -->|"all hostnames to :6323"| Edge
  Edge -->|"Host: nexflow.nextstep-soft.com"| DemoFE
  Edge -->|"Host: nexflow-aoy.nextstep-soft.com"| AoyFE
  Edge -->|"Host: nextflow-lanboon.nextstep-soft.com"| LanboonFE
  Edge -->|"demo /api, /health, /webhook"| DemoBE
  Edge -->|"aoy /api, /health, /webhook"| AoyBE
  Edge -->|"lanboon /api, /health, /webhook"| LanboonBE

  Script --> Release
  Release -->|"edge config"| Edge
  Release -->|"Docker build context"| DemoFE
  Release -->|"Docker build context"| DemoBE
  Release -->|"Docker build context"| AoyFE
  Release -->|"Docker build context"| AoyBE
  Release -->|"Docker build context"| LanboonFE
  Release -->|"Docker build context"| LanboonBE
```

## Standard Code Deploy

Use this when the code changes and all customer instances should run the same
version.

```bash
NX_PASS='<server-password>' python scripts/deploy_nextstep_instances.py --target all
```

The server-side deploy shape is:

```bash
cd /mnt/data/nextstep-node-2/nexflow-release
git fetch --prune origin
git checkout --detach origin/main

cd /mnt/data/nextstep-node-2/nexflow
docker compose up -d --build backend frontend

cd /mnt/data/nextstep-node-2/nexflow-aoy
docker compose up -d --build backend frontend

cd /mnt/data/nextstep-node-2/nexflow-lanboon
docker compose up -d --build backend frontend

cd /mnt/data/nextstep-node-2/nexflow-edge
docker compose up -d
```

Deploy only one instance when the change is intentionally isolated:

```bash
NX_PASS='<server-password>' python scripts/deploy_nextstep_instances.py --target demo
NX_PASS='<server-password>' python scripts/deploy_nextstep_instances.py --target aoy
NX_PASS='<server-password>' python scripts/deploy_nextstep_instances.py --target lanboon
NX_PASS='<server-password>' python scripts/deploy_nextstep_instances.py --ref d52de63
```

The script:

- prepares `/mnt/data/nextstep-node-2/nexflow-release` as the server Git clone
- fetches GitHub and checks out a ref, default `origin/main`
- updates each instance compose file once so Docker build contexts point to the
  release clone and frontends bind to local-only debug ports
- updates and restarts `nexflow-edge`
- preserves each instance `.env`, `docker-compose.yml`, database volume,
  `backups/`, and `artifacts/`
- backs up `.env` and Postgres before rebuilding
- rebuilds and restarts `backend` + `frontend`
- smoke-tests backend health and `/login`
- smoke-tests selected hostnames through edge port `6323`
- scans recent backend logs for severe errors

It intentionally does not turn each instance folder into a Git checkout. The
instance folders remain config/runtime folders; the release clone is the source
of code truth.

## New Customer Onboarding

Use this flow for customers like Lanboon where the customer owns an external
PostgreSQL/SML server and Nexflow reads it through `sml-api-bybos`.

1. Collect customer inputs:
   - customer key, such as `lanboon`
   - public hostname, such as `nextflow-lanboon.nextstep-soft.com`
   - SML PostgreSQL host, port, database/tenant, user, password, sslmode

2. Confirm the production server can reach the customer DB before adding the app:

   ```bash
   ssh ubuntu@10.121.20.83
   timeout 5 bash -lc '</dev/tcp/<customer-pg-host>/<customer-pg-port>' && echo reachable
   ```

3. Add the instance registry row locally. The helper picks the next backend,
   local Postgres, and frontend debug ports when omitted.

   ```bash
   python3 scripts/nextstep_instance_registry.py suggest
   python3 scripts/nextstep_instance_registry.py add \
     --name <shop-key> \
     --hostname nextflow-<shop-key>.nextstep-soft.com \
     --sml-tenant <tenant-or-db-name> \
     --sml-host <customer-pg-host> \
     --sml-port <customer-pg-port>
   ```

   This updates `deploy/nextstep-instances.json` and re-renders the edge
   snapshots. It does not store the customer PG password.

4. Commit and push the registry/tooling change.

5. Bootstrap the server runtime folder using the registry values:
   - create `/mnt/data/nextstep-node-2/nexflow-<shop-key>`
   - create unique containers `nexflow-<shop-key>-postgres/backend/frontend`
   - create a fresh local Nexflow database volume, not a copy of another shop
   - generate fresh `DB_PASSWORD`, `JWT_SECRET`, `MEDIA_SIGNING_KEY`
   - set `PUBLIC_BASE_URL=https://nextflow-<shop-key>.nextstep-soft.com`
   - set `SHOPEE_SML_URL=http://172.17.0.1:8200`
   - set `SHOPEE_SML_PROVIDER=NEXT`, `SHOPEE_SML_CONFIG_FILE=SMLConfigNEXT.xml`
   - set `SHOPEE_SML_DATABASE=<tenant-or-db-name>`
   - keep Shopee/LINE disabled until configured per customer

6. Add the customer tenant to `/mnt/data/nextstep-node-2/sml-api-bybos-nexflow/.env`
   and `nexflow-sml-api-bybos.runtime.env`:

   ```text
   ALLOWED_TENANTS=demo,aoy,lbk63,<new-tenant>
   SML_DB_HOST_<TENANT>=<customer-pg-host>
   SML_DB_PORT_<TENANT>=<customer-pg-port>
   SML_DB_USER_<TENANT>=<customer-pg-user>
   SML_DB_PASSWORD_<TENANT>=<customer-pg-password>
   SML_DB_SSLMODE_<TENANT>=disable
   ```

   Store the password only on the server. For read-only NextStep usage, ask the
   customer for a read-only PG user instead of `postgres`.

7. Test the tenant before exposing it:

   ```bash
   curl -s -H 'x-tenant: <tenant>' http://localhost:8200/health/ready
   curl -s -H 'x-tenant: <tenant>' -H 'x-api-key: smlx' \
     'http://localhost:8200/api/v1/marketplace/nextstep/orders?date_from=YYYY-MM-DD&date_to=YYYY-MM-DD&include_orders=false&page_size=5'
   ```

8. Deploy the registered target:

   ```bash
   NX_PASS='<server-password>' python3 scripts/deploy_nextstep_instances.py --target <shop-key>
   ```

9. Send IT the Cloudflare message printed by:

   ```bash
   python3 scripts/nextstep_instance_registry.py it-message --hostname nextflow-<shop-key>.nextstep-soft.com
   ```

10. Smoke after DNS/proxy:
    - `/login`
    - `/health`
    - `/dashboard`
    - `/nextstep-marketplace`
    - local Nexflow DB has no bills from other shops
    - `app_settings.sml.database` equals the customer tenant

## Per-Instance Config

Use the UI or `app_settings` for customer-specific settings. Do not solve
customer-specific config by changing shared code.

| Config type | Scope |
| --- | --- |
| LINE OA sender/recipients | per instance |
| Shopee connections/OAuth | per instance |
| SML tenant/database | per instance |
| Users/passwords | per instance |
| Feature flags in `.env` | per instance |
| Backend/frontend code | normally all instances |

Current AOY focus:

```text
PUBLIC_BASE_URL=https://nexflow-aoy.nextstep-soft.com
sml.database=aoy
sml.provider=NEXT
sml.config_file=SMLConfigNEXT.xml
sml.rest_base_url=http://172.17.0.1:8200
Shopee realtime/API is enabled as AOY's temporary shared-app fallback until a
dedicated Shopee Open Platform app passes OAuth, token, sync, and preview smoke.
```

Current Lanboon focus:

```text
PUBLIC_BASE_URL=https://nextflow-lanboon.nextstep-soft.com
sml.database=lbk63
sml.provider=NEXT
sml.config_file=SMLConfigNEXT.xml
sml.rest_base_url=http://172.17.0.1:8200
Shopee/LINE settings are customer-specific and start unconfigured
```

## Shopee Open Platform Per-Customer App

Production default:

```text
1 Nexflow instance = 1 Shopee Open Platform App = 1 customer/shop
```

The existing `Nexflow` app / Partner ID `2034838` is a demo or temporary
fallback only. Do not point multiple production customer domains at the same
Shopee app unless a central OAuth/webhook broker exists, because Shopee Console
stores a single live redirect domain and a single live push callback per app.

### Shopee Console Checklist

For each customer app, configure:

| Field | Value pattern |
| --- | --- |
| App name | `Nexflow <Customer>` such as `Nexflow AOY` |
| Redirect domain | customer domain, e.g. `https://nexflow-aoy.nextstep-soft.com` |
| Business product URL | same customer domain |
| IP whitelist | `58.136.190.202` plus any IP returned by Shopee `source_ip_undeclared` |
| Push callback | `https://<customer-domain>/webhook/shopee?token=<customer-push-key>` |
| Push topics | enable only required topics first, such as order status and authorization events |

If Shopee returns `source_ip_undeclared`, add the reported source IP to the app
IP whitelist before retrying. The observed extra source IP for the current
production route was `210.1.1.52`.

### Instance Cutover

Run the helper on the production server only after the customer Shopee app is
approved/online and the live Partner ID/Key is available:

```bash
cd /mnt/data/nextstep-node-2/nexflow-release
python3 scripts/shopee-live-cutover.py \
  --target aoy \
  --partner-id <AOY_LIVE_PARTNER_ID> \
  --enable-realtime \
  --prompt-push-secret
```

The helper:

- resolves `/mnt/data/nextstep-node-2/<instance>/.env` from
  `deploy/nextstep-instances.json`
- reads Partner Key and Push Partner Key via hidden prompts
- writes a timestamped `.env` backup before changing anything
- sets the OAuth callback to `<PUBLIC_BASE_URL>/api/shopee-api/callback`
- prints the Shopee push callback format without revealing the token

Then restart only the target instance:

```bash
cd /mnt/data/nextstep-node-2/nexflow-aoy
docker compose up -d --build backend frontend
```

The cutover is not complete until all gates pass:

- `/health` returns OK
- `/settings/shopee-connections` shows live config ready
- OAuth returns to the same customer domain
- token status is OK
- latest sync succeeds
- `/shopee-operations` loads orders
- `/import/shopee` API preview works for a small date range
- a real push event updates notification/badge

Do not delete the old shop connection or token until the dedicated app passes
the gates above. If any gate fails, restore the `.env` backup and restart the
same target instance. Do not change demo or Lanboon during an AOY cutover.

After completing Shopee Console work, change the Shopee Open Platform account
password if credentials were shared in chat or tickets.

## Smoke Checks

```bash
ssh ubuntu@10.121.20.83

# Containers
sudo docker ps --format '{{.Names}} {{.Status}} {{.Ports}}' | grep -E 'nexflow|sml-api'

# Edge public entrypoint
curl -s -o /dev/null -w '%{http_code}\n' -H 'Host: nexflow.nextstep-soft.com' http://localhost:6323/login
curl -s -o /dev/null -w '%{http_code}\n' -H 'Host: nexflow-aoy.nextstep-soft.com' http://localhost:6323/login
curl -s -o /dev/null -w '%{http_code}\n' -H 'Host: nextflow-lanboon.nextstep-soft.com' http://localhost:6323/login

# Demo debug/direct
curl -s http://localhost:8110/health
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:16323/login

# AOY debug/direct
curl -s http://localhost:8111/health
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:16324/login

# Lanboon debug/direct
curl -s http://localhost:8112/health
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:16325/login

# SML gateway
curl -s -H 'x-tenant: demo' http://localhost:8200/health/ready
curl -s -H 'x-tenant: aoy' http://localhost:8200/health/ready
curl -s -H 'x-tenant: lbk63' http://localhost:8200/health/ready
```

## Cloudflare / IT Routing

The domains can use the same CNAME target from IT when they land on the same
server/router. All hostnames should route to the same origin:

| Hostname | Origin |
| --- | --- |
| `nexflow.nextstep-soft.com` | `http://10.121.20.83:6323` |
| `nexflow-aoy.nextstep-soft.com` | `http://10.121.20.83:6323` |
| `nextflow-lanboon.nextstep-soft.com` | `http://10.121.20.83:6323` |

If Cloudflare returns 525, the origin/proxy SSL mode is wrong for the origin.
Keep Cloudflare public HTTPS, but proxy to the internal HTTP port above unless
IT installs an origin certificate on the reverse proxy.

## Legacy DEV Only

The old deployment below is no longer production:

```text
192.168.2.109
animal-galvanize-tameness.ngrok-free.dev
/home/bosscatdog/billflow-henna
frontend 3030
```

Use it only as a historical/dev reference.
