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
import tempfile
from datetime import datetime
from pathlib import Path


ROOT = Path("/mnt/data/nextstep-node-2")
REGISTRY = Path("deploy/nextstep-instances.json")
GATEWAY_ENV = ROOT / "nexflow-shopee-gateway" / ".env"
DIRECT_ROLLBACK_ENV = ".shopee-direct-rollback.env"
DIRECT_CREDENTIAL_KEYS = (
    "SHOPEE_OPEN_API_PARTNER_ID",
    "SHOPEE_OPEN_API_PARTNER_KEY",
)


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


def write_env_atomic(path: Path, lines: list[str]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile("w", dir=path.parent, delete=False) as handle:
        temporary = Path(handle.name)
        handle.write("\n".join(lines) + "\n")
    os.chmod(temporary, 0o600)
    os.replace(temporary, path)


def direct_credential_values(values: dict[str, str]) -> dict[str, str]:
    return {key: values.get(key, "").strip() for key in DIRECT_CREDENTIAL_KEYS}


def credentials_complete(values: dict[str, str]) -> bool:
    return all(values.get(key, "").strip() for key in DIRECT_CREDENTIAL_KEYS)


def prepare_mode_updates(mode: str, current: dict[str, str], rollback_path: Path) -> dict[str, str]:
    if mode == "gateway":
        active_credentials = direct_credential_values(current)
        if any(active_credentials.values()) and not credentials_complete(active_credentials):
            raise SystemExit("active direct credentials are incomplete; repair or remove them before gateway cutover")
        if credentials_complete(active_credentials):
            write_env_atomic(
                rollback_path,
                [f"{key}={active_credentials[key]}" for key in DIRECT_CREDENTIAL_KEYS],
            )
        return {
            "SHOPEE_OPEN_API_MODE": "gateway",
            **{key: "" for key in DIRECT_CREDENTIAL_KEYS},
        }

    if mode == "direct":
        rollback_credentials: dict[str, str] = {}
        if rollback_path.exists():
            _, rollback_credentials = read_env(rollback_path)
        credentials = direct_credential_values(rollback_credentials)
        if not credentials_complete(credentials):
            credentials = direct_credential_values(current)
        if not credentials_complete(credentials):
            raise SystemExit(
                "direct rollback credentials are unavailable; restore the tenant backup or reconnect explicitly"
            )
        return {
            "SHOPEE_OPEN_API_MODE": "direct",
            **credentials,
        }

    return {}


def gateway_feature_updates(enabled: bool, environment: str) -> dict[str, str]:
    value = "true" if enabled else "false"
    return {
        "SHOPEE_OPEN_API_ENABLED": value,
        "SHOPEE_OPEN_API_ENV": environment,
        "ENABLE_SHOPEE_REALTIME_OPS": value,
        "VITE_ENABLE_SHOPEE_REALTIME_OPS": value,
    }


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--target", required=True)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--mode", choices=["gateway", "direct"])
    mode.add_argument("--identity-only", action="store_true", help="provision signed gateway identity without changing mode")
    parser.add_argument(
        "--open-api-enabled",
        choices=["true", "false"],
        help="explicitly enable or disable the tenant Shopee feature in gateway mode",
    )
    parser.add_argument("--registry", default=str(REGISTRY))
    parser.add_argument("--server-root", default=str(ROOT))
    args = parser.parse_args()
    if args.mode == "gateway" and args.open_api_enabled is None:
        parser.error("--open-api-enabled is required with --mode gateway")
    if args.open_api_enabled is not None and args.mode != "gateway":
        parser.error("--open-api-enabled is valid only with --mode gateway")
    return args


def main() -> int:
    args = parse_args()
    registry = json.loads(Path(args.registry).read_text())
    row = next((item for item in registry.get("instances", []) if item.get("name") == args.target), None)
    if row is None:
        raise SystemExit(f"unknown target: {args.target}")
    env_path = Path(args.server_root) / str(row["folder"]) / ".env"
    lines, current = read_env(env_path)
    _, gateway = read_env(GATEWAY_ENV)
    master = gateway.get("SHOPEE_GATEWAY_INTERNAL_MASTER_KEY", "")
    updates = {
        "SHOPEE_GATEWAY_BASE_URL": "http://nexflow-shopee-gateway:8091",
        "SHOPEE_GATEWAY_PUBLIC_URL": gateway.get("PUBLIC_BASE_URL", "https://shopee-gateway.nextstep-soft.com").rstrip("/"),
        "SHOPEE_GATEWAY_TENANT": args.target,
        "SHOPEE_GATEWAY_INTERNAL_SECRET": derive(master, args.target),
    }
    if args.mode:
        updates.update(prepare_mode_updates(args.mode, current, env_path.with_name(DIRECT_ROLLBACK_ENV)))
    if args.mode == "gateway":
        updates.update(
            gateway_feature_updates(
                args.open_api_enabled == "true",
                gateway.get("SHOPEE_OPEN_API_ENV", "live"),
            )
        )
    output = upsert(lines, updates)
    if output == lines:
        print(f"Gateway identity already configured for {args.target}.")
        return 0
    stamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    backup = env_path.with_name(f"{env_path.name}.bak-shopee-gateway-{stamp}")
    shutil.copy2(env_path, backup)
    os.chmod(backup, 0o600)
    write_env_atomic(env_path, output)
    if args.mode == "gateway":
        feature_state = "enabled" if args.open_api_enabled == "true" else "disabled"
        action = f"gateway mode with Shopee feature {feature_state}"
    else:
        action = f"{args.mode} mode" if args.mode else "gateway identity"
    print(f"Updated {args.target} {action}; backup created.")
    if args.mode:
        print(f"Restart with deploy_nextstep_instances.py --target {args.target}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
