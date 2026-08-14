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
	"sort"
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

const (
	lazadaMaxFileBytes = 20 * 1024 * 1024
	lazadaMaxRows      = 50_000
	lazadaMaxOrders    = 5_000
	lazadaPreviewTTL   = 15 * time.Minute
)

type lazadaPendingPreview struct {
	bytes       []byte
	artifactID  string
	filename    string
	fileHash    string
	importRunID string
	userID      string
	uploadedAt  time.Time
}

type lazadaConfirmRequest struct {
	PreviewToken string   `json:"preview_token"`
	ImportRunID  string   `json:"import_run_id"`
	OrderIDs     []string `json:"order_ids"`
}

// LazadaImportHandler mirrors the Shopee Excel import flow: preview first,
// then create local sale bills for manual review and SML retry.
type LazadaImportHandler struct {
	billRepo        *repository.BillRepo
	mappingRepo     *repository.MappingRepo
	aliasRepo       *repository.MarketplaceAliasRepo
	auditRepo       *repository.AuditLogRepo
	cfg             *config.Config
	channelDefaults *repository.ChannelDefaultRepo
	catalogRepo     *repository.SMLCatalogRepo
	catalogSvc      *catalog.SMLCatalogService
	artifactSvc     *artifact.Service
	logger          *zap.Logger

	pendingUploads sync.Map
}

func NewLazadaImportHandler(
	billRepo *repository.BillRepo,
	mappingRepo *repository.MappingRepo,
	auditRepo *repository.AuditLogRepo,
	cfg *config.Config,
	channelDefaults *repository.ChannelDefaultRepo,
	catalogRepo *repository.SMLCatalogRepo,
	catalogSvc *catalog.SMLCatalogService,
	aliasRepo *repository.MarketplaceAliasRepo,
	logger *zap.Logger,
) *LazadaImportHandler {
	h := &LazadaImportHandler{
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

func (h *LazadaImportHandler) SetArtifactService(svc *artifact.Service) {
	h.artifactSvc = svc
}

func (h *LazadaImportHandler) gcPendingUploads() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		h.pendingUploads.Range(func(key, val any) bool {
			if pu, ok := val.(*lazadaPendingPreview); ok && now.Sub(pu.uploadedAt) > lazadaPreviewTTL {
				h.pendingUploads.Delete(key)
			}
			return true
		})
	}
}

func (h *LazadaImportHandler) GetConfig(c *gin.Context) {
	config := h.currentSaleConfig()
	// Import confirmation reloads channel_defaults server-side. The browser
	// receives only fields required to describe the SML destination.
	c.JSON(http.StatusOK, gin.H{
		"endpoint":        config.Endpoint,
		"doc_format_code": config.DocFormat,
	})
}

