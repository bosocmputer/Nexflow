package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/ai"
	emailservice "nexflow/internal/services/email"
)

// ProcessShopeeShippedEmailBody handles Shopee shipping-confirmation emails
// (subject contains "ถูกจัดส่งแล้ว" or "ยืนยันการชำระเงิน").
// One email may contain multiple Shopee orders (one per seller) — this
// function creates a separate purchase bill for each order_id found by AI.
// Bills are never auto-sent — status is always pending or needs_review.
func (h *EmailHandler) ProcessShopeeShippedEmailBody(subject, from, bodyText, bodyHTML, messageID string, source emailservice.MailSource) error {
	traceID := fmt.Sprintf("shopee-shipped-%d", time.Now().UnixMilli())
	startTime := time.Now()

	if h.catalogSvc == nil {
		h.logger.Warn("shopee_shipped: catalog service not configured — skipping")
		return fmt.Errorf("catalog service not configured")
	}

	if messageID != "" {
		var count int
		_ = h.billRepo.DB().QueryRow(
			`SELECT
			   (SELECT COUNT(*) FROM bills
			     WHERE source='shopee_shipped'
			       AND raw_data->>'email_message_id' = $1) +
			   (SELECT COUNT(*) FROM processed_email_keys
			     WHERE source='shopee_shipped'
			       AND message_id = $1
			       AND order_id = '')`,
			messageID,
		).Scan(&count)
		if count > 0 {
			h.logger.Info("shopee_shipped: skipping duplicate email before AI",
				zap.String("message_id", messageID),
				zap.Int("existing_bills", count),
			)
			return emailservice.SkipMessage("duplicate", "เมลนี้เคยประมวลผลแล้ว")
		}
	}

	// bodyText is already plain text (extractBodyText prefers text/plain).
	// htmlToText is a no-op when input has no HTML tags, so it's safe to call.
	plainText := htmlToText(bodyText)

	// AI extracts all orders from this email, including per-item image URLs
	// resolved from the HTML (more accurate than index-based assignment).
	orders, err := h.aiClient.ExtractOrdersWithHTML(plainText, bodyHTML)
	if err != nil || len(orders) == 0 {
		h.logger.Warn("shopee_shipped: AI extract failed or empty",
			zap.String("subject", subject), zap.Error(err))
		if err == nil {
			return fmt.Errorf("AI extract shopee_shipped: empty orders")
		}
		return fmt.Errorf("AI extract shopee_shipped: %w", err)
	}

	h.logger.Info("shopee_shipped: orders extracted",
		zap.String("trace_id", traceID),
		zap.Int("order_count", len(orders)),
	)

	// Per-item prices parsed from the email body — fallback for AI nulls.
	fallbackPrices := extractShopeePrices(plainText)

	createdCount := 0
	skippedCount := 0
	failedCount := 0
	for _, order := range orders {
		created, err := h.processOneShippedOrder(
			order, subject, from, bodyText, bodyHTML, messageID, fallbackPrices, traceID, startTime, source,
		)
		if err != nil {
			failedCount++
			h.logger.Warn("shopee_shipped: order processing failed",
				zap.String("order_id", order.OrderID), zap.Error(err))
		}
		if created {
			createdCount++
		} else {
			skippedCount++
		}
	}

	h.logger.Info("shopee_shipped: batch done",
		zap.String("trace_id", traceID),
		zap.Int("created", createdCount),
		zap.Int("skipped", skippedCount),
		zap.Int("failed", failedCount),
	)
	if messageID != "" && failedCount == 0 {
		_ = h.billRepo.MarkProcessedEmailKey("shopee_shipped", messageID, "")
	}
	if createdCount == 0 && skippedCount > 0 && failedCount == 0 {
		return emailservice.SkipMessage("duplicate_or_empty", "ไม่มีบิลใหม่จากเมลนี้ อาจซ้ำหรือไม่มีรายการสินค้าที่ใช้ได้")
	}
	return nil
}

