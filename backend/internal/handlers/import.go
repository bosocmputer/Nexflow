package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
	"go.uber.org/zap"

	"nexflow/internal/config"
	"nexflow/internal/middleware"
	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/anomaly"
	"nexflow/internal/services/mapper"
	"nexflow/internal/services/sml"
)

// ImportHandler handles Lazada/Shopee Excel imports (Phase 4)
type ImportHandler struct {
	platformRepo    *repository.PlatformMappingRepo
	mapperSvc       *mapper.Service
	anomalySvc      *anomaly.Service
	saleOrderClient *sml.SaleOrderClient
	billRepo        *repository.BillRepo
	channelDefaults *repository.ChannelDefaultRepo
	docCounters     *repository.DocCounterRepo
	cfg             *config.Config
	threshold       float64
	logger          *zap.Logger
}

func NewImportHandler(
	platformRepo *repository.PlatformMappingRepo,
	mapperSvc *mapper.Service,
	anomalySvc *anomaly.Service,
	saleOrderClient *sml.SaleOrderClient,
	billRepo *repository.BillRepo,
	channelDefaults *repository.ChannelDefaultRepo,
	docCounters *repository.DocCounterRepo,
	cfg *config.Config,
	threshold float64,
	logger *zap.Logger,
) *ImportHandler {
	return &ImportHandler{
		platformRepo:    platformRepo,
		mapperSvc:       mapperSvc,
		anomalySvc:      anomalySvc,
		saleOrderClient: saleOrderClient,
		billRepo:        billRepo,
		channelDefaults: channelDefaults,
		docCounters:     docCounters,
		cfg:             cfg,
		threshold:       threshold,
		logger:          logger,
	}
}

// ── Request / Response types ─────────────────────────────────────────────────

type importRowData struct {
	OrderID       string
	CustomerName  string
	CustomerPhone string
	ItemName      string
	SKU           string
	Qty           float64
	Price         float64
}

type BillPreview struct {
	BillID       string           `json:"bill_id"`
	OrderID      string           `json:"order_id"`
	CustomerName string           `json:"customer_name"`
	ItemCount    int              `json:"item_count"`
	MappedCount  int              `json:"mapped_count"`
	TotalAmount  float64          `json:"total_amount"`
	Anomalies    []models.Anomaly `json:"anomalies"`
	HasBlock     bool             `json:"has_block"`
}

// POST /api/import/upload
// multipart: file=<xlsx>, platform=<lazada|shopee>, bill_type=<sale|purchase>
func (h *ImportHandler) Upload(c *gin.Context) {
	platform := strings.ToLower(c.PostForm("platform"))
	billType := strings.ToLower(c.PostForm("bill_type"))
	if platform != "lazada" && platform != "shopee" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "platform must be lazada or shopee"})
		return
	}
	if billType != "sale" && billType != "purchase" {
		billType = "sale"
	}

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file required"})
		return
	}

	f, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot open file"})
		return
	}
	defer f.Close()

	xlsx, err := excelize.OpenReader(f)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid Excel file: " + err.Error()})
		return
	}
	defer xlsx.Close()

	// Load column mappings from DB (with defaults)
	colMappings, err := h.platformRepo.Get(platform)
	if err != nil {
		h.logger.Warn("load column mappings", zap.Error(err))
	}
	colMap := repository.ToColumnMap(colMappings)

	// Parse Excel rows
	rows, err := h.parseExcel(xlsx, colMap)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parse Excel failed: " + err.Error()})
		return
	}
	if len(rows) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no data rows found in file"})
		return
	}

	// Get current user for created_by
	claims := middleware.GetClaims(c)
	var createdBy *string
	if claims != nil {
		createdBy = &claims.UserID
	}

	// Group rows by order_id
	groups := h.groupByOrder(rows)

	// Process each order group
	var previews []BillPreview
	for orderID, orderRows := range groups {
		preview, err := h.processOrderGroup(orderID, orderRows, platform, billType, createdBy)
		if err != nil {
			h.logger.Warn("process order group", zap.String("order_id", orderID), zap.Error(err))
			continue
		}
		previews = append(previews, preview)
	}

	c.JSON(http.StatusOK, gin.H{
		"platform":  platform,
		"bill_type": billType,
		"total":     len(previews),
		"bills":     previews,
	})
}

