package tiktokshop

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.uber.org/zap"
)

type TikTokWebhookJobStore interface {
	ClaimDue(context.Context, time.Time) (TikTokWebhookJob, error)
	MarkSucceeded(context.Context, string) error
	MarkFailed(context.Context, TikTokWebhookJob, string, string, time.Time) error
}

type TikTokWebhookSnapshotter interface {
	Sync(context.Context, TikTokOrderSnapshotRequest) (*TikTokOrderSnapshotResult, error)
}

type TikTokWebhookWorker struct {
	enabled   bool
	store     TikTokWebhookJobStore
	snapshots TikTokWebhookSnapshotter
	logger    *zap.Logger
	now       func() time.Time
}

func NewTikTokWebhookWorker(enabled bool, store TikTokWebhookJobStore, snapshots TikTokWebhookSnapshotter, logger *zap.Logger) *TikTokWebhookWorker {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &TikTokWebhookWorker{enabled: enabled, store: store, snapshots: snapshots, logger: logger, now: time.Now}
}

func (w *TikTokWebhookWorker) Start(ctx context.Context) {
	if w == nil || !w.enabled {
		return
	}
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := w.ProcessOne(ctx); err != nil && !errors.Is(err, ErrNoWebhookJobDue) && !errors.Is(err, context.Canceled) {
					w.logger.Warn("tiktok_webhook_worker_failed", zap.String("error_code", "worker_error"))
				}
			}
		}
	}()
}

func (w *TikTokWebhookWorker) ProcessOne(ctx context.Context) (bool, error) {
	if w == nil || !w.enabled || w.store == nil || w.snapshots == nil || w.now == nil {
		return false, ErrWebhookStoreNotConfigured
	}
	job, err := w.store.ClaimDue(ctx, w.now())
	if err != nil {
		return false, err
	}
	runContext, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	result, syncErr := w.snapshots.Sync(runContext, TikTokOrderSnapshotRequest{ShopID: job.ShopID, OrderIDs: []string{job.OrderID}})
	if syncErr == nil && result != nil && result.SyncedCount == 1 && len(result.Snapshots) == 1 && strings.TrimSpace(result.Snapshots[0].OrderID) == job.OrderID {
		if err := w.store.MarkSucceeded(ctx, job.ID); err != nil {
			return true, err
		}
		w.logger.Info("tiktok_webhook_reconciliation_succeeded",
			zap.String("gateway_event_id", job.GatewayEventID), zap.String("notification_id", job.NotificationID),
			zap.String("shop_id", job.ShopID), zap.String("order_id", job.OrderID), zap.Int("attempt", job.Attempts))
		return true, nil
	}
	errorCode := "snapshot_failed"
	if errors.Is(syncErr, context.DeadlineExceeded) || errors.Is(runContext.Err(), context.DeadlineExceeded) {
		errorCode = "snapshot_timeout"
	}
	nextRunAt := w.now().Add(tikTokWebhookBackoff(job.Attempts))
	if err := w.store.MarkFailed(ctx, job, errorCode, "TikTok Shop exact-order snapshot refresh failed", nextRunAt); err != nil {
		return true, err
	}
	w.logger.Warn("tiktok_webhook_reconciliation_failed",
		zap.String("gateway_event_id", job.GatewayEventID), zap.String("notification_id", job.NotificationID),
		zap.String("shop_id", job.ShopID), zap.String("order_id", job.OrderID), zap.Int("attempt", job.Attempts),
		zap.String("error_code", errorCode), zap.Bool("terminal", job.Attempts >= maxTikTokWebhookAttempts))
	return true, nil
}

func tikTokWebhookBackoff(attempt int) time.Duration {
	delays := []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute, time.Hour, time.Hour}
	if attempt <= 0 {
		return delays[0]
	}
	if attempt > len(delays) {
		return delays[len(delays)-1]
	}
	return delays[attempt-1]
}
