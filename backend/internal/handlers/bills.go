package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/config"
	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/artifact"
	"nexflow/internal/services/itemcode"
	lineservice "nexflow/internal/services/line"
	"nexflow/internal/services/mapper"
	"nexflow/internal/services/sml"
)

type BillHandler struct {
	billRepo        *repository.BillRepo
	mapperSvc       *mapper.Service
	invoiceClient   *sml.InvoiceClient       // SML 248 saleinvoice REST (legacy)
	saleOrderClient *sml.SaleOrderClient     // SML 248 saleorder REST (default)
	poClient        *sml.PurchaseOrderClient // SML 248 purchaseorder REST
	docNoClient     *sml.DocNoClient         // SML authoritative doc_no running
	cfg             *config.Config
	lineSvc         *lineservice.Service
	auditRepo       *repository.AuditLogRepo
	catalogRepo     *repository.SMLCatalogRepo     // for unit_code defaults on item edit
	channelDefaults *repository.ChannelDefaultRepo // per-(channel,bill_type) party config
	docCounters     *repository.DocCounterRepo     // atomic doc_no generator
	bulkJobRepo     *repository.SMLBulkJobRepo     // async SML bulk send jobs
	artifactSvc     *artifact.Service              // source-artifact storage (PDF/HTML/etc.)
	warehouseCache  *sml.WarehouseCache            // optional validation for wh/shelf chosen in dialog
	smlReadiness    *sml.ReadinessChecker          // fail-closed guard for tenant DB availability
	appSettingsRepo *repository.AppSettingsRepo    // runtime: sml.stock_request_url read per-send
	log             *zap.Logger
}

func NewBillHandler(
	billRepo *repository.BillRepo,
	mapperSvc *mapper.Service,
	invoiceClient *sml.InvoiceClient,
	saleOrderClient *sml.SaleOrderClient,
	poClient *sml.PurchaseOrderClient,
	docNoClient *sml.DocNoClient,
	cfg *config.Config,
	lineSvc *lineservice.Service,
	auditRepo *repository.AuditLogRepo,
	catalogRepo *repository.SMLCatalogRepo,
	channelDefaults *repository.ChannelDefaultRepo,
	docCounters *repository.DocCounterRepo,
	bulkJobRepo *repository.SMLBulkJobRepo,
	artifactSvc *artifact.Service,
	warehouseCache *sml.WarehouseCache,
	smlReadiness *sml.ReadinessChecker,
	appSettingsRepo *repository.AppSettingsRepo,
	log *zap.Logger,
) *BillHandler {
	return &BillHandler{
		billRepo:        billRepo,
		mapperSvc:       mapperSvc,
		invoiceClient:   invoiceClient,
		saleOrderClient: saleOrderClient,
		poClient:        poClient,
		docNoClient:     docNoClient,
		cfg:             cfg,
		lineSvc:         lineSvc,
		auditRepo:       auditRepo,
		catalogRepo:     catalogRepo,
		channelDefaults: channelDefaults,
		docCounters:     docCounters,
		bulkJobRepo:     bulkJobRepo,
		artifactSvc:     artifactSvc,
		warehouseCache:  warehouseCache,
		smlReadiness:    smlReadiness,
		appSettingsRepo: appSettingsRepo,
		log:             log,
	}
}

// ─── Stock recalculation ──────────────────────────────────────────────────────

// stockRecalcSem limits concurrent processstockrequest goroutines to 3
// to prevent overloading the SML stock server during bulk sends.
var stockRecalcSem = make(chan struct{}, 3)

// stockWarnedJobs tracks which bulk jobs have already emitted a stock-recalc
// warning to the application log — audit entries are still written for every
// bill regardless.
var (
	stockWarnOnceMu sync.Mutex
	stockWarnedJobs = make(map[string]struct{})
)

// triggerStockRecalculation fires a background goroutine that calls
// processstockrequest after a bill is successfully sent to SML.
// It is best-effort: failure does NOT roll back the bill's "sent" status.
// bulkJobID is "" for single-bill retries; for bulk jobs it suppresses
// duplicate application log warnings (audit entries are always written).
func (h *BillHandler) triggerStockRecalculation(billID, docNo, route, bulkJobID string, itemCodes []string) {
	if h.appSettingsRepo == nil || len(itemCodes) == 0 {
		return
	}
	stockURL, _ := h.appSettingsRepo.GetValue("sml.stock_request_url")
	if strings.TrimSpace(stockURL) == "" {
		return // not configured — skip silently
	}

	go func() {
		stockRecalcSem <- struct{}{}
		defer func() { <-stockRecalcSem }()

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		c := sml.NewStockRequestClient(stockURL, h.cfg.ShopeeSMLProvider, h.cfg.ShopeeSMLDatabase, h.log)
		if err := c.ProcessStockRequest(ctx, itemCodes); err != nil {
			// Audit every bill regardless of bulk suppression
			_ = h.auditRepo.Log(models.AuditEntry{
				Action:   "sml_stock_recalc_failed",
				TargetID: &billID,
				Source:   "sml",
				Level:    "warn",
				Detail: map[string]any{
					"error":       err.Error(),
					"doc_no":      docNo,
					"route":       route,
					"item_count":  len(itemCodes),
					"bulk_job_id": bulkJobID,
				},
			})
			// Suppress duplicate app.Warn() in bulk — only log the first failure per job
			logApp := true
			if bulkJobID != "" {
				stockWarnOnceMu.Lock()
				if _, warned := stockWarnedJobs[bulkJobID]; warned {
					logApp = false
				} else {
					stockWarnedJobs[bulkJobID] = struct{}{}
				}
				stockWarnOnceMu.Unlock()
			}
			if logApp {
				h.log.Warn("stock recalc failed (best-effort, bill still sent)",
					zap.String("bill_id", billID),
					zap.String("doc_no", docNo),
					zap.String("bulk_job_id", bulkJobID),
					zap.Error(err),
				)
			}
			return
		}

		_ = h.auditRepo.Log(models.AuditEntry{
			Action:   "sml_stock_recalc_ok",
			TargetID: &billID,
			Source:   "sml",
			Level:    "info",
			Detail: map[string]any{
				"doc_no":     docNo,
				"route":      route,
				"item_count": len(itemCodes),
			},
		})
	}()
}

// ─── Doc no ───────────────────────────────────────────────────────────────────

// resolveDocNo returns the doc_no to use for sending bill to SML. Reuses the
// existing bill.sml_doc_no when set (so re-retry of a failed bill doesn't
// inflate the counter or create duplicate docs in SML), otherwise generates
// a fresh one from def.DocPrefix + def.DocRunningFormat.
//
// fallbackPrefix is used when def has no prefix configured — typically the
// endpoint-flavored default ("NX-SO" for saleorder, "NX-PO" for PO, etc.)
func (h *BillHandler) resolveDocNo(bill *models.Bill, def *models.ChannelDefault, fallbackPrefix, route string) (string, error) {
	if bill.SMLDocNo != nil && *bill.SMLDocNo != "" {
		if bill.Status == "failed" && isDuplicateDocNoError(bill.ErrorMsg) {
			// The saved number already failed because SML says it exists.
			// Reserve a new one instead of reusing the bad doc_no forever.
		} else {
			return *bill.SMLDocNo, nil
		}
	}
	return h.allocateFreshDocNo(bill, def, fallbackPrefix, route)
}

func (h *BillHandler) allocateFreshDocNo(bill *models.Bill, def *models.ChannelDefault, fallbackPrefix, route string) (string, error) {
	prefix, format := resolveDocCounterPattern(def, fallbackPrefix)
	if h.docCounters == nil {
		return "", fmt.Errorf("local doc_no counter not configured")
	}
	if h.docNoClient == nil || !h.docNoClient.IsConfigured() {
		return "", fmt.Errorf("ดึงเลข running ล่าสุดจาก SML ไม่ได้: SML doc_no API ยังไม่ได้ตั้งค่า")
	}
	docDate := docDateFromBill(bill)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	next, err := h.docNoClient.Next(ctx, sml.NextDocNoRequest{
		Route:   route,
		Prefix:  prefix,
		Format:  format,
		DocDate: docDate,
	})
	if err != nil {
		return "", fmt.Errorf("ดึงเลข running ล่าสุดจาก SML ไม่สำเร็จ: %w", err)
	}
	docNoDate, err := time.Parse("2006-01-02", next.DocDate)
	if err != nil {
		docNoDate, _ = time.Parse("2006-01-02", docDate)
	}
	if docNoDate.IsZero() {
		docNoDate = time.Now()
	}
	for i := 0; i < 100; i++ {
		docNo, err := h.docCounters.GenerateDocNoAtLeast(prefix, format, docNoDate, next.NextSeq)
		if err != nil {
			return "", err
		}
		exists, err := h.localDocNoExists(docNo, bill.ID)
		if err != nil {
			return "", err
		}
		if !exists {
			return docNo, nil
		}
	}
	return "", fmt.Errorf("cannot allocate unique doc_no for prefix %s", prefix)
}

func (h *BillHandler) previewFreshDocNo(bill *models.Bill, def *models.ChannelDefault, fallbackPrefix, route string) (string, error) {
	prefix, format := resolveDocCounterPattern(def, fallbackPrefix)
	docDate := docDateFromBill(bill)
	docNoDate, _ := time.Parse("2006-01-02", docDate)
	if docNoDate.IsZero() {
		docNoDate = time.Now()
	}

	if h.docNoClient != nil && h.docNoClient.IsConfigured() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		next, err := h.docNoClient.Next(ctx, sml.NextDocNoRequest{
			Route:   route,
			Prefix:  prefix,
			Format:  format,
			DocDate: docDate,
		})
		if err != nil {
			return "", fmt.Errorf("ดึงเลข running ล่าสุดจาก SML ไม่สำเร็จ: %w", err)
		}
		if parsed, err := time.Parse("2006-01-02", next.DocDate); err == nil && !parsed.IsZero() {
			docNoDate = parsed
		}
		for i := 0; i < 100; i++ {
			docNo := next.NextDocNo
			if i > 0 {
				docNo = repository.RenderDocNoFromSeq(prefix, format, docNoDate, next.NextSeq+i)
			}
			exists, err := h.localDocNoExists(docNo, bill.ID)
			if err != nil {
				return "", err
			}
			if !exists {
				return docNo, nil
			}
		}
		return "", fmt.Errorf("cannot preview unique doc_no for prefix %s", prefix)
	}

	if h.docCounters == nil {
		return "", fmt.Errorf("local doc_no counter not configured")
	}
	for i := 0; i < 100; i++ {
		docNo, err := h.docCounters.PeekDocNoWithOffset(prefix, format, docNoDate, i)
		if err != nil {
			return "", err
		}
		exists, err := h.localDocNoExists(docNo, bill.ID)
		if err != nil {
			return "", err
		}
		if !exists {
			return docNo, nil
		}
	}
	return "", fmt.Errorf("cannot preview unique doc_no for prefix %s", prefix)
}

func isDuplicateDocNoError(errMsg *string) bool {
	if errMsg == nil {
		return false
	}
	s := strings.ToLower(*errMsg)
	return strings.Contains(s, "duplicate key") ||
		strings.Contains(s, "already exists") ||
		strings.Contains(s, "มีอยู่")
}

func resolveDocCounterPattern(def *models.ChannelDefault, fallbackPrefix string) (string, string) {
	prefix := fallbackPrefix
	format := "YYMM####"
	if def != nil {
		if def.DocPrefix != "" {
			prefix = def.DocPrefix
		}
		if def.DocRunningFormat != "" {
			format = def.DocRunningFormat
		}
	}
	return prefix, format
}

func (h *BillHandler) localDocNoExists(docNo, currentBillID string) (bool, error) {
	if h.billRepo == nil {
		return false, nil
	}
	var n int
	var err error
	if currentBillID == "" {
		err = h.billRepo.DB().QueryRow(
			`SELECT COUNT(*) FROM bills WHERE sml_doc_no = $1`,
			docNo,
		).Scan(&n)
	} else {
		err = h.billRepo.DB().QueryRow(
			`SELECT COUNT(*) FROM bills WHERE sml_doc_no = $1 AND id <> $2`,
			docNo, currentBillID,
		).Scan(&n)
	}
	if err != nil {
		return false, fmt.Errorf("check local doc_no: %w", err)
	}
	return n > 0, nil
}

func (h *BillHandler) peekDocNo(def *models.ChannelDefault, fallbackPrefix, route string) (string, error) {
	prefix, format := resolveDocCounterPattern(def, fallbackPrefix)
	if h.docNoClient != nil && h.docNoClient.IsConfigured() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		next, err := h.docNoClient.Next(ctx, sml.NextDocNoRequest{
			Route:   route,
			Prefix:  prefix,
			Format:  format,
			DocDate: time.Now().Format("2006-01-02"),
		})
		if err != nil {
			return "", err
		}
		return next.NextDocNo, nil
	}
	if h.docCounters == nil {
		return "", nil
	}
	for i := 0; i < 100; i++ {
		docNo, err := h.docCounters.PeekDocNoWithOffset(prefix, format, time.Now(), i)
		if err != nil {
			return "", err
		}
		exists, err := h.localDocNoExists(docNo, "")
		if err != nil {
			return "", err
		}
		if !exists {
			return docNo, nil
		}
	}
	return "", fmt.Errorf("cannot preview unique doc_no for prefix %s", prefix)
}

func (h *BillHandler) writeDocNoError(c *gin.Context, err error) {
	c.JSON(http.StatusBadRequest, gin.H{"error": "เลขเอกสาร SML ไม่ถูกต้อง: " + err.Error()})
}

func (h *BillHandler) resolvedSaleOrderConfig(def *models.ChannelDefault, req RetryRequest) sml.SaleOrderConfig {
	cfg := h.shopeeSaleOrderConfig()
	if def != nil && def.PartyCode != "" {
		cfg.CustCode = def.PartyCode
	}
	if def != nil && def.DocFormatCode != "" {
		cfg.DocFormat = def.DocFormatCode
	}
	applyDocumentOverrides(def, &cfg.BranchCode, &cfg.SaleCode, &cfg.UnitCode, &cfg.DocTime)
	applyChannelOverrides(def, &cfg.WHCode, &cfg.ShelfCode, &cfg.VATType, &cfg.VATRate)
	applyRetryDocumentOverrides(req, &cfg.BranchCode, &cfg.SaleCode, &cfg.UnitCode, &cfg.DocTime)
	applyRetryOverrides(req, &cfg.WHCode, &cfg.ShelfCode, &cfg.VATType, &cfg.VATRate)
	return cfg
}

func (h *BillHandler) resolvedInvoiceConfig(def *models.ChannelDefault, req RetryRequest) sml.InvoiceConfig {
	cfg := sml.InvoiceConfig{
		BaseURL:    h.cfg.ShopeeSMLURL,
		GUID:       h.cfg.ShopeeSMLGUID,
		Provider:   h.cfg.ShopeeSMLProvider,
		ConfigFile: h.cfg.ShopeeSMLConfigFile,
		Database:   h.cfg.ShopeeSMLDatabase,
		VATType:    -1,
		VATRate:    -1,
	}
	if def != nil && def.PartyCode != "" {
		cfg.CustCode = def.PartyCode
	}
	if def != nil && def.DocFormatCode != "" {
		cfg.DocFormat = def.DocFormatCode
	}
	applyDocumentOverrides(def, &cfg.BranchCode, &cfg.SaleCode, &cfg.UnitCode, &cfg.DocTime)
	applyChannelOverrides(def, &cfg.WHCode, &cfg.ShelfCode, &cfg.VATType, &cfg.VATRate)
	applyRetryDocumentOverrides(req, &cfg.BranchCode, &cfg.SaleCode, &cfg.UnitCode, &cfg.DocTime)
	applyRetryOverrides(req, &cfg.WHCode, &cfg.ShelfCode, &cfg.VATType, &cfg.VATRate)
	return cfg
}

