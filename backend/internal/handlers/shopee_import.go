package handlers

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
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
	db              *sql.DB
	billRepo        *repository.BillRepo
	mappingRepo     *repository.MappingRepo
	auditRepo       *repository.AuditLogRepo
	cfg             *config.Config
	channelDefaults *repository.ChannelDefaultRepo
	catalogSvc      *catalog.SMLCatalogService
	embSvc          *catalog.EmbeddingService
	catalogIdx      *catalog.CatalogIndex
	catalogRepo     *repository.SMLCatalogRepo
	artifactSvc     *artifact.Service
	logger          *zap.Logger

	// Pending uploads keyed by SHA-256 — Preview stashes the raw .xlsx so
	// Confirm (a separate JSON request) can attach it as an artifact to
	// every bill it creates. Entries are removed after Confirm or by the
	// cleanup goroutine when older than 30 minutes.
	pendingUploads sync.Map
}

type pendingUpload struct {
	bytes      []byte
	filename   string
	uploadedAt time.Time
}

const pendingUploadTTL = 30 * time.Minute

func NewShopeeImportHandler(
	db *sql.DB,
	billRepo *repository.BillRepo,
	mappingRepo *repository.MappingRepo,
	auditRepo *repository.AuditLogRepo,
	cfg *config.Config,
	channelDefaults *repository.ChannelDefaultRepo,
	catalogSvc *catalog.SMLCatalogService,
	embSvc *catalog.EmbeddingService,
	catalogIdx *catalog.CatalogIndex,
	catalogRepo *repository.SMLCatalogRepo,
	logger *zap.Logger,
) *ShopeeImportHandler {
	h := &ShopeeImportHandler{
		db:              db,
		billRepo:        billRepo,
		mappingRepo:     mappingRepo,
		auditRepo:       auditRepo,
		cfg:             cfg,
		channelDefaults: channelDefaults,
		catalogSvc:      catalogSvc,
		embSvc:          embSvc,
		catalogIdx:      catalogIdx,
		catalogRepo:     catalogRepo,
		logger:          logger,
	}
	go h.gcPendingUploads()
	return h
}

// SetArtifactService wires source-artifact storage so the original .xlsx
// gets archived next to every bill the import creates.
func (h *ShopeeImportHandler) SetArtifactService(svc *artifact.Service) {
	h.artifactSvc = svc
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
	"price":           {"ราคาขาย"},
	"qty":             {"จำนวน"},
	"paid_amount":     {"ยอดชำระเงิน"},
	"order_total":     {"จำนวนเงินทั้งหมด", "Order Total"},
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
	SKU         string  `json:"sku"`
	LazadaSKU   string  `json:"lazada_sku,omitempty"`
	TikTokSKU   string  `json:"tiktok_sku,omitempty"`
	OrderItemID string  `json:"order_item_id,omitempty"`
	ProductName string  `json:"product_name"`
	OptionName  string  `json:"option_name,omitempty"`
	RawName     string  `json:"raw_name"`
	Price       float64 `json:"price"`
	Qty         float64 `json:"qty"`
	NoSKU       bool    `json:"no_sku,omitempty"`
}

// ShopeeOrder is one parsed Shopee order (returned in preview).
type ShopeeOrder struct {
	OrderID            string            `json:"order_id"`
	DocDate            string            `json:"doc_date"`
	OrderDateTime      string            `json:"order_datetime,omitempty"`
	PaymentTime        string            `json:"payment_time,omitempty"`
	PaymentChannel     string            `json:"payment_channel,omitempty"`
	BuyerUsername      string            `json:"buyer_username,omitempty"`
	TrackingNo         string            `json:"tracking_no,omitempty"`
	PackageNumber      string            `json:"package_number,omitempty"`
	ShippingCarrier    string            `json:"shipping_carrier,omitempty"`
	COD                bool              `json:"cod,omitempty"`
	Status             string            `json:"status"`
	Items              []ShopeeExcelItem `json:"items"`
	ItemCount          int               `json:"item_count"`
	TotalQty           float64           `json:"total_qty"`
	PaidAmount         float64           `json:"paid_amount,omitempty"`
	OrderTotalAmount   float64           `json:"order_total_amount,omitempty"`
	ItemGrossAmount    float64           `json:"item_gross_amount,omitempty"`
	LinePaidAmount     float64           `json:"line_paid_amount,omitempty"`
	ShippingAmount     float64           `json:"shipping_amount,omitempty"`
	DiscountAmount     float64           `json:"discount_amount,omitempty"`
	NoSKUItemCount     int               `json:"no_sku_item_count,omitempty"`
	HasNoSKU           bool              `json:"has_no_sku,omitempty"`
	MultiLine          bool              `json:"multi_line,omitempty"`
	AmountMismatch     bool              `json:"amount_mismatch,omitempty"`
	ExistingBillID     string            `json:"existing_bill_id,omitempty"`
	ShopeeShopID       string            `json:"shopee_shop_id,omitempty"`
	ShopeeConnectionID string            `json:"shopee_connection_id,omitempty"`
	ShopeeShopLabel    string            `json:"shopee_shop_label,omitempty"`
	// preview-only
	Duplicate bool `json:"duplicate"`
}

