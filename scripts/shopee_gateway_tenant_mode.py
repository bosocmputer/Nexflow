#!/usr/bin/env python3
"""Switch one Nexflow instance between central gateway and direct rollback."""

from __future__ import annotations

import argparse
import base64
import hashlib
import hmac
import json
import os
import shutil
from datetime import datetime
from pathlib import Path


ROOT = Path("/mnt/data/nextstep-node-2")
REGISTRY = Path("deploy/nextstep-instances.json")
GATEWAY_ENV = ROOT / "nexflow-shopee-gateway" / ".env"


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
    key = base64.b64decode(master, validate=True)
    if len(key) != 32:
        raise SystemExit("gateway internal master key is invalid")
    return hmac.new(key, f"nexflow-shopee-gateway/tenant/{tenant}".encode(), hashlib.sha256).hexdigest()


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--target", required=True)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--mode", choices=["gateway", "direct"])
    mode.add_argument("--identity-only", action="store_true", help="provision signed gateway identity without changing mode")
    parser.add_argument("--registry", default=str(REGISTRY))
    parser.add_argument("--server-root", default=str(ROOT))
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    registry = json.loads(Path(args.registry).read_text())
    row = next((item for item in registry.get("instances", []) if item.get("name") == args.target), None)
    if row is None:
        raise SystemExit(f"unknown target: {args.target}")
    env_path = Path(args.server_root) / str(row["folder"]) / ".env"
    lines, _ = read_env(env_path)
    _, gateway = read_env(GATEWAY_ENV)
    master = gateway.get("SHOPEE_GATEWAY_INTERNAL_MASTER_KEY", "")
    updates = {
        "SHOPEE_GATEWAY_BASE_URL": "http://nexflow-shopee-gateway:8091",
        "SHOPEE_GATEWAY_PUBLIC_URL": gateway.get("PUBLIC_BASE_URL", "https://shopee-gateway.nextstep-soft.com").rstrip("/"),
        "SHOPEE_GATEWAY_TENANT": args.target,
        "SHOPEE_GATEWAY_INTERNAL_SECRET": derive(master, args.target),
    }
    if args.mode:
        updates["SHOPEE_OPEN_API_MODE"] = args.mode
    if args.mode == "gateway":
        updates.update(
            {
                "SHOPEE_OPEN_API_ENABLED": "true",
                "SHOPEE_OPEN_API_ENV": gateway.get("SHOPEE_OPEN_API_ENV", "live"),
                "ENABLE_SHOPEE_REALTIME_OPS": "true",
                "VITE_ENABLE_SHOPEE_REALTIME_OPS": "true",
            }
        )
    output = upsert(lines, updates)
    if output == lines:
        print(f"Gateway identity already configured for {args.target}.")
        return 0
    stamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    backup = env_path.with_name(f"{env_path.name}.bak-shopee-gateway-{stamp}")
    shutil.copy2(env_path, backup)
    os.chmod(backup, 0o600)
    env_path.write_text("\n".join(output) + "\n")
    os.chmod(env_path, 0o600)
    action = f"{args.mode} mode" if args.mode else "gateway identity"
    print(f"Updated {args.target} {action}; backup created.")
    if args.mode:
        print(f"Restart with deploy_nextstep_instances.py --target {args.target}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