func (h *BillHandler) resolvedPurchaseConfig(def *models.ChannelDefault, req RetryRequest) sml.PurchaseOrderConfig {
	cfg := h.shopeePurchaseConfig()
	if def != nil {
		if def.PartyCode != "" {
			cfg.CustCode = def.PartyCode
		}
		if def.PartyName != "" {
			cfg.SupplierName = def.PartyName
		}
	}
	if def != nil && def.DocFormatCode != "" {
		cfg.DocFormat = def.DocFormatCode
	}
	applyDocumentOverrides(def, &cfg.BranchCode, &cfg.SaleCode, &cfg.UnitCode, &cfg.DocTime)
	applyChannelOverrides(def, &cfg.WHCode, &cfg.ShelfCode, &cfg.VATType, &cfg.VATRate)
	applyRetryDocumentOverrides(req, &cfg.BranchCode, &cfg.SaleCode, &cfg.UnitCode, &cfg.DocTime)
	applyRetryOverrides(req, &cfg.WHCode, &cfg.ShelfCode, &cfg.VATType, &cfg.VATRate)
	return cfg
}

func (h *BillHandler) validateResolvedSendFields(docFormat, whCode, shelfCode, docTime string, vatType int, vatRate float64) error {
	switch {
	case strings.TrimSpace(docFormat) == "":
		return fmt.Errorf("กรุณาเลือกรูปแบบเอกสาร SML ใน /settings/channels ก่อนส่ง")
	case strings.TrimSpace(docTime) == "":
		return fmt.Errorf("กรุณากรอกเวลาเอกสารก่อนส่ง SML")
	case strings.TrimSpace(whCode) == "":
		return fmt.Errorf("กรุณากรอกรหัสคลังก่อนส่ง SML")
	case strings.TrimSpace(shelfCode) == "":
		return fmt.Errorf("กรุณากรอกรหัสพื้นที่เก็บก่อนส่ง SML")
	case vatType < 0:
		return fmt.Errorf("กรุณาเลือกประเภทภาษีก่อนส่ง SML")
	case vatRate < 0:
		return fmt.Errorf("กรุณากรอกอัตราภาษีก่อนส่ง SML")
	}
	if h.warehouseCache != nil {
		wh := strings.TrimSpace(whCode)
		shelf := strings.TrimSpace(shelfCode)
		if whCount, _ := h.warehouseCache.Counts(); whCount > 0 {
			if h.warehouseCache.GetByCode(wh) == nil {
				return fmt.Errorf("ไม่พบรหัสคลัง %s ใน SML", wh)
			}
			if !h.warehouseCache.HasShelf(wh, shelf) {
				return fmt.Errorf("ไม่พบพื้นที่เก็บ %s ภายใต้คลัง %s ใน SML", shelf, wh)
			}
		}
	}
	return nil
}

func (h *BillHandler) resolveRetryDocNo(req RetryRequest, bill *models.Bill, def *models.ChannelDefault, fallbackPrefix, route string) (string, error) {
	if docNo := strings.TrimSpace(req.DocNo); docNo != "" {
		if clean := cleanSMLDocNo(docNo); clean != docNo {
			return "", fmt.Errorf("doc_no contains hidden or invalid Thai mark characters; use %q", clean)
		}
		return docNo, nil
	}
	docNo, err := h.resolveDocNo(bill, def, fallbackPrefix, route)
	if err != nil {
		return "", err
	}
	if clean := cleanSMLDocNo(docNo); clean != strings.TrimSpace(docNo) {
		return "", fmt.Errorf("doc_no contains hidden or invalid Thai mark characters; use %q", clean)
	}
	return docNo, nil
}

func cleanSMLDocNo(docNo string) string {
	replacer := strings.NewReplacer(
		"\u200b", "",
		"\u200c", "",
		"\u200d", "",
		"\ufeff", "",
		"\u0e31", "",
		"\u0e34", "",
		"\u0e35", "",
		"\u0e36", "",
		"\u0e37", "",
		"\u0e38", "",
		"\u0e39", "",
		"\u0e3a", "",
		"\u0e47", "",
		"\u0e48", "",
		"\u0e49", "",
		"\u0e4a", "",
		"\u0e4b", "",
		"\u0e4c", "",
		"\u0e4d", "",
		"\u0e4e", "",
	)
	return strings.TrimSpace(replacer.Replace(docNo))
}

func fallbackDocPrefix(route string) string {
	switch route {
	case "purchaseorder":
		return "NX-PO"
	case "saleinvoice":
		return "NX-INV"
	case "saleorder":
		return "NX-SO"
	default:
		return "BF"
	}
}

// resolveEndpoint figures out which SML client to use for a channel.
//
// The admin-supplied `endpoint` is a free-form URL/path (e.g.
// "/SMLJavaRESTService/v3/api/saleorder" or "https://sml/.../saleinvoice").
// We pick the client by keyword match and pass the URL through as override
// so the client posts to the admin's chosen path.
//
// Returns:
//
//	kind        — "saleorder" | "saleinvoice" | "purchaseorder"
//	urlOverride — the URL to send to (empty = use client's default)
func resolveEndpoint(def *models.ChannelDefault, source, billType string) (kind, urlOverride string) {
	raw := ""
	if def != nil {
		raw = def.Endpoint
	}
	rawLower := strings.ToLower(raw)
	rawTrimmed := strings.Trim(rawLower, " /")

	switch {
	// keyword-only shortcuts (admin types just the document type)
	case rawTrimmed == "purchaseorder", rawTrimmed == "purchase-orders":
		return "purchaseorder", ""
	case rawTrimmed == "saleinvoice", rawTrimmed == "sale-invoices":
		return "saleinvoice", ""
	case rawTrimmed == "saleorder", rawTrimmed == "sale-orders":
		return "saleorder", ""
	// full path match — new v1 paths
	case strings.Contains(rawLower, "purchase-orders"):
		return "purchaseorder", raw
	case strings.Contains(rawLower, "sale-invoices"):
		return "saleinvoice", raw
	case strings.Contains(rawLower, "sale-orders"):
		return "saleorder", raw
	// full path match — legacy compat paths (still accepted)
	case strings.Contains(rawLower, "purchaseorder"):
		return "purchaseorder", raw
	case strings.Contains(rawLower, "saleinvoice"):
		return "saleinvoice", raw
	case strings.Contains(rawLower, "saleorder"):
		return "saleorder", raw
	}

	// No keyword match (or empty) → default routing by channel + bill_type
	if source == "shopee_shipped" || billType == "purchase" {
		return "purchaseorder", ""
	}
	// All sale channels (shopee, lazada, tiktok, line, email, manual) → saleorder
	return "saleorder", ""
}

// resolveItemName returns the catalog name for a mapped item_code, falling
// back to the source raw_name when no catalog row exists. Used so SML
// receives the canonical SML name instead of the original source product
// name (e.g. Shopee's verbose listing title).
func (h *BillHandler) resolveItemName(itemCode, rawName string) string {
	if h.catalogRepo != nil && itemCode != "" {
		if cat, _ := h.catalogRepo.GetOne(itemCode); cat != nil && cat.ItemName != "" {
			return cat.ItemName
		}
	}
	return rawName
}

// GET /api/bills
func (h *BillHandler) List(c *gin.Context) {
	var f models.BillListFilter
	if err := c.ShouldBindQuery(&f); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if f.PerPage > 0 {
		f.PageSize = f.PerPage
	}
	f.CursorMode = c.Query("cursor") != "" || c.Query("limit") != ""
	if v := c.Query("include_total"); v != "" {
		f.IncludeTotal, _ = strconv.ParseBool(v)
	}

	result, err := h.billRepo.List(f)
	if err != nil {
		h.log.Error("List bills", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if result.Bills == nil {
		result.Bills = []models.Bill{}
	}

	resp := gin.H{
		"data":        result.Bills,
		"page":        result.Page,
		"page_size":   result.PageSize,
		"per_page":    result.PageSize,
		"limit":       result.PageSize,
		"has_more":    result.HasMore,
		"next_cursor": result.NextCursor,
	}
	if result.Total != nil {
		resp["total"] = *result.Total
	}
	c.JSON(http.StatusOK, resp)
}

// GET /api/bills/counts
func (h *BillHandler) Counts(c *gin.Context) {
	var f models.BillListFilter
	if err := c.ShouldBindQuery(&f); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	counts, err := h.billRepo.QueueCounts(f)
	if err != nil {
		h.log.Error("Bill counts", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, counts)
}

// GET /api/bills/:id
//
// Response includes a "preview" object showing the SML route + endpoint +
// doc_no pattern that THIS bill would hit on retry. The preview is purely
// informational — it surfaces routing decisions in the BillDetail UI so
// admins catch misconfigured channels BEFORE
// they click Send and have to debug a failed bill afterwards.
func (h *BillHandler) Get(c *gin.Context) {
	id := c.Param("id")
	bill, err := h.billRepo.FindByID(id)
	if err != nil {
		h.log.Error("FindByID", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if bill == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "bill not found"})
		return
	}

	// Resolve which SML route + endpoint + doc_format this bill would use
	// today. Mirror the channel lookup that retry would do — same key
	// (channel, bill_type) — but never fail the GET if the row is missing
	// (e.g. legacy bills from before channel_defaults existed).
	channel := mapSourceToChannel(bill.Source)
	preview := gin.H{
		"channel":   channel,
		"bill_type": bill.BillType,
	}
	if h.channelDefaults != nil {
		def, _ := h.channelDefaults.Get(channel, bill.BillType)
		if def != nil {
			route, urlOverride := resolveEndpoint(def, bill.Source, bill.BillType)
			preview["route"] = route
			if urlOverride != "" {
				preview["endpoint"] = urlOverride
			}
			if bill.SMLDocNo != nil && *bill.SMLDocNo != "" {
				preview["doc_no"] = *bill.SMLDocNo
			} else if docNo, err := h.peekDocNo(def, fallbackDocPrefix(route), route); err == nil && docNo != "" {
				preview["doc_no"] = docNo
			}
			if def.DocPrefix != "" || def.DocRunningFormat != "" {
				preview["doc_format"] = def.DocPrefix + def.DocRunningFormat
			}
			if def.DocFormatCode != "" {
				preview["doc_format_code"] = def.DocFormatCode
			}
			if bill.BillType == "purchase" && def.PartyCode != "" {
				preview["party_code"] = def.PartyCode
				preview["party_name"] = def.PartyName
			}
			preview["sml_defaults"] = h.previewSMLDefaults(def, bill.BillType)
		} else {
			// No channel_default row — admin needs to set one up. Surface
			// this as a preview-level warning so the UI can render a hint.
			preview["error"] = "ยังไม่ได้ตั้งค่า channel — ไปที่ /settings/channels"
		}
	}

	// Wrap bill + preview in a single response. The bill struct is
	// preserved unchanged at the top level so existing consumers keep
	// working without a type migration.
	billJSON, _ := json.Marshal(bill)
	out := gin.H{}
	if err := json.Unmarshal(billJSON, &out); err == nil {
		out["preview"] = preview
		c.JSON(http.StatusOK, out)
		return
	}
	// Fallback if marshal/unmarshal hiccups — return the bill alone.
	c.JSON(http.StatusOK, bill)
}

type archiveBillRequest struct {
	Reason string `json:"reason"`
}

func (h *BillHandler) Archive(c *gin.Context) {
	id := c.Param("id")
	var req archiveBillRequest
	_ = c.ShouldBindJSON(&req)
	if err := h.billRepo.Archive(id, c.GetString("user_id"), req.Reason); err != nil {
		h.log.Error("Archive bill", zap.String("bill", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "archive failed"})
		return
	}
	if h.auditRepo != nil {
		billID := id
		var userID *string
		if uid := c.GetString("user_id"); uid != "" {
			userID = &uid
		}
		_ = h.auditRepo.Log(models.AuditEntry{
			Action:   "bill_archived",
			TargetID: &billID,
			UserID:   userID,
			Source:   "system",
			Level:    "info",
			Detail: map[string]interface{}{
				"reason": req.Reason,
			},
		})
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *BillHandler) Restore(c *gin.Context) {
	id := c.Param("id")
	if err := h.billRepo.Restore(id); err != nil {
		h.log.Error("Restore bill", zap.String("bill", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "restore failed"})
		return
	}
	if h.auditRepo != nil {
		billID := id
		var userID *string
		if uid := c.GetString("user_id"); uid != "" {
			userID = &uid
		}
		_ = h.auditRepo.Log(models.AuditEntry{
			Action:   "bill_restored",
			TargetID: &billID,
			UserID:   userID,
			Source:   "system",
			Level:    "info",
		})
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *BillHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	var body struct {
		Confirm string `json:"confirm"`
	}
	_ = c.ShouldBindJSON(&body)
	if body.Confirm != "DELETE" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "confirm must be DELETE"})
		return
	}
	if err := h.billRepo.Delete(id); err != nil {
		h.log.Error("Delete bill", zap.String("bill", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// mapSourceToChannel mirrors the same logic the retry handler uses to look
// up a channel_defaults row. Kept private to this file.
func mapSourceToChannel(source string) string {
	switch source {
	case "shopee_shipped":
		return "shopee_shipped"
	case "shopee_email":
		return "shopee_email"
	case "shopee":
		return "shopee"
	case "lazada":
		return "lazada"
	case "tiktok":
		return "tiktok"
	case "email":
		return "email"
	}
	return "line"
}

// GET /api/bills/:id/timeline
//
// Returns every audit_log row whose target_id matches this bill, oldest
// first. The BillDetail page renders these as a vertical activity feed so
// admin can answer "ทำไมบิลนี้ถึงเป็นแบบนี้" without leaving the page.
func (h *BillHandler) Timeline(c *gin.Context) {
	id := c.Param("id")
	if h.auditRepo == nil {
		c.JSON(http.StatusOK, gin.H{"data": []any{}})
		return
	}
	rows, err := h.auditRepo.ListByTarget(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// POST /api/bills/:id/retry
// Routes to SML 248 REST based on bill.Source / bill.BillType:
//
//	shopee_shipped / purchase → poClient.CreatePurchaseOrder (ใบสั่งซื้อ)
//	saleinvoice pinned        → invoiceClient.CreateSaleInvoice (ใบกำกับภาษี)
//	all other sale channels   → saleOrderClient.CreateSaleOrder (ใบสั่งขาย)
//
// RetryRequest is the optional POST body for POST /api/bills/:id/retry.
// For purchase bills: party_code overrides channel_defaults.party_code and
// remark is stored on the bill + forwarded to SML.
type RetryRequest struct {
	PartyCode   string   `json:"party_code"`
	PartyName   string   `json:"party_name"`
	DocNo       string   `json:"doc_no"`
	Remark      string   `json:"remark"`
	Remark2     string   `json:"remark_2"`
	BranchCode  string   `json:"branch_code"`
	SaleCode    string   `json:"sale_code"`
	UnitCode    string   `json:"unit_code"`
	DocTime     string   `json:"doc_time"`
	WHCode      string   `json:"wh_code"`
	ShelfCode   string   `json:"shelf_code"`
	VATType     *int     `json:"vat_type"`
	VATRate     *float64 `json:"vat_rate"`
	InquiryType *int     `json:"inquiry_type"`
}

type retrySendOptions struct {
	UserID            string
	TraceID           string
	Via               string
	BulkJobID         string
	BulkJobItemID     string
	BulkItemSequence  int
	SuppressLineAlert bool
}

type retrySendResult struct {
	HTTPStatus     int
	Message        string
	Error          string
	DocNo          string
	DocNoAttempted string
	Route          string
	Skipped        bool
	Warnings       []hiddenItemCodeWarning
	LogWarning     string
}

type hiddenItemCodeWarning struct {
	BillID        string   `json:"bill_id,omitempty"`
	ItemID        string   `json:"item_id,omitempty"`
	RawName       string   `json:"raw_name,omitempty"`
	ItemCode      string   `json:"item_code"`
	CleanItemCode string   `json:"clean_item_code"`
	Kinds         []string `json:"hidden_char_kinds,omitempty"`
	Message       string   `json:"message"`
}

func (h *BillHandler) checkSMLReadiness(ctx context.Context, force bool) sml.ReadinessStatus {
	if h.smlReadiness == nil {
		return sml.ReadinessStatus{
			Configured: true,
			Ready:      true,
			Status:     "ok",
			Message:    "ไม่ได้เปิดใช้ SML readiness guard",
			CheckedAt:  time.Now(),
		}
	}
	return h.smlReadiness.Check(ctx, force)
}

func (h *BillHandler) auditSMLReadinessBlocked(action string, targetID *string, userID, traceID, via string, readiness sml.ReadinessStatus) {
	if h.auditRepo == nil {
		return
	}
	var uid *string
	if userID != "" {
		uid = &userID
	}
	_ = h.auditRepo.Log(models.AuditEntry{
		Action:   action,
		TargetID: targetID,
		UserID:   uid,
		Source:   "sml",
		Level:    "warn",
		TraceID:  traceID,
		Detail: map[string]interface{}{
			"via":         via,
			"tenant":      readiness.Tenant,
			"status":      readiness.Status,
			"http_status": readiness.HTTPStatus,
			"message":     readiness.Message,
			"checked_at":  readiness.CheckedAt,
		},
	})
}

func (h *BillHandler) blockIfSMLNotReady(c *gin.Context, action string, targetID *string, via string) bool {
	readiness := h.checkSMLReadiness(c.Request.Context(), false)
	if readiness.Ready {
		return false
	}
	h.auditSMLReadinessBlocked(action, targetID, c.GetString("user_id"), c.GetString("trace_id"), via, readiness)
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"error":         readiness.Message,
		"sml_readiness": readiness,
	})
	return true
}

func (h *BillHandler) Retry(c *gin.Context) {
	id := c.Param("id")
	bill, err := h.billRepo.FindByID(id)
	if err != nil || bill == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "bill not found"})
		return
	}
	switch bill.Status {
	case "failed", "pending", "needs_review":
		// ok
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "only failed/pending/needs_review bills can be sent"})
		return
	}
	if h.blockIfSMLNotReady(c, "sml_readiness_blocked", &bill.ID, "retry") {
		return
	}

	// Parse optional request body (party_code / remark overrides for purchase bills)
	var req RetryRequest
	_ = c.ShouldBindJSON(&req)

	result := h.sendBillToSML(bill, req, retrySendOptions{
		UserID:  c.GetString("user_id"),
		TraceID: c.GetString("trace_id"),
		Via:     "retry",
	})
	if result.HTTPStatus == http.StatusOK {
		resp := gin.H{"message": result.Message, "doc_no": result.DocNo}
		if len(result.Warnings) > 0 {
			resp["warnings"] = result.Warnings
		}
		if result.LogWarning != "" {
			resp["sml_log_warning"] = result.LogWarning
		}
		c.JSON(http.StatusOK, resp)
		return
	}
	if result.HTTPStatus == http.StatusAccepted {
		resp := gin.H{"message": result.Message}
		if len(result.Warnings) > 0 {
			resp["warnings"] = result.Warnings
		}
		c.JSON(http.StatusAccepted, resp)
		return
	}
	if result.HTTPStatus == 0 {
		result.HTTPStatus = http.StatusInternalServerError
	}
	c.JSON(result.HTTPStatus, gin.H{"error": result.Error})
}

