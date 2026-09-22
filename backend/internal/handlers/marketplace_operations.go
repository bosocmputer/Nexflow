package handlers

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/config"
	"nexflow/internal/models"
)

const (
	marketplaceOperationsDefaultLimit    = 20
	marketplaceOperationsMaxLimit        = 50
	marketplaceOperationsMaxSearchLength = 100
)

type marketplaceOperationsSource string

const (
	marketplaceSourceAll    marketplaceOperationsSource = "all"
	marketplaceSourceShopee marketplaceOperationsSource = "shopee"
	marketplaceSourceTikTok marketplaceOperationsSource = "tiktok"
)

type marketplaceOperationsView string

const (
	marketplaceOperationsViewWork      marketplaceOperationsView = "work"
	marketplaceOperationsViewAll       marketplaceOperationsView = "all"
	marketplaceOperationsViewCancelled marketplaceOperationsView = "cancelled"
)

type marketplaceWorkState string

const (
	marketplaceWorkStateNeedsMapping         marketplaceWorkState = "needs_mapping"
	marketplaceWorkStateNeedsReview          marketplaceWorkState = "needs_review"
	marketplaceWorkStateReadyToCreate        marketplaceWorkState = "ready_to_create"
	marketplaceWorkStateReadyToSend          marketplaceWorkState = "ready_to_send"
	marketplaceWorkStateSendFailed           marketplaceWorkState = "send_failed"
	marketplaceWorkStateCancelDocumentNeeded marketplaceWorkState = "cancel_document_needed"
	marketplaceWorkStateCancelFailed         marketplaceWorkState = "cancel_failed"
	marketplaceWorkStateComplete             marketplaceWorkState = "complete"
)

type marketplaceOperationsQueryReader interface {
	Get(string) string
}

type marketplaceOperationsCursor struct {
	UpdatedAt time.Time                   `json:"updated_at"`
	Source    marketplaceOperationsSource `json:"source"`
	ShopID    string                      `json:"shop_id"`
	OrderID   string                      `json:"order_id"`
}

type marketplaceOperationsQuery struct {
	Source marketplaceOperationsSource
	View   marketplaceOperationsView
	ShopID string
	Status string
	Search string
	From   *time.Time
	To     *time.Time
	Limit  int
	Cursor *marketplaceOperationsCursor
}

type marketplaceOperationsRow struct {
	Source             marketplaceOperationsSource `json:"source"`
	ShopID             string                      `json:"shop_id"`
	ShopName           string                      `json:"shop_name"`
	OrderID            string                      `json:"order_id"`
	MarketplaceStatus  string                      `json:"marketplace_status"`
	ERPStatus          string                      `json:"erp_status,omitempty"`
	BillID             string                      `json:"bill_id,omitempty"`
	BillStatus         string                      `json:"bill_status,omitempty"`
	SMLDocNo           string                      `json:"sml_doc_no,omitempty"`
	CancelSMLDocNo     string                      `json:"cancel_sml_doc_no,omitempty"`
	CancellationStatus string                      `json:"cancellation_status,omitempty"`
	AutoSMLStatus      string                      `json:"auto_sml_status,omitempty"`
	Currency           string                      `json:"currency,omitempty"`
	TotalAmount        string                      `json:"total_amount"`
	ItemCount          int                         `json:"item_count"`
	SKUCount           int                         `json:"sku_count"`
	LastUpdatedAt      time.Time                   `json:"last_updated_at"`
	LastSyncedAt       time.Time                   `json:"last_synced_at"`
	WorkState          marketplaceWorkState        `json:"work_state"`
	AvailableActions   []string                    `json:"available_actions"`
	DocumentPath       string                      `json:"document_path,omitempty"`
}

type MarketplaceOperationsHandler struct {
	db     *sql.DB
	cfg    *config.Config
	users  marketplaceOperationsUserReader
	logger *zap.Logger
}

// marketplaceOperationsUserReader makes the combined queue enforce the same
// current per-user permissions as the menu instead of trusting stale browser
// state or the role embedded in a JWT.
type marketplaceOperationsUserReader interface {
	FindByID(string) (*models.User, error)
}

