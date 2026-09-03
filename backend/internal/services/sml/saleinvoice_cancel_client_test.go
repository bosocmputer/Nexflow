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

func TestSaleInvoiceCancelClientReadsNestedBusinessStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"status":"already_exists","message":"credit note already exists","existing_cancel_doc_no":"CN26060001"}}`))
	}))
	defer srv.Close()

	client := NewSaleInvoiceCancelClient(PartyConfig{
		BaseURL: srv.URL, GUID: "smlx", Provider: "SMLGOH", Database: "sml1_2026",
	}, zap.NewNop())
	_, resp, err := client.Create(context.Background(), "BF-SO260600001", SaleInvoiceCancelRequest{DocFormatCode: "CN"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.IsAlreadyExists() || resp.BusinessStatus() != "already_exists" {
		t.Fatalf("nested status not detected: %#v", resp)
	}
	if got := resp.CancelDocNo(); got != "CN26060001" {
		t.Fatalf("cancel doc no = %q", got)
	}
	if got := resp.GetMessage(); got != "credit note already exists" {
		t.Fatalf("message = %q", got)
	}
}

func TestSaleInvoiceCancelClientUsesVoidEndpointForSaleCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ic/sale-invoices/BF-INV26080058/void" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		for _, want := range []string{`"doc_no":"SIC26080002"`, `"doc_format_code":"SIC"`, `"doc_time":"17:15"`} {
			if !strings.Contains(string(body), want) {
				t.Fatalf("body %s missing %s", body, want)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"success":true,"data":{"cancel_doc_no":"SIC26080002"}}`))
	}))
	defer srv.Close()

	client := NewSaleInvoiceCancelClient(PartyConfig{
		BaseURL: srv.URL, GUID: "smlx", Provider: "SMLGOH", Database: "sml1_2026",
	}, zap.NewNop())
	status, resp, err := client.Create(context.Background(), "BF-INV26080058", SaleInvoiceCancelRequest{
		Kind: SaleInvoiceCancelKindVoid, DocNo: "SIC26080002",
		DocFormatCode: "SIC", DocTime: "17:15",
	})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusCreated || resp.CancelDocNo() != "SIC26080002" {
		t.Fatalf("status/response = %d/%#v", status, resp)
	}
}

func TestSaleInvoiceCancelClientUsesSaleOrderVoidProfileEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ic/sale-orders/SO26090001/void/preview" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		for _, want := range []string{
			`"document_profile_version":"sml-document-v1"`,
			`"doc_no":"BF-SSC26090001"`,
			`"remark_2":"full reversal"`,
			`"creator_code":"BILLFLOW"`,
			`"cashier_code":"BILLFLOW"`,
			`"user_request":"NEXFLOW"`,
		} {
			if !strings.Contains(string(body), want) {
				t.Fatalf("body %s missing %s", body, want)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"status":"ready","cancel_doc_no":"BF-SSC26090001","payload_hash":"abc","core_status":"pending","profile_status":"pending","reconciliation_required":false}}`))
	}))
	defer srv.Close()

	client := NewSaleInvoiceCancelClient(PartyConfig{
		BaseURL: srv.URL, GUID: "smlx", Provider: "SMLGOH", Database: "sml1_2026",
	}, zap.NewNop())
	status, resp, err := client.Preview(context.Background(), "SO26090001", SaleInvoiceCancelRequest{
		Kind: SaleInvoiceCancelKindSaleOrder, DocumentProfileVersion: "sml-document-v1",
		DocNo: "BF-SSC26090001", Remark2: "full reversal", CreatorCode: "BILLFLOW",
		CashierCode: "BILLFLOW", UserRequest: "NEXFLOW",
	})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || resp.CancelDocNo() != "BF-SSC26090001" || resp.ProfileStatus() != "pending" {
		t.Fatalf("status/response = %d/%#v profile=%q", status, resp, resp.ProfileStatus())
	}
}

func TestSaleInvoiceCancelClientRejectsUnknownKindBeforeHTTP(t *testing.T) {
	client := NewSaleInvoiceCancelClient(PartyConfig{
		BaseURL: "http://127.0.0.1:1", GUID: "smlx", Provider: "SMLGOH", Database: "sml1_2026",
	}, zap.NewNop())
	_, _, err := client.Preview(context.Background(), "BF-INV26080058", SaleInvoiceCancelRequest{Kind: "unknown"})
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("error = %v", err)
	}
}
