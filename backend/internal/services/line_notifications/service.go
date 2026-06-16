package linenotify

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/repository"
	lineservice "nexflow/internal/services/line"
)

const (
	maxDeliveryAttempts = 3
	defaultWorkerEvery  = 15 * time.Second
)

type Service struct {
	repo          *repository.LineNotificationRepo
	publicBaseURL string
	logger        *zap.Logger
}

func NewService(repo *repository.LineNotificationRepo, publicBaseURL string, logger *zap.Logger) *Service {
	return &Service{
		repo:          repo,
		publicBaseURL: strings.TrimRight(strings.TrimSpace(publicBaseURL), "/"),
		logger:        logger,
	}
}

func (s *Service) EnqueueShopeeNewOrder(ctx context.Context, snap *models.ShopeeOrderSnapshot, dedupeKey string) (int, error) {
	if s == nil || s.repo == nil || snap == nil {
		return 0, nil
	}
	dedupeKey = strings.TrimSpace(dedupeKey)
	if dedupeKey == "" {
		dedupeKey = fmt.Sprintf("shopee:new_order:%d:%s", snap.ShopID, strings.TrimSpace(snap.OrderSN))
	}
	message := BuildShopeeNewOrderLineText(snap, s.publicBaseURL)
	return s.repo.Enqueue(ctx, models.LineNotificationMessageInput{
		Source:      "shopee_realtime",
		Severity:    "info",
		Title:       "มีออเดอร์ Shopee ใหม่",
		Body:        shopeeLineNotificationBody(snap),
		ActionURL:   ShopeeOrderActionURL(s.publicBaseURL, snap.OrderSN),
		EntityType:  "shopee_order",
		EntityID:    fmt.Sprintf("%d:%s", snap.ShopID, strings.TrimSpace(snap.OrderSN)),
		DedupeKey:   dedupeKey,
		MessageText: message,
	})
}

func (s *Service) EnqueueShopeeCancelledAfterSML(ctx context.Context, snap *models.ShopeeOrderSnapshot, dedupeKey string) (int, error) {
	if s == nil || s.repo == nil || snap == nil {
		return 0, nil
	}
	dedupeKey = strings.TrimSpace(dedupeKey)
	if dedupeKey == "" {
		dedupeKey = fmt.Sprintf("shopee:cancelled_after_sml:%d:%s", snap.ShopID, strings.TrimSpace(snap.OrderSN))
	}
	message := BuildShopeeCancelledAfterSMLLineText(snap, s.publicBaseURL)
	return s.repo.Enqueue(ctx, models.LineNotificationMessageInput{
		Source:      "shopee_realtime",
		Severity:    "error",
		Title:       "Shopee ยกเลิกหลังส่ง SML",
		Body:        shopeeCancelledAfterSMLBody(snap),
		ActionURL:   ShopeeOrderActionURL(s.publicBaseURL, snap.OrderSN),
		EntityType:  "shopee_order",
		EntityID:    fmt.Sprintf("%d:%s", snap.ShopID, strings.TrimSpace(snap.OrderSN)),
		DedupeKey:   dedupeKey,
		MessageText: message,
	})
}

