package sml

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProcessStockRequestIncludesHTTPErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != stockRequestPath {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"stock item AH-0030 missing cost setup"}`))
	}))
	defer srv.Close()

	client := NewStockRequestClient(srv.URL, "SMLGOH", "SML1_2026", nil)
	err := client.ProcessStockRequest(context.Background(), []string{"AH-0030"})
	if err == nil {
		t.Fatal("expected stock request error")
	}

	msg := err.Error()
	if !strings.Contains(msg, "HTTP 500") {
		t.Fatalf("expected HTTP status in error, got %q", msg)
	}
	if !strings.Contains(msg, "missing cost setup") {
		t.Fatalf("expected response body in error, got %q", msg)
	}
}

func TestProcessStockRequestTruncatesLongHTTPErrorBody(t *testing.T) {
	longBody := strings.Repeat("x", stockErrorBodyLimit+128)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(longBody))
	}))
	defer srv.Close()

	client := NewStockRequestClient(srv.URL, "SMLGOH", "SML1_2026", nil)
	err := client.ProcessStockRequest(context.Background(), []string{"AH-0030"})
	if err == nil {
		t.Fatal("expected stock request error")
	}

	msg := err.Error()
	if !strings.Contains(msg, "HTTP 502") {
		t.Fatalf("expected HTTP status in error, got %q", msg)
	}
	if !strings.Contains(msg, "...(truncated)") {
		t.Fatalf("expected truncation marker in error, got %q", msg)
	}
	if strings.Contains(msg, fmt.Sprintf("%s%s", strings.Repeat("x", stockErrorBodyLimit), "x")) {
		t.Fatalf("error body was not truncated: length=%d", len(msg))
	}
}