type ShopeeImportPreflight struct {
	NewOrders            int `json:"new_orders"`
	DuplicateOrders      int `json:"duplicate_orders"`
	SkippedRows          int `json:"skipped_rows"`
	NoSKUOrders          int `json:"no_sku_orders"`
	NoSKUItems           int `json:"no_sku_items"`
	MultiItemOrders      int `json:"multi_item_orders"`
	AmountMismatchOrders int `json:"amount_mismatch_orders"`
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
	// FileToken — SHA-256 of the uploaded .xlsx, returned so Confirm
	// can re-attach the same bytes as an artifact to every bill it
	// creates. Empty when artifact storage is disabled.
	FileToken string `json:"file_token,omitempty"`
}

// ConfirmRequest is sent by the frontend for POST /api/import/shopee/confirm
type ConfirmRequest struct {
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
	OrderID string `json:"order_id"`
	Success bool   `json:"success"`
	DocNo   string `json:"doc_no,omitempty"`
	Message string `json:"message,omitempty"`
	BillID  string `json:"bill_id,omitempty"`
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

// ─── GET /api/settings/shopee-config ─────────────────────────────────────────

// GetConfig returns the active Shopee SML config — env defaults overlaid with
// per-channel overrides from channel_defaults (shopee, sale). Read-only: the
// /import/shopee page renders this as a summary card so users see what'll
// actually be sent on Retry.
func (h *ShopeeImportHandler) GetConfig(c *gin.Context) {
	custCode := ""
	whCode := h.cfg.ShopeeSMLWHCode
	shelfCode := h.cfg.ShopeeSMLShelfCode
	vatType := h.cfg.ShopeeSMLVATType
	vatRate := h.cfg.ShopeeSMLVATRate
	docFormat := h.cfg.ShopeeSMLDocFormat
	endpoint := ""
	if h.channelDefaults != nil {
		if def, _ := h.channelDefaults.Get("shopee", "sale"); def != nil {
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
	c.JSON(http.StatusOK, ShopeeConfigRequest{
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
	})
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

	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "เปิดไฟล์ไม่ได้"})
		return
	}
	defer file.Close()

	// Read once into memory so we can both parse it and stash the bytes
	// for Confirm to archive as an artifact.
	rawBytes, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "อ่านไฟล์ไม่ได้"})
		return
	}

	orders, warnings, skippedCount, err := parseShopeeExcel(bytes.NewReader(rawBytes))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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

	// Compute file token + stash for Confirm. Skip when no artifact service
	// is wired (early dev mode or tests).
	var fileToken string
	if h.artifactSvc != nil {
		sum := sha256.Sum256(rawBytes)
		fileToken = hex.EncodeToString(sum[:])
		h.pendingUploads.Store(fileToken, &pendingUpload{
			bytes:      rawBytes,
			filename:   fileHeader.Filename,
			uploadedAt: time.Now(),
		})
	}

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
	preflight := buildShopeePreflight(orders, skippedCount, dupCount)
	importRunID := h.createShopeeImportRun(c, fileHeader.Filename, fileToken, orders, warnings, preflight)

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
		NewCount:       len(orders) - dupCount,
		DuplicateCount: dupCount,
		SkippedCount:   skippedCount,
		ImportRunID:    importRunID,
		Preflight:      preflight,
		FileToken:      fileToken,
	})
}

