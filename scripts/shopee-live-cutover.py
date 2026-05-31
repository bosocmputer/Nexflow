#!/usr/bin/env python3
"""Switch Nexflow Shopee Open API config from sandbox to live safely.

This script is intentionally small and boring: it updates only the
SHOPEE_OPEN_API_* lines in an env file, makes a timestamped backup, and reads
the live partner key via getpass so the secret does not appear in shell history.
"""

from __future__ import annotations

import argparse
import getpass
import shutil
from datetime import datetime
from pathlib import Path


LIVE_BASE_URL = "https://partner.shopeemobile.com"


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--env-file",
        default="/home/bosscatdog/nexflow/.env",
        help="Nexflow .env file to update",
    )
    parser.add_argument(
        "--partner-id",
        required=True,
        help="Shopee live Partner ID from the approved app",
    )
    parser.add_argument(
        "--public-base-url",
        default="",
        help="Public Nexflow URL, without trailing slash. Defaults to existing PUBLIC_BASE_URL",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print non-secret changes without writing the env file",
    )
    return parser.parse_args()


def read_env(path: Path) -> tuple[list[str], dict[str, str]]:
    lines = path.read_text().splitlines()
    values: dict[str, str] = {}
    for line in lines:
        stripped = line.strip()
        if not stripped or stripped.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        values[key.strip()] = value.strip()
    return lines, values


def upsert_env(lines: list[str], updates: dict[str, str]) -> list[str]:
    seen: set[str] = set()
    out: list[str] = []
    for line in lines:
        stripped = line.strip()
        if not stripped or stripped.startswith("#") or "=" not in line:
            out.append(line)
            continue
        key = line.split("=", 1)[0].strip()
        if key in updates:
            out.append(f"{key}={updates[key]}")
            seen.add(key)
        else:
            out.append(line)
    for key, value in updates.items():
        if key not in seen:
            out.append(f"{key}={value}")
    return out


def main() -> int:
    args = parse_args()
    env_path = Path(args.env_file)
    if not env_path.exists():
        raise SystemExit(f"env file not found: {env_path}")

    lines, existing = read_env(env_path)
    public_base_url = args.public_base_url.strip().rstrip("/") or existing.get("PUBLIC_BASE_URL", "").rstrip("/")
    if not public_base_url:
        raise SystemExit("PUBLIC_BASE_URL is empty; pass --public-base-url")

    partner_id = args.partner_id.strip()
    if not partner_id.isdigit():
        raise SystemExit("--partner-id must be numeric")

    partner_key = ""
    if not args.dry_run:
        partner_key = getpass.getpass("Shopee live Partner Key: ").strip()
        if len(partner_key) < 20:
            raise SystemExit("partner key looks too short")

    updates = {
        "SHOPEE_OPEN_API_ENABLED": "true",
        "SHOPEE_OPEN_API_ENV": "live",
        "SHOPEE_OPEN_API_BASE_URL": LIVE_BASE_URL,
        "SHOPEE_OPEN_API_PARTNER_ID": partner_id,
        "SHOPEE_OPEN_API_PARTNER_KEY": partner_key,
        "SHOPEE_OPEN_API_REDIRECT_URL": f"{public_base_url}/api/shopee-api/callback",
    }

    if args.dry_run:
        for key, value in updates.items():
            printable = "***" if key.endswith("KEY") else value
            print(f"{key}={printable}")
        return 0

    stamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    backup_path = env_path.with_name(f"{env_path.name}.bak.{stamp}")
    shutil.copy2(env_path, backup_path)
    env_path.write_text("\n".join(upsert_env(lines, updates)) + "\n")
    print(f"updated {env_path}")
    print(f"backup  {backup_path}")
    print("next: cd /home/bosscatdog/nexflow && docker compose up -d backend")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
