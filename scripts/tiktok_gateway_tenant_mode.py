#!/usr/bin/env python3
"""Provision one tenant's TikTok Shop gateway identity and feature gate."""

from __future__ import annotations

import argparse
import base64
import binascii
import hashlib
import hmac
import json
import os
import re
import shutil
import tempfile
from datetime import datetime
from pathlib import Path
from urllib.parse import urlparse


ROOT = Path("/mnt/data/nextstep-node-2")
REGISTRY = Path("deploy/nextstep-instances.json")
GATEWAY_FOLDER = "nexflow-tiktok-shop-gateway"
GATEWAY_INTERNAL_URL = "http://nexflow-tiktok-shop-gateway:8092"


def read_env(path: Path) -> tuple[list[str], dict[str, str]]:
    lines = path.read_text().splitlines()
    values: dict[str, str] = {}
    for line in lines:
        if line.strip() and not line.lstrip().startswith("#") and "=" in line:
            key, value = line.split("=", 1)
            values[key.strip()] = value.strip()
    return lines, values


def upsert(lines: list[str], updates: dict[str, str]) -> list[str]:
    output: list[str] = []
    seen: set[str] = set()
    for line in lines:
        if line.strip() and not line.lstrip().startswith("#") and "=" in line:
            key = line.split("=", 1)[0].strip()
            if key in updates:
                output.append(f"{key}={updates[key]}")
                seen.add(key)
                continue
        output.append(line)
    output.extend(f"{key}={value}" for key, value in updates.items() if key not in seen)
    return output


def derive(master: str, tenant: str) -> str:
    tenant = tenant.strip().lower()
    if not re.fullmatch(r"[a-z0-9][a-z0-9_-]{0,62}", tenant):
        raise SystemExit("TikTok Shop gateway tenant is invalid")
    try:
        key = base64.b64decode(master.strip(), validate=True)
    except (binascii.Error, ValueError):
        raise SystemExit("TikTok Shop gateway internal master key is invalid") from None
    if len(key) != 32:
        raise SystemExit("TikTok Shop gateway internal master key is invalid")
    domain = f"nexflow-tiktok-shop-gateway/tenant/{tenant}".encode()
    return hmac.new(key, domain, hashlib.sha256).hexdigest()


def validate_public_url(value: str) -> str:
    normalized = value.strip().rstrip("/")
    parsed = urlparse(normalized)
    if parsed.scheme != "https" or not parsed.netloc or parsed.username or parsed.password or parsed.query or parsed.fragment:
        raise SystemExit("TikTok Shop gateway PUBLIC_BASE_URL must be an absolute HTTPS URL")
    return normalized


def prepare_updates(
    tenant: str,
    current: dict[str, str],
    gateway: dict[str, str],
    *,
    enabled: bool | None,
) -> dict[str, str]:
    del current  # Identity-only deliberately preserves all unrelated tenant values.
    public_url = validate_public_url(gateway.get("PUBLIC_BASE_URL", ""))
    master = gateway.get("TIKTOK_SHOP_GATEWAY_INTERNAL_MASTER_KEY", "")
    updates = {
        "TIKTOK_SHOP_GATEWAY_BASE_URL": GATEWAY_INTERNAL_URL,
        "TIKTOK_SHOP_GATEWAY_PUBLIC_URL": public_url,
        "TIKTOK_SHOP_GATEWAY_TENANT": tenant.strip().lower(),
        "TIKTOK_SHOP_GATEWAY_INTERNAL_SECRET": derive(master, tenant),
    }
    if enabled is not None:
        value = "true" if enabled else "false"
        updates.update(
            {
                "TIKTOK_SHOP_OPEN_API_ENABLED": value,
                "VITE_ENABLE_TIKTOK_SHOP_API": value,
            }
        )
    return updates


def write_env_atomic(path: Path, lines: list[str]) -> None:
    with tempfile.NamedTemporaryFile("w", dir=path.parent, delete=False) as handle:
        temporary = Path(handle.name)
        handle.write("\n".join(lines).rstrip() + "\n")
    os.chmod(temporary, 0o600)
    os.replace(temporary, path)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--target", required=True)
    action = parser.add_mutually_exclusive_group(required=True)
    action.add_argument("--identity-only", action="store_true", help="provision identity without changing feature flags")
    action.add_argument("--open-api-enabled", choices=["true", "false"], help="explicitly enable or disable backend and frontend gates together")
    parser.add_argument("--registry", default=str(REGISTRY))
    parser.add_argument("--server-root", default=str(ROOT))
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    registry = json.loads(Path(args.registry).read_text())
    row = next((item for item in registry.get("instances", []) if item.get("name") == args.target), None)
    if row is None:
        raise SystemExit(f"unknown target: {args.target}")
    server_root = Path(args.server_root)
    tenant_env_path = server_root / str(row["folder"]) / ".env"
    gateway_env_path = server_root / GATEWAY_FOLDER / ".env"
    if not tenant_env_path.is_file():
        raise SystemExit(f"tenant env is missing: {tenant_env_path}")
    if not gateway_env_path.is_file():
        raise SystemExit(f"TikTok Shop gateway env is missing: {gateway_env_path}")
    lines, current = read_env(tenant_env_path)
    _, gateway = read_env(gateway_env_path)
    enabled = None if args.identity_only else args.open_api_enabled == "true"
    updates = prepare_updates(args.target, current, gateway, enabled=enabled)
    output = upsert(lines, updates)
    if output == lines:
        print(f"TikTok Shop gateway settings already current for {args.target}.")
        return 0
    stamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    backup = tenant_env_path.with_name(f"{tenant_env_path.name}.bak-tiktok-gateway-{stamp}")
    shutil.copy2(tenant_env_path, backup)
    os.chmod(backup, 0o600)
    write_env_atomic(tenant_env_path, output)
    if enabled is None:
        action = "identity provisioned; feature flags unchanged"
    else:
        action = "feature enabled" if enabled else "feature disabled"
    print(f"Updated {args.target} TikTok Shop gateway: {action}; private backup created.")
    print(f"Restart with deploy_nextstep_instances.py --target {args.target}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
