package linenotify

import (
	"context"
	"fmt"
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
		Body:        fmt.Sprintf("%s · %s · %.2f", snap.OrderSN, snap.OrderStatus, snap.TotalAmount),
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
			err = svc.PushText(job.DestinationID, job.MessageText)
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
	shop := strings.TrimSpace(snap.ShopLabel)
	if shop == "" && snap.ShopID > 0 {
		shop = fmt.Sprintf("shop_id %d", snap.ShopID)
	}
	amount := ""
	if snap.TotalAmount > 0 {
		amount = fmt.Sprintf("ยอดรวม: ฿%.2f", snap.TotalAmount)
	}
	parts := []string{
		"มีออเดอร์ Shopee ใหม่",
		"ร้าน: " + fallbackDash(shop),
		"Order SN: " + fallbackDash(strings.TrimSpace(snap.OrderSN)),
		"สถานะ Shopee: " + fallbackDash(strings.TrimSpace(snap.OrderStatus)),
		"สถานะ ERP: " + erpStatusLabel(snap.ERPStatus),
	}
	if amount != "" {
		parts = append(parts, amount)
	}
	if url := ShopeeOrderActionURL(publicBaseURL, snap.OrderSN); url != "" {
		parts = append(parts, "เปิดใน Nexflow: "+url)
	}
	return strings.Join(parts, "\n")
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
