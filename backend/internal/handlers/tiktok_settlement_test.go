package handlers

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"nexflow/internal/services/tiktokshop"
)

func TestTikTokSettlementImportRequestBindsSnakeCaseJSON(t *testing.T) {
	var request tikTokSettlementImportRequest
	if err := json.Unmarshal([]byte(`{"shop_id":"7494619203789490654","date_from":"2026-09-09","date_to":"2026-09-23"}`), &request); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if request.ShopID != "7494619203789490654" || request.DateFrom != "2026-09-09" || request.DateTo != "2026-09-23" {
		t.Fatalf("request=%+v", request)
	}
}

func TestParseTikTokSettlementRangeDefaultsAndCapsWindow(t *testing.T) {
	from, to, err := parseTikTokSettlementRange("2026-09-01", "2026-09-15")
	if err != nil || from.Location() != tikTokSettlementBangkok || to.Sub(from) != 15*24*time.Hour {
		t.Fatalf("from=%v to=%v err=%v", from, to, err)
	}
	_, _, err = parseTikTokSettlementRange("2026-09-01", "2026-10-03")
	if err == nil || !strings.Contains(err.Error(), "31") {
		t.Fatalf("err=%v", err)
	}
}

func TestFinanceThaiErrorDoesNotExposeUpstreamDetail(t *testing.T) {
	message := financeThaiError(&tiktokshop.GatewayError{Code: "permission_denied", Message: "secret upstream failure"})
	if !strings.Contains(message, "Finance Information") || strings.Contains(message, "secret") {
		t.Fatalf("message=%q", message)
	}
}
