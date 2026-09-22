package marketplacestock

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"nexflow/internal/services/tiktokshop"
)

// inventoryGateway is deliberately narrow. The central Gateway owns signing,
// token refresh and tenant isolation; this worker only supplies an exact shop,
// product, SKU and absolute quantity.
type inventoryGateway interface {
	SearchInventory(context.Context, tiktokshop.GatewayInventorySearchRequest) (*tiktokshop.GatewayInventorySearchResponse, error)
	UpdateInventory(context.Context, tiktokshop.GatewayInventoryUpdateRequest) (*tiktokshop.GatewayInventoryUpdateResponse, error)
}

type claimedSync struct {
	RunID, PoolID, RequestedBy string
	ConfigVersion              int64
}

// SyncWorker owns all future Marketplace writes. It is intentionally started
// even while writeEnabled is false; no queued write can exist while the gate is
// closed, so this keeps deployment behaviour deterministic.
type SyncWorker struct {
	store        *PostgresStore
	service      *Service
	tiktok       inventoryGateway
	writeEnabled bool
	logger       *zap.Logger
	owner        string
	mu           sync.Mutex
}

func NewSyncWorker(store *PostgresStore, service *Service, tiktok inventoryGateway, writeEnabled bool, logger *zap.Logger) *SyncWorker {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &SyncWorker{store: store, service: service, tiktok: tiktok, writeEnabled: writeEnabled, logger: logger, owner: "marketplace-stock-v2"}
}

func (w *SyncWorker) Start(ctx context.Context) {
	if w == nil || w.store == nil || w.service == nil {
		return
	}
	go func() {
		tick := time.NewTicker(2 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				w.ProcessOnce(ctx)
			}
		}
	}()
}

func (w *SyncWorker) ProcessOnce(ctx context.Context) {
	if w == nil || !w.writeEnabled || !w.mu.TryLock() {
		return
	}
	defer w.mu.Unlock()
	if err := w.store.enqueueDueAuto(ctx); err != nil {
		w.logger.Warn("marketplace_stock_auto_enqueue_failed", zap.Error(err))
		return
	}
	claim, err := w.store.claimSync(ctx, w.owner)
	if err != nil {
		w.logger.Warn("marketplace_stock_claim_failed", zap.Error(err))
		return
	}
	if claim == nil {
		return
	}
	w.execute(ctx, *claim)
}

// enqueueDueAuto coalesces the five-minute schedule at the database boundary.
// The partial unique index allows at most one live sync per pool even when an
// event/manual request arrives at the same time.
func (s *PostgresStore) enqueueDueAuto(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("marketplace stock store is not configured")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id::text,config_version,updated_by::text
		FROM marketplace_stock_pools
		WHERE archived_at IS NULL AND status='active' AND auto_enabled=true AND kill_switch_enabled=false AND dry_run_required=false
		  AND updated_by IS NOT NULL
		  AND (last_schedule_at IS NULL OR last_schedule_at <= NOW() - schedule_interval_seconds * INTERVAL '1 second')
		FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var poolID, userID string
		var version int64
		if err := rows.Scan(&poolID, &version, &userID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO marketplace_stock_runs(pool_id,trigger_source,run_type,status,config_version,requested_by)
			VALUES($1::uuid,'schedule','sync','queued',$2,$3::uuid)
			ON CONFLICT (pool_id,run_type) WHERE status IN ('queued','running') DO NOTHING`, poolID, version, userID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE marketplace_stock_pools SET last_schedule_at=NOW(),updated_at=NOW() WHERE id=$1::uuid`, poolID); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return tx.Commit()
}

