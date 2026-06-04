#!/usr/bin/env bash
set -u

BACKUP_DIR="${BACKUP_DIR:-/home/bosscatdog/nexflow-backups}"
INTERVAL_SECONDS="${INTERVAL_SECONDS:-300}"
MAX_ITERATIONS="${MAX_ITERATIONS:-288}"
RUN_ID="${RUN_ID:-$(date +%Y%m%d-%H%M%S)}"
LOG_FILE="$BACKUP_DIR/shopee-realtime-watch-$RUN_ID.log"
START_BASELINE="$BACKUP_DIR/shopee-realtime-watch-$RUN_ID.start-baseline"
OBSERVED_FILE="$BACKUP_DIR/shopee-realtime-watch-$RUN_ID.observed"
CURRENT_FILE="$BACKUP_DIR/shopee-realtime-watch-$RUN_ID.current"
NEW_FILE="$BACKUP_DIR/shopee-realtime-watch-$RUN_ID.new"
PID_FILE="$BACKUP_DIR/shopee-realtime-watch.pid"

mkdir -p "$BACKUP_DIR"

db_query() {
  docker exec nexflow-postgres psql -U nexflow -d nexflow -Atq -c "$1" 2>&1
}

log_line() {
  printf "%s\n" "$*" >> "$LOG_FILE"
}

log_section() {
  printf "\n[%s] %s\n" "$(date -Is)" "$*" >> "$LOG_FILE"
}

cleanup() {
  rm -f "$CURRENT_FILE" "$NEW_FILE"
}
trap cleanup EXIT

echo $$ > "$PID_FILE"

log_section "START passive Shopee Realtime monitor run_id=$RUN_ID interval=${INTERVAL_SECONDS}s max_iterations=$MAX_ITERATIONS"
log_line "scope|read_only|no_save_erp|no_ship_order|no_import_confirm|no_settings_save"
log_line "log_file|$LOG_FILE"
log_line "public_url|https://animal-galvanize-tameness.ngrok-free.dev"
log_line "callback_url|https://animal-galvanize-tameness.ngrok-free.dev/webhook/shopee"

# Baseline order identity only. Do not log buyer names, phone numbers, or addresses.
db_query "SELECT shop_id::text || '|' || order_sn FROM shopee_order_snapshots ORDER BY 1;" | sort -u > "$START_BASELINE"
cp "$START_BASELINE" "$OBSERVED_FILE"
log_line "baseline_snapshot_orders|$(wc -l < "$START_BASELINE" | tr -d ' ')"
log_line "baseline_push_events|$(db_query "SELECT COUNT(*) FROM shopee_push_events;")"
log_line "baseline_notifications|$(db_query "SELECT COUNT(*) FROM notifications;")"
log_line "baseline_health|$(curl -s -m 5 http://localhost:8110/health || true)"

iteration=0
while [ "$iteration" -lt "$MAX_ITERATIONS" ]; do
  iteration=$((iteration + 1))
  log_section "tick iteration=$iteration"

  log_line "health|$(curl -s -m 5 http://localhost:8110/health || true)"
  log_line "snapshots_total|$(db_query "SELECT COUNT(*) FROM shopee_order_snapshots;")"
  db_query "SELECT 'shopee_status|' || order_status || '|' || COUNT(*) FROM shopee_order_snapshots GROUP BY order_status ORDER BY order_status;" >> "$LOG_FILE"
  db_query "SELECT 'erp_status|' || erp_status || '|' || COUNT(*) FROM shopee_order_snapshots GROUP BY erp_status ORDER BY erp_status;" >> "$LOG_FILE"
  log_line "push_events_total|$(db_query "SELECT COUNT(*) FROM shopee_push_events;")"
  db_query "SELECT 'push_latest|' || received_at::text || '|' || shop_id::text || '|' || COALESCE(order_sn,'') || '|' || push_code::text || '|' || push_name || '|' || COALESCE(event_status,'') || '|' || processing_status FROM shopee_push_events ORDER BY received_at DESC LIMIT 5;" >> "$LOG_FILE"
  db_query "SELECT 'reconcile_jobs|' || status || '|' || COUNT(*) FROM shopee_reconcile_jobs GROUP BY status ORDER BY status;" >> "$LOG_FILE"
  log_line "notifications_total|$(db_query "SELECT COUNT(*) FROM notifications;")"
  db_query "SELECT 'notification_latest|' || created_at::text || '|' || severity || '|' || source || '|' || entity_type || '|' || entity_id || '|' || title FROM notifications ORDER BY created_at DESC LIMIT 5;" >> "$LOG_FILE"

  db_query "SELECT shop_id::text || '|' || order_sn FROM shopee_order_snapshots ORDER BY 1;" | sort -u > "$CURRENT_FILE"
  comm -13 "$OBSERVED_FILE" "$CURRENT_FILE" > "$NEW_FILE" || true
  new_count=$(wc -l < "$NEW_FILE" | tr -d ' ')
  log_line "new_orders_since_last_tick|$new_count"
  if [ "$new_count" -gt 0 ]; then
    while IFS='|' read -r shop_id order_sn; do
      [ -n "${shop_id:-}" ] || continue
      safe_order_sn=$(printf "%s" "$order_sn" | sed "s/'/''/g")
      db_query "SELECT 'new_order|' || shop_id::text || '|' || order_sn || '|' || order_status || '|' || erp_status || '|' || total_amount::text || '|' || last_synced_at::text || '|' || COALESCE(last_order_update_at::text,'') FROM shopee_order_snapshots WHERE shop_id = $shop_id AND order_sn = '$safe_order_sn' LIMIT 1;" >> "$LOG_FILE"
    done < "$NEW_FILE"
    cat "$NEW_FILE" >> "$OBSERVED_FILE"
    sort -u "$OBSERVED_FILE" -o "$OBSERVED_FILE"
  fi

  recent_backend=$(docker logs nexflow-backend --since 10m 2>&1 | grep -iE 'fatal|panic|error|5xx|shopee_realtime|shopee realtime|/webhook/shopee|notification' | tail -80 || true)
  if [ -n "$recent_backend" ]; then
    log_line "backend_log_scan_begin"
    printf "%s\n" "$recent_backend" >> "$LOG_FILE"
    log_line "backend_log_scan_end"
  else
    log_line "backend_log_scan|clean"
  fi

  sleep "$INTERVAL_SECONDS"
done

log_section "END passive Shopee Realtime monitor run_id=$RUN_ID"
rm -f "$PID_FILE"
