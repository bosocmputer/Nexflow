package sml

import (
	"encoding/json"
	"strings"
	"testing"
)

func profileSaleOrderForTest() SaleOrderPayload {
	return SaleOrderPayload{
		DocNo: "BF-SO26090001", DocDate: "2026-09-02", DocTime: "10:00", DocFormatCode: "SO",
		CustCode: "AR-1", VATType: 1, VATRate: 7, TotalValue: 100, TotalBeforeVAT: 93.46,
		TotalVATValue: 6.54, TotalAfterVAT: 100, TotalAmount: 100,
		Items: []SaleOrderItem{{LineNumber: 0, ItemCode: "AH-1", UnitCode: "ชิ้น", Qty: 1,
			Price: 100, PriceExcludeVAT: 93.46, SumAmount: 100, VATAmount: 6.54, SumAmountExclVAT: 93.46}},
	}
}

func TestApplySaleOrderDocumentProfileActiveAddsExactAuthority(t *testing.T) {
	payload := profileSaleOrderForTest()
	err := ApplySaleOrderDocumentProfile(&payload, InvoiceDocumentProfileOptions{
		Mode: "active", Channel: "shopee_realtime", ConfigVersion: 4, RouteSignature: "route-so",
		Remark5: "NEXFLOW|shopee_realtime|ORDER-1", MarketplacePhysicalGoods: true,
		ShipmentApplicability: "required", Shipment: &InvoiceShipment{
			TransportName: "Synthetic", TransportAddress: "Synthetic address", TransportTelephone: "0000000000",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if payload.DocumentProfileVersion != InvoiceDocumentProfileVersion || payload.TotalAmountDecimal != "100.00" || payload.Items[0].QtyDecimal != "1.000000" {
		t.Fatalf("payload=%+v item=%+v", payload, payload.Items[0])
	}
	if payload.CreatorCode != "BILLFLOW" || payload.UserRequest != "NEXFLOW" || payload.ProfileConfigVersion != 4 {
		t.Fatalf("system/snapshot fields missing: %+v", payload)
	}
}

func TestApplySaleOrderDocumentProfileShadowKeepsLegacyWireOptOut(t *testing.T) {
	payload := profileSaleOrderForTest()
	if err := ApplySaleOrderDocumentProfile(&payload, InvoiceDocumentProfileOptions{
		Mode: "shadow", Remark5: "NEXFLOW|manual|BF-SO1", ShipmentApplicability: "not_applicable",
	}); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(payload)
	if strings.Contains(string(body), "document_profile_version") {
		t.Fatalf("shadow must not opt in to Gateway Profile writes: %s", body)
	}
	if payload.TotalValueDecimal == "" || payload.ProfileMode != "shadow" {
		t.Fatalf("shadow did not build exact profile: %+v", payload)
	}
}

func TestSaleOrderResponseUsesUniformDocumentProfileResult(t *testing.T) {
	response := &SaleOrderResponse{Success: true}
	response.Data.DocNo = "BF-SO1"
	response.Data.CoreStatus = "created"
	response.Data.ProfileStatus = "complete"
	response.Data.RequiredChecks = []string{"core", "shipment", "main_log", "erp_log"}
	response.Data.CompletedChecks = append([]string(nil), response.Data.RequiredChecks...)
	got := response.DocumentProfileResult(InvoiceDocumentProfileVersion)
	if got.CoreStatus != "created" || got.ProfileStatus != "complete" || got.ReconciliationRequired {
		t.Fatalf("result=%+v", got)
	}
}
