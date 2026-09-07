package lineservice

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testLimitedQuota() MessageQuota {
	limit, remaining := int64(300), int64(282)
	return MessageQuota{Type: "limited", Limit: &limit, Used: 18, Remaining: &remaining}
}

func TestQuotaCacheCachesForThirtySecondsAndRefreshBypassesCache(t *testing.T) {
	cache := NewQuotaCache()
	now := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	var calls atomic.Int32
	fetch := func(context.Context) (MessageQuota, error) {
		calls.Add(1)
		return testLimitedQuota(), nil
	}

	first, err := cache.Get(context.Background(), "oa-1", false, fetch)
	if err != nil || first.Quota.Used != 18 {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	now = now.Add(29 * time.Second)
	if _, err := cache.Get(context.Background(), "oa-1", false, fetch); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("cached calls=%d", calls.Load())
	}
	now = now.Add(2 * time.Second)
	if _, err := cache.Get(context.Background(), "oa-1", false, fetch); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expired calls=%d", calls.Load())
	}
	now = now.Add(11 * time.Second)
	if _, err := cache.Get(context.Background(), "oa-1", true, fetch); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("refresh calls=%d", calls.Load())
	}
}

func TestQuotaCacheCoalescesConcurrentOARequests(t *testing.T) {
	cache := NewQuotaCache()
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	fetch := func(context.Context) (MessageQuota, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return testLimitedQuota(), nil
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := cache.Get(context.Background(), "oa-1", false, fetch); err != nil {
				t.Errorf("Get: %v", err)
			}
		}()
	}
	<-started
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("calls=%d want=1", calls.Load())
	}
}

func TestQuotaCacheUsesStaleSuccessForFifteenMinutes(t *testing.T) {
	cache := NewQuotaCache()
	now := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	if _, err := cache.Get(context.Background(), "oa-1", false, func(context.Context) (MessageQuota, error) {
		return testLimitedQuota(), nil
	}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	stale, err := cache.Get(context.Background(), "oa-1", false, func(context.Context) (MessageQuota, error) {
		return MessageQuota{}, &QuotaAPIError{Code: "line_server_error", HTTPStatus: 503}
	})
	if err != nil || !stale.IsStale || stale.ErrorCode != "line_server_error" || stale.Quota.Used != 18 {
		t.Fatalf("stale=%#v err=%v", stale, err)
	}
	now = now.Add(15 * time.Minute)
	_, err = cache.Get(context.Background(), "oa-1", false, func(context.Context) (MessageQuota, error) {
		return MessageQuota{}, &QuotaAPIError{Code: "line_server_error", HTTPStatus: 503}
	})
	if err == nil {
		t.Fatal("expected stale expiry error")
	}
}

func TestQuotaCacheLimitsManualRefreshToTenSeconds(t *testing.T) {
	cache := NewQuotaCache()
	now := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	fetch := func(context.Context) (MessageQuota, error) { return testLimitedQuota(), nil }
	if _, err := cache.Get(context.Background(), "oa-1", true, fetch); err != nil {
		t.Fatal(err)
	}
	now = now.Add(3 * time.Second)
	result, err := cache.Get(context.Background(), "oa-1", true, fetch)
	if err != nil || result.ErrorCode != "refresh_cooldown" || result.RetryAfterSeconds != 7 {
		t.Fatalf("result=%#v err=%v", result, err)
	}

	cache.Invalidate("oa-empty")
	if _, err := cache.Get(context.Background(), "oa-empty", true, func(context.Context) (MessageQuota, error) {
		return MessageQuota{}, errors.New("down")
	}); err == nil {
		t.Fatal("expected initial fetch failure")
	}
	now = now.Add(time.Second)
	if _, err := cache.Get(context.Background(), "oa-empty", true, fetch); !errors.Is(err, ErrQuotaRefreshCooldown) {
		t.Fatalf("err=%v", err)
	}
}

func TestQuotaCacheBoundsExternalConcurrencyToFour(t *testing.T) {
	cache := NewQuotaCache()
	var active atomic.Int32
	var maximum atomic.Int32
	release := make(chan struct{})
	fetch := func(context.Context) (MessageQuota, error) {
		current := active.Add(1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		<-release
		active.Add(-1)
		return testLimitedQuota(), nil
	}

	var wg sync.WaitGroup
	for index := range 8 {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, _ = cache.Get(context.Background(), string(rune('a'+index)), false, fetch)
		}(index)
	}
	deadline := time.After(time.Second)
	for maximum.Load() < 4 {
		select {
		case <-deadline:
			t.Fatal("four fetches did not start")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	close(release)
	wg.Wait()
	if maximum.Load() != 4 {
		t.Fatalf("maximum=%d", maximum.Load())
	}
}

func TestQuotaCacheInvalidationFencesAnOldTokenFetch(t *testing.T) {
	cache := NewQuotaCache()
	started := make(chan struct{})
	release := make(chan struct{})
	oldResult := make(chan error, 1)
	go func() {
		_, err := cache.Get(context.Background(), "oa-1", false, func(context.Context) (MessageQuota, error) {
			close(started)
			<-release
			return testLimitedQuota(), nil
		})
		oldResult <- err
	}()
	<-started
	cache.Invalidate("oa-1")
	newQuota := testLimitedQuota()
	newQuota.Used = 20
	if _, err := cache.Get(context.Background(), "oa-1", false, func(context.Context) (MessageQuota, error) {
		return newQuota, nil
	}); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-oldResult; QuotaErrorCode(err) != "quota_config_changed" {
		t.Fatalf("old result err=%v", err)
	}
	got, err := cache.Get(context.Background(), "oa-1", false, func(context.Context) (MessageQuota, error) {
		t.Fatal("new cache entry was overwritten by old fetch")
		return MessageQuota{}, nil
	})
	if err != nil || got.Quota.Used != 20 {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}