func (h *BillHandler) RegenerateDocNo(c *gin.Context) {
	id := c.Param("id")
	bill, err := h.billRepo.FindByID(id)
	if err != nil || bill == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "bill not found"})
		return
	}
	if bill.Status == "sent" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "บิลที่ส่งเข้า SML แล้วไม่ควรออกเลขเอกสารใหม่"})
		return
	}
	if h.blockIfSMLNotReady(c, "sml_readiness_blocked", &bill.ID, "regenerate_doc_no") {
		return
	}
	var def *models.ChannelDefault
	if h.channelDefaults != nil {
		def, _ = h.channelDefaults.Get(bill.Source, bill.BillType)
	}
	kind, _ := resolveEndpoint(def, bill.Source, bill.BillType)
	docNo, err := h.allocateFreshDocNo(bill, def, fallbackDocPrefix(kind), kind)
	if err != nil {
		h.auditDocNoRegenerateFailed(c, bill, kind, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "ออกเลขเอกสารใหม่ไม่สำเร็จ: " + err.Error()})
		return
	}
	if err := h.billRepo.UpdateStatus(bill.ID, bill.Status, &docNo, nil, nil); err != nil {
		h.log.Error("regenerate doc_no update bill", zap.String("bill_id", bill.ID), zap.Error(err))
		h.auditDocNoRegenerateFailed(c, bill, kind, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "บันทึกเลขเอกสารใหม่ไม่สำเร็จ"})
		return
	}
	if h.auditRepo != nil {
		var userID *string
		if uid := c.GetString("user_id"); uid != "" {
			userID = &uid
		}
		_ = h.auditRepo.Log(models.AuditEntry{
			Action:   "bill_doc_no_regenerated",
			TargetID: &bill.ID,
			UserID:   userID,
			Source:   "ui",
			Level:    "info",
			TraceID:  c.GetString("trace_id"),
			Detail: map[string]interface{}{
				"doc_no": docNo,
				"route":  kind,
			},
		})
	}
	c.JSON(http.StatusOK, gin.H{"doc_no": docNo, "route": kind})
}

func (h *BillHandler) LatestDocNo(c *gin.Context) {
	id := c.Param("id")
	bill, err := h.billRepo.FindByID(id)
	if err != nil || bill == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "bill not found"})
		return
	}
	if h.blockIfSMLNotReady(c, "sml_readiness_blocked", &bill.ID, "latest_doc_no") {
		return
	}
	var def *models.ChannelDefault
	if h.channelDefaults != nil {
		def, _ = h.channelDefaults.Get(bill.Source, bill.BillType)
	}
	kind, _ := resolveEndpoint(def, bill.Source, bill.BillType)
	docNo, err := h.previewFreshDocNo(bill, def, fallbackDocPrefix(kind), kind)
	if err != nil {
		h.auditDocNoPreviewFailed(c, bill, kind, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "ดึงเลขล่าสุดจาก SML ไม่สำเร็จ: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"doc_no": docNo, "route": kind})
}

func (h *BillHandler) auditDocNoRegenerateFailed(c *gin.Context, bill *models.Bill, route string, err error) {
	if h.auditRepo == nil || bill == nil || err == nil {
		return
	}
	var userID *string
	if uid := c.GetString("user_id"); uid != "" {
		userID = &uid
	}
	_ = h.auditRepo.Log(models.AuditEntry{
		Action:   "bill_doc_no_regenerate_failed",
		TargetID: &bill.ID,
		UserID:   userID,
		Source:   "ui",
		Level:    "error",
		TraceID:  c.GetString("trace_id"),
		Detail: map[string]interface{}{
			"route": route,
			"error": err.Error(),
		},
	})
}

func (h *BillHandler) auditDocNoPreviewFailed(c *gin.Context, bill *models.Bill, route string, err error) {
	if h.auditRepo == nil || bill == nil || err == nil {
		return
	}
	var userID *string
	if uid := c.GetString("user_id"); uid != "" {
		userID = &uid
	}
	_ = h.auditRepo.Log(models.AuditEntry{
		Action:   "bill_doc_no_preview_failed",
		TargetID: &bill.ID,
		UserID:   userID,
		Source:   "ui",
		Level:    "error",
		TraceID:  c.GetString("trace_id"),
		Detail: map[string]interface{}{
			"route": route,
			"error": err.Error(),
		},
	})
}

func (h *BillHandler) EnsureShopeeShippingLine(c *gin.Context) {
	id := c.Param("id")
	bill, err := h.billRepo.FindByID(id)
	if err != nil || bill == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "bill not found"})
		return
	}
	switch bill.Status {
	case "failed", "pending", "needs_review":
		// ok
	default:
		c.JSON(http.StatusOK, gin.H{"ok": true, "inserted": false})
		return
	}
	item, err := h.ensureShopeeShippingLineForSend(bill)
	if err != nil {
		h.log.Warn("ensure shopee shipping line failed",
			zap.String("bill_id", bill.ID),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "เตรียมรายการค่าขนส่ง Shopee ไม่สำเร็จ: " + err.Error()})
		return
	}
	if item != nil && h.auditRepo != nil {
		billID := bill.ID
		var userID *string
		if uid := c.GetString("user_id"); uid != "" {
			userID = &uid
		}
		_ = h.auditRepo.Log(models.AuditEntry{
			Action:   "shopee_shipping_line_ensured",
			TargetID: &billID,
			UserID:   userID,
			Source:   "ui",
			Level:    "info",
			TraceID:  c.GetString("trace_id"),
			Detail: map[string]interface{}{
				"item_id":    item.ID,
				"item_code":  item.ItemCode,
				"price":      item.Price,
				"source_sku": item.SourceSKU,
			},
		})
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "inserted": item != nil, "item": item})
}

func (h *BillHandler) sendBillToSML(bill *models.Bill, req RetryRequest, opts retrySendOptions) retrySendResult {
	if bill == nil {
		return retrySendResult{HTTPStatus: http.StatusNotFound, Error: "bill not found"}
	}
	if opts.Via == "" {
		opts.Via = "retry"
	}
	switch bill.Status {
	case "failed", "pending", "needs_review":
		// ok
	default:
		return retrySendResult{
			HTTPStatus: http.StatusBadRequest,
			Error:      "only failed/pending/needs_review bills can be sent",
			Skipped:    true,
		}
	}
	if err := validateRemark2(req.Remark2); err != nil {
		return retrySendResult{HTTPStatus: http.StatusBadRequest, Error: err.Error()}
	}
	readiness := h.checkSMLReadiness(context.Background(), false)
	if !readiness.Ready {
		targetID := bill.ID
		h.auditSMLReadinessBlocked("sml_readiness_blocked", &targetID, opts.UserID, opts.TraceID, opts.Via, readiness)
		return retrySendResult{
			HTTPStatus: http.StatusServiceUnavailable,
			Error:      readiness.Message,
		}
	}
	if _, err := h.ensureShopeeShippingLineForSend(bill); err != nil {
		h.log.Warn("ensure shopee shipping line for send failed",
			zap.String("bill_id", bill.ID),
			zap.Error(err))
		return retrySendResult{
			HTTPStatus: http.StatusInternalServerError,
			Error:      "เตรียมรายการค่าขนส่ง Shopee ไม่สำเร็จ: " + err.Error(),
		}
	}

	allMapped := true
	missingCatalogCode := ""
	warnings := []hiddenItemCodeWarning{}
	for _, item := range bill.Items {
		if !item.Mapped || item.ItemCode == nil || strings.TrimSpace(*item.ItemCode) == "" {
			allMapped = false
			break
		}
		if h.catalogRepo != nil {
			code := strings.TrimSpace(*item.ItemCode)
			cat, _ := h.catalogRepo.GetOne(code)
			meta := itemcode.Inspect(code)
			if cat == nil {
				missingCatalogCode = code
				allMapped = false
				break
			}
			if meta.HasHiddenChars {
				warnings = append(warnings, hiddenWarningFromItem(bill.ID, item, code, meta))
			}
		}
	}
	if !allMapped {
		_ = h.billRepo.UpdateStatus(bill.ID, "needs_review", nil, nil, nil)
		if missingCatalogCode != "" {
			meta := itemcode.Inspect(missingCatalogCode)
			if meta.HasHiddenChars {
				return retrySendResult{
					HTTPStatus: http.StatusAccepted,
					Message:    fmt.Sprintf("รหัสสินค้า %s มีอักขระซ่อนและไม่พบในสินค้า SML — ควรเป็น %s", missingCatalogCode, meta.CleanItemCode),
					Skipped:    true,
				}
			}
			return retrySendResult{
				HTTPStatus: http.StatusAccepted,
				Message:    fmt.Sprintf("ไม่พบรหัสสินค้า %s ในสินค้า SML — bill set to needs_review", missingCatalogCode),
				Skipped:    true,
			}
		}
		return retrySendResult{
			HTTPStatus: http.StatusAccepted,
			Message:    "some items still unmapped — bill set to needs_review",
			Skipped:    true,
		}
	}

	// Persist manual remarks for normal routes. Shopee purchase email maps SML
	// remark to seller_name, so a typed UI note must not overwrite bill.remark
	// or leak into ic_trans.remark for that route.
	if req.Remark != "" && !isShopeePurchaseEmailBill(bill) {
		if err := h.billRepo.UpdateRemark(bill.ID, req.Remark); err != nil {
			h.log.Warn("UpdateRemark failed", zap.Error(err))
		}
	}

	def, _ := h.channelDefaults.Get(bill.Source, bill.BillType)
	kind, urlOverride := resolveEndpoint(def, bill.Source, bill.BillType)
	switch kind {
	case "purchaseorder":
		result := h.sendPurchaseOrderToSML(bill, req, urlOverride, opts)
		result.Warnings = warnings
		h.logHiddenItemCodeWarnings(bill, warnings, opts, "sml_send")
		return result
	case "saleinvoice":
		result := h.sendSaleInvoiceToSML(bill, req, urlOverride, opts)
		result.Warnings = warnings
		h.logHiddenItemCodeWarnings(bill, warnings, opts, "sml_send")
		return result
	default:
		result := h.sendSaleOrderToSML(bill, req, urlOverride, opts)
		result.Warnings = warnings
		h.logHiddenItemCodeWarnings(bill, warnings, opts, "sml_send")
		return result
	}
}

func hiddenWarningFromItem(billID string, item models.BillItem, code string, meta itemcode.Analysis) hiddenItemCodeWarning {
	return hiddenItemCodeWarning{
		BillID:        billID,
		ItemID:        item.ID,
		RawName:       item.RawName,
		ItemCode:      code,
		CleanItemCode: meta.CleanItemCode,
		Kinds:         meta.Kinds,
		Message:       fmt.Sprintf("รหัสสินค้า %s มีอักขระซ่อน ควรเป็น %s แต่รหัสนี้มีอยู่ใน SML จึงยังส่งได้", code, meta.CleanItemCode),
	}
}

