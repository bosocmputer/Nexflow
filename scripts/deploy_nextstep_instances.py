#!/usr/bin/env python3
"""Deploy Nexflow to the NextStep production instances from Git on the server.

Production layout:

- release clone: /mnt/data/nextstep-node-2/nexflow-release
- demo:          /mnt/data/nextstep-node-2/nexflow          frontend edge, backend 8110
- aoy:           /mnt/data/nextstep-node-2/nexflow-aoy      frontend edge, backend 8111
- lanboon:       /mnt/data/nextstep-node-2/nexflow-lanboon  frontend edge, backend 8112
- edge:          /mnt/data/nextstep-node-2/nexflow-edge public 6323
- Shopee gateway:/mnt/data/nextstep-node-2/nexflow-shopee-gateway

Instance definitions live in deploy/nextstep-instances.json. Use
scripts/nextstep_instance_registry.py to add future customers and render edge
snapshots.

The release clone is the only Git checkout used for Docker build contexts.
Each instance keeps its own docker-compose.yml, .env, Postgres volume, backups,
and artifacts. The script updates instance compose files once so backend/frontend
build from the shared release clone instead of copied source folders. Public
traffic enters through nexflow-edge and is routed by Host header.

Usage:
  NX_PASS=... python scripts/deploy_nextstep_instances.py --target all
  NX_PASS=... python scripts/deploy_nextstep_instances.py --target demo
  NX_PASS=... python scripts/deploy_nextstep_instances.py --target aoy
  NX_PASS=... python scripts/deploy_nextstep_instances.py --target lanboon
  NX_PASS=... python scripts/deploy_nextstep_instances.py --target gateway
  NX_PASS=... python scripts/deploy_nextstep_instances.py --ref d52de63
  python scripts/deploy_nextstep_instances.py --list-targets
"""
from __future__ import annotations

import argparse
import json
import os
import shlex
import shutil
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path


DEFAULT_HOST = "10.121.20.83"
DEFAULT_USER = "ubuntu"
REPO_URL = "https://github.com/bosocmputer/Nexflow.git"
SERVER_ROOT = "/mnt/data/nextstep-node-2"
RELEASE_DIR = f"{SERVER_ROOT}/nexflow-release"
BACKUP_DIR = f"{SERVER_ROOT}/nexflow-backups"
EDGE_DIR = f"{SERVER_ROOT}/nexflow-edge"
GATEWAY_DIR = f"{SERVER_ROOT}/nexflow-shopee-gateway"
GATEWAY_HOSTNAME = "shopee-gateway.nextstep-soft.com"
GATEWAY_CONTAINER = "nexflow-shopee-gateway"
GATEWAY_POSTGRES_CONTAINER = "nexflow-shopee-gateway-postgres"
GATEWAY_NETWORK = "nexflow-shopee-gateway_default"
EDGE_PORT = 6323
ROOT = Path(__file__).resolve().parents[1]
REGISTRY_PATH = ROOT / "deploy" / "nextstep-instances.json"


@dataclass(frozen=True)
class Target:
    name: str
    remote: str
    hostname: str
    frontend_debug_port: int
    previous_frontend_port: int
    backend_port: int
    postgres_port: int
    postgres_container: str
    backend_container: str
    frontend_container: str
    public_url: str
    folder: str
    sml_tenant: str


def fail(message: str) -> None:
    print(f"ERROR: {message}", file=sys.stderr)
    raise SystemExit(1)