// processOneShippedOrder creates a single purchase bill for one Shopee order.
// Returns (true, nil) when the bill was created, (false, nil) when skipped (dedup).
func (h *EmailHandler) processOneShippedOrder(
	order ai.ExtractedOrder,
	subject, from, bodyText, bodyHTML, messageID string,
	fallbackPrices []float64,
	traceID string,
	startTime time.Time,
	source emailservice.MailSource,
) (bool, error) {
	orderID := strings.TrimSpace(order.OrderID)
	if orderID == "" || strings.EqualFold(orderID, "#unknown") {
		h.logger.Warn("shopee_shipped: skipping order without order_id",
			zap.String("message_id", messageID),
			zap.String("subject", subject),
		)
		return false, nil
	}

	validItems := make([]ai.ExtractedItem, 0, len(order.Items))
	for _, extItem := range order.Items {
		extItem.RawName = strings.TrimSpace(extItem.RawName)
		if extItem.RawName == "" || extItem.Qty <= 0 {
			continue
		}
		validItems = append(validItems, extItem)
	}
	if len(validItems) == 0 {
		h.logger.Warn("shopee_shipped: skipping order without usable items",
			zap.String("message_id", messageID),
			zap.String("order_id", orderID),
			zap.String("subject", subject),
		)
		if messageID != "" {
			_ = h.billRepo.MarkProcessedEmailKey("shopee_shipped", messageID, orderID)
		}
		return false, nil
	}

	// Dedup: skip if a bill with the same (email_message_id, order_id) already exists.
	var count int
	_ = h.billRepo.DB().QueryRow(
		`SELECT
		   (SELECT COUNT(*) FROM bills
		     WHERE source='shopee_shipped'
		       AND raw_data->>'email_message_id' = $1
		       AND raw_data->>'order_id' = $2) +
		   (SELECT COUNT(*) FROM processed_email_keys
		     WHERE source='shopee_shipped'
		       AND message_id = $1
		       AND order_id = $2)`,
		messageID, orderID,
	).Scan(&count)
	if count > 0 {
		h.logger.Info("shopee_shipped: skipping duplicate",
			zap.String("message_id", messageID),
			zap.String("order_id", orderID),
		)
		return false, nil
	}
	if existingBillID, exists, err := h.findExistingShopeeShippedBillID(orderID); err != nil {
		return false, fmt.Errorf("lookup existing shopee_shipped order: %w", err)
	} else if exists {
		h.recordShopeeOrderEvent(existingBillID, subject, from, messageID, source, orderID)
		h.saveShopeeShippedEmailArtifacts(existingBillID, subject, from, bodyText, bodyHTML, messageID)
		discountSummary := repository.ExtractShopeeDiscountSummary(bodyText, bodyHTML, orderID)
		if ok, err := h.billRepo.ApplyShopeePurchaseDiscountsToBill(existingBillID, discountSummary); err != nil {
			h.logger.Warn("shopee_shipped: existing bill discount update failed",
				zap.String("message_id", messageID),
				zap.String("order_id", orderID),
				zap.String("bill_id", existingBillID),
				zap.Error(err))
		} else if ok {
			h.logger.Info("shopee_shipped: updated existing bill discounts",
				zap.String("message_id", messageID),
				zap.String("order_id", orderID),
				zap.String("bill_id", existingBillID),
				zap.Float64("discount", discountSummary.TotalDiscountAmount))
		}
		paymentSummary := repository.ExtractShopeePaymentSummary(bodyText, bodyHTML, orderID)
		if ok, err := h.billRepo.ApplyShopeePurchasePaymentSummaryToBill(existingBillID, paymentSummary); err != nil {
			h.logger.Warn("shopee_shipped: existing bill payment summary update failed",
				zap.String("message_id", messageID),
				zap.String("order_id", orderID),
				zap.String("bill_id", existingBillID),
				zap.Error(err))
		} else if ok {
			h.logger.Info("shopee_shipped: updated existing bill payment summary",
				zap.String("message_id", messageID),
				zap.String("order_id", orderID),
				zap.String("bill_id", existingBillID),
				zap.String("payment_method", paymentSummary.PaymentMethod))
		}
		if messageID != "" {
			_ = h.billRepo.MarkProcessedEmailKey("shopee_shipped", messageID, orderID)
		}
		h.logger.Info("shopee_shipped: recorded status event on existing bill",
			zap.String("message_id", messageID),
			zap.String("order_id", orderID),
			zap.String("bill_id", existingBillID),
		)
		return false, nil
	}

	const topK = 5
	const highConfThreshold = 0.85

	type itemWithCandidates struct {
		item       models.BillItem
		candidates []models.CatalogMatch
	}

	var itemsWithCandidates []itemWithCandidates
	allHighConfidence := true

	for i, extItem := range validItems {
		var matches []models.CatalogMatch

		if h.embSvc != nil && h.embSvc.IsConfigured() && h.catalogIdx != nil && h.catalogIdx.Size() > 0 {
			queryEmb, err := h.embSvc.EmbedText(extItem.RawName)
			if err == nil {
				matches = h.catalogIdx.Search(queryEmb, topK)
			}
		}
		if len(matches) == 0 {
			matches, _ = h.catalogSvc.SearchByText(extItem.RawName, topK)
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
		}
		if extItem.ImageURL != "" {
			item.SourceImageURL = extItem.ImageURL
		}

		if len(matches) > 0 && matches[0].Score >= highConfThreshold {
			item.ItemCode = &matches[0].ItemCode
			item.UnitCode = &matches[0].UnitCode
			item.Mapped = true
		} else {
			allHighConfidence = false
		}

		itemsWithCandidates = append(itemsWithCandidates, itemWithCandidates{
			item:       item,
			candidates: matches,
		})
	}

	shippingAmount, hasShippingAmount := repository.ExtractShopeeShippingAmount(bodyText, bodyHTML, orderID)
	shippingItem, shippingReady := h.configuredShopeeShippingLine(orderID, shippingAmount, hasShippingAmount)
	shippingLineAdded := shippingItem != nil
	if shippingLineAdded {
		itemsWithCandidates = append(itemsWithCandidates, itemWithCandidates{item: *shippingItem})
		if !shippingReady {
			allHighConfidence = false
		}
	}
	discountSummary := repository.ExtractShopeeDiscountSummary(bodyText, bodyHTML, orderID)
	if discountSummary.HasAny() {
		itemCopies := make([]models.BillItem, len(itemsWithCandidates))
		for i := range itemsWithCandidates {
			itemCopies[i] = itemsWithCandidates[i].item
		}
		repository.ApplyShopeeDiscountsToItems(itemCopies, discountSummary.TotalDiscountAmount)
		for i := range itemsWithCandidates {
			itemsWithCandidates[i].item.DiscountAmount = itemCopies[i].DiscountAmount
		}
	}

	// doc_date: prefer AI-extracted date, then regex from body, then empty string
	// (falls back to today at retry time via docDateFromBill).
	docDate := order.DocDate
	if docDate == "" {
		docDate = extractDocDate(bodyText)
	}

	rawDataMap := map[string]interface{}{
		"subject":          subject,
		"from":             from,
		"email_message_id": messageID,
		"order_id":         orderID,
		"seller_name":      order.SellerName,
		"items":            validItems,
		"flow":             "shopee_shipped",
		"doc_date":         docDate,
		"body_text":        bodyText,
		"body_html":        bodyHTML,
	}
	if hasShippingAmount {
		rawDataMap["shipping_amount"] = shippingAmount
	}
	if discountSummary.HasAny() {
		rawDataMap["discount_summary"] = discountSummary
	}
	if paymentSummary := repository.ExtractShopeePaymentSummary(bodyText, bodyHTML, orderID); paymentSummary.HasAny() {
		rawDataMap["payment_summary"] = paymentSummary
	}
	applyMailSource(rawDataMap, source)
	rawDataBytes, _ := json.Marshal(rawDataMap)

	status := "needs_review"
	if allHighConfidence && len(itemsWithCandidates) > 0 {
		status = "pending"
	}

	conf := order.Confidence
	bill := &models.Bill{
		BillType:     "purchase",
		Source:       "shopee_shipped",
		Status:       status,
		AIConfidence: &conf,
		RawData:      json.RawMessage(rawDataBytes),
		SMLOrderID:   orderID,
	}
	if err := h.billRepo.Create(bill); err != nil {
		return false, fmt.Errorf("create shopee_shipped bill: %w", err)
	}
	h.recordShopeeOrderEvent(bill.ID, subject, from, messageID, source, orderID)
	_ = h.billRepo.MarkProcessedEmailKey("shopee_shipped", messageID, orderID)

	// Save original email body as artifact on the first order only to avoid
	// storing N copies of the same email. Prefer HTML body (renders nicely in
	// the bill detail viewer) and fall back to plain text when HTML is absent.
	if count == 0 {
		h.saveShopeeShippedEmailArtifacts(bill.ID, subject, from, bodyText, bodyHTML, messageID)
	}

	for _, iwc := range itemsWithCandidates {
		item := iwc.item
		item.BillID = bill.ID
		candidatesJSON, _ := json.Marshal(iwc.candidates)
		_ = h.billRepo.InsertItemWithCandidates(&item, candidatesJSON)
	}

	if h.auditRepo != nil {
		billIDStr := bill.ID
		durMs := int(time.Since(startTime).Milliseconds())
		_ = h.auditRepo.Log(models.AuditEntry{
			Action:     "shopee_shipped_received",
			TargetID:   &billIDStr,
			Source:     "shopee_shipped",
			Level:      "info",
			TraceID:    traceID,
			DurationMs: &durMs,
			Detail: map[string]interface{}{
				"subject":             subject,
				"from":                from,
				"message_id":          messageID,
				"order_id":            orderID,
				"seller_name":         order.SellerName,
				"items_count":         len(itemsWithCandidates),
				"all_high_conf":       allHighConfidence,
				"shipping_line_added": shippingLineAdded,
				"status":              status,
			},
		})
	}

	h.adminNotify(fmt.Sprintf("📦 Shopee Shipped: บิลรอตรวจสอบ\nOrder: %s (%s)\nItems: %d\nBill ID: %s",
		orderID, order.SellerName, len(itemsWithCandidates), bill.ID))

	h.logger.Info("shopee_shipped: bill created",
		zap.String("bill_id", bill.ID),
		zap.String("status", status),
		zap.String("order_id", orderID),
		zap.String("seller_name", order.SellerName),
		zap.Int("items", len(itemsWithCandidates)),
	)

	return true, nil
}