func (h *LazadaImportHandler) currentSaleConfig() ShopeeConfigRequest {
	custCode := ""
	whCode := h.cfg.ShopeeSMLWHCode
	shelfCode := h.cfg.ShopeeSMLShelfCode
	vatType := h.cfg.ShopeeSMLVATType
	vatRate := h.cfg.ShopeeSMLVATRate
	docFormat := h.cfg.ShopeeSMLDocFormat
	endpoint := "/api/v1/ic/sale-orders"
	if h.channelDefaults != nil {
		if def, _ := h.channelDefaults.Get("lazada", "sale"); def != nil {
			custCode = def.PartyCode
			if def.Endpoint != "" {
				endpoint = def.Endpoint
			}
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

func (h *LazadaImportHandler) ListRuns(c *gin.Context) {
	limit := 8
	if raw := c.Query("limit"); raw != "" {
		if n, err := strconvAtoi(raw); err == nil && n > 0 && n <= 50 {
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
		  WHERE source = 'lazada'
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
			&run.ID, &run.Filename, &run.FileSHA256, &run.PeriodStart, &run.PeriodEnd,
			&run.TotalOrders, &run.NewOrders, &run.DuplicateOrders, &run.SkippedOrders,
			&run.WarningCount, &run.CreatedCount, &run.FailedCount, &run.Status, &run.Detail,
			&run.CreatedAt, &run.ConfirmedAt,
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

func (h *LazadaImportHandler) Preview(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณาแนบไฟล์ Excel (.xlsx)"})
		return
	}
	if !strings.HasSuffix(strings.ToLower(fileHeader.Filename), ".xlsx") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "รองรับเฉพาะไฟล์ .xlsx เท่านั้น"})
		return
	}
	if fileHeader.Size > lazadaMaxFileBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "ไฟล์เกิน 20 MB กรุณาแบ่งไฟล์แล้วนำเข้าใหม่"})
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "เปิดไฟล์ไม่ได้"})
		return
	}
	defer file.Close()
	rawBytes, err := io.ReadAll(io.LimitReader(file, lazadaMaxFileBytes+1))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "อ่านไฟล์ไม่ได้"})
		return
	}
	if len(rawBytes) > lazadaMaxFileBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "ไฟล์เกิน 20 MB กรุณาแบ่งไฟล์แล้วนำเข้าใหม่"})
		return
	}

	orders, warnings, skippedCount, err := parseLazadaExcel(bytes.NewReader(rawBytes))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(orders) > lazadaMaxOrders {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "ไฟล์มีเกิน 5,000 orders กรุณาแบ่งไฟล์แล้วนำเข้าใหม่"})
		return
	}
	var channelDefault *models.ChannelDefault
	if h.channelDefaults != nil {
		channelDefault, _ = h.channelDefaults.Get("lazada", "sale")
	}
	applyLazadaShippingPreflight(orders, channelDefault)
	sum := sha256.Sum256(rawBytes)
	fileHash := hex.EncodeToString(sum[:])
	orderIDs := make([]string, 0, len(orders))
	for i := range orders {
		orderIDs = append(orderIDs, orders[i].OrderID)
	}
	existingBills, err := h.findLazadaOrderBills(orderIDs)
	if err != nil {
		h.logger.Error("lazada_import: duplicate preflight failed", zap.Error(err))
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "ตรวจสอบ Order ซ้ำไม่สำเร็จ กรุณาลองใหม่"})
		return
	}
	dupCount := 0
	for i := range orders {
		if billID, exists := existingBills[orders[i].OrderID]; exists {
			orders[i].Duplicate = true
			orders[i].ExistingBillID = billID
			dupCount++
		}
	}
	preflight := buildShopeePreflight(orders, skippedCount, dupCount)
	importRunID := h.createLazadaImportRun(c, fileHeader.Filename, fileHash, orders, warnings, preflight)
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
			map[string]interface{}{"source": "lazada", "file_sha256": fileHash})
		if saveErr != nil {
			h.finishLazadaImportRun(importRunID, 0, 1, "failed")
			h.logger.Error("lazada_import: persist source artifact", zap.String("import_run_id", importRunID), zap.Error(saveErr))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "เก็บไฟล์ต้นฉบับไม่สำเร็จ กรุณาลองใหม่"})
			return
		}
		artifactID = saved.ID
	}
	pendingBytes := rawBytes
	if artifactID != "" {
		pendingBytes = nil
	}
	h.pendingUploads.Store(previewToken, &lazadaPendingPreview{
		bytes: pendingBytes, artifactID: artifactID, filename: fileHeader.Filename, fileHash: fileHash,
		importRunID: importRunID, userID: c.GetString("user_id"), uploadedAt: time.Now(),
	})

	if h.auditRepo != nil {
		var userID *string
		if uid := c.GetString("user_id"); uid != "" {
			userID = &uid
		}
		_ = h.auditRepo.Log(models.AuditEntry{
			Action:  "lazada_import_preview",
			UserID:  userID,
			Source:  "lazada",
			Level:   "info",
			TraceID: c.GetString("trace_id"),
			Detail: map[string]interface{}{
				"filename":        fileHeader.Filename,
				"total_orders":    len(orders),
				"duplicate_count": dupCount,
				"skipped_count":   skippedCount,
				"import_run_id":   importRunID,
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
		PreviewToken:   previewToken,
	})
}

func (h *LazadaImportHandler) Confirm(c *gin.Context) {
	if blockIfCatalogNotReady(c, h.catalogRepo) {
		return
	}
	var req lazadaConfirmRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request ไม่ถูกต้อง", "code": "invalid_request"})
		return
	}
	if strings.TrimSpace(req.PreviewToken) == "" || strings.TrimSpace(req.ImportRunID) == "" {
		writeMarketplacePreviewExpired(c)
		return
	}
	pending, ok := h.consumeLazadaPreview(req.PreviewToken, req.ImportRunID, c.GetString("user_id"), time.Now())
	if !ok {
		writeMarketplacePreviewExpired(c)
		return
	}
	sourceBytes := pending.bytes
	if pending.artifactID != "" {
		if h.artifactSvc == nil {
			writeMarketplacePreviewExpired(c)
			return
		}
		var readErr error
		sourceBytes, _, readErr = h.artifactSvc.ReadForImportRun(pending.importRunID, pending.artifactID)
		if readErr != nil {
			h.logger.Warn("lazada_import: read source artifact", zap.String("import_run_id", pending.importRunID), zap.Error(readErr))
			writeMarketplacePreviewExpired(c)
			return
		}
	}
	sum := sha256.Sum256(sourceBytes)
	if hex.EncodeToString(sum[:]) != pending.fileHash {
		c.JSON(http.StatusConflict, gin.H{"error": "ไฟล์ preview ถูกเปลี่ยน กรุณาอัปโหลดใหม่", "code": "preview_tampered"})
		return
	}
	orders, _, _, err := parseLazadaExcel(bytes.NewReader(sourceBytes))
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "อ่านไฟล์ต้นฉบับซ้ำไม่สำเร็จ กรุณาอัปโหลดใหม่", "code": "preview_expired"})
		return
	}
	selectedSet := make(map[string]bool, len(req.OrderIDs))
	for _, rawID := range req.OrderIDs {
		id := strings.TrimSpace(rawID)
		if id != "" {
			selectedSet[id] = true
		}
	}
	if len(selectedSet) == 0 || len(selectedSet) > lazadaMaxOrders {
		c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณาเลือก order 1–5,000 รายการ", "code": "invalid_order_selection"})
		return
	}
	orderByID := make(map[string]ShopeeOrder, len(orders))
	for _, order := range orders {
		orderByID[order.OrderID] = order
	}
	selectedOrders := make([]ShopeeOrder, 0, len(selectedSet))
	for id := range selectedSet {
		order, exists := orderByID[id]
		if !exists {
			c.JSON(http.StatusConflict, gin.H{"error": "รายการที่เลือกไม่อยู่ในไฟล์ต้นฉบับ กรุณาอัปโหลดใหม่", "code": "preview_tampered"})
			return
		}
		if order.BlockedReason != "" {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": order.BlockedReason, "code": "order_blocked", "order_id": id})
			return
		}
		selectedOrders = append(selectedOrders, order)
	}
	sort.Slice(selectedOrders, func(i, j int) bool { return selectedOrders[i].OrderID < selectedOrders[j].OrderID })
	selectedIDs := make([]string, 0, len(selectedOrders))
	for i := range selectedOrders {
		selectedIDs = append(selectedIDs, selectedOrders[i].OrderID)
	}
	existingBills, err := h.findLazadaOrderBills(selectedIDs)
	if err != nil {
		h.logger.Error("lazada_import: confirm duplicate check failed", zap.Error(err))
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "ตรวจสอบ Order ซ้ำไม่สำเร็จ กรุณาอัปโหลดใหม่", "code": "duplicate_check_failed"})
		return
	}
	config := h.currentSaleConfig()
	var def *models.ChannelDefault
	if h.channelDefaults != nil {
		def, _ = h.channelDefaults.Get("lazada", "sale")
	}
	if err := validateLazadaShippingConfig(selectedOrders, def); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error(), "code": "shipping_item_not_configured"})
		return
	}
	documentRoute := shopeeImportRoute(config)
	destinationName := shopeeImportDocumentName(config)
	reviewPath := shopeeImportReviewPath(config)
	defaultUnit := config.UnitCode

	var userID *string
	if uid := c.GetString("user_id"); uid != "" {
		userID = &uid
	}
	traceID := c.GetString("trace_id")
	confirmStart := time.Now()

	resolutionBatch, err := prepareMarketplaceResolution(
		c.Request.Context(), "lazada", selectedOrders, selectedSet,
		h.catalogRepo, h.catalogSvc, h.aliasRepo, h.mappingRepo, h.logger,
	)
	if err != nil {
		h.logger.Warn("lazada_import: prepare product resolution failed", zap.Error(err))
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "catalog_unavailable", "message": "เตรียมข้อมูลสินค้า SML ไม่สำเร็จ กรุณาลองใหม่"})
		return
	}
	defer flushMarketplaceResolutionUsage(resolutionBatch, h.aliasRepo, h.logger)

	results := []ConfirmResult{}
	for _, order := range selectedOrders {
		if billID, exists := existingBills[order.OrderID]; exists {
			results = append(results, ConfirmResult{
				OrderID: order.OrderID,
				Success: false,
				BillID:  billID,
				Message: "order นี้มีอยู่ในระบบแล้ว (ข้าม)",
			})
			continue
		}

		items := make([]models.BillItem, 0, len(order.Items)+1)
		allMapped := true
		orderItemIDs := []string{}

		for _, it := range order.Items {
			rawName := shopeeItemRawName(it.ProductName, it.OptionName, it.RawName)
			if it.OrderItemID != "" {
				orderItemIDs = append(orderItemIDs, it.OrderItemID)
			}
			resolved := resolutionBatch.resolutionScoped("default", it.SourceItemID, it.SourceVariantID, it.SKU, rawName)
			confirmedBy := ""
			if userID != nil {
				confirmedBy = *userID
			}
			bi, mapped, resolveErr := marketplaceBillItemFromResolution("lazada", "default", it, defaultUnit, resolved, resolutionBatch, h.aliasRepo, confirmedBy)
			if resolveErr != nil {
				h.logger.Warn("lazada_import: save product master failed", zap.String("order_id", order.OrderID), zap.Error(resolveErr))
				mapped = false
			}
			exactSKU := normalizeMarketplaceSKU(it.SKU) != "" && resolutionBatch.catalogLookup(it.SKU) != nil
			if mapped {
				recordMarketplaceResolutionUsage(resolutionBatch, resolved, exactSKU)
			}
			if !mapped {
				allMapped = false
			}
			items = append(items, bi)
		}
		if order.ShippingAmount > 0 && def != nil {
			shippingGross := order.ShippingAmount
			shippingPrice := order.ShippingAmount
			shippingCode := strings.TrimSpace(def.ShippingItemCode)
			shippingUnit := strings.TrimSpace(def.ShippingItemUnitCode)
			items = append(items, models.BillItem{
				RawName: "ค่าจัดส่ง Lazada", SourceSKU: models.LazadaShippingSourceSKU,
				ItemCode: &shippingCode, UnitCode: &shippingUnit, Qty: 1, Price: &shippingPrice,
				GrossAmount: &shippingGross, Mapped: true,
			})
		}

		status := "pending"
		if !allMapped || order.AmountMismatch {
			status = "needs_review"
		}
		fingerprint := tikTokAmountFingerprint(items)
		rawData, _ := json.Marshal(map[string]interface{}{
			"flow":                      "lazada_excel",
			"lazada_order_id":           order.OrderID,
			"order_id":                  order.OrderID,
			"doc_date":                  order.DocDate,
			"order_datetime":            order.OrderDateTime,
			"payment_channel":           order.PaymentChannel,
			"customer_name":             order.BuyerUsername,
			"tracking_no":               order.TrackingNo,
			"status":                    order.Status,
			"item_count":                order.ItemCount,
			"total_qty":                 order.TotalQty,
			"paid_total_amount":         order.PaidAmount,
			"order_total_amount":        order.OrderTotalAmount,
			"item_gross_amount":         order.ItemGrossAmount,
			"platform_discount_amount":  order.PlatformDiscountAmount,
			"seller_discount_amount":    order.SellerDiscountAmount,
			"line_paid_amount":          order.LinePaidAmount,
			"shipping_amount":           order.ShippingAmount,
			"discount_amount":           order.DiscountAmount,
			"net_product_amount":        order.NetProductAmount,
			"payment_discount_amount":   order.PaymentDiscountAmount,
			"amount_difference":         order.AmountDifference,
			"amount_review_required":    order.AmountMismatch,
			"amount_review_reason":      order.AmountReviewReason,
			"amount_source_fingerprint": fingerprint,
			"has_no_sku":                order.HasNoSKU,
			"no_sku_item_count":         order.NoSKUItemCount,
			"multi_line":                order.MultiLine,
			"order_item_ids":            orderItemIDs,
			"import_run_id":             req.ImportRunID,
			"document_route":            documentRoute,
			"sml_destination":           destinationName,
		})
		bill := &models.Bill{
			BillType:         "sale",
			Source:           "lazada",
			SourceAccountKey: "default",
			Status:           status,
			DocumentRoute:    documentRoute,
			AIConfidence:     nil,
			RawData:          rawData,
			SMLOrderID:       order.OrderID,
		}
		if userID != nil {
			bill.CreatedBy = userID
		}
		durMs := int(time.Since(confirmStart).Milliseconds())
		audit := models.AuditEntry{
			Action: "bill_created", UserID: userID, Source: "lazada", Level: "info",
			TraceID: traceID, DurationMs: &durMs,
			Detail: map[string]interface{}{
				"order_id": order.OrderID, "items_count": len(items), "status": status,
				"amount_mismatch": order.AmountMismatch, "flow": "lazada_excel",
			},
		}
		if err := h.billRepo.CreateWithItemsAndAudit(bill, items, audit); err != nil {
			if isDuplicateMarketplaceBillError(err) {
				billID, _, _ := h.findLazadaOrderBillID(order.OrderID)
				results = append(results, ConfirmResult{
					OrderID: order.OrderID,
					Success: false,
					BillID:  billID,
					Message: "order นี้ถูกสร้างไปแล้วระหว่างนำเข้า (ข้าม)",
				})
				continue
			}
			h.logger.Error("lazada_excel: atomic bill creation failed", zap.String("order_id", order.OrderID), zap.Error(err))
			results = append(results, ConfirmResult{OrderID: order.OrderID, Success: false, Message: "บันทึก bill และรายการสินค้าไม่สำเร็จ"})
			continue
		}
		results = append(results, ConfirmResult{
			OrderID: order.OrderID,
			Success: true,
			BillID:  bill.ID,
			Message: fmt.Sprintf("สร้าง%sแล้ว (status=%s) — รอตรวจสอบใน %s", destinationName, status, reviewPath),
		})
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
			Action:     "lazada_import_done",
			UserID:     userID,
			Source:     "lazada",
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
	h.finishLazadaImportRun(req.ImportRunID, successCount, len(results)-successCount, "confirmed")
	c.JSON(http.StatusOK, gin.H{
		"results":       results,
		"success_count": successCount,
		"fail_count":    len(results) - successCount,
		"total":         len(results),
		"message":       destinationName + "ถูกสร้างแล้ว — กรุณาเข้าไปตรวจสอบและกดยืนยันส่งใน " + reviewPath,
	})
}

