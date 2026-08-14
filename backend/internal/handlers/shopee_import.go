package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/xuri/excelize/v2"
	"go.uber.org/zap"

	"nexflow/internal/config"
	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/artifact"
	"nexflow/internal/services/catalog"
)

// ShopeeImportHandler handles Shopee Excel import.
//
// Behavior change (2026-04-27): Confirm no longer pushes to SML inline.
// Bills are created with catalog-matched items and saved as pending /
// needs_review; the user reviews them in BillDetail and clicks "ส่ง SML",
// which routes through bills.go retrySaleInvoice (same path as Shopee
// email orders). This unifies all manual-confirm flows.
type ShopeeImportHandler struct {
	db                     *sql.DB
	billRepo               *repository.BillRepo
	mappingRepo            *repository.MappingRepo
	aliasRepo              *repository.MarketplaceAliasRepo
	auditRepo              *repository.AuditLogRepo
	cfg                    *config.Config
	channelDefaults        *repository.ChannelDefaultRepo
	catalogRepo            *repository.SMLCatalogRepo
	catalogSvc             *catalog.SMLCatalogService
	artifactSvc            *artifact.Service
	settlementLineNotifier shopeeSettlementLineNotifier
	logger                 *zap.Logger

	// Preview state is server-owned so Confirm cannot replace parsed amounts or
	// routing configuration with browser-supplied values. Source files are kept
	// once per import run and referenced by every bill from that run.
	pendingUploads sync.Map
}

type shopeeSettlementLineNotifier interface {
	EnqueueShopeeSettlementReady(ctx context.Context, run models.ShopeeSettlementLineRun, dedupeKey string) (int, error)
}

type pendingUpload struct {
	bytes        []byte
	artifactID   string
	filename     string
	fileHash     string
	importRunID  string
	userID       string
	sourceFlow   string
	connectionID string
	orders       []ShopeeOrder
	uploadedAt   time.Time
}

const (
	pendingUploadTTL   = 15 * time.Minute
	shopeeMaxFileBytes = 20 * 1024 * 1024
	shopeeMaxOrders    = 5_000
)

func NewShopeeImportHandler(
	db *sql.DB,
	billRepo *repository.BillRepo,
	mappingRepo *repository.MappingRepo,
	auditRepo *repository.AuditLogRepo,
	cfg *config.Config,
	channelDefaults *repository.ChannelDefaultRepo,
	catalogRepo *repository.SMLCatalogRepo,
	catalogSvc *catalog.SMLCatalogService,
	aliasRepo *repository.MarketplaceAliasRepo,
	logger *zap.Logger,
) *ShopeeImportHandler {
	h := &ShopeeImportHandler{
		db:              db,
		billRepo:        billRepo,
		mappingRepo:     mappingRepo,
		aliasRepo:       aliasRepo,
		auditRepo:       auditRepo,
		cfg:             cfg,
		channelDefaults: channelDefaults,
		catalogRepo:     catalogRepo,
		catalogSvc:      catalogSvc,
		logger:          logger,
	}
	go h.gcPendingUploads()
	return h
}

// SetArtifactService wires immutable source-artifact storage per import run.
func (h *ShopeeImportHandler) SetArtifactService(svc *artifact.Service) {
	h.artifactSvc = svc
}

func (h *ShopeeImportHandler) SetSettlementLineNotifier(notifier shopeeSettlementLineNotifier) {
	if h != nil {
		h.settlementLineNotifier = notifier
	}
}

// gcPendingUploads runs forever, evicting cached uploads older than the TTL.
// Tiny goroutine — pending map size is at most a few entries at a time
// (one per active import session), so a periodic walk is cheap.
func (h *ShopeeImportHandler) gcPendingUploads() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		h.pendingUploads.Range(func(key, val any) bool {
			if pu, ok := val.(*pendingUpload); ok {
				if now.Sub(pu.uploadedAt) > pendingUploadTTL {
					h.pendingUploads.Delete(key)
				}
			}
			return true
		})
	}
}

func (h *ShopeeImportHandler) consumeShopeePreview(token, importRunID, userID string, now time.Time) (*pendingUpload, bool) {
	loaded, ok := h.pendingUploads.LoadAndDelete(strings.TrimSpace(token))
	if !ok {
		return nil, false
	}
	pending, ok := loaded.(*pendingUpload)
	if !ok || pending.importRunID != strings.TrimSpace(importRunID) || pending.userID != userID ||
		now.Sub(pending.uploadedAt) > pendingUploadTTL || now.Before(pending.uploadedAt) {
		return nil, false
	}
	return pending, true
}

func validateShopeeShippingConfig(orders []ShopeeOrder, def *models.ChannelDefault, label string) error {
	for _, order := range orders {
		if order.ShippingAmount <= 0 {
			continue
		}
		if def == nil || !def.ShippingItemEnabled || strings.TrimSpace(def.ShippingItemCode) == "" || strings.TrimSpace(def.ShippingItemUnitCode) == "" {
			return fmt.Errorf("รายการมีค่าส่งที่ลูกค้าจ่าย กรุณาตั้งสินค้า SML สำหรับค่าจัดส่ง %s ที่เมนูเส้นทางเอกสารก่อน", label)
		}
		break
	}
	return nil
}

func applyShopeeShippingPreflight(orders []ShopeeOrder, def *models.ChannelDefault) {
	if validateShopeeShippingConfig(orders, def, "Shopee") == nil {
		return
	}
	for i := range orders {
		if orders[i].ShippingAmount > 0 && orders[i].BlockedReason == "" {
			orders[i].BlockedReason = "ยังไม่ได้ตั้งสินค้า SML สำหรับค่าจัดส่ง Shopee"
		}
	}
}

// ─── Shopee column name candidates ───────────────────────────────────────────

// shopeeColCandidates maps field names to keyword substrings.
// Matching uses strings.Contains so partial header names work
// even when Shopee adds English translations like "หมายเลขคำสั่งซื้อ (Order No.)"
var shopeeColCandidates = map[string][]string{
	"order_id":        {"หมายเลขคำสั่งซื้อ"},
	"status":          {"สถานะการสั่งซื้อ"},
	"buyer_username":  {"ชื่อผู้ใช้ (ผู้ซื้อ)", "ชื่อผู้ใช้", "Buyer Username"},
	"order_date":      {"วันที่สั่งซื้อ", "วันที่ทำการสั่งซื้อ", "Order Creation Date", "Order Date"},
	"payment_time":    {"เวลาการชำระสินค้า", "เวลาชำระสินค้า", "Paid Time"},
	"payment_channel": {"ช่องทางการชำระเงิน"},
	"tracking_no":     {"หมายเลขติดตามพัสดุ", "Tracking Number"},
	"product_name":    {"ชื่อสินค้า"},
	"option_name":     {"ชื่อตัวเลือก", "Variation Name"},
	"sku":             {"เลขอ้างอิง SKU", "SKU Reference No."},
	"gross_price":     {"ราคาตั้งต้น"},
	"price":           {"ราคาขาย"},
	"net_price":       {"ราคาขายสุทธิ"},
	"qty":             {"จำนวน"},
	"paid_amount":     {"ยอดชำระเงิน"},
	"order_total":     {"จำนวนเงินทั้งหมด", "Order Total"},
	"buyer_product":   {"ราคาสินค้าที่ชำระโดยผู้ซื้อ"},
	"shipping_amount": {"ค่าจัดส่งที่ชำระโดยผู้ซื้อ"},
}

var excludeStatuses = map[string]bool{
	"ยกเลิกแล้ว": true,
}

// ─── Request / Response types ─────────────────────────────────────────────────

// ShopeeConfigRequest holds the config fields sent from the frontend dialog.
type ShopeeConfigRequest struct {
	ServerURL  string  `json:"server_url"`
	GUID       string  `json:"guid"`
	Provider   string  `json:"provider"`
	ConfigFile string  `json:"config_file_name"`
	Database   string  `json:"database_name"`
	DocFormat  string  `json:"doc_format_code"`
	Endpoint   string  `json:"endpoint"`
	CustCode   string  `json:"cust_code"`
	SaleCode   string  `json:"sale_code"`
	BranchCode string  `json:"branch_code"`
	WHCode     string  `json:"wh_code"`
	ShelfCode  string  `json:"shelf_code"`
	UnitCode   string  `json:"unit_code"`
	VATType    int     `json:"vat_type"`
	VATRate    float64 `json:"vat_rate"`
	DocTime    string  `json:"doc_time"`
}

// ShopeeExcelItem is one parsed Shopee Excel line. SKU is optional in real
// Seller Center exports; when it is missing RawName becomes the matching key.
type ShopeeExcelItem struct {
	SKU                    string  `json:"sku"`
	LazadaSKU              string  `json:"lazada_sku,omitempty"`
	TikTokSKU              string  `json:"tiktok_sku,omitempty"`
	OrderItemID            string  `json:"order_item_id,omitempty"`
	SourceItemID           string  `json:"source_item_id,omitempty"`
	SourceVariantID        string  `json:"source_variant_id,omitempty"`
	ProductName            string  `json:"product_name"`
	OptionName             string  `json:"option_name,omitempty"`
	RawName                string  `json:"raw_name"`
	Price                  float64 `json:"price"`
	GrossAmount            float64 `json:"gross_amount,omitempty"`
	DiscountAmount         float64 `json:"discount_amount,omitempty"`
	PlatformDiscountAmount float64 `json:"platform_discount_amount,omitempty"`
	SellerDiscountAmount   float64 `json:"seller_discount_amount,omitempty"`
	Qty                    float64 `json:"qty"`
	NoSKU                  bool    `json:"no_sku,omitempty"`
}