def load_targets(registry_path: Path = REGISTRY_PATH) -> dict[str, Target]:
    raw = json.loads(registry_path.read_text())
    targets: dict[str, Target] = {}
    seen_values: dict[str, set[str | int]] = {
        "name": set(),
        "hostname": set(),
        "backend_port": set(),
        "postgres_port": set(),
        "frontend_debug_port": set(),
        "folder": set(),
    }
    for item in raw.get("instances", []):
        name = str(item["name"]).strip()
        folder = str(item["folder"]).strip()
        target = Target(
            name=name,
            folder=folder,
            remote=f"{SERVER_ROOT}/{folder}",
            hostname=str(item["hostname"]).strip(),
            frontend_debug_port=int(item["frontend_debug_port"]),
            previous_frontend_port=int(item.get("previous_frontend_port", item["frontend_debug_port"])),
            backend_port=int(item["backend_port"]),
            postgres_port=int(item["postgres_port"]),
            postgres_container=str(item["postgres_container"]).strip(),
            backend_container=str(item["backend_container"]).strip(),
            frontend_container=str(item["frontend_container"]).strip(),
            public_url=str(item.get("public_url") or f"https://{item['hostname']}").strip(),
            sml_tenant=str(item["sml_tenant"]).strip(),
        )
        values = {
            "name": target.name,
            "hostname": target.hostname,
            "backend_port": target.backend_port,
            "postgres_port": target.postgres_port,
            "frontend_debug_port": target.frontend_debug_port,
            "folder": target.folder,
        }
        for key, value in values.items():
            if value in seen_values[key]:
                fail(f"duplicate {key} in {registry_path}: {value}")
            seen_values[key].add(value)
        targets[target.name] = target
    if not targets:
        fail(f"no instances found in {registry_path}")
    return targets


TARGETS: dict[str, Target] = load_targets()


def require_tool(name: str) -> None:
    if shutil.which(name) is None:
        fail(f"{name} is required")


def password() -> str:
    value = os.environ.get("NX_PASS") or os.environ.get("NX_NEXTSTEP_PASS")
    if not value:
        fail("NX_PASS env var required")
    return value


def command_env() -> dict[str, str]:
    env = os.environ.copy()
    env["SSHPASS"] = password()
    return env


def run(cmd: list[str], *, label: str | None = None, timeout: int = 900, display: str | None = None) -> str:
    if label:
        print(f"\n========== {label} ==========")
    print("$ " + (display or " ".join(shlex.quote(part) for part in cmd)))
    result = subprocess.run(
        cmd,
        env=command_env(),
        text=True,
        capture_output=True,
        timeout=timeout,
        encoding="utf-8",
        errors="replace",
    )
    if result.stdout:
        print(result.stdout.rstrip())
    if result.stderr:
        print(result.stderr.rstrip(), file=sys.stderr)
    if result.returncode != 0:
        fail(f"command failed ({result.returncode})")
    return result.stdout.strip()


def ssh(command: str, *, label: str | None = None, timeout: int = 900) -> str:
    host = os.environ.get("NX_NEXTSTEP_HOST", DEFAULT_HOST)
    user = os.environ.get("NX_NEXTSTEP_USER", DEFAULT_USER)
    return run(
        [
            "sshpass",
            "-e",
            "ssh",
            "-o",
            "StrictHostKeyChecking=no",
            f"{user}@{host}",
            command,
        ],
        label=label,
        timeout=timeout,
        display=f"ssh {user}@{host} '<remote command>'",
    )


def sudo(command: str, *, label: str | None = None, timeout: int = 900) -> str:
    quoted_password = shlex.quote(password())
    quoted_command = shlex.quote(command)
    return ssh(
        f"printf %s\\\\n {quoted_password} | sudo -S bash -lc {quoted_command}",
        label=label,
        timeout=timeout,
    )


def resolve_ref(ref: str) -> str:
    if ref in {"main", "master"}:
        return f"origin/{ref}"
    return ref


def ensure_release_clone(ref: str) -> str:
    repo_url = os.environ.get("NX_REPO_URL", REPO_URL)
    resolved_ref = resolve_ref(ref)
    remote_user = os.environ.get("NX_NEXTSTEP_USER", DEFAULT_USER)
    script = f"""
set -euo pipefail
mkdir -p {shlex.quote(SERVER_ROOT)}
chown {shlex.quote(remote_user)}:{shlex.quote(remote_user)} {shlex.quote(SERVER_ROOT)}
if [ ! -d {shlex.quote(RELEASE_DIR)}/.git ]; then
  rm -rf {shlex.quote(RELEASE_DIR)}
  git clone {shlex.quote(repo_url)} {shlex.quote(RELEASE_DIR)}
fi
cd {shlex.quote(RELEASE_DIR)}
git remote set-url origin {shlex.quote(repo_url)}
git fetch --prune origin
git checkout --detach {shlex.quote(resolved_ref)}
git clean -fdx
git rev-parse HEAD
chown -R {shlex.quote(remote_user)}:{shlex.quote(remote_user)} {shlex.quote(RELEASE_DIR)}
"""
    return sudo(script, label=f"prepare release clone ({resolved_ref})", timeout=300).splitlines()[-1].strip()


