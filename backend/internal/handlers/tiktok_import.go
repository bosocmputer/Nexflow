package handlers

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
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

const (
	tiktokMaxFileBytes = 20 * 1024 * 1024
	tiktokMaxRows      = 50_000
	tiktokMaxOrders    = 5_000
	tiktokPreviewTTL   = 15 * time.Minute
)

type tiktokPendingPreview struct {
	bytes       []byte
	artifactID  string
	filename    string
	fileHash    string
	importRunID string
	userID      string
	uploadedAt  time.Time
}

type tiktokConfirmRequest struct {
	PreviewToken string   `json:"preview_token"`
	ImportRunID  string   `json:"import_run_id"`
	OrderIDs     []string `json:"order_ids"`
}

// TikTokImportHandler mirrors the Shopee Excel import flow: preview first,
// then create local sale bills for manual review and SML retry.
type TikTokImportHandler struct {
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

func NewTikTokImportHandler(
	billRepo *repository.BillRepo,
	mappingRepo *repository.MappingRepo,
	auditRepo *repository.AuditLogRepo,
	cfg *config.Config,
	channelDefaults *repository.ChannelDefaultRepo,
	catalogRepo *repository.SMLCatalogRepo,
	catalogSvc *catalog.SMLCatalogService,
	aliasRepo *repository.MarketplaceAliasRepo,
	logger *zap.Logger,
) *TikTokImportHandler {
	h := &TikTokImportHandler{
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

func (h *TikTokImportHandler) SetArtifactService(svc *artifact.Service) {
	h.artifactSvc = svc
}

func (h *TikTokImportHandler) gcPendingUploads() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		h.pendingUploads.Range(func(key, val any) bool {
			if pu, ok := val.(*tiktokPendingPreview); ok && now.Sub(pu.uploadedAt) > tiktokPreviewTTL {
				h.pendingUploads.Delete(key)
			}
			return true
		})
	}
}

func (h *TikTokImportHandler) GetConfig(c *gin.Context) {
	config := h.currentSaleConfig()
	// The browser only needs enough information to label the destination.
	// SML credentials, tenant and internal URLs remain server-side and are
	// loaded again from channel_defaults during confirm.
	c.JSON(http.StatusOK, gin.H{
		"endpoint":        config.Endpoint,
		"doc_format_code": config.DocFormat,
	})
}

func (h *TikTokImportHandler) currentSaleConfig() ShopeeConfigRequest {
	custCode := ""
	whCode := h.cfg.ShopeeSMLWHCode
	shelfCode := h.cfg.ShopeeSMLShelfCode
	vatType := h.cfg.ShopeeSMLVATType
	vatRate := h.cfg.ShopeeSMLVATRate
	docFormat := h.cfg.ShopeeSMLDocFormat
	endpoint := "/api/v1/ic/sale-orders"
	if h.channelDefaults != nil {
		if def, _ := h.channelDefaults.Get("tiktok", "sale"); def != nil {
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

func (h *TikTokImportHandler) ListRuns(c *gin.Context) {
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
		  WHERE source = 'tiktok'
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

func (h *TikTokImportHandler) Preview(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณาแนบไฟล์ TikTok (.xlsx หรือ .csv)"})
		return
	}
	lowerName := strings.ToLower(fileHeader.Filename)
	isXLSX := strings.HasSuffix(lowerName, ".xlsx")
	isCSV := strings.HasSuffix(lowerName, ".csv")
	if !isXLSX && !isCSV {
		c.JSON(http.StatusBadRequest, gin.H{"error": "รองรับไฟล์ .xlsx หรือ .csv เท่านั้น"})
		return
	}
	if fileHeader.Size > tiktokMaxFileBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "ไฟล์เกิน 20 MB กรุณาแบ่งไฟล์แล้วนำเข้าใหม่"})
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "เปิดไฟล์ไม่ได้"})
		return
	}
	defer file.Close()
	rawBytes, err := io.ReadAll(io.LimitReader(file, tiktokMaxFileBytes+1))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "อ่านไฟล์ไม่ได้"})
		return
	}
	if len(rawBytes) > tiktokMaxFileBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "ไฟล์เกิน 20 MB กรุณาแบ่งไฟล์แล้วนำเข้าใหม่"})
		return
	}

	var orders []ShopeeOrder
	var warnings []string
	var skippedCount int
	if isCSV {
		orders, warnings, skippedCount, err = parseTikTokCSV(bytes.NewReader(rawBytes))
	} else {
		orders, warnings, skippedCount, err = parseTikTokExcel(bytes.NewReader(rawBytes))
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(orders) > tiktokMaxOrders {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "ไฟล์มีเกิน 5,000 orders กรุณาแบ่งไฟล์แล้วนำเข้าใหม่"})
		return
	}
	var channelDefault *models.ChannelDefault
	if h.channelDefaults != nil {
		channelDefault, _ = h.channelDefaults.Get("tiktok", "sale")
	}
	applyTikTokShippingPreflight(orders, channelDefault)
	sum := sha256.Sum256(rawBytes)
	fileHash := hex.EncodeToString(sum[:])
	orderIDs := make([]string, 0, len(orders))
	for i := range orders {
		orderIDs = append(orderIDs, orders[i].OrderID)
	}
	existingBills, err := h.findTikTokOrderBills(orderIDs)
	if err != nil {
		h.logger.Error("tiktok_import: duplicate preflight failed", zap.Error(err))
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
	importRunID := h.createTikTokImportRun(c, fileHeader.Filename, fileHash, orders, warnings, preflight)
	if importRunID == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "สร้างรอบนำเข้าไม่สำเร็จ กรุณาลองใหม่"})
		return
	}
	previewToken, err := randomTikTokPreviewToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "สร้าง preview token ไม่สำเร็จ"})
		return
	}
	artifactID := ""
	if h.artifactSvc != nil {
		kind, contentType := tiktokArtifactType(fileHeader.Filename)
		saved, saveErr := h.artifactSvc.SaveForImportRun(importRunID, kind, fileHeader.Filename, contentType, rawBytes,
			map[string]interface{}{"source": "tiktok", "file_sha256": fileHash})
		if saveErr != nil {
			h.finishTikTokImportRun(importRunID, 0, 1, "failed")
			h.logger.Error("tiktok_import: persist source artifact", zap.String("import_run_id", importRunID), zap.Error(saveErr))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "เก็บไฟล์ต้นฉบับไม่สำเร็จ กรุณาลองใหม่"})
			return
		}
		artifactID = saved.ID
	}
	pendingBytes := rawBytes
	if artifactID != "" {
		pendingBytes = nil
	}
	h.pendingUploads.Store(previewToken, &tiktokPendingPreview{
		bytes: pendingBytes, artifactID: artifactID, filename: fileHeader.Filename, fileHash: fileHash,
		importRunID: importRunID, userID: c.GetString("user_id"), uploadedAt: time.Now(),
	})

	if h.auditRepo != nil {
		var userID *string
		if uid := c.GetString("user_id"); uid != "" {
			userID = &uid
		}
		_ = h.auditRepo.Log(models.AuditEntry{
			Action:  "tiktok_import_preview",
			UserID:  userID,
			Source:  "tiktok",
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

func (h *TikTokImportHandler) Confirm(c *gin.Context) {
	if blockIfCatalogNotReady(c, h.catalogRepo) {
		return
	}
	var req tiktokConfirmRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request ไม่ถูกต้อง", "code": "invalid_request"})
		return
	}
	if strings.TrimSpace(req.PreviewToken) == "" || strings.TrimSpace(req.ImportRunID) == "" {
		writeTikTokPreviewExpired(c)
		return
	}
	pending, ok := h.consumeTikTokPreview(req.PreviewToken, req.ImportRunID, c.GetString("user_id"), time.Now())
	if !ok {
		writeTikTokPreviewExpired(c)
		return
	}
	sourceBytes := pending.bytes
	if pending.artifactID != "" {
		if h.artifactSvc == nil {
			writeTikTokPreviewExpired(c)
			return
		}
		var readErr error
		sourceBytes, _, readErr = h.artifactSvc.ReadForImportRun(pending.importRunID, pending.artifactID)
		if readErr != nil {
			h.logger.Warn("tiktok_import: read source artifact", zap.String("import_run_id", pending.importRunID), zap.Error(readErr))
			writeTikTokPreviewExpired(c)
			return
		}
	}
	sum := sha256.Sum256(sourceBytes)
	if hex.EncodeToString(sum[:]) != pending.fileHash {
		c.JSON(http.StatusConflict, gin.H{"error": "ไฟล์ preview ถูกเปลี่ยน กรุณาอัปโหลดใหม่", "code": "preview_tampered"})
		return
	}

	orders, _, _, err := parseTikTokSource(pending.filename, sourceBytes)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "อ่านไฟล์ต้นฉบับซ้ำไม่สำเร็จ กรุณาอัปโหลดใหม่", "code": "preview_expired"})
		return
	}
	selectedSet := make(map[string]bool, len(req.OrderIDs))
	for _, rawID := range req.OrderIDs {
		id := strings.TrimSpace(rawID)
		if id == "" || selectedSet[id] {
			continue
		}
		selectedSet[id] = true
	}
	if len(selectedSet) == 0 || len(selectedSet) > tiktokMaxOrders {
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
	existingBills, err := h.findTikTokOrderBills(selectedIDs)
	if err != nil {
		h.logger.Error("tiktok_import: confirm duplicate check failed", zap.Error(err))
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "ตรวจสอบ Order ซ้ำไม่สำเร็จ กรุณาอัปโหลดใหม่", "code": "duplicate_check_failed"})
		return
	}

	config := h.currentSaleConfig()
	var def *models.ChannelDefault
	if h.channelDefaults != nil {
		def, _ = h.channelDefaults.Get("tiktok", "sale")
	}
	if err := validateTikTokShippingConfig(selectedOrders, def); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error(), "code": "shipping_item_not_configured"})
		return
	}
	documentRoute := shopeeImportRoute(config)
	destinationName := shopeeImportDocumentName(config)
	reviewPath := shopeeImportReviewPath(config)
	userIDValue := c.GetString("user_id")
	var userID *string
	if userIDValue != "" {
		userID = &userIDValue
	}
	traceID := c.GetString("trace_id")
	confirmStart := time.Now()
	resolutionBatch, err := prepareMarketplaceResolution(c.Request.Context(), "tiktok", selectedOrders, selectedSet,
		h.catalogRepo, h.catalogSvc, h.aliasRepo, h.mappingRepo, h.logger)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "เตรียมข้อมูลสินค้า SML ไม่สำเร็จ กรุณาลองใหม่", "code": "catalog_unavailable"})
		return
	}
	defer flushMarketplaceResolutionUsage(resolutionBatch, h.aliasRepo, h.logger)

	results := make([]ConfirmResult, 0, len(selectedOrders))
	for _, order := range selectedOrders {
		if billID, exists := existingBills[order.OrderID]; exists {
			results = append(results, ConfirmResult{OrderID: order.OrderID, BillID: billID, Message: "order นี้มีอยู่ในระบบแล้ว (ข้าม)"})
			continue
		}
		items := make([]models.BillItem, 0, len(order.Items)+1)
		allMapped := true
		orderItemIDs := make([]string, 0, len(order.Items))
		for itemIndex, sourceItem := range order.Items {
			sourceItem.OrderItemID = marketplaceSourceLineID(order.OrderID, itemIndex, sourceItem)
			rawName := shopeeItemRawName(sourceItem.ProductName, sourceItem.OptionName, sourceItem.RawName)
			if sourceItem.OrderItemID != "" {
				orderItemIDs = append(orderItemIDs, sourceItem.OrderItemID)
			}
			resolved := resolutionBatch.resolutionScoped("default", sourceItem.SourceItemID, sourceItem.SourceVariantID, sourceItem.SKU, rawName)
			billItem, mapped, resolveErr := marketplaceBillItemFromResolution("tiktok", "default", sourceItem, config.UnitCode,
				resolved, resolutionBatch, h.aliasRepo, userIDValue)
			if resolveErr != nil {
				h.logger.Warn("tiktok_import: save product master failed", zap.String("order_id", order.OrderID), zap.Error(resolveErr))
				mapped = false
			}
			if mapped {
				exactSKU := normalizeMarketplaceSKU(sourceItem.SKU) != "" && resolutionBatch.catalogLookup(sourceItem.SKU) != nil
				recordMarketplaceResolutionUsage(resolutionBatch, resolved, exactSKU)
			} else {
				allMapped = false
			}
			items = append(items, billItem)
		}
		if order.ShippingAmount > 0 && def != nil {
			shippingGross := order.ShippingAmount
			shippingPrice := order.ShippingAmount
			shippingCode := strings.TrimSpace(def.ShippingItemCode)
			shippingUnit := strings.TrimSpace(def.ShippingItemUnitCode)
			items = append(items, models.BillItem{RawName: "ค่าจัดส่ง TikTok", SourceSKU: models.TikTokShippingSourceSKU,
				ItemCode: &shippingCode, UnitCode: &shippingUnit, Qty: 1, Price: &shippingPrice,
				GrossAmount: &shippingGross, Mapped: true})
		}
		status := "pending"
		if !allMapped || order.AmountMismatch {
			status = "needs_review"
		}
		fingerprint := tikTokAmountFingerprint(items)
		rawData, _ := json.Marshal(map[string]interface{}{
			"flow": "tiktok_excel", "tiktok_order_id": order.OrderID, "order_id": order.OrderID,
			"doc_date": order.DocDate, "order_datetime": order.OrderDateTime, "payment_channel": order.PaymentChannel,
			"customer_name": order.BuyerUsername, "tracking_no": order.TrackingNo, "status": order.Status,
			"item_count": order.ItemCount, "total_qty": order.TotalQty, "order_total_amount": order.OrderTotalAmount,
			"item_gross_amount": order.ItemGrossAmount, "platform_discount_amount": order.PlatformDiscountAmount,
			"seller_discount_amount": order.SellerDiscountAmount, "discount_amount": order.DiscountAmount,
			"net_product_amount": order.NetProductAmount, "shipping_amount": order.ShippingAmount,
			"taxes_amount": order.TaxesAmount, "payment_discount_amount": order.PaymentDiscountAmount,
			"amount_difference": order.AmountDifference, "amount_review_required": order.AmountMismatch,
			"amount_review_reason": order.AmountReviewReason, "amount_source_fingerprint": fingerprint,
			"has_no_sku": order.HasNoSKU, "no_sku_item_count": order.NoSKUItemCount,
			"multi_line": order.MultiLine, "order_item_ids": orderItemIDs, "import_run_id": req.ImportRunID,
			"document_route": documentRoute, "sml_destination": destinationName,
		})
		bill := &models.Bill{BillType: "sale", Source: "tiktok", SourceAccountKey: "default", Status: status,
			DocumentRoute: documentRoute, RawData: rawData, SMLOrderID: order.OrderID, CreatedBy: userID}
		durationMs := int(time.Since(confirmStart).Milliseconds())
		audit := models.AuditEntry{Action: "bill_created", UserID: userID, Source: "tiktok", Level: "info",
			TraceID: traceID, DurationMs: &durationMs, Detail: map[string]interface{}{
				"order_id": order.OrderID, "items_count": len(items), "status": status,
				"amount_mismatch": order.AmountMismatch, "flow": "tiktok_excel",
			}}
		if err := h.billRepo.CreateWithItemsAndAudit(bill, items, audit); err != nil {
			if isDuplicateMarketplaceBillError(err) {
				billID, _, _ := h.findTikTokOrderBillID(order.OrderID)
				results = append(results, ConfirmResult{OrderID: order.OrderID, BillID: billID, Message: "order นี้ถูกสร้างไปแล้วระหว่างนำเข้า (ข้าม)"})
				continue
			}
			h.logger.Error("tiktok_import: atomic bill creation failed", zap.String("order_id", order.OrderID), zap.Error(err))
			results = append(results, ConfirmResult{OrderID: order.OrderID, Message: "บันทึก bill และรายการสินค้าไม่สำเร็จ"})
			continue
		}
		results = append(results, ConfirmResult{OrderID: order.OrderID, Success: true, BillID: bill.ID,
			Message: fmt.Sprintf("สร้าง%sแล้ว (status=%s) — รอตรวจสอบใน %s", destinationName, status, reviewPath)})
	}
	successCount := 0
	for _, result := range results {
		if result.Success {
			successCount++
		}
	}
	h.finishTikTokImportRun(req.ImportRunID, successCount, len(results)-successCount, "confirmed")
	c.JSON(http.StatusOK, gin.H{"results": results, "success_count": successCount,
		"fail_count": len(results) - successCount, "total": len(results),
		"message": destinationName + "ถูกสร้างแล้ว — กรุณาตรวจสอบก่อนส่ง SML"})
}

