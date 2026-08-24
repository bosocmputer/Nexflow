package stockrecalc

import (
	"testing"

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