func NewMarketplaceOperationsHandler(db *sql.DB, cfg *config.Config, users marketplaceOperationsUserReader, logger *zap.Logger) *MarketplaceOperationsHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &MarketplaceOperationsHandler{db: db, cfg: cfg, users: users, logger: logger}
}

func (h *MarketplaceOperationsHandler) enabledSources() []marketplaceOperationsSource {
	if h == nil || h.cfg == nil || !h.cfg.MarketplaceOperationsEnabled {
		return nil
	}
	sources := make([]marketplaceOperationsSource, 0, 2)
	if h.cfg.ShopeeRealtimeOpsEnabled {
		sources = append(sources, marketplaceSourceShopee)
	}
	if h.cfg.TikTokShopOpenAPIEnabled {
		sources = append(sources, marketplaceSourceTikTok)
	}
	return sources
}

func (h *MarketplaceOperationsHandler) enabled(c *gin.Context) bool {
	if h == nil || h.db == nil || h.cfg == nil || !h.cfg.MarketplaceOperationsEnabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "Marketplace Operations ยังไม่เปิดใช้งาน"})
		return false
	}
	return true
}

func (h *MarketplaceOperationsHandler) List(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	startedAt := time.Now()
	query, err := parseMarketplaceOperationsQuery(c.Request.URL.Query())
	if err != nil {
		h.logger.Warn("marketplace_operations_query_rejected")
		c.JSON(http.StatusBadRequest, gin.H{"error": "ตัวกรอง Marketplace Operations ไม่ถูกต้อง"})
		return
	}
	sources, ok := h.authorizedSources(c)
	if !ok {
		return
	}
	if query.Source != marketplaceSourceAll {
		sources = filterMarketplaceSource(sources, query.Source)
		if len(sources) == 0 {
			c.JSON(http.StatusForbidden, gin.H{"error": "ไม่มีสิทธิ์ดูคำสั่งซื้อ Marketplace ช่องทางนี้"})
			return
		}
	}
	rows := make([]marketplaceOperationsRow, 0, query.Limit+1)
	partial := make([]gin.H, 0, len(sources))
	for _, source := range sources {
		var sourceRows []marketplaceOperationsRow
		switch source {
		case marketplaceSourceShopee:
			sourceRows, err = h.listShopee(c.Request.Context(), query)
		case marketplaceSourceTikTok:
			sourceRows, err = h.listTikTok(c.Request.Context(), query)
		}
		if err != nil {
			h.logger.Warn("marketplace_operations_source_list_failed", zap.String("source", string(source)), zap.Error(err))
			partial = append(partial, gin.H{"source": source, "message": "โหลดข้อมูลช่องทางนี้ไม่สำเร็จ"})
			continue
		}
		rows = append(rows, sourceRows...)
	}
	sort.Slice(rows, func(i, j int) bool {
		left, right := rows[i], rows[j]
		if !left.LastUpdatedAt.Equal(right.LastUpdatedAt) {
			return left.LastUpdatedAt.After(right.LastUpdatedAt)
		}
		if left.Source != right.Source {
			return left.Source < right.Source
		}
		if left.ShopID != right.ShopID {
			return left.ShopID < right.ShopID
		}
		return left.OrderID < right.OrderID
	})
	hasMore := len(rows) > query.Limit
	if hasMore {
		rows = rows[:query.Limit]
	}
	nextCursor := ""
	if hasMore && len(rows) > 0 {
		nextCursor = encodeMarketplaceOperationsCursor(rows[len(rows)-1])
	}
	h.logger.Info("marketplace_operations_listed",
		zap.String("channel", string(query.Source)),
		zap.String("view", string(query.View)),
		zap.Int("row_count", len(rows)),
		zap.Int("partial_sources", len(partial)),
		zap.Int64("latency_ms", time.Since(startedAt).Milliseconds()),
	)
	c.JSON(http.StatusOK, gin.H{
		"data":           rows,
		"next_cursor":    nextCursor,
		"has_more":       hasMore,
		"partial_errors": partial,
		"channels":       sources,
	})
}

