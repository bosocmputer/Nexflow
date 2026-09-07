package sml

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestSaleOrderRawSendReplaysExactPayloadBytes(t *testing.T) {
	wantBody := []byte(`{"doc_no":"BF-SO26080001","doc_date":"2026-08-24","doc_time":"10:00","doc_format_code":"SO","cust_code":"AR1","sale_code":"","sale_type":0,"vat_type":2,"vat_rate":0,"total_value":10,"total_discount":0,"total_before_vat":10,"total_vat_value":0,"total_except_vat":0,"total_after_vat":10,"total_amount":10,"items":[],"expand_set_items":false,"remark":"\u0e44\u0e17\u0e22"}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ := io.ReadAll(r.Body)
		if string(got) != string(wantBody) {
			t.Errorf("wire body changed\n got: %s\nwant: %s", got, wantBody)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"doc_no":"BF-SO26080001"}}`))
	}))
	defer server.Close()

	client := NewSaleOrderClient(SaleOrderConfig{BaseURL: server.URL}, nil)
	status, response, responseBytes, err := client.CreateSaleOrderBytes(wantBody, "")
	if err != nil || status != http.StatusOK || response == nil || !response.IsSuccess() {
		t.Fatalf("status=%d response=%#v err=%v", status, response, err)
	}
	if string(responseBytes) != `{"status":"success","data":{"doc_no":"BF-SO26080001"}}` {
		t.Fatalf("response bytes = %s", responseBytes)
	}
}

func TestSaleOrderProfileFieldsDoNotChangeLegacyMarshal(t *testing.T) {
	wantBody := []byte(`{"doc_no":"BF-SO26080001","doc_date":"2026-08-24","doc_time":"10:00","doc_format_code":"SO","cust_code":"AR1","sale_code":"","sale_type":0,"vat_type":2,"vat_rate":0,"inquiry_type":0,"total_value":10,"total_discount":0,"total_before_vat":10,"total_vat_value":0,"total_except_vat":0,"total_after_vat":10,"total_amount":10,"items":[],"expand_set_items":false}`)
	var payload SaleOrderPayload
	if err := json.Unmarshal(wantBody, &payload); err != nil {
		t.Fatal(err)
	}
	got, err := MarshalASCII(payload)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(wantBody) {
		t.Fatalf("legacy Sale Order wire changed\n got: %s\nwant: %s", got, wantBody)
	}
}

func TestInvoiceRawSendReplaysExactPayloadBytes(t *testing.T) {
	wantBody := []byte(`{"doc_no":"BF-INV26080001","doc_date":"2026-08-24","details":[],"remark":"\u0e44\u0e17\u0e22"}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ := io.ReadAll(r.Body)
		if string(got) != string(wantBody) {
			t.Errorf("wire body changed\n got: %s\nwant: %s", got, wantBody)
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"doc_no":"BF-INV26080001"}}`))
	}))
	defer server.Close()

	client := NewInvoiceClient(InvoiceConfig{BaseURL: server.URL}, nil)
	status, response, _, err := client.CreateInvoiceBytes(wantBody, "")
	if err != nil || status != http.StatusOK || response == nil || !response.IsSuccess() {
		t.Fatalf("status=%d response=%#v err=%v", status, response, err)
	}
}

func TestInvoiceRawSendPropagatesCorrelationWithoutChangingPayload(t *testing.T) {
	wantBody := []byte(`{"document_profile_version":"sml-document-v1","doc_no":"BF-1"}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ := io.ReadAll(r.Body)
		if string(got) != string(wantBody) {
			t.Errorf("wire body changed: %s", got)
		}
		if got := r.Header.Get("X-Correlation-ID"); got != "ui-12345678" {
			t.Errorf("correlation header=%q", got)
		}
		_, _ = w.Write([]byte(`{"success":true,"data":{"doc_no":"BF-1"}}`))
	}))
	defer server.Close()
	client := NewInvoiceClient(InvoiceConfig{BaseURL: server.URL}, nil)
	status, response, _, err := client.CreateInvoiceBytesWithCorrelation(wantBody, "", "ui-12345678")
	if err != nil || status != http.StatusOK || response == nil || !response.IsSuccess() {
		t.Fatalf("status=%d response=%#v err=%v", status, response, err)
	}
}

func TestInvoiceDiagnosticsHooksRunAroundBoundedOutboundExchange(t *testing.T) {
	wantBody := []byte(`{"doc_no":"BF-1"}`)
	responseBody := `{"message":"` + strings.Repeat("x", MaxDiagnosticResponseBytes) + `"}`
	var beforeCalls, afterCalls atomic.Int32
	var captured HTTPExchangeResult
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if beforeCalls.Load() != 1 {
			t.Error("exchange evidence was not started before outbound call")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-ID", "request-1")
		_, _ = w.Write([]byte(responseBody))
	}))
	defer server.Close()

	client := NewInvoiceClient(InvoiceConfig{BaseURL: server.URL}, nil)
	_, _, raw, _ := client.CreateInvoiceBytesWithDiagnostics(wantBody, "", "trace-1", &HTTPExchangeHooks{
		Before: func(request HTTPExchangeRequest) string {
			beforeCalls.Add(1)
			if request.CanonicalPath != "/api/v1/ic/sale-invoices" || request.CorrelationID != "trace-1" {
				t.Errorf("request metadata = %#v", request)
			}
			return "exchange-1"
		},
		After: func(id string, result HTTPExchangeResult) {
			afterCalls.Add(1)
			if id != "exchange-1" {
				t.Errorf("exchange id = %q", id)
			}
			captured = result
		},
	})

	if beforeCalls.Load() != 1 || afterCalls.Load() != 1 {
		t.Fatalf("before=%d after=%d", beforeCalls.Load(), afterCalls.Load())
	}
	if len(raw) != MaxDiagnosticResponseBytes || len(captured.Body) != MaxDiagnosticResponseBytes {
		t.Fatalf("response reads were not bounded: raw=%d captured=%d", len(raw), len(captured.Body))
	}
	if !captured.Truncated || captured.ResponseHash == "" || captured.ResponseSize != MaxDiagnosticResponseBytes {
		t.Fatalf("truncated=%t hash_present=%t size=%d", captured.Truncated, captured.ResponseHash != "", captured.ResponseSize)
	}
}

func TestSaleOrderDiagnosticsRecordsEveryInternalRetry(t *testing.T) {
	wantBody := []byte(`{"doc_no":"BF-SO1","items":[]}`)
	var serverCalls atomic.Int32
	var mu sync.Mutex
	statuses := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if serverCalls.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"status":"failed","code":"temporary"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"doc_no":"BF-SO1"}}`))
	}))
	defer server.Close()

	client := NewSaleOrderClient(SaleOrderConfig{BaseURL: server.URL}, nil)
	status, response, _, err := client.CreateSaleOrderBytesWithDiagnostics(wantBody, "", "trace-1", &HTTPExchangeHooks{
		Before: func(request HTTPExchangeRequest) string {
			if request.CanonicalPath != "/api/v1/ic/sale-orders" {
				t.Errorf("request metadata = %#v", request)
			}
			return "exchange-" + string(rune('0'+serverCalls.Load()+1))
		},
		After: func(_ string, result HTTPExchangeResult) {
			mu.Lock()
			statuses = append(statuses, result.Status)
			mu.Unlock()
		},
	})
	if err != nil || status != http.StatusOK || response == nil || !response.IsSuccess() {
		t.Fatalf("status=%d response=%#v err=%v", status, response, err)
	}
	if serverCalls.Load() != 2 {
		t.Fatalf("server calls = %d", serverCalls.Load())
	}
	assertStringsEqual(t, statuses, []string{"failed", "succeeded"})
}