func (h *LazadaImportHandler) consumeLazadaPreview(token, importRunID, userID string, now time.Time) (*lazadaPendingPreview, bool) {
	loaded, ok := h.pendingUploads.LoadAndDelete(strings.TrimSpace(token))
	if !ok {
		return nil, false
	}
	pending, ok := loaded.(*lazadaPendingPreview)
	if !ok || pending.importRunID != strings.TrimSpace(importRunID) || pending.userID != userID ||
		now.Sub(pending.uploadedAt) > lazadaPreviewTTL || now.Before(pending.uploadedAt) {
		return nil, false
	}
	return pending, true
}

func randomMarketplacePreviewToken() (string, error) { return randomTikTokPreviewToken() }

func writeMarketplacePreviewExpired(c *gin.Context) { writeTikTokPreviewExpired(c) }

func validateLazadaShippingConfig(orders []ShopeeOrder, def *models.ChannelDefault) error {
	requiresShippingItem := false
	for _, order := range orders {
		if order.ShippingAmount > 0 {
			requiresShippingItem = true
			break
		}
	}
	if !requiresShippingItem {
		return nil
	}
	if def == nil || !def.ShippingItemEnabled || strings.TrimSpace(def.ShippingItemCode) == "" || strings.TrimSpace(def.ShippingItemUnitCode) == "" {
		return fmt.Errorf("ไฟล์มีค่าส่งที่ลูกค้าจ่าย กรุณาตั้งสินค้า SML สำหรับค่าจัดส่ง Lazada ที่เมนูเส้นทางเอกสารก่อน")
	}
	return nil
}