func (h *BillHandler) logHiddenItemCodeWarnings(bill *models.Bill, warnings []hiddenItemCodeWarning, opts retrySendOptions, context string) {
	if h.auditRepo == nil || len(warnings) == 0 {
		return
	}
	var userID *string
	if opts.UserID != "" {
		userID = &opts.UserID
	}
	for _, warning := range warnings {
		targetID := warning.ItemID
		_ = h.auditRepo.Log(models.AuditEntry{
			Action:   "hidden_item_code_detected",
			TargetID: &targetID,
			UserID:   userID,
			Source:   bill.Source,
			Level:    "warn",
			TraceID:  opts.TraceID,
			Detail: map[string]interface{}{
				"context":            context,
				"via":                opts.Via,
				"bill_id":            bill.ID,
				"item_id":            warning.ItemID,
				"raw_name":           warning.RawName,
				"item_code":          warning.ItemCode,
				"clean_item_code":    warning.CleanItemCode,
				"hidden_char_kinds":  warning.Kinds,
				"bulk_job_id":        opts.BulkJobID,
				"bulk_job_item_id":   opts.BulkJobItemID,
				"bulk_item_sequence": opts.BulkItemSequence,
				"allowed":            true,
				"reason":             "dirty code exists in SML catalog",
			},
		})
	}
}

func (h *BillHandler) validateWritableItemCode(c *gin.Context, bill *models.Bill, itemID, code, context string) (itemcode.Analysis, bool) {
	meta := itemcode.Inspect(code)
	if !meta.HasHiddenChars {
		return meta, true
	}
	if h.catalogRepo == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":           "item_code has hidden characters but catalog validation is unavailable",
			"item_code":       code,
			"clean_item_code": meta.CleanItemCode,
		})
		return meta, false
	}
	cat, _ := h.catalogRepo.GetOne(code)
	if cat == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":           "item_code has hidden characters and does not exist in SML catalog",
			"item_code":       code,
			"clean_item_code": meta.CleanItemCode,
		})
		return meta, false
	}
	if h.auditRepo != nil {
		var userID *string
		if uid := c.GetString("user_id"); uid != "" {
			userID = &uid
		}
		targetID := itemID
		_ = h.auditRepo.Log(models.AuditEntry{
			Action:   "hidden_item_code_detected",
			TargetID: &targetID,
			UserID:   userID,
			Source:   bill.Source,
			Level:    "warn",
			TraceID:  c.GetString("trace_id"),
			Detail: map[string]interface{}{
				"context":           context,
				"bill_id":           bill.ID,
				"item_id":           itemID,
				"item_code":         code,
				"clean_item_code":   meta.CleanItemCode,
				"hidden_char_kinds": meta.Kinds,
				"allowed":           true,
				"reason":            "dirty code exists in SML catalog",
			},
		})
	}
	return meta, true
}

func (h *BillHandler) sendSaleOrderToSML(bill *models.Bill, req RetryRequest, urlOverride string, opts retrySendOptions) retrySendResult {
	id := bill.ID
	route := "SaleOrder"
	if h.saleOrderClient == nil {
		return retrySendResult{HTTPStatus: http.StatusServiceUnavailable, Error: "saleorder client not configured", Route: route}
	}

	sentItemCodes := make([]string, 0, len(bill.Items))
	items := make([]sml.SOItem, 0, len(bill.Items))
	for _, it := range bill.Items {
		if it.ItemCode == nil {
			continue
		}
		sentItemCodes = append(sentItemCodes, *it.ItemCode)
		price := 0.0
		if it.Price != nil {
			price = *it.Price
		}
		unit := ""
		if it.UnitCode != nil {
			unit = *it.UnitCode
		}
		items = append(items, sml.SOItem{
			ItemCode: *it.ItemCode,
			ItemName: h.resolveItemName(*it.ItemCode, it.RawName),
			Qty:      it.Qty,
			Price:    price,
			UnitCode: unit,
		})
	}

	def, err := h.lookupChannelDefault(bill.Source, "sale")
	if err != nil {
		return retrySendResult{HTTPStatus: http.StatusBadRequest, Error: err.Error(), Route: route}
	}
	cfg := h.resolvedSaleOrderConfig(def, req)
	if req.PartyCode != "" {
		cfg.CustCode = req.PartyCode
	}
	if cfg.CustCode == "" {
		return retrySendResult{HTTPStatus: http.StatusBadRequest, Error: "กรุณาเลือกลูกค้าก่อนส่ง SML", Route: route}
	}
	if err := h.validateResolvedSendFields(cfg.DocFormat, cfg.WHCode, cfg.ShelfCode, cfg.DocTime, cfg.VATType, cfg.VATRate); err != nil {
		return retrySendResult{HTTPStatus: http.StatusBadRequest, Error: err.Error(), Route: route}
	}

	docDate := docDateFromBill(bill)
	docRef := docRefFromBill(bill)
	docRefDate := ""
	if docRef != "" {
		docRefDate = docDate
	}
	reqDocNo, err := h.resolveRetryDocNo(req, bill, def, "NX-SO", "saleorder")
	if err != nil {
		return retrySendResult{HTTPStatus: http.StatusBadRequest, Error: "เลขเอกสาร SML ไม่ถูกต้อง: " + err.Error(), Route: route}
	}
	_ = h.billRepo.UpdateStatus(id, bill.Status, &reqDocNo, nil, nil)
	payload := sml.BuildSaleOrderPayload(reqDocNo, docDate, docRef, docRefDate, items, cfg, req.Remark, sml.SaleOrderHeaderOptions{
		Remark2: req.Remark2,
	})
	reqJSON, _ := json.Marshal(payload)

	start := time.Now()
	statusCode, resp, err := h.saleOrderClient.CreateSaleOrder(payload, urlOverride)
	if err != nil || resp == nil || !resp.IsSuccess() {
		errMsg := smlSendErrorMessage(statusCode, resp, err)
		storedErr := h.recordFailureForSend(id, bill.Source, reqJSON, fmt.Errorf("%s", errMsg), start, route, reqDocNo, opts)
		return retrySendResult{
			HTTPStatus:     http.StatusBadGateway,
			Error:          "SML send failed: " + storedErr,
			DocNoAttempted: reqDocNo,
			Route:          route,
		}
	}

	respJSON, _ := json.Marshal(resp)
	docNo := resp.GetDocNo()
	if docNo == "" {
		docNo = reqDocNo
	}
	_ = h.billRepo.UpdateStatus(id, "sent", &docNo, respJSON, nil)
	_ = h.billRepo.UpdateSMLPayload(id, reqJSON)
	h.recordSuccessForSend(id, bill.Source, respJSON, docNo, route, start, opts)
	h.triggerStockRecalculation(id, docNo, route, opts.BulkJobID, sentItemCodes)
	logWarning := extractSMLERPLogWarning(respJSON)
	return retrySendResult{
		HTTPStatus:     http.StatusOK,
		Message:        "bill sent to SML (saleorder)",
		DocNo:          docNo,
		DocNoAttempted: reqDocNo,
		Route:          route,
		LogWarning:     logWarning,
	}
}

func (h *BillHandler) sendSaleInvoiceToSML(bill *models.Bill, req RetryRequest, urlOverride string, opts retrySendOptions) retrySendResult {
	id := bill.ID
	route := "SaleInvoice"
	if h.invoiceClient == nil {
		return retrySendResult{HTTPStatus: http.StatusServiceUnavailable, Error: "saleinvoice client not configured", Route: route}
	}

	sentItemCodesInv := make([]string, 0, len(bill.Items))
	items := make([]sml.ShopeeOrderItem, 0, len(bill.Items))
	for _, it := range bill.Items {
		if it.ItemCode == nil {
			continue
		}
		sentItemCodesInv = append(sentItemCodesInv, *it.ItemCode)
		price := 0.0
		if it.Price != nil {
			price = *it.Price
		}
		items = append(items, sml.ShopeeOrderItem{
			SKU:         *it.ItemCode,
			ProductName: h.resolveItemName(*it.ItemCode, it.RawName),
			Price:       price,
			Qty:         it.Qty,
		})
	}

	def, err := h.lookupChannelDefault(bill.Source, "sale")
	if err != nil {
		return retrySendResult{HTTPStatus: http.StatusBadRequest, Error: err.Error(), Route: route}
	}
	cfg := h.resolvedInvoiceConfig(def, req)
	if req.PartyCode != "" {
		cfg.CustCode = req.PartyCode
	}
	if cfg.CustCode == "" {
		return retrySendResult{HTTPStatus: http.StatusBadRequest, Error: "กรุณาเลือกลูกค้าก่อนส่ง SML", Route: route}
	}
	if err := h.validateResolvedSendFields(cfg.DocFormat, cfg.WHCode, cfg.ShelfCode, cfg.DocTime, cfg.VATType, cfg.VATRate); err != nil {
		return retrySendResult{HTTPStatus: http.StatusBadRequest, Error: err.Error(), Route: route}
	}
	productCache := map[string]*sml.ProductInfo{}
	for _, it := range bill.Items {
		if it.ItemCode == nil || it.UnitCode == nil {
			continue
		}
		productCache[*it.ItemCode] = &sml.ProductInfo{
			Code:          *it.ItemCode,
			StartSaleUnit: *it.UnitCode,
		}
	}

	docDate := docDateFromBill(bill)
	docRef := docRefFromBill(bill)
	docRefDate := ""
	if docRef != "" {
		docRefDate = docDate
	}
	reqDocNo, err := h.resolveRetryDocNo(req, bill, def, "NX-INV", "saleinvoice")
	if err != nil {
		return retrySendResult{HTTPStatus: http.StatusBadRequest, Error: "เลขเอกสาร SML ไม่ถูกต้อง: " + err.Error(), Route: route}
	}
	_ = h.billRepo.UpdateStatus(id, bill.Status, &reqDocNo, nil, nil)
	payload := sml.BuildInvoicePayload(reqDocNo, docDate, docRef, docRefDate, items, cfg, productCache, req.Remark, sml.InvoiceHeaderOptions{
		Remark2: req.Remark2,
	})
	reqJSON, _ := json.Marshal(payload)

	start := time.Now()
	statusCode, resp, err := h.invoiceClient.CreateInvoice(payload, urlOverride)
	if err != nil || resp == nil || !resp.IsSuccess() {
		errMsg := smlSendErrorMessage(statusCode, resp, err)
		storedErr := h.recordFailureForSend(id, bill.Source, reqJSON, fmt.Errorf("%s", errMsg), start, route, reqDocNo, opts)
		return retrySendResult{
			HTTPStatus:     http.StatusBadGateway,
			Error:          "SML send failed: " + storedErr,
			DocNoAttempted: reqDocNo,
			Route:          route,
		}
	}

	respJSON, _ := json.Marshal(resp)
	docNo := resp.GetDocNo()
	if docNo == "" {
		docNo = reqDocNo
	}
	_ = h.billRepo.UpdateStatus(id, "sent", &docNo, respJSON, nil)
	_ = h.billRepo.UpdateSMLPayload(id, reqJSON)
	h.recordSuccessForSend(id, bill.Source, respJSON, docNo, route, start, opts)
	h.triggerStockRecalculation(id, docNo, route, opts.BulkJobID, sentItemCodesInv)
	logWarning := extractSMLERPLogWarning(respJSON)
	return retrySendResult{
		HTTPStatus:     http.StatusOK,
		Message:        "bill sent to SML (saleinvoice)",
		DocNo:          docNo,
		DocNoAttempted: reqDocNo,
		Route:          route,
		LogWarning:     logWarning,
	}
}

func (h *BillHandler) sendPurchaseOrderToSML(bill *models.Bill, req RetryRequest, urlOverride string, opts retrySendOptions) retrySendResult {
	id := bill.ID
	route := "PurchaseOrder"
	if h.poClient == nil {
		return retrySendResult{HTTPStatus: http.StatusServiceUnavailable, Error: "purchaseorder client not configured", Route: route}
	}

	sentItemCodesPO := make([]string, 0, len(bill.Items))
	items := make([]sml.POItem, 0, len(bill.Items))
	for _, it := range bill.Items {
		if it.ItemCode == nil {
			continue
		}
		sentItemCodesPO = append(sentItemCodesPO, *it.ItemCode)
		price := 0.0
		if it.Price != nil {
			price = *it.Price
		}
		unit := ""
		if it.UnitCode != nil {
			unit = *it.UnitCode
		}
		items = append(items, sml.POItem{
			ItemCode:       *it.ItemCode,
			ItemName:       h.resolveItemName(*it.ItemCode, it.RawName),
			Qty:            it.Qty,
			Price:          price,
			DiscountAmount: it.DiscountAmount,
			UnitCode:       unit,
		})
	}

	def, err := h.lookupChannelDefault(bill.Source, "purchase")
	if err != nil {
		return retrySendResult{HTTPStatus: http.StatusBadRequest, Error: err.Error(), Route: route}
	}
	cfg := h.resolvedPurchaseConfig(def, req)
	if req.PartyCode != "" {
		cfg.CustCode = req.PartyCode
	}
	if req.PartyName != "" {
		cfg.SupplierName = req.PartyName
	}
	if cfg.CustCode == "" {
		return retrySendResult{HTTPStatus: http.StatusBadRequest, Error: "กรุณาเลือกผู้ขายก่อนส่ง SML", Route: route}
	}
	if err := h.validateResolvedSendFields(cfg.DocFormat, cfg.WHCode, cfg.ShelfCode, cfg.DocTime, cfg.VATType, cfg.VATRate); err != nil {
		return retrySendResult{HTTPStatus: http.StatusBadRequest, Error: err.Error(), Route: route}
	}
	inquiryType, err := validatePurchaseInquiryType(req.InquiryType)
	if err != nil {
		return retrySendResult{HTTPStatus: http.StatusBadRequest, Error: err.Error(), Route: route}
	}

	docDate := docDateFromBill(bill)
	docRef := docRefFromBill(bill)
	docRefDate := ""
	remark := req.Remark
	remark5 := ""
	if isShopeePurchaseEmailBill(bill) {
		docRef = shopeePurchaseDocRefFromBill(bill)
		remark = shopeePurchaseSellerFromBill(bill)
		remark5 = docRefFromBill(bill)
	} else if docRef != "" {
		docRefDate = docDate
	}
	reqDocNo, err := h.resolveRetryDocNo(req, bill, def, "NX-PO", "purchaseorder")
	if err != nil {
		return retrySendResult{HTTPStatus: http.StatusBadRequest, Error: "เลขเอกสาร SML ไม่ถูกต้อง: " + err.Error(), Route: route}
	}
	if req.Remark != "" && !isShopeePurchaseEmailBill(bill) {
		_ = h.billRepo.UpdateRemark(id, req.Remark)
	}
	_ = h.billRepo.UpdateStatus(id, bill.Status, &reqDocNo, nil, nil)
	payload := sml.BuildPurchaseOrderPayload(reqDocNo, docDate, docRef, docRefDate, items, cfg, remark, sml.PurchaseOrderHeaderOptions{
		Remark:      remark,
		Remark2:     req.Remark2,
		Remark5:     remark5,
		InquiryType: inquiryType,
	})
	reqJSON, _ := json.Marshal(payload)

	start := time.Now()
	statusCode, resp, err := h.poClient.CreatePurchaseOrder(payload, urlOverride)
	if err != nil || resp == nil || !resp.IsSuccess() {
		errMsg := smlSendErrorMessage(statusCode, resp, err)
		storedErr := h.recordFailureForSend(id, bill.Source, reqJSON, fmt.Errorf("%s", errMsg), start, route, reqDocNo, opts)
		return retrySendResult{
			HTTPStatus:     http.StatusBadGateway,
			Error:          "SML send failed: " + storedErr,
			DocNoAttempted: reqDocNo,
			Route:          route,
		}
	}

	respJSON, _ := json.Marshal(resp)
	docNo := resp.GetDocNo()
	if docNo == "" {
		docNo = reqDocNo
	}
	_ = h.billRepo.UpdateStatus(id, "sent", &docNo, respJSON, nil)
	_ = h.billRepo.UpdateSMLPayload(id, reqJSON)
	h.recordSuccessForSend(id, bill.Source, respJSON, docNo, route, start, opts)
	h.triggerStockRecalculation(id, docNo, route, opts.BulkJobID, sentItemCodesPO)
	logWarning := extractSMLERPLogWarning(respJSON)
	return retrySendResult{
		HTTPStatus:     http.StatusOK,
		Message:        "bill sent to SML (purchaseorder)",
		DocNo:          docNo,
		DocNoAttempted: reqDocNo,
		Route:          route,
		LogWarning:     logWarning,
	}
}

