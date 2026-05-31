package sml

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// PartyCache is an in-memory snapshot of SML 248 customers + suppliers.
//
// On boot it fetches both lists, then refreshes every 6 h. Callers can also
// trigger an on-demand RefreshNow (e.g. after admin creates a row in SML).
// Search runs in O(N) over ~1500 records — sub-millisecond, so no need for
// a trie/prefix tree.
type PartyCache struct {
	client *PartyClient
	log    *zap.Logger

	mu          sync.RWMutex
	customers   []Party
	suppliers   []Party
	lastSync    time.Time
	lastAttempt time.Time
	lastErr     string

	stopCh chan struct{}
}

func NewPartyCache(client *PartyClient, log *zap.Logger) *PartyCache {
	return &PartyCache{
		client: client,
		log:    log,
		stopCh: make(chan struct{}),
	}
}

// Start runs the initial fetch then loops on a 6 h ticker. Failures are
// logged but never block startup — handlers fall back to empty results,
// admin can click refresh in the UI to retry.
func (pc *PartyCache) Start(ctx context.Context) {
	if !pc.client.IsConfigured() {
		pc.log.Warn("party_cache_skipped_unconfigured")
		return
	}
	go func() {
		pc.refreshInitialWithRetry(ctx)
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-pc.stopCh:
				return
			case <-ticker.C:
				if err := pc.RefreshNow(ctx); err != nil {
					pc.log.Error(
						"party_cache_refresh_failed",
						zap.Error(err),
						zap.String("user_message", "ดึงรายชื่อลูกค้า/ผู้ขายจาก SML ไม่สำเร็จ กรุณากดรีเฟรชหรือตรวจ SML API"),
					)
				}
			}
		}
	}()
}

func (pc *PartyCache) refreshInitialWithRetry(ctx context.Context) {
	delays := []time.Duration{0, 3 * time.Second, 10 * time.Second, 30 * time.Second, time.Minute}
	var lastErr error
	for attempt, delay := range delays {
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		if err := pc.RefreshNow(ctx); err != nil {
			lastErr = err
			pc.log.Warn(
				"party_cache_initial_fetch_retry",
				zap.Int("attempt", attempt+1),
				zap.Int("max_attempts", len(delays)),
				zap.Error(err),
				zap.String("user_message", "ยังดึงรายชื่อลูกค้า/ผู้ขายจาก SML ไม่สำเร็จ ระบบจะลองใหม่อัตโนมัติ"),
			)
			continue
		}
		return
	}
	if lastErr != nil {
		pc.log.Error(
			"party_cache_initial_fetch_failed",
			zap.Error(lastErr),
			zap.String("user_message", "ดึงรายชื่อลูกค้า/ผู้ขายจาก SML ไม่สำเร็จ กรุณากดรีเฟรชหรือตรวจ SML API"),
		)
	}
}

func (pc *PartyCache) Stop() {
	select {
	case <-pc.stopCh:
	default:
		close(pc.stopCh)
	}
}

// RefreshNow fetches both customer and supplier lists and atomically swaps
// the cache. Returns the first error encountered (cache is unchanged on
// failure).
func (pc *PartyCache) RefreshNow(ctx context.Context) error {
	start := time.Now()
	pc.recordAttempt()
	customers, err := pc.client.FetchAllCustomers(ctx)
	if err != nil {
		pc.recordError(err)
		return err
	}
	suppliers, err := pc.client.FetchAllSuppliers(ctx)
	if err != nil {
		pc.recordError(err)
		return err
	}
	pc.mu.Lock()
	pc.customers = customers
	pc.suppliers = suppliers
	pc.lastSync = time.Now()
	pc.lastErr = ""
	pc.mu.Unlock()
	pc.log.Info("party_cache_refreshed",
		zap.Int("customers", len(customers)),
		zap.Int("suppliers", len(suppliers)),
		zap.Duration("dur", time.Since(start)),
	)
	return nil
}

func (pc *PartyCache) recordAttempt() {
	pc.mu.Lock()
	pc.lastAttempt = time.Now()
	pc.mu.Unlock()
}

func (pc *PartyCache) recordError(err error) {
	pc.mu.Lock()
	pc.lastErr = err.Error()
	pc.mu.Unlock()
}

// LastSync returns when the cache was last filled. Zero value means never.
func (pc *PartyCache) LastSync() time.Time {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	return pc.lastSync
}

type PartyCacheStatus struct {
	Customers   int       `json:"customers"`
	Suppliers   int       `json:"suppliers"`
	LastSync    time.Time `json:"last_sync"`
	LastAttempt time.Time `json:"last_attempt"`
	Status      string    `json:"status"`
	Error       string    `json:"error,omitempty"`
}

