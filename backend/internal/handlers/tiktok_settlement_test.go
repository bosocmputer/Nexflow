package handlers

import (
	"encoding/json"
	"errors"
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

func TestTikTokSettlementPreflightUsesAllStatementStatuses(t *testing.T) {
	from := time.Date(2026, 9, 22, 0, 0, 0, 0, tikTokSettlementBangkok)
	to := from.Add(24 * time.Hour)
	request := tikTokSettlementPreflightSearch(from, to)

	if request.StatementStatus != "" || request.PageSize != 1 {
		t.Fatalf("preflight request = %#v", request)
	}
	if request.StatementTimeGE != from.Unix() || request.StatementTimeLT != to.Unix() {
		t.Fatalf("preflight range = %#v", request)
	}
}

func TestTikTokStatementImportSearchUsesAllStatuses(t *testing.T) {
	from := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 24, 0, 0, 1, 0, time.UTC)
	request := tikTokStatementImportSearch(from, to, "next-page")

	if request.StatementStatus != "" || request.PageSize != 100 || request.PageToken != "next-page" {
		t.Fatalf("import request = %#v", request)
	}
	if request.StatementTimeGE != from.Unix() || request.StatementTimeLT != to.Unix() {
		t.Fatalf("import range = %#v", request)
	}
}

func TestParseTikTokStatementImportRangeUsesNextUTCDay(t *testing.T) {
	from, to, err := parseTikTokStatementImportRange("2026-09-01", "2026-09-23")
	if err != nil {
		t.Fatal(err)
	}
	wantFrom := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	wantTo := time.Date(2026, 9, 24, 0, 0, 1, 0, time.UTC)
	if !from.Equal(wantFrom) || !to.Equal(wantTo) {
		t.Fatalf("range = %v to %v, want %v to %v", from, to, wantFrom, wantTo)
	}
}

func TestTikTokSettlementImportFailureStage(t *testing.T) {
	for _, test := range []struct {
		err  error
		want string
	}{
		{errors.New("read_statement_page: unavailable"), "read_statement_page"},
		{errors.New("store_statement: database unavailable"), "store_statement"},
		{errors.New("other"), "unknown"},
	} {
		if got := tikTokSettlementImportFailureStage(test.err); got != test.want {
			t.Fatalf("stage=%q want=%q", got, test.want)
		}
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
