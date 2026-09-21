package tiktokshop

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/services/events"
)

type tikTokNotificationStore interface {
	CreateForRoles(context.Context, []string, models.NotificationInput) ([]models.Notification, error)
	UnreadCount(context.Context, string) (int, error)
	UnreadCountsBySource(context.Context, string) (map[string]int, error)
}

type tikTokNotificationPublisher interface {
	Publish(events.Event)
}

type TikTokNewOrderInAppObserver struct {
	enabled   bool
	cutoff    time.Time
	shops     tikTokShopLabelStore
	notifier  tikTokNotificationStore
	publisher tikTokNotificationPublisher
	logger    *zap.Logger
}

func NewTikTokNewOrderInAppObserver(
	enabled bool,
	cutoff time.Time,
	shops tikTokShopLabelStore,
	notifier tikTokNotificationStore,
	publisher tikTokNotificationPublisher,
	logger *zap.Logger,
) *TikTokNewOrderInAppObserver {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &TikTokNewOrderInAppObserver{
		enabled: enabled, cutoff: cutoff.UTC(), shops: shops, notifier: notifier, publisher: publisher, logger: logger,
	}
}

func (o *TikTokNewOrderInAppObserver) ObserveTikTokOrderSnapshot(ctx context.Context, shopID string, record TikTokOrderSnapshotRecord) error {
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
	shopLabel = strings.TrimSpace(shopLabel)
	if shopLabel == "" {
		shopLabel = "TikTok Shop " + shopID
	}
	entityID := shopID + ":" + orderID
	dedupeKey := "tiktok_shop:new_order:" + entityID
	created, err := o.notifier.CreateForRoles(ctx, []string{"admin", "staff"}, models.NotificationInput{
		Source:     "tiktok_shop",
		Severity:   "warning",
		Title:      "มีออเดอร์ TikTok Shop ใหม่",
		Body:       fmt.Sprintf("%s · ออเดอร์ %s · %d รายการ", shopLabel, orderID, record.ItemCount),
		ActionURL:  "/tiktok-shop-operations?order_id=" + url.QueryEscape(orderID),
		EntityType: "tiktok_shop_order",
		EntityID:   entityID,
		DedupeKey:  dedupeKey,
	})
	if err != nil {
		return fmt.Errorf("create TikTok Shop in-app notification: %w", err)
	}

	for _, notification := range created {
		unread, _ := o.notifier.UnreadCount(ctx, notification.RecipientID)
		bySource, _ := o.notifier.UnreadCountsBySource(ctx, notification.RecipientID)
		if bySource == nil {
			bySource = map[string]int{}
		}
		if o.publisher == nil {
			continue
		}
		o.publisher.Publish(events.Event{
			Type:         events.TypeNotificationCreated,
			TargetUserID: notification.RecipientID,
			Payload: map[string]any{
				"notification":     notification,
				"unread_count":     unread,
				"unread_by_source": bySource,
			},
		})
		o.publisher.Publish(events.Event{
			Type:         events.TypeNotificationUnreadChanged,
			TargetUserID: notification.RecipientID,
			Payload:      map[string]any{"total": unread, "unread_by_source": bySource},
		})
	}

	o.logger.Info("tiktok_shop_in_app_new_order_observed",
		zap.String("shop_id", shopID),
		zap.String("order_id", orderID),
		zap.String("order_status", string(record.OrderStatus)),
		zap.String("observation_source", normalizeTikTokObservationSource(record.ObservationSource)),
		zap.Int("recipient_count", len(created)),
	)
	return nil
}