func applyLazadaShippingPreflight(orders []ShopeeOrder, def *models.ChannelDefault) {
	if validateLazadaShippingConfig(orders, def) == nil {
		return
	}
	for i := range orders {
		if orders[i].ShippingAmount > 0 && orders[i].BlockedReason == "" {
			orders[i].BlockedReason = "ยังไม่ได้ตั้งสินค้า SML สำหรับค่าจัดส่ง Lazada"
		}
	}
}

func marketplaceMoneyCents(row []string, colIdx map[string]int, key string) (int64, error) {
	raw := strings.ReplaceAll(optionalCell(row, colIdx, key), ",", "")
	if raw == "" {
		return 0, nil
	}
	value, err := parseDecimalCents(raw)
	if err != nil {
		return 0, fmt.Errorf("ยอด %s ไม่ใช่ตัวเลข", key)
	}
	return value, nil
}

var lazadaColCandidates = map[string][]string{
	"order_id":          {"orderNumber"},
	"order_item_id":     {"orderItemId"},
	"lazada_id":         {"lazadaId"},
	"status":            {"status"},
	"seller_sku":        {"sellerSku"},
	"lazada_sku":        {"lazadaSku"},
	"order_date":        {"createTime"},
	"update_time":       {"updateTime"},
	"delivered_date":    {"deliveredDate"},
	"customer_name":     {"customerName", "shippingName"},
	"payment_channel":   {"payMethod"},
	"tracking_no":       {"trackingCode"},
	"product_name":      {"itemName"},
	"option_name":       {"variation"},
	"paid_price":        {"paidPrice"},
	"unit_price":        {"unitPrice"},
	"seller_discount":   {"sellerDiscountTotal"},
	"platform_discount": {"platformDiscountTotal"},
	"shipping_amount":   {"shippingFee"},
	"wallet_credit":     {"walletCredit"},
	"bundle_discount":   {"bundleDiscount"},
}

