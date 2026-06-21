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
	repo                        *repository.LineNotificationRepo
	publicBaseURL               string
	richFlexEnabled             bool
	settlementLineAlertsEnabled bool
	logger                      *zap.Logger
}

func NewService(repo *repository.LineNotificationRepo, publicBaseURL string, richFlexEnabled, settlementLineAlertsEnabled bool, logger *zap.Logger) *Service {
	return &Service{
		repo:                        repo,
		publicBaseURL:               strings.TrimRight(strings.TrimSpace(publicBaseURL), "/"),
		richFlexEnabled:             richFlexEnabled,
		settlementLineAlertsEnabled: settlementLineAlertsEnabled,
		logger:                      logger,
	}
}

func (s *Service) EnqueueShopeeNewOrder(ctx context.Context, snap *models.ShopeeOrderSnapshot, payment *models.ShopeeOrderPaymentSnapshot, dedupeKey string) (int, error) {
	if s == nil || s.repo == nil || snap == nil {
		return 0, nil
	}
	dedupeKey = strings.TrimSpace(dedupeKey)
	if dedupeKey == "" {
		dedupeKey = fmt.Sprintf("shopee:new_order:%d:%s", snap.ShopID, strings.TrimSpace(snap.OrderSN))
	}
	message := BuildShopeeNewOrderLineTextWithPayment(snap, payment, s.publicBaseURL)
	altText := ""
	var flexPayload json.RawMessage
	payloadVersion := 0
	if s.richFlexEnabled {
		if alt, contents := BuildShopeeNewOrderRichLineFlexWithPayment(snap, payment, s.publicBaseURL); contents != nil {
			if raw, err := json.Marshal(contents); err == nil {
				altText = alt
				flexPayload = raw
				payloadVersion = 1
				if shopeePaymentReady(payment) {
					payloadVersion = 2
				}
			} else if s.logger != nil {
				s.logger.Warn("line notification rich flex marshal failed",
					zap.Int64("shop_id", snap.ShopID),
					zap.String("order_sn", snap.OrderSN),
					zap.Error(err),
				)
			}
		}
	}
	return s.repo.Enqueue(ctx, models.LineNotificationMessageInput{
		Source:         "shopee_realtime",
		Severity:       "info",
		Title:          "มีออเดอร์ Shopee ใหม่",
		Body:           shopeeLineNotificationBody(snap),
		ActionURL:      ShopeeOrderActionURL(s.publicBaseURL, snap.OrderSN),
		EntityType:     "shopee_order",
		EntityID:       fmt.Sprintf("%d:%s", snap.ShopID, strings.TrimSpace(snap.OrderSN)),
		DedupeKey:      dedupeKey,
		MessageText:    message,
		AltText:        altText,
		FlexPayload:    flexPayload,
		PayloadVersion: payloadVersion,
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

func (s *Service) EnqueueShopeeSettlementReady(ctx context.Context, run models.ShopeeSettlementLineRun, dedupeKey string) (int, error) {
	if s == nil || s.repo == nil || !s.settlementLineAlertsEnabled || strings.TrimSpace(run.ID) == "" || run.TotalCount <= 0 {
		return 0, nil
	}
	dedupeKey = strings.TrimSpace(dedupeKey)
	if dedupeKey == "" {
		dedupeKey = "shopee:settlement:" + strings.TrimSpace(run.ID)
	}
	message := BuildShopeeSettlementLineText(run, s.publicBaseURL)
	altText := ""
	var flexPayload json.RawMessage
	payloadVersion := 0
	if s.richFlexEnabled {
		if alt, contents := BuildShopeeSettlementLineFlex(run, s.publicBaseURL); contents != nil {
			if raw, err := json.Marshal(contents); err == nil {
				altText = alt
				flexPayload = raw
				payloadVersion = 1
			} else if s.logger != nil {
				s.logger.Warn("line settlement flex marshal failed",
					zap.String("run_id", run.ID),
					zap.Int64("shop_id", run.ShopID),
					zap.Error(err),
				)
			}
		}
	}
	return s.repo.Enqueue(ctx, models.LineNotificationMessageInput{
		Source:         "shopee_settlement",
		Severity:       settlementSeverity(run),
		Title:          "Shopee settlement พร้อมตรวจยอด",
		Body:           shopeeSettlementNotificationBody(run),
		ActionURL:      ShopeeSettlementActionURL(s.publicBaseURL),
		EntityType:     "shopee_settlement",
		EntityID:       strings.TrimSpace(run.ID),
		DedupeKey:      dedupeKey,
		MessageText:    message,
		AltText:        altText,
		FlexPayload:    flexPayload,
		PayloadVersion: payloadVersion,
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
			err = s.pushDelivery(ctx, svc, job)
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

type linePusher interface {
	PushFlex(to, altText string, contents map[string]any) error
	PushText(to, text string) error
}

func (s *Service) pushDelivery(ctx context.Context, svc linePusher, job models.LineNotificationDeliveryJob) error {
	_ = ctx
	if s.richFlexEnabled && job.PayloadVersion > 0 && len(job.FlexPayload) > 0 {
		altText, contents, err := flexPayloadFromDelivery(job)
		if err == nil && contents != nil {
			if pushErr := svc.PushFlex(job.DestinationID, altText, contents); pushErr == nil {
				return nil
			} else if s.logger != nil {
				s.logger.Warn("line flex notification failed, falling back to text",
					zap.String("delivery_id", job.ID),
					zap.String("source", job.Source),
					zap.String("entity_type", job.EntityType),
					zap.String("entity_id", job.EntityID),
					zap.Int("payload_version", job.PayloadVersion),
					zap.Error(pushErr),
				)
			}
		} else if s.logger != nil {
			s.logger.Warn("line flex payload invalid, falling back to text",
				zap.String("delivery_id", job.ID),
				zap.String("source", job.Source),
				zap.String("entity_type", job.EntityType),
				zap.String("entity_id", job.EntityID),
				zap.Int("payload_version", job.PayloadVersion),
				zap.Error(err),
			)
		}
	}
	return svc.PushText(job.DestinationID, job.MessageText)
}

func flexPayloadFromDelivery(job models.LineNotificationDeliveryJob) (string, map[string]any, error) {
	var contents map[string]any
	if err := json.Unmarshal(job.FlexPayload, &contents); err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(fmt.Sprint(contents["type"])) == "" {
		return "", nil, fmt.Errorf("LINE flex payload missing type")
	}
	altText := strings.TrimSpace(job.AltText)
	if altText == "" {
		altText = BuildShopeeNewOrderAltText(job.MessageText)
	}
	if altText == "" {
		altText = "แจ้งเตือน Nexflow"
	}
	return truncateRunes(altText, 400), contents, nil
}

func BuildShopeeNewOrderLineText(snap *models.ShopeeOrderSnapshot, publicBaseURL string) string {
	return BuildShopeeNewOrderLineTextWithPayment(snap, nil, publicBaseURL)
}

func BuildShopeeNewOrderLineTextWithPayment(snap *models.ShopeeOrderSnapshot, payment *models.ShopeeOrderPaymentSnapshot, publicBaseURL string) string {
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
	paymentLabel := shopeePaymentLabel(snap.PaymentMethod, detail.PaymentMethod, detail.COD)
	items := shopeeProductLines(detail.Items, snap.ItemCount, 3)
	parts := []string{
		"มีออเดอร์ Shopee ใหม่",
		"ร้าน: " + fallbackDash(shop),
		"Order SN: " + fallbackDash(strings.TrimSpace(snap.OrderSN)),
	}
	if amount != "" {
		parts = append(parts, amount)
	}
	if paymentLabel != "" {
		parts = append(parts, "ชำระเงิน: "+paymentLabel)
	}
	if shopeePaymentReady(payment) {
		parts = append(parts, shopeeOrderPaymentTextLines(payment)...)
	}
	if detail.PayTime > 0 {
		parts = append(parts, "เวลาชำระ: "+formatShopeeUnixTime(detail.PayTime))
	}
	if line := shopeeShippingFeeText(detail); line != "" {
		parts = append(parts, line)
	}
	if carrier := firstNonEmpty(snap.ShippingCarrier, detail.ShippingCarrier, detail.CheckoutCarrier, firstPackageCarrier(detail.Packages)); carrier != "" {
		parts = append(parts, "ขนส่ง: "+carrier)
	}
	if pkg := firstNonEmpty(snap.PackageNumber, firstPackageNumberRaw(detail.Packages)); pkg != "" {
		parts = append(parts, "Package: "+pkg)
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

func BuildShopeeNewOrderRichLineFlex(snap *models.ShopeeOrderSnapshot, publicBaseURL string) (string, map[string]any) {
	return BuildShopeeNewOrderRichLineFlexWithPayment(snap, nil, publicBaseURL)
}

func BuildShopeeNewOrderRichLineFlexWithPayment(snap *models.ShopeeOrderSnapshot, payment *models.ShopeeOrderPaymentSnapshot, publicBaseURL string) (string, map[string]any) {
	if snap == nil {
		return "มีออเดอร์ Shopee ใหม่", nil
	}
	detail := parseShopeeRawOrderDetail(snap.RawDetail)
	shop := strings.TrimSpace(snap.ShopLabel)
	if shop == "" && snap.ShopID > 0 {
		shop = fmt.Sprintf("shop_id %d", snap.ShopID)
	}
	orderSN := firstNonEmpty(strings.TrimSpace(snap.OrderSN), detail.OrderSN)
	total := snap.TotalAmount
	if total <= 0 {
		total = detail.TotalAmount
	}
	paymentLabel := shopeePaymentLabel(snap.PaymentMethod, detail.PaymentMethod, detail.COD)
	actionURL := ShopeeOrderActionURL(publicBaseURL, orderSN)
	amountLabel := formatTHB(total)
	title := "ออเดอร์ Shopee ใหม่"
	alt := strings.Join(filterNonEmpty([]string{title, shop, amountLabel}), " · ")
	if alt == "" {
		alt = "มีออเดอร์ Shopee ใหม่"
	}

	body := []map[string]any{
		flexText(title, "lg", "bold", "#0F172A", "", true),
		flexText(fallbackDash(shop), "sm", "", "#64748B", "", true),
		flexAmountRow("ยอดลูกค้าชำระ", amountLabel, "#2563EB"),
	}
	body = appendFlexSection(body, "คำสั่งซื้อ", []flexKVRow{
		{"Order SN", orderSN},
		{"วันที่สั่ง", formatShopeeUnixTime(detail.CreateTime)},
		{"วันที่ชำระ", formatShopeeUnixTime(detail.PayTime)},
	})
	body = appendFlexSection(body, "การชำระเงิน", shopeePaymentRows(paymentLabel, detail, total))
	if shopeePaymentReady(payment) {
		body = appendFlexSection(body, "ข้อมูลการชำระเงิน Shopee", shopeeOrderPaymentRows(payment))
	}
	body = appendFlexSection(body, "จัดส่ง", shopeeShippingRows(snap, detail))

	body = append(body,
		map[string]any{"type": "separator", "margin": "md"},
		flexText("สินค้า", "sm", "bold", "#334155", "md", true),
	)
	for _, item := range shopeeRichItemRows(detail.Items, snap.ItemCount, 5) {
		body = append(body, flexText(item, "sm", "", "#0F172A", "", true))
	}

	contents := map[string]any{
		"type": "bubble",
		"size": "mega",
		"body": map[string]any{
			"type":     "box",
			"layout":   "vertical",
			"spacing":  "sm",
			"contents": body,
		},
	}
	if isAbsoluteHTTPURL(actionURL) {
		contents["footer"] = flexButtonFooter("เปิดใน Nexflow", actionURL)
	}
	return alt, contents
}

type flexKVRow struct {
	Label string
	Value string
}

func appendFlexSection(body []map[string]any, title string, rows []flexKVRow) []map[string]any {
	filtered := make([]flexKVRow, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.Value) != "" && strings.TrimSpace(row.Value) != "-" {
			filtered = append(filtered, row)
		}
	}
	if len(filtered) == 0 {
		return body
	}
	body = append(body,
		map[string]any{"type": "separator", "margin": "md"},
		flexText(title, "sm", "bold", "#334155", "md", true),
	)
	for _, row := range filtered {
		body = append(body, flexKeyValue(row.Label, row.Value))
	}
	return body
}

func flexText(text, size, weight, color, margin string, wrap bool) map[string]any {
	out := map[string]any{
		"type":  "text",
		"text":  fallbackDash(truncateRunes(text, 220)),
		"size":  firstNonEmpty(size, "sm"),
		"color": firstNonEmpty(color, "#0F172A"),
		"wrap":  wrap,
	}
	if weight != "" {
		out["weight"] = weight
	}
	if margin != "" {
		out["margin"] = margin
	}
	return out
}

func flexAmountRow(label, value, color string) map[string]any {
	return map[string]any{
		"type":    "box",
		"layout":  "horizontal",
		"margin":  "md",
		"spacing": "sm",
		"contents": []map[string]any{
			{"type": "text", "text": label, "size": "xs", "color": "#64748B", "flex": 2, "wrap": true},
			{"type": "text", "text": fallbackDash(value), "size": "xl", "weight": "bold", "color": firstNonEmpty(color, "#2563EB"), "align": "end", "flex": 3, "wrap": true},
		},
	}
}

func flexKeyValue(label, value string) map[string]any {
	return map[string]any{
		"type":    "box",
		"layout":  "horizontal",
		"spacing": "sm",
		"contents": []map[string]any{
			{"type": "text", "text": truncateRunes(label, 40), "size": "xs", "color": "#64748B", "flex": 2, "wrap": true},
			{"type": "text", "text": truncateRunes(value, 120), "size": "xs", "color": "#0F172A", "align": "end", "flex": 3, "wrap": true},
		},
	}
}

func flexButtonFooter(label, uri string) map[string]any {
	return map[string]any{
		"type":   "box",
		"layout": "vertical",
		"contents": []map[string]any{
			{
				"type":   "button",
				"style":  "primary",
				"color":  "#2563EB",
				"height": "sm",
				"action": map[string]any{"type": "uri", "label": label, "uri": uri},
			},
		},
	}
}

func BuildShopeeSettlementLineText(run models.ShopeeSettlementLineRun, publicBaseURL string) string {
	totals := settlementTotals(run)
	parts := []string{
		"Shopee settlement พร้อมตรวจยอด",
		"ร้าน: " + fallbackDash(settlementShopLabel(run)),
	}
	if run.ReleaseDateFrom != "" || run.ReleaseDateTo != "" {
		parts = append(parts, "ช่วง release: "+fallbackDash(settlementReleaseRange(run)))
	}
	parts = append(parts,
		fmt.Sprintf("จำนวนรายการ: %d", run.TotalCount),
		"ยอดลูกค้าชำระ: "+formatTHBValue(totals.BuyerTotal),
		"ยอดสุทธิร้านได้: "+formatTHBValue(totals.Payout),
		"ยอดหัก/ส่วนต่าง: "+formatTHBValue(totals.Deduction),
	)
	if fees := settlementFeeTextLines(run.Items, 4); len(fees) > 0 {
		parts = append(parts, "รายละเอียดยอดหัก:")
		parts = append(parts, fees...)
	}
	rows := settlementItemTextLines(run.Items, 5)
	if len(rows) > 0 {
		parts = append(parts, "ตัวอย่างรายการ:")
		parts = append(parts, rows...)
	}
	if url := ShopeeSettlementActionURL(publicBaseURL); url != "" {
		parts = append(parts, "เปิดใน Nexflow: "+url)
	}
	return strings.Join(parts, "\n")
}

func BuildShopeeSettlementLineFlex(run models.ShopeeSettlementLineRun, publicBaseURL string) (string, map[string]any) {
	if strings.TrimSpace(run.ID) == "" {
		return "Shopee settlement พร้อมตรวจยอด", nil
	}
	totals := settlementTotals(run)
	title := "Shopee settlement พร้อมตรวจยอด"
	shop := settlementShopLabel(run)
	alt := strings.Join(filterNonEmpty([]string{title, shop, formatTHBValue(totals.Payout)}), " · ")
	if alt == "" {
		alt = title
	}
	body := []map[string]any{
		flexText(title, "lg", "bold", "#0F172A", "", true),
		flexText(fallbackDash(shop), "sm", "", "#64748B", "", true),
		flexAmountRow("ยอดสุทธิร้านได้", formatTHBValue(totals.Payout), "#059669"),
	}
	body = appendFlexSection(body, "ยอดรวม", []flexKVRow{
		{"ช่วง release", settlementReleaseRange(run)},
		{"จำนวนรายการ", fmt.Sprintf("%d รายการ", run.TotalCount)},
		{"ยอดลูกค้าชำระ", formatTHBValue(totals.BuyerTotal)},
		{"ยอดหัก/ส่วนต่าง", formatTHBValue(totals.Deduction)},
		{"สถานะ", settlementStatusSummary(run)},
	})
	body = appendFlexSection(body, "ยอดหักจาก Shopee", settlementFeeRows(run.Items, 6))
	body = append(body,
		map[string]any{"type": "separator", "margin": "md"},
		flexText("ตัวอย่างรายการ", "sm", "bold", "#334155", "md", true),
	)
	for _, line := range settlementItemTextLines(run.Items, 5) {
		body = append(body, flexText(line, "xs", "", "#0F172A", "", true))
	}
	if len(run.Items) == 0 {
		body = append(body, flexText("เปิดดูรายการใน Nexflow", "xs", "", "#0F172A", "", true))
	}
	contents := map[string]any{
		"type": "bubble",
		"size": "mega",
		"body": map[string]any{
			"type":     "box",
			"layout":   "vertical",
			"spacing":  "sm",
			"contents": body,
		},
	}
	if url := ShopeeSettlementActionURL(publicBaseURL); isAbsoluteHTTPURL(url) {
		contents["footer"] = flexButtonFooter("เปิดรับชำระ Shopee", url)
	}
	return alt, contents
}

type settlementAmountTotals struct {
	BuyerTotal float64
	Payout     float64
	Deduction  float64
}

func settlementTotals(run models.ShopeeSettlementLineRun) settlementAmountTotals {
	totals := settlementAmountTotals{
		BuyerTotal: run.BuyerTotalAmountTotal,
		Payout:     run.PayoutAmountTotal,
		Deduction:  run.DeductionAmountTotal,
	}
	if totals.BuyerTotal != 0 || totals.Payout != 0 || totals.Deduction != 0 {
		return totals
	}
	for _, item := range run.Items {
		totals.BuyerTotal += item.BuyerTotalAmount
		totals.Payout += item.PayoutAmount
		totals.Deduction += item.DeductionAmount
	}
	totals.BuyerTotal = roundMoney(totals.BuyerTotal)
	totals.Payout = roundMoney(totals.Payout)
	totals.Deduction = roundMoney(totals.Deduction)
	return totals
}

func settlementFeeRows(items []models.ShopeeSettlementLineItem, limit int) []flexKVRow {
	lines := settlementFeeTextLines(items, limit)
	rows := make([]flexKVRow, 0, len(lines))
	for _, line := range lines {
		label, value, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		rows = append(rows, flexKVRow{label, value})
	}
	return rows
}

func settlementFeeTextLines(items []models.ShopeeSettlementLineItem, limit int) []string {
	if limit <= 0 {
		limit = 6
	}
	fees := map[string]float64{}
	order := []struct {
		key   string
		label string
	}{
		{"commission_fee", "Commission"},
		{"service_fee", "Service fee"},
		{"seller_transaction_fee", "Transaction"},
		{"actual_shipping_fee", "ค่าส่งจริง"},
		{"final_shipping_fee", "ค่าส่งสุทธิ"},
		{"reverse_shipping_fee", "ค่าส่งคืน"},
		{"escrow_tax", "ภาษี escrow"},
		{"withholding_tax", "หัก ณ ที่จ่าย"},
		{"voucher_from_seller", "Voucher ร้าน"},
		{"shopee_shipping_rebate", "Shipping rebate"},
	}
	for _, item := range items {
		values := parseSettlementFeeValues(item.RawEscrow)
		for k, v := range values {
			fees[k] += v
		}
	}
	out := []string{}
	for _, field := range order {
		v := roundMoney(fees[field.key])
		if math.Abs(v) < 0.005 {
			continue
		}
		out = append(out, field.label+": "+formatTHBValue(v))
		if len(out) >= limit {
			break
		}
	}
	return out
}

func parseSettlementFeeValues(raw json.RawMessage) map[string]float64 {
	if len(raw) == 0 {
		return map[string]float64{}
	}
	var decoded struct {
		OrderIncome map[string]float64 `json:"order_income"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil || len(decoded.OrderIncome) == 0 {
		return map[string]float64{}
	}
	return decoded.OrderIncome
}

func settlementItemTextLines(items []models.ShopeeSettlementLineItem, limit int) []string {
	if limit <= 0 {
		limit = 5
	}
	out := make([]string, 0, minInt(len(items), limit)+1)
	for i, item := range items {
		if i >= limit {
			break
		}
		line := fmt.Sprintf("%d. %s · รับ %s", i+1, fallbackDash(item.OrderSN), formatTHBValue(item.PayoutAmount))
		if item.BuyerTotalAmount > 0 {
			line += " จาก " + formatTHBValue(item.BuyerTotalAmount)
		}
		if item.DeductionAmount != 0 {
			line += " · หัก " + formatTHBValue(item.DeductionAmount)
		}
		if item.Status != "" {
			line += " · " + settlementItemStatusLabel(item.Status)
		}
		out = append(out, truncateRunes(line, 140))
	}
	if len(items) > limit {
		out = append(out, fmt.Sprintf("และอีก %d รายการ", len(items)-limit))
	}
	return out
}

func settlementShopLabel(run models.ShopeeSettlementLineRun) string {
	if strings.TrimSpace(run.ShopLabel) != "" {
		return strings.TrimSpace(run.ShopLabel)
	}
	if run.ShopID > 0 {
		return fmt.Sprintf("shop_id %d", run.ShopID)
	}
	return ""
}

func settlementReleaseRange(run models.ShopeeSettlementLineRun) string {
	from := strings.TrimSpace(run.ReleaseDateFrom)
	to := strings.TrimSpace(run.ReleaseDateTo)
	if from != "" && to != "" && from != to {
		return from + " ถึง " + to
	}
	return firstNonEmpty(from, to)
}

func settlementStatusSummary(run models.ShopeeSettlementLineRun) string {
	parts := []string{}
	if run.ReadyCount > 0 {
		parts = append(parts, fmt.Sprintf("พร้อม %d", run.ReadyCount))
	}
	if run.BlockedCount > 0 {
		parts = append(parts, fmt.Sprintf("ติดขัด %d", run.BlockedCount))
	}
	if run.SentCount > 0 {
		parts = append(parts, fmt.Sprintf("ส่งแล้ว %d", run.SentCount))
	}
	return strings.Join(parts, " · ")
}

func settlementItemStatusLabel(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "ready":
		return "พร้อมรับชำระ"
	case "blocked":
		return "ติดขัด"
	case "sent":
		return "ส่งแล้ว"
	case "failed":
		return "ล้มเหลว"
	default:
		return fallbackDash(status)
	}
}

func settlementSeverity(run models.ShopeeSettlementLineRun) string {
	if run.ReadyCount == 0 && run.BlockedCount > 0 {
		return "warning"
	}
	return "info"
}

func shopeeSettlementNotificationBody(run models.ShopeeSettlementLineRun) string {
	totals := settlementTotals(run)
	return strings.Join(filterNonEmpty([]string{
		settlementShopLabel(run),
		fmt.Sprintf("%d รายการ", run.TotalCount),
		"รับ " + formatTHBValue(totals.Payout),
		"หัก " + formatTHBValue(totals.Deduction),
	}), " · ")
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

func ShopeeSettlementActionURL(publicBaseURL string) string {
	path := "/shopee-settlements"
	base := strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
	if base == "" {
		return path
	}
	return base + path
}

type shopeeRawOrderDetail struct {
	OrderSN              string
	OrderStatus          string
	PaymentMethod        string
	COD                  bool
	TotalAmount          float64
	Currency             string
	CreateTime           int64
	PayTime              int64
	ActualShippingFee    float64
	EstimatedShippingFee float64
	ReverseShippingFee   float64
	TrackingNumber       string
	ShippingCarrier      string
	CheckoutCarrier      string
	Packages             []shopeeRawOrderPackage
	Items                []shopeeRawOrderItem
}

type shopeeRawOrderItem struct {
	ItemName        string
	Model           string
	Qty             float64
	OriginalPrice   float64
	DiscountedPrice float64
	ItemSKU         string
	ModelSKU        string
}

type shopeeRawOrderPackage struct {
	PackageNumber              string
	LogisticsStatus            string
	ShippingCarrier            string
	TrackingNumber             string
	ParcelChargeableWeightGram float64
}

func parseShopeeRawOrderDetail(raw json.RawMessage) shopeeRawOrderDetail {
	if len(raw) == 0 {
		return shopeeRawOrderDetail{}
	}
	var decoded struct {
		OrderSN              string  `json:"order_sn"`
		OrderStatus          string  `json:"order_status"`
		PaymentMethod        string  `json:"payment_method"`
		COD                  bool    `json:"cod"`
		TotalAmount          float64 `json:"total_amount"`
		Currency             string  `json:"currency"`
		CreateTime           int64   `json:"create_time"`
		PayTime              int64   `json:"pay_time"`
		ActualShippingFee    float64 `json:"actual_shipping_fee"`
		EstimatedShippingFee float64 `json:"estimated_shipping_fee"`
		ReverseShippingFee   float64 `json:"reverse_shipping_fee"`
		TrackingNumber       string  `json:"tracking_number"`
		ShippingCarrier      string  `json:"shipping_carrier"`
		CheckoutCarrier      string  `json:"checkout_shipping_carrier"`
		PackageList          []struct {
			PackageNumber              string  `json:"package_number"`
			LogisticsStatus            string  `json:"logistics_status"`
			ShippingCarrier            string  `json:"shipping_carrier"`
			TrackingNumber             string  `json:"tracking_number"`
			ParcelChargeableWeightGram float64 `json:"parcel_chargeable_weight_gram"`
		} `json:"package_list"`
		ItemList []struct {
			ItemName               string  `json:"item_name"`
			ItemSKU                string  `json:"item_sku"`
			ModelName              string  `json:"model_name"`
			ModelSKU               string  `json:"model_sku"`
			ModelQuantityPurchased float64 `json:"model_quantity_purchased"`
			ModelOriginalPrice     float64 `json:"model_original_price"`
			ModelDiscountedPrice   float64 `json:"model_discounted_price"`
		} `json:"item_list"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return shopeeRawOrderDetail{}
	}
	out := shopeeRawOrderDetail{
		OrderSN:              compactWhitespace(decoded.OrderSN),
		OrderStatus:          compactWhitespace(decoded.OrderStatus),
		PaymentMethod:        decoded.PaymentMethod,
		COD:                  decoded.COD,
		TotalAmount:          decoded.TotalAmount,
		Currency:             compactWhitespace(decoded.Currency),
		CreateTime:           decoded.CreateTime,
		PayTime:              decoded.PayTime,
		ActualShippingFee:    decoded.ActualShippingFee,
		EstimatedShippingFee: decoded.EstimatedShippingFee,
		ReverseShippingFee:   decoded.ReverseShippingFee,
		TrackingNumber:       compactWhitespace(decoded.TrackingNumber),
		ShippingCarrier:      compactWhitespace(decoded.ShippingCarrier),
		CheckoutCarrier:      compactWhitespace(decoded.CheckoutCarrier),
	}
	for _, pkg := range decoded.PackageList {
		out.Packages = append(out.Packages, shopeeRawOrderPackage{
			PackageNumber:              compactWhitespace(pkg.PackageNumber),
			LogisticsStatus:            compactWhitespace(pkg.LogisticsStatus),
			ShippingCarrier:            compactWhitespace(pkg.ShippingCarrier),
			TrackingNumber:             compactWhitespace(pkg.TrackingNumber),
			ParcelChargeableWeightGram: pkg.ParcelChargeableWeightGram,
		})
	}
	for _, item := range decoded.ItemList {
		name := compactWhitespace(item.ItemName)
		if name == "" {
			continue
		}
		out.Items = append(out.Items, shopeeRawOrderItem{
			ItemName:        name,
			Model:           compactWhitespace(item.ModelName),
			Qty:             item.ModelQuantityPurchased,
			OriginalPrice:   item.ModelOriginalPrice,
			DiscountedPrice: item.ModelDiscountedPrice,
			ItemSKU:         compactWhitespace(item.ItemSKU),
			ModelSKU:        compactWhitespace(item.ModelSKU),
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

func shopeeRichItemRows(items []shopeeRawOrderItem, itemCount, limit int) []string {
	if limit <= 0 {
		limit = 5
	}
	if len(items) == 0 {
		if itemCount > 0 {
			return []string{fmt.Sprintf("%d รายการ", itemCount)}
		}
		return []string{"เปิดดูรายละเอียดสินค้าใน Nexflow"}
	}
	out := make([]string, 0, minInt(len(items), limit)+1)
	for i, item := range items {
		if i >= limit {
			break
		}
		name := truncateRunes(item.ItemName, 72)
		if shouldAppendModel(name, item.Model) {
			suffix := " (" + item.Model + ")"
			name = truncateRunes(item.ItemName, maxInt(24, 72-len([]rune(suffix)))) + suffix
		}
		price := item.DiscountedPrice
		if price <= 0 {
			price = item.OriginalPrice
		}
		line := fmt.Sprintf("%d. %s x%s", i+1, name, formatShopeeQty(item.Qty))
		if price > 0 {
			line += " · " + formatTHB(price)
		}
		out = append(out, line)
	}
	if len(items) > limit {
		out = append(out, fmt.Sprintf("และอีก %d รายการ", len(items)-limit))
	}
	return out
}

func shopeePaymentRows(payment string, detail shopeeRawOrderDetail, total float64) []flexKVRow {
	rows := []flexKVRow{
		{"ช่องทาง", payment},
		{"COD", boolThai(detail.COD)},
		{"ยอดลูกค้าชำระ", formatTHB(total)},
	}
	if detail.ActualShippingFee > 0 {
		rows = append(rows, flexKVRow{"ค่าส่งจริง", formatTHB(detail.ActualShippingFee)})
	} else if detail.EstimatedShippingFee > 0 {
		rows = append(rows, flexKVRow{"ค่าส่งประมาณการ", formatTHB(detail.EstimatedShippingFee)})
	}
	if detail.ReverseShippingFee > 0 {
		rows = append(rows, flexKVRow{"ค่าส่งคืน", formatTHB(detail.ReverseShippingFee)})
	}
	return rows
}

func shopeePaymentReady(payment *models.ShopeeOrderPaymentSnapshot) bool {
	return payment != nil && strings.TrimSpace(payment.Status) == "ready"
}

func shopeeOrderPaymentTextLines(payment *models.ShopeeOrderPaymentSnapshot) []string {
	if !shopeePaymentReady(payment) {
		return nil
	}
	lines := []string{
		"ยอดสุทธิตาม Shopee escrow: " + formatTHBValue(payment.EscrowAmount),
		"ส่วนต่างจากยอดลูกค้าชำระ: " + formatSignedTHB(payment.DeductionAmount),
	}
	for _, row := range shopeeOrderPaymentFeeRows(payment) {
		lines = append(lines, row.Label+": "+row.Value)
	}
	return lines
}

func shopeeOrderPaymentRows(payment *models.ShopeeOrderPaymentSnapshot) []flexKVRow {
	if !shopeePaymentReady(payment) {
		return nil
	}
	rows := []flexKVRow{
		{"ยอดลูกค้าชำระ", formatTHBValue(payment.BuyerTotalAmount)},
		{"ยอดสุทธิตาม Shopee escrow", formatTHBValue(payment.EscrowAmount)},
		{"ส่วนต่างจากยอดลูกค้าชำระ", formatSignedTHB(payment.DeductionAmount)},
	}
	rows = append(rows, shopeeOrderPaymentFeeRows(payment)...)
	return rows
}

func shopeeOrderPaymentFeeRows(payment *models.ShopeeOrderPaymentSnapshot) []flexKVRow {
	if payment == nil {
		return nil
	}
	candidates := []struct {
		label string
		value float64
	}{
		{"Commission", payment.CommissionFee},
		{"Transaction fee", payment.SellerTransactionFee},
		{"Service fee", payment.ServiceFee},
		{"Voucher Shopee", payment.VoucherFromShopee},
		{"Voucher ร้าน", payment.VoucherFromSeller},
		{"ส่วนลด Shopee", payment.ShopeeDiscount},
		{"ส่วนลดร้าน", payment.SellerDiscount},
		{"ค่าส่งลูกค้าจ่าย", payment.BuyerPaidShippingFee},
		{"ส่วนลดค่าส่งร้าน", payment.SellerShippingDiscount},
		{"ค่าส่งจริง", payment.ActualShippingFee},
		{"ค่าส่งสุทธิ", payment.FinalShippingFee},
		{"ค่าส่งคืน", payment.ReverseShippingFee},
		{"ภาษี escrow", payment.EscrowTax},
		{"หัก ณ ที่จ่าย", payment.WithholdingTax},
		{"Coin", payment.Coin},
	}
	rows := make([]flexKVRow, 0, len(candidates))
	for _, candidate := range candidates {
		if roundMoney(candidate.value) == 0 {
			continue
		}
		rows = append(rows, flexKVRow{candidate.label, formatTHBValue(candidate.value)})
		if len(rows) >= 8 {
			break
		}
	}
	return rows
}

func shopeeShippingRows(snap *models.ShopeeOrderSnapshot, detail shopeeRawOrderDetail) []flexKVRow {
	var pkg shopeeRawOrderPackage
	if len(detail.Packages) > 0 {
		pkg = detail.Packages[0]
	}
	carrier := firstNonEmpty(snap.ShippingCarrier, detail.ShippingCarrier, detail.CheckoutCarrier, pkg.ShippingCarrier)
	tracking := firstNonEmpty(snap.TrackingNumber, detail.TrackingNumber, pkg.TrackingNumber)
	return []flexKVRow{
		{"ขนส่ง", carrier},
		{"Package", firstNonEmpty(snap.PackageNumber, pkg.PackageNumber)},
		{"Tracking", tracking},
		{"สถานะขนส่ง", firstNonEmpty(snap.LogisticsStatus, pkg.LogisticsStatus)},
		{"น้ำหนักคิดเงิน", formatGram(pkg.ParcelChargeableWeightGram)},
	}
}

func shopeeShippingFeeText(detail shopeeRawOrderDetail) string {
	if detail.ActualShippingFee > 0 {
		return "ค่าส่งจริง: " + formatTHB(detail.ActualShippingFee)
	}
	if detail.EstimatedShippingFee > 0 {
		return "ค่าส่งประมาณการ: " + formatTHB(detail.EstimatedShippingFee)
	}
	return ""
}

func firstPackageCarrier(packages []shopeeRawOrderPackage) string {
	for _, pkg := range packages {
		if pkg.ShippingCarrier != "" {
			return pkg.ShippingCarrier
		}
	}
	return ""
}

func firstPackageNumberRaw(packages []shopeeRawOrderPackage) string {
	for _, pkg := range packages {
		if pkg.PackageNumber != "" {
			return pkg.PackageNumber
		}
	}
	return ""
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

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func boolThai(v bool) string {
	if v {
		return "ใช่"
	}
	return "ไม่ใช่"
}

func formatTHB(v float64) string {
	if v <= 0 {
		return ""
	}
	return fmt.Sprintf("฿%.2f", v)
}

func formatTHBValue(v float64) string {
	return fmt.Sprintf("฿%.2f", roundMoney(v))
}

func formatSignedTHB(v float64) string {
	v = roundMoney(v)
	if v < 0 {
		return fmt.Sprintf("-฿%.2f", math.Abs(v))
	}
	return fmt.Sprintf("฿%.2f", v)
}

func roundMoney(v float64) float64 {
	return math.Round(v*100) / 100
}

func formatGram(v float64) string {
	if v <= 0 {
		return ""
	}
	if math.Abs(v-math.Round(v)) < 0.000001 {
		return fmt.Sprintf("%.0f g", v)
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", v), "0"), ".") + " g"
}

func formatShopeeUnixTime(v int64) string {
	if v <= 0 {
		return ""
	}
	return time.Unix(v, 0).Format("02/01/2006 15:04")
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
