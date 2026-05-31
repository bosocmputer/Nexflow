package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/ai"
	"nexflow/internal/services/anomaly"
	"nexflow/internal/services/artifact"
	"nexflow/internal/services/catalog"
	emailservice "nexflow/internal/services/email"
	lineservice "nexflow/internal/services/line"
	"nexflow/internal/services/mapper"
	"nexflow/internal/services/mistral"
)

// Date extraction patterns for Shopee email bodies. Priority order matches
// what's most semantic for SML's doc_date field.
var docDatePatterns = []*regexp.Regexp{
	regexp.MustCompile(`ถูกจัดส่งแล้วเมื่อวันที่[\s:	]*(\d{1,2})/(\d{1,2})/(\d{4})`),
	regexp.MustCompile(`วันที่สั่งซื้อ[\s:	]*(\d{1,2})/(\d{1,2})/(\d{4})`),
}

// shopeePricePattern matches "ราคา: ฿NUMBER" (per-item line in Shopee emails).
// We use this as a fallback when the AI extract returns nil prices —
// observed when Gemini sees the ฿ glyph and outputs the price as a string
// instead of a number, then drops it.
//
// Carefully NOT matching "รวม*ราคา" / "ยอด*" so we don't slurp the order
// total or shipping fee. The (?:...) limits it to the line-level "ราคา"
// label that appears right after qty in each item block.
var shopeePricePattern = regexp.MustCompile(`(?:^|\s)ราคา\s*[:：]\s*฿\s*([\d,]+(?:\.\d+)?)`)
var imgSrcPattern = regexp.MustCompile(`(?i)<img[^>]+src=["']([^"']+)["']`)

// extractShopeePrices returns the per-item prices from a Shopee email body
// in the order they appear in the email. Used as a fallback when the AI
// extract drops a Price field.
func extractShopeePrices(body string) []float64 {
	matches := shopeePricePattern.FindAllStringSubmatch(body, -1)
	out := make([]float64, 0, len(matches))
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		clean := strings.ReplaceAll(m[1], ",", "")
		if v, err := strconv.ParseFloat(clean, 64); err == nil {
			out = append(out, v)
		}
	}
	return out
}

func extractShopeeImageURLs(html string) []string {
	if strings.TrimSpace(html) == "" {
		return nil
	}
	seen := map[string]bool{}
	primary := []string{}
	fallback := []string{}
	out := []string{}
	for _, m := range imgSrcPattern.FindAllStringSubmatch(html, -1) {
		if len(m) < 2 {
			continue
		}
		u := strings.TrimSpace(m[1])
		if u == "" || seen[u] || strings.HasPrefix(strings.ToLower(u), "data:") {
			continue
		}
		lower := strings.ToLower(u)
		if !(strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")) {
			continue
		}
		if strings.Contains(lower, "tracking.") ||
			strings.Contains(lower, "/tracking/") ||
			strings.Contains(lower, "/open/") {
			continue
		}
		// Product images in Thai Shopee emails usually use the local CDN and
		// a "th-" file key. Global Shopee assets are often logos/app/social
		// icons, so keep them only as a fallback.
		isProductLike := strings.Contains(lower, "cf.shopee.co.th/file/") ||
			strings.Contains(lower, "f.shopee.co.th/file/") ||
			strings.Contains(lower, "/file/th-")
		isShopeeAsset := strings.Contains(lower, "shopee") && strings.Contains(lower, "/file/")
		if !isProductLike && !isShopeeAsset {
			continue
		}
		seen[u] = true
		if isProductLike {
			primary = append(primary, u)
		} else {
			fallback = append(fallback, u)
		}
	}
	out = append(out, primary...)
	out = append(out, fallback...)
	return out
}

// EmailHandler processes email attachments through the AI pipeline
// It is NOT an HTTP handler — it implements emailservice.AttachmentProcessor
type EmailHandler struct {
	aiClient   *ai.Client
	ocrClient  *mistral.OCRClient
	mapperSvc  *mapper.Service
	anomalySvc *anomaly.Service
	billRepo   *repository.BillRepo
	auditRepo  *repository.AuditLogRepo
	lineSvc    *lineservice.Service
	threshold  float64
	logger     *zap.Logger
	// Catalog-based matching (Shopee email flow)
	catalogSvc      *catalog.SMLCatalogService
	embSvc          *catalog.EmbeddingService
	catalogIdx      *catalog.CatalogIndex
	catalogRepo     *repository.SMLCatalogRepo
	channelDefaults *repository.ChannelDefaultRepo
	// Source-artifact storage (PDF/HTML/envelope)
	artifactSvc *artifact.Service
}