func (h *TikTokImportHandler) consumeTikTokPreview(token, importRunID, userID string, now time.Time) (*tiktokPendingPreview, bool) {
	loaded, ok := h.pendingUploads.LoadAndDelete(strings.TrimSpace(token))
	if !ok {
		return nil, false
	}
	pending, ok := loaded.(*tiktokPendingPreview)
	if !ok || pending.importRunID != strings.TrimSpace(importRunID) || pending.userID != userID ||
		now.Sub(pending.uploadedAt) > tiktokPreviewTTL || now.Before(pending.uploadedAt) {
		return nil, false
	}
	return pending, true
}

var tiktokColCandidates = map[string][]string{
	"order_id":          {"Order ID"},
	"order_item_id":     {"SKU ID"},
	"status":            {"Order Status"},
	"substatus":         {"Order Substatus"},
	"cancel_type":       {"Cancelation/Return Type"},
	"seller_sku":        {"Seller SKU"},
	"tiktok_sku":        {"SKU ID"},
	"order_date":        {"Created Time"},
	"payment_time":      {"Paid Time"},
	"delivered_date":    {"Delivered Time"},
	"customer_name":     {"Recipient", "Buyer Username"},
	"payment_channel":   {"Payment Method"},
	"tracking_no":       {"Tracking ID"},
	"product_name":      {"Product Name"},
	"option_name":       {"Variation"},
	"qty":               {"Quantity"},
	"gross_amount":      {"SKU Subtotal Before Discount"},
	"platform_discount": {"SKU Platform Discount"},
	"seller_discount":   {"SKU Seller Discount"},
	"paid_price":        {"SKU Subtotal After Discount"},
	"unit_price":        {"SKU Unit Original Price"},
	"order_amount":      {"Order Amount"},
	"shipping_amount":   {"Shipping Fee After Discount"},
	"taxes":             {"Taxes"},
	"payment_discount":  {"Payment platform discount"},
}

