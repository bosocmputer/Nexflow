#!/usr/bin/env python3
"""TEST-ONLY destructive helper.

This script deploys, then wipes bills/artifacts from the configured server.
Do not use for production releases.

Required:
  NX_PASS=xxx NX_WIPE_CONFIRM=WIPE_NEXFLOW_TEST_DATA python scripts/wipe_and_deploy.py
"""
from __future__ import annotations

import os
import subprocess
import sys
from pathlib import Path

import paramiko


ROOT = Path(__file__).resolve().parents[1]
HOST = "192.168.2.109"
USER = "bosscatdog"


def fail(message: str) -> None:
    print(f"ERROR: {message}", file=sys.stderr)
    raise SystemExit(1)


password = os.environ.get("NX_PASS")
if not password:
    fail("NX_PASS is required")
if os.environ.get("NX_WIPE_CONFIRM") != "WIPE_NEXFLOW_TEST_DATA":
    fail("Refusing to wipe data. Set NX_WIPE_CONFIRM=WIPE_NEXFLOW_TEST_DATA for test-only runs.")

proc = subprocess.run(
    [sys.executable, "scripts/deploy.py"],
    cwd=ROOT,
    env=os.environ.copy(),
    capture_output=True,
    text=True,
    encoding="utf-8",
    errors="replace",
    timeout=1800,
)
if proc.returncode != 0:
    print(proc.stdout[-2000:])
    print(proc.stderr[-1000:], file=sys.stderr)
    fail("deploy failed; data was not wiped")
print("deploy ok")

client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
client.connect(HOST, username=USER, password=password, timeout=10, allow_agent=False, look_for_keys=False)


def sh(cmd: str, timeout: int = 60) -> str:
    _, stdout, stderr = client.exec_command(cmd, timeout=timeout)
    out = stdout.read().decode("utf-8", errors="replace").rstrip()
    err = stderr.read().decode("utf-8", errors="replace").rstrip()
    if err:
        print(err, file=sys.stderr)
    return out


print("\n=== wiping DB rows ===")
print(sh('docker exec nexflow-postgres psql -U nexflow -d nexflow -c "'
         'BEGIN; '
         'DELETE FROM bill_artifacts; '
         'DELETE FROM bill_items; '
         'DELETE FROM audit_logs WHERE target_id IS NOT NULL; '
         'DELETE FROM bills; '
         'COMMIT;"'))

print("\n=== wiping artifact files ===")
print(sh("docker exec nexflow-backend sh -c 'rm -rf /app/artifacts/* 2>/dev/null; ls /app/artifacts'"))

print("\n=== bills + artifacts after wipe ===")
print(sh('docker exec nexflow-postgres psql -U nexflow -d nexflow -c "'
         'SELECT '
         '(SELECT COUNT(*) FROM bills)          AS bills, '
         '(SELECT COUNT(*) FROM bill_items)     AS bill_items, '
         '(SELECT COUNT(*) FROM bill_artifacts) AS bill_artifacts;"'))

client.close()
print("\nTEST-ONLY wipe complete")