var lazadaAllowedStatuses = map[string]bool{
	"confirmed": true,
	"shipped":   true,
	"delivered": true,
}

func parseLazadaExcel(src interface{ Read([]byte) (int, error) }) ([]ShopeeOrder, []string, int, error) {
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
	for i, row := range rows {
		for _, cell := range row {
			if strings.EqualFold(strings.TrimSpace(cell), "orderNumber") {
				headerRowIdx = i
				goto foundHeader
			}
		}
	}
foundHeader:
	headerRow := rows[headerRowIdx]
	colIdx := map[string]int{}
	for field, candidates := range lazadaColCandidates {
		for j, cell := range headerRow {
			trimmed := strings.TrimSpace(cell)
			for _, c := range candidates {
				if strings.EqualFold(trimmed, c) {
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
		"order_id", "order_item_id", "status", "order_date", "product_name",
		"paid_price", "unit_price", "shipping_amount", "seller_discount",
		"platform_discount", "wallet_credit", "bundle_discount",
	}
	for _, f := range required {
		if _, ok := colIdx[f]; !ok {
			return nil, nil, 0, fmt.Errorf("ไม่พบ column '%s' ในไฟล์ Lazada — columns ที่พบ: %s",
				f, strings.Join(headerRow[:min(len(headerRow), 15)], ", "))
		}
	}

	if len(rows)-headerRowIdx-1 > lazadaMaxRows {
		return nil, nil, 0, fmt.Errorf("ไฟล์มีเกิน %d แถว กรุณาแบ่งไฟล์แล้วนำเข้าใหม่", lazadaMaxRows)
	}
	warnings := []string{}
	orderMap := map[string]*ShopeeOrder{}
	itemMap := map[string]map[string]int{}
	orderKeys := []string{}
	noSKUOrderIDs := map[string]bool{}
	noSKUItemCount := 0
	skippedCount := 0
	skippedStatuses := map[string]int{}

	for rowOffset, row := range rows[headerRowIdx+1:] {
		if len(row) == 0 {
			continue
		}
		orderID := optionalCell(row, colIdx, "order_id")
		if orderID == "" {
			continue
		}
		status := optionalCell(row, colIdx, "status")
		if !lazadaAllowedStatuses[strings.ToLower(strings.TrimSpace(status))] {
			skippedCount++
			if status == "" {
				status = "(ว่าง)"
			}
			skippedStatuses[status]++
			continue
		}
		orderDateTime := optionalCell(row, colIdx, "order_date")
		docDate := lazadaDocDate(orderDateTime)
		if _, exists := orderMap[orderID]; !exists {
			orderMap[orderID] = &ShopeeOrder{
				OrderID:        orderID,
				DocDate:        docDate,
				OrderDateTime:  orderDateTime,
				PaymentChannel: optionalCell(row, colIdx, "payment_channel"),
				BuyerUsername:  firstNonEmpty(optionalCell(row, colIdx, "customer_name"), optionalCell(row, colIdx, "shipping_name")),
				TrackingNo:     optionalCell(row, colIdx, "tracking_no"),
				Status:         status,
				Items:          []ShopeeExcelItem{},
			}
			itemMap[orderID] = map[string]int{}
			orderKeys = append(orderKeys, orderID)
		} else if existing := orderMap[orderID]; existing.Status != status ||
			existing.OrderDateTime != orderDateTime ||
			existing.PaymentChannel != optionalCell(row, colIdx, "payment_channel") {
			existing.BlockedReason = "ข้อมูลระดับ Order ซ้ำหลายแถวแต่ไม่ตรงกัน กรุณาส่งออกไฟล์ใหม่จาก Lazada"
		}
		productName := optionalCell(row, colIdx, "product_name")
		optionName := optionalCell(row, colIdx, "option_name")
		rawName := shopeeItemRawName(productName, optionName, "")
		sku := optionalCell(row, colIdx, "seller_sku")
		noSKU := sku == ""
		if noSKU {
			noSKUOrderIDs[orderID] = true
			noSKUItemCount++
			orderMap[orderID].HasNoSKU = true
			orderMap[orderID].NoSKUItemCount++
		}
		grossCents, grossErr := marketplaceMoneyCents(row, colIdx, "unit_price")
		paidCents, paidErr := marketplaceMoneyCents(row, colIdx, "paid_price")
		shippingCents, shippingErr := marketplaceMoneyCents(row, colIdx, "shipping_amount")
		adjustmentKeys := []string{"seller_discount", "platform_discount", "wallet_credit", "bundle_discount"}
		discountCents := int64(0)
		platformDiscountCents := int64(0)
		sellerDiscountCents := int64(0)
		adjustmentInvalid := false
		for _, key := range adjustmentKeys {
			value, moneyErr := marketplaceMoneyCents(row, colIdx, key)
			if moneyErr != nil {
				adjustmentInvalid = true
				continue
			}
			// Lazada exports discounts/credits as negative adjustments. A
			// positive value has different accounting semantics and must not be
			// silently converted into a discount.
			if value > 0 {
				adjustmentInvalid = true
				continue
			}
			value = -value
			discountCents += value
			switch key {
			case "seller_discount":
				sellerDiscountCents += value
			default:
				platformDiscountCents += value
			}
		}
		order := orderMap[orderID]
		if grossErr != nil || paidErr != nil || shippingErr != nil || adjustmentInvalid || grossCents < 0 || shippingCents < 0 || discountCents > grossCents {
			order.BlockedReason = fmt.Sprintf("ยอดเงินแถว %d ไม่ถูกต้อง กรุณาตรวจไฟล์ Lazada", headerRowIdx+rowOffset+2)
		}
		expectedPaid := grossCents - discountCents + shippingCents
		if absInt64(expectedPaid-paidCents) > 1 {
			order.AmountMismatch = true
			order.AmountReviewReason = fmt.Sprintf("Order %s: ยอดเต็ม %.2f - ส่วนลด %.2f + ค่าส่ง %.2f = %.2f แต่ paidPrice %.2f",
				orderID, moneyFromCents(grossCents), moneyFromCents(discountCents), moneyFromCents(shippingCents), moneyFromCents(expectedPaid), moneyFromCents(paidCents))
		}
		gross := moneyFromCents(grossCents)
		discount := moneyFromCents(discountCents)
		item := ShopeeExcelItem{
			SKU: sku, LazadaSKU: optionalCell(row, colIdx, "lazada_sku"),
			OrderItemID:     optionalCell(row, colIdx, "order_item_id"),
			SourceItemID:    optionalCell(row, colIdx, "lazada_id"),
			SourceVariantID: optionalCell(row, colIdx, "lazada_sku"),
			ProductName:     productName, OptionName: optionName, RawName: rawName,
			Price: gross, GrossAmount: gross, DiscountAmount: discount,
			PlatformDiscountAmount: moneyFromCents(platformDiscountCents),
			SellerDiscountAmount:   moneyFromCents(sellerDiscountCents),
			Qty:                    1, NoSKU: noSKU,
		}
		itemKey := strings.Join([]string{item.SKU, item.SourceVariantID, item.ProductName, item.OptionName,
			fmt.Sprintf("%d", grossCents), fmt.Sprintf("%d", discountCents)}, "\x1f")
		if index, exists := itemMap[orderID][itemKey]; exists {
			existing := &order.Items[index]
			existing.Qty++
			existing.GrossAmount = roundFloat(existing.GrossAmount+item.GrossAmount, 2)
			existing.DiscountAmount = roundFloat(existing.DiscountAmount+item.DiscountAmount, 2)
			existing.PlatformDiscountAmount = roundFloat(existing.PlatformDiscountAmount+item.PlatformDiscountAmount, 2)
			existing.SellerDiscountAmount = roundFloat(existing.SellerDiscountAmount+item.SellerDiscountAmount, 2)
		} else {
			itemMap[orderID][itemKey] = len(order.Items)
			order.Items = append(order.Items, item)
		}
		order.ItemGrossAmount += gross
		order.DiscountAmount += discount
		order.PlatformDiscountAmount += moneyFromCents(platformDiscountCents)
		order.SellerDiscountAmount += moneyFromCents(sellerDiscountCents)
		order.NetProductAmount += moneyFromCents(grossCents - discountCents)
		order.ShippingAmount += moneyFromCents(shippingCents)
		order.LinePaidAmount += moneyFromCents(grossCents - discountCents)
		order.PaidAmount += moneyFromCents(paidCents)
	}

	orders := []ShopeeOrder{}
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
		o.LinePaidAmount = roundFloat(o.LinePaidAmount, 2)
		o.PaidAmount = roundFloat(o.PaidAmount, 2)
		o.OrderTotalAmount = o.PaidAmount
		expected := roundFloat(o.ItemGrossAmount-o.DiscountAmount+o.ShippingAmount, 2)
		o.AmountDifference = roundFloat(o.OrderTotalAmount-expected, 2)
		if math.Abs(o.AmountDifference) > 0.01 {
			o.AmountMismatch = true
			if o.AmountReviewReason == "" {
				o.AmountReviewReason = fmt.Sprintf("Order %s: ยอดสินค้าและค่าส่งต่างจากยอดชำระ %.2f", id, o.AmountDifference)
			}
		}
		orders = append(orders, *o)
	}
	if noSKUItemCount > 0 {
		warnings = append(warnings, fmt.Sprintf("พบ %d รายการสินค้าใน %d order ที่ไม่มี sellerSku — ระบบจะใช้ชื่อสินค้า + variation จับคู่แทน", noSKUItemCount, len(noSKUOrderIDs)))
	}
	if skippedCount > 0 {
		parts := make([]string, 0, len(skippedStatuses))
		for status, n := range skippedStatuses {
			parts = append(parts, fmt.Sprintf("%s %d", status, n))
		}
		sort.Strings(parts)
		warnings = append([]string{fmt.Sprintf("กรอง %d แถวเพราะสถานะไม่ใช่ confirmed/shipped/delivered (%s)", skippedCount, strings.Join(parts, ", "))}, warnings...)
	}
	return orders, warnings, skippedCount, nil
}

func lazadaDocDate(raw string) string {
	raw = strings.TrimSpace(raw)
	layouts := []string{
		"02 Jan 2006 15:04",
		"2 Jan 2006 15:04",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return t.Format("2006-01-02")
		}
	}
	if len(raw) >= 10 {
		return raw[:10]
	}
	return time.Now().Format("2006-01-02")
}

func (h *LazadaImportHandler) findLazadaOrderBillID(orderID string) (string, bool, error) {
	if strings.TrimSpace(orderID) == "" {
		return "", false, nil
	}
	var id string
	err := h.billRepo.DB().QueryRow(
		`SELECT id::text
		   FROM bills
		  WHERE source = 'lazada'
		    AND (raw_data->>'order_id' = $1 OR raw_data->>'lazada_order_id' = $1 OR sml_order_id = $1)
		  ORDER BY created_at DESC
		  LIMIT 1`,
		orderID,
	).Scan(&id)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, err
	}
	return id, true, nil
}

