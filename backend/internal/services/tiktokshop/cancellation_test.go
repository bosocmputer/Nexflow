package tiktokshop

import "testing"

func TestTikTokCancellationEligibilityRequiresOwnedSuccessfulSale(t *testing.T) {
	base := TikTokCancellationEvidence{
		ShopID: "7494619203789490654", OrderID: "586030483469993439", OrderStatus: OrderStatusCancelled,
		BillID: "03ee1216-acb4-4a88-842c-7edc6eb44292", BillSource: "tiktok",
		BillSourceAccountKey: "shop:7494619203789490654", BillSourceFlow: TikTokReviewedBillFlow,
		BillStatus: "sent", BillDocumentRoute: "saleinvoice", BillSMLDocNo: "BF-INV26090001",
		SMLAttemptID: "11111111-1111-1111-1111-111111111111", SMLAttemptState: "sent",
		SMLAttemptRoute: "saleinvoice", SMLAttemptDocNo: "BF-INV26090001",
	}

	result := EvaluateTikTokCancellationEvidence(base)
	if !result.Eligible || result.Code != TikTokCancellationEligible {
		t.Fatalf("eligible result = %+v", result)
	}

	tests := []struct {
		name string
		edit func(*TikTokCancellationEvidence)
		code TikTokCancellationEligibilityCode
	}{
		{name: "not final", edit: func(e *TikTokCancellationEvidence) { e.OrderStatus = OrderStatusAwaitingCollection }, code: TikTokCancellationOrderNotFinal},
		{name: "no bill", edit: func(e *TikTokCancellationEvidence) { e.BillID = "" }, code: TikTokCancellationNotRequired},
		{name: "unsent bill", edit: func(e *TikTokCancellationEvidence) {
			e.BillStatus = "pending"
			e.BillSMLDocNo = ""
			e.SMLAttemptID = ""
		}, code: TikTokCancellationNotRequired},
		{name: "wrong shop scope", edit: func(e *TikTokCancellationEvidence) { e.BillSourceAccountKey = "shop:999" }, code: TikTokCancellationOwnershipMismatch},
		{name: "excel bill", edit: func(e *TikTokCancellationEvidence) { e.BillSourceAccountKey = "default"; e.BillSourceFlow = "" }, code: TikTokCancellationOwnershipMismatch},
		{name: "missing attempt", edit: func(e *TikTokCancellationEvidence) { e.SMLAttemptID = "" }, code: TikTokCancellationEvidenceInconsistent},
		{name: "attempt not sent", edit: func(e *TikTokCancellationEvidence) { e.SMLAttemptState = "unknown" }, code: TikTokCancellationEvidenceInconsistent},
		{name: "doc mismatch", edit: func(e *TikTokCancellationEvidence) { e.SMLAttemptDocNo = "BF-INV26090002" }, code: TikTokCancellationEvidenceInconsistent},
		{name: "wrong sale route", edit: func(e *TikTokCancellationEvidence) {
			e.BillDocumentRoute = "saleorder"
			e.SMLAttemptRoute = "saleorder"
		}, code: TikTokCancellationUnsupportedSaleRoute},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			evidence := base
			tc.edit(&evidence)
			got := EvaluateTikTokCancellationEvidence(evidence)
			if got.Eligible || got.Code != tc.code {
				t.Fatalf("result = %+v, want ineligible %s", got, tc.code)
			}
		})
	}
}

func TestTikTokCancellationReviewDigestBindsSourceSaleAndRouteEvidence(t *testing.T) {
	base := TikTokCancellationReviewEvidence{
		ShopID: "7494619203789490654", OrderID: "586030483469993439", SourceHash: "source-hash",
		BillID: "03ee1216-acb4-4a88-842c-7edc6eb44292", SMLAttemptID: "11111111-1111-1111-1111-111111111111",
		SaleSMLDocNo: "BF-INV26090001", RouteConfigVersion: 3, RouteSignature: "route-signature",
	}
	want := TikTokCancellationReviewDigest(base)
	if len(want) != 64 {
		t.Fatalf("digest length = %d", len(want))
	}

	changes := []func(*TikTokCancellationReviewEvidence){
		func(e *TikTokCancellationReviewEvidence) { e.SourceHash = "changed" },
		func(e *TikTokCancellationReviewEvidence) { e.SMLAttemptID = "22222222-2222-2222-2222-222222222222" },
		func(e *TikTokCancellationReviewEvidence) { e.SaleSMLDocNo = "BF-INV26090002" },
		func(e *TikTokCancellationReviewEvidence) { e.RouteConfigVersion++ },
		func(e *TikTokCancellationReviewEvidence) { e.RouteSignature = "changed-route" },
	}
	for index, change := range changes {
		changed := base
		change(&changed)
		if got := TikTokCancellationReviewDigest(changed); got == want {
			t.Fatalf("change %d did not change digest", index)
		}
	}
}
