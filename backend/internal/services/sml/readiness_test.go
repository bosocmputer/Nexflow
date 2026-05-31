package sml

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestReadinessCheckerOK(t *testing.T) {
	var gotTenant string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTenant = r.Header.Get("X-Tenant")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	checker := NewReadinessChecker(PartyConfig{
		BaseURL:  srv.URL,
		GUID:     "smlx",
		Database: "data1_test",
	}, zap.NewNop()).WithHTTPClient(srv.Client())

	status := checker.Check(context.Background(), true)
	if !status.Ready || status.Status != "ok" {
		t.Fatalf("status=%+v, want ready ok", status)
	}
	if gotTenant != "data1_test" {
		t.Fatalf("X-Tenant=%q, want data1_test", gotTenant)
	}
}

func TestReadinessCheckerTenantDBDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"success":false,"error":{"code":"customer_count_failed","message":"count customers failed","details":"context deadline exceeded"}}`))
	}))
	defer srv.Close()

	checker := NewReadinessChecker(PartyConfig{
		BaseURL:  srv.URL,
		GUID:     "smlx",
		Database: "data1_test",
	}, zap.NewNop()).WithHTTPClient(srv.Client())

	status := checker.Check(context.Background(), true)
	if status.Ready {
		t.Fatalf("Ready=true, want false")
	}
	if status.HTTPStatus != http.StatusServiceUnavailable {
		t.Fatalf("HTTPStatus=%d, want 503", status.HTTPStatus)
	}
	if status.Message != "เชื่อมต่อฐานข้อมูล SML ของร้านนี้ไม่ได้ เครื่อง SML/Postgres อาจยังไม่เปิดหรือเครือข่ายยังไม่พร้อม" {
		t.Fatalf("Message=%q", status.Message)
	}
}

func TestReadinessCheckerMissingConfig(t *testing.T) {
	checker := NewReadinessChecker(PartyConfig{}, zap.NewNop())
	status := checker.Check(context.Background(), true)
	if status.Configured || status.Ready || status.Status != "not_configured" {
		t.Fatalf("status=%+v, want not_configured", status)
	}
}

func TestReadinessCheckerMalformedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv.Close()

	checker := NewReadinessChecker(PartyConfig{
		BaseURL:  srv.URL,
		GUID:     "smlx",
		Database: "sml1_2026",
	}, zap.NewNop()).WithHTTPClient(srv.Client())

	status := checker.Check(context.Background(), true)
	if status.Ready || status.Status != "unexpected_response" {
		t.Fatalf("status=%+v, want unexpected_response", status)
	}
}

func TestReadinessCheckerTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	client := srv.Client()
	client.Timeout = 10 * time.Millisecond
	checker := NewReadinessChecker(PartyConfig{
		BaseURL:  srv.URL,
		GUID:     "smlx",
		Database: "sml1_2026",
	}, zap.NewNop()).WithHTTPClient(client)

	status := checker.Check(context.Background(), true)
	if status.Ready || status.Status != "unreachable" {
		t.Fatalf("status=%+v, want unreachable", status)
	}
}

func TestReadinessCheckerCache(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	checker := NewReadinessChecker(PartyConfig{
		BaseURL:  srv.URL,
		GUID:     "smlx",
		Database: "sml1_2026",
	}, zap.NewNop()).WithHTTPClient(srv.Client()).WithTTL(time.Minute)

	first := checker.Check(context.Background(), false)
	second := checker.Check(context.Background(), false)
	if !first.Ready || !second.Ready || !second.Cached {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	if calls != 1 {
		t.Fatalf("calls=%d, want 1", calls)
	}
}