func (h *BillHandler) ensureShopeeShippingLineForSend(bill *models.Bill) (*models.BillItem, error) {
	if !isShopeePurchaseEmailBill(bill) || h.channelDefaults == nil || h.billRepo == nil {
		return nil, nil
	}
	for _, item := range bill.Items {
		if item.SourceSKU == models.ShopeeShippingSourceSKU {
			return nil, nil
		}
	}
	rd := rawDataMapFromBill(bill)
	shippingAmount, ok := rawMoneyField(rd, "shipping_amount")
	if !ok || shippingAmount < 0 {
		return nil, nil
	}
	def, err := h.channelDefaults.Get(bill.Source, bill.BillType)
	if err != nil {
		return nil, err
	}
	if def == nil || !def.ShippingItemEnabled {
		return nil, nil
	}
	code := strings.TrimSpace(def.ShippingItemCode)
	if code == "" {
		return nil, fmt.Errorf("เปิดใช้ค่าขนส่ง Shopee แต่ยังไม่ได้เลือกสินค้า SML")
	}
	unit := strings.TrimSpace(def.ShippingItemUnitCode)
	rawName := "ค่าจัดส่งสินค้า"
	if h.catalogRepo != nil {
		if cat, err := h.catalogRepo.GetOne(code); err == nil && cat != nil {
			if strings.TrimSpace(cat.ItemName) != "" {
				rawName = strings.TrimSpace(cat.ItemName)
			}
			if unit == "" {
				unit = strings.TrimSpace(cat.UnitCode)
			}
		}
	}
	itemCode := code
	price := shippingAmount
	item := models.BillItem{
		BillID:    bill.ID,
		RawName:   rawName,
		SourceSKU: models.ShopeeShippingSourceSKU,
		ItemCode:  &itemCode,
		Qty:       1,
		Price:     &price,
		Mapped:    true,
	}
	if unit != "" {
		item.UnitCode = &unit
	}
	if err := h.billRepo.InsertItem(&item); err != nil {
		return nil, err
	}
	bill.Items = append(bill.Items, item)
	return &bill.Items[len(bill.Items)-1], nil
}

type smlMessageResponse interface {
	GetMessage() string
}

func smlSendErrorMessage(statusCode int, resp smlMessageResponse, err error) string {
	switch {
	case err != nil:
		return err.Error()
	case resp != nil:
		msg := strings.TrimSpace(resp.GetMessage())
		if msg == "" {
			if statusCode == http.StatusNotFound {
				return "HTTP 404 — ไม่พบ endpoint SML ที่ตั้งไว้ กรุณาตรวจ SML REST URL ใน /settings/instance และปลายทางใน /settings/channels"
			}
			return fmt.Sprintf("HTTP %d", statusCode)
		}
		return fmt.Sprintf("HTTP %d — %s", statusCode, msg)
	default:
		return fmt.Sprintf("HTTP %d", statusCode)
	}
}

type createBulkSendJobRequest struct {
	ClientRequestID string                 `json:"client_request_id"`
	BillIDs         []string               `json:"bill_ids"`
	Payload         RetryRequest           `json:"payload"`
	FilterSnapshot  map[string]interface{} `json:"filter_snapshot"`
	Source          string                 `json:"source"`
	BillType        string                 `json:"bill_type"`
	DocumentRoute   string                 `json:"document_route"`
	Title           string                 `json:"title"`
}

type retryFailedBulkSendJobRequest struct {
	ClientRequestID string `json:"client_request_id"`
}

func (h *BillHandler) ListBulkSendJobs(c *gin.Context) {
	if h.bulkJobRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "bulk send job store not configured"})
		return
	}
	page := positiveIntQuery(c, "page", 1, 1, 1000000)
	perPage := positiveIntQuery(c, "per_page", 20, 1, 100)
	status := strings.TrimSpace(c.Query("status"))
	if status != "" && !validBulkJobStatus(status) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid status"})
		return
	}
	result, err := h.bulkJobRepo.List(repository.SMLBulkJobListFilter{
		Status:        status,
		Source:        c.Query("source"),
		BillType:      c.Query("bill_type"),
		DocumentRoute: c.Query("document_route"),
		Page:          page,
		PerPage:       perPage,
	})
	if err != nil {
		h.log.Error("list bulk send jobs", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list bulk send jobs failed"})
		return
	}
	if result.Jobs == nil {
		result.Jobs = []models.SMLBulkJob{}
	}
	c.JSON(http.StatusOK, gin.H{
		"data":     result.Jobs,
		"total":    result.Total,
		"page":     result.Page,
		"per_page": result.PerPage,
		"has_more": result.Page*result.PerPage < result.Total,
	})
}

func (h *BillHandler) CreateBulkSendJob(c *gin.Context) {
	if h.bulkJobRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "bulk send job store not configured"})
		return
	}
	var req createBulkSendJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.ClientRequestID = strings.TrimSpace(req.ClientRequestID)
	if req.ClientRequestID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "client_request_id is required"})
		return
	}
	if existing, err := h.bulkJobRepo.FindByClientRequestID(req.ClientRequestID); err == nil && existing != nil {
		c.JSON(http.StatusAccepted, gin.H{"job_id": existing.ID, "job": existing})
		return
	}
	if err := validateBulkBillIDs(req.BillIDs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateBulkSendPayload(req.BillType, req.DocumentRoute, req.Payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if h.blockIfSMLNotReady(c, "sml_readiness_blocked", nil, "bulk_job_create") {
		return
	}
	payloadJSON, _ := json.Marshal(req.Payload)
	filterJSON, _ := json.Marshal(req.FilterSnapshot)
	job, err := h.bulkJobRepo.Create(repository.CreateSMLBulkJobInput{
		ClientRequestID: req.ClientRequestID,
		BillIDs:         req.BillIDs,
		Source:          req.Source,
		BillType:        req.BillType,
		DocumentRoute:   req.DocumentRoute,
		Title:           req.Title,
		RequestPayload:  payloadJSON,
		FilterSnapshot:  filterJSON,
		CreatedBy:       c.GetString("user_id"),
		CreatedByEmail:  c.GetString("user_email"),
	})
	if err != nil {
		switch e := err.(type) {
		case repository.ActiveBulkJobConflictError:
			c.JSON(http.StatusConflict, gin.H{
				"error":    "บางบิลอยู่ใน bulk job ที่ยังทำงานอยู่",
				"bill_ids": e.BillIDs,
			})
		default:
			h.log.Error("create bulk send job", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "create bulk send job failed"})
		}
		return
	}
	go h.runBulkSendJob(job.ID)
	c.JSON(http.StatusAccepted, gin.H{"job_id": job.ID, "job": job})
}

func (h *BillHandler) GetBulkSendJob(c *gin.Context) {
	if h.bulkJobRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "bulk send job store not configured"})
		return
	}
	job, err := h.bulkJobRepo.Get(c.Param("job_id"))
	if err != nil {
		h.log.Error("get bulk send job", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "get bulk send job failed"})
		return
	}
	if job == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "bulk send job not found"})
		return
	}
	c.JSON(http.StatusOK, job)
}

func (h *BillHandler) GetActiveBulkSendJob(c *gin.Context) {
	if h.bulkJobRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "bulk send job store not configured"})
		return
	}
	job, err := h.bulkJobRepo.FindActive(
		c.Query("source"),
		c.Query("bill_type"),
		c.Query("document_route"),
		c.GetString("user_id"),
	)
	if err != nil {
		h.log.Error("get active bulk send job", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "get active bulk send job failed"})
		return
	}
	if job == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "active bulk send job not found"})
		return
	}
	if !bulkJobMatchesSnapshotFilter(job.FilterSnapshot, "shopee_shop_id", c.Query("shopee_shop_id")) {
		c.JSON(http.StatusNotFound, gin.H{"error": "active bulk send job not found"})
		return
	}
	c.JSON(http.StatusOK, job)
}

func bulkJobMatchesSnapshotFilter(snapshot json.RawMessage, key, expected string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return true
	}
	var filter map[string]interface{}
	if err := json.Unmarshal(snapshot, &filter); err != nil {
		return false
	}
	got, ok := filter[key]
	if !ok {
		return false
	}
	switch v := got.(type) {
	case string:
		return strings.TrimSpace(v) == expected
	default:
		return strings.TrimSpace(fmt.Sprint(v)) == expected
	}
}

func positiveIntQuery(c *gin.Context, key string, fallback, min, max int) int {
	v, err := strconv.Atoi(strings.TrimSpace(c.Query(key)))
	if err != nil {
		return fallback
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func validBulkJobStatus(status string) bool {
	switch models.SMLBulkJobStatus(status) {
	case models.SMLBulkJobQueued,
		models.SMLBulkJobRunning,
		models.SMLBulkJobCompleted,
		models.SMLBulkJobCompletedWithErrors,
		models.SMLBulkJobFailed:
		return true
	default:
		return false
	}
}

func validateBulkSendPayload(billType, documentRoute string, payload RetryRequest) error {
	if err := validateRemark2(payload.Remark2); err != nil {
		return err
	}
	if strings.TrimSpace(billType) != "purchase" && strings.TrimSpace(documentRoute) != "purchaseorder" {
		return nil
	}
	_, err := validatePurchaseInquiryType(payload.InquiryType)
	return err
}

func (h *BillHandler) RetryFailedBulkSendJob(c *gin.Context) {
	if h.bulkJobRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "bulk send job store not configured"})
		return
	}
	jobID := c.Param("job_id")
	original, err := h.bulkJobRepo.Get(jobID)
	if err != nil {
		h.log.Error("get bulk send job for retry failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "get bulk send job failed"})
		return
	}
	if original == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "bulk send job not found"})
		return
	}
	var req retryFailedBulkSendJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.ClientRequestID = strings.TrimSpace(req.ClientRequestID)
	if req.ClientRequestID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "client_request_id is required"})
		return
	}
	if existing, err := h.bulkJobRepo.FindByClientRequestID(req.ClientRequestID); err == nil && existing != nil {
		c.JSON(http.StatusAccepted, gin.H{"job_id": existing.ID, "job": existing})
		return
	}
	failedBillIDs, err := h.bulkJobRepo.FailedBillIDs(jobID)
	if err != nil {
		h.log.Error("list failed bulk job bills", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list failed bills failed"})
		return
	}
	if len(failedBillIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ไม่มีรายการที่ไม่สำเร็จให้ retry"})
		return
	}
	var originalPayload RetryRequest
	if len(original.RequestPayload) > 0 {
		if err := json.Unmarshal(original.RequestPayload, &originalPayload); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "bulk job เดิมมี payload ไม่ถูกต้อง กรุณาเปิด dialog ส่งใหม่"})
			return
		}
	}
	if err := validateBulkSendPayload(original.BillType, original.DocumentRoute, originalPayload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if h.blockIfSMLNotReady(c, "sml_readiness_blocked", nil, "bulk_job_retry_failed") {
		return
	}
	filterJSON := appendRetryOfJob(original.FilterSnapshot, original.ID)
	newJob, err := h.bulkJobRepo.Create(repository.CreateSMLBulkJobInput{
		ClientRequestID: req.ClientRequestID,
		BillIDs:         failedBillIDs,
		Source:          original.Source,
		BillType:        original.BillType,
		DocumentRoute:   original.DocumentRoute,
		Title:           original.Title + " (retry failed)",
		RequestPayload:  original.RequestPayload,
		FilterSnapshot:  filterJSON,
		CreatedBy:       c.GetString("user_id"),
		CreatedByEmail:  c.GetString("user_email"),
	})
	if err != nil {
		switch e := err.(type) {
		case repository.ActiveBulkJobConflictError:
			c.JSON(http.StatusConflict, gin.H{
				"error":    "บางบิลอยู่ใน bulk job ที่ยังทำงานอยู่",
				"bill_ids": e.BillIDs,
			})
		default:
			h.log.Error("create retry failed bulk send job", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "create retry failed bulk send job failed"})
		}
		return
	}
	go h.runBulkSendJob(newJob.ID)
	c.JSON(http.StatusAccepted, gin.H{"job_id": newJob.ID, "job": newJob})
}

func (h *BillHandler) RecoverInterruptedBulkSendJobs() {
	if h.bulkJobRepo == nil {
		return
	}
	n, err := h.bulkJobRepo.RecoverInterruptedActiveJobs("server interrupted bulk SML send; retry failed rows safely")
	if err != nil {
		h.log.Warn("recover interrupted bulk send jobs failed", zap.Error(err))
		return
	}
	if n > 0 {
		h.log.Warn("recovered interrupted bulk send jobs", zap.Int("jobs", n))
	}
}

