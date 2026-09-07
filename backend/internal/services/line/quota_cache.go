package lineservice

import (
	"context"
	"errors"
	"sync"
	"time"
)

const (
	quotaCacheTTL        = 30 * time.Second
	quotaRefreshCooldown = 10 * time.Second
	quotaStaleTTL        = 15 * time.Minute
	quotaMaxConcurrency  = 4
)

var ErrQuotaRefreshCooldown = errors.New("LINE quota refresh cooldown")

type QuotaFetchFunc func(context.Context) (MessageQuota, error)

type CachedMessageQuota struct {
	Quota             MessageQuota
	CheckedAt         time.Time
	IsStale           bool
	ErrorCode         string
	RetryAfterSeconds int
}

type quotaCacheEntry struct {
	quota     MessageQuota
	checkedAt time.Time
}

type quotaCall struct {
	done   chan struct{}
	result CachedMessageQuota
	err    error
}

// QuotaCache coalesces per-OA calls and bounds the total number of simultaneous
// LINE quota fetches. It stores only validated numeric quota data, never tokens.
type QuotaCache struct {
	mu          sync.Mutex
	entries     map[string]quotaCacheEntry
	inflight    map[string]*quotaCall
	lastRefresh map[string]time.Time
	semaphore   chan struct{}
	now         func() time.Time
}

func NewQuotaCache() *QuotaCache {
	return &QuotaCache{
		entries:     make(map[string]quotaCacheEntry),
		inflight:    make(map[string]*quotaCall),
		lastRefresh: make(map[string]time.Time),
		semaphore:   make(chan struct{}, quotaMaxConcurrency),
		now:         time.Now,
	}
}

func (q *QuotaCache) Get(ctx context.Context, oaID string, refresh bool, fetch QuotaFetchFunc) (CachedMessageQuota, error) {
	if q == nil || fetch == nil {
		return CachedMessageQuota{}, &QuotaAPIError{Code: "line_unavailable"}
	}
	now := q.currentTime()
	q.mu.Lock()
	entry, hasEntry := q.entries[oaID]
	if !refresh && hasEntry && now.Sub(entry.checkedAt) <= quotaCacheTTL {
		q.mu.Unlock()
		return CachedMessageQuota{Quota: entry.quota, CheckedAt: entry.checkedAt}, nil
	}
	if refresh {
		if last := q.lastRefresh[oaID]; !last.IsZero() && now.Sub(last) < quotaRefreshCooldown {
			retryAfter := int((quotaRefreshCooldown - now.Sub(last) + time.Second - 1) / time.Second)
			q.mu.Unlock()
			if hasEntry {
				return CachedMessageQuota{
					Quota: entry.quota, CheckedAt: entry.checkedAt,
					IsStale:   now.Sub(entry.checkedAt) > quotaCacheTTL,
					ErrorCode: "refresh_cooldown", RetryAfterSeconds: max(retryAfter, 1),
				}, nil
			}
			return CachedMessageQuota{ErrorCode: "refresh_cooldown", RetryAfterSeconds: max(retryAfter, 1)}, ErrQuotaRefreshCooldown
		}
		q.lastRefresh[oaID] = now
	}
	if call := q.inflight[oaID]; call != nil {
		q.mu.Unlock()
		select {
		case <-ctx.Done():
			return CachedMessageQuota{}, ctx.Err()
		case <-call.done:
			return call.result, call.err
		}
	}
	call := &quotaCall{done: make(chan struct{})}
	q.inflight[oaID] = call
	q.mu.Unlock()

	result, err := q.fetch(ctx, fetch)
	completedAt := q.currentTime()
	q.mu.Lock()
	if err == nil {
		q.entries[oaID] = quotaCacheEntry{quota: result, checkedAt: completedAt}
		call.result = CachedMessageQuota{Quota: result, CheckedAt: completedAt}
	} else if current, ok := q.entries[oaID]; ok && completedAt.Sub(current.checkedAt) <= quotaStaleTTL {
		call.result = CachedMessageQuota{
			Quota: current.quota, CheckedAt: current.checkedAt,
			IsStale: true, ErrorCode: QuotaErrorCode(err),
		}
		call.err = nil
	} else {
		call.result = CachedMessageQuota{IsStale: false, ErrorCode: QuotaErrorCode(err)}
		call.err = err
	}
	delete(q.inflight, oaID)
	close(call.done)
	q.mu.Unlock()
	return call.result, call.err
}

func (q *QuotaCache) Invalidate(oaID string) {
	if q == nil {
		return
	}
	q.mu.Lock()
	delete(q.entries, oaID)
	delete(q.lastRefresh, oaID)
	q.mu.Unlock()
}

func (q *QuotaCache) fetch(ctx context.Context, fetch QuotaFetchFunc) (MessageQuota, error) {
	select {
	case q.semaphore <- struct{}{}:
		defer func() { <-q.semaphore }()
	case <-ctx.Done():
		return MessageQuota{}, ctx.Err()
	}
	return fetch(ctx)
}

func (q *QuotaCache) currentTime() time.Time {
	if q.now == nil {
		return time.Now()
	}
	return q.now()
}