def ensure_instance_compose(target: Target) -> None:
    script = f"""
set -euo pipefail
python3 - <<'PY'
from datetime import datetime, timezone
from pathlib import Path
import shutil

path = Path({target.remote!r}) / "docker-compose.yml"
text = path.read_text()
new = text
new = new.replace("build: ./backend", "build: {RELEASE_DIR}/backend")
new = new.replace("context: ./frontend", "context: {RELEASE_DIR}/frontend")
new = new.replace('      - "{target.previous_frontend_port}:80"', '      - "127.0.0.1:{target.frontend_debug_port}:80"')
new = new.replace('      - "0.0.0.0:{target.previous_frontend_port}:80"', '      - "127.0.0.1:{target.frontend_debug_port}:80"')
new = new.replace('      - "127.0.0.1:{target.previous_frontend_port}:80"', '      - "127.0.0.1:{target.frontend_debug_port}:80"')
if new != text:
    backup = path.with_name(path.name + ".bak-gitdeploy-" + datetime.now(timezone.utc).strftime("%Y%m%d%H%M%S"))
    shutil.copy2(path, backup)
    path.write_text(new)
    print(f"updated {{path}}; backup={{backup}}")
else:
    print(f"compose build contexts already point to {RELEASE_DIR}")
PY
"""
    sudo(script, label=f"ensure instance compose {target.name}", timeout=60)


def edge_network_name(target: Target) -> str:
    return f"{target.folder}_default"


def render_edge_nginx(targets: dict[str, Target] | None = None) -> str:
    targets = targets or TARGETS
    parts = [
        "server {",
        "    listen 80 default_server;",
        "    server_name _;",
        "    return 444;",
        "}",
        "",
    ]
    for target in targets.values():
        parts.extend(
            [
                "server {",
                "    listen 80;",
                f"    server_name {target.hostname};",
                "",
                "    location = /api/admin/events {",
                f"        proxy_pass http://{target.backend_container}:8090;",
                "        proxy_http_version 1.1;",
                "        proxy_set_header Host $host;",
                "        proxy_set_header X-Real-IP $remote_addr;",
                "        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;",
                "        proxy_set_header X-Forwarded-Proto $scheme;",
                '        proxy_set_header Connection "";',
                "        proxy_buffering off;",
                "        proxy_cache off;",
                "        proxy_read_timeout 1h;",
                "        proxy_send_timeout 1h;",
                '        add_header X-Accel-Buffering "no" always;',
                "    }",
                "",
                "    location /api/ {",
                f"        proxy_pass http://{target.backend_container}:8090;",
                "        proxy_set_header Host $host;",
                "        proxy_set_header X-Real-IP $remote_addr;",
                "        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;",
                "        proxy_set_header X-Forwarded-Proto $scheme;",
                "    }",
                "",
                "    location /webhook/ {",
                f"        proxy_pass http://{target.backend_container}:8090;",
                "        proxy_set_header Host $host;",
                "        proxy_set_header X-Real-IP $remote_addr;",
                "        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;",
                "        proxy_set_header X-Forwarded-Proto $scheme;",
                "    }",
                "",
                "    location /health {",
                f"        proxy_pass http://{target.backend_container}:8090;",
                "        proxy_set_header Host $host;",
                "        proxy_set_header X-Real-IP $remote_addr;",
                "        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;",
                "        proxy_set_header X-Forwarded-Proto $scheme;",
                "    }",
                "",
                "    location / {",
                f"        proxy_pass http://{target.frontend_container}:80;",
                "        proxy_set_header Host $host;",
                "        proxy_set_header X-Real-IP $remote_addr;",
                "        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;",
                "        proxy_set_header X-Forwarded-Proto $scheme;",
                "    }",
                "}",
                "",
            ]
        )
    parts.extend(
        [
            "server {",
            "    listen 80;",
            f"    server_name {GATEWAY_HOSTNAME};",
            "",
            "    location /internal/ { return 404; }",
            "",
            "    location = /health {",
            f"        proxy_pass http://{GATEWAY_CONTAINER}:8091;",
            "        proxy_set_header Host $host;",
            "        proxy_set_header X-Real-IP $remote_addr;",
            "        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;",
            "        proxy_set_header X-Forwarded-Proto $scheme;",
            "    }",
            "",
            "    location = /api/shopee/callback {",
            f"        proxy_pass http://{GATEWAY_CONTAINER}:8091;",
            "        proxy_set_header Host $host;",
            "        proxy_set_header X-Real-IP $remote_addr;",
            "        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;",
            "        proxy_set_header X-Forwarded-Proto $scheme;",
            "    }",
            "",
            "    location = /webhook/shopee {",
            f"        proxy_pass http://{GATEWAY_CONTAINER}:8091;",
            "        proxy_set_header Host $host;",
            "        proxy_set_header X-Real-IP $remote_addr;",
            "        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;",
            "        proxy_set_header X-Forwarded-Proto $scheme;",
            "    }",
            "",
            "    location / { return 404; }",
            "}",
            "",
        ]
    )
    return "\n".join(parts).rstrip() + "\n"


