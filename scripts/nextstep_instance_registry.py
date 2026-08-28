#!/usr/bin/env python3
"""Manage Nexflow NextStep production instance registry.

This helper is intentionally local-file only: it never stores SML passwords.
Use it before provisioning a new customer instance, then review/commit the
registry and generated edge snapshots.
"""
from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
REGISTRY_PATH = ROOT / "deploy" / "nextstep-instances.json"
EDGE_NGINX_PATH = ROOT / "deploy" / "nextstep-edge" / "nginx.conf"
EDGE_COMPOSE_PATH = ROOT / "deploy" / "nextstep-edge" / "docker-compose.yml"
NAME_RE = re.compile(r"^[a-z0-9][a-z0-9-]*$")
TENANT_RE = re.compile(r"^[a-z0-9_]+$")


def fail(message: str) -> None:
    print(f"ERROR: {message}", file=sys.stderr)
    raise SystemExit(1)


def load_registry() -> dict:
    return json.loads(REGISTRY_PATH.read_text())


def save_registry(data: dict) -> None:
    REGISTRY_PATH.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n")


def instances(data: dict) -> list[dict]:
    value = data.get("instances")
    if not isinstance(value, list) or not value:
        fail(f"{REGISTRY_PATH} must contain a non-empty instances list")
    return value


def validate_registry(data: dict, *, check_deploy_script: bool = True) -> None:
    checks: dict[str, set] = {
        "name": set(),
        "hostname": set(),
        "folder": set(),
        "frontend_debug_port": set(),
        "backend_port": set(),
        "postgres_port": set(),
    }
    for item in instances(data):
        name = str(item.get("name", ""))
        tenant = str(item.get("sml_tenant", ""))
        if not NAME_RE.match(name):
            fail(f"invalid instance name: {name!r}")
        if not TENANT_RE.match(tenant):
            fail(f"invalid sml_tenant for {name}: {tenant!r}")
        for key in checks:
            value = item.get(key)
            if value in checks[key]:
                fail(f"duplicate {key}: {value!r}")
            checks[key].add(value)
    if check_deploy_script:
        subprocess.run([sys.executable, str(ROOT / "scripts" / "deploy_nextstep_instances.py"), "--list-targets"], check=True)


def next_ports(data: dict) -> tuple[int, int, int]:
    rows = instances(data)
    return (
        max(int(row["backend_port"]) for row in rows) + 1,
        max(int(row["postgres_port"]) for row in rows) + 1,
        max(int(row["frontend_debug_port"]) for row in rows) + 1,
    )


def render_edge_snapshots() -> None:
    nginx = subprocess.check_output(
        [sys.executable, str(ROOT / "scripts" / "deploy_nextstep_instances.py"), "--print-edge-nginx"],
        text=True,
    )
    compose = subprocess.check_output(
        [sys.executable, str(ROOT / "scripts" / "deploy_nextstep_instances.py"), "--print-edge-compose"],
        text=True,
    )
    EDGE_NGINX_PATH.write_text(nginx)
    EDGE_COMPOSE_PATH.write_text(compose)


def command_suggest(_: argparse.Namespace) -> None:
    backend, postgres, frontend = next_ports(load_registry())
    print("Next available ports:")
    print(f"  backend:        {backend}")
    print(f"  postgres:       {postgres}")
    print(f"  frontend debug: {frontend}")
    print()
    print("Example:")
    print(
        "  python3 scripts/nextstep_instance_registry.py add "
        "--name newshop "
        "--hostname nextflow-newshop.nextstep-soft.com "
        "--sml-tenant newshop_db "
        "--sml-host customer-ddns.example.com "
        "--sml-port 5432"
    )