func NewEmailHandler(
	aiClient *ai.Client,
	ocrClient *mistral.OCRClient,
	mapperSvc *mapper.Service,
	anomalySvc *anomaly.Service,
	billRepo *repository.BillRepo,
	auditRepo *repository.AuditLogRepo,
	lineSvc *lineservice.Service,
	threshold float64,
	logger *zap.Logger,
) *EmailHandler {
	return &EmailHandler{
		aiClient:   aiClient,
		ocrClient:  ocrClient,
		mapperSvc:  mapperSvc,
		anomalySvc: anomalySvc,
		billRepo:   billRepo,
		auditRepo:  auditRepo,
		lineSvc:    lineSvc,
		threshold:  threshold,
		logger:     logger,
	}
}

// SetCatalogServices wires catalog-based search for Shopee email flow
func (h *EmailHandler) SetCatalogServices(
	catalogSvc *catalog.SMLCatalogService,
	embSvc *catalog.EmbeddingService,
	catalogIdx *catalog.CatalogIndex,
	catalogRepo *repository.SMLCatalogRepo,
) {
	h.catalogSvc = catalogSvc
	h.embSvc = embSvc
	h.catalogIdx = catalogIdx
	h.catalogRepo = catalogRepo
}

// SetChannelDefaults wires per-channel routing/config used by email flows.
func (h *EmailHandler) SetChannelDefaults(repo *repository.ChannelDefaultRepo) {
	h.channelDefaults = repo
}

// SetArtifactService wires source-artifact storage for evidence/audit trails.
func (h *EmailHandler) SetArtifactService(svc *artifact.Service) {
	h.artifactSvc = svc
}

// saveEmailArtifacts persists the original source files (binary + envelope JSON)
// to bill_artifacts. Best-effort: failures are logged but don't break ingestion.
func (h *EmailHandler) saveEmailArtifacts(
	billID, kind, filename, contentType string,
	data []byte,
	subject, fromAddr, messageID string,
) {
	if h.artifactSvc == nil || billID == "" {
		return
	}
	// Inline-attached PDFs from Gmail often arrive with no MIME filename.
	// Fall back to a sensible default per content-type so the download
	// button has a meaningful name.
	if filename == "" {
		switch {
		case contentType == "application/pdf":
			filename = "attachment.pdf"
		case strings.HasPrefix(contentType, "image/"):
			filename = "attachment." + strings.TrimPrefix(contentType, "image/")
		case strings.HasPrefix(contentType, "text/html"):
			filename = "body.html"
		default:
			filename = "attachment.bin"
		}
	}
	envelope := map[string]interface{}{
		"subject":    subject,
		"from":       fromAddr,
		"message_id": messageID,
	}
	// 1. The binary itself (PDF / HTML / etc.)
	if len(data) > 0 {
		if _, err := h.artifactSvc.Save(billID, kind, filename, contentType, data, envelope); err != nil {
			h.logger.Warn("artifact: save body failed",
				zap.String("bill_id", billID), zap.String("kind", kind), zap.Error(err))
		}
	}
	// 2. Envelope JSON for quick reference (independent from #1 so it shows
	//    even when the body file is too big and gets rejected by the size cap).
	envelopeJSON, _ := json.Marshal(envelope)
	if _, err := h.artifactSvc.Save(billID, "email_envelope", "envelope.json", "application/json", envelopeJSON, nil); err != nil {
		h.logger.Warn("artifact: save envelope failed",
			zap.String("bill_id", billID), zap.Error(err))
	}
}

func applyMailSource(raw map[string]interface{}, source emailservice.MailSource) {
	if source.AccountID != "" {
		raw["imap_account_id"] = source.AccountID
	}
	if source.AccountName != "" {
		raw["imap_account_name"] = source.AccountName
	}
	if source.Username != "" {
		raw["imap_username"] = source.Username
	}
	if source.Mailbox != "" {
		raw["imap_mailbox"] = source.Mailbox
	}
	if source.EmailDate != "" {
		raw["email_date"] = source.EmailDate
	}
}

