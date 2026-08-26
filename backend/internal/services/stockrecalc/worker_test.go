package stockrecalc

import (
	"math/big"
	"testing"
	"time"

	"nexflow/internal/repository"
	"nexflow/internal/services/sml"
)

func TestVerifyBalanceChunkRequiresEveryRequestedItem(t *testing.T) {
	response := &sml.StockBalanceBatchResponse{Scopes: []sml.StockBalanceScopeResult{{
		ScopeID: "recalc:job-1", Items: []sml.StockBalanceItem{{ItemCode: "A"}},
	}}}
	if err := verifyBalanceChunk(response, "recalc:job-1", []string{"A", "B"}); err == nil {
		t.Fatal("missing balance item was accepted")
	}
	response.Scopes[0].Items = append(response.Scopes[0].Items, sml.StockBalanceItem{ItemCode: "B"})
	if err := verifyBalanceChunk(response, "recalc:job-1", []string{"A", "B"}); err != nil {
		t.Fatalf("complete balance response rejected: %v", err)
	}
}

func TestVerifyDemandEvidenceRequiresEveryExactLineAndApprovedFingerprint(t *testing.T) {
	demand := repository.StockRecalcDemand{Lines: []repository.StockRecalcDemandLine{{
		EvidenceID: "r1:A", ReservationID: "r1", SMLAttemptID: "attempt-1", DocNo: "SO1", Route: "saleorder",
		ItemCode: "A", Warehouse: "W1", Location: "S1", ExpectedBaseQtyExact: "48", EvidenceKind: "sale_order_demand",
	}}}
	groups, err := groupDemandEvidence(demand.Lines)
	if err != nil {
		t.Fatal(err)
	}
	response := &sml.StockDemandEvidenceBatchResponse{
		SchemaVersion: "stock-demand-evidence-v1", SourceSemanticsFingerprint: "sha256:approved",
		SourceSnapshotAt: "2026-08-26T03:00:00Z",
		Lines: []sml.StockDemandEvidenceResultLine{{EvidenceID: groups[0].Request.EvidenceID, DocNo: "SO1", Route: "saleorder", ItemCode: "A",
			WarehouseCode: "W1", LocationCode: "S1", ExpectedBaseQtyExact: "48", ActualBaseQtyExact: "48", Status: "verified", EvidenceHash: "sha256:evidence"}},
	}
	verified, err := verifyDemandEvidence(groups, response, "sha256:approved", time.Date(2026, 8, 26, 3, 1, 0, 0, time.UTC))
	if err != nil || len(verified) != 1 {
		t.Fatalf("verified=%+v err=%v", verified, err)
	}
	response.SourceSemanticsFingerprint = "sha256:changed"
	if _, err := verifyDemandEvidence(groups, response, "sha256:approved", time.Now()); err == nil {
		t.Fatal("fingerprint drift must fail closed")
	}
}

func TestGroupDemandEvidenceSumsSameDocumentResourceWithoutLosingMembers(t *testing.T) {
	lines := []repository.StockRecalcDemandLine{
		{EvidenceID: "r1:A", ReservationID: "r1", SMLAttemptID: "attempt-1", DocNo: "SO1", Route: "saleorder", ItemCode: "A", Warehouse: "W1", Location: "S1", ExpectedBaseQtyExact: "2.50"},
		{EvidenceID: "r2:A", ReservationID: "r2", SMLAttemptID: "attempt-1", DocNo: "SO1", Route: "saleorder", ItemCode: "A", Warehouse: "W1", Location: "S1", ExpectedBaseQtyExact: "3.5"},
	}
	groups, err := groupDemandEvidence(lines)
	if err != nil || len(groups) != 1 || len(groups[0].Members) != 2 || groups[0].Request.ExpectedBaseQtyExact != "6" {
		t.Fatalf("groups=%+v err=%v", groups, err)
	}
}

func TestExactDecimalStringDoesNotTrimIntegerZeros(t *testing.T) {
	value, _ := new(big.Rat).SetString("10")
	if got := exactDecimalString(value); got != "10" {
		t.Fatalf("exactDecimalString(10)=%q", got)
	}
}