func (h *MarketplaceOperationsHandler) Summary(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	startedAt := time.Now()
	sources, ok := h.authorizedSources(c)
	if !ok {
		return
	}
	type sourceSummary struct {
		Source       marketplaceOperationsSource `json:"source"`
		Enabled      bool                        `json:"enabled"`
		Total        int                         `json:"total"`
		LastSyncedAt *time.Time                  `json:"last_synced_at,omitempty"`
		Error        string                      `json:"error,omitempty"`
	}
	type shopSummary struct {
		Source marketplaceOperationsSource `json:"source"`
		ShopID string                      `json:"shop_id"`
		Name   string                      `json:"name"`
	}
	data := make([]sourceSummary, 0, len(sources))
	shops := make([]shopSummary, 0, 8)
	for _, source := range sources {
		var total int
		var latest sql.NullTime
		var err error
		switch source {
		case marketplaceSourceShopee:
			err = h.db.QueryRowContext(c.Request.Context(), `SELECT COUNT(*), MAX(last_synced_at) FROM shopee_order_snapshots`).Scan(&total, &latest)
		case marketplaceSourceTikTok:
			err = h.db.QueryRowContext(c.Request.Context(), `SELECT COUNT(*), MAX(s.last_synced_at) FROM tiktok_shop_order_snapshots s JOIN tiktok_shop_connections c ON c.shop_id = s.shop_id AND c.disabled_at IS NULL`).Scan(&total, &latest)
		}
		entry := sourceSummary{Source: source, Enabled: true, Total: total}
		if err != nil {
			h.logger.Warn("marketplace_operations_source_summary_failed", zap.String("source", string(source)), zap.Error(err))
			entry.Error = "โหลดสถานะช่องทางนี้ไม่สำเร็จ"
		} else if latest.Valid {
			value := latest.Time.UTC()
			entry.LastSyncedAt = &value
		}
		data = append(data, entry)
		if err != nil {
			continue
		}
		var shopRows *sql.Rows
		switch source {
		case marketplaceSourceShopee:
			shopRows, err = h.db.QueryContext(c.Request.Context(), `SELECT shop_id::text, MAX(shop_label) FROM shopee_order_snapshots GROUP BY shop_id ORDER BY MAX(shop_label), shop_id`)
		case marketplaceSourceTikTok:
			shopRows, err = h.db.QueryContext(c.Request.Context(), `SELECT shop_id, shop_name FROM tiktok_shop_connections WHERE disabled_at IS NULL ORDER BY shop_name, shop_id`)
		}
		if err != nil {
			h.logger.Warn("marketplace_operations_shop_summary_failed", zap.String("source", string(source)), zap.Error(err))
			continue
		}
		for shopRows.Next() {
			var shop shopSummary
			shop.Source = source
			if err := shopRows.Scan(&shop.ShopID, &shop.Name); err != nil {
				shopRows.Close()
				h.logger.Warn("marketplace_operations_shop_summary_scan_failed", zap.String("source", string(source)), zap.Error(err))
				break
			}
			shops = append(shops, shop)
		}
		shopRows.Close()
	}
	h.logger.Info("marketplace_operations_summary_listed",
		zap.Int("source_count", len(data)),
		zap.Int("shop_count", len(shops)),
		zap.Int64("latency_ms", time.Since(startedAt).Milliseconds()),
	)
	c.JSON(http.StatusOK, gin.H{"data": data, "shops": shops, "checked_at": time.Now().UTC()})
}

func (h *MarketplaceOperationsHandler) authorizedSources(c *gin.Context) ([]marketplaceOperationsSource, bool) {
	if h == nil || h.users == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "ยังตรวจสอบสิทธิ์คำสั่งซื้อ Marketplace ไม่ได้"})
		return nil, false
	}
	user, err := h.users.FindByID(c.GetString("user_id"))
	if err != nil || user == nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "ไม่มีสิทธิ์ดูคำสั่งซื้อ Marketplace"})
		return nil, false
	}
	if !marketplaceOperationsPermissionAllowed(user, "marketplace_operations") {
		c.JSON(http.StatusForbidden, gin.H{"error": "ไม่มีสิทธิ์เข้าเมนูคำสั่งซื้อ Marketplace"})
		return nil, false
	}

	sources := make([]marketplaceOperationsSource, 0, 2)
	for _, source := range h.enabledSources() {
		if marketplaceOperationsPermissionAllowed(user, marketplaceOperationsSourceMenuKey(source)) {
			sources = append(sources, source)
		}
	}
	if len(sources) == 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": "ไม่มีสิทธิ์ดูคำสั่งซื้อของช่องทางที่เชื่อมต่อ"})
		return nil, false
	}
	return sources, true
}

