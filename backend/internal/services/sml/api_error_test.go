package sml

import "testing"

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
