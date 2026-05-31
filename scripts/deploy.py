"""Deploy committed Nexflow code to server (sync + restart + verify).

Auth: NX_PASS env var (no hardcoded password). Run from project root:
  NX_PASS=xxx python scripts/deploy.py
"""
import paramiko, sys, io, os, time, tarfile

sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding="utf-8", errors="replace")

HOST = "192.168.2.109"
USER = "bosscatdog"
PASS = os.environ.get("NX_PASS")
ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
REMOTE = "/home/bosscatdog/billflow-henna"

if not PASS:
    print("ERROR: NX_PASS env var required", file=sys.stderr)
    sys.exit(1)

# Files to sync (matches the commit)
# Sync the entire frontend + backend trees (excludes node_modules / dist /
# .git via tar_filter below). Triggers Docker rebuild for whichever side
# changed since the last build.
FILES = ["frontend", "backend"]


def run(client, cmd, label=None, timeout=900):
    if label:
        print(f"\n========== {label} ==========")
    print(f"$ {cmd}")
    si, so, se = client.exec_command(cmd, timeout=timeout, get_pty=True)
    out = so.read().decode("utf-8", errors="replace").rstrip()
    err = se.read().decode("utf-8", errors="replace").rstrip()
    if out:
        print(out)
    if err:
        print(f"[stderr] {err}")
    return so.channel.recv_exit_status()


def main():
    print(f"Connecting to {USER}@{HOST}...")
    c = paramiko.SSHClient()
    c.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    c.connect(HOST, username=USER, password=PASS, timeout=10,
              allow_agent=False, look_for_keys=False)
    print("✓ connected")

    # Build tar of changed files in memory.
    # Exclude heavyweight dirs that the Docker build can regenerate.
    SKIP = {"node_modules", "dist", ".git", "backups", "artifacts", "__pycache__"}

    def tar_filter(info):
        parts = set(info.name.replace("\\", "/").split("/"))
        return None if parts & SKIP else info

    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode="w:gz") as tar:
        for f in FILES:
            local = os.path.join(ROOT, f)
            if not os.path.exists(local):
                print(f"⚠️  missing: {f}")
                continue
            tar.add(local, arcname=f.replace("\\", "/"), filter=tar_filter)
    buf.seek(0)
    print(f"\ntar: {len(buf.getvalue()):,} bytes")

    # Upload + extract
    sftp = c.open_sftp()
    sftp.putfo(buf, "/tmp/nexflow-deploy.tar.gz")
    sftp.close()
    print("✓ uploaded")

    # Remove legacy CSS files that have been deleted locally — the tar only
    # adds files, never removes. Matches every page+component .css that the
    # redesign retired so orphans don't get bundled by Vite.
    LEGACY_FILES = [
        # Old per-page/per-component CSS files retired in Phase 3
        "frontend/src/components/Layout.css",
        "frontend/src/components/StatsCard.css",
        "frontend/src/components/InsightCard.css",
        "frontend/src/components/LearningProgress.css",
        "frontend/src/components/BillStatusBadge.css",
        "frontend/src/components/BillTable.css",
        "frontend/src/pages/Login.css",
        "frontend/src/pages/Dashboard.css",
        "frontend/src/pages/Bills.css",
        "frontend/src/pages/Mappings.css",
        "frontend/src/pages/Logs.css",
        "frontend/src/pages/Import.css",
        "frontend/src/pages/ShopeeImport.css",
        "frontend/src/pages/Settings.css",
        "frontend/src/pages/CatalogSettings.css",
        # Old monolithic BillDetail.tsx + .css — replaced by BillDetail/ dir.
        # Without removing the .tsx, the stale file keeps importing
        # uninstalled deps (react-hot-toast) and breaks the Docker build.
        "frontend/src/pages/BillDetail.tsx",
        "frontend/src/pages/BillDetail.css",
        # Phase 1 of multi-account IMAP deleted the old singleton poller.
        "backend/internal/jobs/email_poller.go",
    ]
    rm_cmd = " && ".join(f"rm -f {REMOTE}/{p}" for p in LEGACY_FILES)
    run(c, rm_cmd, label="remove orphan legacy files")

    run(c, f"cd {REMOTE} && tar -xzf /tmp/nexflow-deploy.tar.gz && rm /tmp/nexflow-deploy.tar.gz",
        label="extract")
    run(c, f"mkdir -p {REMOTE}/backups {REMOTE}/artifacts", label="ensure backups + artifacts dirs")

    # Rebuild only what's needed based on file paths
    has_backend_change = any(f == "backend" or f.startswith("backend/") for f in FILES)
    has_frontend_change = any(f == "frontend" or f.startswith("frontend/") for f in FILES)
    services = []
    if has_backend_change: services.append("backend")
    if has_frontend_change: services.append("frontend")
    if not services:
        services = ["backend", "frontend"]

    rc = run(c, f"cd {REMOTE} && docker compose build {' '.join(services)}",
             timeout=900, label=f"build {' '.join(services)}")
    if rc != 0:
        print(f"❌ build failed (exit {rc})")
        c.close(); sys.exit(1)

    run(c, f"cd {REMOTE} && docker compose up -d {' '.join(services)}", label="restart")

    print("\n... waiting 8s ...")
    time.sleep(8)

    # Verify
    run(c, "docker ps --format 'table {{.Names}}\t{{.Status}}' | grep -E 'nexflow|NAME'", label="containers")

    # Health check — must return non-empty body containing "ok", else fail loud.
    si, so, se = c.exec_command("curl -s -m 5 http://localhost:8090/health", timeout=15)
    health = so.read().decode("utf-8", errors="replace").strip()
    print(f"\n========== health ==========\n{health!r}")
    if '"status":"ok"' not in health:
        print("❌ health check failed — backend may be crashing. Recent fatal logs:")
        si, so, se = c.exec_command(
            "docker logs nexflow-backend 2>&1 | grep -i 'fatal\\|panic\\|error.:\\|migration' | tail -20",
            timeout=15,
        )
        print(so.read().decode("utf-8", errors="replace"))
        c.close()
        sys.exit(2)

    run(c, "docker logs nexflow-backend 2>&1 | grep -i 'migration applied' | tail -10", label="migrations applied")

    c.close()
    print("\n✅ deploy complete")


if __name__ == "__main__":
    main()