func (pc *PartyCache) Status() PartyCacheStatus {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	status := "ok"
	if pc.lastSync.IsZero() {
		status = "not_ready"
	}
	if pc.lastErr != "" {
		status = "error"
	}
	return PartyCacheStatus{
		Customers:   len(pc.customers),
		Suppliers:   len(pc.suppliers),
		LastSync:    pc.lastSync,
		LastAttempt: pc.lastAttempt,
		Status:      status,
		Error:       pc.lastErr,
	}
}

// Counts returns (customerCount, supplierCount).
func (pc *PartyCache) Counts() (int, int) {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	return len(pc.customers), len(pc.suppliers)
}

func (pc *PartyCache) Upsert(billType string, party Party) {
	normalizeParty(&party)
	if strings.TrimSpace(party.Code) == "" {
		return
	}
	pc.mu.Lock()
	defer pc.mu.Unlock()
	if billType == "purchase" {
		pc.suppliers = upsertParty(pc.suppliers, party)
		return
	}
	pc.customers = upsertParty(pc.customers, party)
}

func upsertParty(list []Party, party Party) []Party {
	for i := range list {
		if list[i].Code == party.Code {
			list[i] = party
			return list
		}
	}
	list = append(list, party)
	sort.SliceStable(list, func(i, j int) bool {
		return list[i].Code < list[j].Code
	})
	return list
}

func (pc *PartyCache) listForBillType(billType string) []Party {
	if billType == "purchase" {
		return pc.suppliers
	}
	return pc.customers
}

// GetByCode returns the cached party with the given code, or nil if missing.
func (pc *PartyCache) GetByCode(billType, code string) *Party {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	for i, p := range pc.listForBillType(billType) {
		if p.Code == code {
			return &pc.listForBillType(billType)[i]
		}
	}
	return nil
}

// FindByExactName returns the first party whose Name matches exactly.
// Used by Quick-setup to find the AR00001-04 placeholders.
func (pc *PartyCache) FindByExactName(billType, name string) *Party {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	for i, p := range pc.listForBillType(billType) {
		if p.Name == name {
			return &pc.listForBillType(billType)[i]
		}
	}
	return nil
}

// Search returns up to `limit` parties matching `query`, ranked by relevance.
//
// Matching rules:
//   - empty query → top N alphabetical by code
//   - non-empty   → score each row, descending by score
//
// Scoring (highest wins):
//
//	100  exact code match
//	 90  code starts with query
//	 70  code contains query
//	 60  name starts with query
//	 40  name contains query
//	 30  English name contains query
//	 20  tax_id/card_id contains query
//
// Strings are compared case-insensitive for ASCII; Thai is left as-is
// (Thai has no case so strings.Contains works directly).
func (pc *PartyCache) Search(billType, query string, limit int) []Party {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	src := pc.listForBillType(billType)
	if limit <= 0 {
		limit = 20
	}
	if query == "" {
		out := make([]Party, 0, limit)
		for i := 0; i < len(src) && i < limit; i++ {
			out = append(out, src[i])
		}
		return out
	}

	q := strings.ToLower(strings.TrimSpace(query))
	type scored struct {
		party Party
		score int
	}
	var hits []scored
	for _, p := range src {
		code := strings.ToLower(p.Code)
		name := strings.ToLower(p.Name)
		nameEng := strings.ToLower(p.NameEng1)
		taxID := strings.ToLower(p.TaxID)
		cardID := strings.ToLower(p.CardID)

		score := 0
		switch {
		case code == q:
			score = 100
		case strings.HasPrefix(code, q):
			score = 90
		case strings.Contains(code, q):
			score = 70
		}
		// also check name (best of code-score vs name-score)
		switch {
		case strings.HasPrefix(name, q):
			if 60 > score {
				score = 60
			}
		case strings.Contains(name, q):
			if 40 > score {
				score = 40
			}
		}
		// Thai-friendly: original-case substring
		if score == 0 && strings.Contains(p.Name, query) {
			score = 40
		}
		if score == 0 && nameEng != "" && strings.Contains(nameEng, q) {
			score = 30
		}
		if score == 0 && taxID != "" && strings.Contains(taxID, q) {
			score = 20
		}
		if score == 0 && cardID != "" && strings.Contains(cardID, q) {
			score = 20
		}
		if score > 0 {
			hits = append(hits, scored{party: p, score: score})
		}
	}

	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].party.Code < hits[j].party.Code
	})

	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]Party, len(hits))
	for i, h := range hits {
		out[i] = h.party
	}
	return out
}