// POST /api/import/confirm
// body: {"bill_ids": ["uuid", ...]}
func (h *ImportHandler) Confirm(c *gin.Context) {
	var req struct {
		BillIDs []string `json:"bill_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.BillIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bill_ids cannot be empty"})
		return
	}

	type errorEntry struct {
		BillID string `json:"bill_id"`
		Reason string `json:"reason"`
	}

	success := 0
	failed := 0
	var errors []errorEntry

	for _, billID := range req.BillIDs {
		bill, err := h.billRepo.FindByID(billID)
		if err != nil || bill == nil {
			failed++
			errors = append(errors, errorEntry{BillID: billID, Reason: "bill not found"})
			continue
		}

		// Skip non-pending bills
		if bill.Status != "pending" {
			continue
		}

		// Check for block anomalies
		var storedAnomalies []models.Anomaly
		if bill.Anomalies != nil {
			_ = json.Unmarshal(bill.Anomalies, &storedAnomalies)
		}
		hasBlock := false
		for _, a := range storedAnomalies {
			if a.Severity == "block" {
				hasBlock = true
				break
			}
		}
		if hasBlock {
			failed++
			errors = append(errors, errorEntry{BillID: billID, Reason: "บิลมี anomaly ระดับ block"})
			continue
		}

		// Build SML payload
		payload, docNo, err := h.buildImportPayload(bill)
		if err != nil {
			failed++
			errors = append(errors, errorEntry{BillID: billID, Reason: err.Error()})
			continue
		}

		// Stamp doc_no before calling SML so re-confirm reuses the same number
		_ = h.billRepo.UpdateStatus(billID, bill.Status, &docNo, nil, nil)

		// Send to SML
		_, soResp, err := h.saleOrderClient.CreateSaleOrder(payload, "")
		if err != nil || soResp == nil || !soResp.IsSuccess() {
			errMsg := ""
			if err != nil {
				errMsg = err.Error()
			} else {
				errMsg = soResp.Message
			}
			_ = h.billRepo.UpdateStatus(billID, "failed", nil, nil, &errMsg)
			failed++
			errors = append(errors, errorEntry{BillID: billID, Reason: errMsg})
			continue
		}

		// Mark sent
		sentDocNo := soResp.GetDocNo()
		if sentDocNo == "" {
			sentDocNo = docNo
		}
		resp, _ := json.Marshal(soResp)
		_ = h.billRepo.UpdateStatus(billID, "sent", &sentDocNo, resp, nil)
		success++
	}

	c.JSON(http.StatusOK, gin.H{
		"success": success,
		"failed":  failed,
		"errors":  errors,
	})
}

// ── Internal helpers ─────────────────────────────────────────────────────────

// parseExcel reads the first sheet, finds the header row, and extracts data rows.
func (h *ImportHandler) parseExcel(xlsx *excelize.File, colMap map[string]string) ([]importRowData, error) {
	sheets := xlsx.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("no sheets found")
	}
	sheet := sheets[0]

	rows, err := xlsx.GetRows(sheet)
	if err != nil {
		return nil, fmt.Errorf("get rows: %w", err)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("file has fewer than 2 rows")
	}

	// Reverse-map: column_name → field_name (case-insensitive)
	colNameToField := make(map[string]string, len(colMap))
	for field, colName := range colMap {
		colNameToField[strings.ToLower(strings.TrimSpace(colName))] = field
	}

	// Find header row (first 5 rows)
	headerIdx := -1
	var colIdxToField map[int]string
	maxHeader := 5
	if len(rows) < maxHeader {
		maxHeader = len(rows)
	}
	for i := 0; i < maxHeader; i++ {
		mapping := h.findHeaderRow(rows[i], colNameToField)
		if len(mapping) >= 2 {
			headerIdx = i
			colIdxToField = mapping
			break
		}
	}
	if headerIdx < 0 {
		return nil, fmt.Errorf("header row not found — check column mapping in settings")
	}

	var result []importRowData
	for i := headerIdx + 1; i < len(rows); i++ {
		row := rows[i]
		rd := h.extractRow(row, colIdxToField)
		if rd.ItemName == "" && rd.OrderID == "" {
			continue // skip empty rows
		}
		result = append(result, rd)
	}
	return result, nil
}

// findHeaderRow returns column_index → field_name for a candidate header row.
func (h *ImportHandler) findHeaderRow(row []string, colNameToField map[string]string) map[int]string {
	result := map[int]string{}
	for i, cell := range row {
		key := strings.ToLower(strings.TrimSpace(cell))
		if field, ok := colNameToField[key]; ok {
			result[i] = field
		}
	}
	return result
}

// extractRow converts a data row into importRowData.
func (h *ImportHandler) extractRow(row []string, colIdxToField map[int]string) importRowData {
	rd := importRowData{}
	for idx, field := range colIdxToField {
		if idx >= len(row) {
			continue
		}
		val := strings.TrimSpace(row[idx])
		switch field {
		case "order_id":
			rd.OrderID = val
		case "buyer_name":
			rd.CustomerName = val
		case "buyer_phone":
			rd.CustomerPhone = val
		case "item_name":
			rd.ItemName = val
		case "sku":
			rd.SKU = val
		case "qty":
			rd.Qty, _ = strconv.ParseFloat(val, 64)
		case "price":
			clean := strings.ReplaceAll(val, ",", "")
			rd.Price, _ = strconv.ParseFloat(clean, 64)
		}
	}
	return rd
}

// groupByOrder groups rows by order_id (ungrouped rows get unique keys).
func (h *ImportHandler) groupByOrder(rows []importRowData) map[string][]importRowData {
	groups := map[string][]importRowData{}
	ungrouped := 0
	for _, r := range rows {
		key := r.OrderID
		if key == "" {
			key = fmt.Sprintf("__row_%d", ungrouped)
			ungrouped++
		}
		groups[key] = append(groups[key], r)
	}
	return groups
}

// processOrderGroup creates a pending bill with F1+F2 for one order group.
func (h *ImportHandler) processOrderGroup(
	orderID string,
	rows []importRowData,
	platform, billType string,
	createdBy *string,
) (BillPreview, error) {
	customerName := rows[0].CustomerName
	if customerName == "" {
		customerName = "ไม่ระบุชื่อลูกค้า"
	}
	customerPhone := rows[0].CustomerPhone

	type rawItem struct {
		RawName string  `json:"raw_name"`
		SKU     string  `json:"sku,omitempty"`
		Qty     float64 `json:"qty"`
		Price   float64 `json:"price"`
	}
	type rawBill struct {
		Source        string    `json:"source"`
		OrderID       string    `json:"order_id"`
		CustomerName  string    `json:"customer_name"`
		CustomerPhone string    `json:"customer_phone,omitempty"`
		Items         []rawItem `json:"items"`
	}
	rawItems := make([]rawItem, 0, len(rows))
	for _, r := range rows {
		rawItems = append(rawItems, rawItem{RawName: r.ItemName, SKU: r.SKU, Qty: r.Qty, Price: r.Price})
	}
	rawData, _ := json.Marshal(rawBill{
		Source:        platform,
		OrderID:       orderID,
		CustomerName:  customerName,
		CustomerPhone: customerPhone,
		Items:         rawItems,
	})

	conf := h.threshold
	bill := &models.Bill{
		BillType:     billType,
		Source:       platform,
		AIConfidence: &conf,
		RawData:      rawData,
		CreatedBy:    createdBy,
	}
	if err := h.billRepo.Create(bill); err != nil {
		return BillPreview{}, fmt.Errorf("create bill: %w", err)
	}

	// F1: map each item
	var billItems []models.BillItem
	mappedCount := 0
	var itemCodes []string

	for _, r := range rows {
		if r.ItemName == "" {
			continue
		}
		match := h.mapperSvc.Match(r.ItemName)
		item := models.BillItem{BillID: bill.ID, RawName: r.ItemName, Qty: r.Qty}
		if r.Price > 0 {
			p := r.Price
			item.Price = &p
		}
		if !match.Unmapped && match.Mapping != nil {
			item.ItemCode = &match.Mapping.ItemCode
			item.UnitCode = &match.Mapping.UnitCode
			item.Mapped = true
			item.MappingID = &match.Mapping.ID
			mappedCount++
			itemCodes = append(itemCodes, match.Mapping.ItemCode)
		}
		_ = h.billRepo.InsertItem(&item)
		billItems = append(billItems, item)
	}

	// F2: anomaly check
	avgPrices, maxQtys, _ := h.billRepo.GetPriceHistories(itemCodes)
	knownItems := map[string]bool{}
	for _, code := range itemCodes {
		knownItems[code] = true
	}
	anomalies := h.anomalySvc.Check(anomaly.CheckInput{
		Items:        billItems,
		CustomerName: customerName,
		AvgPrices:    avgPrices,
		MaxQtys:      maxQtys,
		KnownItems:   knownItems,
	})
	_ = h.billRepo.UpdateAnomalies(bill.ID, anomalies)

	totalAmount := 0.0
	for _, r := range rows {
		totalAmount += r.Qty * r.Price
	}
	hasBlock := false
	for _, a := range anomalies {
		if a.Severity == "block" {
			hasBlock = true
			break
		}
	}

	return BillPreview{
		BillID:       bill.ID,
		OrderID:      orderID,
		CustomerName: customerName,
		ItemCount:    len(billItems),
		MappedCount:  mappedCount,
		TotalAmount:  totalAmount,
		Anomalies:    anomalies,
		HasBlock:     hasBlock,
	}, nil
}

// buildImportPayload assembles a SaleOrderPayload from a bill for the import confirm flow.
func (h *ImportHandler) buildImportPayload(bill *models.Bill) (sml.SaleOrderPayload, string, error) {
	if len(bill.Items) == 0 {
		return sml.SaleOrderPayload{}, "", fmt.Errorf("bill has no items")
	}

	def, _ := h.channelDefaults.Get(bill.Source, "sale")
	if def == nil || def.PartyCode == "" {
		return sml.SaleOrderPayload{}, "", fmt.Errorf("ยังไม่ได้ตั้งค่าลูกค้า default สำหรับ %s/sale — ไปที่ /settings/channels", bill.Source)
	}

	cfg := sml.SaleOrderConfig{
		BaseURL:    h.cfg.ShopeeSMLURL,
		GUID:       h.cfg.ShopeeSMLGUID,
		Provider:   h.cfg.ShopeeSMLProvider,
		ConfigFile: h.cfg.ShopeeSMLConfigFile,
		Database:   h.cfg.ShopeeSMLDatabase,
		DocFormat:  h.cfg.ShopeeSMLDocFormat,
		SaleCode:   h.cfg.ShopeeSMLSaleCode,
		BranchCode: h.cfg.ShopeeSMLBranchCode,
		WHCode:     h.cfg.ShopeeSMLWHCode,
		ShelfCode:  h.cfg.ShopeeSMLShelfCode,
		UnitCode:   h.cfg.ShopeeSMLUnitCode,
		VATType:    h.cfg.ShopeeSMLVATType,
		VATRate:    h.cfg.ShopeeSMLVATRate,
		DocTime:    h.cfg.ShopeeSMLDocTime,
		CustCode:   def.PartyCode,
	}
	applyDocumentOverrides(def, &cfg.BranchCode, &cfg.SaleCode, &cfg.UnitCode, &cfg.DocTime)
	applyChannelOverrides(def, &cfg.WHCode, &cfg.ShelfCode, &cfg.VATType, &cfg.VATRate)
	if def.DocFormatCode != "" {
		cfg.DocFormat = def.DocFormatCode
	}

	var items []sml.SOItem
	for _, item := range bill.Items {
		if item.ItemCode == nil || *item.ItemCode == "" {
			return sml.SaleOrderPayload{}, "", fmt.Errorf("item '%s' ยังไม่ได้ mapping", item.RawName)
		}
		price := 0.0
		if item.Price != nil {
			price = *item.Price
		}
		unit := cfg.UnitCode
		if item.UnitCode != nil && *item.UnitCode != "" {
			unit = *item.UnitCode
		}
		items = append(items, sml.SOItem{
			ItemCode: *item.ItemCode,
			ItemName: item.RawName,
			Qty:      item.Qty,
			Price:    price,
			UnitCode: unit,
		})
	}

	prefix, format := resolveDocCounterPattern(def, "NX-SO")
	docNo, err := h.docCounters.GenerateDocNo(prefix, format, bill.CreatedAt)
	if err != nil {
		return sml.SaleOrderPayload{}, "", fmt.Errorf("สร้างเลขเอกสารไม่ได้: %w", err)
	}

	docDate := bill.CreatedAt.Format("2006-01-02")
	payload := sml.BuildSaleOrderPayload(docNo, docDate, "", "", items, cfg, "")
	return payload, docNo, nil
}
