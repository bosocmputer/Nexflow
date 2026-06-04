#!/usr/bin/env python3
"""Deploy Nexflow to the production server with brand/release guards.

Auth: NX_PASS env var. Run from project root:
  NX_PASS=xxx python scripts/deploy.py
"""
from __future__ import annotations

import os
import shutil
import subprocess
import sys
import time
from pathlib import Path


HOST = "192.168.2.109"
USER = "bosscatdog"
REMOTE = "/home/bosscatdog/billflow-henna"
BACKUP_DIR = "/home/bosscatdog/nexflow-backups"
ROOT = Path(__file__).resolve().parents[1]
PUBLIC_URL = "https://animal-galvanize-tameness.ngrok-free.dev/"


def fail(message: str) -> None:
    print(f"ERROR: {message}", file=sys.stderr)
    raise SystemExit(1)


def require_tool(name: str) -> None:
    if shutil.which(name) is None:
        fail(f"{name} is required for deploy")


def env_with_password() -> dict[str, str]:
    password = os.environ.get("NX_PASS")
    if not password:
        fail("NX_PASS env var required")
    env = os.environ.copy()
    env["SSHPASS"] = password
    return env


def run(cmd: list[str], *, label: str | None = None, timeout: int = 900) -> str:
    if label:
        print(f"\n========== {label} ==========")
    print("$ " + " ".join(cmd))
    result = subprocess.run(
        cmd,
        cwd=ROOT,
        env=env_with_password(),
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
    return run(
        [
            "sshpass",
            "-e",
            "ssh",
            "-o",
            "StrictHostKeyChecking=no",
            f"{USER}@{HOST}",
            command,
        ],
        label=label,
        timeout=timeout,
    )


def rsync_tree(name: str) -> None:
    source = ROOT / name
    if not source.exists():
        fail(f"missing local tree: {name}")
    destination = f"{USER}@{HOST}:{REMOTE}/{name}/"
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
        label=f"sync {name}",
        timeout=900,
    )


def rsync_file(name: str) -> None:
    source = ROOT / name
    if not source.exists():
        fail(f"missing local file: {name}")
    destination = f"{USER}@{HOST}:{REMOTE}/{name}"
    run(
        [
            "sshpass",
            "-e",
            "rsync",
            "-az",
            "-e",
            "ssh -o StrictHostKeyChecking=no",
            str(source),
            destination,
        ],
        label=f"sync {name}",
        timeout=120,
    )


def main() -> None:
    require_tool("sshpass")
    require_tool("rsync")

    run([sys.executable, "scripts/release_guard.py", "--local"], label="local Nexflow brand guard", timeout=120)

    ssh(f"mkdir -p {REMOTE}/frontend {REMOTE}/backend {REMOTE}/artifacts {BACKUP_DIR}", label="ensure remote dirs")
    ssh(
        f"cd {REMOTE} && "
        "echo 'current containers:' && docker ps --format '{{.Names}} {{.Status}} {{.Ports}}' | grep -E 'nexflow|sml-api-bybos' || true && "
        "echo 'health:' && curl -s -m 5 http://localhost:8110/health || true",
        label="record current state",
        timeout=60,
    )
    ssh(
        f"cd {REMOTE} && "
        "ts=$(date +%Y%m%d-%H%M%S) && "
        f"[ -f .env ] && cp -p .env {BACKUP_DIR}/.env.bak.$ts || true && "
        f"docker exec nexflow-postgres pg_dump -U nexflow -d nexflow | gzip > {BACKUP_DIR}/pre-deploy-$ts.sql.gz && "
        f"ls -lh {BACKUP_DIR}/pre-deploy-$ts.sql.gz",
        label="backup env + database",
        timeout=300,
    )

    rsync_tree("frontend")
    rsync_tree("backend")
    rsync_file("docker-compose.yml")

    ssh(f"cd {REMOTE} && docker compose build backend frontend", label="docker build", timeout=1200)
    ssh(f"cd {REMOTE} && docker compose up -d backend frontend", label="restart app containers", timeout=300)

    print("\n... waiting 8s ...")
    time.sleep(8)

    health = ssh("curl -s -m 5 http://localhost:8110/health", label="backend health", timeout=30)
    if '"status":"ok"' not in health:
        ssh("docker logs nexflow-backend --tail=80", label="backend logs after failed health", timeout=30)
        fail("backend health check failed")

    ssh("curl -s -o /dev/null -w '%{http_code}' http://localhost:3030/", label="frontend local status", timeout=30)
    run([sys.executable, "scripts/release_guard.py", "--production"], label="production Nexflow brand guard", timeout=180)

    ssh(
        "docker logs nexflow-backend --tail=120 2>&1 | grep -iE 'fatal|panic|error|5xx' | tail -30 || true",
        label="recent backend error scan",
        timeout=30,
    )
    print(f"\nOK deploy complete: {PUBLIC_URL}")


if __name__ == "__main__":
    main()