def render_edge_compose(targets: dict[str, Target] | None = None) -> str:
    targets = targets or TARGETS
    networks = [*(edge_network_name(target) for target in targets.values()), GATEWAY_NETWORK]
    network_lines = "\n".join(f"      - {network}" for network in networks)
    external_lines = "\n".join(f"  {network}:\n    external: true" for network in networks)
    return f"""services:
  edge:
    image: nginx:1.27-alpine
    container_name: nexflow-edge
    ports:
      - "${{EDGE_BIND:-6323:80}}"
    volumes:
      - ./nginx.conf:/etc/nginx/conf.d/default.conf:ro
    networks:
{network_lines}
    restart: unless-stopped

networks:
{external_lines}
"""


def ensure_edge_config() -> None:
    sudo(
        "set -euo pipefail; "
        f"mkdir -p {shlex.quote(EDGE_DIR)}; "
        f"python3 {shlex.quote(RELEASE_DIR)}/scripts/deploy_nextstep_instances.py --print-edge-compose > {shlex.quote(EDGE_DIR)}/docker-compose.yml; "
        f"python3 {shlex.quote(RELEASE_DIR)}/scripts/deploy_nextstep_instances.py --print-edge-nginx > {shlex.quote(EDGE_DIR)}/nginx.conf; "
        f"touch {shlex.quote(EDGE_DIR)}/.env; "
        f"grep -q '^EDGE_BIND=' {shlex.quote(EDGE_DIR)}/.env "
        f"&& sed -i 's#^EDGE_BIND=.*#EDGE_BIND={EDGE_PORT}:80#' {shlex.quote(EDGE_DIR)}/.env "
        f"|| printf 'EDGE_BIND={EDGE_PORT}:80\\n' >> {shlex.quote(EDGE_DIR)}/.env; "
        f"cd {shlex.quote(EDGE_DIR)} && docker compose config >/dev/null",
        label="ensure edge config",
        timeout=60,
    )


def start_edge() -> None:
    sudo(
        f"cd {shlex.quote(EDGE_DIR)} && docker compose up -d",
        label="start edge",
        timeout=120,
    )


def smoke_edge(targets: list[Target]) -> None:
    for target in targets:
        login_status = ssh(
            f"curl -s -o /dev/null -w '%{{http_code}}' -H 'Host: {target.hostname}' http://localhost:{EDGE_PORT}/login",
            label=f"edge login status {target.name}",
            timeout=30,
        )
        if login_status.strip() != "200":
            fail(f"{target.name} edge login returned {login_status!r}")
        health = ssh(
            f"curl -s -m 10 -H 'Host: {target.hostname}' http://localhost:{EDGE_PORT}/health",
            label=f"edge health {target.name}",
            timeout=30,
        )
        if '"status":"ok"' not in health:
            fail(f"{target.name} edge health check failed")
    ssh(
        f"curl -s -o /dev/null -w '%{{http_code}}' -H 'Host: unknown.nextstep-soft.com' http://localhost:{EDGE_PORT}/ || true",
        label="edge unknown host status",
        timeout=30,
    )
    gateway_health = ssh(
        f"curl -s -m 10 -H 'Host: {GATEWAY_HOSTNAME}' http://localhost:{EDGE_PORT}/health",
        label="edge health Shopee gateway",
        timeout=30,
    )
    if '"status":"ok"' not in gateway_health:
        fail("Shopee gateway edge health check failed")
    internal_status = ssh(
        f"curl -s -o /dev/null -w '%{{http_code}}' -H 'Host: {GATEWAY_HOSTNAME}' http://localhost:{EDGE_PORT}/internal/v1/shopee/execute",
        label="edge blocks Shopee gateway internal API",
        timeout=30,
    )
    if internal_status.strip() != "404":
        fail(f"Shopee gateway internal API is publicly reachable: {internal_status!r}")