func (h *LazadaImportHandler) findLazadaOrderBills(orderIDs []string) (map[string]string, error) {
	result := make(map[string]string)
	if len(orderIDs) == 0 {
		return result, nil
	}
	rows, err := h.billRepo.DB().Query(`SELECT DISTINCT ON (order_id) order_id, id::text
		FROM (
			SELECT id, created_at,
			       COALESCE(NULLIF(raw_data->>'order_id',''), NULLIF(raw_data->>'lazada_order_id',''), sml_order_id) AS order_id
			FROM bills
			WHERE source='lazada'
			  AND (raw_data->>'order_id'=ANY($1) OR raw_data->>'lazada_order_id'=ANY($1) OR sml_order_id=ANY($1))
		) matched
		WHERE order_id IS NOT NULL
		ORDER BY order_id, created_at DESC`, pq.Array(orderIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var orderID, billID string
		if err := rows.Scan(&orderID, &billID); err != nil {
			return nil, err
		}
		result[orderID] = billID
	}
	return result, rows.Err()
}

func (h *LazadaImportHandler) createLazadaImportRun(c *gin.Context, filename, fileToken string, orders []ShopeeOrder, warnings []string, preflight ShopeeImportPreflight) string {
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
	detail, _ := json.Marshal(map[string]interface{}{"preflight": preflight, "warnings": warnings})
	var id string
	err := h.billRepo.DB().QueryRow(
		`INSERT INTO import_runs
		   (source, filename, file_sha256, period_start, period_end,
		    total_orders, new_orders, duplicate_orders, skipped_orders,
		    warning_count, status, detail, created_by)
		 VALUES
		   ('lazada', $1, $2, $3, $4, $5, $6, $7, $8, $9, 'preview', $10, $11)
		 RETURNING id::text`,
		filename, fileToken, periodStart, periodEnd, len(orders), preflight.NewOrders,
		preflight.DuplicateOrders, preflight.SkippedRows, len(warnings), detail, userID,
	).Scan(&id)
	if err != nil {
		h.logger.Warn("lazada_excel: create import run failed", zap.Error(err))
		return ""
	}
	return id
}

func (h *LazadaImportHandler) finishLazadaImportRun(id string, createdCount, failedCount int, status string) {
	if strings.TrimSpace(id) == "" {
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
		id, createdCount, failedCount, status,
	); err != nil {
		h.logger.Warn("lazada_excel: update import run failed", zap.String("import_run_id", id), zap.Error(err))
	}
}

func (h *LazadaImportHandler) lookupCatalogItem(code string) *models.CatalogItem {
	code = strings.TrimSpace(code)
	if code == "" || h.catalogRepo == nil {
		return nil
	}
	item, err := h.catalogRepo.GetActive(code)
	if err != nil {
		h.logger.Warn("lazada_excel: catalog sku lookup failed", zap.String("sku", code), zap.Error(err))
		return nil
	}
	return item
}

func isDuplicateMarketplaceBillError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "bills_lazada_order_id_unique") ||
		strings.Contains(msg, "bills_tiktok_order_id_unique") ||
		(strings.Contains(msg, "duplicate key") && strings.Contains(msg, "order_id"))
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func strconvAtoi(s string) (int, error) {
	n := 0
	for _, r := range strings.TrimSpace(s) {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("invalid integer")
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}
