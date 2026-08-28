"""Pure rendering helpers for a fresh, isolated Nexflow tenant runtime."""

from __future__ import annotations

import secrets
from dataclasses import dataclass
from typing import Protocol


class RuntimeTarget(Protocol):
    name: str
    folder: str
    public_url: str
    sml_tenant: str
    postgres_container: str
    backend_container: str
    frontend_container: str
    postgres_port: int
    backend_port: int
    frontend_debug_port: int


@dataclass(frozen=True)
class BootstrapSecrets:
    db_password: str
    jwt_secret: str
    media_signing_key: str
    admin_password: str


def generate_bootstrap_secrets() -> BootstrapSecrets:
    return BootstrapSecrets(
        db_password=secrets.token_urlsafe(32),
        jwt_secret=secrets.token_urlsafe(48),
        media_signing_key=secrets.token_urlsafe(48),
        admin_password=secrets.token_urlsafe(32),
    )


def render_fresh_instance_compose(target: RuntimeTarget, release_dir: str) -> str:
    volume_name = f"{target.folder}_pgdata"
    return f"""services:
  postgres:
    image: postgres:16-alpine
    container_name: {target.postgres_container}
    volumes:
      - nexflow_pgdata:/var/lib/postgresql/data
    environment:
      POSTGRES_DB: nexflow
      POSTGRES_USER: ${{DB_USER}}
      POSTGRES_PASSWORD: ${{DB_PASSWORD}}
    ports:
      - "127.0.0.1:{target.postgres_port}:5432"
    mem_limit: 512m
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${{DB_USER}} -d nexflow"]
      interval: 10s
      timeout: 5s
      retries: 10
    restart: unless-stopped

  backend:
    build: {release_dir}/backend
    container_name: {target.backend_container}
    ports:
      - "127.0.0.1:{target.backend_port}:8090"
    env_file: .env
    environment:
      DATABASE_URL: "postgres://${{DB_USER}}:${{DB_PASSWORD}}@postgres:5432/nexflow?sslmode=disable"
      GIN_MODE: release
      ENV: production
    volumes:
      - ./backups:/app/backups
      - ./artifacts:/app/artifacts
    depends_on:
      postgres:
        condition: service_healthy
    mem_limit: 512m
    restart: unless-stopped

  frontend:
    build:
      context: {release_dir}/frontend
      args:
        VITE_PHASE: ${{VITE_PHASE:-99}}
        VITE_ENABLE_SALES_ORDERS: ${{VITE_ENABLE_SALES_ORDERS:-true}}
        VITE_ENABLE_SHOPEE_EXCEL: ${{VITE_ENABLE_SHOPEE_EXCEL:-true}}
        VITE_ENABLE_SHOPEE_REALTIME_OPS: ${{VITE_ENABLE_SHOPEE_REALTIME_OPS:-true}}
        VITE_ENABLE_LAZADA_EXCEL: ${{VITE_ENABLE_LAZADA_EXCEL:-true}}
        VITE_ENABLE_TIKTOK_EXCEL: ${{VITE_ENABLE_TIKTOK_EXCEL:-true}}
        VITE_ENABLE_CHAT: ${{VITE_ENABLE_CHAT:-false}}
    container_name: {target.frontend_container}
    ports:
      - "127.0.0.1:{target.frontend_debug_port}:80"
    depends_on:
      - backend
    mem_limit: 256m
    restart: unless-stopped

volumes:
  nexflow_pgdata:
    name: {volume_name}
"""


def render_fresh_instance_env(target: RuntimeTarget, runtime: BootstrapSecrets) -> str:
    return f"""PROJECT_NAME={target.folder}
PORT=8090
ENV=production

BOOTSTRAP_ADMIN_EMAIL=admin@nexflow.local
BOOTSTRAP_ADMIN_NAME={target.name.title()} Admin
BOOTSTRAP_ADMIN_PASSWORD={runtime.admin_password}

DB_USER=nexflow
DB_PASSWORD={runtime.db_password}
JWT_SECRET={runtime.jwt_secret}
JWT_EXPIRE_HOURS=24
MEDIA_SIGNING_KEY={runtime.media_signing_key}
PUBLIC_BASE_URL={target.public_url}

LINE_CHANNEL_SECRET=
LINE_CHANNEL_ACCESS_TOKEN=
LINE_ADMIN_USER_ID=
ENABLE_LINE_MYSHOP=false

SHOPEE_SML_URL=http://172.17.0.1:8200
SHOPEE_SML_GUID=smlx
SHOPEE_SML_PROVIDER=NEXT
SHOPEE_SML_CONFIG_FILE=SMLConfigNEXT.xml
SHOPEE_SML_DATABASE={target.sml_tenant}
SHOPEE_SML_DOC_FORMAT=
SHOPEE_SML_SALE_CODE=
SHOPEE_SML_BRANCH_CODE=
SHOPEE_SML_WH_CODE=
SHOPEE_SML_SHELF_CODE=
SHOPEE_SML_UNIT_CODE=
SHOPEE_SML_VAT_TYPE=-1
SHOPEE_SML_VAT_RATE=-1

MARKETPLACE_GROUPED_UI_ENABLED=true
MARKETPLACE_UNIT_CATALOG_ENABLED=false
MARKETPLACE_CONVERSION_MODE=off
MARKETPLACE_RESERVATION_LEDGER_ENABLED=false
PRODUCT_MAPPING_MASTER_MODE=active
SML_SET_PRODUCT_EXPANSION_ENABLED=false
SHOPEE_SET_STOCK_ENABLED=false
SML_STOCK_AVAILABILITY_MODE=physical_v1
SML_STOCK_SOURCE_FINGERPRINT=

SHOPEE_OPEN_API_MODE=gateway
SHOPEE_OPEN_API_ENABLED=false
SHOPEE_OPEN_API_ENV=live
SHOPEE_OPEN_API_PARTNER_ID=
SHOPEE_OPEN_API_PARTNER_KEY=
SHOPEE_GATEWAY_TENANT={target.name}
ENABLE_SHOPEE_REALTIME_OPS=false
VITE_ENABLE_SHOPEE_REALTIME_OPS=true
SHOPEE_AUTO_SML_ENABLED=false
SHOPEE_AUTO_SML_CANCEL_ENABLED=false
ENABLE_SHOPEE_SML_CANCEL_DOCUMENTS=false
ENABLE_SHOPEE_CANCEL_AFTER_SML_ALERTS=true
ENABLE_SHOPEE_RICH_LINE_FLEX=true
ENABLE_SHOPEE_SETTLEMENT_LINE_ALERTS=true
ENABLE_SHOPEE_ORDER_ESCROW_ENRICHMENT=true
SHOPEE_REALTIME_SYNC_INTERVAL_SECONDS=0

VITE_PHASE=99
VITE_ENABLE_SALES_ORDERS=true
VITE_ENABLE_SHOPEE_EXCEL=true
VITE_ENABLE_LAZADA_EXCEL=true
VITE_ENABLE_TIKTOK_EXCEL=true
VITE_ENABLE_CHAT=false
PURCHASE_FLOW_ENABLED=false

BACKUP_CRON_HOUR=0
DISK_WARN_PERCENT=90
DATA_LIFECYCLE_ENABLED=true
DATA_LIFECYCLE_CRON_HOUR=2
HOT_LOG_DAYS=90
AUTO_ARCHIVE_DAYS=180
SUMMARY_RETENTION_DAYS=730
PURGE_BATCH_SIZE=1000
ARTIFACTS_DIR=/app/artifacts
ARTIFACTS_MAX_BYTES=10485760
"""
