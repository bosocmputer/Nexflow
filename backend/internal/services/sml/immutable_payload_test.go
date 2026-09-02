package sml

import (
	"io"
	"net/http"
	"net/http/httptest"
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
