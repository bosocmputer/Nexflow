#!/usr/bin/env python3
"""Verify Nexflow branding and production release invariants.

Local checks are always available:
  python scripts/release_guard.py --local

Production checks require NX_PASS and sshpass:
  NX_PASS=xxx python scripts/release_guard.py --production
"""
from __future__ import annotations

import argparse
import os
import re
import subprocess
import sys
import urllib.error
import urllib.request
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
REMOTE = "/home/bosscatdog/billflow-henna"
HOST = "192.168.2.109"
USER = "bosscatdog"
PUBLIC_URL = "https://animal-galvanize-tameness.ngrok-free.dev/"

OLD_BRAND_RE = re.compile(r"BillFlow|Review Desk|billflow-mark|#0F7584|#111817|#B5F266|86 100%|96 58%")
SOURCE_PATHS = [
    ROOT / "frontend" / "index.html",
    ROOT / "frontend" / "src",
    ROOT / "frontend" / "public",
]


def fail(message: str) -> None:
    print(f"ERROR: {message}", file=sys.stderr)
    raise SystemExit(1)


def run(cmd: list[str], *, env: dict[str, str] | None = None, timeout: int = 60) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        cmd,
        cwd=ROOT,
        env=env,
        text=True,
        capture_output=True,
        timeout=timeout,
        encoding="utf-8",
        errors="replace",
    )


def scan_file(path: Path) -> list[str]:
    try:
        text = path.read_text(encoding="utf-8", errors="replace")
    except OSError:
        return []
    hits = []
    for line_no, line in enumerate(text.splitlines(), start=1):
        if OLD_BRAND_RE.search(line):
            hits.append(f"{path.relative_to(ROOT)}:{line_no}: {line.strip()[:180]}")
    return hits


def scan_path(path: Path) -> list[str]:
    if not path.exists():
        return []
    if path.is_file():
        return scan_file(path)
    hits: list[str] = []
    for child in path.rglob("*"):
        if child.is_dir():
            continue
        if any(part in {"node_modules", "dist", ".git"} for part in child.parts):
            continue
        if child.suffix.lower() in {".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".woff", ".woff2"}:
            continue
        hits.extend(scan_file(child))
    return hits


def assert_index_identity(html: str, label: str) -> None:
    checks = {
        "title Nexflow": "<title>Nexflow</title>" in html,
        "nexflow favicon": "nexflow-mark.svg" in html,
        "blue-white theme color": '#0F172A' in html or "#0F172A" in html,
    }
    missing = [name for name, ok in checks.items() if not ok]
    if missing:
        fail(f"{label} missing identity checks: {', '.join(missing)}")
    if OLD_BRAND_RE.search(html):
        fail(f"{label} contains old BillFlow/Henna brand text")


def check_local() -> None:
    print("== local brand guard ==")
    hits: list[str] = []
    for path in SOURCE_PATHS:
        hits.extend(scan_path(path))
    if hits:
        print("\n".join(hits[:80]), file=sys.stderr)
        fail(f"found {len(hits)} old-brand hits in active frontend source")

    index = ROOT / "frontend" / "index.html"
    assert_index_identity(index.read_text(encoding="utf-8", errors="replace"), "frontend/index.html")

    dist = ROOT / "frontend" / "dist"
    if dist.exists():
        dist_hits = scan_path(dist)
        if dist_hits:
            print("\n".join(dist_hits[:80]), file=sys.stderr)
            fail(f"found {len(dist_hits)} old-brand hits in frontend/dist")
    print("OK local source is Nexflow")


def ssh_env() -> dict[str, str]:
    password = os.environ.get("NX_PASS")
    if not password:
        fail("NX_PASS is required for production release guard")
    env = os.environ.copy()
    env["SSHPASS"] = password
    return env


def ssh(cmd: str, *, timeout: int = 60) -> str:
    result = run(
        [
            "sshpass",
            "-e",
            "ssh",
            "-o",
            "StrictHostKeyChecking=no",
            f"{USER}@{HOST}",
            cmd,
        ],
        env=ssh_env(),
        timeout=timeout,
    )
    if result.returncode != 0:
        fail(f"ssh command failed: {cmd}\n{result.stderr or result.stdout}")
    return result.stdout.strip()


def check_remote_grep(label: str, cmd: str) -> None:
    output = ssh(cmd, timeout=120)
    if output.strip():
        print(output, file=sys.stderr)
        fail(f"{label} contains old BillFlow/Henna brand remnants")


def fetch_public_index() -> str:
    result = run(
        [
            "curl",
            "-sS",
            "-H",
            "ngrok-skip-browser-warning: true",
            PUBLIC_URL,
        ],
        timeout=30,
    )
    if result.returncode == 0:
        return result.stdout
    try:
        req = urllib.request.Request(PUBLIC_URL, headers={"ngrok-skip-browser-warning": "true"})
        with urllib.request.urlopen(req, timeout=30) as response:
            return response.read().decode("utf-8", errors="replace")
    except (OSError, urllib.error.URLError) as exc:
        fail(f"ngrok frontend fetch failed: {result.stderr or result.stdout}; urllib fallback: {exc}")


def check_production() -> None:
    print("== production brand guard ==")
    pattern = r"BillFlow|Review Desk|billflow-mark|#0F7584|#111817|#B5F266|86 100%|96 58%"
    check_remote_grep(
        "server frontend source",
        f"grep -RInE '{pattern}' {REMOTE}/frontend/index.html {REMOTE}/frontend/src {REMOTE}/frontend/public 2>/dev/null || true",
    )
    check_remote_grep(
        "frontend container dist",
        f"docker exec nexflow-frontend sh -lc \"grep -RInE '{pattern}' /usr/share/nginx/html 2>/dev/null || true\"",
    )

    health = ssh("curl -s -m 5 http://localhost:8110/health", timeout=15)
    if '"status":"ok"' not in health:
        fail(f"backend health is not ok: {health}")

    assert_index_identity(fetch_public_index(), "ngrok index")
    print("OK production is Nexflow")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--local", action="store_true", help="check local source and dist")
    parser.add_argument("--production", action="store_true", help="check server source, container dist, health, and ngrok index")
    args = parser.parse_args()
    if not args.local and not args.production:
        args.local = True
    if args.local:
        check_local()
    if args.production:
        check_production()


if __name__ == "__main__":
    main()
