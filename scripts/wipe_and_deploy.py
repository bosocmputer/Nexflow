"""Wipe all bills/artifacts then redeploy. Use before fresh import testing."""
import paramiko, sys, io, os, subprocess

sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding="utf-8", errors="replace")

env = os.environ.copy()
if not env.get("BF_PASS"):
    raise SystemExit("BF_PASS is required")

# 1. Deploy first (build + restart) — so the new artifact-saving code is live.
proc = subprocess.run(
    ["python", "scripts/deploy.py"],
    cwd=os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
    env=env, capture_output=True, text=True,
    encoding="utf-8", errors="replace", timeout=600,
)
if proc.returncode != 0:
    print(proc.stdout[-1500:]); print(proc.stderr[-500:]); sys.exit(1)
print("✓ deploy ok")

# 2. Wipe bills + dependent rows + filesystem artifacts.
c = paramiko.SSHClient()
c.set_missing_host_key_policy(paramiko.AutoAddPolicy())
c.connect("192.168.2.109", username="bosscatdog", password=env["BF_PASS"],
          timeout=10, allow_agent=False, look_for_keys=False)


def sh(cmd, t=60):
    si, so, _ = c.exec_command(cmd, timeout=t)
    out = so.read().decode("utf-8", errors="replace").rstrip()
    return out


print("\n=== wiping DB rows ===")
print(sh('docker exec nexflow-postgres psql -U nexflow -d nexflow -c "'
        'BEGIN; '
        'DELETE FROM bill_artifacts; '
        'DELETE FROM bill_items; '
        'DELETE FROM audit_logs WHERE target_id IS NOT NULL; '
        'DELETE FROM bills; '
        'COMMIT;"'))

print("\n=== wiping artifact files (root-owned via container) ===")
print(sh("docker exec nexflow-backend sh -c 'rm -rf /app/artifacts/* 2>/dev/null; ls /app/artifacts'"))

print("\n=== bills + artifacts after wipe ===")
print(sh('docker exec nexflow-postgres psql -U nexflow -d nexflow -c "'
        'SELECT '
        '(SELECT COUNT(*) FROM bills)          AS bills, '
        '(SELECT COUNT(*) FROM bill_items)     AS bill_items, '
        '(SELECT COUNT(*) FROM bill_artifacts) AS bill_artifacts;"'))

c.close()
print("\n✅ done — system is empty + ready for fresh import")
