package sml

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// WarehouseCache is an in-memory snapshot of SML warehouse + shelf master data.
// It mirrors PartyCache: startup fetch, 6 h refresh, manual refresh endpoint.
type WarehouseCache struct {
	client *WarehouseClient
	log    *zap.Logger

	mu         sync.RWMutex
	warehouses []Warehouse
	lastSync   time.Time

	stopCh chan struct{}
}

func NewWarehouseCache(client *WarehouseClient, log *zap.Logger) *WarehouseCache {
	return &WarehouseCache{
		client: client,
		log:    log,
		stopCh: make(chan struct{}),
	}
}

func (wc *WarehouseCache) Start(ctx context.Context) {
	if !wc.client.IsConfigured() {
		wc.log.Warn("warehouse_cache_skipped_unconfigured")
		return
	}
	go func() {
		if err := wc.RefreshNow(ctx); err != nil {
			wc.log.Error("warehouse_cache_initial_fetch_failed", zap.Error(err))
		}
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-wc.stopCh:
				return
			case <-ticker.C:
				if err := wc.RefreshNow(ctx); err != nil {
					wc.log.Error("warehouse_cache_refresh_failed", zap.Error(err))
				}
			}
		}
	}()
}

func (wc *WarehouseCache) Stop() {
	select {
	case <-wc.stopCh:
	default:
		close(wc.stopCh)
	}
}

func (wc *WarehouseCache) RefreshNow(ctx context.Context) error {
	start := time.Now()
	warehouses, err := wc.client.FetchAll(ctx)
	if err != nil {
		return err
	}
	sort.SliceStable(warehouses, func(i, j int) bool {
		return warehouses[i].Code < warehouses[j].Code
	})
	wc.mu.Lock()
	wc.warehouses = warehouses
	wc.lastSync = time.Now()
	wc.mu.Unlock()
	wc.log.Info("warehouse_cache_refreshed",
		zap.Int("warehouses", len(warehouses)),
		zap.Int("shelves", countShelves(warehouses)),
		zap.Duration("dur", time.Since(start)),
	)
	return nil
}

func countShelves(warehouses []Warehouse) int {
	total := 0
	for _, w := range warehouses {
		total += len(w.Shelves)
	}
	return total
}

func (wc *WarehouseCache) LastSync() time.Time {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	return wc.lastSync
}

func (wc *WarehouseCache) Counts() (int, int) {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	return len(wc.warehouses), countShelves(wc.warehouses)
}

func (wc *WarehouseCache) GetByCode(code string) *Warehouse {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	for i := range wc.warehouses {
		if wc.warehouses[i].Code == code {
			return &wc.warehouses[i]
		}
	}
	return nil
}

func (wc *WarehouseCache) HasShelf(whCode, shelfCode string) bool {
	w := wc.GetByCode(whCode)
	if w == nil {
		return false
	}
	for _, s := range w.Shelves {
		if s.Code == shelfCode {
			return true
		}
	}
	return false
}

func (wc *WarehouseCache) SearchWarehouses(query string, limit int) []Warehouse {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	if limit <= 0 {
		limit = 20
	}
	return searchWarehouseRows(wc.warehouses, query, limit)
}

func (wc *WarehouseCache) SearchShelves(whCode, query string, limit int) []Shelf {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	if limit <= 0 {
		limit = 50
	}
	var shelves []Shelf
	for _, w := range wc.warehouses {
		if w.Code == whCode {
			shelves = append(shelves, w.Shelves...)
			break
		}
	}
	return searchShelfRows(shelves, query, limit)
}

func searchWarehouseRows(rows []Warehouse, query string, limit int) []Warehouse {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		out := make([]Warehouse, 0, limit)
		for i := 0; i < len(rows) && i < limit; i++ {
			out = append(out, rows[i])
		}
		return out
	}
	type scored struct {
		row   Warehouse
		score int
	}
	var hits []scored
	for _, w := range rows {
		score := scoreCodeName(w.Code, w.Name, q, query)
		if score > 0 {
			hits = append(hits, scored{row: w, score: score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].row.Code < hits[j].row.Code
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]Warehouse, len(hits))
	for i, h := range hits {
		out[i] = h.row
	}
	return out
}

func searchShelfRows(rows []Shelf, query string, limit int) []Shelf {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		out := make([]Shelf, 0, limit)
		for i := 0; i < len(rows) && i < limit; i++ {
			out = append(out, rows[i])
		}
		return out
	}
	type scored struct {
		row   Shelf
		score int
	}
	var hits []scored
	for _, s := range rows {
		score := scoreCodeName(s.Code, s.Name, q, query)
		if score > 0 {
			hits = append(hits, scored{row: s, score: score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].row.Code < hits[j].row.Code
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]Shelf, len(hits))
	for i, h := range hits {
		out[i] = h.row
	}
	return out
}

func scoreCodeName(code, name, q, rawQuery string) int {
	c := strings.ToLower(code)
	n := strings.ToLower(name)
	switch {
	case c == q:
		return 100
	case strings.HasPrefix(c, q):
		return 90
	case strings.Contains(c, q):
		return 70
	case strings.HasPrefix(n, q):
		return 60
	case strings.Contains(n, q):
		return 40
	case strings.Contains(name, rawQuery):
		return 40
	default:
		return 0
	}
}