var tiktokAllowedStatuses = map[string]bool{
	"จัดส่งแล้ว":   true,
	"shipped":      true,
	"delivered":    true,
	"completed":    true,
	"เสร็จสมบูรณ์": true,
}

func parseTikTokExcel(src interface{ Read([]byte) (int, error) }) ([]ShopeeOrder, []string, int, error) {
	f, err := excelize.OpenReader(src)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("เปิดไฟล์ Excel ไม่ได้: %w", err)
	}
	defer f.Close()
	sheetName := f.GetSheetName(0)
	rowStream, err := f.Rows(sheetName)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("อ่าน sheet ไม่ได้: %w", err)
	}
	defer rowStream.Close()
	rows := make([][]string, 0, 1024)
	for rowStream.Next() {
		row, rowErr := rowStream.Columns()
		if rowErr != nil {
			return nil, nil, 0, fmt.Errorf("อ่านแถว Excel ไม่ได้: %w", rowErr)
		}
		rows = append(rows, row)
		if len(rows) > tiktokMaxRows+1 {
			return nil, nil, 0, fmt.Errorf("ไฟล์มีเกิน %d แถว กรุณาแบ่งไฟล์แล้วนำเข้าใหม่", tiktokMaxRows)
		}
	}
	if err := rowStream.Error(); err != nil {
		return nil, nil, 0, fmt.Errorf("อ่าน sheet ไม่ได้: %w", err)
	}
	if len(rows) < 2 {
		return nil, nil, 0, fmt.Errorf("ไฟล์ว่างหรือไม่มีข้อมูล")
	}
	return parseTikTokRows(rows)
}