// ProcessAttachment is called once per qualifying email attachment.
// It satisfies emailservice.AttachmentProcessor signature.
func (h *EmailHandler) ProcessAttachment(data []byte, mimeType, filename, messageID, subject, fromAddr string, source emailservice.MailSource) error {
	// Use message-id as trace_id so all events for the same email are correlated.
	traceID := messageID
	if traceID == "" {
		traceID = fmt.Sprintf("email-%d", time.Now().UnixMilli())
	}
	attachStart := time.Now()

	// Deduplication: skip if we've already processed this email
	if messageID != "" {
		exists, err := h.billRepo.FindByEmailMessageID(messageID)
		if err != nil {
			h.logger.Warn("email: dedup check failed", zap.String("message_id", messageID), zap.Error(err))
		} else if exists {
			h.logger.Info("email: skipping duplicate", zap.String("message_id", messageID))
			return emailservice.SkipMessage("duplicate", "เมลนี้เคยสร้างบิลแล้ว")
		}
	}
	var extracted *ai.ExtractedBill
	var err error

	b64 := base64.StdEncoding.EncodeToString(data)

	switch {
	case strings.HasPrefix(mimeType, "image/"):
		extracted, err = h.aiClient.ExtractImage(b64, mimeType)
	case mimeType == "application/pdf" || strings.HasSuffix(strings.ToLower(filename), ".pdf"):
		if h.ocrClient != nil && h.ocrClient.IsConfigured() {
			// Use Mistral OCR to extract markdown text, then parse with OpenRouter
			var ocrText string
			ocrText, err = h.ocrClient.ExtractTextFromPDF(b64)
			if err == nil {
				extracted, err = h.aiClient.ExtractText(ocrText)
			}
		} else {
			extracted, err = h.aiClient.ExtractPDF(b64)
		}
	default:
		return fmt.Errorf("unsupported attachment type: %s", mimeType)
	}

	if err != nil {
		h.logger.Error("email: AI extract failed",
			zap.String("mime", mimeType), zap.String("file", filename), zap.Error(err))
		h.adminNotify(fmt.Sprintf("⚠️ Email AI extract failed\nFile: %s\nError: %s", filename, err.Error()))
		return err
	}

	if extracted == nil || len(extracted.Items) == 0 {
		h.logger.Warn("email: no items extracted", zap.String("file", filename))
		return fmt.Errorf("no items extracted from %s", filename)
	}

	return h.handleExtracted(extracted, filename, mimeType, messageID, subject, fromAddr, traceID, data, attachStart, source)
}

