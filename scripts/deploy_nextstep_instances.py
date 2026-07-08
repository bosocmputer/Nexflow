#!/usr/bin/env python3
"""Deploy Nexflow to the NextStep production instances from Git on the server.

Production layout:

- release clone: /mnt/data/nextstep-node-2/nexflow-release
- demo:          /mnt/data/nextstep-node-2/nexflow      frontend 6323, backend 8110
- aoy:           /mnt/data/nextstep-node-2/nexflow-aoy  frontend 6324, backend 8111
- edge:          /mnt/data/nextstep-node-2/nexflow-edge public 6323

The release clone is the only Git checkout used for Docker build contexts.
Each instance keeps its own docker-compose.yml, .env, Postgres volume, backups,
and artifacts. The script updates instance compose files once so backend/frontend
build from the shared release clone instead of copied source folders. Public
traffic enters through nexflow-edge and is routed by Host header.

Usage:
  NX_PASS=... python scripts/deploy_nextstep_instances.py --target all
  NX_PASS=... python scripts/deploy_nextstep_instances.py --target demo
  NX_PASS=... python scripts/deploy_nextstep_instances.py --target aoy
  NX_PASS=... python scripts/deploy_nextstep_instances.py --ref d52de63
"""
from __future__ import annotations

import argparse
import os
import shlex
import shutil
import subprocess
import sys
from dataclasses import dataclass


DEFAULT_HOST = "10.121.20.83"
DEFAULT_USER = "ubuntu"
REPO_URL = "https://github.com/bosocmputer/Nexflow.git"
SERVER_ROOT = "/mnt/data/nextstep-node-2"
RELEASE_DIR = f"{SERVER_ROOT}/nexflow-release"
BACKUP_DIR = f"{SERVER_ROOT}/nexflow-backups"
EDGE_DIR = f"{SERVER_ROOT}/nexflow-edge"
EDGE_PORT = 6323


@dataclass(frozen=True)
class Target:
    name: str
    remote: str
    hostname: str
    frontend_debug_port: int
    previous_frontend_port: int
    backend_port: int
    postgres_container: str
    backend_container: str
    public_url: str


TARGETS: dict[str, Target] = {
    "demo": Target(
        name="demo",
        remote=f"{SERVER_ROOT}/nexflow",
        hostname="nexflow.nextstep-soft.com",
        frontend_debug_port=16323,
        previous_frontend_port=6323,
        backend_port=8110,
        postgres_container="nexflow-postgres",
        backend_container="nexflow-backend",
        public_url="https://nexflow.nextstep-soft.com",
    ),
    "aoy": Target(
        name="aoy",
        remote=f"{SERVER_ROOT}/nexflow-aoy",
        hostname="nexflow-aoy.nextstep-soft.com",
        frontend_debug_port=16324,
        previous_frontend_port=6324,
        backend_port=8111,
        postgres_container="nexflow-aoy-postgres",
        backend_container="nexflow-aoy-backend",
        public_url="https://nexflow-aoy.nextstep-soft.com",
    ),
}


def fail(message: str) -> None:
    print(f"ERROR: {message}", file=sys.stderr)
    raise SystemExit(1)


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


def ensure_edge_config() -> None:
    sudo(
        "set -euo pipefail; "
        f"mkdir -p {shlex.quote(EDGE_DIR)}; "
        f"cp -f {shlex.quote(RELEASE_DIR)}/deploy/nextstep-edge/docker-compose.yml {shlex.quote(EDGE_DIR)}/docker-compose.yml; "
        f"cp -f {shlex.quote(RELEASE_DIR)}/deploy/nextstep-edge/nginx.conf {shlex.quote(EDGE_DIR)}/nginx.conf; "
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
    backup_target(target)
    sudo(
        f"cd {shlex.quote(target.remote)} && docker compose up -d --build backend frontend",
        label=f"docker up {target.name}",
        timeout=1200,
    )
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
    parser = argparse.ArgumentParser(description="Deploy Nexflow to NextStep demo/aoy instances from server Git")
    parser.add_argument("--target", choices=["all", *TARGETS.keys()], default="all")
    parser.add_argument("--ref", default="origin/main", help="server git ref to deploy, default origin/main")
    return parser.parse_args()


def main() -> None:
    require_tool("sshpass")
    args = parse_args()
    selected = list(TARGETS.values()) if args.target == "all" else [TARGETS[args.target]]
    deployed_commit = ensure_release_clone(args.ref)
    print(f"Deploying commit: {deployed_commit}")
    for target in selected:
        deploy_target(target)
    ensure_edge_config()
    start_edge()
    smoke_edge(selected)
    print("\nDeploy complete.")


if __name__ == "__main__":
    main()
