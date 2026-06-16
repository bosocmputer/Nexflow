package sml

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestSaleInvoiceCancelClientPreviewUsesCancelPreviewEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if r.URL.Path != "/api/v1/ic/sale-invoices/BF-SO260600001/cancel/preview" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("guid"); got != "smlx" {
			t.Fatalf("guid = %q", got)
		}
		if got := r.Header.Get("X-Tenant"); got != "sml1_2026" {
			t.Fatalf("X-Tenant = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		bodyText := string(body)
		if strings.Contains(bodyText, "ยกเลิก") {
			t.Fatalf("body should be ASCII escaped for SML Latin-1 reader: %s", bodyText)
		}
		if !strings.Contains(bodyText, `\u0e`) {
			t.Fatalf("body missing escaped Thai text: %s", bodyText)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"doc_no":"CN26060001"}}`))
	}))
	defer srv.Close()

	client := NewSaleInvoiceCancelClient(PartyConfig{
		BaseURL:  srv.URL,
		GUID:     "smlx",
		Provider: "SMLGOH",
		Database: "sml1_2026",
	}, zap.NewNop())
	status, resp, err := client.Preview(context.Background(), "BF-SO260600001", SaleInvoiceCancelRequest{
		DocFormatCode: "CN",
		Remark:        "Shopee ยกเลิกหลังส่ง SML",
	})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if !resp.IsSuccess() {
		t.Fatalf("expected success response: %#v", resp)
	}
	if got := resp.CancelDocNo(); got != "CN26060001" {
		t.Fatalf("cancel doc no = %q", got)
	}
}

func TestSaleInvoiceCancelClientCreateTreatsAlreadyExistsAsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ic/sale-invoices/BF-SO260600001/cancel" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"success":false,"status":"already_exists","cancel_sml_doc_no":"CN26060001"}`))
	}))
	defer srv.Close()

	client := NewSaleInvoiceCancelClient(PartyConfig{
		BaseURL:  srv.URL,
		GUID:     "smlx",
		Provider: "SMLGOH",
		Database: "sml1_2026",
	}, zap.NewNop())
	status, resp, err := client.Create(context.Background(), "BF-SO260600001", SaleInvoiceCancelRequest{DocFormatCode: "CN"})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusConflict {
		t.Fatalf("status = %d", status)
	}
	if !resp.IsSuccess() {
		t.Fatalf("already_exists should be success-like for idempotency: %#v", resp)
	}
	if got := resp.CancelDocNo(); got != "CN26060001" {
		t.Fatalf("cancel doc no = %q", got)
	}
}
