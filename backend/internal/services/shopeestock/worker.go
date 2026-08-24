package shopeestock

import (
	"context"
	"errors"
	"hash/fnv"
	"time"

	"go.uber.org/zap"
)

type Worker struct {
	service      *Service
	log          *zap.Logger
	syncSlots    chan struct{}
	previewSlots chan struct{}
}

func NewWorker(service *Service, log *zap.Logger) *Worker {
	return &Worker{service: service, log: log, syncSlots: make(chan struct{}, 5), previewSlots: make(chan struct{}, 2)}
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
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.previewTick(ctx)
			}
		}
	}()
}

func (w *Worker) previewTick(ctx context.Context) {
	select {
	case w.previewSlots <- struct{}{}:
	default:
		return
	}
	owner := w.service.leaseOwner("preview", time.Now().UnixNano())
	job, err := w.service.store.ClaimQueuedPreview(ctx, owner, 5*time.Minute)
	if err != nil || job == nil {
		<-w.previewSlots
		if err != nil {
			w.log.Warn("shopee stock preview claim", zap.Error(err))
		}
		return
	}
	go func() {
		defer func() { <-w.previewSlots }()
		token, acquired, err := w.service.store.AcquireFencedLease(ctx, job.ShopID, owner, 5*time.Minute)
		if err != nil || !acquired {
			_ = w.service.store.ReleasePreviewRun(context.Background(), job.ID, owner)
			if err != nil {
				w.log.Warn("shopee stock preview lease", zap.Int64("shop_id", job.ShopID), zap.Error(err))
			}
			return
		}
		defer func() { _ = w.service.store.ReleaseLease(context.Background(), job.ShopID, owner) }()
		if err := w.service.store.AttachPreviewFencingToken(ctx, job.ID, owner, token); err != nil {
			_ = w.service.store.ReleasePreviewRun(context.Background(), job.ID, owner)
			return
		}
		runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		heartbeatDone := make(chan struct{})
		go func() {
			defer close(heartbeatDone)
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-runCtx.Done():
					return
				case <-ticker.C:
					runOK, runErr := w.service.store.HeartbeatPreviewRun(runCtx, job.ID, owner, 5*time.Minute)
					leaseOK, leaseErr := w.service.store.HeartbeatFencedLease(runCtx, job.ShopID, owner, token, 5*time.Minute)
					if runErr != nil || leaseErr != nil || !runOK || !leaseOK {
						cancel()
						return
					}
				}
			}
		}()
		_, err = w.service.ExecuteQueuedPreview(runCtx, job)
		cancel()
		<-heartbeatDone
		if err != nil && !errors.Is(err, ErrPreviewStale) {
			w.log.Warn("shopee stock preview", zap.Int64("shop_id", job.ShopID), zap.String("run_id", job.ID), zap.Error(err))
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