func parseTikTokCSV(src io.Reader) ([]ShopeeOrder, []string, int, error) {
	r := csv.NewReader(src)
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	rows := make([][]string, 0, 1024)
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, 0, fmt.Errorf("อ่านไฟล์ CSV ไม่ได้: %w", err)
		}
		rows = append(rows, row)
		if len(rows) > tiktokMaxRows+1 {
			return nil, nil, 0, fmt.Errorf("ไฟล์มีเกิน %d แถว กรุณาแบ่งไฟล์แล้วนำเข้าใหม่", tiktokMaxRows)
		}
	}
	if len(rows) < 2 {
		return nil, nil, 0, fmt.Errorf("ไฟล์ว่างหรือไม่มีข้อมูล")
	}
	return parseTikTokRows(rows)
}

func parseTikTokRows(rows [][]string) ([]ShopeeOrder, []string, int, error) {
	if len(rows) > tiktokMaxRows+1 {
		return nil, nil, 0, fmt.Errorf("ไฟล์มีเกิน %d แถว กรุณาแบ่งไฟล์แล้วนำเข้าใหม่", tiktokMaxRows)
	}
	headerRowIdx := -1
	for i, row := range rows {
		for _, cell := range row {
			if strings.EqualFold(cleanTikTokCell(cell), "Order ID") {
				headerRowIdx = i
				break
			}
		}
		if headerRowIdx >= 0 {
			break
		}
	}
	if headerRowIdx < 0 {
		return nil, nil, 0, fmt.Errorf("ไม่พบ header 'Order ID' ในไฟล์ TikTok")
	}
	headerRow := rows[headerRowIdx]
	colIdx := map[string]int{}
	for field, candidates := range tiktokColCandidates {
		for j, cell := range headerRow {
			for _, candidate := range candidates {
				if strings.EqualFold(cleanTikTokCell(cell), candidate) {
					colIdx[field] = j
					break
				}
			}
			if _, found := colIdx[field]; found {
				break
			}
		}
	}
	required := []string{"order_id", "status", "order_date", "product_name", "qty", "gross_amount",
		"platform_discount", "seller_discount", "paid_price", "shipping_amount", "order_amount"}
	for _, field := range required {
		if _, ok := colIdx[field]; !ok {
			return nil, nil, 0, fmt.Errorf("ไม่พบ column '%s' ในไฟล์ TikTok", field)
		}
	}

	type accumulator struct {
		order        *ShopeeOrder
		itemIndex    map[string]int
		orderValues  map[string]int64
		orderStrings map[string]string
		inconsistent map[string]bool
	}
	accumulators := map[string]*accumulator{}
	orderKeys := make([]string, 0)
	warnings := []string{}
	noSKUOrders := map[string]bool{}
	noSKUItems := 0
	skippedRows := 0
	skippedOrderIDs := map[string]bool{}
	skippedStatuses := map[string]int{}

	for rowIndex, row := range rows[headerRowIdx+1:] {
		if len(row) == 0 {
			continue
		}
		orderID := tikTokCell(row, colIdx, "order_id")
		if orderID == "" {
			continue
		}
		status := tikTokCell(row, colIdx, "status")
		if !tiktokAllowedStatuses[strings.ToLower(status)] && !tiktokAllowedStatuses[status] {
			skippedRows++
			skippedOrderIDs[orderID] = true
			if status == "" {
				status = "(ว่าง)"
			}
			skippedStatuses[status]++
			continue
		}
		grossCents, err := tikTokMoneyCents(row, colIdx, "gross_amount")
		if err != nil {
			return nil, nil, 0, fmt.Errorf("Order %s แถว %d: %w", orderID, rowIndex+2, err)
		}
		platformDiscountCents, err := tikTokMoneyCents(row, colIdx, "platform_discount")
		if err != nil {
			return nil, nil, 0, fmt.Errorf("Order %s แถว %d: %w", orderID, rowIndex+2, err)
		}
		sellerDiscountCents, err := tikTokMoneyCents(row, colIdx, "seller_discount")
		if err != nil {
			return nil, nil, 0, fmt.Errorf("Order %s แถว %d: %w", orderID, rowIndex+2, err)
		}
		netLineCents, err := tikTokMoneyCents(row, colIdx, "paid_price")
		if err != nil {
			return nil, nil, 0, fmt.Errorf("Order %s แถว %d: %w", orderID, rowIndex+2, err)
		}
		if grossCents < 0 || platformDiscountCents < 0 || sellerDiscountCents < 0 || netLineCents < 0 {
			return nil, nil, 0, fmt.Errorf("Order %s แถว %d: ยอดเงินต้องไม่ติดลบ", orderID, rowIndex+2)
		}

		acc := accumulators[orderID]
		if acc == nil {
			orderDateTime := tikTokCell(row, colIdx, "order_date")
			docDate := tiktokDocDate(orderDateTime)
			acc = &accumulator{order: &ShopeeOrder{OrderID: orderID, DocDate: docDate,
				OrderDateTime: orderDateTime, PaymentTime: tikTokCell(row, colIdx, "payment_time"),
				PaymentChannel: tikTokCell(row, colIdx, "payment_channel"), BuyerUsername: tikTokCell(row, colIdx, "customer_name"),
				TrackingNo: tikTokCell(row, colIdx, "tracking_no"), Status: status, Items: []ShopeeExcelItem{}},
				itemIndex: map[string]int{}, orderValues: map[string]int64{}, orderStrings: map[string]string{}, inconsistent: map[string]bool{}}
			accumulators[orderID] = acc
			orderKeys = append(orderKeys, orderID)
			if docDate == "" {
				acc.order.BlockedReason = "วันที่สร้าง Order ไม่ถูกต้อง กรุณาตรวจไฟล์ TikTok"
			}
		}
		for _, field := range []string{"status", "order_date", "payment_time", "payment_channel", "tracking_no", "customer_name"} {
			value := tikTokCell(row, colIdx, field)
			if previous, exists := acc.orderStrings[field]; exists && previous != value {
				acc.inconsistent[field] = true
			} else if !exists {
				acc.orderStrings[field] = value
			}
		}
		for _, field := range []string{"order_amount", "shipping_amount", "taxes", "payment_discount"} {
			value, valueErr := tikTokMoneyCents(row, colIdx, field)
			if valueErr != nil {
				return nil, nil, 0, fmt.Errorf("Order %s แถว %d: %w", orderID, rowIndex+2, valueErr)
			}
			if previous, exists := acc.orderValues[field]; exists && previous != value {
				acc.inconsistent[field] = true
			} else if !exists {
				acc.orderValues[field] = value
			}
		}

		qty := tikTokFloat(row, colIdx, "qty")
		if qty <= 0 {
			acc.order.BlockedReason = "จำนวนสินค้าในไฟล์ต้องมากกว่า 0"
			qty = 1
		}
		productName := tikTokCell(row, colIdx, "product_name")
		optionName := tikTokCell(row, colIdx, "option_name")
		sellerSKU := normalizeMarketplaceSKU(tikTokCell(row, colIdx, "seller_sku"))
		variantID := strings.TrimSpace(tikTokCell(row, colIdx, "tiktok_sku"))
		noSellerSKU := sellerSKU == ""
		if noSellerSKU {
			noSKUItems++
			noSKUOrders[orderID] = true
			acc.order.HasNoSKU = true
			acc.order.NoSKUItemCount++
		}
		lineDiscountCents := platformDiscountCents + sellerDiscountCents
		if lineDiscountCents > grossCents {
			acc.order.BlockedReason = "ส่วนลดสินค้ามากกว่ายอดเต็ม กรุณาตรวจไฟล์ TikTok"
		}
		if absInt64((grossCents-lineDiscountCents)-netLineCents) > 1 {
			acc.order.AmountMismatch = true
			acc.order.AmountReviewReason = "ยอดสุทธิสินค้าไม่ตรงกับยอดเต็มหักส่วนลด"
		}
		price := 0.0
		if qty > 0 {
			price = moneyFromCents(grossCents) / qty
		}
		key := strings.Join([]string{sellerSKU, variantID, productName, optionName,
			strconv.FormatInt(grossCents, 10), strconv.FormatInt(lineDiscountCents, 10)}, "\x1f")
		if index, exists := acc.itemIndex[key]; exists {
			item := &acc.order.Items[index]
			item.Qty += qty
			item.GrossAmount = roundFloat(item.GrossAmount+moneyFromCents(grossCents), 2)
			item.DiscountAmount = roundFloat(item.DiscountAmount+moneyFromCents(lineDiscountCents), 2)
			item.PlatformDiscountAmount = roundFloat(item.PlatformDiscountAmount+moneyFromCents(platformDiscountCents), 2)
			item.SellerDiscountAmount = roundFloat(item.SellerDiscountAmount+moneyFromCents(sellerDiscountCents), 2)
			item.Price = item.GrossAmount / item.Qty
		} else {
			acc.itemIndex[key] = len(acc.order.Items)
			acc.order.Items = append(acc.order.Items, ShopeeExcelItem{SKU: sellerSKU, TikTokSKU: variantID,
				OrderItemID: firstNonEmpty(variantID, fmt.Sprintf("%s-%d", orderID, rowIndex+1)), SourceVariantID: variantID,
				ProductName: productName, OptionName: optionName, RawName: shopeeItemRawName(productName, optionName, ""),
				Price: price, GrossAmount: moneyFromCents(grossCents), DiscountAmount: moneyFromCents(lineDiscountCents),
				PlatformDiscountAmount: moneyFromCents(platformDiscountCents), SellerDiscountAmount: moneyFromCents(sellerDiscountCents),
				Qty: qty, NoSKU: noSellerSKU})
		}
	}

	orders := make([]ShopeeOrder, 0, len(orderKeys))
	for _, orderID := range orderKeys {
		acc := accumulators[orderID]
		order := acc.order
		if skippedOrderIDs[orderID] {
			order.BlockedReason = "Order นี้มีทั้งรายการขายและรายการยกเลิก/คืนสินค้า กรุณาแยกตรวจใน TikTok ก่อนนำเข้า"
		}
		for field := range acc.inconsistent {
			if order.BlockedReason == "" {
				order.BlockedReason = fmt.Sprintf("ข้อมูล %s ของ Order เดียวกันไม่ตรงกันหลายแถว", field)
			}
			break
		}
		if len(order.Items) == 0 {
			continue
		}
		grossCents, platformCents, sellerCents := int64(0), int64(0), int64(0)
		for _, item := range order.Items {
			order.TotalQty += item.Qty
			grossCents += centsFromMoney(item.GrossAmount)
			platformCents += centsFromMoney(item.PlatformDiscountAmount)
			sellerCents += centsFromMoney(item.SellerDiscountAmount)
		}
		netProductCents := grossCents - platformCents - sellerCents
		orderAmountCents := acc.orderValues["order_amount"]
		shippingCents := acc.orderValues["shipping_amount"]
		taxesCents := acc.orderValues["taxes"]
		paymentDiscountCents := acc.orderValues["payment_discount"]
		controlCents := netProductCents + shippingCents + taxesCents - paymentDiscountCents
		differenceCents := orderAmountCents - controlCents
		if absInt64(differenceCents) > 1 {
			order.AmountMismatch = true
			if order.AmountReviewReason == "" {
				order.AmountReviewReason = "Order Amount ไม่ตรงกับยอดสินค้า ค่าส่ง ภาษี และส่วนลดชำระเงิน"
			}
		}
		order.ItemCount = len(order.Items)
		order.MultiLine = len(order.Items) > 1
		order.ItemGrossAmount = moneyFromCents(grossCents)
		order.PlatformDiscountAmount = moneyFromCents(platformCents)
		order.SellerDiscountAmount = moneyFromCents(sellerCents)
		order.DiscountAmount = moneyFromCents(platformCents + sellerCents)
		order.NetProductAmount = moneyFromCents(netProductCents)
		order.LinePaidAmount = order.NetProductAmount
		order.ShippingAmount = moneyFromCents(shippingCents)
		order.TaxesAmount = moneyFromCents(taxesCents)
		order.PaymentDiscountAmount = moneyFromCents(paymentDiscountCents)
		order.OrderTotalAmount = moneyFromCents(orderAmountCents)
		order.PaidAmount = order.OrderTotalAmount
		order.AmountDifference = moneyFromCents(differenceCents)
		orders = append(orders, *order)
	}
	if len(orders) > tiktokMaxOrders {
		return nil, nil, 0, fmt.Errorf("ไฟล์มีเกิน %d orders กรุณาแบ่งไฟล์แล้วนำเข้าใหม่", tiktokMaxOrders)
	}
	if noSKUItems > 0 {
		warnings = append(warnings, fmt.Sprintf("พบ %d รายการใน %d orders ที่ไม่มี Seller SKU — ระบบจะใช้ TikTok SKU ID จับคู่ variant ที่ผู้ใช้ยืนยันเท่านั้น", noSKUItems, len(noSKUOrders)))
	}
	if skippedRows > 0 {
		parts := make([]string, 0, len(skippedStatuses))
		for status, count := range skippedStatuses {
			parts = append(parts, fmt.Sprintf("%s %d", status, count))
		}
		sort.Strings(parts)
		warnings = append([]string{fmt.Sprintf("ข้าม %d แถวจาก %d orders เพราะสถานะไม่ใช่ จัดส่งแล้ว/เสร็จสมบูรณ์ (%s)", skippedRows, len(skippedOrderIDs), strings.Join(parts, ", "))}, warnings...)
	}
	return orders, warnings, len(skippedOrderIDs), nil
}

func randomTikTokPreviewToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func writeTikTokPreviewExpired(c *gin.Context) {
	c.JSON(http.StatusConflict, gin.H{
		"error": "Preview หมดอายุหรือถูกใช้แล้ว กรุณาอัปโหลดไฟล์ใหม่",
		"code":  "preview_expired",
	})
}

func parseTikTokSource(filename string, data []byte) ([]ShopeeOrder, []string, int, error) {
	if strings.HasSuffix(strings.ToLower(filename), ".csv") {
		return parseTikTokCSV(bytes.NewReader(data))
	}
	return parseTikTokExcel(bytes.NewReader(data))
}

func tiktokArtifactType(filename string) (string, string) {
	if strings.HasSuffix(strings.ToLower(filename), ".csv") {
		return "csv", "text/csv; charset=utf-8"
	}
	return "xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
}

func validateTikTokShippingConfig(orders []ShopeeOrder, def *models.ChannelDefault) error {
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
		return fmt.Errorf("ไฟล์มีค่าส่งสุทธิ กรุณาตั้งสินค้า SML สำหรับค่าจัดส่ง TikTok ที่เมนูเส้นทางเอกสารก่อน")
	}
	return nil
}

func applyTikTokShippingPreflight(orders []ShopeeOrder, def *models.ChannelDefault) {
	if validateTikTokShippingConfig(orders, def) == nil {
		return
	}
	for i := range orders {
		if orders[i].ShippingAmount > 0 && orders[i].BlockedReason == "" {
			orders[i].BlockedReason = "ยังไม่ได้ตั้งสินค้า SML สำหรับค่าจัดส่ง TikTok"
		}
	}
}