// handleExtracted resolves item codes via F1 (exact mappings) → catalog
// embedding search → text fallback, in that priority order. Each item
// stores its top-5 catalog candidates so the user can review/override
// in MapItemModal. The bill stays pending; SML send happens later via
// the Retry handler.
//
// Why not fuzzy mapper.Match? With a small mappings table (often a
// single user-added entry), Levenshtein on UTF-8 byte distances of
// Thai text creates spurious 0.6–0.8 matches between completely
// unrelated products. The 3000-item embedded catalog is the reliable
// matcher; the F1 mapping table is reserved for explicit user-confirmed
// (raw_name → item_code) memory.
func (h *EmailHandler) handleExtracted(extracted *ai.ExtractedBill, filename, mimeType, messageID, subject, fromAddr, traceID string, sourceBytes []byte, startTime time.Time, source emailservice.MailSource) error {
	const topK = 5
	const highConfThreshold = 0.85

	type itemWithCandidates struct {
		item       models.BillItem
		candidates []models.CatalogMatch
	}
	var enriched []itemWithCandidates
	var itemCodes []string

	for _, extItem := range extracted.Items {
		// 1. F1 exact match — only exact (Score=1.0). Skip fuzzy entirely.
		match := h.mapperSvc.Match(extItem.RawName)
		exactMapping := match.Mapping != nil && !match.NeedsReview && match.Score >= 1.0

		// 2. Catalog embedding search (always run, regardless of F1 hit)
		var matches []models.CatalogMatch
		if h.embSvc != nil && h.embSvc.IsConfigured() && h.catalogIdx != nil && h.catalogIdx.Size() > 0 {
			if queryEmb, err := h.embSvc.EmbedText(extItem.RawName); err == nil {
				matches = h.catalogIdx.Search(queryEmb, topK)
			}
		}
		if len(matches) == 0 && h.catalogSvc != nil {
			matches, _ = h.catalogSvc.SearchByText(extItem.RawName, topK)
		}

		item := models.BillItem{
			RawName: extItem.RawName,
			Qty:     extItem.Qty,
		}
		if extItem.Price != nil {
			item.Price = extItem.Price
		}

		// 3. Decide what to pre-fill, in priority order:
		//    (a) F1 exact mapping → user-confirmed memory wins
		//    (b) Catalog top with score ≥ 0.85 → auto-pick
		//    (c) Catalog top with low score → pre-fill but mapped=false
		//    (d) Nothing → leave blank, user must search/create
		switch {
		case exactMapping:
			item.ItemCode = &match.Mapping.ItemCode
			item.UnitCode = &match.Mapping.UnitCode
			item.MappingID = &match.Mapping.ID
			item.Mapped = true
			itemCodes = append(itemCodes, match.Mapping.ItemCode)
		case len(matches) > 0 && matches[0].Score >= highConfThreshold:
			item.ItemCode = &matches[0].ItemCode
			item.UnitCode = &matches[0].UnitCode
			item.Mapped = true
			itemCodes = append(itemCodes, matches[0].ItemCode)
		case len(matches) > 0:
			// Pre-fill best guess; user reviews via MapItemModal.
			item.ItemCode = &matches[0].ItemCode
			item.UnitCode = &matches[0].UnitCode
			item.Mapped = false
		}

		enriched = append(enriched, itemWithCandidates{item: item, candidates: matches})
	}

	billItems := make([]models.BillItem, 0, len(enriched))
	for _, e := range enriched {
		billItems = append(billItems, e.item)
	}

	// F2 Anomaly detection
	avgPrices, maxQtys, _ := h.billRepo.GetPriceHistories(itemCodes)
	checkItems := make([]models.BillItem, len(billItems))
	copy(checkItems, billItems)
	anomalies := h.anomalySvc.Check(anomaly.CheckInput{
		Items:     checkItems,
		AvgPrices: avgPrices,
		MaxQtys:   maxQtys,
	})

	// Save bill — keep raw_data shape consistent with the Shopee email flows
	// (subject, from, flow, doc_date, etc.) so the BillDetail JSON viewer
	// shows the same context regardless of source.
	conf := extracted.Confidence
	rawDataMap := map[string]interface{}{
		"flow":             "email_pdf",
		"subject":          subject,
		"from":             fromAddr,
		"customer_name":    extracted.CustomerName,
		"customer_phone":   extracted.CustomerPhone,
		"note":             extracted.Note,
		"email_file":       filename,
		"email_message_id": messageID,
	}
	applyMailSource(rawDataMap, source)
	rawDataBytes, _ := json.Marshal(rawDataMap)
	billType := "purchase"
	if extracted.DocType == "sale" || extracted.DocType == "purchase" {
		billType = extracted.DocType
	}
	bill := &models.Bill{
		BillType:     billType,
		Source:       "email",
		AIConfidence: &conf,
		RawData:      json.RawMessage(rawDataBytes),
	}
	if err := h.billRepo.Create(bill); err != nil {
		return fmt.Errorf("create bill: %w", err)
	}

	// Save the original PDF + email envelope as a downloadable artifact.
	// kind="email_pdf" tells the UI to render a "ดู PDF" button (inline).
	h.saveEmailArtifacts(bill.ID, "email_pdf", filename, mimeType, sourceBytes, subject, fromAddr, messageID)

	// Audit: bill received from email
	if h.auditRepo != nil {
		billIDStr := bill.ID
		durMs := int(time.Since(startTime).Milliseconds())
		_ = h.auditRepo.Log(models.AuditEntry{
			Action:     "bill_created",
			TargetID:   &billIDStr,
			Source:     "email",
			Level:      "info",
			TraceID:    traceID,
			DurationMs: &durMs,
			Detail: map[string]interface{}{
				"filename":      filename,
				"message_id":    messageID,
				"customer_name": extracted.CustomerName,
				"items_count":   len(extracted.Items),
				"confidence":    extracted.Confidence,
				"raw_data":      json.RawMessage(rawDataBytes),
			},
		})
	}

	if len(anomalies) > 0 {
		_ = h.billRepo.UpdateAnomalies(bill.ID, anomalies)
	}
	for i := range enriched {
		enriched[i].item.BillID = bill.ID
		candidatesJSON, _ := json.Marshal(enriched[i].candidates)
		_ = h.billRepo.InsertItemWithCandidates(&enriched[i].item, candidatesJSON)
	}
	// Refresh billItems IDs for any later use (anomaly status calc uses Mapped flag only)
	for i := range enriched {
		billItems[i] = enriched[i].item
	}

	// Manual-confirm flow: every email-sourced bill stays pending (or
	// needs_review when items aren't all mapped) until a user confirms it
	// in the UI and clicks "ส่ง SML". No auto-send anymore.
	allMapped := true
	for _, item := range billItems {
		if !item.Mapped {
			allMapped = false
			break
		}
	}

	status := "pending"
	if !allMapped {
		status = "needs_review"
	}
	_ = h.billRepo.UpdateStatus(bill.ID, status, nil, nil, nil)

	if h.auditRepo != nil {
		billIDStr := bill.ID
		durMs := int(time.Since(startTime).Milliseconds())
		_ = h.auditRepo.Log(models.AuditEntry{
			Action:     "bill_pending",
			TargetID:   &billIDStr,
			Source:     "email",
			Level:      "info",
			TraceID:    traceID,
			DurationMs: &durMs,
			Detail: map[string]interface{}{
				"filename":      filename,
				"all_mapped":    allMapped,
				"confidence":    extracted.Confidence,
				"anomaly_count": len(anomalies),
				"status":        status,
			},
		})
	}
	h.adminNotify(fmt.Sprintf("📋 Email bill pending review\nBill: %s\nFile: %s\nStatus: %s\nConfidence: %.2f\nAnomalies: %d",
		bill.ID, filename, status, extracted.Confidence, len(anomalies)))

	return nil
}

