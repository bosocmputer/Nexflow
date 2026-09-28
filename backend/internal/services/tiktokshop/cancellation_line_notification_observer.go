package tiktokshop

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"nexflow/internal/models"
)

type tikTokCancellationLineNotifier interface {
	EnqueueTikTokShopOrderCancelled(context.Context, models.TikTokShopOrderCancellationNotification, string) (int, error)
}

type tikTokCancellationBillLookup interface {
	ExistingBillSMLDocNo(context.Context, string) (string, error)
}

// TikTokCancellationLineObserver emits a cancellation notice only for order
// updates observed after its cutover. The durable outbox dedupe key absorbs
// webhook/polling replays without relying on an in-memory transition cache.
type TikTokCancellationLineObserver struct {
	enabled  bool
	cutoff   time.Time
	shops    tikTokShopLabelStore
	bills    tikTokCancellationBillLookup
	notifier tikTokCancellationLineNotifier
	logger   *zap.Logger
}

func NewTikTokCancellationLineObserver(enabled bool, cutoff time.Time, shops tikTokShopLabelStore, bills tikTokCancellationBillLookup, notifier tikTokCancellationLineNotifier, logger *zap.Logger) *TikTokCancellationLineObserver {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &TikTokCancellationLineObserver{enabled: enabled, cutoff: cutoff.UTC(), shops: shops, bills: bills, notifier: notifier, logger: logger}
}

func (o *TikTokCancellationLineObserver) ObserveTikTokOrderSnapshot(ctx context.Context, shopID string, record TikTokOrderSnapshotRecord) error {
	shopID, orderID := strings.TrimSpace(shopID), strings.TrimSpace(record.OrderID)
	if o == nil || !o.enabled || o.cutoff.IsZero() || o.shops == nil || o.bills == nil || o.notifier == nil ||
		shopID == "" || orderID == "" || record.OrderStatus != OrderStatusCancelled ||
		record.LastOrderUpdateAt == nil || record.LastOrderUpdateAt.Before(o.cutoff) ||
		normalizeTikTokObservationSource(record.ObservationSource) == "settlement_backfill" {
		return nil
	}
	shopLabel, err := o.shops.ActiveShopLabel(ctx, shopID)
	if err != nil {
		return fmt.Errorf("resolve TikTok Shop label: %w", err)
	}
	smlDocNo, err := o.bills.ExistingBillSMLDocNo(ctx, orderID)
	if err != nil {
		return fmt.Errorf("load TikTok Shop cancellation bill status: %w", err)
	}
	input := models.TikTokShopOrderCancellationNotification{
		ShopID: shopID, ShopName: shopLabel, OrderID: orderID,
		Currency: record.Currency, PaymentTotalAmount: record.PaymentTotal,
		ItemCount: record.ItemCount, SKUCount: record.SKUCount,
		SMLDocNo: smlDocNo, OrderUpdatedAt: record.LastOrderUpdateAt.UTC(),
	}
	inserted, err := o.notifier.EnqueueTikTokShopOrderCancelled(ctx, input, fmt.Sprintf("tiktok_shop:cancelled:%s:%s", shopID, orderID))
	if err != nil {
		return fmt.Errorf("enqueue TikTok Shop cancellation LINE notification: %w", err)
	}
	o.logger.Info("tiktok_shop_line_cancellation_observed", zap.String("shop_id", shopID), zap.String("order_id", orderID), zap.String("observation_source", normalizeTikTokObservationSource(record.ObservationSource)), zap.Int("recipient_count", inserted))
	return nil
}
