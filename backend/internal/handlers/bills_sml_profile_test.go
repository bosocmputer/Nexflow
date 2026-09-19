package handlers

import (
	"context"
	"encoding/json"
	"testing"

	"nexflow/internal/config"
	"nexflow/internal/models"
	"nexflow/internal/services/smlprofile"
)

func TestResolveInvoiceDocumentProfileUsesDocumentPrecedenceAndOmitsShipment(t *testing.T) {
	h := &BillHandler{cfg: &config.Config{SMLDocumentProfileMode: smlprofile.ModeActive}}
	bill := &models.Bill{
		ID: "bill-1", BillType: "sale", Source: "shopee", Remark: "bill fallback",
		RawData: json.RawMessage(`{
			"flow":"shopee_realtime","shopee_order_id":"ORDER-1",
			"recipient_address":{"name":"Synthetic","full_address":"Synthetic address","phone":"0000000000"}
		}`),
	}
	def := &models.ChannelDefault{
		Channel: "shopee_realtime", BillType: "sale", Remark: "{{channel}}/{{order_ref}}/{{bill_no}}",
		Remark2: "default note", ConfigVersion: 7, WHCode: "AB-1", ShelfCode: "001",
	}
	got, err := h.resolveInvoiceDocumentProfile(context.Background(), bill, def, RetryRequest{}, "BF-INV1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Remark != "{{channel}}/{{order_ref}}/{{bill_no}}" || got.Remark2 != "default note" {
		t.Fatalf("resolved text=%q/%q", got.Remark, got.Remark2)
	}
	if got.Options.Remark5 != "NEXFLOW|shopee_realtime|ORDER-1" || got.Options.ConfigVersion != 7 || got.Options.RouteSignature == "" {
		t.Fatalf("profile authority=%+v", got.Options)
	}
	if !got.Options.MarketplacePhysicalGoods || got.Options.ShipmentApplicability != "not_applicable" || got.Options.Shipment != nil {
		t.Fatalf("shipment policy=%+v", got.Options)
	}

	got, err = h.resolveInvoiceDocumentProfile(context.Background(), bill, def, RetryRequest{Remark: "explicit", Remark2: "explicit-2"}, "BF-INV1")
	if err != nil || got.Remark != "explicit" || got.Remark2 != "explicit-2" {
		t.Fatalf("explicit precedence got=%+v err=%v", got, err)
	}
}

func TestResolveInvoiceDocumentProfileDoesNotRequireMarketplaceShipmentForStockFocusedDocuments(t *testing.T) {
	h := &BillHandler{cfg: &config.Config{SMLDocumentProfileMode: smlprofile.ModeActive}}
	bill := &models.Bill{BillType: "sale", Source: "tiktok", RawData: json.RawMessage(`{
		"flow":"tiktok_excel","tiktok_order_id":"ORDER-1","customer_name":"BUYER"
	}`)}

	got, err := h.resolveInvoiceDocumentProfile(context.Background(), bill,
		&models.ChannelDefault{Channel: "tiktok", BillType: "sale"}, RetryRequest{}, "BF-1")
	if err != nil {
		t.Fatalf("marketplace document without shipment must remain sendable: %v", err)
	}
	if !got.Options.MarketplacePhysicalGoods || got.Options.ShipmentApplicability != "not_applicable" || got.Options.Shipment != nil {
		t.Fatalf("shipment policy=%+v", got.Options)
	}
}

func TestResolveSaleOrderDocumentProfileUsesRouteScopedMode(t *testing.T) {
	h := &BillHandler{cfg: &config.Config{
		SMLDocumentProfileMode: smlprofile.ModeActive,
		SMLDocumentProfileRouteModes: map[string]string{
			"saleinvoice": smlprofile.ModeActive,
			"saleorder":   smlprofile.ModeShadow,
		},
	}}
	bill := &models.Bill{BillType: "sale", Source: "manual", Remark: "sale order"}
	got, err := h.resolveSaleOrderDocumentProfile(context.Background(), bill,
		&models.ChannelDefault{Channel: "manual", BillType: "sale", ConfigVersion: 2}, RetryRequest{}, "BF-SO1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != smlprofile.ModeShadow || got.Options.Mode != smlprofile.ModeShadow || got.Options.ShipmentApplicability != "not_applicable" {
		t.Fatalf("sale order route profile=%+v", got)
	}
}

func TestShopeeRouteSignatureIncludesProfileAuthority(t *testing.T) {
	cfg := ShopeeConfigRequest{Endpoint: "saleinvoice", DocFormat: "SI", CustCode: "AR-1", WHCode: "AB-1", ShelfCode: "001", VATType: 1, VATRate: 7}
	def := &models.ChannelDefault{Channel: "shopee_realtime", BillType: "sale", ConfigVersion: 1, Remark: "one"}
	base := shopeeRealtimeRouteSignature(cfg, def, smlprofile.ModeShadow)
	changed := *def
	changed.Remark = "two"
	if shopeeRealtimeRouteSignature(cfg, &changed, smlprofile.ModeShadow) == base {
		t.Fatal("remark must change route signature")
	}
	if shopeeRealtimeRouteSignature(cfg, def, smlprofile.ModeActive) == base {
		t.Fatal("profile mode/version authority must change route signature")
	}
}