func marketplaceOperationsSourceMenuKey(source marketplaceOperationsSource) string {
	switch source {
	case marketplaceSourceShopee:
		return "shopee_operations"
	case marketplaceSourceTikTok:
		return "tiktok_shop_operations"
	default:
		return ""
	}
}

func marketplaceOperationsPermissionAllowed(user *models.User, menuKey string) bool {
	if user == nil || menuKey == "" {
		return false
	}
	for _, permission := range user.MenuPermissions {
		if permission.MenuKey == menuKey {
			return permission.CanView
		}
	}
	return false
}

func filterMarketplaceSource(sources []marketplaceOperationsSource, wanted marketplaceOperationsSource) []marketplaceOperationsSource {
	for _, source := range sources {
		if source == wanted {
			return []marketplaceOperationsSource{source}
		}
	}
	return nil
}

func (h *MarketplaceOperationsHandler) listShopee(ctx context.Context, query marketplaceOperationsQuery) ([]marketplaceOperationsRow, error) {
	where, args := marketplaceOperationsFilters(query, "s.shop_id::text", "s.order_status", "s.order_sn", "s.shop_label", "COALESCE(b.sml_doc_no, s.sml_doc_no, '')", "COALESCE(s.last_order_update_at, s.updated_at)", marketplaceSourceShopee)
	statement := `
WITH source_rows AS (
  SELECT
    'shopee'::text AS source,
    s.shop_id::text AS shop_id,
    s.shop_label AS shop_name,
    s.order_sn AS order_id,
    s.order_status AS marketplace_status,
    s.erp_status AS erp_status,
    COALESCE(s.bill_id::text, '') AS bill_id,
    COALESCE(b.status, '') AS bill_status,
    COALESCE(b.sml_doc_no, s.sml_doc_no, '') AS sml_doc_no,
    COALESCE(cancel.cancel_sml_doc_no, '') AS cancel_sml_doc_no,
    COALESCE(cancel.status, '') AS cancellation_status,
    COALESCE(auto.status, '') AS auto_sml_status,
    s.currency,
    s.total_amount::text AS total_amount,
    s.item_count,
    s.item_count AS sku_count,
    COALESCE(s.last_order_update_at, s.updated_at) AS last_updated_at,
    s.last_synced_at,
    CASE b.document_route
      WHEN 'saleinvoice' THEN '/sale-invoices/' || b.id::text
      WHEN 'saleorder' THEN '/sales-orders/' || b.id::text
      ELSE ''
    END AS document_path
  FROM shopee_order_snapshots s
  LEFT JOIN bills b ON b.id = s.bill_id
  LEFT JOIN LATERAL (
    SELECT cancel_sml_doc_no, status
    FROM shopee_sml_cancellations c
    WHERE c.shop_id = s.shop_id AND c.order_sn = s.order_sn
    ORDER BY (c.status IN ('created', 'already_exists')) DESC, c.updated_at DESC
    LIMIT 1
  ) cancel ON TRUE
  LEFT JOIN LATERAL (
    SELECT status
    FROM shopee_auto_sml_jobs j
    WHERE j.shop_id = s.shop_id AND j.order_sn = s.order_sn
    ORDER BY j.updated_at DESC
    LIMIT 1
  ) auto ON TRUE
  WHERE ` + where + `
), normalized AS (
  SELECT source_rows.*,
    CASE
      WHEN marketplace_status IN ('CANCELLED', 'IN_CANCEL') AND cancellation_status IN ('failed', 'manual_reconciliation') THEN 'cancel_failed'
      WHEN marketplace_status IN ('CANCELLED', 'IN_CANCEL') AND cancellation_status IN ('created', 'already_exists') THEN 'complete'
      WHEN marketplace_status IN ('CANCELLED', 'IN_CANCEL') AND sml_doc_no <> '' THEN 'cancel_document_needed'
      WHEN marketplace_status IN ('CANCELLED', 'IN_CANCEL') THEN 'complete'
      WHEN marketplace_status = 'UNPAID' THEN 'complete'
      WHEN auto_sml_status = 'failed' OR erp_status = 'failed' THEN 'send_failed'
      WHEN sml_doc_no <> '' THEN 'complete'
      WHEN bill_id <> '' THEN 'ready_to_send'
      WHEN erp_status = 'needs_review' THEN 'needs_review'
      ELSE 'ready_to_create'
    END AS work_state
  FROM source_rows
)
SELECT source, shop_id, shop_name, order_id, marketplace_status, erp_status, bill_id, bill_status,
       sml_doc_no, cancel_sml_doc_no, cancellation_status, auto_sml_status, currency, total_amount,
       item_count, sku_count, last_updated_at, last_synced_at, document_path, work_state
FROM normalized
WHERE ` + marketplaceOperationsViewPredicate(query.View) + `
ORDER BY last_updated_at DESC, source ASC, shop_id ASC, order_id ASC
LIMIT $` + fmt.Sprint(len(args)+1)
	args = append(args, query.Limit+1)
	return h.scanMarketplaceRows(ctx, statement, args...)
}