func (w *SyncWorker) execute(ctx context.Context, claim claimedSync) {
	if strings.TrimSpace(claim.RequestedBy) == "" {
		_ = w.store.finishSync(context.Background(), claim, "failed", 0, 0, 1, "ไม่พบผู้สั่งงานสำหรับตรวจสอบสิทธิ์")
		return
	}
	preview, err := w.service.PreviewPool(ctx, claim.PoolID, PreviewRequest{ExpectedConfigVersion: claim.ConfigVersion, ConfirmAction: "PREVIEW_MARKETPLACE_STOCK_POOL"}, claim.RequestedBy)
	if err != nil {
		_ = w.store.finishSync(context.Background(), claim, "failed", 0, 0, 1, "ตรวจ SML ก่อนเขียนสต๊อกไม่สำเร็จ")
		return
	}
	overview, err := w.store.Overview(ctx)
	if err != nil {
		_ = w.store.finishSync(context.Background(), claim, "failed", 0, 0, 1, "โหลดการตั้งค่ากลุ่มสต๊อกไม่สำเร็จ")
		return
	}
	var pool *Pool
	for i := range overview.Pools {
		if overview.Pools[i].ID == claim.PoolID {
			pool = &overview.Pools[i]
			break
		}
	}
	if pool == nil || pool.ConfigVersion != claim.ConfigVersion || overview.Settings.KillSwitch || pool.KillSwitch || time.Now().UTC().After(preview.ExpiresAt) {
		_ = w.store.finishSync(context.Background(), claim, "paused", 0, 0, 1, "แผนสต๊อกหมดอายุหรือการตั้งค่าเปลี่ยนก่อนเขียน")
		return
	}
	targets := map[string]int64{}
	for _, line := range preview.Lines {
		targets[line.MemberID] = line.TargetQty
	}
	changed, blocked, failed := 0, 0, 0
	for _, member := range pool.Members {
		if !member.Enabled {
			continue
		}
		if ok, err := w.store.validateMemberLive(ctx, member); err != nil || !ok {
			blocked++
			_ = w.store.recordExecutionLine(ctx, preview.RunID, member.ID, "blocked", 0, 0, "mapping_changed", "การจับคู่สินค้า หน่วย หรือสถานะ Catalog เปลี่ยน กรุณา dry-run ใหม่")
			continue
		}
		target, ok := targets[member.ID]
		if !ok {
			blocked++
			_ = w.store.recordExecutionLine(ctx, preview.RunID, member.ID, "blocked", 0, 0, "missing_target", "ไม่พบเป้าหมายของ SKU")
			continue
		}
		if member.Source != "tiktok" {
			blocked++
			_ = w.store.recordExecutionLine(ctx, preview.RunID, member.ID, "blocked", 0, target, "legacy_shopee_writer", "SKU Shopee ยังใช้ worker เดิมและยังไม่ย้ายเข้า pool")
			continue
		}
		status, previous, actual, code, message := w.syncTikTok(ctx, member, target)
		_ = w.store.recordExecutionLine(context.Background(), preview.RunID, member.ID, status, previous, actual, code, message)
		switch status {
		case "changed":
			changed++
		case "unchanged":
		default:
			if status == "blocked" {
				blocked++
			} else {
				failed++
			}
		}
	}
	final := "success"
	message := ""
	if failed > 0 || blocked > 0 {
		final = "warning"
		message = "มี SKU ที่ยังไม่ยืนยันผล ระบบพักกลุ่มเพื่อป้องกันยอดคลาดเคลื่อน"
	}
	_ = w.store.finishPreviewExecution(context.Background(), preview.RunID, claim.PoolID, claim.RequestedBy, final, changed, blocked, failed, message)
	_ = w.store.finishSync(context.Background(), claim, final, changed, blocked, failed, message)
}