def command_add(args: argparse.Namespace) -> None:
    data = load_registry()
    backend_port = args.backend_port
    postgres_port = args.postgres_port
    frontend_debug_port = args.frontend_debug_port
    if backend_port is None or postgres_port is None or frontend_debug_port is None:
        suggested_backend, suggested_postgres, suggested_frontend = next_ports(data)
        backend_port = backend_port or suggested_backend
        postgres_port = postgres_port or suggested_postgres
        frontend_debug_port = frontend_debug_port or suggested_frontend

    name = args.name.strip().lower()
    tenant = args.sml_tenant.strip().lower()
    if not NAME_RE.match(name):
        fail("name must be lowercase letters/numbers/hyphens")
    if not TENANT_RE.match(tenant):
        fail("sml-tenant must be lowercase letters/numbers/underscore")

    folder = args.folder or f"nexflow-{name}"
    row = {
        "name": name,
        "hostname": args.hostname.strip().lower(),
        "folder": folder,
        "frontend_container": f"{folder}-frontend",
        "backend_container": f"{folder}-backend",
        "postgres_container": f"{folder}-postgres",
        "frontend_debug_port": int(frontend_debug_port),
        "previous_frontend_port": int(frontend_debug_port),
        "backend_port": int(backend_port),
        "postgres_port": int(postgres_port),
        "public_url": f"https://{args.hostname.strip().lower()}",
        "gateway_backend_url": f"http://{folder}-backend:8090",
        "sml_tenant": tenant,
        "sml_host": args.sml_host.strip(),
        "sml_port": int(args.sml_port),
        "sml_database": (args.sml_database or tenant).strip(),
    }
    data["instances"].append(row)
    validate_registry(data, check_deploy_script=False)
    save_registry(data)
    validate_registry(load_registry())
    render_edge_snapshots()
    print(f"Added {name} to {REGISTRY_PATH}")
    print(f"Rendered {EDGE_NGINX_PATH} and {EDGE_COMPOSE_PATH}")
    print()
    print_it_message(row["hostname"])


def print_it_message(hostname: str) -> None:
    print("IT / Cloudflare message:")
    print()
    print(f"ขอเพิ่ม hostname ใหม่: {hostname}")
    print()
    print("DNS:")
    print("Type: CNAME")
    print(f"Name: {hostname.split('.')[0]}")
    print("Target: 9bd809c61265.sn.mynetname.net")
    print("Proxy: ON")
    print()
    print("Origin/proxy forward ไป:")
    print("http://10.121.20.83:6323")
    print()
    print("สำคัญ: ใช้ HTTP origin ไม่ใช่ HTTPS origin และ preserve Host header เดิม")


def command_it_message(args: argparse.Namespace) -> None:
    print_it_message(args.hostname.strip().lower())


def command_validate(_: argparse.Namespace) -> None:
    validate_registry(load_registry())
    render_edge_snapshots()
    print("Registry valid. Edge snapshots rendered.")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Manage NextStep Nexflow instance registry")
    sub = parser.add_subparsers(dest="command", required=True)

    sub.add_parser("suggest", help="show next available ports").set_defaults(func=command_suggest)
    sub.add_parser("validate", help="validate registry and render edge snapshots").set_defaults(func=command_validate)

    msg = sub.add_parser("it-message", help="print Cloudflare/IT message for a hostname")
    msg.add_argument("--hostname", required=True)
    msg.set_defaults(func=command_it_message)

    add = sub.add_parser("add", help="add a future customer instance to registry")
    add.add_argument("--name", required=True, help="short customer key, e.g. lanboon")
    add.add_argument("--hostname", required=True, help="public hostname")
    add.add_argument("--folder", help="server folder name, default nexflow-<name>")
    add.add_argument("--backend-port", type=int)
    add.add_argument("--postgres-port", type=int)
    add.add_argument("--frontend-debug-port", type=int)
    add.add_argument("--sml-tenant", required=True, help="tenant/database header used by sml-api-bybos")
    add.add_argument("--sml-host", required=True)
    add.add_argument("--sml-port", required=True, type=int)
    add.add_argument("--sml-database", help="actual SML database name, default --sml-tenant")
    add.set_defaults(func=command_add)
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    args.func(args)


if __name__ == "__main__":
    main()
