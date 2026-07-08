#!/usr/bin/env python3
"""Deploy committed Nexflow code to the NextStep server instances.

This script is for the 10.121.20.83 server layout:

- demo: /mnt/data/nextstep-node-2/nexflow      frontend 6323, backend 8110
- aoy:  /mnt/data/nextstep-node-2/nexflow-aoy  frontend 6324, backend 8111

It syncs only backend/ and frontend/ from a git ref. It intentionally does not
sync docker-compose.yml or .env because each instance has different ports,
container names, volumes, public URLs, and feature flags.

Usage:
  NX_PASS=... python scripts/deploy_nextstep_instances.py --target all
  NX_PASS=... python scripts/deploy_nextstep_instances.py --target demo
  NX_PASS=... python scripts/deploy_nextstep_instances.py --target aoy
"""
from __future__ import annotations

import argparse
import os
import shlex
import shutil
import subprocess
import sys
import tarfile
import tempfile
from dataclasses import dataclass
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
DEFAULT_HOST = "10.121.20.83"
DEFAULT_USER = "ubuntu"
BACKUP_DIR = "/mnt/data/nextstep-node-2/nexflow-backups"


@dataclass(frozen=True)
class Target:
    name: str
    remote: str
    frontend_port: int
    backend_port: int
    postgres_container: str
    backend_container: str
    frontend_container: str
    public_url: str


TARGETS: dict[str, Target] = {
    "demo": Target(
        name="demo",
        remote="/mnt/data/nextstep-node-2/nexflow",
        frontend_port=6323,
        backend_port=8110,
        postgres_container="nexflow-postgres",
        backend_container="nexflow-backend",
        frontend_container="nexflow-frontend",
        public_url="https://nexflow.nextstep-soft.com",
    ),
    "aoy": Target(
        name="aoy",
        remote="/mnt/data/nextstep-node-2/nexflow-aoy",
        frontend_port=6324,
        backend_port=8111,
        postgres_container="nexflow-aoy-postgres",
        backend_container="nexflow-aoy-backend",
        frontend_container="nexflow-aoy-frontend",
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


def run(
    cmd: list[str],
    *,
    label: str | None = None,
    timeout: int = 900,
    cwd: Path | None = None,
    display: str | None = None,
) -> str:
    if label:
        print(f"\n========== {label} ==========")
    print("$ " + (display or " ".join(shlex.quote(part) for part in cmd)))
    result = subprocess.run(
        cmd,
        cwd=cwd or ROOT,
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


def git_archive(ref: str, destination: Path) -> None:
    archive = destination / "source.tar"
    run(["git", "rev-parse", "--verify", ref], label=f"verify git ref {ref}", timeout=30)
    status = run(["git", "status", "--short"], label="local git status", timeout=30)
    if status:
        print("WARNING: working tree has uncommitted files; deploying committed git ref only.")
    with archive.open("wb") as fh:
        result = subprocess.run(
            ["git", "archive", "--format=tar", ref],
            cwd=ROOT,
            stdout=fh,
            stderr=subprocess.PIPE,
            text=False,
            timeout=120,
        )
    if result.returncode != 0:
        fail(result.stderr.decode("utf-8", errors="replace"))
    with tarfile.open(archive) as tar:
        tar.extractall(destination / "source")


def rsync_tree(source_root: Path, target: Target, name: str) -> None:
    host = os.environ.get("NX_NEXTSTEP_HOST", DEFAULT_HOST)
    user = os.environ.get("NX_NEXTSTEP_USER", DEFAULT_USER)
    source = source_root / name
    destination = f"{user}@{host}:{target.remote}/{name}/"
    run(
        [
            "sshpass",
            "-e",
            "rsync",
            "-az",
            "--delete",
            "-e",
            "ssh -o StrictHostKeyChecking=no",
            "--exclude",
            ".git",
            "--exclude",
            ".env",
            "--exclude",
            "node_modules",
            "--exclude",
            "dist",
            "--exclude",
            "backups",
            "--exclude",
            "artifacts",
            str(source) + "/",
            destination,
        ],
        label=f"sync {name} -> {target.name}",
        timeout=900,
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


def deploy_target(source_root: Path, target: Target) -> None:
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
    backup_target(target)
    rsync_tree(source_root, target, "backend")
    rsync_tree(source_root, target, "frontend")
    sudo(
        f"cd {shlex.quote(target.remote)} && docker compose build backend frontend",
        label=f"docker build {target.name}",
        timeout=1200,
    )
    sudo(
        f"cd {shlex.quote(target.remote)} && docker compose up -d backend frontend",
        label=f"restart {target.name}",
        timeout=300,
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
        f"curl -s -o /dev/null -w '%{{http_code}}' http://localhost:{target.frontend_port}/login",
        label=f"frontend login status {target.name}",
        timeout=30,
    )
    sudo(
        f"docker logs {shlex.quote(target.backend_container)} --tail=120 2>&1 "
        "| grep -iE 'fatal|panic|error|5xx' | tail -30 || true",
        label=f"recent backend error scan {target.name}",
        timeout=30,
    )
    print(f"OK {target.name}: {target.public_url}")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Deploy Nexflow to NextStep demo/aoy instances")
    parser.add_argument("--target", choices=["all", *TARGETS.keys()], default="all")
    parser.add_argument("--ref", default="HEAD", help="git ref to deploy, default HEAD")
    return parser.parse_args()


def main() -> None:
    require_tool("git")
    require_tool("sshpass")
    require_tool("rsync")
    args = parse_args()
    selected = list(TARGETS.values()) if args.target == "all" else [TARGETS[args.target]]
    with tempfile.TemporaryDirectory(prefix="nexflow-deploy-") as tmp:
        temp = Path(tmp)
        git_archive(args.ref, temp)
        source_root = temp / "source"
        for target in selected:
            deploy_target(source_root, target)
    print("\nDeploy complete.")


if __name__ == "__main__":
    main()