def backup_target(target: Target) -> None:
    sudo(
        "set -euo pipefail; "
        f"cd {shlex.quote(target.remote)}; "
        "ts=$(date +%Y%m%d-%H%M%S); "
        f"mkdir -p {shlex.quote(BACKUP_DIR)}/{shlex.quote(target.name)}; "
        f"[ -f .env ] && cp -p .env {shlex.quote(BACKUP_DIR)}/{shlex.quote(target.name)}/.env.bak.$ts || true; "
        f"docker exec {shlex.quote(target.postgres_container)} pg_dump -U nexflow -d nexflow "
        f"| gzip > {shlex.quote(BACKUP_DIR)}/{shlex.quote(target.name)}/pre-deploy-$ts.sql.gz; "
        f"ls -lh {shlex.quote(BACKUP_DIR)}/{shlex.quote(target.name)}/pre-deploy-$ts.sql.gz",
        label=f"backup {target.name}",
        timeout=300,
    )


def ensure_gateway_runtime() -> None:
    script = f"""
set -euo pipefail
mkdir -p {shlex.quote(GATEWAY_DIR)}
if [ -f {shlex.quote(GATEWAY_DIR)}/docker-compose.yml ]; then
  cp -p {shlex.quote(GATEWAY_DIR)}/docker-compose.yml {shlex.quote(GATEWAY_DIR)}/docker-compose.yml.bak.$(date +%Y%m%d-%H%M%S)
fi
cp {shlex.quote(RELEASE_DIR)}/deploy/shopee-gateway/docker-compose.yml {shlex.quote(GATEWAY_DIR)}/docker-compose.yml
test -f {shlex.quote(GATEWAY_DIR)}/.env || {{
  echo 'Missing {GATEWAY_DIR}/.env. Provision gateway secrets before deploy.' >&2
  exit 1
}}
chmod 600 {shlex.quote(GATEWAY_DIR)}/.env
cd {shlex.quote(GATEWAY_DIR)}
docker compose config >/dev/null
"""
    sudo(script, label="ensure Shopee gateway runtime", timeout=60)


def backup_gateway() -> None:
    script = f"""
set -euo pipefail
ts=$(date +%Y%m%d-%H%M%S)
mkdir -p {shlex.quote(BACKUP_DIR)}/gateway
cp -p {shlex.quote(GATEWAY_DIR)}/.env {shlex.quote(BACKUP_DIR)}/gateway/.env.bak.$ts
if docker inspect {shlex.quote(GATEWAY_POSTGRES_CONTAINER)} >/dev/null 2>&1; then
  docker exec {shlex.quote(GATEWAY_POSTGRES_CONTAINER)} pg_dump -U nexflow_gateway -d nexflow_shopee_gateway \
    | gzip > {shlex.quote(BACKUP_DIR)}/gateway/pre-deploy-$ts.sql.gz
fi
"""
    sudo(script, label="backup Shopee gateway", timeout=300)


def deploy_gateway() -> None:
    ensure_gateway_runtime()
    backup_gateway()
    sudo(
        f"cd {shlex.quote(GATEWAY_DIR)} && docker compose up -d --build",
        label="docker up Shopee gateway",
        timeout=1200,
    )
    health = sudo(
        f"docker run --rm --network {shlex.quote(GATEWAY_NETWORK)} curlimages/curl:8.12.1 -fsS http://{shlex.quote(GATEWAY_CONTAINER)}:8091/health",
        label="Shopee gateway internal health",
        timeout=60,
    )
    if '"status":"ok"' not in health:
        sudo(f"docker logs {shlex.quote(GATEWAY_CONTAINER)} --tail=150", label="Shopee gateway logs", timeout=30)
        fail("Shopee gateway health check failed")