// ShopeeOrder is one parsed Shopee order (returned in preview).
type ShopeeOrder struct {
	OrderID                string            `json:"order_id"`
	DocDate                string            `json:"doc_date"`
	OrderDateTime          string            `json:"order_datetime,omitempty"`
	PaymentTime            string            `json:"payment_time,omitempty"`
	PaymentChannel         string            `json:"payment_channel,omitempty"`
	BuyerUsername          string            `json:"buyer_username,omitempty"`
	TrackingNo             string            `json:"tracking_no,omitempty"`
	PackageNumber          string            `json:"package_number,omitempty"`
	ShippingCarrier        string            `json:"shipping_carrier,omitempty"`
	COD                    bool              `json:"cod,omitempty"`
	Status                 string            `json:"status"`
	Items                  []ShopeeExcelItem `json:"items"`
	ItemCount              int               `json:"item_count"`
	TotalQty               float64           `json:"total_qty"`
	PaidAmount             float64           `json:"paid_amount,omitempty"`
	OrderTotalAmount       float64           `json:"order_total_amount,omitempty"`
	ItemGrossAmount        float64           `json:"item_gross_amount,omitempty"`
	LinePaidAmount         float64           `json:"line_paid_amount,omitempty"`
	ShippingAmount         float64           `json:"shipping_amount,omitempty"`
	DiscountAmount         float64           `json:"discount_amount,omitempty"`
	PlatformDiscountAmount float64           `json:"platform_discount_amount,omitempty"`
	SellerDiscountAmount   float64           `json:"seller_discount_amount,omitempty"`
	NetProductAmount       float64           `json:"net_product_amount,omitempty"`
	TaxesAmount            float64           `json:"taxes_amount,omitempty"`
	PaymentDiscountAmount  float64           `json:"payment_discount_amount,omitempty"`
	AmountDifference       float64           `json:"amount_difference,omitempty"`
	AmountReviewReason     string            `json:"amount_review_reason,omitempty"`
	NoSKUItemCount         int               `json:"no_sku_item_count,omitempty"`
	HasNoSKU               bool              `json:"has_no_sku,omitempty"`
	MultiLine              bool              `json:"multi_line,omitempty"`
	AmountMismatch         bool              `json:"amount_mismatch,omitempty"`
	ExistingBillID         string            `json:"existing_bill_id,omitempty"`
	ShopeeShopID           string            `json:"shopee_shop_id,omitempty"`
	ShopeeConnectionID     string            `json:"shopee_connection_id,omitempty"`
	ShopeeShopLabel        string            `json:"shopee_shop_label,omitempty"`
	// preview-only
	Duplicate      bool   `json:"duplicate"`
	BlockedReason  string `json:"blocked_reason,omitempty"`
	RealtimeStatus string `json:"realtime_status,omitempty"`
	RealtimeBillID string `json:"realtime_bill_id,omitempty"`
	ActionURL      string `json:"action_url,omitempty"`
}

type ShopeeImportPreflight struct {
	NewOrders             int `json:"new_orders"`
	DuplicateOrders       int `json:"duplicate_orders"`
	SkippedRows           int `json:"skipped_rows"`
	NoSKUOrders           int `json:"no_sku_orders"`
	NoSKUItems            int `json:"no_sku_items"`
	MultiItemOrders       int `json:"multi_item_orders"`
	AmountMismatchOrders  int `json:"amount_mismatch_orders"`
	RealtimeManagedOrders int `json:"realtime_managed_orders"`
}

// PreviewResponse is returned from POST /api/import/shopee/preview
type PreviewResponse struct {
	Orders         []ShopeeOrder         `json:"orders"`
	Warnings       []string              `json:"warnings"`
	TotalOrders    int                   `json:"total_orders"`
	NewCount       int                   `json:"new_count"`
	DuplicateCount int                   `json:"duplicate_count"`
	SkippedCount   int                   `json:"skipped_count"`
	ImportRunID    string                `json:"import_run_id,omitempty"`
	Preflight      ShopeeImportPreflight `json:"preflight"`
	// FileToken is retained as an empty compatibility field for older clients.
	// New clients must use PreviewToken; the source file is stored once per run.
	FileToken    string `json:"file_token,omitempty"`
	PreviewToken string `json:"preview_token,omitempty"`
}

// ConfirmRequest is sent by the frontend for POST /api/import/shopee/confirm
type ConfirmRequest struct {
	PreviewToken string `json:"preview_token,omitempty"`
	// Deprecated compatibility fields below are always overwritten from the
	// server-owned preview and current channel settings before use.
	Config       ShopeeConfigRequest `json:"config"`
	OrderIDs     []string            `json:"order_ids"`            // only these order IDs will be processed
	Orders       []ShopeeOrder       `json:"orders"`               // full parsed order data
	FileToken    string              `json:"file_token,omitempty"` // returned by Preview, used for artifact archiving
	ImportRunID  string              `json:"import_run_id,omitempty"`
	SourceFlow   string              `json:"source_flow,omitempty"` // shopee_excel (default) or shopee_api
	ConnectionID string              `json:"connection_id,omitempty"`
}

// ConfirmResult is one processed order result.
type ConfirmResult struct {
	OrderID        string `json:"order_id"`
	Success        bool   `json:"success"`
	DocNo          string `json:"doc_no,omitempty"`
	Message        string `json:"message,omitempty"`
	BillID         string `json:"bill_id,omitempty"`
	BlockedReason  string `json:"blocked_reason,omitempty"`
	ActionURL      string `json:"action_url,omitempty"`
	RealtimeStatus string `json:"realtime_status,omitempty"`
	RealtimeBillID string `json:"realtime_bill_id,omitempty"`
}