func (h *MarketplaceOperationsHandler) listTikTok(ctx context.Context, query marketplaceOperationsQuery) ([]marketplaceOperationsRow, error) {
	where, args := marketplaceOperationsFilters(query, "s.shop_id", "s.order_status", "s.order_id", "c.shop_name", "COALESCE(b.sml_doc_no, '')", "COALESCE(s.last_order_update_at, s.updated_at)", marketplaceSourceTikTok)
	statement := `
WITH source_rows AS (
  SELECT
    'tiktok'::text AS source,
    s.shop_id,
    c.shop_name,
    s.order_id,
    s.order_status AS marketplace_status,
    ''::text AS erp_status,
    COALESCE(b.bill_id, '') AS bill_id,
    COALESCE(b.bill_status, '') AS bill_status,
    COALESCE(b.sml_doc_no, '') AS sml_doc_no,
    COALESCE(cancel.cancel_sml_doc_no, '') AS cancel_sml_doc_no,
    COALESCE(cancel.status, '') AS cancellation_status,
    COALESCE(auto.status, '') AS auto_sml_status,
    s.currency,
    s.payment_total_amount::text AS total_amount,
    s.item_count,
    s.sku_count,
    COALESCE(s.last_order_update_at, s.updated_at) AS last_updated_at,
    s.last_synced_at,
    COALESCE(b.document_path, '') AS document_path
  FROM tiktok_shop_order_snapshots s
  JOIN tiktok_shop_connections c ON c.shop_id = s.shop_id AND c.disabled_at IS NULL
  LEFT JOIN LATERAL (
    SELECT bill.id::text AS bill_id, bill.status AS bill_status, COALESCE(bill.sml_doc_no, '') AS sml_doc_no,
      CASE bill.document_route
        WHEN 'saleinvoice' THEN '/sale-invoices/' || bill.id::text
        WHEN 'saleorder' THEN '/sales-orders/' || bill.id::text
        ELSE ''
      END AS document_path
    FROM bills bill
    WHERE bill.source = 'tiktok' AND bill.sml_order_id = s.order_id AND bill.archived_at IS NULL
    ORDER BY bill.created_at DESC, bill.id DESC
    LIMIT 1
  ) b ON TRUE
  LEFT JOIN LATERAL (
    SELECT cancel_sml_doc_no, status
    FROM tiktok_shop_sml_cancellations tc
    WHERE tc.shop_id = s.shop_id AND tc.order_id = s.order_id
      AND (b.bill_id = '' OR tc.bill_id::text = b.bill_id)
    ORDER BY tc.updated_at DESC
    LIMIT 1
  ) cancel ON TRUE
  LEFT JOIN LATERAL (
    SELECT status
    FROM tiktok_shop_auto_sml_jobs j
    WHERE j.shop_id = s.shop_id AND j.order_id = s.order_id
    ORDER BY j.updated_at DESC
    LIMIT 1
  ) auto ON TRUE
  WHERE ` + where + `
), normalized AS (
  SELECT source_rows.*,
    CASE
      WHEN marketplace_status = 'CANCELLED' AND cancellation_status IN ('failed', 'manual_reconciliation') THEN 'cancel_failed'
      WHEN marketplace_status = 'CANCELLED' AND cancellation_status IN ('created', 'already_exists') THEN 'complete'
      WHEN marketplace_status = 'CANCELLED' AND sml_doc_no <> '' THEN 'cancel_document_needed'
      WHEN marketplace_status = 'CANCELLED' THEN 'complete'
      WHEN marketplace_status NOT IN ('AWAITING_COLLECTION', 'IN_TRANSIT', 'DELIVERED', 'COMPLETED') THEN 'complete'
      WHEN auto_sml_status = 'failed' THEN 'send_failed'
      WHEN sml_doc_no <> '' THEN 'complete'
      WHEN bill_id <> '' THEN 'ready_to_send'
      ELSE 'ready_to_create'
    END AS work_state
  FROM source_rows
)
SELECT source, shop_id, shop_name, order_id, marketplace_status, erp_status, bill_id, bill_status,
       sml_doc_no, cancel_sml_doc_no, cancellation_status, auto_sml_status, currency, total_amount,
       item_count, sku_count, last_updated_at, last_synced_at, document_path, work_state
FROM normalized
WHERE ` + marketplaceOperationsViewPredicate(query.View) + `
ORDER BY last_updated_at DESC, source ASC, shop_id ASC, order_id ASC
LIMIT $` + fmt.Sprint(len(args)+1)
	args = append(args, query.Limit+1)
	return h.scanMarketplaceRows(ctx, statement, args...)
}