// ─── POST /api/import/shopee/confirm ─────────────────────────────────────────

// Confirm processes the selected orders: calls SML 224 and saves bills to DB.
func (h *ShopeeImportHandler) Confirm(c *gin.Context) {
	var req ConfirmRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request ไม่ถูกต้อง: " + err.Error()})
		return
	}

	selectedSet := make(map[string]bool, len(req.OrderIDs))
	for _, id := range req.OrderIDs {
		selectedSet[id] = true
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

	// Default unit code from the request config; used as a fallback when
	// catalog matching doesn't pick a specific unit.
	defaultUnit := req.Config.UnitCode

	var userID *string
	if uid := c.GetString("user_id"); uid != "" {
		userID = &uid
	}
	traceID := c.GetString("trace_id")
	confirmStart := time.Now()

	const topK = 5
	const highConfThreshold = 0.85
	type matchResolution struct {
		learned *models.Mapping
		matches []models.CatalogMatch
	}
	resolutionCache := map[string]matchResolution{}

	// Pull the original .xlsx bytes once so we can attach the same artifact
	// to every bill the import creates. May be nil when artifact service is
	// off, when the user re-confirmed long after Preview, or when running
	// against a Confirm request that didn't go through the new Preview.
	var (
		uploadBytes    []byte
		uploadFilename string
	)
	if h.artifactSvc != nil && req.FileToken != "" {
		if v, ok := h.pendingUploads.LoadAndDelete(req.FileToken); ok {
			if pu, ok := v.(*pendingUpload); ok {
				uploadBytes = pu.bytes
				uploadFilename = pu.filename
			}
		}
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
		type itemEnriched struct {
			item       models.BillItem
			candidates []models.CatalogMatch
		}
		var enriched []itemEnriched
		allHigh := true

		for _, it := range order.Items {
			rawName := shopeeItemRawName(it.ProductName, it.OptionName, it.RawName)
			resolved, ok := resolutionCache[rawName]
			if !ok {
				if h.mappingRepo != nil {
					if m, err := h.mappingRepo.FindByRawName(rawName); err == nil {
						resolved.learned = m
					} else {
						h.logger.Warn("shopee_excel: lookup mapping failed",
							zap.String("raw_name", rawName),
							zap.Error(err))
					}
				}
				if resolved.learned == nil && h.embSvc != nil && h.embSvc.IsConfigured() && h.catalogIdx != nil && h.catalogIdx.Size() > 0 {
					if emb, err := h.embSvc.EmbedText(rawName); err == nil {
						resolved.matches = h.catalogIdx.Search(emb, topK)
					} else {
						h.logger.Warn("shopee_excel: embedding lookup failed",
							zap.String("raw_name", rawName),
							zap.Error(err))
					}
				}
				if resolved.learned == nil && len(resolved.matches) == 0 && h.catalogSvc != nil {
					resolved.matches, _ = h.catalogSvc.SearchByText(rawName, topK)
				}
				resolutionCache[rawName] = resolved
			}
			matches := resolved.matches

			price := it.Price
			bi := models.BillItem{
				RawName:   rawName,
				SourceSKU: it.SKU,
				Qty:       it.Qty,
				Price:     &price,
			}

			// Priority:
			// 1. Human/F1 mapping from /mappings. This is the user's source
			//    of truth and must win over Shopee SKU guesses.
			// 2. High-confidence catalog match.
			// 3. Excel SKU only when it exists in SML catalog. Otherwise keep
			//    it as source_sku, not item_code, so Shopee SKU cannot masquerade
			//    as an SML product code.
			switch {
			case resolved.learned != nil:
				bi.ItemCode = &resolved.learned.ItemCode
				bi.UnitCode = &resolved.learned.UnitCode
				bi.MappingID = &resolved.learned.ID
				bi.Mapped = true
				_ = h.mappingRepo.IncrementUsage(resolved.learned.ID)
			case len(matches) > 0 && matches[0].Score >= highConfThreshold:
				bi.ItemCode = &matches[0].ItemCode
				unit := matches[0].UnitCode
				if unit == "" {
					unit = defaultUnit
				}
				bi.UnitCode = &unit
				bi.Mapped = true
			case it.SKU != "":
				if cat := h.lookupCatalogItem(it.SKU); cat != nil {
					code := cat.ItemCode
					unit := cat.UnitCode
					if unit == "" {
						unit = defaultUnit
					}
					bi.ItemCode = &code
					bi.UnitCode = &unit
					bi.Mapped = true
				} else {
					bi.Mapped = false
					allHigh = false
				}
			default:
				if len(matches) > 0 {
					bi.ItemCode = &matches[0].ItemCode
					unit := matches[0].UnitCode
					if unit == "" {
						unit = defaultUnit
					}
					bi.UnitCode = &unit
				}
				bi.Mapped = false
				allHigh = false
			}

			enriched = append(enriched, itemEnriched{item: bi, candidates: matches})
		}

		status := "pending"
		if !allHigh {
			status = "needs_review"
		}

		aiConf := 1.0
		raw := map[string]interface{}{
			"flow":               sourceFlow,
			"shopee_order_id":    order.OrderID,
			"order_id":           order.OrderID,
			"doc_date":           order.DocDate,
			"order_datetime":     order.OrderDateTime,
			"payment_time":       order.PaymentTime,
			"payment_channel":    order.PaymentChannel,
			"customer_name":      order.BuyerUsername,
			"buyer_username":     order.BuyerUsername,
			"tracking_no":        order.TrackingNo,
			"package_number":     order.PackageNumber,
			"shipping_carrier":   order.ShippingCarrier,
			"cod":                order.COD,
			"status":             order.Status,
			"item_count":         order.ItemCount,
			"total_qty":          order.TotalQty,
			"paid_total_amount":  order.PaidAmount,
			"order_total_amount": order.OrderTotalAmount,
			"item_gross_amount":  order.ItemGrossAmount,
			"line_paid_amount":   order.LinePaidAmount,
			"shipping_amount":    order.ShippingAmount,
			"discount_amount":    order.DiscountAmount,
			"has_no_sku":         order.HasNoSKU,
			"no_sku_item_count":  order.NoSKUItemCount,
			"amount_mismatch":    order.AmountMismatch,
			"multi_line":         order.MultiLine,
			"import_run_id":      req.ImportRunID,
			"document_route":     documentRoute,
			"sml_destination":    destinationName,
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
			BillType:      "sale",
			Source:        "shopee",
			Status:        status,
			DocumentRoute: documentRoute,
			AIConfidence:  &aiConf,
			RawData:       rawData,
			SMLOrderID:    order.OrderID,
		}
		if userID != nil {
			bill.CreatedBy = userID
		}
		if err := h.billRepo.Create(bill); err != nil {
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

		for i := range enriched {
			enriched[i].item.BillID = bill.ID
			candidatesJSON, _ := json.Marshal(enriched[i].candidates)
			_ = h.billRepo.InsertItemWithCandidates(&enriched[i].item, candidatesJSON)
		}

		// Archive the source .xlsx as a per-bill artifact so the user can
		// always trace back which file produced this bill (audit trail
		// + SHA-256 integrity, same pattern as email_pdf / email_html).
		if h.artifactSvc != nil && uploadBytes != nil {
			meta := map[string]interface{}{
				"order_id":    order.OrderID,
				"uploaded_by": "",
				"trace_id":    traceID,
			}
			if userID != nil {
				meta["uploaded_by"] = *userID
			}
			filename := uploadFilename
			if filename == "" {
				filename = "shopee-import.xlsx"
			}
			if _, err := h.artifactSvc.Save(
				bill.ID,
				"xlsx",
				filename,
				"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
				uploadBytes,
				meta,
			); err != nil {
				h.logger.Warn("shopee_excel: save artifact failed",
					zap.String("bill_id", bill.ID), zap.Error(err))
			}
		}

		// Audit log — bill created (no SML call, that happens later via Retry)
		if h.auditRepo != nil {
			billIDStr := bill.ID
			durMs := int(time.Since(confirmStart).Milliseconds())
			_ = h.auditRepo.Log(models.AuditEntry{
				Action:     "bill_created",
				TargetID:   &billIDStr,
				UserID:     userID,
				Source:     sourceFlow,
				Level:      "info",
				TraceID:    traceID,
				DurationMs: &durMs,
				Detail: map[string]interface{}{
					"order_id":       order.OrderID,
					"shopee_shop_id": shopID,
					"items_count":    len(enriched),
					"all_high_conf":  allHigh,
					"status":         status,
				},
			})
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

	// Find header row: first row that contains an order_id candidate keyword
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

	// Map field → column index using substring matching.
	// This handles Shopee headers that include English translations, e.g.
	// "หมายเลขคำสั่งซื้อ (Order No.)" matches candidate "หมายเลขคำสั่งซื้อ".
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

	// Check required columns
	required := []string{"order_id", "status", "order_date", "product_name", "price", "qty"}
	for _, f := range required {
		if _, ok := colIdx[f]; !ok {
			return nil, nil, 0, fmt.Errorf("ไม่พบ column '%s' ในไฟล์ — columns ที่พบ: %s",
				f, strings.Join(headerRow[:min(len(headerRow), 15)], ", "))
		}
	}

	warnings := []string{} // initialize as empty slice (never nil) to avoid JSON null
	orderMap := map[string]*ShopeeOrder{}
	orderKeys := []string{} // preserve insertion order
	noSKUOrderIDs := map[string]bool{}
	noSKUItemCount := 0
	skippedCount := 0

	for _, row := range rows[headerRowIdx+1:] {
		if len(row) == 0 {
			continue
		}
		orderID := cellStr(row, colIdx["order_id"])
		if orderID == "" || strings.EqualFold(orderID, "nan") {
			continue
		}

		// Filter excluded statuses
		status := cellStr(row, colIdx["status"])
		if excludeStatuses[status] {
			skippedCount++
			continue
		}

		// Parse date
		orderDateTime := cellStr(row, colIdx["order_date"])
		docDate := orderDateTime
		if len(orderDateTime) >= 10 {
			docDate = orderDateTime[:10]
		} else {
			docDate = time.Now().Format("2006-01-02")
		}

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
		}
		orderMap[orderID].LinePaidAmount += optionalCellFloat(row, colIdx, "paid_amount")
		if v := optionalCellFloat(row, colIdx, "order_total"); v > 0 {
			orderMap[orderID].OrderTotalAmount = v
		}
		if v := optionalCellFloat(row, colIdx, "shipping_amount"); v > 0 {
			orderMap[orderID].ShippingAmount = v
		}

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

		price := cellFloat(row, colIdx["price"])
		qty := cellFloat(row, colIdx["qty"])
		if qty <= 0 {
			qty = 1
		}

		orderMap[orderID].Items = append(orderMap[orderID].Items, ShopeeExcelItem{
			SKU:         sku,
			ProductName: productName,
			OptionName:  optionName,
			RawName:     rawName,
			Price:       price,
			Qty:         qty,
			NoSKU:       noSKU,
		})
		orderMap[orderID].ItemGrossAmount += price * qty
	}

	// Build result list in original order, skip orders with no items
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
		if o.OrderTotalAmount > 0 {
			o.PaidAmount = o.OrderTotalAmount
		} else {
			o.PaidAmount = o.LinePaidAmount
		}
		o.DiscountAmount = roundFloat(o.ItemGrossAmount+o.ShippingAmount-o.PaidAmount, 2)
		o.AmountMismatch = o.PaidAmount > 0 && math.Abs(o.ItemGrossAmount-o.PaidAmount) > 0.01
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

func buildShopeePreflight(orders []ShopeeOrder, skippedCount, duplicateCount int) ShopeeImportPreflight {
	p := ShopeeImportPreflight{
		NewOrders:       len(orders) - duplicateCount,
		DuplicateOrders: duplicateCount,
		SkippedRows:     skippedCount,
	}
	for _, o := range orders {
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
	if strings.Contains(route, "saleinvoice") || strings.Contains(route, " si") || strings.TrimSpace(strings.ToUpper(cfg.DocFormat)) == "SI" {
		return "saleinvoice"
	}
	return "saleorder"
}

func (h *ShopeeImportHandler) lookupCatalogItem(code string) *models.CatalogItem {
	code = strings.TrimSpace(code)
	if code == "" || h.catalogRepo == nil {
		return nil
	}
	item, err := h.catalogRepo.GetOne(code)
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
