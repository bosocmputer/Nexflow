package sml

import (
	"io"
	"net/http"
	"net/http/httptest"
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
