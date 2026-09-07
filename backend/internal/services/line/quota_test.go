package lineservice

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func quotaTestService(server *httptest.Server) *Service {
	return &Service{
		accessToken: "test-secret-token",
		httpClient:  server.Client(),
		apiBaseURL:  server.URL,
	}
}

func TestGetMessageQuotaValidatesAndClampsLimitedQuota(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-secret-token" {
			t.Fatalf("missing bearer token")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/bot/message/quota":
			_, _ = w.Write([]byte(`{"type":"limited","value":300}`))
		case "/v2/bot/message/quota/consumption":
			_, _ = w.Write([]byte(`{"totalUsage":318}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	got, err := quotaTestService(server).GetMessageQuota(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != "limited" || got.Limit == nil || *got.Limit != 300 || got.Used != 318 || got.Remaining == nil || *got.Remaining != 0 {
		t.Fatalf("unexpected quota: %#v", got)
	}
}

func TestGetMessageQuotaSupportsUnlimitedAndNone(t *testing.T) {
	for _, quotaType := range []string{"unlimited", "none"} {
		t.Run(quotaType, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/consumption") {
					_, _ = w.Write([]byte(`{"totalUsage":12}`))
					return
				}
				_, _ = w.Write([]byte(`{"type":"` + quotaType + `"}`))
			}))
			defer server.Close()
			got, err := quotaTestService(server).GetMessageQuota(context.Background())
			if err != nil || got.Type != quotaType || got.Limit != nil || got.Remaining != nil || got.Used != 12 {
				t.Fatalf("quota=%#v err=%v", got, err)
			}
		})
	}
}

func TestGetMessageQuotaRetries429OnceAndHonorsBoundedRetryAfter(t *testing.T) {
	var quotaCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/bot/message/quota" && quotaCalls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"message":"secret raw LINE body"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/consumption") {
			_, _ = w.Write([]byte(`{"totalUsage":18}`))
			return
		}
		_, _ = w.Write([]byte(`{"type":"limited","value":300}`))
	}))
	defer server.Close()

	got, err := quotaTestService(server).GetMessageQuota(context.Background())
	if err != nil || quotaCalls.Load() != 2 || got.Used != 18 {
		t.Fatalf("quota=%#v calls=%d err=%v", got, quotaCalls.Load(), err)
	}
}

func TestGetMessageQuotaRetriesServerErrorOnce(t *testing.T) {
	var quotaCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/bot/message/quota" && quotaCalls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"message":"temporary upstream failure"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/consumption") {
			_, _ = w.Write([]byte(`{"totalUsage":18}`))
			return
		}
		_, _ = w.Write([]byte(`{"type":"limited","value":300}`))
	}))
	defer server.Close()

	got, err := quotaTestService(server).GetMessageQuota(context.Background())
	if err != nil || quotaCalls.Load() != 2 || got.Used != 18 {
		t.Fatalf("quota=%#v calls=%d err=%v", got, quotaCalls.Load(), err)
	}
}

func TestGetMessageQuotaDoesNotRetryAuthenticationOrExposeRawBody(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"token test-secret-token is invalid"}`))
	}))
	defer server.Close()

	_, err := quotaTestService(server).GetMessageQuota(context.Background())
	if err == nil || calls.Load() != 1 {
		t.Fatalf("calls=%d err=%v", calls.Load(), err)
	}
	if strings.Contains(err.Error(), "test-secret-token") || strings.Contains(err.Error(), "raw") {
		t.Fatalf("unsafe error: %v", err)
	}
	if QuotaErrorCode(err) != "line_token_invalid" {
		t.Fatalf("error code = %q", QuotaErrorCode(err))
	}
}

func TestGetMessageQuotaRejectsMalformedResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"type":"limited"}`))
	}))
	defer server.Close()

	_, err := quotaTestService(server).GetMessageQuota(context.Background())
	if err == nil || QuotaErrorCode(err) != "invalid_quota_response" {
		t.Fatalf("err=%v code=%s", err, QuotaErrorCode(err))
	}
}

func TestGetMessageQuotaRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"type":"limited","padding":"` + strings.Repeat("x", maxQuotaResponseBytes) + `","value":300}`))
	}))
	defer server.Close()

	_, err := quotaTestService(server).GetMessageQuota(context.Background())
	if err == nil || QuotaErrorCode(err) != "line_response_too_large" {
		t.Fatalf("err=%v code=%s", err, QuotaErrorCode(err))
	}
}

func TestGetMessageQuotaHonorsRequestTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := quotaTestService(server).GetMessageQuota(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
}
