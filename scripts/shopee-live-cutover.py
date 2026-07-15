#!/usr/bin/env python3
"""Switch one Nexflow instance to a Shopee Open Platform live app safely.

This script updates only the selected instance `.env`, makes a timestamped
backup, and reads Shopee secrets via getpass so they do not appear in shell
history. It is intentionally conservative: Shopee Console approval, OAuth, and
sync smoke tests still happen outside this script before the cutover is done.
"""

from __future__ import annotations

import argparse
import getpass
import json
import shutil
from datetime import datetime
from pathlib import Path


LIVE_BASE_URL = "https://partner.shopeemobile.com"
DEFAULT_REGISTRY = Path("deploy/nextstep-instances.json")
DEFAULT_SERVER_ROOT = Path("/mnt/data/nextstep-node-2")
LEGACY_ENV_FILE = Path("/home/bosscatdog/nexflow/.env")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--target",
        default="",
        help="Instance name from deploy/nextstep-instances.json, e.g. aoy",
    )
    parser.add_argument(
        "--registry",
        default=str(DEFAULT_REGISTRY),
        help="Instance registry JSON used with --target",
    )
    parser.add_argument(
        "--server-root",
        default=str(DEFAULT_SERVER_ROOT),
        help="Production root containing instance folders, used with --target",
    )
    parser.add_argument(
        "--env-file",
        default="",
        help="Nexflow .env file to update. Overrides --target path when set",
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
        "--enable-realtime",
        action="store_true",
        help="Enable Shopee realtime operations flags for backend and frontend",
    )
    parser.add_argument(
        "--prompt-push-secret",
        action="store_true",
        help="Prompt for Shopee Push Partner Key and set SHOPEE_REALTIME_WEBHOOK_SECRET",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print non-secret changes without writing the env file",
    )
    return parser.parse_args()


def load_registry(path: Path) -> dict[str, dict[str, object]]:
    data = json.loads(path.read_text())
    instances = data.get("instances", [])
    return {str(row.get("name", "")).strip(): row for row in instances if row.get("name")}


def resolve_env_path(args: argparse.Namespace) -> tuple[Path, str, str]:
    target = args.target.strip()
    if target:
        registry = load_registry(Path(args.registry))
        instance = registry.get(target)
        if not instance:
            known = ", ".join(sorted(registry)) or "(none)"
            raise SystemExit(f"unknown --target {target!r}; known targets: {known}")
        folder = str(instance.get("folder") or "").strip()
        if not folder:
            raise SystemExit(f"target {target!r} has no folder in {args.registry}")
        env_path = Path(args.env_file) if args.env_file else Path(args.server_root) / folder / ".env"
        public_base_url = args.public_base_url.strip().rstrip("/") or str(instance.get("public_url") or "").rstrip("/")
        return env_path, public_base_url, folder

    env_path = Path(args.env_file) if args.env_file else LEGACY_ENV_FILE
    return env_path, args.public_base_url.strip().rstrip("/"), env_path.parent.name


def read_env(path: Path, *, allow_missing: bool = False) -> tuple[list[str], dict[str, str]]:
    if not path.exists():
        if allow_missing:
            return [], {}
        raise SystemExit(f"env file not found: {path}")
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


def mask_env_value(key: str, value: str) -> str:
    upper = key.upper()
    if "KEY" in upper or "SECRET" in upper or "TOKEN" in upper:
        return "***"
    return value


def main() -> int:
    args = parse_args()
    env_path, registry_public_base_url, folder = resolve_env_path(args)

    lines, existing = read_env(env_path, allow_missing=args.dry_run)
    public_base_url = registry_public_base_url or existing.get("PUBLIC_BASE_URL", "").rstrip("/")
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

    push_secret = ""
    if args.prompt_push_secret:
        if args.dry_run:
            push_secret = "<hidden>"
        else:
            push_secret = getpass.getpass("Shopee Push Partner Key / webhook token: ").strip()
            if len(push_secret) < 20:
                raise SystemExit("push secret looks too short")

    updates = {
        "SHOPEE_OPEN_API_ENABLED": "true",
        "SHOPEE_OPEN_API_ENV": "live",
        "SHOPEE_OPEN_API_BASE_URL": LIVE_BASE_URL,
        "SHOPEE_OPEN_API_PARTNER_ID": partner_id,
        "SHOPEE_OPEN_API_PARTNER_KEY": partner_key,
        "SHOPEE_OPEN_API_REDIRECT_URL": f"{public_base_url}/api/shopee-api/callback",
    }
    if args.enable_realtime:
        updates["ENABLE_SHOPEE_REALTIME_OPS"] = "true"
        updates["VITE_ENABLE_SHOPEE_REALTIME_OPS"] = "true"
    if args.prompt_push_secret:
        updates["SHOPEE_REALTIME_WEBHOOK_SECRET"] = push_secret

    if args.dry_run:
        if not env_path.exists():
            print(f"env file not found, dry-run only: {env_path}")
        print(f"target folder: {folder}")
        print(f"public url: {public_base_url}")
        for key, value in updates.items():
            printable = mask_env_value(key, value)
            print(f"{key}={printable}")
        print(f"oauth callback: {public_base_url}/api/shopee-api/callback")
        if args.prompt_push_secret:
            print(f"push callback: {public_base_url}/webhook/shopee?token=<secret>")
        return 0

    stamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    backup_path = env_path.with_name(f"{env_path.name}.bak.{stamp}")
    shutil.copy2(env_path, backup_path)
    env_path.write_text("\n".join(upsert_env(lines, updates)) + "\n")
    print(f"updated {env_path}")
    print(f"backup  {backup_path}")
    print(f"oauth callback: {public_base_url}/api/shopee-api/callback")
    if args.prompt_push_secret:
        print(f"push callback: {public_base_url}/webhook/shopee?token=<secret>")
    print(f"next: cd {env_path.parent} && docker compose up -d --build backend frontend")
    print("gate: OAuth connect, token status, latest sync, and order preview must pass before the cutover is complete")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
