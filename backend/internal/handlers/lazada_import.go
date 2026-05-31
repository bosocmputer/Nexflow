package handlers

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
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

// LazadaImportHandler mirrors the Shopee Excel import flow: preview first,
// then create local sale bills for manual review and SML retry.
type LazadaImportHandler struct {
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

	pendingUploads sync.Map
}

func NewLazadaImportHandler(
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
) *LazadaImportHandler {
	h := &LazadaImportHandler{
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

func (h *LazadaImportHandler) SetArtifactService(svc *artifact.Service) {
	h.artifactSvc = svc
}

func (h *LazadaImportHandler) gcPendingUploads() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		h.pendingUploads.Range(func(key, val any) bool {
			if pu, ok := val.(*pendingUpload); ok && now.Sub(pu.uploadedAt) > pendingUploadTTL {
				h.pendingUploads.Delete(key)
			}
			return true
		})
	}
}

func (h *LazadaImportHandler) GetConfig(c *gin.Context) {
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
	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "เปิดไฟล์ไม่ได้"})
		return
	}
	defer file.Close()
	rawBytes, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "อ่านไฟล์ไม่ได้"})
		return
	}

	orders, warnings, skippedCount, err := parseLazadaExcel(bytes.NewReader(rawBytes))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
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
	dupCount := 0
	for i := range orders {
		if billID, exists, _ := h.findLazadaOrderBillID(orders[i].OrderID); exists {
			orders[i].Duplicate = true
			orders[i].ExistingBillID = billID
			dupCount++
		}
	}
	preflight := buildShopeePreflight(orders, skippedCount, dupCount)
	importRunID := h.createLazadaImportRun(c, fileHeader.Filename, fileToken, orders, warnings, preflight)

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
		FileToken:      fileToken,
	})
}