func TestDiagnosticHookFailureDoesNotChangeInvoiceResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"doc_no":"BF-1"}}`))
	}))
	defer server.Close()

	client := NewInvoiceClient(InvoiceConfig{BaseURL: server.URL}, nil)
	status, response, _, err := client.CreateInvoiceBytesWithDiagnostics(
		[]byte(`{"doc_no":"BF-1"}`), "", "trace-1", &HTTPExchangeHooks{
			Before: func(HTTPExchangeRequest) string { panic("diagnostics unavailable") },
			After:  func(string, HTTPExchangeResult) { panic("diagnostics unavailable") },
		},
	)
	if err != nil || status != http.StatusOK || response == nil || !response.IsSuccess() {
		t.Fatalf("diagnostics changed result: status=%d response=%#v err=%v", status, response, err)
	}
}

func assertStringsEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("got=%v want=%v", got, want)
		}
	}
}

func TestInvoiceRetryAfterUnknownPostCommitResultReplaysExactBytes(t *testing.T) {
	wantBody := []byte(`{"document_profile_version":"sml-document-v1","doc_no":"BF-1"}`)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ := io.ReadAll(r.Body)
		if string(got) != string(wantBody) {
			t.Errorf("immutable retry body changed: %s", got)
		}
		if calls.Add(1) == 1 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			_ = conn.Close() // Simulate a lost HTTP response after the external commit.
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"doc_no":"BF-1","status":"already_exists","payload_hash":"hash-1","core_status":"already_exists","profile_status":"complete","reconciliation_required":false}}`))
	}))
	defer server.Close()

	client := NewInvoiceClient(InvoiceConfig{BaseURL: server.URL}, nil)
	if _, _, _, err := client.CreateInvoiceBytes(wantBody, ""); err == nil {
		t.Fatal("first request should have an unknown transport result")
	}
	status, response, _, err := client.CreateInvoiceBytes(wantBody, "")
	if err != nil || status != http.StatusOK || response == nil || !response.IsSuccess() || calls.Load() != 2 {
		t.Fatalf("status=%d response=%#v calls=%d err=%v", status, response, calls.Load(), err)
	}
}

func TestImmutableSalesPayloadLimitsFailBeforeNetwork(t *testing.T) {
	invoiceClient := NewInvoiceClient(InvoiceConfig{BaseURL: "http://127.0.0.1:1"}, nil)
	if _, _, _, err := invoiceClient.CreateInvoiceBytes(make([]byte, MaxInvoiceDocumentBytes+1), ""); err == nil {
		t.Fatal("oversized invoice payload must fail before network access")
	}
	invoiceBody, err := json.Marshal(InvoicePayload{Details: make([]InvoiceDetail, MaxInvoiceDocumentItems+1)})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := invoiceClient.CreateInvoiceBytes(invoiceBody, ""); err == nil {
		t.Fatal("invoice with more than 500 details must fail before network access")
	}

	saleOrderClient := NewSaleOrderClient(SaleOrderConfig{BaseURL: "http://127.0.0.1:1"}, nil)
	orderBody, err := json.Marshal(SaleOrderPayload{Items: make([]SaleOrderItem, MaxInvoiceDocumentItems+1)})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := saleOrderClient.CreateSaleOrderBytes(orderBody, ""); err == nil {
		t.Fatal("sale order with more than 500 items must fail before network access")
	}
}