// validateMemberLive prevents a queued worker from writing a SKU after its
// Product Master mapping, conversion, or active status changed. It uses only
// the tenant database; the subsequent Gateway read validates Marketplace state.
func (s *PostgresStore) validateMemberLive(ctx context.Context, member Member) (bool, error) {
	if strings.TrimSpace(member.AliasID) == "" {
		return false, nil
	}
	var active, sales bool
	var conversion string
	var factor float64
	err := s.db.QueryRowContext(ctx, `SELECT is_active,sales_enabled,conversion_status,quantity_multiplier::float8
		FROM marketplace_item_aliases
		WHERE id=$1::uuid AND source=$2 AND account_key=$3 AND external_item_id=$4 AND external_variant_id=$5`,
		member.AliasID, member.Source, member.AccountKey, member.ExternalProductID, member.ExternalSKUID).Scan(&active, &sales, &conversion, &factor)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return active && sales && conversion == "ready" && factor == member.UnitFactor, nil
}

func (w *SyncWorker) syncTikTok(ctx context.Context, member Member, target int64) (status string, previous, actual int64, code, message string) {
	if w.tiktok == nil {
		return "failed", 0, 0, "gateway_unavailable", "ยังเชื่อม TikTok Shop Gateway ไม่ได้"
	}
	shopID := strings.TrimPrefix(strings.TrimSpace(member.AccountKey), "shop:")
	if shopID == "" {
		return "blocked", 0, 0, "invalid_shop", "ไม่พบรหัสร้าน TikTok Shop"
	}
	// TikTok accepts product IDs or SKU IDs for inventory search, never both.
	// A stock member is SKU-granular, so the exact SKU is the authoritative
	// lookup identity before both the write and the read-back.
	read, err := w.tiktok.SearchInventory(ctx, tiktokshop.GatewayInventorySearchRequest{ShopID: shopID, Search: tiktokshop.InventorySearchRequest{SKUIDs: []string{member.ExternalSKUID}}})
	if err != nil {
		return "failed", 0, 0, "inventory_read_failed", "อ่านยอด TikTok Shop ก่อนส่งไม่สำเร็จ"
	}
	warehouse, current, ok := exactTikTokInventory(read, member)
	if !ok {
		return "blocked", 0, 0, "inventory_not_ready", "SKU หรือคลัง TikTok Shop ไม่พร้อมสำหรับการเขียน"
	}
	previous = current
	if current == target {
		return "unchanged", previous, current, "", ""
	}
	write, err := w.tiktok.UpdateInventory(ctx, tiktokshop.GatewayInventoryUpdateRequest{ShopID: shopID, ProductID: member.ExternalProductID, Update: tiktokshop.UpdateInventoryRequest{SKUs: []tiktokshop.InventorySKUUpdate{{ID: member.ExternalSKUID, Inventory: []tiktokshop.WarehouseInventoryUpdate{{WarehouseID: warehouse, Quantity: target}}}}}})
	if err != nil {
		return "failed", previous, 0, "inventory_write_unknown", "TikTok Shop ยังยืนยันผลการเขียนไม่ได้ จึงไม่ลองซ้ำอัตโนมัติ"
	}
	if len(write.Errors) > 0 {
		return "failed", previous, 0, "inventory_item_rejected", "TikTok Shop ปฏิเสธ SKU นี้"
	}
	readBack, err := w.tiktok.SearchInventory(ctx, tiktokshop.GatewayInventorySearchRequest{ShopID: shopID, Search: tiktokshop.InventorySearchRequest{SKUIDs: []string{member.ExternalSKUID}}})
	if err != nil {
		return "failed", previous, 0, "read_back_failed", "อ่านผลหลังส่ง TikTok Shop ไม่สำเร็จ"
	}
	_, actual, ok = exactTikTokInventory(readBack, member)
	if !ok || actual != target {
		return "failed", previous, actual, "read_back_mismatch", "ยอด TikTok Shop หลังส่งไม่ตรงกับเป้าหมาย"
	}
	return "changed", previous, actual, "", ""
}