def connect_target_to_gateway(target: Target) -> None:
    script = f"""
set -euo pipefail
mode=$(sed -n 's/^SHOPEE_OPEN_API_MODE=//p' {shlex.quote(target.remote)}/.env | tail -n 1)
if ! docker network inspect {shlex.quote(GATEWAY_NETWORK)} >/dev/null 2>&1; then
  if [ "$mode" = "gateway" ]; then
    echo 'Shopee gateway network is missing for a gateway-mode tenant' >&2
    exit 1
  fi
  exit 0
fi
if ! docker inspect {shlex.quote(target.backend_container)} --format '{{{{json .NetworkSettings.Networks}}}}' | grep -q '"{GATEWAY_NETWORK}"'; then
  docker network connect {shlex.quote(GATEWAY_NETWORK)} {shlex.quote(target.backend_container)}
fi
"""
    sudo(script, label=f"connect {target.name} backend to Shopee gateway network", timeout=30)


def provision_target_gateway_identity(target: Target) -> None:
    sudo(
        f"cd {shlex.quote(RELEASE_DIR)} && "
        f"python3 scripts/shopee_gateway_tenant_mode.py --target {shlex.quote(target.name)} --identity-only",
        label=f"provision {target.name} Shopee gateway identity",
        timeout=60,
    )


def deploy_target(target: Target) -> None:
    ssh(
        f"test -d {shlex.quote(target.remote)} && test -f {shlex.quote(target.remote)}/docker-compose.yml && test -f {shlex.quote(target.remote)}/.env",
        label=f"precheck {target.name}",
        timeout=30,
    )
    ssh(
        f"curl -s -m 5 http://localhost:{target.backend_port}/health || true",
        label=f"current health {target.name}",
        timeout=30,
    )
    ensure_instance_compose(target)
    provision_target_gateway_identity(target)
    backup_target(target)
    sudo(
        f"cd {shlex.quote(target.remote)} && docker compose up -d --build backend frontend",
        label=f"docker up {target.name}",
        timeout=1200,
    )
    connect_target_to_gateway(target)
    health = ssh(
        f"curl -s -m 10 http://localhost:{target.backend_port}/health",
        label=f"backend health {target.name}",
        timeout=30,
    )
    if '"status":"ok"' not in health:
        sudo(
            f"docker logs {shlex.quote(target.backend_container)} --tail=120",
            label=f"backend logs {target.name}",
            timeout=30,
        )
        fail(f"{target.name} backend health check failed")
    ssh(
        f"curl -s -o /dev/null -w '%{{http_code}}' http://127.0.0.1:{target.frontend_debug_port}/login",
        label=f"frontend debug login status {target.name}",
        timeout=30,
    )
    sudo(
        f"docker logs {shlex.quote(target.backend_container)} --since=2m 2>&1 "
        "| grep -iE 'fatal|panic|error|5xx' | tail -30 || true",
        label=f"recent backend error scan {target.name}",
        timeout=30,
    )
    print(f"OK {target.name}: {target.public_url}")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Deploy Nexflow to NextStep production instances from server Git")
    parser.add_argument("--target", choices=["all", "gateway", *TARGETS.keys()], default="all")
    parser.add_argument("--ref", default="origin/main", help="server git ref to deploy, default origin/main")
    parser.add_argument("--print-edge-nginx", action="store_true", help="render edge nginx.conf from registry and exit")
    parser.add_argument("--print-edge-compose", action="store_true", help="render edge docker-compose.yml from registry and exit")
    parser.add_argument("--list-targets", action="store_true", help="list deploy targets from registry and exit")
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    if args.print_edge_nginx:
        print(render_edge_nginx(), end="")
        return
    if args.print_edge_compose:
        print(render_edge_compose(), end="")
        return
    if args.list_targets:
        print(f"gateway\t{GATEWAY_HOSTNAME}\t{GATEWAY_DIR}\t-")
        for target in TARGETS.values():
            print(f"{target.name}\t{target.hostname}\t{target.remote}\t{target.sml_tenant}")
        return

    require_tool("sshpass")
    selected = list(TARGETS.values()) if args.target == "all" else ([] if args.target == "gateway" else [TARGETS[args.target]])
    deployed_commit = ensure_release_clone(args.ref)
    print(f"Deploying commit: {deployed_commit}")
    if args.target in {"all", "gateway"}:
        deploy_gateway()
    for target in selected:
        deploy_target(target)
    ensure_edge_config()
    start_edge()
    smoke_edge(selected)
    print("\nDeploy complete.")


if __name__ == "__main__":
    main()