func (s *Service) StartWorker(ctx context.Context, interval time.Duration, batchSize int) {
	if s == nil || s.repo == nil {
		return
	}
	if interval <= 0 {
		interval = defaultWorkerEvery
	}
	if batchSize <= 0 || batchSize > 50 {
		batchSize = 10
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := s.ProcessBatch(ctx, batchSize); err != nil && ctx.Err() == nil && s.logger != nil {
			s.logger.Warn("line notifications worker failed", zap.Error(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) ProcessBatch(ctx context.Context, batchSize int) (int, error) {
	if s == nil || s.repo == nil {
		return 0, nil
	}
	jobs, err := s.repo.LeaseDeliveries(ctx, batchSize, maxDeliveryAttempts)
	if err != nil {
		return 0, err
	}
	done := 0
	for _, job := range jobs {
		if ctx.Err() != nil {
			return done, ctx.Err()
		}
		svc, err := lineservice.New(job.ChannelSecret, job.ChannelAccessToken, "")
		if err == nil {
			if shouldSendShopeeOrderFlex(job) {
				altText, contents := BuildShopeeNewOrderLineFlex(job)
				if contents != nil {
					err = svc.PushFlex(job.DestinationID, altText, contents)
					if err != nil && s.logger != nil {
						s.logger.Warn("line flex notification failed, falling back to text",
							zap.String("delivery_id", job.ID),
							zap.String("recipient_id", job.RecipientID),
							zap.String("entity_id", job.EntityID),
							zap.Error(err),
						)
					}
				}
			}
			if err != nil || !shouldSendShopeeOrderFlex(job) {
				err = svc.PushText(job.DestinationID, job.MessageText)
			}
		}
		if err != nil {
			_ = s.repo.MarkDeliveryFailed(ctx, job.ID, err.Error(), maxDeliveryAttempts)
			if s.logger != nil {
				s.logger.Warn("line notification delivery failed",
					zap.String("delivery_id", job.ID),
					zap.String("recipient_id", job.RecipientID),
					zap.String("entity_id", job.EntityID),
					zap.Error(err),
				)
			}
			continue
		}
		if err := s.repo.MarkDeliverySent(ctx, job.ID); err != nil {
			if s.logger != nil {
				s.logger.Warn("line notification mark sent failed", zap.String("delivery_id", job.ID), zap.Error(err))
			}
			continue
		}
		done++
	}
	return done, nil
}

func BuildShopeeNewOrderLineText(snap *models.ShopeeOrderSnapshot, publicBaseURL string) string {
	if snap == nil {
		return "มีออเดอร์ Shopee ใหม่"
	}
	detail := parseShopeeRawOrderDetail(snap.RawDetail)
	shop := strings.TrimSpace(snap.ShopLabel)
	if shop == "" && snap.ShopID > 0 {
		shop = fmt.Sprintf("shop_id %d", snap.ShopID)
	}
	amount := ""
	if snap.TotalAmount > 0 {
		amount = fmt.Sprintf("ยอดรวม: ฿%.2f", snap.TotalAmount)
	}
	payment := shopeePaymentLabel(snap.PaymentMethod, detail.PaymentMethod, detail.COD)
	items := shopeeProductLines(detail.Items, snap.ItemCount, 3)
	parts := []string{
		"มีออเดอร์ Shopee ใหม่",
		"ร้าน: " + fallbackDash(shop),
		"Order SN: " + fallbackDash(strings.TrimSpace(snap.OrderSN)),
	}
	if amount != "" {
		parts = append(parts, amount)
	}
	if payment != "" {
		parts = append(parts, "ชำระเงิน: "+payment)
	}
	if len(items) == 1 && !strings.HasPrefix(items[0], "1.") {
		parts = append(parts, "สินค้า: "+items[0])
	} else if len(items) > 0 {
		parts = append(parts, "สินค้า:")
		parts = append(parts, items...)
	}
	if url := ShopeeOrderActionURL(publicBaseURL, snap.OrderSN); url != "" {
		parts = append(parts, "เปิดใน Nexflow: "+url)
	}
	return strings.Join(parts, "\n")
}

func shopeeLineNotificationBody(snap *models.ShopeeOrderSnapshot) string {
	if snap == nil {
		return ""
	}
	detail := parseShopeeRawOrderDetail(snap.RawDetail)
	items := shopeeProductLines(detail.Items, snap.ItemCount, 1)
	bits := []string{strings.TrimSpace(snap.OrderSN)}
	if snap.TotalAmount > 0 {
		bits = append(bits, fmt.Sprintf("฿%.2f", snap.TotalAmount))
	}
	if len(items) > 0 {
		bits = append(bits, strings.TrimPrefix(items[0], "1. "))
	}
	return strings.Join(filterNonEmpty(bits), " · ")
}

func BuildShopeeNewOrderLineFlex(job models.LineNotificationDeliveryJob) (string, map[string]any) {
	parts := parseShopeeLineMessageParts(job.MessageText)
	title := fallbackDash(parts.Title)
	if title == "-" {
		title = "มีออเดอร์ Shopee ใหม่"
	}
	actionURL := strings.TrimSpace(job.ActionURL)
	if actionURL == "" {
		actionURL = parts.URL
	}
	amount := fallbackDash(parts.Amount)
	shop := fallbackDash(parts.Shop)
	orderSN := fallbackDash(parts.OrderSN)
	alt := strings.TrimSpace(strings.Join(filterNonEmpty([]string{title, parts.Shop, parts.Amount}), " · "))
	if alt == "" {
		alt = "มีออเดอร์ Shopee ใหม่"
	}

	bodyContents := []map[string]any{
		{
			"type":   "text",
			"text":   title,
			"weight": "bold",
			"size":   "lg",
			"color":  "#0F172A",
			"wrap":   true,
		},
		{
			"type":  "text",
			"text":  shop,
			"size":  "sm",
			"color": "#64748B",
			"wrap":  true,
		},
		{
			"type":    "box",
			"layout":  "horizontal",
			"margin":  "md",
			"spacing": "sm",
			"contents": []map[string]any{
				{
					"type":  "text",
					"text":  "ยอดรวม",
					"size":  "xs",
					"color": "#64748B",
					"flex":  1,
				},
				{
					"type":   "text",
					"text":   amount,
					"size":   "xl",
					"weight": "bold",
					"color":  "#2563EB",
					"align":  "end",
					"flex":   2,
				},
			},
		},
	}
	meta := []string{}
	if orderSN != "-" {
		meta = append(meta, "Order "+orderSN)
	}
	if parts.Payment != "" {
		meta = append(meta, parts.Payment)
	}
	if len(meta) > 0 {
		bodyContents = append(bodyContents, map[string]any{
			"type":   "text",
			"text":   strings.Join(meta, " · "),
			"size":   "xs",
			"color":  "#64748B",
			"wrap":   true,
			"margin": "sm",
		})
	}
	bodyContents = append(bodyContents,
		map[string]any{
			"type":   "separator",
			"margin": "md",
		},
		map[string]any{
			"type":   "text",
			"text":   "สินค้า",
			"weight": "bold",
			"size":   "sm",
			"color":  "#334155",
			"margin": "md",
		},
	)
	items := parts.Items
	if len(items) == 0 && parts.ItemSummary != "" {
		items = []string{parts.ItemSummary}
	}
	if len(items) == 0 {
		items = []string{"เปิดดูรายละเอียดสินค้าใน Nexflow"}
	}
	for _, item := range items {
		bodyContents = append(bodyContents, map[string]any{
			"type":  "text",
			"text":  truncateRunes(item, 120),
			"size":  "sm",
			"color": "#0F172A",
			"wrap":  true,
		})
	}

	contents := map[string]any{
		"type": "bubble",
		"size": "mega",
		"body": map[string]any{
			"type":     "box",
			"layout":   "vertical",
			"spacing":  "sm",
			"contents": bodyContents,
		},
	}
	if isAbsoluteHTTPURL(actionURL) {
		contents["footer"] = map[string]any{
			"type":   "box",
			"layout": "vertical",
			"contents": []map[string]any{
				{
					"type":   "button",
					"style":  "primary",
					"color":  "#2563EB",
					"height": "sm",
					"action": map[string]any{
						"type":  "uri",
						"label": "เปิดใน Nexflow",
						"uri":   actionURL,
					},
				},
			},
		}
	}
	return alt, contents
}

func ShopeeOrderActionURL(publicBaseURL, orderSN string) string {
	orderSN = strings.TrimSpace(orderSN)
	path := "/shopee-operations"
	if orderSN != "" {
		path += "?order=" + url.QueryEscape(orderSN)
	}
	base := strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
	if base == "" {
		return path
	}
	return base + path
}

type shopeeRawOrderDetail struct {
	PaymentMethod string
	COD           bool
	Items         []shopeeRawOrderItem
}

type shopeeRawOrderItem struct {
	ItemName string
	Model    string
	Qty      float64
}

func parseShopeeRawOrderDetail(raw json.RawMessage) shopeeRawOrderDetail {
	if len(raw) == 0 {
		return shopeeRawOrderDetail{}
	}
	var decoded struct {
		PaymentMethod string `json:"payment_method"`
		COD           bool   `json:"cod"`
		ItemList      []struct {
			ItemName               string  `json:"item_name"`
			ModelName              string  `json:"model_name"`
			ModelQuantityPurchased float64 `json:"model_quantity_purchased"`
		} `json:"item_list"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return shopeeRawOrderDetail{}
	}
	out := shopeeRawOrderDetail{PaymentMethod: decoded.PaymentMethod, COD: decoded.COD}
	for _, item := range decoded.ItemList {
		name := compactWhitespace(item.ItemName)
		if name == "" {
			continue
		}
		out.Items = append(out.Items, shopeeRawOrderItem{
			ItemName: name,
			Model:    compactWhitespace(item.ModelName),
			Qty:      item.ModelQuantityPurchased,
		})
	}
	return out
}

func shopeeProductLines(items []shopeeRawOrderItem, itemCount, limit int) []string {
	if limit <= 0 {
		limit = 3
	}
	if len(items) == 0 {
		if itemCount > 0 {
			return []string{fmt.Sprintf("%d รายการ", itemCount)}
		}
		return []string{"ดูรายละเอียดสินค้าใน Nexflow"}
	}
	out := make([]string, 0, minInt(len(items), limit)+1)
	for i, item := range items {
		if i >= limit {
			break
		}
		name := truncateRunes(item.ItemName, 86)
		if shouldAppendModel(name, item.Model) {
			suffix := " (" + item.Model + ")"
			name = truncateRunes(item.ItemName, maxInt(24, 86-len([]rune(suffix)))) + suffix
		}
		out = append(out, fmt.Sprintf("%d. %s x%s", i+1, name, formatShopeeQty(item.Qty)))
	}
	if len(items) > limit {
		out = append(out, fmt.Sprintf("และอีก %d รายการ", len(items)-limit))
	}
	return out
}

func shopeePaymentLabel(snapshotPayment, rawPayment string, cod bool) string {
	payment := strings.TrimSpace(snapshotPayment)
	if payment == "" {
		payment = strings.TrimSpace(rawPayment)
	}
	if cod || strings.Contains(strings.ToLower(payment), "cod") || strings.Contains(payment, "เก็บเงินปลายทาง") {
		return "เก็บเงินปลายทาง"
	}
	return payment
}

type shopeeLineMessageParts struct {
	Title       string
	Shop        string
	OrderSN     string
	Amount      string
	Payment     string
	ItemSummary string
	Items       []string
	URL         string
}

func parseShopeeLineMessageParts(text string) shopeeLineMessageParts {
	var out shopeeLineMessageParts
	lines := strings.Split(text, "\n")
	inItems := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch {
		case out.Title == "" && strings.Contains(line, "ออเดอร์ Shopee"):
			out.Title = line
			inItems = false
		case strings.HasPrefix(line, "ร้าน:"):
			out.Shop = strings.TrimSpace(strings.TrimPrefix(line, "ร้าน:"))
			inItems = false
		case strings.HasPrefix(line, "Order SN:"):
			out.OrderSN = strings.TrimSpace(strings.TrimPrefix(line, "Order SN:"))
			inItems = false
		case strings.HasPrefix(line, "Order:"):
			out.OrderSN = strings.TrimSpace(strings.TrimPrefix(line, "Order:"))
			inItems = false
		case strings.HasPrefix(line, "ยอดรวม:"):
			out.Amount = strings.TrimSpace(strings.TrimPrefix(line, "ยอดรวม:"))
			inItems = false
		case strings.HasPrefix(line, "ชำระเงิน:"):
			out.Payment = strings.TrimSpace(strings.TrimPrefix(line, "ชำระเงิน:"))
			inItems = false
		case strings.HasPrefix(line, "สินค้า:"):
			itemSummary := strings.TrimSpace(strings.TrimPrefix(line, "สินค้า:"))
			if itemSummary != "" {
				out.ItemSummary = itemSummary
				inItems = false
			} else {
				inItems = true
			}
		case strings.HasPrefix(line, "เปิดใน Nexflow:"):
			out.URL = strings.TrimSpace(strings.TrimPrefix(line, "เปิดใน Nexflow:"))
			inItems = false
		case inItems:
			out.Items = append(out.Items, line)
		}
	}
	return out
}

func shouldSendShopeeOrderFlex(job models.LineNotificationDeliveryJob) bool {
	return job.Source == "shopee_realtime" && job.EntityType == "shopee_order" && strings.Contains(job.Title, "ออเดอร์ Shopee ใหม่")
}

func BuildShopeeCancelledAfterSMLLineText(snap *models.ShopeeOrderSnapshot, publicBaseURL string) string {
	if snap == nil {
		return "Shopee ยกเลิกหลังส่ง SML"
	}
	parts := []string{
		"Shopee ยกเลิกหลังส่ง SML",
		"Order SN: " + fallbackDash(strings.TrimSpace(snap.OrderSN)),
	}
	if strings.TrimSpace(snap.SMLDocNo) != "" {
		parts = append(parts, "ใบขาย SML: "+strings.TrimSpace(snap.SMLDocNo))
	}
	if snap.TotalAmount > 0 {
		parts = append(parts, fmt.Sprintf("ยอดรวม: ฿%.2f", snap.TotalAmount))
	}
	parts = append(parts, "ต้องสร้างเอกสารยกเลิก SML ใน Nexflow")
	if url := ShopeeOrderActionURL(publicBaseURL, snap.OrderSN); url != "" {
		parts = append(parts, "เปิดใน Nexflow: "+url)
	}
	return strings.Join(parts, "\n")
}

func shopeeCancelledAfterSMLBody(snap *models.ShopeeOrderSnapshot) string {
	if snap == nil {
		return ""
	}
	bits := []string{strings.TrimSpace(snap.OrderSN)}
	if strings.TrimSpace(snap.SMLDocNo) != "" {
		bits = append(bits, "ใบขาย "+strings.TrimSpace(snap.SMLDocNo))
	}
	if snap.TotalAmount > 0 {
		bits = append(bits, fmt.Sprintf("฿%.2f", snap.TotalAmount))
	}
	return strings.Join(filterNonEmpty(bits), " · ")
}

func BuildShopeeNewOrderAltText(text string) string {
	parts := parseShopeeLineMessageParts(text)
	alt := strings.Join(filterNonEmpty([]string{"ออเดอร์ Shopee ใหม่", parts.Shop, parts.Amount}), " · ")
	if strings.TrimSpace(alt) == "" {
		return "มีออเดอร์ Shopee ใหม่"
	}
	return alt
}

func erpStatusLabel(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "pending":
		return "รอสร้างเอกสาร"
	case "pending_erp":
		return "สร้างเอกสารแล้ว รอส่ง SML"
	case "needs_review":
		return "ต้องตรวจสอบ"
	case "sent":
		return "ส่ง SML แล้ว"
	case "failed":
		return "บันทึกล้มเหลว"
	case "blocked":
		return "ถูกบล็อก"
	case "cancelled":
		return "ยกเลิก"
	case "waiting_shopee":
		return "รอ Shopee ยืนยัน"
	default:
		return fallbackDash(status)
	}
}

func fallbackDash(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "-"
	}
	return v
}

func compactWhitespace(v string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(v)), " ")
}

func shouldAppendModel(name, model string) bool {
	model = strings.TrimSpace(model)
	if model == "" || strings.EqualFold(model, "default") {
		return false
	}
	return !strings.Contains(strings.ToLower(name), strings.ToLower(model))
}

func formatShopeeQty(qty float64) string {
	if qty <= 0 {
		qty = 1
	}
	if math.Abs(qty-math.Round(qty)) < 0.000001 {
		return fmt.Sprintf("%.0f", qty)
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", qty), "0"), ".")
}

func truncateRunes(v string, limit int) string {
	v = strings.TrimSpace(v)
	if limit <= 0 {
		return v
	}
	runes := []rune(v)
	if len(runes) <= limit {
		return v
	}
	if limit <= 1 {
		return string(runes[:limit])
	}
	return string(runes[:limit-1]) + "…"
}

func filterNonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" && v != "-" {
			out = append(out, v)
		}
	}
	return out
}

func isAbsoluteHTTPURL(v string) bool {
	v = strings.TrimSpace(v)
	return strings.HasPrefix(v, "https://") || strings.HasPrefix(v, "http://")
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
