package handlers

import "testing"

func TestParseMarketplaceOperationsQueryDefaultsToActionableWork(t *testing.T) {
	query, err := parseMarketplaceOperationsQuery(mapQuery{
		"channel": "all",
	})
	if err != nil {
		t.Fatalf("parse query: %v", err)
	}
	if query.View != marketplaceOperationsViewWork {
		t.Fatalf("view = %q, want actionable work", query.View)
	}
	if query.Limit != marketplaceOperationsDefaultLimit {
		t.Fatalf("limit = %d, want %d", query.Limit, marketplaceOperationsDefaultLimit)
	}
}

func TestParseMarketplaceOperationsQueryRejectsUnsafeFilters(t *testing.T) {
	for name, values := range map[string]mapQuery{
		"unknown channel":  {"channel": "lazada"},
		"oversized search": {"q": string(make([]byte, marketplaceOperationsMaxSearchLength+1))},
		"invalid cursor":   {"cursor": "not-a-cursor"},
		"invalid date":     {"from": "2026-99-99"},
		"range too large":  {"from": "2026-01-01", "to": "2026-06-01"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseMarketplaceOperationsQuery(values); err == nil {
				t.Fatal("expected invalid query to be rejected")
			}
		})
	}
}

func TestParseMarketplaceOperationsQueryUsesBangkokCalendarDays(t *testing.T) {
	query, err := parseMarketplaceOperationsQuery(mapQuery{"from": "2026-09-22", "to": "2026-09-22"})
	if err != nil {
		t.Fatalf("parse query: %v", err)
	}
	if query.From == nil || query.To == nil {
		t.Fatal("expected date range")
	}
	if got, want := query.From.Format("2006-01-02 15:04 MST"), "2026-09-21 17:00 UTC"; got != want {
		t.Fatalf("from = %s, want %s", got, want)
	}
	if got, want := query.To.Format("2006-01-02 15:04 MST"), "2026-09-22 17:00 UTC"; got != want {
		t.Fatalf("to = %s, want %s", got, want)
	}
}

func TestMarketplaceWorkStateFailsClosedForCancelledAndStaleDocuments(t *testing.T) {
	tests := []struct {
		name string
		row  marketplaceOperationsRow
		want marketplaceWorkState
	}{
		{
			name: "cancelled sale without cancellation document requires review",
			row:  marketplaceOperationsRow{Source: marketplaceSourceShopee, MarketplaceStatus: "CANCELLED", SMLDocNo: "BF-INV1"},
			want: marketplaceWorkStateCancelDocumentNeeded,
		},
		{
			name: "bill without SML document is ready to send",
			row:  marketplaceOperationsRow{Source: marketplaceSourceTikTok, MarketplaceStatus: "DELIVERED", BillID: "bill-1", BillStatus: "pending"},
			want: marketplaceWorkStateReadyToSend,
		},
		{
			name: "successful SML document is complete",
			row:  marketplaceOperationsRow{Source: marketplaceSourceShopee, ERPStatus: "sent", SMLDocNo: "BF-INV1"},
			want: marketplaceWorkStateComplete,
		},
		{
			name: "unpaid order is visible in all orders but not actionable",
			row:  marketplaceOperationsRow{Source: marketplaceSourceShopee, MarketplaceStatus: "UNPAID"},
			want: marketplaceWorkStateComplete,
		},
		{
			name: "tiktok awaiting shipment is visible but not ready for a reviewed bill",
			row:  marketplaceOperationsRow{Source: marketplaceSourceTikTok, MarketplaceStatus: "AWAITING_SHIPMENT"},
			want: marketplaceWorkStateComplete,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deriveMarketplaceWorkState(tt.row); got != tt.want {
				t.Fatalf("work state = %q, want %q", got, tt.want)
			}
		})
	}
}

type mapQuery map[string]string

func (q mapQuery) Get(key string) string { return q[key] }
