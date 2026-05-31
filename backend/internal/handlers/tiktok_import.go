package handlers

import (
	"bytes"
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
	"github.com/xuri/excelize/v2"
	"go.uber.org/zap"

	"nexflow/internal/config"
	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/artifact"
	"nexflow/internal/services/catalog"
)

// TikTokImportHandler mirrors the Shopee Excel import flow: preview first,
// then create local sale bills for manual review and SML retry.
type TikTokImportHandler struct {
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

func NewTikTokImportHandler(
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
) *TikTokImportHandler {
	h := &TikTokImportHandler{
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

func (h *TikTokImportHandler) SetArtifactService(svc *artifact.Service) {
	h.artifactSvc = svc
}

func (h *TikTokImportHandler) gcPendingUploads() {
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

func (h *TikTokImportHandler) GetConfig(c *gin.Context) {
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
		if billID, exists, _ := h.findTikTokOrderBillID(orders[i].OrderID); exists {
			orders[i].Duplicate = true
			orders[i].ExistingBillID = billID
			dupCount++
		}
	}
	preflight := buildShopeePreflight(orders, skippedCount, dupCount)
	importRunID := h.createTikTokImportRun(c, fileHeader.Filename, fileToken, orders, warnings, preflight)

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
		FileToken:      fileToken,
	})
}

func (h *TikTokImportHandler) Confirm(c *gin.Context) {
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
		if billID, exists, _ := h.findTikTokOrderBillID(order.OrderID); exists {
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
						h.logger.Warn("tiktok_excel: lookup mapping failed",
							zap.String("raw_name", rawName), zap.Error(err))
					}
				}
				if resolved.learned == nil && h.embSvc != nil && h.embSvc.IsConfigured() && h.catalogIdx != nil && h.catalogIdx.Size() > 0 {
					if emb, err := h.embSvc.EmbedText(rawName); err == nil {
						resolved.matches = h.catalogIdx.Search(emb, topK)
					} else {
						h.logger.Warn("tiktok_excel: embedding lookup failed",
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
			"flow":               "tiktok_excel",
			"tiktok_order_id":    order.OrderID,
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
			Source:        "tiktok",
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
				billID, _, _ := h.findTikTokOrderBillID(order.OrderID)
				results = append(results, ConfirmResult{
					OrderID: order.OrderID,
					Success: false,
					BillID:  billID,
					Message: "order นี้ถูกสร้างไปแล้วระหว่างนำเข้า (ข้าม)",
				})
				continue
			}
			h.logger.Error("tiktok_excel: create bill", zap.String("order_id", order.OrderID), zap.Error(err))
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
				filename = "tiktok-import.xlsx"
			}
			artifactKind := "xlsx"
			contentType := "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
			if strings.HasSuffix(strings.ToLower(filename), ".csv") {
				artifactKind = "csv"
				contentType = "text/csv; charset=utf-8"
			}
			if _, err := h.artifactSvc.Save(
				bill.ID,
				artifactKind,
				filename,
				contentType,
				uploadBytes,
				meta,
			); err != nil {
				h.logger.Warn("tiktok_excel: save artifact failed", zap.String("bill_id", bill.ID), zap.Error(err))
			}
		}
		if h.auditRepo != nil {
			billIDStr := bill.ID
			durMs := int(time.Since(confirmStart).Milliseconds())
			_ = h.auditRepo.Log(models.AuditEntry{
				Action:     "bill_created",
				TargetID:   &billIDStr,
				UserID:     userID,
				Source:     "tiktok",
				Level:      "info",
				TraceID:    traceID,
				DurationMs: &durMs,
				Detail: map[string]interface{}{
					"order_id":      order.OrderID,
					"items_count":   len(enriched),
					"all_high_conf": allHigh,
					"status":        status,
					"flow":          "tiktok_excel",
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
			Action:     "tiktok_import_done",
			UserID:     userID,
			Source:     "tiktok",
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
	h.finishTikTokImportRun(req.ImportRunID, successCount, len(results)-successCount, "confirmed")
	c.JSON(http.StatusOK, gin.H{
		"results":       results,
		"success_count": successCount,
		"fail_count":    len(results) - successCount,
		"total":         len(results),
		"message":       destinationName + "ถูกสร้างแล้ว — กรุณาเข้าไปตรวจสอบและกดยืนยันส่งใน " + reviewPath,
	})
}

var tiktokColCandidates = map[string][]string{
	"order_id":        {"Order ID"},
	"order_item_id":   {"SKU ID"},
	"status":          {"Order Status"},
	"substatus":       {"Order Substatus"},
	"cancel_type":     {"Cancelation/Return Type"},
	"seller_sku":      {"Seller SKU", "SKU ID"},
	"tiktok_sku":      {"SKU ID"},
	"order_date":      {"Created Time"},
	"payment_time":    {"Paid Time"},
	"delivered_date":  {"Delivered Time"},
	"customer_name":   {"Recipient", "Buyer Username"},
	"payment_channel": {"Payment Method"},
	"tracking_no":     {"Tracking ID"},
	"product_name":    {"Product Name"},
	"option_name":     {"Variation"},
	"qty":             {"Quantity"},
	"paid_price":      {"SKU Subtotal After Discount"},
	"unit_price":      {"SKU Unit Original Price"},
	"order_amount":    {"Order Amount"},
	"shipping_amount": {"Shipping Fee After Discount"},
}

var tiktokAllowedStatuses = map[string]bool{
	"จัดส่งแล้ว": true,
	"shipped":    true,
	"delivered":  true,
	"completed":  true,
}

func parseTikTokExcel(src interface{ Read([]byte) (int, error) }) ([]ShopeeOrder, []string, int, error) {
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
	return parseTikTokRows(rows)
}

func parseTikTokCSV(src io.Reader) ([]ShopeeOrder, []string, int, error) {
	r := csv.NewReader(src)
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	rows, err := r.ReadAll()
	if err != nil {
		return nil, nil, 0, fmt.Errorf("อ่านไฟล์ CSV ไม่ได้: %w", err)
	}
	if len(rows) < 2 {
		return nil, nil, 0, fmt.Errorf("ไฟล์ว่างหรือไม่มีข้อมูล")
	}
	return parseTikTokRows(rows)
}

func parseTikTokRows(rows [][]string) ([]ShopeeOrder, []string, int, error) {
	headerRowIdx := -1
	for i, row := range rows {
		for _, cell := range row {
			if strings.EqualFold(cleanTikTokCell(cell), "Order ID") {
				headerRowIdx = i
				goto foundHeader
			}
		}
	}
foundHeader:
	if headerRowIdx < 0 {
		return nil, nil, 0, fmt.Errorf("ไม่พบ header 'Order ID' ในไฟล์ TikTok")
	}
	headerRow := rows[headerRowIdx]
	colIdx := map[string]int{}
	for field, candidates := range tiktokColCandidates {
		for j, cell := range headerRow {
			trimmed := cleanTikTokCell(cell)
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
	required := []string{"order_id", "status", "order_date", "product_name", "paid_price", "qty"}
	for _, f := range required {
		if _, ok := colIdx[f]; !ok {
			return nil, nil, 0, fmt.Errorf("ไม่พบ column '%s' ในไฟล์ TikTok — columns ที่พบ: %s",
				f, strings.Join(cleanTikTokRow(headerRow[:min(len(headerRow), 15)]), ", "))
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

	for rowIdx, row := range rows[headerRowIdx+1:] {
		if len(row) == 0 {
			continue
		}
		orderID := tikTokCell(row, colIdx, "order_id")
		if orderID == "" {
			continue
		}
		status := tikTokCell(row, colIdx, "status")
		if !tiktokAllowedStatuses[strings.ToLower(status)] && !tiktokAllowedStatuses[status] {
			skippedCount++
			if status == "" {
				status = "(ว่าง)"
			}
			skippedStatuses[status]++
			continue
		}
		orderDateTime := tikTokCell(row, colIdx, "order_date")
		docDate := tiktokDocDate(orderDateTime)
		if _, exists := orderMap[orderID]; !exists {
			orderMap[orderID] = &ShopeeOrder{
				OrderID:        orderID,
				DocDate:        docDate,
				OrderDateTime:  orderDateTime,
				PaymentTime:    tikTokCell(row, colIdx, "payment_time"),
				PaymentChannel: tikTokCell(row, colIdx, "payment_channel"),
				BuyerUsername:  tikTokCell(row, colIdx, "customer_name"),
				TrackingNo:     tikTokCell(row, colIdx, "tracking_no"),
				Status:         status,
				Items:          []ShopeeExcelItem{},
			}
			itemMap[orderID] = map[string]int{}
			orderKeys = append(orderKeys, orderID)
		}
		productName := tikTokCell(row, colIdx, "product_name")
		optionName := tikTokCell(row, colIdx, "option_name")
		rawName := shopeeItemRawName(productName, optionName, "")
		tiktokSKU := tikTokCell(row, colIdx, "tiktok_sku")
		sku := firstNonEmpty(tikTokCell(row, colIdx, "seller_sku"), tiktokSKU)
		noSKU := sku == ""
		if noSKU {
			noSKUOrderIDs[orderID] = true
			noSKUItemCount++
			orderMap[orderID].HasNoSKU = true
			orderMap[orderID].NoSKUItemCount++
		}
		qty := tikTokFloat(row, colIdx, "qty")
		if qty <= 0 {
			qty = 1
		}
		lineSubtotal := tikTokFloat(row, colIdx, "paid_price")
		price := 0.0
		if lineSubtotal > 0 {
			price = lineSubtotal / qty
		}
		if price <= 0 {
			price = tikTokFloat(row, colIdx, "unit_price")
		}
		if price <= 0 {
			price = tikTokFloat(row, colIdx, "order_amount") / qty
		}
		key := strings.Join([]string{sku, productName, optionName, fmt.Sprintf("%.4f", price)}, "\x1f")
		if idx, ok := itemMap[orderID][key]; ok {
			orderMap[orderID].Items[idx].Qty += qty
		} else {
			itemMap[orderID][key] = len(orderMap[orderID].Items)
			orderMap[orderID].Items = append(orderMap[orderID].Items, ShopeeExcelItem{
				SKU:         sku,
				TikTokSKU:   tiktokSKU,
				OrderItemID: firstNonEmpty(tikTokCell(row, colIdx, "order_item_id"), fmt.Sprintf("%s-%d", orderID, rowIdx+1)),
				ProductName: productName,
				OptionName:  optionName,
				RawName:     rawName,
				Price:       price,
				Qty:         qty,
				NoSKU:       noSKU,
			})
		}
		orderMap[orderID].LinePaidAmount += lineSubtotal
		orderAmount := tikTokFloat(row, colIdx, "order_amount")
		if orderAmount > orderMap[orderID].PaidAmount {
			orderMap[orderID].PaidAmount = orderAmount
			orderMap[orderID].OrderTotalAmount = orderAmount
		}
		if v := tikTokFloat(row, colIdx, "shipping_amount"); v > orderMap[orderID].ShippingAmount {
			orderMap[orderID].ShippingAmount = v
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
		o.LinePaidAmount = roundFloat(o.LinePaidAmount, 2)
		if o.PaidAmount <= 0 {
			o.PaidAmount = o.LinePaidAmount
			o.OrderTotalAmount = o.PaidAmount
		}
		o.PaidAmount = roundFloat(o.PaidAmount, 2)
		o.OrderTotalAmount = roundFloat(o.OrderTotalAmount, 2)
		o.ShippingAmount = roundFloat(o.ShippingAmount, 2)
		o.DiscountAmount = roundFloat(o.ItemGrossAmount+o.ShippingAmount-o.PaidAmount, 2)
		orders = append(orders, *o)
	}
	if noSKUItemCount > 0 {
		warnings = append(warnings, fmt.Sprintf("พบ %d รายการสินค้าใน %d order ที่ไม่มี Seller SKU / SKU ID — ระบบจะใช้ชื่อสินค้า + variation จับคู่แทน", noSKUItemCount, len(noSKUOrderIDs)))
	}
	if skippedCount > 0 {
		parts := make([]string, 0, len(skippedStatuses))
		for status, n := range skippedStatuses {
			parts = append(parts, fmt.Sprintf("%s %d", status, n))
		}
		sort.Strings(parts)
		warnings = append([]string{fmt.Sprintf("กรอง %d แถวเพราะสถานะไม่ใช่ จัดส่งแล้ว/shipped/delivered (%s)", skippedCount, strings.Join(parts, ", "))}, warnings...)
	}
	return orders, warnings, skippedCount, nil
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
	if len(raw) >= 10 {
		return raw[:10]
	}
	return time.Now().Format("2006-01-02")
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
	item, err := h.catalogRepo.GetOne(code)
	if err != nil {
		h.logger.Warn("tiktok_excel: catalog sku lookup failed", zap.String("sku", code), zap.Error(err))
		return nil
	}
	return item
}