func marketplaceOperationsFilters(query marketplaceOperationsQuery, shopColumn, statusColumn, orderColumn, shopNameColumn, documentColumn, updatedAtColumn string, source marketplaceOperationsSource) (string, []interface{}) {
	where := []string{"TRUE"}
	args := make([]interface{}, 0, 8)
	add := func(predicate string, value interface{}) {
		args = append(args, value)
		where = append(where, predicate+" $"+fmt.Sprint(len(args)))
	}
	if query.ShopID != "" {
		add(shopColumn+" =", query.ShopID)
	}
	if query.Status != "" {
		add(statusColumn+" =", query.Status)
	}
	if query.Search != "" {
		args = append(args, "%"+query.Search+"%")
		placeholder := "$" + fmt.Sprint(len(args))
		where = append(where, "("+orderColumn+" ILIKE "+placeholder+" OR "+shopNameColumn+" ILIKE "+placeholder+" OR "+documentColumn+" ILIKE "+placeholder+")")
	}
	if query.From != nil {
		add(updatedAtColumn+" >=", query.From.UTC())
	}
	if query.To != nil {
		add(updatedAtColumn+" <", query.To.UTC())
	}
	if cursor := query.Cursor; cursor != nil {
		args = append(args, cursor.UpdatedAt.UTC())
		updatedAtPlaceholder := "$" + fmt.Sprint(len(args))
		args = append(args, string(cursor.Source))
		sourcePlaceholder := "$" + fmt.Sprint(len(args))
		args = append(args, cursor.ShopID)
		shopPlaceholder := "$" + fmt.Sprint(len(args))
		args = append(args, cursor.OrderID)
		orderPlaceholder := "$" + fmt.Sprint(len(args))
		where = append(where, "("+updatedAtColumn+" < "+updatedAtPlaceholder+" OR ("+updatedAtColumn+" = "+updatedAtPlaceholder+" AND ('"+string(source)+"' > "+sourcePlaceholder+" OR ('"+string(source)+"' = "+sourcePlaceholder+" AND ("+shopColumn+" > "+shopPlaceholder+" OR ("+shopColumn+" = "+shopPlaceholder+" AND "+orderColumn+" > "+orderPlaceholder+"))))))")
	}
	return strings.Join(where, " AND "), args
}

func marketplaceOperationsViewPredicate(view marketplaceOperationsView) string {
	switch view {
	case marketplaceOperationsViewCancelled:
		return "marketplace_status IN ('CANCELLED', 'IN_CANCEL')"
	case marketplaceOperationsViewWork:
		return "work_state <> 'complete'"
	default:
		return "TRUE"
	}
}