func (h *BillHandler) runBulkSendJob(jobID string) {
	if h.bulkJobRepo == nil {
		return
	}
	// Clean up warn-once tracker when the job finishes (success or failure)
	defer func() {
		stockWarnOnceMu.Lock()
		delete(stockWarnedJobs, jobID)
		stockWarnOnceMu.Unlock()
	}()

	if err := h.bulkJobRepo.StartJob(jobID); err != nil {
		h.log.Error("start bulk send job", zap.String("job_id", jobID), zap.Error(err))
		_ = h.bulkJobRepo.MarkJobFailed(jobID, err.Error())
		return
	}
	job, err := h.bulkJobRepo.Get(jobID)
	if err != nil || job == nil {
		if err != nil {
			h.log.Error("load bulk send job", zap.String("job_id", jobID), zap.Error(err))
			_ = h.bulkJobRepo.MarkJobFailed(jobID, err.Error())
		}
		return
	}
	var payload RetryRequest
	if len(job.RequestPayload) > 0 {
		if err := json.Unmarshal(job.RequestPayload, &payload); err != nil {
			_ = h.bulkJobRepo.MarkJobFailed(jobID, "invalid bulk job payload: "+err.Error())
			return
		}
	}
	if err := validateBulkSendPayload(job.BillType, job.DocumentRoute, payload); err != nil {
		_ = h.bulkJobRepo.MarkJobFailed(jobID, err.Error())
		return
	}
	traceID := fmt.Sprintf("bulk-job-%s", job.ID)
	userID := ""
	if job.CreatedBy != nil {
		userID = *job.CreatedBy
	}
	readiness := h.checkSMLReadiness(context.Background(), false)
	if !readiness.Ready {
		h.auditSMLReadinessBlocked("sml_readiness_blocked", nil, userID, traceID, "bulk_job", readiness)
		_ = h.bulkJobRepo.MarkJobFailed(jobID, readiness.Message)
		return
	}

	for _, item := range job.Items {
		if item.Status != models.SMLBulkJobItemQueued {
			continue
		}
		if err := h.bulkJobRepo.StartItem(item.ID); err != nil {
			h.log.Error("start bulk send item", zap.String("job_id", jobID), zap.String("item_id", item.ID), zap.Error(err))
			continue
		}

		bill, err := h.billRepo.FindByID(item.BillID)
		if err != nil || bill == nil {
			reason := "bill not found"
			if err != nil {
				reason = "load bill failed: " + err.Error()
			}
			_ = h.bulkJobRepo.FinishItemSkipped(item.ID, reason)
			_ = h.bulkJobRepo.RefreshCounts(jobID)
			continue
		}
		if bill.ArchivedAt != nil {
			_ = h.bulkJobRepo.FinishItemSkipped(item.ID, "บิลถูก archive แล้ว")
			_ = h.bulkJobRepo.RefreshCounts(jobID)
			continue
		}
		if bill.Status != "pending" && bill.Status != "failed" && bill.Status != "needs_review" {
			_ = h.bulkJobRepo.FinishItemSkipped(item.ID, "สถานะบิลเปลี่ยนเป็น "+bill.Status)
			_ = h.bulkJobRepo.RefreshCounts(jobID)
			continue
		}

		result := h.sendBillToSML(bill, payload, retrySendOptions{
			UserID:            userID,
			TraceID:           traceID,
			Via:               "bulk_job",
			BulkJobID:         job.ID,
			BulkJobItemID:     item.ID,
			BulkItemSequence:  item.Sequence,
			SuppressLineAlert: true,
		})
		switch {
		case result.HTTPStatus == http.StatusOK:
			_ = h.bulkJobRepo.FinishItemSent(item.ID, result.DocNo, result.DocNoAttempted)
		case result.Skipped || result.HTTPStatus == http.StatusAccepted:
			msg := result.Message
			if msg == "" {
				msg = result.Error
			}
			_ = h.bulkJobRepo.FinishItemSkipped(item.ID, msg)
		default:
			msg := result.Error
			if msg == "" {
				msg = "ส่ง SML ไม่สำเร็จ"
			}
			_ = h.bulkJobRepo.FinishItemFailed(item.ID, msg, result.DocNoAttempted)
		}
		_ = h.bulkJobRepo.RefreshCounts(jobID)
	}
	if err := h.bulkJobRepo.FinalizeJob(jobID); err != nil {
		h.log.Error("finalize bulk send job", zap.String("job_id", jobID), zap.Error(err))
		_ = h.bulkJobRepo.MarkJobFailed(jobID, err.Error())
		return
	}
	h.notifyBulkSendSummary(jobID)
}

func (h *BillHandler) notifyBulkSendSummary(jobID string) {
	if h.lineSvc == nil || h.bulkJobRepo == nil {
		return
	}
	job, err := h.bulkJobRepo.Get(jobID)
	if err != nil || job == nil || job.FailedCount == 0 {
		return
	}
	_ = h.lineSvc.PushAdmin(fmt.Sprintf(
		"⚠️ Bulk SML send finished with failures\nJob: %s\nสำเร็จ: %d\nไม่สำเร็จ: %d\nข้าม: %d\nเปิด Nexflow แล้วกด Retry failed เฉพาะรายการที่พลาด",
		job.ID, job.SentCount, job.FailedCount, job.SkippedCount,
	))
}

func validateBulkBillIDs(ids []string) error {
	if len(ids) == 0 {
		return fmt.Errorf("bill_ids is required")
	}
	if len(ids) > 100 {
		return fmt.Errorf("bulk send จำกัดที่ 100 บิลต่อรอบ")
	}
	seen := map[string]bool{}
	for i, id := range ids {
		id = strings.TrimSpace(id)
		if !isUUIDLike(id) {
			return fmt.Errorf("bill_ids[%d] is not a valid UUID", i)
		}
		if seen[id] {
			return fmt.Errorf("bill_ids contains duplicate bill: %s", id)
		}
		seen[id] = true
		ids[i] = id
	}
	return nil
}

func isUUIDLike(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, ch := range value {
		switch i {
		case 8, 13, 18, 23:
			if ch != '-' {
				return false
			}
		default:
			if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')) {
				return false
			}
		}
	}
	return true
}

func appendRetryOfJob(raw json.RawMessage, jobID string) json.RawMessage {
	out := map[string]interface{}{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	out["retry_of_job_id"] = jobID
	b, _ := json.Marshal(out)
	return b
}

// ─── Route 1: SML 248 saleorder REST ─────────────────────────────────────────
// ใบสั่งขาย — landed in /v3/api/saleorder, the sale-side counterpart to
// purchaseorder. Replaces the old saleinvoice default path so Shopee
// orders show up under "ใบสั่งขาย" in SML instead of "ใบกำกับภาษี".
func (h *BillHandler) retrySaleOrder(c *gin.Context, bill *models.Bill, req RetryRequest) {
	id := bill.ID
	if h.saleOrderClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "saleorder client not configured"})
		return
	}

	items := make([]sml.SOItem, 0, len(bill.Items))
	for _, it := range bill.Items {
		if it.ItemCode == nil {
			continue
		}
		price := 0.0
		if it.Price != nil {
			price = *it.Price
		}
		unit := ""
		if it.UnitCode != nil {
			unit = *it.UnitCode
		}
		items = append(items, sml.SOItem{
			ItemCode: *it.ItemCode,
			ItemName: h.resolveItemName(*it.ItemCode, it.RawName),
			Qty:      it.Qty,
			Price:    price,
			UnitCode: unit,
		})
	}

	def, err := h.lookupChannelDefault(bill.Source, "sale")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	cfg := h.resolvedSaleOrderConfig(def, req)
	if req.PartyCode != "" {
		cfg.CustCode = req.PartyCode
	}
	if cfg.CustCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณาเลือกลูกค้าก่อนส่ง SML"})
		return
	}
	if err := h.validateResolvedSendFields(cfg.DocFormat, cfg.WHCode, cfg.ShelfCode, cfg.DocTime, cfg.VATType, cfg.VATRate); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	docDate := docDateFromBill(bill)
	docRef := docRefFromBill(bill)
	docRefDate := ""
	if docRef != "" {
		docRefDate = docDate
	}
	reqDocNo, err := h.resolveRetryDocNo(req, bill, def, "NX-SO", "saleorder")
	if err != nil {
		h.writeDocNoError(c, err)
		return
	}
	// Stamp doc_no on the bill BEFORE calling SML so a re-retry uses the same
	// number (no counter inflation, no duplicate docs in SML on transient fail).
	_ = h.billRepo.UpdateStatus(id, bill.Status, &reqDocNo, nil, nil)
	payload := sml.BuildSaleOrderPayload(reqDocNo, docDate, docRef, docRefDate, items, cfg, req.Remark, sml.SaleOrderHeaderOptions{
		Remark2: req.Remark2,
	})
	reqJSON, _ := json.Marshal(payload)

	start := time.Now()
	urlOverride := c.GetString("sml_url_override")
	statusCode, resp, err := h.saleOrderClient.CreateSaleOrder(payload, urlOverride)
	if err != nil || resp == nil || !resp.IsSuccess() {
		errMsg := smlSendErrorMessage(statusCode, resp, err)
		h.recordFailure(c, id, bill.Source, reqJSON, fmt.Errorf("%s", errMsg), start, "SaleOrder", reqDocNo)
		return
	}

	respJSON, _ := json.Marshal(resp)
	// SML often returns success with an empty data.doc_no — fall back to
	// the client-generated code so the bill is still trackable.
	docNo := resp.GetDocNo()
	if docNo == "" {
		docNo = reqDocNo
	}
	_ = h.billRepo.UpdateStatus(id, "sent", &docNo, respJSON, nil)
	_ = h.billRepo.UpdateSMLPayload(id, reqJSON)
	h.recordSuccess(c, id, bill.Source, respJSON, docNo, "SaleOrder", start)
	c.JSON(http.StatusOK, gin.H{"message": "bill sent to SML (saleorder)", "doc_no": docNo})
}

// ─── Route 2b: SML 248 saleinvoice REST (legacy ใบกำกับภาษี) ─────────────────
// Kept for admins who explicitly select endpoint="saleinvoice" on a channel
// (e.g. they need invoices instead of sale orders for tax purposes).
func (h *BillHandler) retrySaleInvoice(c *gin.Context, bill *models.Bill, req RetryRequest) {
	id := bill.ID
	if h.invoiceClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "saleinvoice client not configured"})
		return
	}

	items := make([]sml.ShopeeOrderItem, 0, len(bill.Items))
	for _, it := range bill.Items {
		if it.ItemCode == nil {
			continue
		}
		price := 0.0
		if it.Price != nil {
			price = *it.Price
		}
		items = append(items, sml.ShopeeOrderItem{
			SKU:         *it.ItemCode,
			ProductName: h.resolveItemName(*it.ItemCode, it.RawName),
			Price:       price,
			Qty:         it.Qty,
		})
	}

	def, err := h.lookupChannelDefault(bill.Source, "sale")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	cfg := h.resolvedInvoiceConfig(def, req)
	if req.PartyCode != "" {
		cfg.CustCode = req.PartyCode
	}
	if cfg.CustCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณาเลือกลูกค้าก่อนส่ง SML"})
		return
	}
	if err := h.validateResolvedSendFields(cfg.DocFormat, cfg.WHCode, cfg.ShelfCode, cfg.DocTime, cfg.VATType, cfg.VATRate); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	productCache := map[string]*sml.ProductInfo{}
	for _, it := range bill.Items {
		if it.ItemCode == nil || it.UnitCode == nil {
			continue
		}
		productCache[*it.ItemCode] = &sml.ProductInfo{
			Code:          *it.ItemCode,
			StartSaleUnit: *it.UnitCode,
		}
	}

	docDate := docDateFromBill(bill)
	docRef := docRefFromBill(bill)
	docRefDate := ""
	if docRef != "" {
		docRefDate = docDate
	}
	reqDocNo, err := h.resolveRetryDocNo(req, bill, def, "NX-INV", "saleinvoice")
	if err != nil {
		h.writeDocNoError(c, err)
		return
	}
	_ = h.billRepo.UpdateStatus(id, bill.Status, &reqDocNo, nil, nil)
	payload := sml.BuildInvoicePayload(reqDocNo, docDate, docRef, docRefDate, items, cfg, productCache, req.Remark, sml.InvoiceHeaderOptions{
		Remark2: req.Remark2,
	})
	reqJSON, _ := json.Marshal(payload)

	start := time.Now()
	urlOverride := c.GetString("sml_url_override")
	statusCode, resp, err := h.invoiceClient.CreateInvoice(payload, urlOverride)
	if err != nil || resp == nil || !resp.IsSuccess() {
		errMsg := smlSendErrorMessage(statusCode, resp, err)
		h.recordFailure(c, id, bill.Source, reqJSON, fmt.Errorf("%s", errMsg), start, "SaleInvoice", reqDocNo)
		return
	}

	respJSON, _ := json.Marshal(resp)
	docNo := resp.GetDocNo()
	if docNo == "" {
		docNo = reqDocNo
	}
	_ = h.billRepo.UpdateStatus(id, "sent", &docNo, respJSON, nil)
	_ = h.billRepo.UpdateSMLPayload(id, reqJSON)
	h.recordSuccess(c, id, bill.Source, respJSON, docNo, "SaleInvoice", start)
	c.JSON(http.StatusOK, gin.H{"message": "bill sent to SML (saleinvoice)", "doc_no": docNo})
}

// ─── Route 3: SML 248 purchaseorder REST (shopee_shipped) ────────────────────
func (h *BillHandler) retryPurchaseOrder(c *gin.Context, bill *models.Bill, req RetryRequest) {
	id := bill.ID
	if h.poClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "purchaseorder client not configured"})
		return
	}

	items := make([]sml.POItem, 0, len(bill.Items))
	for _, it := range bill.Items {
		if it.ItemCode == nil {
			continue
		}
		price := 0.0
		if it.Price != nil {
			price = *it.Price
		}
		unit := ""
		if it.UnitCode != nil {
			unit = *it.UnitCode
		}
		items = append(items, sml.POItem{
			ItemCode:       *it.ItemCode,
			ItemName:       h.resolveItemName(*it.ItemCode, it.RawName),
			Qty:            it.Qty,
			Price:          price,
			DiscountAmount: it.DiscountAmount,
			UnitCode:       unit,
		})
	}

	def, err := h.lookupChannelDefault(bill.Source, "purchase")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	cfg := h.resolvedPurchaseConfig(def, req)
	if req.PartyCode != "" {
		cfg.CustCode = req.PartyCode
	}
	if req.PartyName != "" {
		cfg.SupplierName = req.PartyName
	}
	if cfg.CustCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณาเลือกผู้ขายก่อนส่ง SML"})
		return
	}
	if err := h.validateResolvedSendFields(cfg.DocFormat, cfg.WHCode, cfg.ShelfCode, cfg.DocTime, cfg.VATType, cfg.VATRate); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	inquiryType, err := validatePurchaseInquiryType(req.InquiryType)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	docDate := docDateFromBill(bill)
	docRef := docRefFromBill(bill)
	docRefDate := ""
	remark := req.Remark
	remark5 := ""
	if isShopeePurchaseEmailBill(bill) {
		docRef = shopeePurchaseDocRefFromBill(bill)
		remark = shopeePurchaseSellerFromBill(bill)
		remark5 = docRefFromBill(bill)
	} else if docRef != "" {
		docRefDate = docDate
	}
	reqDocNo, err := h.resolveRetryDocNo(req, bill, def, "NX-PO", "purchaseorder")
	if err != nil {
		h.writeDocNoError(c, err)
		return
	}
	// Persist remark before SML call so it's available even on failure
	if req.Remark != "" && !isShopeePurchaseEmailBill(bill) {
		_ = h.billRepo.UpdateRemark(id, req.Remark)
	}
	_ = h.billRepo.UpdateStatus(id, bill.Status, &reqDocNo, nil, nil)
	payload := sml.BuildPurchaseOrderPayload(reqDocNo, docDate, docRef, docRefDate, items, cfg, remark, sml.PurchaseOrderHeaderOptions{
		Remark:      remark,
		Remark2:     req.Remark2,
		Remark5:     remark5,
		InquiryType: inquiryType,
	})
	reqJSON, _ := json.Marshal(payload)

	start := time.Now()
	urlOverride := c.GetString("sml_url_override")
	statusCode, resp, err := h.poClient.CreatePurchaseOrder(payload, urlOverride)
	if err != nil || resp == nil || !resp.IsSuccess() {
		errMsg := smlSendErrorMessage(statusCode, resp, err)
		h.recordFailure(c, id, bill.Source, reqJSON, fmt.Errorf("%s", errMsg), start, "PurchaseOrder", reqDocNo)
		return
	}

	respJSON, _ := json.Marshal(resp)
	// SML purchaseorder returns success but with an empty doc_no field —
	// fall back to the doc_no we generated client-side so the bill is
	// still trackable in the UI.
	docNo := resp.GetDocNo()
	if docNo == "" {
		docNo = reqDocNo
	}
	_ = h.billRepo.UpdateStatus(id, "sent", &docNo, respJSON, nil)
	_ = h.billRepo.UpdateSMLPayload(id, reqJSON)
	h.recordSuccess(c, id, bill.Source, respJSON, docNo, "PurchaseOrder", start)
	c.JSON(http.StatusOK, gin.H{"message": "bill sent to SML (purchaseorder)", "doc_no": docNo})
}

