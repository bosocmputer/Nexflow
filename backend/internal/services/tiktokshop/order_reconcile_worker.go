package tiktokshop

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"go.uber.org/zap"
)

const tikTokOrderReconcileWorkerInterval = 30 * time.Second

type dueOrderReconcileStore interface {
	ClaimDueRun(context.Context, time.Time) (TikTokOrderReconcileRun, error)
}

type scheduledOrderReconciler interface {
	ExecuteScheduled(context.Context, TikTokOrderReconcileRun) (*TikTokOrderReconcileResult, error)
}

type TikTokOrderReconcileWorker struct {
	enabled    bool
	store      dueOrderReconcileStore
	reconciler scheduledOrderReconciler
	logger     *zap.Logger
}

func NewTikTokOrderReconcileWorker(enabled bool, store dueOrderReconcileStore, reconciler scheduledOrderReconciler, logger *zap.Logger) *TikTokOrderReconcileWorker {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &TikTokOrderReconcileWorker{enabled: enabled, store: store, reconciler: reconciler, logger: logger}
}

func (w *TikTokOrderReconcileWorker) Start(ctx context.Context) {
	if w == nil || !w.enabled || w.store == nil || w.reconciler == nil {
		return
	}
	go func() {
		w.runOnce(ctx, time.Now().UTC())
		ticker := time.NewTicker(tikTokOrderReconcileWorkerInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				w.runOnce(ctx, now.UTC())
			}
		}
	}()
}

func (w *TikTokOrderReconcileWorker) runOnce(ctx context.Context, now time.Time) {
	if w == nil || !w.enabled || w.store == nil || w.reconciler == nil {
		return
	}
	run, err := w.store.ClaimDueRun(ctx, now)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, context.Canceled) {
			w.logger.Error("tiktok_shop_order_reconciliation_claim_failed", zap.String("entry_point", "scheduled_worker"), zap.Error(err))
		}
		return
	}
	result, err := w.reconciler.ExecuteScheduled(ctx, run)
	if err != nil {
		w.logger.Warn("tiktok_shop_order_reconciliation_scheduled_failed",
			zap.String("shop_id", run.ShopID), zap.String("run_id", run.ID),
			zap.String("entry_point", "scheduled_worker"), zap.Error(err))
		return
	}
	w.logger.Info("tiktok_shop_order_reconciliation_scheduled_succeeded",
		zap.String("shop_id", result.ShopID), zap.String("run_id", result.RunID),
		zap.Int("page_count", result.PageCount), zap.Int("order_count", result.SnapshottedCount),
		zap.String("entry_point", "scheduled_worker"))
}