func (h *EmailHandler) adminNotify(msg string) {
	if h.lineSvc != nil {
		_ = h.lineSvc.PushAdmin(msg)
	}
}

// ProcessShopeeEmailBody handles Shopee order confirmation emails.
// bodyText is the raw HTML (or plain text) from the email body.
// This method:
//  1. Uses AI to extract order items from the HTML body
//  2. Runs catalog similarity search for each item
//  3. Creates a bill with source='shopee_email'
//     - status='needs_review' if any item has low confidence
//     - status='pending' if all items are high confidence (and sends to SML)
func (h *EmailHandler) ProcessShopeeEmailBody(subject, from, bodyText, bodyHTML, messageID string, source emailservice.MailSource) error {
	traceID := fmt.Sprintf("shopee-email-%d", time.Now().UnixMilli())
	startTime := time.Now()

	// Deduplication check
	if messageID != "" {
		exists, err := h.billRepo.FindByEmailMessageID(messageID)
		if err != nil {
			h.logger.Warn("shopee_email: dedup check failed", zap.String("message_id", messageID), zap.Error(err))
		} else if exists {
			h.logger.Info("shopee_email: skipping duplicate", zap.String("message_id", messageID))
			return emailservice.SkipMessage("duplicate", "เมลนี้เคยสร้างบิลแล้ว")
		}
	}

	if h.catalogSvc == nil {
		h.logger.Warn("shopee_email: catalog service not configured — skipping")
		return fmt.Errorf("catalog service not configured")
	}

	// Use AI to extract order info from HTML body
	// Strip HTML tags for cleaner extraction
	plainText := htmlToText(bodyText)
	extracted, err := h.aiClient.ExtractText(plainText)
	if err != nil || extracted == nil || len(extracted.Items) == 0 {
		h.logger.Warn("shopee_email: AI extract failed or empty",
			zap.String("subject", subject), zap.Error(err))
		if err == nil {
			return fmt.Errorf("AI extract shopee email: empty items")
		}
		return fmt.Errorf("AI extract shopee email: %w", err)
	}

	// Extract Shopee order ID from subject (e.g. "คำสั่งซื้อ #2501234567890")
	shopeeOrderID := extractShopeeOrderID(subject)

	// Dedup by order ID
	if shopeeOrderID != "" {
		existsByOrderID, _ := h.billRepo.FindByShopeeOrderID(shopeeOrderID)
		if existsByOrderID {
			h.logger.Info("shopee_email: skipping duplicate order", zap.String("order_id", shopeeOrderID))
			return emailservice.SkipMessage("duplicate_order", "คำสั่งซื้อนี้เคยสร้างบิลแล้ว")
		}
	}

	// For each extracted item, run catalog search
	type itemWithCandidates struct {
		item       models.BillItem
		candidates []models.CatalogMatch
		topMatch   *models.CatalogMatch
	}

	var itemsWithCandidates []itemWithCandidates
	allHighConfidence := true
	const topK = 5
	const highConfThreshold = 0.85

	// Fallback per-item prices in case the AI returned nil for some lines
	// (Gemini occasionally drops Price fields when it sees the ฿ glyph).
	fallbackPrices := extractShopeePrices(plainText)
	sourceImages := extractShopeeImageURLs(bodyHTML)
	if len(sourceImages) == 0 {
		sourceImages = extractShopeeImageURLs(bodyText)
	}

	for i, extItem := range extracted.Items {
		var matches []models.CatalogMatch

		// Try embedding search first
		if h.embSvc != nil && h.embSvc.IsConfigured() && h.catalogIdx != nil && h.catalogIdx.Size() > 0 {
			queryEmb, err := h.embSvc.EmbedText(extItem.RawName)
			if err == nil {
				matches = h.catalogIdx.Search(queryEmb, topK)
			}
		}

		// Fallback to text search
		if len(matches) == 0 {
			matches, _ = h.catalogSvc.SearchByText(extItem.RawName, topK)
		}

		price := 0.0
		if extItem.Price != nil {
			price = *extItem.Price
		}

		item := models.BillItem{
			RawName: extItem.RawName,
			Qty:     extItem.Qty,
			Mapped:  false,
		}
		if extItem.Price != nil {
			item.Price = extItem.Price
		} else if i < len(fallbackPrices) {
			p := fallbackPrices[i]
			item.Price = &p
			price = p
		}

		var topMatch *models.CatalogMatch
		if len(matches) > 0 {
			if matches[0].Score >= highConfThreshold {
				topMatch = &matches[0]
				item.ItemCode = &matches[0].ItemCode
				item.UnitCode = &matches[0].UnitCode
				item.Mapped = true
				_ = price // keep the assignment for SML payload
			} else {
				allHighConfidence = false
			}
		} else {
			allHighConfidence = false
		}

		itemsWithCandidates = append(itemsWithCandidates, itemWithCandidates{
			item:       item,
			candidates: matches,
			topMatch:   topMatch,
		})
	}

	// doc_date: prefer the order date parsed from the email body
	// (e.g. "วันที่สั่งซื้อ 04/04/2026"); otherwise the retry handler
	// falls back to today.
	docDate := extractDocDate(plainText)

	// Build raw_data payload
	rawDataMap := map[string]interface{}{
		"flow":             "shopee_email_order",
		"subject":          subject,
		"from":             from,
		"email_message_id": messageID,
		"shopee_order_id":  shopeeOrderID,
		"customer_name":    extracted.CustomerName,
		"customer_phone":   extracted.CustomerPhone,
		"note":             extracted.Note,
		"doc_date":         docDate,
	}
	applyMailSource(rawDataMap, source)
	rawDataBytes, _ := json.Marshal(rawDataMap)

	// Determine bill status
	status := "needs_review"
	if allHighConfidence {
		status = "pending"
	}

	conf := extracted.Confidence
	bill := &models.Bill{
		BillType:     "sale",
		Source:       "shopee_email",
		Status:       status,
		AIConfidence: &conf,
		RawData:      json.RawMessage(rawDataBytes),
		SMLOrderID:   shopeeOrderID,
	}
	if err := h.billRepo.Create(bill); err != nil {
		return fmt.Errorf("create shopee_email bill: %w", err)
	}
	h.recordShopeeOrderEvent(bill.ID, subject, from, messageID, source, shopeeOrderID)
	_ = h.billRepo.MarkProcessedEmailKey("shopee_email", messageID, shopeeOrderID)

	// Save the email body as artifact. Prefer the rich HTML MIME part (bodyHTML)
	// so the bill detail viewer renders it as a proper email. Fall back to plain
	// text when no HTML part exists.
	if bodyHTML != "" {
		h.saveEmailArtifacts(bill.ID, "email_html", "shopee-order.html", "text/html; charset=utf-8",
			[]byte(bodyHTML), subject, from, messageID)
	} else {
		h.saveEmailArtifacts(bill.ID, "email_text", "shopee-order.txt", "text/plain; charset=utf-8",
			[]byte(bodyText), subject, from, messageID)
	}

	// Insert bill items with candidates
	for _, iwc := range itemsWithCandidates {
		item := iwc.item
		item.BillID = bill.ID

		// Store top-5 candidates as JSON
		candidatesJSON, _ := json.Marshal(iwc.candidates)

		_ = h.billRepo.InsertItemWithCandidates(&item, candidatesJSON)
	}

	// Audit log
	if h.auditRepo != nil {
		billIDStr := bill.ID
		durMs := int(time.Since(startTime).Milliseconds())
		_ = h.auditRepo.Log(models.AuditEntry{
			Action:     "shopee_email_received",
			TargetID:   &billIDStr,
			Source:     "shopee_email",
			Level:      "info",
			TraceID:    traceID,
			DurationMs: &durMs,
			Detail: map[string]interface{}{
				"subject":       subject,
				"from":          from,
				"message_id":    messageID,
				"order_id":      shopeeOrderID,
				"items_count":   len(itemsWithCandidates),
				"all_high_conf": allHighConfidence,
				"status":        status,
			},
		})
	}

	if status == "needs_review" {
		h.adminNotify(fmt.Sprintf("🛒 Shopee Email: บิลรอยืนยัน\nSubject: %s\nOrder: %s\nItems: %d\nBill ID: %s",
			subject, shopeeOrderID, len(itemsWithCandidates), bill.ID))
	}

	h.logger.Info("shopee_email: bill created",
		zap.String("bill_id", bill.ID),
		zap.String("status", status),
		zap.String("order_id", shopeeOrderID),
		zap.Int("items", len(itemsWithCandidates)),
	)

	return nil
}

