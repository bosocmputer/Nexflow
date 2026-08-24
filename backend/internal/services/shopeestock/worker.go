package shopeestock

import (
	"context"
	"hash/fnv"
	"time"

	"go.uber.org/zap"
)

type Worker struct {
	service   *Service
	log       *zap.Logger
	syncSlots chan struct{}
}

func NewWorker(service *Service, log *zap.Logger) *Worker {
	return &Worker{service: service, log: log, syncSlots: make(chan struct{}, 5)}
}

func (w *Worker) Start(ctx context.Context) {
	if w == nil || w.service == nil || !w.service.Available() {
		return
	}
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				w.tick(ctx, now)
			}
		}
	}()
}

func (w *Worker) tick(ctx context.Context, now time.Time) {
	catalogDue, err := w.service.store.CatalogDueShops(ctx)
	if err != nil {
		w.log.Warn("shopee stock catalog scheduler list", zap.Error(err))
		return
	}
	catalogShops := map[int64]struct{}{}
	if len(catalogDue) > 0 {
		item := catalogDue[0]
		catalogShops[item.ShopID] = struct{}{}
		go func() {
			runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			defer cancel()
			if _, err := w.service.SyncCatalogScheduled(runCtx, item.ShopID, item.Full); err != nil {
				w.log.Warn("shopee stock catalog sync", zap.Int64("shop_id", item.ShopID), zap.Bool("full", item.Full), zap.Error(err))
			}
		}()
	}
	shops, err := w.service.store.EnabledDueShops(ctx)
	if err != nil {
		w.log.Warn("shopee stock scheduler list", zap.Error(err))
		return
	}
	for _, shopID := range shops {
		if _, catalogRunning := catalogShops[shopID]; catalogRunning {
			continue
		}
		if int64(now.Second()) < schedulerJitter(shopID) {
			continue
		}
		select {
		case w.syncSlots <- struct{}{}:
		default:
			continue
		}
		shopID := shopID
		go func() {
			defer func() { <-w.syncSlots }()
			runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			if _, err := w.service.RunSync(runCtx, shopID, "scheduler"); err != nil {
				w.log.Warn("shopee stock sync", zap.Int64("shop_id", shopID), zap.Error(err))
			}
		}()
	}
	_ = w.service.store.DeleteOldAttempts(ctx)
}

func schedulerJitter(shopID int64) int64 {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(time.Unix(shopID, 0).String()))
	return int64(hash.Sum32() % 25)
}