func (h *MarketplaceOperationsHandler) scanMarketplaceRows(ctx context.Context, statement string, args ...interface{}) ([]marketplaceOperationsRow, error) {
	rows, err := h.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]marketplaceOperationsRow, 0)
	for rows.Next() {
		var row marketplaceOperationsRow
		if err := rows.Scan(
			&row.Source, &row.ShopID, &row.ShopName, &row.OrderID, &row.MarketplaceStatus, &row.ERPStatus,
			&row.BillID, &row.BillStatus, &row.SMLDocNo, &row.CancelSMLDocNo, &row.CancellationStatus,
			&row.AutoSMLStatus, &row.Currency, &row.TotalAmount, &row.ItemCount, &row.SKUCount,
			&row.LastUpdatedAt, &row.LastSyncedAt, &row.DocumentPath, &row.WorkState,
		); err != nil {
			return nil, err
		}
		row.LastUpdatedAt = row.LastUpdatedAt.UTC()
		row.LastSyncedAt = row.LastSyncedAt.UTC()
		row.WorkState = deriveMarketplaceWorkState(row)
		row.AvailableActions = marketplaceActionsForRow(row)
		out = append(out, row)
	}
	return out, rows.Err()
}

func parseMarketplaceOperationsQuery(values marketplaceOperationsQueryReader) (marketplaceOperationsQuery, error) {
	query := marketplaceOperationsQuery{
		Source: marketplaceSourceAll,
		View:   marketplaceOperationsViewWork,
		Limit:  marketplaceOperationsDefaultLimit,
	}
	if values == nil {
		return query, nil
	}
	if value := strings.TrimSpace(values.Get("channel")); value != "" {
		query.Source = marketplaceOperationsSource(value)
	}
	if query.Source != marketplaceSourceAll && query.Source != marketplaceSourceShopee && query.Source != marketplaceSourceTikTok {
		return marketplaceOperationsQuery{}, fmt.Errorf("invalid channel")
	}
	if value := strings.TrimSpace(values.Get("view")); value != "" {
		query.View = marketplaceOperationsView(value)
	}
	if query.View != marketplaceOperationsViewWork && query.View != marketplaceOperationsViewAll && query.View != marketplaceOperationsViewCancelled {
		return marketplaceOperationsQuery{}, fmt.Errorf("invalid view")
	}
	query.ShopID = strings.TrimSpace(values.Get("shop_id"))
	query.Status = strings.TrimSpace(values.Get("status"))
	query.Search = strings.TrimSpace(values.Get("q"))
	if len(query.Search) > marketplaceOperationsMaxSearchLength {
		return marketplaceOperationsQuery{}, fmt.Errorf("search is too long")
	}
	from, err := parseMarketplaceOperationsDate(values.Get("from"), false)
	if err != nil {
		return marketplaceOperationsQuery{}, err
	}
	to, err := parseMarketplaceOperationsDate(values.Get("to"), true)
	if err != nil {
		return marketplaceOperationsQuery{}, err
	}
	if from != nil && to != nil && !from.Before(*to) {
		return marketplaceOperationsQuery{}, fmt.Errorf("invalid date range")
	}
	if from != nil && to != nil && to.Sub(*from) > 93*24*time.Hour {
		return marketplaceOperationsQuery{}, fmt.Errorf("date range is too large")
	}
	query.From, query.To = from, to
	if value := strings.TrimSpace(values.Get("limit")); value != "" {
		var parsed int
		if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil || parsed < 1 || parsed > marketplaceOperationsMaxLimit {
			return marketplaceOperationsQuery{}, fmt.Errorf("invalid limit")
		}
		query.Limit = parsed
	}
	if encoded := strings.TrimSpace(values.Get("cursor")); encoded != "" {
		raw, err := base64.RawURLEncoding.DecodeString(encoded)
		if err != nil {
			return marketplaceOperationsQuery{}, fmt.Errorf("invalid cursor")
		}
		var cursor marketplaceOperationsCursor
		if err := json.Unmarshal(raw, &cursor); err != nil || cursor.UpdatedAt.IsZero() || cursor.Source == "" || cursor.ShopID == "" || cursor.OrderID == "" {
			return marketplaceOperationsQuery{}, fmt.Errorf("invalid cursor")
		}
		if cursor.Source != marketplaceSourceShopee && cursor.Source != marketplaceSourceTikTok {
			return marketplaceOperationsQuery{}, fmt.Errorf("invalid cursor")
		}
		query.Cursor = &cursor
	}
	return query, nil
}

