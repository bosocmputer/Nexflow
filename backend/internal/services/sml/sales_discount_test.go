package sml

import "testing"

func TestBuildSaleOrderPayloadKeepsGrossLineAndAppliesHeaderDiscount(t *testing.T) {
	gross := 100.0
	payload := BuildSaleOrderPayload("BF-SO26080001", "2026-08-14", "TT-1", "2026-08-14", []SOItem{{
		ItemCode: "SKU-1", Qty: 3, Price: 33.33, GrossAmount: &gross, DiscountAmount: 10, UnitCode: "ชิ้น",
	}}, SaleOrderConfig{DocFormat: "SO", CustCode: "AR001", UnitCode: "ชิ้น", VATType: 1, VATRate: 7}, "")

	if payload.TotalValue != 100 || payload.TotalDiscount != 10 || payload.TotalBeforeVAT != 84.11 ||
		payload.TotalVATValue != 5.89 || payload.TotalAmount != 90 {
		t.Fatalf("header totals = gross %.2f discount %.2f before %.2f vat %.2f amount %.2f",
			payload.TotalValue, payload.TotalDiscount, payload.TotalBeforeVAT, payload.TotalVATValue, payload.TotalAmount)
	}
	if len(payload.Items) != 1 || payload.Items[0].SumAmount != 100 || payload.Items[0].DiscountAmount != 0 {
		t.Fatalf("detail = %+v, want gross 100 and line discount 0", payload.Items)
	}
	if payload.Items[0].Price != 33.333333 {
		t.Fatalf("detail price = %.6f, want exact gross/qty", payload.Items[0].Price)
	}
}

func TestBuildSaleOrderPayloadKeepsMarketplaceGrossWhenQuantityIsConverted(t *testing.T) {
	gross := 120.0
	payload := BuildSaleOrderPayload("BF-SO26080002", "2026-08-14", "TT-3", "2026-08-14", []SOItem{{
		ItemCode: "SKU-PACK", Qty: 4, Price: 60, GrossAmount: &gross, UnitCode: "ชิ้น",
	}}, SaleOrderConfig{DocFormat: "SO", CustCode: "AR001", UnitCode: "ชิ้น", VATType: 2}, "")

	if payload.TotalValue != 120 || payload.TotalAmount != 120 {
		t.Fatalf("header totals changed after conversion: %+v", payload)
	}
	if len(payload.Items) != 1 || payload.Items[0].Qty != 4 || payload.Items[0].Price != 30 || payload.Items[0].SumAmount != 120 {
		t.Fatalf("converted detail = %+v, want qty=4 price=30 gross=120", payload.Items)
	}
}

func TestBuildInvoicePayloadCalculatesExcludedVATAfterDiscount(t *testing.T) {
	gross := 100.0
	payload := BuildInvoicePayload("BF-INV26080001", "2026-08-14", "TT-2", "2026-08-14", []ShopeeOrderItem{{
		SKU: "SKU-1", Qty: 1, Price: 100, GrossAmount: &gross, DiscountAmount: 10,
	}}, InvoiceConfig{DocFormat: "SI", CustCode: "AR001", UnitCode: "ชิ้น", VATType: 0, VATRate: 7}, nil, "")

	if payload.TotalValue != 100 || payload.TotalDiscount != 10 || payload.TotalBeforeVAT != 90 ||
		payload.TotalVATValue != 6.3 || payload.TotalAfterVAT != 96.3 || payload.TotalAmount != 96.3 {
		t.Fatalf("header totals = %+v", payload)
	}
	if payload.Details[0].SumAmount != 100 || payload.Details[0].VATAmount != 7 || payload.Details[0].DiscountAmount != 0 {
		t.Fatalf("detail = %+v, want gross VAT with no line discount", payload.Details[0])
	}
}

func TestBuildInvoicePayloadMatchesSMLHeaderTotalsForEveryVATMode(t *testing.T) {
	tests := []struct {
		name                            string
		vatType                         int
		beforeVAT, vat, afterVAT, total float64
	}{
		{name: "external", vatType: 0, beforeVAT: 300, vat: 21, afterVAT: 321, total: 321},
		{name: "included", vatType: 1, beforeVAT: 280.37, vat: 19.63, afterVAT: 300, total: 300},
		{name: "zero_rate", vatType: 2, beforeVAT: 0, vat: 0, afterVAT: 0, total: 300},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gross := 300.0
			payload := BuildInvoicePayload("BF-INV26090001", "2026-09-02", "ORDER-1", "2026-09-02", []ShopeeOrderItem{{
				SKU: "SKU-1", Qty: 1, Price: 300, GrossAmount: &gross,
			}}, InvoiceConfig{DocFormat: "SI", CustCode: "AR001", UnitCode: "ชิ้น", VATType: tt.vatType, VATRate: 7}, nil, "")
			if payload.TotalBeforeVAT != tt.beforeVAT || payload.TotalVATValue != tt.vat ||
				payload.TotalAfterVAT != tt.afterVAT || payload.TotalAmount != tt.total {
				t.Fatalf("VAT type %d totals before/vat/after/total = %.2f/%.2f/%.2f/%.2f",
					tt.vatType, payload.TotalBeforeVAT, payload.TotalVATValue, payload.TotalAfterVAT, payload.TotalAmount)
			}
		})
	}
}