func (h *LazadaImportHandler) Confirm(c *gin.Context) {
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

	var uploadBytes []byte
	var uploadFilename string
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
		if billID, exists, _ := h.findLazadaOrderBillID(order.OrderID); exists {
			results = append(results, ConfirmResult{
				OrderID: order.OrderID,
				Success: false,
				BillID:  billID,
				Message: "order นี้มีอยู่ในระบบแล้ว (ข้าม)",
			})
			continue
		}

		type itemEnriched struct {
			item       models.BillItem
			candidates []models.CatalogMatch
		}
		var enriched []itemEnriched
		allHigh := true
		orderItemIDs := []string{}

		for _, it := range order.Items {
			rawName := shopeeItemRawName(it.ProductName, it.OptionName, it.RawName)
			if it.OrderItemID != "" {
				orderItemIDs = append(orderItemIDs, it.OrderItemID)
			}
			resolved, ok := resolutionCache[rawName]
			if !ok {
				if h.mappingRepo != nil {
					if m, err := h.mappingRepo.FindByRawName(rawName); err == nil {
						resolved.learned = m
					} else {
						h.logger.Warn("lazada_excel: lookup mapping failed",
							zap.String("raw_name", rawName), zap.Error(err))
					}
				}
				if resolved.learned == nil && h.embSvc != nil && h.embSvc.IsConfigured() && h.catalogIdx != nil && h.catalogIdx.Size() > 0 {
					if emb, err := h.embSvc.EmbedText(rawName); err == nil {
						resolved.matches = h.catalogIdx.Search(emb, topK)
					} else {
						h.logger.Warn("lazada_excel: embedding lookup failed",
							zap.String("raw_name", rawName), zap.Error(err))
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
		rawData, _ := json.Marshal(map[string]interface{}{
			"flow":               "lazada_excel",
			"lazada_order_id":    order.OrderID,
			"order_id":           order.OrderID,
			"doc_date":           order.DocDate,
			"order_datetime":     order.OrderDateTime,
			"payment_channel":    order.PaymentChannel,
			"customer_name":      order.BuyerUsername,
			"tracking_no":        order.TrackingNo,
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
			"multi_line":         order.MultiLine,
			"order_item_ids":     orderItemIDs,
			"import_run_id":      req.ImportRunID,
			"document_route":     documentRoute,
			"sml_destination":    destinationName,
		})
		bill := &models.Bill{
			BillType:      "sale",
			Source:        "lazada",
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
			h.logger.Error("lazada_excel: create bill", zap.String("order_id", order.OrderID), zap.Error(err))
			results = append(results, ConfirmResult{OrderID: order.OrderID, Success: false, Message: "บันทึก bill ล้มเหลว: " + err.Error()})
			continue
		}
		for i := range enriched {
			enriched[i].item.BillID = bill.ID
			candidatesJSON, _ := json.Marshal(enriched[i].candidates)
			_ = h.billRepo.InsertItemWithCandidates(&enriched[i].item, candidatesJSON)
		}
		if h.artifactSvc != nil && uploadBytes != nil {
			meta := map[string]interface{}{"order_id": order.OrderID, "uploaded_by": "", "trace_id": traceID}
			if userID != nil {
				meta["uploaded_by"] = *userID
			}
			filename := uploadFilename
			if filename == "" {
				filename = "lazada-import.xlsx"
			}
			if _, err := h.artifactSvc.Save(
				bill.ID,
				"xlsx",
				filename,
				"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
				uploadBytes,
				meta,
			); err != nil {
				h.logger.Warn("lazada_excel: save artifact failed", zap.String("bill_id", bill.ID), zap.Error(err))
			}
		}
		if h.auditRepo != nil {
			billIDStr := bill.ID
			durMs := int(time.Since(confirmStart).Milliseconds())
			_ = h.auditRepo.Log(models.AuditEntry{
				Action:     "bill_created",
				TargetID:   &billIDStr,
				UserID:     userID,
				Source:     "lazada",
				Level:      "info",
				TraceID:    traceID,
				DurationMs: &durMs,
				Detail: map[string]interface{}{
					"order_id":      order.OrderID,
					"items_count":   len(enriched),
					"all_high_conf": allHigh,
					"status":        status,
					"flow":          "lazada_excel",
				},
			})
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

var lazadaColCandidates = map[string][]string{
	"order_id":        {"orderNumber"},
	"order_item_id":   {"orderItemId"},
	"status":          {"status"},
	"seller_sku":      {"sellerSku"},
	"lazada_sku":      {"lazadaSku"},
	"order_date":      {"createTime"},
	"update_time":     {"updateTime"},
	"delivered_date":  {"deliveredDate"},
	"customer_name":   {"customerName", "shippingName"},
	"payment_channel": {"payMethod"},
	"tracking_no":     {"trackingCode"},
	"product_name":    {"itemName"},
	"option_name":     {"variation"},
	"paid_price":      {"paidPrice"},
	"unit_price":      {"unitPrice"},
	"shipping_amount": {"shippingFee"},
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
	required := []string{"order_id", "order_item_id", "status", "order_date", "product_name", "paid_price"}
	for _, f := range required {
		if _, ok := colIdx[f]; !ok {
			return nil, nil, 0, fmt.Errorf("ไม่พบ column '%s' ในไฟล์ Lazada — columns ที่พบ: %s",
				f, strings.Join(headerRow[:min(len(headerRow), 15)], ", "))
		}
	}

	warnings := []string{}
	orderMap := map[string]*ShopeeOrder{}
	itemMap := map[string]map[string]int{}
	orderKeys := []string{}
	noSKUOrderIDs := map[string]bool{}
	noSKUItemCount := 0
	skippedCount := 0
	skippedStatuses := map[string]int{}

	for _, row := range rows[headerRowIdx+1:] {
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
		price := optionalCellFloat(row, colIdx, "paid_price")
		if price <= 0 {
			price = optionalCellFloat(row, colIdx, "unit_price")
		}
		key := strings.Join([]string{sku, productName, optionName, fmt.Sprintf("%.4f", price)}, "\x1f")
		if idx, ok := itemMap[orderID][key]; ok {
			orderMap[orderID].Items[idx].Qty++
		} else {
			itemMap[orderID][key] = len(orderMap[orderID].Items)
			orderMap[orderID].Items = append(orderMap[orderID].Items, ShopeeExcelItem{
				SKU:         sku,
				LazadaSKU:   optionalCell(row, colIdx, "lazada_sku"),
				OrderItemID: optionalCell(row, colIdx, "order_item_id"),
				ProductName: productName,
				OptionName:  optionName,
				RawName:     rawName,
				Price:       price,
				Qty:         1,
				NoSKU:       noSKU,
			})
		}
		orderMap[orderID].LinePaidAmount += price
		if v := optionalCellFloat(row, colIdx, "shipping_amount"); v > 0 {
			orderMap[orderID].ShippingAmount += v
		}
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
			o.ItemGrossAmount += it.Price * it.Qty
		}
		o.MultiLine = len(o.Items) > 1
		o.PaidAmount = roundFloat(o.LinePaidAmount, 2)
		o.OrderTotalAmount = o.PaidAmount
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
	item, err := h.catalogRepo.GetOne(code)
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