// docDateFromBill returns "YYYY-MM-DD" — the email-extracted doc_date stored
// in raw_data["doc_date"] when present, else today's date.
// Used by saleinvoice + purchaseorder retry paths so SML records reflect the
// real order/ship date rather than the moment the user clicked "ส่ง".
func docDateFromBill(bill *models.Bill) string {
	if bill != nil && bill.RawData != nil {
		var rd map[string]interface{}
		if err := json.Unmarshal(bill.RawData, &rd); err == nil {
			if v, ok := rd["doc_date"].(string); ok && v != "" {
				return v
			}
		}
	}
	return time.Now().Format("2006-01-02")
}

// docRefFromBill returns the upstream Shopee order number for SML doc_ref.
// Shopee email extraction has used a few key names across iterations, so this
// accepts the known aliases and keeps the SML reference stable.
func docRefFromBill(bill *models.Bill) string {
	if bill == nil || bill.RawData == nil {
		return ""
	}
	var rd map[string]interface{}
	if err := json.Unmarshal(bill.RawData, &rd); err != nil {
		return ""
	}
	for _, key := range []string{"shopee_order_id", "order_id", "order_no", "doc_ref"} {
		if v, ok := rd[key].(string); ok {
			if s := strings.TrimSpace(v); s != "" {
				return strings.TrimLeft(s, "#")
			}
		}
	}
	return ""
}

func isShopeePurchaseEmailBill(bill *models.Bill) bool {
	return bill != nil && bill.Source == "shopee_shipped" && bill.BillType == "purchase"
}

func validatePurchaseInquiryType(value *int) (int, error) {
	if value == nil {
		return 0, fmt.Errorf("กรุณาเลือกประเภทรายการก่อนส่งใบสั่งซื้อเข้า SML")
	}
	switch *value {
	case 0, 1, 3, 4:
		return *value, nil
	default:
		return 0, fmt.Errorf("ประเภทรายการไม่ถูกต้อง (เลือกได้เฉพาะ 0, 1, 3, 4)")
	}
}

func validateRemark2(value string) error {
	switch value {
	case "", "tax", "notax", "re":
		return nil
	default:
		return fmt.Errorf("สถานะเอกสารไม่ถูกต้อง (เลือกได้เฉพาะ tax, notax, re)")
	}
}

func shopeePurchaseSellerFromBill(bill *models.Bill) string {
	rd := rawDataMapFromBill(bill)
	if rd == nil {
		return ""
	}
	for _, key := range []string{"seller_name", "supplier_name"} {
		if v, ok := rd[key].(string); ok {
			if s := strings.TrimSpace(v); s != "" {
				return s
			}
		}
	}
	return ""
}

func shopeePurchaseDocRefFromBill(bill *models.Bill) string {
	rd := rawDataMapFromBill(bill)
	if rd == nil {
		return ""
	}
	summary, ok := rd["payment_summary"].(map[string]interface{})
	if !ok {
		return ""
	}
	if isCard, _ := summary["is_credit_debit_card"].(bool); !isCard {
		return ""
	}
	if v, ok := summary["doc_ref_amount"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if v, ok := summary["payment_paid_amount"].(float64); ok && v > 0 {
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(v, 'f', 2, 64), "0"), ".")
	}
	return ""
}

func rawDataMapFromBill(bill *models.Bill) map[string]interface{} {
	if bill == nil || bill.RawData == nil {
		return nil
	}
	var rd map[string]interface{}
	if err := json.Unmarshal(bill.RawData, &rd); err != nil {
		return nil
	}
	return rd
}