func parseMarketplaceOperationsDate(raw string, endExclusive bool) (*time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, nil
	}
	location, err := time.LoadLocation("Asia/Bangkok")
	if err != nil {
		return nil, err
	}
	parsed, err := time.ParseInLocation("2006-01-02", value, location)
	if err != nil {
		return nil, fmt.Errorf("invalid date")
	}
	if endExclusive {
		parsed = parsed.AddDate(0, 0, 1)
	}
	utc := parsed.UTC()
	return &utc, nil
}

func deriveMarketplaceWorkState(row marketplaceOperationsRow) marketplaceWorkState {
	status := strings.ToUpper(strings.TrimSpace(row.MarketplaceStatus))
	cancellation := strings.ToLower(strings.TrimSpace(row.CancellationStatus))
	if status == "CANCELLED" || status == "IN_CANCEL" {
		switch cancellation {
		case "failed", "manual_reconciliation":
			return marketplaceWorkStateCancelFailed
		case "created", "already_exists":
			return marketplaceWorkStateComplete
		}
		if strings.TrimSpace(row.SMLDocNo) != "" {
			return marketplaceWorkStateCancelDocumentNeeded
		}
		return marketplaceWorkStateComplete
	}
	if row.Source == marketplaceSourceTikTok && !tikTokMarketplaceBillLifecycleReady(status) {
		// TikTok has a narrower reviewed-bill lifecycle than its visible order
		// list. Keep a normal unpaid/to-ship order out of the actionable queue;
		// the source adapter will re-check its current snapshot before any write.
		return marketplaceWorkStateComplete
	}
	if row.Source == marketplaceSourceShopee && status == "UNPAID" {
		return marketplaceWorkStateComplete
	}
	if strings.EqualFold(strings.TrimSpace(row.AutoSMLStatus), "failed") || strings.EqualFold(strings.TrimSpace(row.ERPStatus), "failed") {
		return marketplaceWorkStateSendFailed
	}
	if strings.TrimSpace(row.SMLDocNo) != "" {
		return marketplaceWorkStateComplete
	}
	if strings.TrimSpace(row.BillID) != "" && strings.TrimSpace(row.SMLDocNo) == "" {
		return marketplaceWorkStateReadyToSend
	}
	if strings.EqualFold(strings.TrimSpace(row.ERPStatus), "needs_review") {
		return marketplaceWorkStateNeedsReview
	}
	if strings.TrimSpace(row.BillID) == "" {
		return marketplaceWorkStateReadyToCreate
	}
	return marketplaceWorkStateComplete
}

func tikTokMarketplaceBillLifecycleReady(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "AWAITING_COLLECTION", "IN_TRANSIT", "DELIVERED", "COMPLETED":
		return true
	default:
		return false
	}
}

func marketplaceActionsForRow(row marketplaceOperationsRow) []string {
	switch row.WorkState {
	case marketplaceWorkStateReadyToCreate, marketplaceWorkStateNeedsReview, marketplaceWorkStateNeedsMapping:
		return []string{"review_document"}
	case marketplaceWorkStateReadyToSend, marketplaceWorkStateSendFailed:
		return []string{"open_document"}
	case marketplaceWorkStateCancelDocumentNeeded, marketplaceWorkStateCancelFailed:
		return []string{"review_cancellation"}
	default:
		if strings.TrimSpace(row.DocumentPath) != "" {
			return []string{"open_document"}
		}
		return []string{"view_details"}
	}
}

func encodeMarketplaceOperationsCursor(row marketplaceOperationsRow) string {
	payload, err := json.Marshal(marketplaceOperationsCursor{
		UpdatedAt: row.LastUpdatedAt.UTC(), Source: row.Source, ShopID: row.ShopID, OrderID: row.OrderID,
	})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(payload)
}
