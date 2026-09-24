package handlers

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

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
		{errors.New("store_statement: save_snapshot: database unavailable"), "save_snapshot"},
		{errors.New("store_statement: reconcile_statement: unavailable"), "reconcile_statement"},
		{errors.New("other"), "unknown"},
	} {
		if got := tikTokSettlementImportFailureStage(test.err); got != test.want {
			t.Fatalf("stage=%q want=%q", got, test.want)
		}
	}
}

func TestTikTokSettlementAmountOrZero(t *testing.T) {
	if got := tikTokSettlementAmountOrZero(" "); got != "0" {
		t.Fatalf("empty amount=%q", got)
	}
	if got := tikTokSettlementAmountOrZero(" 12.50 "); got != "12.50" {
		t.Fatalf("amount=%q", got)
	}
}

func TestTikTokStatementTransactionWithZeroOptionalAmounts(t *testing.T) {
	transaction := tikTokStatementTransactionWithZeroOptionalAmounts(tiktokshop.StatementTransaction{
		SettlementAmount: "222.30",
		FeeAmount:        " ",
		ShippingAmount:   "",
		AdjustmentAmount: " 0.50 ",
		ReserveAmount:    "",
	})
	if transaction.SettlementAmount != "222.30" {
		t.Fatalf("settlement amount must remain required evidence: %q", transaction.SettlementAmount)
	}
	if transaction.FeeAmount != "0" || transaction.ShippingAmount != "0" || transaction.AdjustmentAmount != "0.50" || transaction.ReserveAmount != "0" {
		t.Fatalf("transaction=%+v", transaction)
	}
}

func TestTikTokStatementSettlementReadyAcceptsDocumentedAndLiveStatuses(t *testing.T) {
	for _, status := range []tiktokshop.StatementStatus{
		tiktokshop.StatementStatusPaid,
		tiktokshop.StatementStatusSettled,
	} {
		if !tikTokStatementIsSettlementReady(status) {
			t.Fatalf("status %q must be reconciliation-ready", status)
		}
	}
	for _, status := range []tiktokshop.StatementStatus{
		tiktokshop.StatementStatusProcessing,
		tiktokshop.StatementStatusFailed,
		"",
	} {
		if tikTokStatementIsSettlementReady(status) {
			t.Fatalf("status %q must remain blocked", status)
		}
	}
}

func TestSafeTikTokSettlementErrorReasonIsBounded(t *testing.T) {
	if got := safeTikTokSettlementErrorReason(errors.New(strings.Repeat("x", 181))); len(got) != 180 {
		t.Fatalf("length=%d", len(got))
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

func TestTikTokSettlementListWhereAppliesLocalSnapshotFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest("GET", "/?shop_id=shop-a&date_from=2026-09-01&date_to=2026-09-23&status=ready", nil)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request

	where, args, err := tikTokSettlementListWhere(context, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(where, "shop_id=$1") || !strings.Contains(where, "status=$2") || !strings.Contains(where, "COALESCE(statement_time,payment_time) >= $3") {
		t.Fatalf("where=%q", where)
	}
	if len(args) != 4 || args[0] != "shop-a" || args[1] != "ready" {
		t.Fatalf("args=%#v", args)
	}
}

func TestTikTokSettlementSummaryKeepsWorkStatusOutOfItsAggregate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest("GET", "/?shop_id=shop-a&status=ready", nil)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request

	where, args, err := tikTokSettlementListWhere(context, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(where, "status=$") || len(args) != 1 || args[0] != "shop-a" {
		t.Fatalf("summary must aggregate all statuses: where=%q args=%#v", where, args)
	}
}

func TestTikTokSettlementBatchDigestBindsSelectionAndAmounts(t *testing.T) {
	batch := &tikTokSettlementBatchView{
		ShopID: "shop-a", Currency: "THB", ConfigVersion: 4, RouteVersion: 8,
		SettlementAmount: 120.50, InvoiceAmount: 150, RunIDs: []string{"run-a", "run-b"},
	}
	first := tikTokSettlementBatchDigest(batch)
	if first == "" {
		t.Fatal("digest must not be empty")
	}
	batch.SettlementAmount = 120.51
	if second := tikTokSettlementBatchDigest(batch); second == first {
		t.Fatal("amount change must invalidate digest")
	}
	batch.SettlementAmount = 120.50
	batch.RunIDs = []string{"run-b", "run-a"}
	if second := tikTokSettlementBatchDigest(batch); second == first {
		t.Fatal("selection change must invalidate digest")
	}
}

func TestTikTokSettlementMoneyRequiresNonNegativeTwoDecimalAmount(t *testing.T) {
	for _, raw := range []string{"0", "2922.57", "10.5"} {
		if _, err := tikTokSettlementMoney(raw); err != nil {
			t.Fatalf("%q must be accepted: %v", raw, err)
		}
	}
	for _, raw := range []string{"", "-1", "1.234", "not-a-number"} {
		if _, err := tikTokSettlementMoney(raw); err == nil {
			t.Fatalf("%q must be rejected", raw)
		}
	}
}

func TestTikTokSettlementUniqueIDsDropsBlankAndDuplicateValues(t *testing.T) {
	got := tikTokSettlementUniqueIDs([]string{" run-a ", "", "run-a", "run-b"})
	if strings.Join(got, ",") != "run-a,run-b" {
		t.Fatalf("got %#v", got)
	}
}