func tikTokAmountFingerprint(items []models.BillItem) string {
	type fingerprintLine struct {
		RawName         string  `json:"raw_name"`
		SourceSKU       string  `json:"source_sku"`
		SourceVariantID string  `json:"source_variant_id"`
		Qty             float64 `json:"qty"`
		Price           float64 `json:"price"`
		GrossAmount     float64 `json:"gross_amount"`
		DiscountAmount  float64 `json:"discount_amount"`
	}
	lines := make([]fingerprintLine, 0, len(items))
	for _, item := range items {
		price := 0.0
		if item.Price != nil {
			price = *item.Price
		}
		gross := item.Qty * price
		if item.GrossAmount != nil {
			gross = *item.GrossAmount
		}
		lines = append(lines, fingerprintLine{RawName: item.RawName, SourceSKU: item.SourceSKU,
			SourceVariantID: item.SourceVariantID, Qty: item.Qty, Price: price,
			GrossAmount: roundFloat(gross, 2), DiscountAmount: roundFloat(item.DiscountAmount, 2)})
	}
	encoded, _ := json.Marshal(lines)
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

func tikTokMoneyCents(row []string, colIdx map[string]int, key string) (int64, error) {
	raw := strings.ReplaceAll(tikTokCell(row, colIdx, key), ",", "")
	if raw == "" {
		return 0, nil
	}
	value, err := parseDecimalCents(raw)
	if err != nil {
		return 0, fmt.Errorf("ยอด %s ไม่ใช่ตัวเลข", key)
	}
	return value, nil
}

func parseDecimalCents(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	negative := strings.HasPrefix(raw, "-")
	if negative || strings.HasPrefix(raw, "+") {
		raw = raw[1:]
	}
	parts := strings.Split(raw, ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0, fmt.Errorf("invalid decimal")
	}
	for _, part := range parts {
		for _, char := range part {
			if char < '0' || char > '9' {
				return 0, fmt.Errorf("invalid decimal")
			}
		}
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole > (1<<63-1)/100 {
		return 0, fmt.Errorf("decimal overflow")
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	for len(fraction) < 2 {
		fraction += "0"
	}
	fracCents := int64(0)
	if len(fraction) >= 2 {
		fracCents, err = strconv.ParseInt(fraction[:2], 10, 64)
		if err != nil {
			return 0, err
		}
	}
	cents := whole*100 + fracCents
	if len(fraction) > 2 && fraction[2] >= '5' {
		cents++
	}
	if negative {
		cents = -cents
	}
	return cents, nil
}

func centsFromMoney(value float64) int64 {
	if value >= 0 {
		return int64(value*100 + 0.5)
	}
	return int64(value*100 - 0.5)
}

func moneyFromCents(value int64) float64 { return float64(value) / 100 }

func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func cleanTikTokCell(s string) string {
	s = strings.TrimPrefix(s, "\ufeff")
	return strings.TrimSpace(strings.ReplaceAll(s, "\u00a0", " "))
}

func cleanTikTokRow(row []string) []string {
	out := make([]string, len(row))
	for i, cell := range row {
		out[i] = cleanTikTokCell(cell)
	}
	return out
}

func tikTokCell(row []string, colIdx map[string]int, key string) string {
	if idx, ok := colIdx[key]; ok && idx >= 0 && idx < len(row) {
		return cleanTikTokCell(row[idx])
	}
	return ""
}

func tikTokFloat(row []string, colIdx map[string]int, key string) float64 {
	s := strings.ReplaceAll(tikTokCell(row, colIdx, key), ",", "")
	if s == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func tiktokDocDate(raw string) string {
	raw = cleanTikTokCell(raw)
	layouts := []string{
		"02/01/2006 15:04:05",
		"02/01/2006 15:04",
		"02/01/2006",
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
	return ""
}

func (h *TikTokImportHandler) findTikTokOrderBillID(orderID string) (string, bool, error) {
	if strings.TrimSpace(orderID) == "" {
		return "", false, nil
	}
	var id string
	err := h.billRepo.DB().QueryRow(
		`SELECT id::text
		   FROM bills
		  WHERE source = 'tiktok'
		    AND (raw_data->>'order_id' = $1 OR raw_data->>'tiktok_order_id' = $1 OR sml_order_id = $1)
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

func (h *TikTokImportHandler) findTikTokOrderBills(orderIDs []string) (map[string]string, error) {
	result := make(map[string]string)
	if len(orderIDs) == 0 {
		return result, nil
	}
	rows, err := h.billRepo.DB().Query(`SELECT DISTINCT ON (order_id) order_id, id::text
		FROM (
			SELECT id, created_at,
			       COALESCE(NULLIF(raw_data->>'order_id',''), NULLIF(raw_data->>'tiktok_order_id',''), sml_order_id) AS order_id
			FROM bills
			WHERE source='tiktok'
			  AND (raw_data->>'order_id'=ANY($1) OR raw_data->>'tiktok_order_id'=ANY($1) OR sml_order_id=ANY($1))
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

func (h *TikTokImportHandler) createTikTokImportRun(c *gin.Context, filename, fileToken string, orders []ShopeeOrder, warnings []string, preflight ShopeeImportPreflight) string {
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
		   ('tiktok', $1, $2, $3, $4, $5, $6, $7, $8, $9, 'preview', $10, $11)
		 RETURNING id::text`,
		filename, fileToken, periodStart, periodEnd, len(orders), preflight.NewOrders,
		preflight.DuplicateOrders, preflight.SkippedRows, len(warnings), detail, userID,
	).Scan(&id)
	if err != nil {
		h.logger.Warn("tiktok_excel: create import run failed", zap.Error(err))
		return ""
	}
	return id
}

func (h *TikTokImportHandler) finishTikTokImportRun(id string, createdCount, failedCount int, status string) {
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
		h.logger.Warn("tiktok_excel: update import run failed", zap.String("import_run_id", id), zap.Error(err))
	}
}

func (h *TikTokImportHandler) lookupCatalogItem(code string) *models.CatalogItem {
	code = strings.TrimSpace(code)
	if code == "" || h.catalogRepo == nil {
		return nil
	}
	item, err := h.catalogRepo.GetActive(code)
	if err != nil {
		h.logger.Warn("tiktok_excel: catalog sku lookup failed", zap.String("sku", code), zap.Error(err))
		return nil
	}
	return item
}
