#!/usr/bin/env python3
"""Provision central Shopee gateway secrets on the production server.

Run on the server release clone. Secrets are read with hidden prompts, never
printed, and written only to the gateway runtime `.env` with mode 0600.
"""

from __future__ import annotations

import argparse
import base64
import getpass
import os
import secrets
import shutil
from datetime import datetime
from pathlib import Path


DEFAULT_ENV = Path("/mnt/data/nextstep-node-2/nexflow-shopee-gateway/.env")


def read_env(path: Path) -> tuple[list[str], dict[str, str]]:
    if not path.exists():
        return [], {}
    lines = path.read_text().splitlines()
    values: dict[str, str] = {}
    for line in lines:
        if not line.strip() or line.lstrip().startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        values[key.strip()] = value.strip()
    return lines, values


def upsert(lines: list[str], updates: dict[str, str]) -> list[str]:
    output: list[str] = []
    seen: set[str] = set()
    for line in lines:
        if not line.strip() or line.lstrip().startswith("#") or "=" not in line:
            output.append(line)
            continue
        key = line.split("=", 1)[0].strip()
        if key in updates:
            output.append(f"{key}={updates[key]}")
            seen.add(key)
        else:
            output.append(line)
    output.extend(f"{key}={value}" for key, value in updates.items() if key not in seen)
    return output


def key32() -> str:
    return base64.b64encode(os.urandom(32)).decode("ascii")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--partner-id", type=int)
    parser.add_argument(
        "--source-env",
        help="Read existing Partner ID/Key and push secret from a server env without printing them",
    )
    parser.add_argument("--env-file", default=str(DEFAULT_ENV))
    parser.add_argument("--public-url", default="https://shopee-gateway.nextstep-soft.com")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    source: dict[str, str] = {}
    if args.source_env:
        source_path = Path(args.source_env)
        if not source_path.is_file():
            raise SystemExit(f"source env not found: {source_path}")
        _, source = read_env(source_path)
    partner_id_raw = str(args.partner_id or source.get("SHOPEE_OPEN_API_PARTNER_ID", "")).strip()
    try:
        partner_id = int(partner_id_raw)
    except ValueError as exc:
        raise SystemExit("--partner-id must be positive, or provide a valid --source-env") from exc
    if partner_id <= 0:
        raise SystemExit("--partner-id must be positive, or provide a valid --source-env")
    path = Path(args.env_file)
    path.parent.mkdir(parents=True, exist_ok=True)
    lines, existing = read_env(path)
    partner_key = source.get("SHOPEE_OPEN_API_PARTNER_KEY", "").strip()
    push_secret = (
        source.get("SHOPEE_GATEWAY_PUSH_SECRET", "").strip()
        or source.get("SHOPEE_REALTIME_WEBHOOK_SECRET", "").strip()
    )
    if not partner_key:
        partner_key = getpass.getpass("Shopee Partner Key: ").strip()
    if not push_secret:
        push_secret = getpass.getpass("Shopee Push Partner Key / callback secret: ").strip()
    if len(partner_key) < 20 or len(push_secret) < 20:
        raise SystemExit("Partner Key and Push secret look too short")
    updates = {
        "ENV": "production",
        "PUBLIC_BASE_URL": args.public_url.rstrip("/"),
        "SHOPEE_OPEN_API_ENV": "live",
        "SHOPEE_OPEN_API_BASE_URL": "https://partner.shopeemobile.com",
        "SHOPEE_OPEN_API_PARTNER_ID": str(partner_id),
        "SHOPEE_OPEN_API_PARTNER_KEY": partner_key,
        "SHOPEE_GATEWAY_PUSH_SECRET": push_secret,
        "SHOPEE_GATEWAY_DB_PASSWORD": existing.get("SHOPEE_GATEWAY_DB_PASSWORD") or secrets.token_urlsafe(32),
        "SHOPEE_GATEWAY_TOKEN_ENCRYPTION_KEY": existing.get("SHOPEE_GATEWAY_TOKEN_ENCRYPTION_KEY") or key32(),
        "SHOPEE_GATEWAY_INTERNAL_MASTER_KEY": existing.get("SHOPEE_GATEWAY_INTERNAL_MASTER_KEY") or key32(),
        "SHOPEE_GATEWAY_OAUTH_SIGNING_KEY": existing.get("SHOPEE_GATEWAY_OAUTH_SIGNING_KEY") or key32(),
        "SHOPEE_GATEWAY_EXTERNAL_TIMEOUT": existing.get("SHOPEE_GATEWAY_EXTERNAL_TIMEOUT") or "20s",
        "SHOPEE_GATEWAY_TENANT_TIMEOUT": existing.get("SHOPEE_GATEWAY_TENANT_TIMEOUT") or "10s",
    }
    if path.exists():
        stamp = datetime.now().strftime("%Y%m%d-%H%M%S")
        backup = path.with_name(f"{path.name}.bak.{stamp}")
        shutil.copy2(path, backup)
        os.chmod(backup, 0o600)
    path.write_text("\n".join(upsert(lines, updates)) + "\n")
    os.chmod(path, 0o600)
    print(f"Gateway environment ready: {path}")
    print("Secrets were not printed. Deploy with --target gateway next.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
