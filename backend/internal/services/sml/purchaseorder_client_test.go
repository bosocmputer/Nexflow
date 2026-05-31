package sml

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestBuildPurchaseOrderPayloadAppliesLineDiscounts(t *testing.T) {
	payload := BuildPurchaseOrderPayload(
		"PO-1",
		"2026-05-21",
		"#2605211KR3XK1G",
		"2026-05-21",
		[]POItem{
			{ItemCode: "ITEM-1", ItemName: "สินค้า 1", Qty: 1, Price: 100, DiscountAmount: 10, UnitCode: "ชิ้น"},
			{ItemCode: "ITEM-2", ItemName: "สินค้า 2", Qty: 2, Price: 50, DiscountAmount: 5, UnitCode: "ชิ้น"},
		},
		PurchaseOrderConfig{DocFormat: "PO", CustCode: "V001", VATType: 2, VATRate: 0, UnitCode: "ชิ้น"},
		"",
	)

	if len(payload.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(payload.Items))
	}
	if payload.Items[0].DiscountAmount != 10 || payload.Items[0].SumAmount != 90 {
		t.Fatalf("first line = %+v, want discount=10 sum=90", payload.Items[0])
	}
	if payload.Items[1].DiscountAmount != 5 || payload.Items[1].SumAmount != 95 {
		t.Fatalf("second line = %+v, want discount=5 sum=95", payload.Items[1])
	}
	if payload.TotalValue != 185 || payload.TotalDiscount != 0 || payload.TotalAmount != 185 {
		t.Fatalf("totals value=%v discount=%v amount=%v, want 185/0/185",
			payload.TotalValue, payload.TotalDiscount, payload.TotalAmount)
	}
}

func TestBuildPurchaseOrderPayloadCapsLineDiscountAtGross(t *testing.T) {
	payload := BuildPurchaseOrderPayload(
		"PO-1",
		"2026-05-21",
		"#2605211KR3XK1G",
		"2026-05-21",
		[]POItem{{ItemCode: "ITEM-1", Qty: 1, Price: 100, DiscountAmount: 150, UnitCode: "ชิ้น"}},
		PurchaseOrderConfig{DocFormat: "PO", CustCode: "V001", VATType: 2, UnitCode: "ชิ้น"},
		"",
	)

	if payload.Items[0].DiscountAmount != 100 || payload.Items[0].SumAmount != 0 {
		t.Fatalf("line = %+v, want capped discount=100 sum=0", payload.Items[0])
	}
	if payload.TotalValue != 0 || payload.TotalDiscount != 0 || payload.TotalAmount != 0 {
		t.Fatalf("totals value=%v discount=%v amount=%v, want 0/0/0",
			payload.TotalValue, payload.TotalDiscount, payload.TotalAmount)
	}
}

func TestBuildPurchaseOrderPayloadIncludesHeaderFields(t *testing.T) {
	payload := BuildPurchaseOrderPayload(
		"PO-1",
		"2026-05-21",
		"7275",
		"",
		[]POItem{{ItemCode: "ITEM-1", Qty: 1, Price: 100, UnitCode: "ชิ้น"}},
		PurchaseOrderConfig{DocFormat: "PO", CustCode: "V001", VATType: 2, UnitCode: "ชิ้น", WHCode: "WH-01", ShelfCode: "SH-01"},
		"alove922",
		PurchaseOrderHeaderOptions{
			Remark:      "alove922",
			Remark2:     "tax",
			Remark5:     "2605211KR3XK1G",
			InquiryType: 1,
		},
	)

	if payload.Remark != "alove922" || payload.Remark2 != "tax" || payload.Remark5 != "2605211KR3XK1G" {
		t.Fatalf("remark fields = %q/%q/%q", payload.Remark, payload.Remark2, payload.Remark5)
	}
	if payload.DocRef != "7275" || payload.Items[0].DocRef != "7275" {
		t.Fatalf("doc_ref payload=%q item=%q, want 7275", payload.DocRef, payload.Items[0].DocRef)
	}
	if payload.InquiryType != 1 {
		t.Fatalf("inquiry_type = %d, want 1", payload.InquiryType)
	}
	if payload.Items[0].WHCode2 != "WH-01" || payload.Items[0].ShelfCode2 != "SH-01" {
		t.Fatalf("secondary warehouse/shelf = %q/%q, want WH-01/SH-01", payload.Items[0].WHCode2, payload.Items[0].ShelfCode2)
	}
}

func TestSaleAndInvoicePayloadIncludeRemark2WhenProvided(t *testing.T) {
	saleOrder := BuildSaleOrderPayload(
		"SO-1",
		"2026-05-25",
		"",
		"",
		[]SOItem{{ItemCode: "ITEM-1", Qty: 1, Price: 100}},
		SaleOrderConfig{DocFormat: "SO", CustCode: "AR001", VATType: 2, UnitCode: "ชิ้น"},
		"",
		SaleOrderHeaderOptions{Remark2: "notax"},
	)
	if saleOrder.Remark2 != "notax" {
		t.Fatalf("saleorder remark_2 = %q, want notax", saleOrder.Remark2)
	}

	invoice := BuildInvoicePayload(
		"INV-1",
		"2026-05-25",
		"",
		"",
		[]ShopeeOrderItem{{SKU: "ITEM-1", ProductName: "สินค้า", Qty: 1, Price: 100}},
		InvoiceConfig{DocFormat: "INV", CustCode: "AR001", VATType: 2, UnitCode: "ชิ้น"},
		map[string]*ProductInfo{},
		"",
		InvoiceHeaderOptions{Remark2: "re"},
	)
	if invoice.Remark2 != "re" {
		t.Fatalf("saleinvoice remark_2 = %q, want re", invoice.Remark2)
	}
}

func TestRemark2IsOmittedWhenEmpty(t *testing.T) {
	payload := BuildPurchaseOrderPayload(
		"PO-1",
		"2026-05-25",
		"",
		"",
		[]POItem{{ItemCode: "ITEM-1", Qty: 1, Price: 100, UnitCode: "ชิ้น"}},
		PurchaseOrderConfig{DocFormat: "PO", CustCode: "V001", VATType: 2, UnitCode: "ชิ้น"},
		"",
	)
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte("remark_2")) {
		t.Fatalf("remark_2 should be omitted when empty: %s", string(body))
	}
}
