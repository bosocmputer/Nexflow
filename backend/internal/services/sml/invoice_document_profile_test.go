package sml

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func profileInvoiceForTest() InvoicePayload {
	return InvoicePayload{
		VATType: 1, VATRate: 7, TotalValue: 100, TotalBeforeVAT: 93.46,
		TotalVATValue: 6.54, TotalAfterVAT: 100, TotalAmount: 100,
		Details: []InvoiceDetail{{LineNumber: 0, ItemCode: "AH-1", UnitCode: "ชิ้น", Qty: 1,
			Price: 100, PriceExcludeVAT: 93.46, SumAmount: 100, VATAmount: 6.54, SumAmountExclVAT: 93.46}},
	}
}

func TestApplyInvoiceDocumentProfileActiveAddsExactAuthority(t *testing.T) {
	payload := profileInvoiceForTest()
	err := ApplyInvoiceDocumentProfile(&payload, InvoiceDocumentProfileOptions{
		Mode: "active", Channel: "shopee_realtime", ConfigVersion: 3, RouteSignature: "route-1",
		Remark5: "NEXFLOW|shopee_realtime|ORDER-1", MarketplacePhysicalGoods: true,
		ShipmentApplicability: "required", Shipment: &InvoiceShipment{
			TransportName: "Synthetic", TransportAddress: "Synthetic address", TransportTelephone: "0000000000",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if payload.DocumentProfileVersion != InvoiceDocumentProfileVersion || payload.TotalAmountDecimal != "100.00" || payload.Details[0].QtyDecimal != "1.000000" {
		t.Fatalf("payload=%+v detail=%+v", payload, payload.Details[0])
	}
	if payload.CreatorCode != "BILLFLOW" || payload.UserRequest != "NEXFLOW" || payload.ProfileConfigVersion != 3 {
		t.Fatalf("system/snapshot fields missing: %+v", payload)
	}
}

func TestApplyInvoiceDocumentProfileShadowValidatesButDoesNotOptIn(t *testing.T) {
	payload := profileInvoiceForTest()
	if err := ApplyInvoiceDocumentProfile(&payload, InvoiceDocumentProfileOptions{
		Mode: "shadow", Remark5: "NEXFLOW|manual|BF-1", ShipmentApplicability: "not_applicable",
	}); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(payload)
	if strings.Contains(string(body), "document_profile_version") {
		t.Fatalf("shadow must not opt in to Gateway supplements: %s", body)
	}
	if payload.TotalValueDecimal == "" || payload.ProfileMode != "shadow" {
		t.Fatalf("shadow did not build exact profile: %+v", payload)
	}
}

func TestApplyInvoiceDocumentProfileFailsClosedForIncompleteMarketplaceShipment(t *testing.T) {
	payload := profileInvoiceForTest()
	err := ApplyInvoiceDocumentProfile(&payload, InvoiceDocumentProfileOptions{
		Mode: "active", Remark5: "NEXFLOW|shopee|ORDER-1", MarketplacePhysicalGoods: true,
		ShipmentApplicability: "required", Shipment: &InvoiceShipment{TransportName: "Name"},
	})
	if err == nil || !strings.Contains(err.Error(), "transport_address") {
		t.Fatalf("error=%v", err)
	}
}

func TestInvoiceResponseTreatsMissingActiveProfileResultAsReconciliation(t *testing.T) {
	response := &InvoiceResponse{Success: true}
	response.Data.DocNo = "BF-1"
	got := response.DocumentProfileResult(InvoiceDocumentProfileVersion)
	if got.CoreStatus != "created" || got.ProfileStatus != "needs_reconciliation" || !got.ReconciliationRequired {
		t.Fatalf("result=%+v", got)
	}
}

func BenchmarkApplyInvoiceDocumentProfile(b *testing.B) {
	for _, itemCount := range []int{1, 10, 50, 200} {
		b.Run(fmt.Sprintf("items_%d", itemCount), func(b *testing.B) {
			base := InvoicePayload{
				VATRate: 7, TotalValue: float64(itemCount), TotalBeforeVAT: float64(itemCount),
				TotalAfterVAT: float64(itemCount), TotalAmount: float64(itemCount),
				Details: make([]InvoiceDetail, itemCount),
			}
			for i := range base.Details {
				base.Details[i] = InvoiceDetail{ItemCode: fmt.Sprintf("ITEM-%03d", i), UnitCode: "EA", Qty: 1, Price: 1, PriceExcludeVAT: 1, SumAmount: 1, SumAmountExclVAT: 1}
			}
			opts := InvoiceDocumentProfileOptions{
				Mode: "active", Channel: "benchmark", Remark5: "NEXFLOW|benchmark|BF-BENCH",
				ShipmentApplicability: "not_applicable",
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				payload := base
				payload.Details = append([]InvoiceDetail(nil), base.Details...)
				if err := ApplyInvoiceDocumentProfile(&payload, opts); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