func exactTikTokInventory(response *tiktokshop.GatewayInventorySearchResponse, member Member) (string, int64, bool) {
	if response == nil {
		return "", 0, false
	}
	var sku *tiktokshop.InventorySearchSKU
	for _, product := range response.Inventory {
		if product.ProductID != member.ExternalProductID {
			continue
		}
		for i := range product.SKUs {
			if product.SKUs[i].ID == member.ExternalSKUID {
				if sku != nil {
					return "", 0, false
				}
				sku = &product.SKUs[i]
			}
		}
	}
	if sku == nil {
		return "", 0, false
	}
	wanted := strings.TrimSpace(member.ExternalWarehouseID)
	var selected *tiktokshop.WarehouseInventory
	for i := range sku.WarehouseInventory {
		entry := &sku.WarehouseInventory[i]
		if wanted != "" && entry.WarehouseID != wanted {
			continue
		}
		if selected != nil {
			return "", 0, false
		}
		selected = entry
	}
	if selected == nil {
		return "", 0, false
	}
	return selected.WarehouseID, selected.AvailableQuantity, true
}

func (s *PostgresStore) claimSync(ctx context.Context, owner string) (*claimedSync, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("marketplace stock store is not configured")
	}
	row := s.db.QueryRowContext(ctx, `WITH next AS (SELECT r.id FROM marketplace_stock_runs r JOIN marketplace_stock_pools p ON p.id=r.pool_id JOIN marketplace_stock_settings st ON st.singleton=true WHERE r.status='queued' AND r.run_type='sync' AND st.kill_switch_enabled=false AND p.archived_at IS NULL AND p.kill_switch_enabled=false AND p.status IN ('ready','active') ORDER BY r.created_at FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE marketplace_stock_runs r SET status='running',lease_owner=$1,started_at=NOW(),updated_at=NOW() FROM next WHERE r.id=next.id RETURNING r.id::text,r.pool_id::text,COALESCE(r.requested_by::text,''),r.config_version`, owner)
	var out claimedSync
	err := row.Scan(&out.RunID, &out.PoolID, &out.RequestedBy, &out.ConfigVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *PostgresStore) recordExecutionLine(ctx context.Context, runID, memberID, status string, previous, actual int64, code, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE marketplace_stock_run_lines SET status=$3,previous_qty=$4,actual_qty=$5,error_code=$6,error_message=$7,updated_at=NOW() WHERE run_id=$1::uuid AND member_id=$2::uuid`, runID, memberID, status, previous, actual, code, message)
	return err
}

func (s *PostgresStore) finishPreviewExecution(ctx context.Context, runID, poolID, userID, status string, changed, blocked, failed int, message string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `UPDATE marketplace_stock_runs SET status=$2,changed_count=$3,blocked_count=$4,error_count=$5,error_message=$6,finished_at=NOW(),updated_at=NOW() WHERE id=$1::uuid`, runID, status, changed, blocked, failed, message)
	if err != nil {
		return err
	}
	if status == "success" {
		_, err = tx.ExecContext(ctx, `UPDATE marketplace_stock_pools SET last_success_at=NOW(),last_error='',updated_at=NOW() WHERE id=$1::uuid`, poolID)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE marketplace_stock_pools SET status='paused',auto_enabled=false,dry_run_required=true,paused_reason='execution_needs_review',last_error=$2,updated_at=NOW() WHERE id=$1::uuid`, poolID, message)
	}
	if err != nil {
		return err
	}
	if err = insertAudit(ctx, tx, "marketplace_stock_sync_"+status, poolID, userID, map[string]any{"run_id": runID, "changed": changed, "blocked": blocked, "failed": failed}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *PostgresStore) finishSync(ctx context.Context, claim claimedSync, status string, changed, blocked, failed int, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE marketplace_stock_runs SET status=$2,changed_count=$3,blocked_count=$4,error_count=$5,error_message=$6,finished_at=NOW(),updated_at=NOW() WHERE id=$1::uuid AND status='running'`, claim.RunID, status, changed, blocked, failed, message)
	return err
}
