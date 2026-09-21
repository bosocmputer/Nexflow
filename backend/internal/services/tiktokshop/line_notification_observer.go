package tiktokshop

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"nexflow/internal/models"
)

type tikTokShopLabelStore interface {
	ActiveShopLabel(context.Context, string) (string, error)
}

type tikTokNewOrderLineNotifier interface {
	EnqueueTikTokShopNewOrder(context.Context, models.TikTokShopNewOrderNotification, string) (int, error)
}

type TikTokNewOrderLineObserver struct {
	enabled  bool
	cutoff   time.Time
	shops    tikTokShopLabelStore
	notifier tikTokNewOrderLineNotifier
	logger   *zap.Logger
}

func NewTikTokNewOrderLineObserver(
	enabled bool,
	cutoff time.Time,
	shops tikTokShopLabelStore,
	notifier tikTokNewOrderLineNotifier,
	logger *zap.Logger,
) *TikTokNewOrderLineObserver {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &TikTokNewOrderLineObserver{
		enabled: enabled, cutoff: cutoff.UTC(), shops: shops, notifier: notifier, logger: logger,
	}
}

func (o *TikTokNewOrderLineObserver) ObserveTikTokOrderSnapshot(ctx context.Context, shopID string, record TikTokOrderSnapshotRecord) error {
	shopID = strings.TrimSpace(shopID)
	orderID := strings.TrimSpace(record.OrderID)
	if o == nil || !o.enabled || o.cutoff.IsZero() || o.shops == nil || o.notifier == nil ||
		shopID == "" || orderID == "" || record.OrderCreatedAt == nil || record.OrderCreatedAt.Before(o.cutoff) ||
		!tikTokNewOrderNotificationEligibleStatus(record.OrderStatus) {
		return nil
	}
	shopLabel, err := o.shops.ActiveShopLabel(ctx, shopID)
	if err != nil {
		return fmt.Errorf("resolve TikTok Shop label: %w", err)
	}
	items := make([]models.TikTokShopNewOrderNotificationItem, 0, len(record.NormalizedItems))
	for _, item := range record.NormalizedItems {
		items = append(items, models.TikTokShopNewOrderNotificationItem{
			ProductName: item.ProductName, VariantName: item.SKUName, Quantity: item.Quantity,
		})
	}
	input := models.TikTokShopNewOrderNotification{
		ShopID: shopID, ShopName: shopLabel, OrderID: orderID, OrderStatus: string(record.OrderStatus),
		Currency: record.Currency, PaymentTotalAmount: record.PaymentTotal,
		ProductSubtotalAmount: record.ProductSubtotal, ShippingFeeAmount: record.ShippingFee,
		ItemCount: record.ItemCount, SKUCount: record.SKUCount, CreatedAt: record.OrderCreatedAt.UTC(),
		ObservationSource: normalizeTikTokObservationSource(record.ObservationSource), Items: items,
	}
	dedupeKey := fmt.Sprintf("tiktok_shop:new_order:%s:%s", shopID, orderID)
	inserted, err := o.notifier.EnqueueTikTokShopNewOrder(ctx, input, dedupeKey)
	if err != nil {
		return fmt.Errorf("enqueue TikTok Shop LINE notification: %w", err)
	}
	o.logger.Info("tiktok_shop_line_new_order_observed",
		zap.String("shop_id", shopID),
		zap.String("order_id", orderID),
		zap.String("order_status", string(record.OrderStatus)),
		zap.String("observation_source", input.ObservationSource),
		zap.Int("recipient_count", inserted),
	)
	return nil
}

func tikTokNewOrderNotificationEligibleStatus(status OrderStatus) bool {
	switch status {
	case OrderStatusAwaitingShipment, OrderStatusPartiallyShipping, OrderStatusAwaitingCollection,
		OrderStatusInTransit, OrderStatusDelivered, OrderStatusCompleted:
		return true
	default:
		return false
	}
}
