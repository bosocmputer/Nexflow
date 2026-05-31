package sml

import (
	"encoding/json"
	"testing"
)

func TestSMLLinePayloadsHardcodeIsGetPrice(t *testing.T) {
	t.Run("saleorder", func(t *testing.T) {
		payload := BuildSaleOrderPayload(
			"SO-1",
			"2026-05-11",
			"",
			"",
			[]SOItem{{ItemCode: "ITEM-1", ItemName: "สินค้า", Qty: 1, Price: 10, UnitCode: "ชิ้น"}},
			SaleOrderConfig{DocFormat: "SO", CustCode: "AR0001", UnitCode: "ชิ้น", WHCode: "WH-01", ShelfCode: "SH-01", VATType: 0, VATRate: 7, DocTime: "09:00"},
			"",
		)
		if len(payload.Items) != 1 || payload.Items[0].IsGetPrice != 1 {
			t.Fatalf("saleorder is_get_price = %+v, want 1", payload.Items)
		}
		assertJSONHasIsGetPrice(t, payload.Items[0])
	})

	t.Run("saleinvoice", func(t *testing.T) {
		payload := BuildInvoicePayload(
			"INV-1",
			"2026-05-11",
			"",
			"",
			[]ShopeeOrderItem{{SKU: "ITEM-1", ProductName: "สินค้า", Qty: 1, Price: 10}},
			InvoiceConfig{DocFormat: "INV", CustCode: "AR0001", UnitCode: "ชิ้น", WHCode: "WH-01", ShelfCode: "SH-01", VATType: 0, VATRate: 7, DocTime: "09:00"},
			map[string]*ProductInfo{},
			"",
		)
		if len(payload.Details) != 1 || payload.Details[0].IsGetPrice != 1 {
			t.Fatalf("saleinvoice is_get_price = %+v, want 1", payload.Details)
		}
		assertJSONHasIsGetPrice(t, payload.Details[0])
	})

	t.Run("purchaseorder", func(t *testing.T) {
		payload := BuildPurchaseOrderPayload(
			"PO-1",
			"2026-05-11",
			"",
			"",
			[]POItem{{ItemCode: "ITEM-1", ItemName: "สินค้า", Qty: 1, Price: 10, UnitCode: "ชิ้น"}},
			PurchaseOrderConfig{DocFormat: "PO", CustCode: "V0001", UnitCode: "ชิ้น", WHCode: "WH-01", ShelfCode: "SH-01", VATType: 0, VATRate: 7, DocTime: "09:00"},
			"",
		)
		if len(payload.Items) != 1 || payload.Items[0].IsGetPrice != 1 {
			t.Fatalf("purchaseorder is_get_price = %+v, want 1", payload.Items)
		}
		assertJSONHasIsGetPrice(t, payload.Items[0])
	})
}

func assertJSONHasIsGetPrice(t *testing.T, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if err := json.Unmarshal(b, &row); err != nil {
		t.Fatal(err)
	}
	if got := row["is_get_price"]; got != float64(1) {
		t.Fatalf("json is_get_price = %v in %s, want 1", got, b)
	}
}