func rawMoneyField(raw map[string]interface{}, key string) (float64, bool) {
	if raw == nil {
		return 0, false
	}
	switch v := raw[key].(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	case string:
		clean := strings.TrimSpace(v)
		clean = strings.TrimPrefix(clean, "฿")
		clean = strings.ReplaceAll(clean, ",", "")
		if clean == "" {
			return 0, false
		}
		n, err := strconv.ParseFloat(clean, 64)
		return n, err == nil
	default:
		return 0, false
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// shopeeSaleOrderConfig returns only the technical SML 248 saleorder config.
// Business send fields are filled from channel_defaults or the visible dialog.
func (h *BillHandler) shopeeSaleOrderConfig() sml.SaleOrderConfig {
	return sml.SaleOrderConfig{
		BaseURL:    h.cfg.ShopeeSMLURL,
		GUID:       h.cfg.ShopeeSMLGUID,
		Provider:   h.cfg.ShopeeSMLProvider,
		ConfigFile: h.cfg.ShopeeSMLConfigFile,
		Database:   h.cfg.ShopeeSMLDatabase,
		VATType:    -1,
		VATRate:    -1,
	}
}

func (h *BillHandler) shopeePurchaseConfig() sml.PurchaseOrderConfig {
	return sml.PurchaseOrderConfig{
		BaseURL:    h.cfg.ShopeeSMLURL,
		GUID:       h.cfg.ShopeeSMLGUID,
		Provider:   h.cfg.ShopeeSMLProvider,
		ConfigFile: h.cfg.ShopeeSMLConfigFile,
		Database:   h.cfg.ShopeeSMLDatabase,
		VATType:    -1,
		VATRate:    -1,
	}
}

func (h *BillHandler) previewSMLDefaults(def *models.ChannelDefault, billType string) gin.H {
	if billType == "purchase" {
		cfg := h.resolvedPurchaseConfig(def, RetryRequest{})
		result := gin.H{
			"branch_code": cfg.BranchCode,
			"sale_code":   cfg.SaleCode,
			"wh_code":     cfg.WHCode,
			"shelf_code":  cfg.ShelfCode,
			"unit_code":   cfg.UnitCode,
			"vat_type":    cfg.VATType,
			"vat_rate":    cfg.VATRate,
			"doc_time":    cfg.DocTime,
			"doc_format":  cfg.DocFormat,
			"database":    cfg.Database,
			"base_url":    cfg.BaseURL,
		}
		if def != nil && def.InquiryType >= 0 {
			result["inquiry_type"] = def.InquiryType
		}
		if def != nil && def.Remark2 != "" {
			result["remark_2"] = def.Remark2
		}
		if def != nil && def.PartyCode != "" {
			result["party_code"] = def.PartyCode
			result["party_name"] = def.PartyName
		}
		return result
	}
	cfg := h.resolvedSaleOrderConfig(def, RetryRequest{})
	result := gin.H{
		"branch_code": cfg.BranchCode,
		"sale_code":   cfg.SaleCode,
		"wh_code":     cfg.WHCode,
		"shelf_code":  cfg.ShelfCode,
		"unit_code":   cfg.UnitCode,
		"vat_type":    cfg.VATType,
		"vat_rate":    cfg.VATRate,
		"doc_time":    cfg.DocTime,
		"doc_format":  cfg.DocFormat,
		"database":    cfg.Database,
		"base_url":    cfg.BaseURL,
	}
	if def != nil && def.InquiryType >= 0 {
		result["inquiry_type"] = def.InquiryType
	}
	if def != nil && def.PartyCode != "" {
		result["party_code"] = def.PartyCode
		result["party_name"] = def.PartyName
	}
	return result
}

// applyDocumentOverrides overlays per-channel document defaults. Empty means
// "unset"; send validation will ask the admin/user to choose a visible value.
func applyDocumentOverrides(def *models.ChannelDefault, branch, sale, unit, docTime *string) {
	if def == nil {
		return
	}
	if def.BranchCode != "" {
		*branch = def.BranchCode
	}
	if def.SaleCode != "" {
		*sale = def.SaleCode
	}
	if def.UnitCode != "" {
		*unit = def.UnitCode
	}
	if def.DocTime != "" {
		*docTime = def.DocTime
	}
}

// applyChannelOverrides overlays the per-channel WH/Shelf/VAT settings. Sentinel
// (empty / -1) means unset; there is intentionally no hidden env fallback.
func applyChannelOverrides(def *models.ChannelDefault, wh, shelf *string, vatType *int, vatRate *float64) {
	if def == nil {
		return
	}
	if def.WHCode != "" {
		*wh = def.WHCode
	}
	if def.ShelfCode != "" {
		*shelf = def.ShelfCode
	}
	if def.VATType >= 0 {
		*vatType = def.VATType
	}
	if def.VATRate >= 0 {
		*vatRate = def.VATRate
	}
}

// applyRetryOverrides overlays one-off choices from the Bill Detail send
// dialog. These are per-bill decisions, so they win over channel defaults and
// env defaults without mutating the saved channel config.
func applyRetryOverrides(req RetryRequest, wh, shelf *string, vatType *int, vatRate *float64) {
	if req.WHCode != "" {
		*wh = req.WHCode
	}
	if req.ShelfCode != "" {
		*shelf = req.ShelfCode
	}
	if req.VATType != nil {
		*vatType = *req.VATType
	}
	if req.VATRate != nil {
		*vatRate = *req.VATRate
	}
}

// applyRetryDocumentOverrides overlays one-off document values from the Bill
// Detail send dialog. These per-bill decisions win over channel config without
// mutating the saved channel config.
func applyRetryDocumentOverrides(req RetryRequest, branch, sale, unit, docTime *string) {
	if req.BranchCode != "" {
		*branch = req.BranchCode
	}
	if req.SaleCode != "" {
		*sale = req.SaleCode
	}
	if req.UnitCode != "" {
		*unit = req.UnitCode
	}
	if req.DocTime != "" {
		*docTime = req.DocTime
	}
}

func (h *BillHandler) validateSendDialogOverrides(req RetryRequest) error {
	switch {
	case strings.TrimSpace(req.WHCode) == "":
		return fmt.Errorf("กรุณากรอกรหัสคลังก่อนส่ง SML")
	case strings.TrimSpace(req.ShelfCode) == "":
		return fmt.Errorf("กรุณากรอกรหัสพื้นที่เก็บก่อนส่ง SML")
	case req.VATType == nil:
		return fmt.Errorf("กรุณาเลือกประเภทภาษีก่อนส่ง SML")
	case req.VATRate == nil:
		return fmt.Errorf("กรุณากรอกอัตราภาษีก่อนส่ง SML")
	case strings.TrimSpace(req.DocTime) == "":
		return fmt.Errorf("กรุณากรอกเวลาเอกสารก่อนส่ง SML")
	}
	if h.warehouseCache != nil {
		whCode := strings.TrimSpace(req.WHCode)
		shelfCode := strings.TrimSpace(req.ShelfCode)
		if whCount, _ := h.warehouseCache.Counts(); whCount > 0 {
			if h.warehouseCache.GetByCode(whCode) == nil {
				return fmt.Errorf("ไม่พบรหัสคลัง %s ใน SML", whCode)
			}
			if !h.warehouseCache.HasShelf(whCode, shelfCode) {
				return fmt.Errorf("ไม่พบพื้นที่เก็บ %s ภายใต้คลัง %s ใน SML", shelfCode, whCode)
			}
		}
	}
	return nil
}

// lookupChannelDefault fetches the (channel, bill_type) party config or
// returns an error suitable for a 400 response when nothing's set.
func (h *BillHandler) lookupChannelDefault(channel, billType string) (*models.ChannelDefault, error) {
	if h.channelDefaults == nil {
		return nil, fmt.Errorf("channel defaults not configured")
	}
	def, err := h.channelDefaults.Get(channel, billType)
	if err != nil {
		return nil, fmt.Errorf("lookup channel default: %w", err)
	}
	if def == nil {
		return nil, fmt.Errorf("ยังไม่ได้ตั้งค่าลูกค้า default สำหรับ %s/%s — ไปที่ /settings/channels", channel, billType)
	}
	return def, nil
}

// failureDetail is the JSON shape persisted to bills.error_msg when an SML
// retry fails. Storing structured data instead of a plain string lets the
// BillDetail UI render route + attempted doc_no + monospace error
// separately, and lets admin copy the raw error text to share with dev
// without other UI clutter.
//
// For backwards compat: the frontend tries JSON.parse first; if it fails
// (i.e. an old plain-text error_msg from before this change) it falls back
// to displaying the string verbatim.
type failureDetail struct {
	Route          string `json:"route"`            // SaleReserve / SaleOrder / SaleInvoice / PurchaseOrder
	DocNoAttempted string `json:"doc_no_attempted"` // empty for SaleReserve (SML generates)
	Error          string `json:"error"`
	OccurredAt     string `json:"occurred_at"` // RFC3339
}

func (h *BillHandler) recordFailure(c *gin.Context, id, source string, reqJSON []byte, err error, start time.Time, route, docNoAttempted string) {
	errMsg := h.recordFailureForSend(id, source, reqJSON, err, start, route, docNoAttempted, retrySendOptions{
		UserID:  c.GetString("user_id"),
		TraceID: c.GetString("trace_id"),
		Via:     "retry",
	})
	c.JSON(http.StatusBadGateway, gin.H{"error": "SML send failed: " + errMsg})
}

func (h *BillHandler) recordFailureForSend(id, source string, reqJSON []byte, err error, start time.Time, route, docNoAttempted string, opts retrySendOptions) string {
	rawErr := err.Error()
	fail := failureDetail{
		Route:          route,
		DocNoAttempted: docNoAttempted,
		Error:          rawErr,
		OccurredAt:     time.Now().UTC().Format(time.RFC3339),
	}
	errMsgJSON, _ := json.Marshal(fail)
	errMsg := string(errMsgJSON)
	respJSON, _ := json.Marshal(map[string]string{"error": rawErr})
	var docNo *string
	if docNoAttempted != "" {
		docNo = &docNoAttempted
	}
	_ = h.billRepo.UpdateStatus(id, "failed", docNo, respJSON, &errMsg)
	if len(reqJSON) > 0 {
		_ = h.billRepo.UpdateSMLPayload(id, reqJSON)
	}
	h.log.Error("SML send failed", zap.String("bill", id), zap.String("route", route), zap.String("via", opts.Via), zap.Error(err))
	if h.auditRepo != nil {
		billID := id
		durMs := int(time.Since(start).Milliseconds())
		var userID *string
		if uid := opts.UserID; uid != "" {
			userID = &uid
		}
		detail := map[string]interface{}{
			"doc_no":     docNoAttempted,
			"error":      errMsg,
			"error_code": inferSMLFailureCode(rawErr),
			"message":    rawErr,
			"route":      route,
			"via":        opts.Via,
		}
		if opts.BulkJobID != "" {
			detail["bulk_job_id"] = opts.BulkJobID
		}
		if opts.BulkJobItemID != "" {
			detail["bulk_job_item_id"] = opts.BulkJobItemID
		}
		if opts.BulkItemSequence > 0 {
			detail["bulk_item_sequence"] = opts.BulkItemSequence
		}
		_ = h.auditRepo.Log(models.AuditEntry{
			Action:     "sml_failed",
			TargetID:   &billID,
			UserID:     userID,
			Source:     source,
			Level:      "error",
			TraceID:    opts.TraceID,
			DurationMs: &durMs,
			Detail:     detail,
		})
	}
	if h.lineSvc != nil && !opts.SuppressLineAlert {
		_ = h.lineSvc.PushAdmin(fmt.Sprintf("⚠️ Bill retry SML failed (%s)\nBill: %s\nError: %s", route, id, errMsg))
	}
	return errMsg
}

func (h *BillHandler) recordSuccess(c *gin.Context, id, source string, respJSON []byte, docNo, route string, start time.Time) {
	h.recordSuccessForSend(id, source, respJSON, docNo, route, start, retrySendOptions{
		UserID:  c.GetString("user_id"),
		TraceID: c.GetString("trace_id"),
		Via:     "retry",
	})
}

func (h *BillHandler) recordSuccessForSend(id, source string, respJSON []byte, docNo, route string, start time.Time, opts retrySendOptions) {
	if h.auditRepo == nil {
		return
	}
	billID := id
	durMs := int(time.Since(start).Milliseconds())
	var userID *string
	if uid := opts.UserID; uid != "" {
		userID = &uid
	}
	detail := map[string]interface{}{
		"doc_no":        docNo,
		"route":         route,
		"response_size": len(respJSON),
		"via":           opts.Via,
	}
	if opts.BulkJobID != "" {
		detail["bulk_job_id"] = opts.BulkJobID
	}
	if opts.BulkJobItemID != "" {
		detail["bulk_job_item_id"] = opts.BulkJobItemID
	}
	if opts.BulkItemSequence > 0 {
		detail["bulk_item_sequence"] = opts.BulkItemSequence
	}
	_ = h.auditRepo.Log(models.AuditEntry{
		Action:     "sml_sent",
		TargetID:   &billID,
		UserID:     userID,
		Source:     source,
		Level:      "info",
		TraceID:    opts.TraceID,
		DurationMs: &durMs,
		Detail:     detail,
	})
	if warning := extractSMLERPLogWarning(respJSON); warning != "" {
		warnDetail := map[string]interface{}{
			"doc_no":         docNo,
			"route":          route,
			"message":        warning,
			"via":            opts.Via,
			"source_channel": source,
		}
		if opts.BulkJobID != "" {
			warnDetail["bulk_job_id"] = opts.BulkJobID
		}
		if opts.BulkJobItemID != "" {
			warnDetail["bulk_job_item_id"] = opts.BulkJobItemID
		}
		_ = h.auditRepo.Log(models.AuditEntry{
			Action:     "sml_erp_log_warning",
			TargetID:   &billID,
			UserID:     userID,
			Source:     "sml",
			Level:      "warn",
			TraceID:    opts.TraceID,
			DurationMs: &durMs,
			Detail:     warnDetail,
		})
	}
	h.log.Info("SML bill sent", zap.String("bill", id), zap.String("doc", docNo), zap.String("via", opts.Via))
}

func extractSMLERPLogWarning(respJSON []byte) string {
	if len(respJSON) == 0 {
		return ""
	}
	var raw struct {
		LogWarning string `json:"log_warning"`
		Data       struct {
			LogWarning string `json:"log_warning"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respJSON, &raw); err != nil {
		return ""
	}
	if s := strings.TrimSpace(raw.Data.LogWarning); s != "" {
		return s
	}
	return strings.TrimSpace(raw.LogWarning)
}

func inferSMLFailureCode(msg string) string {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "duplicate") || strings.Contains(lower, "already exists"):
		return "duplicate_doc_no"
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline"):
		return "timeout"
	case strings.Contains(lower, "connection refused") || strings.Contains(lower, "eof"):
		return "connection_failed"
	case strings.Contains(lower, "warehouse") || strings.Contains(lower, "wh_code") || strings.Contains(lower, "shelf"):
		return "warehouse_invalid"
	case strings.Contains(lower, "customer") || strings.Contains(lower, "supplier") || strings.Contains(lower, "party"):
		return "party_invalid"
	case strings.Contains(lower, "item") || strings.Contains(lower, "unit"):
		return "item_invalid"
	case strings.Contains(lower, "vat") || strings.Contains(lower, "tax"):
		return "vat_invalid"
	default:
		return "sml_failed"
	}
}

// ─── Item edit ───────────────────────────────────────────────────────────────

// POST /api/bills/:id/items — add a new line item to a not-yet-sent bill.
type addItemRequest struct {
	RawName  string   `json:"raw_name" binding:"required"`
	ItemCode *string  `json:"item_code"`
	UnitCode *string  `json:"unit_code"`
	Qty      float64  `json:"qty" binding:"required"`
	Price    *float64 `json:"price"`
}

func (h *BillHandler) AddItem(c *gin.Context) {
	billID := c.Param("id")

	bill, err := h.billRepo.FindByID(billID)
	if err != nil || bill == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "bill not found"})
		return
	}
	if bill.Status == "sent" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot add items to a bill already sent to SML"})
		return
	}

	var req addItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.ItemCode != nil && strings.TrimSpace(*req.ItemCode) != "" {
		*req.ItemCode = strings.TrimSpace(*req.ItemCode)
		if _, ok := h.validateWritableItemCode(c, bill, "", *req.ItemCode, "bill_item_add"); !ok {
			return
		}
	}

	mapped := req.ItemCode != nil && *req.ItemCode != ""
	item := &models.BillItem{
		BillID:   billID,
		RawName:  req.RawName,
		ItemCode: req.ItemCode,
		UnitCode: req.UnitCode,
		Qty:      req.Qty,
		Price:    req.Price,
		Mapped:   mapped,
	}
	if err := h.billRepo.InsertItem(item); err != nil {
		h.log.Error("AddItem", zap.String("bill", billID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "insert failed"})
		return
	}

	if h.auditRepo != nil {
		var userID *string
		if uid := c.GetString("user_id"); uid != "" {
			userID = &uid
		}
		_ = h.auditRepo.Log(models.AuditEntry{
			Action:   "bill_item_added",
			TargetID: &billID,
			UserID:   userID,
			Source:   bill.Source,
			Level:    "info",
			Detail: map[string]interface{}{
				"item_id":   item.ID,
				"raw_name":  req.RawName,
				"item_code": req.ItemCode,
				"qty":       req.Qty,
			},
		})
	}

	c.JSON(http.StatusCreated, item)
}

// DELETE /api/bills/:id/items/:item_id — remove a line item from a not-yet-sent bill.
func (h *BillHandler) DeleteItemRow(c *gin.Context) {
	billID := c.Param("id")
	itemID := c.Param("item_id")

	bill, err := h.billRepo.FindByID(billID)
	if err != nil || bill == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "bill not found"})
		return
	}
	if bill.Status == "sent" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot delete items from a bill already sent to SML"})
		return
	}

	if err := h.billRepo.DeleteItem(billID, itemID); err != nil {
		h.log.Error("DeleteItem", zap.String("bill", billID), zap.String("item", itemID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete failed"})
		return
	}

	if h.auditRepo != nil {
		var userID *string
		if uid := c.GetString("user_id"); uid != "" {
			userID = &uid
		}
		_ = h.auditRepo.Log(models.AuditEntry{
			Action:   "bill_item_deleted",
			TargetID: &billID,
			UserID:   userID,
			Source:   bill.Source,
			Level:    "info",
			Detail: map[string]interface{}{
				"item_id": itemID,
			},
		})
	}

	c.JSON(http.StatusOK, gin.H{"message": "item deleted"})
}

// PUT /api/bills/:id/items/:item_id — edit item code/unit/qty/price before sending.
type updateItemRequest struct {
	ItemCode *string  `json:"item_code"`
	UnitCode *string  `json:"unit_code"`
	Qty      *float64 `json:"qty"`
	Price    *float64 `json:"price"`
}

func (h *BillHandler) UpdateItem(c *gin.Context) {
	billID := c.Param("id")
	itemID := c.Param("item_id")

	bill, err := h.billRepo.FindByID(billID)
	if err != nil || bill == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "bill not found"})
		return
	}
	if bill.Status == "sent" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot edit items on a bill already sent to SML"})
		return
	}

	// Find the item being edited so we know its raw_name for F1 feedback
	var existingItem *models.BillItem
	for i := range bill.Items {
		if bill.Items[i].ID == itemID {
			existingItem = &bill.Items[i]
			break
		}
	}
	if existingItem == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "item not found in bill"})
		return
	}

	var req updateItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.ItemCode != nil {
		*req.ItemCode = strings.TrimSpace(*req.ItemCode)
		if *req.ItemCode != "" {
			if _, ok := h.validateWritableItemCode(c, bill, itemID, *req.ItemCode, "bill_item_update"); !ok {
				return
			}
		}
	}

	// If user is changing item_code, fill unit_code from catalog if not provided.
	// This makes the F1 feedback richer and the SML payload more correct.
	if req.ItemCode != nil && *req.ItemCode != "" && (req.UnitCode == nil || *req.UnitCode == "") && h.catalogRepo != nil {
		if cat, _ := h.catalogRepo.GetOne(*req.ItemCode); cat != nil && cat.UnitCode != "" {
			u := cat.UnitCode
			req.UnitCode = &u
		}
	}

	if err := h.billRepo.UpdateBillItemFields(itemID, req.ItemCode, req.UnitCode, req.Qty, req.Price); err != nil {
		h.log.Error("UpdateItem", zap.String("bill", billID), zap.String("item", itemID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "update failed"})
		return
	}

	// F1 learning loop: if the user supplies a non-empty item_code, treat the
	// save as human confirmation when the code changed OR when the row was still
	// an unconfirmed low-confidence match. This covers marketplace imports where
	// AI prefilled the same code but still left the bill in needs_review.
	if req.ItemCode != nil && *req.ItemCode != "" && existingItem.RawName != "" {
		prev := ""
		if existingItem.ItemCode != nil {
			prev = *existingItem.ItemCode
		}
		wasUnconfirmed := !existingItem.Mapped || existingItem.MappingID == nil || *existingItem.MappingID == ""
		if prev != *req.ItemCode || wasUnconfirmed {
			unit := ""
			if req.UnitCode != nil {
				unit = *req.UnitCode
			}
			if err := h.mapperSvc.LearnFromFeedback(existingItem.RawName, *req.ItemCode, unit, &billID); err != nil {
				h.log.Warn("UpdateItem: F1 feedback save failed",
					zap.String("raw_name", existingItem.RawName),
					zap.String("item_code", *req.ItemCode),
					zap.Error(err))
			} else {
				appliedItems, readyBills, applyErr := h.billRepo.ApplyVerifiedMappingToOpenItems(
					bill.Source,
					bill.BillType,
					existingItem.RawName,
					*req.ItemCode,
					unit,
				)
				if applyErr != nil {
					h.log.Warn("UpdateItem: apply learned mapping to open bills failed",
						zap.String("source", bill.Source),
						zap.String("bill_type", bill.BillType),
						zap.String("raw_name", existingItem.RawName),
						zap.String("item_code", *req.ItemCode),
						zap.Error(applyErr))
				}
				if h.auditRepo != nil {
					var userID *string
					if uid := c.GetString("user_id"); uid != "" {
						userID = &uid
					}
					_ = h.auditRepo.Log(models.AuditEntry{
						Action:   "mapping_feedback",
						TargetID: &itemID,
						UserID:   userID,
						Source:   bill.Source,
						Level:    "info",
						Detail: map[string]interface{}{
							"raw_name":           existingItem.RawName,
							"prev_code":          prev,
							"new_code":           *req.ItemCode,
							"unit_code":          unit,
							"bill_id":            billID,
							"confirmed_existing": prev == *req.ItemCode,
							"applied_items":      appliedItems,
							"ready_bills":        readyBills,
						},
					})
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "item updated"})
}

// ─── Source artifact endpoints ────────────────────────────────────────────────

// GET /api/bills/:id/artifacts
func (h *BillHandler) ListArtifacts(c *gin.Context) {
	if h.artifactSvc == nil {
		c.JSON(http.StatusOK, gin.H{"data": []models.BillArtifact{}})
		return
	}
	billID := c.Param("id")
	items, err := h.artifactSvc.ListByBill(billID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if items == nil {
		items = []models.BillArtifact{}
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}

// POST /api/bills/:id/artifacts/:artifact_id/print-events
func (h *BillHandler) RecordArtifactPrint(c *gin.Context) {
	if h.billRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "bill repository not configured"})
		return
	}
	billID := c.Param("id")
	artID := c.Param("artifact_id")
	event, err := h.billRepo.RecordEmailPrintEvent(
		billID,
		artID,
		c.GetString("user_id"),
		c.GetString("user_email"),
	)
	if err != nil {
		if strings.Contains(err.Error(), "not a printable email") || strings.Contains(err.Error(), "no email message id") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		h.log.Error("Record artifact print", zap.String("bill", billID), zap.String("artifact", artID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "record print event failed"})
		return
	}
	if event == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "artifact not found for this bill"})
		return
	}

	if h.auditRepo != nil {
		var userID *string
		if uid := c.GetString("user_id"); uid != "" {
			userID = &uid
		}
		_ = h.auditRepo.Log(models.AuditEntry{
			Action:   "email_print_requested",
			TargetID: &billID,
			UserID:   userID,
			Source:   "system",
			Level:    "info",
			TraceID:  c.GetString("trace_id"),
			Detail: map[string]interface{}{
				"artifact_id":     artID,
				"email_group_key": event.EmailGroupKey,
			},
		})
	}
	c.JSON(http.StatusCreated, gin.H{"data": event})
}

func (h *BillHandler) serveArtifact(c *gin.Context, inline bool) {
	if h.artifactSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "artifact service not configured"})
		return
	}
	billID := c.Param("id")
	artID := c.Param("artifact_id")

	data, art, err := h.artifactSvc.Read(artID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if art == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "artifact not found"})
		return
	}
	// Scope check: artifact must belong to the requested bill
	if art.BillID != billID {
		c.JSON(http.StatusNotFound, gin.H{"error": "artifact not found for this bill"})
		return
	}

	contentType := art.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	// All our text artifacts (email_html, JSON envelope, etc.) are stored
	// as UTF-8 bytes. Browsers default text/html / text/plain to Latin-1
	// when the Content-Type header has no charset, which mangles Thai
	// (e.g. "เรียน" → "à¹€à¸£à¸µà¸¢à¸™"). Backfill charset=utf-8 so
	// historical artifacts saved before the canonical fix still render.
	if (strings.HasPrefix(contentType, "text/") || contentType == "application/json") &&
		!strings.Contains(strings.ToLower(contentType), "charset=") {
		contentType = contentType + "; charset=utf-8"
	}
	disposition := "attachment"
	if inline {
		disposition = "inline"
	}
	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, art.Filename))
	c.Header("X-Content-SHA256", art.SHA256)
	c.Data(http.StatusOK, contentType, data)
}

// GET /api/bills/:id/artifacts/:artifact_id/download
func (h *BillHandler) DownloadArtifact(c *gin.Context) { h.serveArtifact(c, false) }

// GET /api/bills/:id/artifacts/:artifact_id/preview
func (h *BillHandler) PreviewArtifact(c *gin.Context) { h.serveArtifact(c, true) }