// htmlToText strips HTML tags for cleaner AI extraction
func htmlToText(html string) string {
	// Replace common block tags with newlines
	replacer := strings.NewReplacer(
		"<br>", "\n", "<br/>", "\n", "<br />", "\n",
		"</p>", "\n", "</div>", "\n", "</tr>", "\n",
		"</td>", " ", "</th>", " ",
		"&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">",
	)
	text := replacer.Replace(html)

	// Remove remaining tags
	var result strings.Builder
	inTag := false
	for _, r := range text {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			result.WriteRune(r)
		}
	}

	// Collapse multiple spaces/newlines
	lines := strings.Split(result.String(), "\n")
	var cleaned []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			cleaned = append(cleaned, line)
		}
	}
	return strings.Join(cleaned, "\n")
}

// extractShopeeOrderID tries to find the order number from the email subject
// Shopee subjects are like "คำสั่งซื้อ #250123456789012" or "Order #250123456789012 shipped"
// extractDocDate scans free-form Thai email body text for a Shopee-style date
// and returns it as "YYYY-MM-DD". Returns empty string if no date is found;
// callers should fall back to time.Now().
func extractDocDate(body string) string {
	for _, re := range docDatePatterns {
		m := re.FindStringSubmatch(body)
		if len(m) == 4 {
			day, mo, year := m[1], m[2], m[3]
			if len(day) == 1 {
				day = "0" + day
			}
			if len(mo) == 1 {
				mo = "0" + mo
			}
			return year + "-" + mo + "-" + day
		}
	}
	return ""
}

func extractShopeeOrderID(subject string) string {
	// Look for "#" followed by alphanumeric characters (Shopee uses both
	// pure-digit and alphanumeric IDs, e.g. "260404V08VQU10").
	idx := strings.Index(subject, "#")
	if idx < 0 {
		return ""
	}
	rest := subject[idx+1:]
	end := 0
	for end < len(rest) {
		c := rest[end]
		if (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			end++
		} else {
			break
		}
	}
	if end > 0 {
		return rest[:end]
	}
	return ""
}