func (h *EmailHandler) saveShopeeShippedEmailArtifacts(billID, subject, from, bodyText, bodyHTML, messageID string) {
	if bodyHTML != "" {
		h.saveEmailArtifacts(billID, "email_html", "shopee-shipped.html", "text/html; charset=utf-8",
			[]byte(bodyHTML), subject, from, messageID)
		return
	}
	h.saveEmailArtifacts(billID, "email_text", "shopee-shipped.txt", "text/plain; charset=utf-8",
		[]byte(bodyText), subject, from, messageID)
}

func (h *EmailHandler) configuredShopeeShippingLine(orderID string, amount float64, hasAmount bool) (*models.BillItem, bool) {
	if !hasAmount || amount < 0 || h.channelDefaults == nil {
		return nil, false
	}
	def, err := h.channelDefaults.Get("shopee_shipped", "purchase")
	if err != nil {
		h.logger.Warn("shopee_shipped: shipping config lookup failed",
			zap.String("order_id", orderID), zap.Error(err))
		return nil, false
	}
	if def == nil || !def.ShippingItemEnabled {
		return nil, false
	}
	code := strings.TrimSpace(def.ShippingItemCode)
	if code == "" {
		h.logger.Warn("shopee_shipped: shipping config enabled without item code",
			zap.String("order_id", orderID))
		return nil, false
	}

	rawName := "ค่าจัดส่งสินค้า"
	unit := strings.TrimSpace(def.ShippingItemUnitCode)
	if h.catalogRepo != nil {
		if cat, err := h.catalogRepo.GetOne(code); err != nil {
			h.logger.Warn("shopee_shipped: shipping catalog lookup failed",
				zap.String("order_id", orderID), zap.String("item_code", code), zap.Error(err))
		} else if cat != nil {
			if strings.TrimSpace(cat.ItemName) != "" {
				rawName = strings.TrimSpace(cat.ItemName)
			}
			if unit == "" {
				unit = strings.TrimSpace(cat.UnitCode)
			}
		}
	}

	itemCode := code
	price := amount
	item := &models.BillItem{
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
	return item, unit != ""
}

func (h *EmailHandler) findExistingShopeeShippedBillID(orderID string) (string, bool, error) {
	if h == nil || h.billRepo == nil {
		return "", false, nil
	}
	normalized := normalizeShopeeOrderID(orderID)
	if normalized == "" {
		return "", false, nil
	}
	var id string
	err := h.billRepo.DB().QueryRow(
		`SELECT b.id::text
		   FROM bills b
		   JOIN (
		     SELECT id FROM bills
		      WHERE archived_at IS NULL
		        AND UPPER(TRIM(LEADING '#' FROM COALESCE(sml_order_id, ''))) = $1
		     UNION
		     SELECT id FROM bills
		      WHERE archived_at IS NULL
		        AND UPPER(TRIM(LEADING '#' FROM COALESCE(raw_data->>'order_id', ''))) = $1
		     UNION
		     SELECT id FROM bills
		      WHERE archived_at IS NULL
		        AND UPPER(TRIM(LEADING '#' FROM COALESCE(raw_data->>'shopee_order_id', ''))) = $1
		     UNION
		     SELECT bill_id FROM shopee_order_events
		      WHERE UPPER(order_id) = $1
		   ) existing ON existing.id = b.id
		  WHERE b.source = 'shopee_shipped'
		    AND b.archived_at IS NULL
		  ORDER BY b.created_at ASC, b.id ASC
		  LIMIT 1`,
		strings.ToUpper(normalized),
	).Scan(&id)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, err
	}
	return id, true, nil
}
