package sml

import (
	"encoding/json"
	"testing"
)

func TestAPIErrorMessage(t *testing.T) {
	msg := apiErrorMessage(map[string]interface{}{
		"code":    "duplicate_doc_no",
		"message": "doc already exists",
	})
	if msg != "doc already exists" {
		t.Fatalf("message = %q", msg)
	}
}

func TestSaleOrderResponseGetMessageFromNexflowNativeError(t *testing.T) {
	resp := SaleOrderResponse{Error: map[string]interface{}{
		"code":    "product_not_found",
		"message": "item 0 product not found",
	}}
	if resp.GetMessage() != "item 0 product not found" {
		t.Fatalf("message = %q", resp.GetMessage())
	}
}

func TestInvoiceResponseReadsCodeFromGatewayErrorEnvelope(t *testing.T) {
	var resp InvoiceResponse
	if err := json.Unmarshal([]byte(`{
		"success": false,
		"error": {
			"code": "doc_no_payload_mismatch",
			"message": "document number belongs to a different payload"
		}
	}`), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got := resp.GetCode(); got != "doc_no_payload_mismatch" {
		t.Fatalf("code = %q", got)
	}
	if got := resp.GetMessage(); got != "document number belongs to a different payload" {
		t.Fatalf("message = %q", got)
	}
}

func TestResponseTopLevelCodeTakesPrecedence(t *testing.T) {
	resp := SaleOrderResponse{
		Code: "top_level",
		Error: map[string]interface{}{
			"code": "nested",
		},
	}
	if got := resp.GetCode(); got != "top_level" {
		t.Fatalf("code = %q", got)
	}
}