type ImportRunSummary struct {
	ID              string          `json:"id"`
	Filename        string          `json:"filename"`
	FileSHA256      string          `json:"file_sha256,omitempty"`
	PeriodStart     string          `json:"period_start,omitempty"`
	PeriodEnd       string          `json:"period_end,omitempty"`
	TotalOrders     int             `json:"total_orders"`
	NewOrders       int             `json:"new_orders"`
	DuplicateOrders int             `json:"duplicate_orders"`
	SkippedOrders   int             `json:"skipped_orders"`
	WarningCount    int             `json:"warning_count"`
	CreatedCount    int             `json:"created_count"`
	FailedCount     int             `json:"failed_count"`
	Status          string          `json:"status"`
	Detail          json.RawMessage `json:"detail,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	ConfirmedAt     *time.Time      `json:"confirmed_at,omitempty"`
}

type ShopeeBillCreateOptions struct {
	Config      ShopeeConfigRequest
	SourceFlow  string
	ImportRunID string
	Connection  *ShopeeAPIConnection
	UserID      *string
	TraceID     string
	StartedAt   time.Time
}

// ─── GET /api/settings/shopee-config ─────────────────────────────────────────

// GetConfig returns the active Shopee SML config — env defaults overlaid with
// per-channel overrides from channel_defaults (shopee, sale). Read-only: the
// /import/shopee page renders this as a summary card so users see what'll
// actually be sent on Retry.
func (h *ShopeeImportHandler) GetConfig(c *gin.Context) {
	c.JSON(http.StatusOK, h.CurrentShopeeSaleConfig())
}

func (h *ShopeeImportHandler) CurrentShopeeSaleConfig() ShopeeConfigRequest {
	return h.CurrentShopeeSaleConfigForChannel("shopee")
}

func (h *ShopeeImportHandler) CurrentShopeeSaleConfigForChannel(channel string) ShopeeConfigRequest {
	custCode := ""
	whCode := h.cfg.ShopeeSMLWHCode
	shelfCode := h.cfg.ShopeeSMLShelfCode
	vatType := h.cfg.ShopeeSMLVATType
	vatRate := h.cfg.ShopeeSMLVATRate
	docFormat := h.cfg.ShopeeSMLDocFormat
	endpoint := ""
	channel = strings.TrimSpace(channel)
	if channel == "" {
		channel = "shopee"
	}
	if h.channelDefaults != nil {
		if def, _ := h.channelDefaults.Get(channel, "sale"); def != nil {
			custCode = def.PartyCode
			endpoint = def.Endpoint
			if def.WHCode != "" {
				whCode = def.WHCode
			}
			if def.ShelfCode != "" {
				shelfCode = def.ShelfCode
			}
			if def.VATType >= 0 {
				vatType = def.VATType
			}
			if def.VATRate >= 0 {
				vatRate = def.VATRate
			}
			if def.DocFormatCode != "" {
				docFormat = def.DocFormatCode
			}
		}
	}
	return ShopeeConfigRequest{
		ServerURL:  h.cfg.ShopeeSMLURL,
		GUID:       h.cfg.ShopeeSMLGUID,
		Provider:   h.cfg.ShopeeSMLProvider,
		ConfigFile: h.cfg.ShopeeSMLConfigFile,
		Database:   h.cfg.ShopeeSMLDatabase,
		DocFormat:  docFormat,
		Endpoint:   endpoint,
		CustCode:   custCode,
		SaleCode:   h.cfg.ShopeeSMLSaleCode,
		BranchCode: h.cfg.ShopeeSMLBranchCode,
		WHCode:     whCode,
		ShelfCode:  shelfCode,
		UnitCode:   h.cfg.ShopeeSMLUnitCode,
		VATType:    vatType,
		VATRate:    vatRate,
		DocTime:    h.cfg.ShopeeSMLDocTime,
	}
}

// ListRuns returns recent Shopee Excel import sessions so admins can see
// duplicate-safe re-imports and what each preview/confirm produced.
func (h *ShopeeImportHandler) ListRuns(c *gin.Context) {
	limit := 8
	if raw := c.Query("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 50 {
			limit = n
		}
	}
	rows, err := h.billRepo.DB().Query(
		`SELECT id::text, filename, file_sha256,
		        COALESCE(period_start::text, ''), COALESCE(period_end::text, ''),
		        total_orders, new_orders, duplicate_orders, skipped_orders,
		        warning_count, created_count, failed_count, status, detail,
		        created_at, confirmed_at
		   FROM import_runs
		  WHERE source = 'shopee'
		  ORDER BY created_at DESC
		  LIMIT $1`,
		limit,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลดประวัติ import ไม่ได้"})
		return
	}
	defer rows.Close()

	runs := []ImportRunSummary{}
	for rows.Next() {
		var run ImportRunSummary
		if err := rows.Scan(
			&run.ID,
			&run.Filename,
			&run.FileSHA256,
			&run.PeriodStart,
			&run.PeriodEnd,
			&run.TotalOrders,
			&run.NewOrders,
			&run.DuplicateOrders,
			&run.SkippedOrders,
			&run.WarningCount,
			&run.CreatedCount,
			&run.FailedCount,
			&run.Status,
			&run.Detail,
			&run.CreatedAt,
			&run.ConfirmedAt,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "อ่านประวัติ import ไม่ได้"})
			return
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "อ่านประวัติ import ไม่ได้"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"runs": runs})
}

// ─── POST /api/import/shopee/preview ─────────────────────────────────────────

// Preview parses the uploaded Shopee Excel and returns order previews + warnings.
// Does NOT write to DB or call SML.
func (h *ShopeeImportHandler) Preview(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณาแนบไฟล์ Excel (.xlsx)"})
		return
	}
	if !strings.HasSuffix(strings.ToLower(fileHeader.Filename), ".xlsx") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "รองรับเฉพาะไฟล์ .xlsx เท่านั้น"})
		return
	}
	if fileHeader.Size > shopeeMaxFileBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "ไฟล์เกิน 20 MB กรุณาแบ่งไฟล์แล้วนำเข้าใหม่"})
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "เปิดไฟล์ไม่ได้"})
		return
	}
	defer file.Close()

	// Read once into memory so we can both parse it and stash the bytes
	// for Confirm to archive as an artifact.
	rawBytes, err := io.ReadAll(io.LimitReader(file, shopeeMaxFileBytes+1))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "อ่านไฟล์ไม่ได้"})
		return
	}
	if len(rawBytes) > shopeeMaxFileBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "ไฟล์เกิน 20 MB กรุณาแบ่งไฟล์แล้วนำเข้าใหม่"})
		return
	}

	orders, warnings, skippedCount, err := parseShopeeExcel(bytes.NewReader(rawBytes))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(orders) > shopeeMaxOrders {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "ไฟล์มีเกิน 5,000 orders กรุณาแบ่งไฟล์แล้วนำเข้าใหม่"})
		return
	}
	var selectedConn *ShopeeAPIConnection
	if connID := strings.TrimSpace(c.PostForm("connection_id")); connID != "" {
		selectedConn, err = h.resolveShopeeAPIConnection(c.Request.Context(), connID)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusBadRequest, gin.H{"error": "ไม่พบร้าน Shopee ที่เลือก"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลดร้าน Shopee ที่เลือกไม่สำเร็จ"})
			return
		}
		if selectedConn.DisabledAt.Valid {
			c.JSON(http.StatusBadRequest, gin.H{"error": "ร้าน Shopee ที่เลือกถูกปิดใช้งาน"})
			return
		}
	}

	sum := sha256.Sum256(rawBytes)
	fileHash := hex.EncodeToString(sum[:])

	// Mark duplicates (orders already in DB)
	dupCount := 0
	shopID := ""
	if selectedConn != nil {
		shopID = strconv.FormatInt(selectedConn.ShopID, 10)
	}
	for i := range orders {
		if selectedConn != nil {
			orders[i].ShopeeShopID = shopID
			orders[i].ShopeeConnectionID = selectedConn.ID
			orders[i].ShopeeShopLabel = selectedConn.DisplayLabel()
		}
		if billID, exists, _ := h.findShopeeOrderBillIDForShop(orders[i].OrderID, shopID); exists {
			orders[i].Duplicate = true
			orders[i].ExistingBillID = billID
			dupCount++
		}
	}
	if err := h.markRealtimeManagedOrders(c.Request.Context(), orders, shopID); err != nil {
		h.logger.Warn("shopee_import: mark realtime managed orders failed", zap.Error(err))
	}
	var channelDefault *models.ChannelDefault
	if h.channelDefaults != nil {
		channelDefault, _ = h.channelDefaults.Get("shopee", "sale")
	}
	applyShopeeShippingPreflight(orders, channelDefault)
	preflight := buildShopeePreflight(orders, skippedCount, dupCount)
	importRunID := h.createShopeeImportRun(c, fileHeader.Filename, fileHash, orders, warnings, preflight)
	if importRunID == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "สร้างรอบนำเข้าไม่สำเร็จ กรุณาลองใหม่"})
		return
	}
	previewToken, err := randomMarketplacePreviewToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "สร้าง preview token ไม่สำเร็จ"})
		return
	}
	artifactID := ""
	if h.artifactSvc != nil {
		saved, saveErr := h.artifactSvc.SaveForImportRun(importRunID, "xlsx", fileHeader.Filename,
			"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", rawBytes,
			map[string]interface{}{"source": "shopee", "file_sha256": fileHash})
		if saveErr != nil {
			h.finishShopeeImportRun(importRunID, 0, 1, "failed")
			h.logger.Error("shopee_import: persist source artifact", zap.String("import_run_id", importRunID), zap.Error(saveErr))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "เก็บไฟล์ต้นฉบับไม่สำเร็จ กรุณาลองใหม่"})
			return
		}
		artifactID = saved.ID
	}
	pendingBytes := rawBytes
	if artifactID != "" {
		pendingBytes = nil
	}
	connectionID := ""
	if selectedConn != nil {
		connectionID = selectedConn.ID
	}
	h.pendingUploads.Store(previewToken, &pendingUpload{
		bytes: pendingBytes, artifactID: artifactID, filename: fileHeader.Filename, fileHash: fileHash,
		importRunID: importRunID, userID: c.GetString("user_id"), sourceFlow: "shopee_excel",
		connectionID: connectionID, uploadedAt: time.Now(),
	})

	if h.auditRepo != nil {
		traceID := c.GetString("trace_id")
		var userID *string
		if uid := c.GetString("user_id"); uid != "" {
			userID = &uid
		}
		_ = h.auditRepo.Log(models.AuditEntry{
			Action:  "shopee_import_preview",
			UserID:  userID,
			Source:  "shopee_excel",
			Level:   "info",
			TraceID: traceID,
			Detail: map[string]interface{}{
				"filename":        fileHeader.Filename,
				"total_orders":    len(orders),
				"duplicate_count": dupCount,
				"skipped_count":   skippedCount,
				"import_run_id":   importRunID,
				"shopee_shop_id":  shopID,
			},
		})
	}

	c.JSON(http.StatusOK, PreviewResponse{
		Orders:         orders,
		Warnings:       warnings,
		TotalOrders:    len(orders),
		NewCount:       preflight.NewOrders,
		DuplicateCount: dupCount,
		SkippedCount:   skippedCount,
		ImportRunID:    importRunID,
		Preflight:      preflight,
		PreviewToken:   previewToken,
	})
}

// ─── POST /api/import/shopee/confirm ─────────────────────────────────────────

// Confirm processes the selected orders: calls SML 224 and saves bills to DB.
func (h *ShopeeImportHandler) Confirm(c *gin.Context) {
	if blockIfCatalogNotReady(c, h.catalogRepo) {
		return
	}
	var req ConfirmRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request ไม่ถูกต้อง: " + err.Error()})
		return
	}
	if strings.TrimSpace(req.PreviewToken) == "" || strings.TrimSpace(req.ImportRunID) == "" {
		writeMarketplacePreviewExpired(c)
		return
	}
	pending, ok := h.consumeShopeePreview(req.PreviewToken, req.ImportRunID, c.GetString("user_id"), time.Now())
	if !ok {
		writeMarketplacePreviewExpired(c)
		return
	}
	serverOrders := append([]ShopeeOrder(nil), pending.orders...)
	if pending.sourceFlow == "shopee_excel" {
		sourceBytes := pending.bytes
		if pending.artifactID != "" {
			if h.artifactSvc == nil {
				writeMarketplacePreviewExpired(c)
				return
			}
			var readErr error
			sourceBytes, _, readErr = h.artifactSvc.ReadForImportRun(pending.importRunID, pending.artifactID)
			if readErr != nil {
				h.logger.Warn("shopee_import: read source artifact", zap.String("import_run_id", pending.importRunID), zap.Error(readErr))
				writeMarketplacePreviewExpired(c)
				return
			}
		}
		sum := sha256.Sum256(sourceBytes)
		if hex.EncodeToString(sum[:]) != pending.fileHash {
			c.JSON(http.StatusConflict, gin.H{"error": "ไฟล์ preview ถูกเปลี่ยน กรุณาอัปโหลดใหม่", "code": "preview_tampered"})
			return
		}
		var parseErr error
		serverOrders, _, _, parseErr = parseShopeeExcel(bytes.NewReader(sourceBytes))
		if parseErr != nil {
			writeMarketplacePreviewExpired(c)
			return
		}
	}
	req.Orders = serverOrders
	req.SourceFlow = pending.sourceFlow
	req.ConnectionID = pending.connectionID
	req.Config = h.CurrentShopeeSaleConfig()

	selectedSet := make(map[string]bool, len(req.OrderIDs))
	for _, id := range req.OrderIDs {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			selectedSet[trimmed] = true
		}
	}
	if len(selectedSet) == 0 || len(selectedSet) > shopeeMaxOrders {
		c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณาเลือก order 1–5,000 รายการ", "code": "invalid_order_selection"})
		return
	}
	serverOrderIDs := make(map[string]ShopeeOrder, len(req.Orders))
	for _, order := range req.Orders {
		serverOrderIDs[order.OrderID] = order
	}
	for id := range selectedSet {
		order, exists := serverOrderIDs[id]
		if !exists {
			c.JSON(http.StatusConflict, gin.H{"error": "รายการที่เลือกไม่อยู่ใน Preview กรุณาเริ่มใหม่", "code": "preview_tampered"})
			return
		}
		if order.BlockedReason != "" {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": order.BlockedReason, "code": "order_blocked", "order_id": id})
			return
		}
	}
	documentRoute := shopeeImportRoute(req.Config)
	destinationName := shopeeImportDocumentName(req.Config)
	reviewPath := shopeeImportReviewPath(req.Config)
	sourceFlow := strings.TrimSpace(req.SourceFlow)
	if sourceFlow == "" {
		sourceFlow = "shopee_excel"
	}
	var selectedConn *ShopeeAPIConnection
	if strings.TrimSpace(req.ConnectionID) != "" {
		var err error
		selectedConn, err = h.resolveShopeeAPIConnection(c.Request.Context(), req.ConnectionID)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusBadRequest, gin.H{"error": "ไม่พบร้าน Shopee ที่เลือก"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลดร้าน Shopee ที่เลือกไม่สำเร็จ"})
			return
		}
		if selectedConn.DisabledAt.Valid {
			c.JSON(http.StatusBadRequest, gin.H{"error": "ร้าน Shopee ที่เลือกถูกปิดใช้งาน"})
			return
		}
	}
	var channelDefault *models.ChannelDefault
	if h.channelDefaults != nil {
		channelDefault, _ = h.channelDefaults.Get("shopee", "sale")
	}
	selectedOrders := make([]ShopeeOrder, 0, len(selectedSet))
	for _, order := range req.Orders {
		if selectedSet[order.OrderID] {
			selectedOrders = append(selectedOrders, order)
		}
	}
	if err := validateShopeeShippingConfig(selectedOrders, channelDefault, "Shopee"); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error(), "code": "shipping_item_not_configured"})
		return
	}

	// Default unit code from the request config; used as a fallback when
	// catalog matching doesn't pick a specific unit.
	defaultUnit := req.Config.UnitCode

	var userID *string
	if uid := c.GetString("user_id"); uid != "" {
		userID = &uid
	}
	traceID := c.GetString("trace_id")
	confirmStart := time.Now()
	if selectedConn != nil {
		shopID := strconv.FormatInt(selectedConn.ShopID, 10)
		for i := range req.Orders {
			req.Orders[i].ShopeeShopID = shopID
			req.Orders[i].ShopeeConnectionID = selectedConn.ID
			req.Orders[i].ShopeeShopLabel = selectedConn.DisplayLabel()
		}
	}

	resolutionBatch, err := prepareMarketplaceResolution(
		c.Request.Context(), "shopee", req.Orders, selectedSet,
		h.catalogRepo, h.catalogSvc, h.aliasRepo, h.mappingRepo, h.logger,
	)
	if err != nil {
		h.logger.Warn("shopee_import: prepare product resolution failed", zap.Error(err))
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "catalog_unavailable", "message": "เตรียมข้อมูลสินค้า SML ไม่สำเร็จ กรุณาลองใหม่"})
		return
	}
	defer flushMarketplaceResolutionUsage(resolutionBatch, h.aliasRepo, h.logger)

	selectedOrderIDs := make([]string, 0, len(req.OrderIDs))
	if len(req.OrderIDs) > 0 {
		selectedOrderIDs = append(selectedOrderIDs, req.OrderIDs...)
	} else {
		for _, order := range req.Orders {
			selectedOrderIDs = append(selectedOrderIDs, order.OrderID)
		}
	}
	selectedShopID := ""
	if selectedConn != nil {
		selectedShopID = strconv.FormatInt(selectedConn.ShopID, 10)
	}
	realtimeStates, err := h.loadRealtimeImportStates(c.Request.Context(), selectedOrderIDs, selectedShopID)
	if err != nil {
		h.logger.Warn("shopee_import: load realtime managed orders failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ตรวจ order ที่อยู่ในคำสั่งซื้อ Shopee ไม่สำเร็จ"})
		return
	}

	results := []ConfirmResult{}

	for _, order := range req.Orders {
		if !selectedSet[order.OrderID] {
			continue
		}
		shopID := strings.TrimSpace(order.ShopeeShopID)
		shopLabel := strings.TrimSpace(order.ShopeeShopLabel)
		connectionID := strings.TrimSpace(order.ShopeeConnectionID)
		if selectedConn != nil {
			shopID = strconv.FormatInt(selectedConn.ShopID, 10)
			shopLabel = selectedConn.DisplayLabel()
			connectionID = selectedConn.ID
		}
		if state, ok := realtimeStates[strings.TrimSpace(order.OrderID)]; ok {
			results = append(results, ConfirmResult{
				OrderID:        order.OrderID,
				Success:        false,
				BillID:         state.BillID,
				Message:        "order นี้อยู่ในเมนูคำสั่งซื้อ Shopee แล้ว ให้เปิดจากคิวงานประจำวันแทนการนำเข้าย้อนหลัง",
				BlockedReason:  "realtime_managed",
				ActionURL:      shopeeRealtimeOrderURL(order.OrderID),
				RealtimeStatus: state.OrderStatus,
				RealtimeBillID: state.BillID,
			})
			continue
		}
		if billID, exists, _ := h.findShopeeOrderBillIDForShop(order.OrderID, shopID); exists {
			results = append(results, ConfirmResult{
				OrderID: order.OrderID,
				Success: false,
				BillID:  billID,
				Message: "order นี้มีอยู่ในระบบแล้ว (ข้าม)",
			})
			continue
		}

		// Resolve each item BEFORE creating the bill so we know the
		// final status (pending vs needs_review).
		type itemEnriched struct{ item models.BillItem }
		var enriched []itemEnriched
		allHigh := true

		for _, it := range order.Items {
			rawName := shopeeItemRawName(it.ProductName, it.OptionName, it.RawName)
			accountKey := marketplaceSourceAccountKey("shopee", shopID)
			resolved := resolutionBatch.resolutionScoped(accountKey, it.SourceItemID, it.SourceVariantID, it.SKU, rawName)
			confirmedBy := ""
			if userID != nil {
				confirmedBy = *userID
			}
			bi, mapped, resolveErr := marketplaceBillItemFromResolution("shopee", accountKey, it, defaultUnit, resolved, resolutionBatch, h.aliasRepo, confirmedBy)
			if resolveErr != nil {
				h.logger.Warn("shopee_import: save product master failed", zap.Error(resolveErr))
				allHigh = false
			}
			exactSKU := normalizeMarketplaceSKU(it.SKU) != "" && resolutionBatch.catalogLookup(it.SKU) != nil
			if mapped {
				recordMarketplaceResolutionUsage(resolutionBatch, resolved, exactSKU)
			}
			if !mapped {
				allHigh = false
			}
			enriched = append(enriched, itemEnriched{item: bi})
		}
		if order.ShippingAmount > 0 && channelDefault != nil {
			shippingCode := strings.TrimSpace(channelDefault.ShippingItemCode)
			shippingUnit := strings.TrimSpace(channelDefault.ShippingItemUnitCode)
			shippingPrice := roundFloat(order.ShippingAmount, 2)
			shippingGross := shippingPrice
			enriched = append(enriched, itemEnriched{item: models.BillItem{
				RawName: "ค่าจัดส่ง Shopee", SourceSKU: models.ShopeeShippingSourceSKU,
				ItemCode: &shippingCode, UnitCode: &shippingUnit, Qty: 1, Price: &shippingPrice,
				GrossAmount: &shippingGross, Mapped: true,
			}})
		}

		status := "pending"
		if !allHigh || order.AmountMismatch {
			status = "needs_review"
		}

		raw := map[string]interface{}{
			"flow":                   sourceFlow,
			"shopee_order_id":        order.OrderID,
			"order_id":               order.OrderID,
			"doc_date":               order.DocDate,
			"order_datetime":         order.OrderDateTime,
			"payment_time":           order.PaymentTime,
			"payment_channel":        order.PaymentChannel,
			"customer_name":          order.BuyerUsername,
			"buyer_username":         order.BuyerUsername,
			"tracking_no":            order.TrackingNo,
			"package_number":         order.PackageNumber,
			"shipping_carrier":       order.ShippingCarrier,
			"cod":                    order.COD,
			"status":                 order.Status,
			"item_count":             order.ItemCount,
			"total_qty":              order.TotalQty,
			"paid_total_amount":      order.PaidAmount,
			"order_total_amount":     order.OrderTotalAmount,
			"item_gross_amount":      order.ItemGrossAmount,
			"line_paid_amount":       order.LinePaidAmount,
			"shipping_amount":        order.ShippingAmount,
			"discount_amount":        order.DiscountAmount,
			"net_product_amount":     order.NetProductAmount,
			"amount_difference":      order.AmountDifference,
			"amount_review_required": order.AmountMismatch,
			"amount_review_reason":   order.AmountReviewReason,
			"has_no_sku":             order.HasNoSKU,
			"no_sku_item_count":      order.NoSKUItemCount,
			"amount_mismatch":        order.AmountMismatch,
			"multi_line":             order.MultiLine,
			"import_run_id":          req.ImportRunID,
			"document_route":         documentRoute,
			"sml_destination":        destinationName,
		}
		if shopID != "" {
			raw["shopee_shop_id"] = shopID
		}
		if connectionID != "" {
			raw["shopee_connection_id"] = connectionID
		}
		if shopLabel != "" {
			raw["shopee_shop_label"] = shopLabel
		}
		rawData, _ := json.Marshal(raw)
		bill := &models.Bill{
			BillType:         "sale",
			Source:           "shopee",
			SourceAccountKey: marketplaceSourceAccountKey("shopee", shopID),
			Status:           status,
			DocumentRoute:    documentRoute,
			AIConfidence:     nil,
			RawData:          rawData,
			SMLOrderID:       order.OrderID,
		}
		if userID != nil {
			bill.CreatedBy = userID
		}
		items := make([]models.BillItem, 0, len(enriched))
		for i := range enriched {
			items = append(items, enriched[i].item)
		}
		raw["amount_source_fingerprint"] = tikTokAmountFingerprint(items)
		rawData, _ = json.Marshal(raw)
		bill.RawData = rawData
		durMs := int(time.Since(confirmStart).Milliseconds())
		audit := models.AuditEntry{
			Action: "bill_created", UserID: userID, Source: sourceFlow, Level: "info",
			TraceID: traceID, DurationMs: &durMs,
			Detail: map[string]interface{}{
				"order_id": order.OrderID, "shopee_shop_id": shopID, "items_count": len(items),
				"all_high_conf": allHigh, "status": status, "amount_mismatch": order.AmountMismatch,
			},
		}
		if err := h.billRepo.CreateWithItemsAndAudit(bill, items, audit); err != nil {
			h.logger.Error("create bill", zap.String("order_id", order.OrderID), zap.Error(err))
			if isDuplicateShopeeBillError(err) {
				billID, _, _ := h.findShopeeOrderBillIDForShop(order.OrderID, shopID)
				results = append(results, ConfirmResult{
					OrderID: order.OrderID,
					Success: false,
					BillID:  billID,
					Message: "order นี้ถูกสร้างไปแล้วระหว่างนำเข้า (ข้าม)",
				})
				continue
			}
			results = append(results, ConfirmResult{
				OrderID: order.OrderID,
				Success: false,
				Message: "บันทึก bill ล้มเหลว: " + err.Error(),
			})
			continue
		}

		results = append(results, ConfirmResult{
			OrderID: order.OrderID,
			Success: true,
			BillID:  bill.ID,
			Message: fmt.Sprintf("สร้าง%sแล้ว (status=%s) — รอตรวจสอบใน %s", destinationName, status, reviewPath),
		})
		h.logger.Info("shopee_excel: bill created",
			zap.String("order_id", order.OrderID),
			zap.String("shopee_shop_id", shopID),
			zap.String("bill_id", bill.ID),
			zap.String("status", status),
		)
	}

	successCount := 0
	for _, r := range results {
		if r.Success {
			successCount++
		}
	}

	if h.auditRepo != nil {
		totalDurMs := int(time.Since(confirmStart).Milliseconds())
		_ = h.auditRepo.Log(models.AuditEntry{
			Action:     "shopee_import_done",
			UserID:     userID,
			Source:     sourceFlow,
			Level:      "info",
			TraceID:    traceID,
			DurationMs: &totalDurMs,
			Detail: map[string]interface{}{
				"total":         len(results),
				"success_count": successCount,
				"fail_count":    len(results) - successCount,
			},
		})
	}
	h.finishShopeeImportRun(req.ImportRunID, successCount, len(results)-successCount, "confirmed")

	c.JSON(http.StatusOK, gin.H{
		"results":       results,
		"success_count": successCount,
		"fail_count":    len(results) - successCount,
		"total":         len(results),
		"message":       destinationName + "ถูกสร้างแล้ว — กรุณาเข้าไปตรวจสอบและกดยืนยันส่งใน " + reviewPath,
	})
}

// ─── Excel Parser ─────────────────────────────────────────────────────────────

func parseShopeeExcel(src interface{ Read([]byte) (int, error) }) ([]ShopeeOrder, []string, int, error) {
	f, err := excelize.OpenReader(src)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("เปิดไฟล์ Excel ไม่ได้: %w", err)
	}
	defer f.Close()
	sheetName := f.GetSheetName(0)
	rows, err := f.GetRows(sheetName)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("อ่าน sheet ไม่ได้: %w", err)
	}
	if len(rows) < 2 {
		return nil, nil, 0, fmt.Errorf("ไฟล์ว่างหรือไม่มีข้อมูล")
	}
	headerRowIdx := 0
	orderIDCandidates := shopeeColCandidates["order_id"]
	for i, row := range rows {
		for _, cell := range row {
			trimmed := strings.TrimSpace(cell)
			for _, candidate := range orderIDCandidates {
				if strings.Contains(trimmed, candidate) {
					headerRowIdx = i
					goto foundHeader
				}
			}
		}
	}
foundHeader:
	headerRow := rows[headerRowIdx]
	colIdx := map[string]int{}
	for field, candidates := range shopeeColCandidates {
		for j, cell := range headerRow {
			trimmed := strings.TrimSpace(cell)
			for _, c := range candidates {
				if strings.Contains(trimmed, c) {
					colIdx[field] = j
					break
				}
			}
			if _, found := colIdx[field]; found {
				break
			}
		}
	}
	required := []string{
		"order_id", "status", "order_date", "product_name", "qty",
		"gross_price", "buyer_product", "shipping_amount", "order_total",
	}
	for _, f := range required {
		if _, ok := colIdx[f]; !ok {
			return nil, nil, 0, fmt.Errorf("ไม่พบ column '%s' ในไฟล์ — columns ที่พบ: %s",
				f, strings.Join(headerRow[:min(len(headerRow), 15)], ", "))
		}
	}
	if len(rows)-headerRowIdx-1 > lazadaMaxRows {
		return nil, nil, 0, fmt.Errorf("ไฟล์มีเกิน %d แถว กรุณาแบ่งไฟล์แล้วนำเข้าใหม่", lazadaMaxRows)
	}
	warnings := []string{}
	orderMap := map[string]*ShopeeOrder{}
	orderKeys := []string{}
	noSKUOrderIDs := map[string]bool{}
	noSKUItemCount := 0
	skippedCount := 0
	for rowOffset, row := range rows[headerRowIdx+1:] {
		if len(row) == 0 {
			continue
		}
		orderID := cellStr(row, colIdx["order_id"])
		if orderID == "" || strings.EqualFold(orderID, "nan") {
			continue
		}
		status := cellStr(row, colIdx["status"])
		if excludeStatuses[status] {
			skippedCount++
			continue
		}
		orderDateTime := cellStr(row, colIdx["order_date"])
		docDate := marketplaceDocDate(orderDateTime)
		if _, exists := orderMap[orderID]; !exists {
			orderMap[orderID] = &ShopeeOrder{
				OrderID:        orderID,
				DocDate:        docDate,
				OrderDateTime:  orderDateTime,
				PaymentTime:    optionalCell(row, colIdx, "payment_time"),
				PaymentChannel: optionalCell(row, colIdx, "payment_channel"),
				BuyerUsername:  optionalCell(row, colIdx, "buyer_username"),
				TrackingNo:     optionalCell(row, colIdx, "tracking_no"),
				Status:         status,
				Items:          []ShopeeExcelItem{},
			}
			orderKeys = append(orderKeys, orderID)
		} else if existing := orderMap[orderID]; existing.Status != status || existing.OrderDateTime != orderDateTime {
			existing.BlockedReason = "ข้อมูลระดับ Order ซ้ำหลายแถวแต่ไม่ตรงกัน กรุณาส่งออกไฟล์ใหม่จาก Shopee"
		}
		order := orderMap[orderID]
		qty := cellFloat(row, colIdx["qty"])
		if qty <= 0 {
			order.BlockedReason = fmt.Sprintf("จำนวนสินค้าแถว %d ต้องมากกว่า 0", headerRowIdx+rowOffset+2)
			qty = 1
		}
		unitGrossCents, grossErr := marketplaceMoneyCents(row, colIdx, "gross_price")
		buyerProductCents, buyerErr := marketplaceMoneyCents(row, colIdx, "buyer_product")
		shippingCents, shippingErr := marketplaceMoneyCents(row, colIdx, "shipping_amount")
		orderTotalCents, totalErr := marketplaceMoneyCents(row, colIdx, "order_total")
		grossCents := centsFromMoney(moneyFromCents(unitGrossCents) * qty)
		discountCents := grossCents - buyerProductCents
		if grossErr != nil || buyerErr != nil || shippingErr != nil || totalErr != nil ||
			grossCents < 0 || buyerProductCents < 0 || shippingCents < 0 || discountCents < 0 {
			order.BlockedReason = fmt.Sprintf("ยอดเงินแถว %d ไม่ถูกต้อง กรุณาตรวจไฟล์ Shopee", headerRowIdx+rowOffset+2)
		}
		if order.OrderTotalAmount != 0 && centsFromMoney(order.OrderTotalAmount) != orderTotalCents {
			order.BlockedReason = "จำนวนเงินทั้งหมดของ Order ซ้ำหลายแถวแต่ไม่ตรงกัน กรุณาส่งออกไฟล์ใหม่จาก Shopee"
		}
		if order.ShippingAmount != 0 && centsFromMoney(order.ShippingAmount) != shippingCents {
			order.BlockedReason = "ค่าจัดส่งของ Order ซ้ำหลายแถวแต่ไม่ตรงกัน กรุณาส่งออกไฟล์ใหม่จาก Shopee"
		}
		order.OrderTotalAmount = moneyFromCents(orderTotalCents)
		order.ShippingAmount = moneyFromCents(shippingCents)
		sku := optionalCell(row, colIdx, "sku")
		productName := cellStr(row, colIdx["product_name"])
		optionName := optionalCell(row, colIdx, "option_name")
		rawName := shopeeItemRawName(productName, optionName, "")
		noSKU := sku == "" || strings.EqualFold(sku, "nan")
		if noSKU {
			noSKUOrderIDs[orderID] = true
			noSKUItemCount++
			orderMap[orderID].HasNoSKU = true
			orderMap[orderID].NoSKUItemCount++
		}
		gross := moneyFromCents(grossCents)
		discount := moneyFromCents(discountCents)
		price := 0.0
		if qty > 0 {
			price = gross / qty
		}
		order.Items = append(order.Items, ShopeeExcelItem{
			SKU:            sku,
			ProductName:    productName,
			OptionName:     optionName,
			RawName:        rawName,
			Price:          price,
			GrossAmount:    gross,
			DiscountAmount: discount,
			Qty:            qty,
			NoSKU:          noSKU,
		})
		order.ItemGrossAmount += gross
		order.DiscountAmount += discount
		order.NetProductAmount += moneyFromCents(buyerProductCents)
		order.LinePaidAmount += moneyFromCents(buyerProductCents)
	}
	var orders []ShopeeOrder
	for _, id := range orderKeys {
		o := orderMap[id]
		if len(o.Items) == 0 {
			warnings = append(warnings, fmt.Sprintf("Order %s: ไม่มีสินค้า — ข้ามไป", id))
			continue
		}
		o.ItemCount = len(o.Items)
		for _, it := range o.Items {
			o.TotalQty += it.Qty
		}
		o.MultiLine = len(o.Items) > 1
		expected := roundFloat(o.NetProductAmount+o.ShippingAmount, 2)
		if o.OrderTotalAmount <= 0 {
			o.OrderTotalAmount = expected
		}
		o.PaidAmount = o.OrderTotalAmount
		o.AmountDifference = roundFloat(o.PaidAmount-expected, 2)
		o.AmountMismatch = math.Abs(o.AmountDifference) > 0.01
		if o.AmountMismatch {
			o.AmountReviewReason = fmt.Sprintf("Order %s: ยอดสินค้าที่ลูกค้าจ่าย %.2f + ค่าส่ง %.2f = %.2f แต่จำนวนเงินทั้งหมด %.2f",
				id, o.NetProductAmount, o.ShippingAmount, expected, o.PaidAmount)
		}
		orders = append(orders, *o)
	}

	if noSKUItemCount > 0 {
		warnings = append(warnings,
			fmt.Sprintf("พบ %d รายการสินค้าใน %d order ที่ไม่มี SKU — ระบบจะใช้ชื่อสินค้า + ตัวเลือกสินค้าในการจับคู่แทน", noSKUItemCount, len(noSKUOrderIDs)))
	}
	if skippedCount > 0 {
		warnings = append([]string{fmt.Sprintf("กรอง %d แถว (สถานะ: ยกเลิกแล้ว)", skippedCount)}, warnings...)
	}

	return orders, warnings, skippedCount, nil
}

func marketplaceDocDate(raw string) string {
	raw = strings.TrimSpace(raw)
	layouts := []string{
		"02/01/2006 15:04:05", "02/01/2006 15:04", "02/01/2006",
		"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02",
		"02 Jan 2006 15:04", "2 Jan 2006 15:04",
	}
	for _, layout := range layouts {
		if parsed, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return parsed.Format("2006-01-02")
		}
	}
	return time.Now().Format("2006-01-02")
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func (h *ShopeeImportHandler) existsShopeeOrder(orderID string) (bool, error) {
	_, exists, err := h.findShopeeOrderBillID(orderID)
	return exists, err
}

func (h *ShopeeImportHandler) findShopeeOrderBillID(orderID string) (string, bool, error) {
	return h.findShopeeOrderBillIDForShop(orderID, "")
}

func (h *ShopeeImportHandler) findShopeeOrderBillIDForShop(orderID, shopID string) (string, bool, error) {
	if strings.TrimSpace(orderID) == "" {
		return "", false, nil
	}
	var id string
	orderID = strings.TrimSpace(orderID)
	shopID = strings.TrimSpace(shopID)
	var err error
	if shopID != "" {
		err = h.billRepo.DB().QueryRow(
			`SELECT id::text
			   FROM bills
			  WHERE source = 'shopee'
			    AND archived_at IS NULL
			    AND (raw_data->>'order_id' = $1 OR sml_order_id = $1)
			    AND (
			      raw_data->>'shopee_shop_id' = $2
			      OR COALESCE(raw_data->>'shopee_shop_id', '') = ''
			    )
			  ORDER BY created_at DESC
			  LIMIT 1`,
			orderID, shopID,
		).Scan(&id)
	} else {
		err = h.billRepo.DB().QueryRow(
			`SELECT id::text
			   FROM bills
			  WHERE source = 'shopee'
			    AND archived_at IS NULL
			    AND (raw_data->>'order_id' = $1 OR sml_order_id = $1)
			    AND COALESCE(raw_data->>'shopee_shop_id', '') = ''
			  ORDER BY created_at DESC
			  LIMIT 1`,
			orderID,
		).Scan(&id)
	}
	if err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, err
	}
	return id, true, nil
}

type shopeeRealtimeImportState struct {
	ShopID      string
	OrderStatus string
	ERPStatus   string
	BillID      string
}

func (h *ShopeeImportHandler) loadRealtimeImportStates(ctx context.Context, orderIDs []string, shopID string) (map[string]shopeeRealtimeImportState, error) {
	out := map[string]shopeeRealtimeImportState{}
	if h == nil || h.billRepo == nil {
		return out, nil
	}
	seen := map[string]bool{}
	ids := make([]string, 0, len(orderIDs))
	for _, id := range orderIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return out, nil
	}
	shopID = strings.TrimSpace(shopID)
	rows, err := h.billRepo.DB().QueryContext(ctx,
		`SELECT shop_id::text, order_sn, order_status, erp_status, bill_id::text
		   FROM shopee_order_snapshots
		  WHERE order_sn = ANY($1)
		    AND ($2 = '' OR shop_id::text = $2)`,
		pq.Array(ids), shopID,
	)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var orderSN string
		var state shopeeRealtimeImportState
		var billID sql.NullString
		if err := rows.Scan(&state.ShopID, &orderSN, &state.OrderStatus, &state.ERPStatus, &billID); err != nil {
			return out, err
		}
		if billID.Valid {
			state.BillID = billID.String
		}
		if strings.TrimSpace(orderSN) != "" {
			out[strings.TrimSpace(orderSN)] = state
		}
	}
	return out, rows.Err()
}

func (h *ShopeeImportHandler) markRealtimeManagedOrders(ctx context.Context, orders []ShopeeOrder, shopID string) error {
	ids := make([]string, 0, len(orders))
	for _, order := range orders {
		ids = append(ids, order.OrderID)
	}
	states, err := h.loadRealtimeImportStates(ctx, ids, shopID)
	if err != nil {
		return err
	}
	for i := range orders {
		state, ok := states[strings.TrimSpace(orders[i].OrderID)]
		if !ok {
			continue
		}
		orders[i].BlockedReason = "realtime_managed"
		orders[i].RealtimeStatus = state.OrderStatus
		orders[i].RealtimeBillID = state.BillID
		orders[i].ActionURL = shopeeRealtimeOrderURL(orders[i].OrderID)
		if orders[i].ExistingBillID == "" && state.BillID != "" {
			orders[i].ExistingBillID = state.BillID
		}
	}
	return nil
}

func shopeeRealtimeOrderURL(orderID string) string {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return "/shopee-operations"
	}
	return "/shopee-operations?order=" + url.QueryEscape(orderID)
}

func buildShopeePreflight(orders []ShopeeOrder, skippedCount, duplicateCount int) ShopeeImportPreflight {
	p := ShopeeImportPreflight{
		DuplicateOrders: duplicateCount,
		SkippedRows:     skippedCount,
	}
	excluded := 0
	for _, o := range orders {
		if o.Duplicate || strings.TrimSpace(o.BlockedReason) != "" {
			excluded++
		}
		if o.BlockedReason == "realtime_managed" {
			p.RealtimeManagedOrders++
		}
		if o.HasNoSKU {
			p.NoSKUOrders++
			p.NoSKUItems += o.NoSKUItemCount
		}
		if o.MultiLine {
			p.MultiItemOrders++
		}
		if o.AmountMismatch {
			p.AmountMismatchOrders++
		}
	}
	p.NewOrders = len(orders) - excluded
	if p.NewOrders < 0 {
		p.NewOrders = 0
	}
	return p
}

func (h *ShopeeImportHandler) createShopeeImportRun(c *gin.Context, filename, fileToken string, orders []ShopeeOrder, warnings []string, preflight ShopeeImportPreflight) string {
	if h == nil || h.billRepo == nil {
		return ""
	}
	var userID interface{}
	if uid := c.GetString("user_id"); uid != "" {
		userID = uid
	}
	var periodStart, periodEnd interface{}
	for _, o := range orders {
		t, err := time.Parse("2006-01-02", o.DocDate)
		if err != nil {
			continue
		}
		if periodStart == nil || t.Before(periodStart.(time.Time)) {
			periodStart = t
		}
		if periodEnd == nil || t.After(periodEnd.(time.Time)) {
			periodEnd = t
		}
	}
	detail, _ := json.Marshal(map[string]interface{}{
		"preflight": preflight,
		"warnings":  warnings,
	})
	var id string
	err := h.billRepo.DB().QueryRow(
		`INSERT INTO import_runs
		   (source, filename, file_sha256, period_start, period_end,
		    total_orders, new_orders, duplicate_orders, skipped_orders,
		    warning_count, status, detail, created_by)
		 VALUES
		   ('shopee', $1, $2, $3, $4, $5, $6, $7, $8, $9, 'preview', $10, $11)
		 RETURNING id::text`,
		filename,
		fileToken,
		periodStart,
		periodEnd,
		len(orders),
		preflight.NewOrders,
		preflight.DuplicateOrders,
		preflight.SkippedRows,
		len(warnings),
		detail,
		userID,
	).Scan(&id)
	if err != nil {
		h.logger.Warn("shopee_excel: create import run failed", zap.Error(err))
		return ""
	}
	return id
}

func (h *ShopeeImportHandler) finishShopeeImportRun(id string, createdCount, failedCount int, status string) {
	if h == nil || h.billRepo == nil || strings.TrimSpace(id) == "" {
		return
	}
	if status == "" {
		status = "confirmed"
	}
	if _, err := h.billRepo.DB().Exec(
		`UPDATE import_runs
		    SET created_count = $2,
		        failed_count = $3,
		        status = $4,
		        confirmed_at = NOW()
		  WHERE id = $1`,
		id,
		createdCount,
		failedCount,
		status,
	); err != nil {
		h.logger.Warn("shopee_excel: update import run failed", zap.String("import_run_id", id), zap.Error(err))
	}
}

func (h *ShopeeImportHandler) CreateBillFromShopeeOrder(ctx context.Context, order ShopeeOrder, opts ShopeeBillCreateOptions) (ConfirmResult, error) {
	if h == nil || h.billRepo == nil {
		return ConfirmResult{OrderID: order.OrderID, Success: false, Message: "Shopee import handler ยังไม่พร้อม"}, fmt.Errorf("shopee import handler is not ready")
	}
	if strings.TrimSpace(order.OrderID) == "" {
		return ConfirmResult{Success: false, Message: "order_id ว่าง"}, fmt.Errorf("order_id is required")
	}
	sourceFlow := strings.TrimSpace(opts.SourceFlow)
	if sourceFlow == "" {
		sourceFlow = "shopee_realtime"
	}
	documentRoute := shopeeImportRoute(opts.Config)
	destinationName := shopeeImportDocumentName(opts.Config)
	reviewPath := shopeeImportReviewPath(opts.Config)
	defaultUnit := opts.Config.UnitCode
	channel := "shopee"
	if sourceFlow == "shopee_realtime" {
		channel = "shopee_realtime"
	}
	var channelDefault *models.ChannelDefault
	if h.channelDefaults != nil {
		channelDefault, _ = h.channelDefaults.Get(channel, "sale")
	}
	if err := validateShopeeShippingConfig([]ShopeeOrder{order}, channelDefault, "Shopee"); err != nil {
		return ConfirmResult{OrderID: order.OrderID, Success: false, Message: err.Error()}, err
	}

	shopID := strings.TrimSpace(order.ShopeeShopID)
	shopLabel := strings.TrimSpace(order.ShopeeShopLabel)
	connectionID := strings.TrimSpace(order.ShopeeConnectionID)
	if opts.Connection != nil {
		shopID = strconv.FormatInt(opts.Connection.ShopID, 10)
		shopLabel = opts.Connection.DisplayLabel()
		connectionID = opts.Connection.ID
	}
	if billID, exists, err := h.findShopeeOrderBillIDForShop(order.OrderID, shopID); err != nil {
		return ConfirmResult{OrderID: order.OrderID, Success: false, Message: "ตรวจ duplicate order ไม่สำเร็จ: " + err.Error()}, err
	} else if exists {
		return ConfirmResult{
			OrderID: order.OrderID,
			Success: false,
			BillID:  billID,
			Message: "order นี้มีอยู่ในระบบแล้ว (reuse)",
		}, nil
	}

	type itemEnriched struct{ item models.BillItem }
	activeCatalog, err := h.catalogRepo.CountActive()
	if err != nil || activeCatalog == 0 {
		return ConfirmResult{OrderID: order.OrderID, Success: false, Message: "ยังไม่มีข้อมูลสินค้า SML กรุณารีเฟรชสินค้า SML ก่อน"}, fmt.Errorf("catalog_not_ready")
	}
	resolutionBatch, err := prepareMarketplaceResolution(
		ctx, "shopee", []ShopeeOrder{order}, nil,
		h.catalogRepo, h.catalogSvc, h.aliasRepo, h.mappingRepo, h.logger,
	)
	if err != nil {
		return ConfirmResult{OrderID: order.OrderID, Success: false, Message: "เตรียมข้อมูลสินค้า SML ไม่สำเร็จ"}, err
	}
	defer flushMarketplaceResolutionUsage(resolutionBatch, h.aliasRepo, h.logger)
	enriched := []itemEnriched{}
	allHigh := true

	for _, it := range order.Items {
		rawName := shopeeItemRawName(it.ProductName, it.OptionName, it.RawName)
		accountKey := marketplaceSourceAccountKey("shopee", shopID)
		resolved := resolutionBatch.resolutionScoped(accountKey, it.SourceItemID, it.SourceVariantID, it.SKU, rawName)
		confirmedBy := ""
		if opts.UserID != nil {
			confirmedBy = *opts.UserID
		}
		bi, mapped, resolveErr := marketplaceBillItemFromResolution("shopee", accountKey, it, defaultUnit, resolved, resolutionBatch, h.aliasRepo, confirmedBy)
		if resolveErr != nil {
			return ConfirmResult{OrderID: order.OrderID, Success: false, Message: "บันทึก Product Master ไม่สำเร็จ"}, resolveErr
		}
		exactSKU := normalizeMarketplaceSKU(it.SKU) != "" && resolutionBatch.catalogLookup(it.SKU) != nil
		if mapped {
			recordMarketplaceResolutionUsage(resolutionBatch, resolved, exactSKU)
		}
		if !mapped {
			allHigh = false
		}
		enriched = append(enriched, itemEnriched{item: bi})
	}
	if order.ShippingAmount > 0 && channelDefault != nil {
		shippingCode := strings.TrimSpace(channelDefault.ShippingItemCode)
		shippingUnit := strings.TrimSpace(channelDefault.ShippingItemUnitCode)
		shippingPrice := roundFloat(order.ShippingAmount, 2)
		shippingGross := shippingPrice
		enriched = append(enriched, itemEnriched{item: models.BillItem{
			RawName: "ค่าจัดส่ง Shopee", SourceSKU: models.ShopeeShippingSourceSKU,
			ItemCode: &shippingCode, UnitCode: &shippingUnit, Qty: 1, Price: &shippingPrice,
			GrossAmount: &shippingGross, Mapped: true,
		}})
	}

	if len(enriched) == 0 {
		return ConfirmResult{OrderID: order.OrderID, Success: false, Message: "order นี้ไม่มี item ที่สร้างบิลได้"}, fmt.Errorf("order has no importable items")
	}

	status := "pending"
	if !allHigh || order.AmountMismatch {
		status = "needs_review"
	}

	raw := map[string]interface{}{
		"flow":                   sourceFlow,
		"shopee_order_id":        order.OrderID,
		"order_id":               order.OrderID,
		"doc_date":               order.DocDate,
		"order_datetime":         order.OrderDateTime,
		"payment_time":           order.PaymentTime,
		"payment_channel":        order.PaymentChannel,
		"customer_name":          order.BuyerUsername,
		"buyer_username":         order.BuyerUsername,
		"tracking_no":            order.TrackingNo,
		"package_number":         order.PackageNumber,
		"shipping_carrier":       order.ShippingCarrier,
		"cod":                    order.COD,
		"status":                 order.Status,
		"item_count":             order.ItemCount,
		"total_qty":              order.TotalQty,
		"paid_total_amount":      order.PaidAmount,
		"order_total_amount":     order.OrderTotalAmount,
		"item_gross_amount":      order.ItemGrossAmount,
		"line_paid_amount":       order.LinePaidAmount,
		"shipping_amount":        order.ShippingAmount,
		"discount_amount":        order.DiscountAmount,
		"net_product_amount":     order.NetProductAmount,
		"amount_difference":      order.AmountDifference,
		"amount_review_required": order.AmountMismatch,
		"amount_review_reason":   order.AmountReviewReason,
		"has_no_sku":             order.HasNoSKU,
		"no_sku_item_count":      order.NoSKUItemCount,
		"amount_mismatch":        order.AmountMismatch,
		"multi_line":             order.MultiLine,
		"import_run_id":          opts.ImportRunID,
		"document_route":         documentRoute,
		"sml_destination":        destinationName,
	}
	if shopID != "" {
		raw["shopee_shop_id"] = shopID
	}
	if connectionID != "" {
		raw["shopee_connection_id"] = connectionID
	}
	if shopLabel != "" {
		raw["shopee_shop_label"] = shopLabel
	}
	items := make([]models.BillItem, 0, len(enriched))
	for i := range enriched {
		items = append(items, enriched[i].item)
	}
	raw["amount_source_fingerprint"] = tikTokAmountFingerprint(items)
	rawData, _ := json.Marshal(raw)
	bill := &models.Bill{
		BillType:         "sale",
		Source:           "shopee",
		SourceAccountKey: marketplaceSourceAccountKey("shopee", shopID),
		Status:           status,
		DocumentRoute:    documentRoute,
		AIConfidence:     nil,
		RawData:          rawData,
		SMLOrderID:       order.OrderID,
	}
	if opts.UserID != nil {
		bill.CreatedBy = opts.UserID
	}
	started := opts.StartedAt
	if started.IsZero() {
		started = time.Now()
	}
	durMs := int(time.Since(started).Milliseconds())
	audit := models.AuditEntry{
		Action: "bill_created", UserID: opts.UserID, Source: sourceFlow, Level: "info",
		TraceID: opts.TraceID, DurationMs: &durMs,
		Detail: map[string]interface{}{
			"order_id": order.OrderID, "shopee_shop_id": shopID, "items_count": len(items),
			"all_high_conf": allHigh, "status": status, "via": sourceFlow,
			"amount_mismatch": order.AmountMismatch,
		},
	}
	if err := h.billRepo.CreateWithItemsAndAudit(bill, items, audit); err != nil {
		if isDuplicateShopeeBillError(err) {
			billID, _, _ := h.findShopeeOrderBillIDForShop(order.OrderID, shopID)
			return ConfirmResult{
				OrderID: order.OrderID,
				Success: false,
				BillID:  billID,
				Message: "order นี้ถูกสร้างไปแล้วระหว่างนำเข้า (reuse)",
			}, nil
		}
		h.logger.Error("shopee_realtime: create bill failed", zap.String("order_id", order.OrderID), zap.Error(err))
		return ConfirmResult{OrderID: order.OrderID, Success: false, Message: "บันทึก bill ล้มเหลว: " + err.Error()}, err
	}

	h.logger.Info("shopee_realtime: bill created",
		zap.String("order_id", order.OrderID),
		zap.String("shopee_shop_id", shopID),
		zap.String("bill_id", bill.ID),
		zap.String("status", status),
	)

	return ConfirmResult{
		OrderID: order.OrderID,
		Success: true,
		BillID:  bill.ID,
		Message: fmt.Sprintf("สร้าง%sแล้ว (status=%s) — รอตรวจสอบใน %s", destinationName, status, reviewPath),
	}, nil
}

func isDuplicateShopeeBillError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "bills_shopee_order_id_unique") ||
		(strings.Contains(msg, "duplicate key") && strings.Contains(msg, "order_id"))
}

func shopeeItemRawName(productName, optionName, rawName string) string {
	rawName = strings.TrimSpace(rawName)
	if rawName != "" {
		return rawName
	}
	productName = strings.TrimSpace(productName)
	optionName = strings.TrimSpace(optionName)
	if productName == "" {
		return optionName
	}
	if optionName == "" || optionName == "-" {
		return productName
	}
	return productName + " / " + optionName
}

func shopeeImportDocumentName(cfg ShopeeConfigRequest) string {
	if shopeeImportRoute(cfg) == "saleinvoice" {
		return "เอกสารขายสินค้าและบริการ"
	}
	return "ใบสั่งขาย"
}

func shopeeImportReviewPath(cfg ShopeeConfigRequest) string {
	if shopeeImportRoute(cfg) == "saleinvoice" {
		return "/sale-invoices"
	}
	return "/sales-orders"
}

func shopeeImportRoute(cfg ShopeeConfigRequest) string {
	route := strings.ToLower(strings.TrimSpace(cfg.Endpoint + " " + cfg.DocFormat))
	compact := strings.NewReplacer("-", "", "_", "", "/", "", " ", "").Replace(route)
	docFormat := strings.ToUpper(strings.TrimSpace(cfg.DocFormat))
	if strings.Contains(compact, "saleinvoice") ||
		strings.Contains(route, "sale-invoice") ||
		strings.Contains(route, "sale-invoices") ||
		strings.Contains(route, "sale_invoice") ||
		strings.Contains(route, "sale_invoices") ||
		strings.Contains(route, " si") ||
		docFormat == "SI" ||
		strings.Contains(docFormat, "INV") {
		return "saleinvoice"
	}
	return "saleorder"
}

func (h *ShopeeImportHandler) lookupCatalogItem(code string) *models.CatalogItem {
	code = strings.TrimSpace(code)
	if code == "" || h.catalogRepo == nil {
		return nil
	}
	item, err := h.catalogRepo.GetActive(code)
	if err != nil {
		h.logger.Warn("shopee_excel: catalog sku lookup failed",
			zap.String("sku", code),
			zap.Error(err))
		return nil
	}
	return item
}

func roundFloat(v float64, digits int) float64 {
	pow := math.Pow(10, float64(digits))
	return math.Round(v*pow) / pow
}

func cellStr(row []string, idx int) string {
	if idx >= 0 && idx < len(row) {
		v := strings.TrimSpace(row[idx])
		if strings.EqualFold(v, "nan") {
			return ""
		}
		return v
	}
	return ""
}

func optionalCell(row []string, colIdx map[string]int, key string) string {
	if idx, ok := colIdx[key]; ok {
		return cellStr(row, idx)
	}
	return ""
}

func cellFloat(row []string, idx int) float64 {
	s := cellStr(row, idx)
	if s == "" {
		return 0
	}
	// Remove commas (Thai number formatting)
	s = strings.ReplaceAll(s, ",", "")
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func optionalCellFloat(row []string, colIdx map[string]int, key string) float64 {
	if idx, ok := colIdx[key]; ok {
		return cellFloat(row, idx)
	}
	return 0
}

func strPtr(s string) *string { return &s }

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
