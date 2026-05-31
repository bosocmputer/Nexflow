package handlers

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"math"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"

	"nexflow/internal/repository"
)

func TestNormalizeSettlementSendRequestTrimsFields(t *testing.T) {
	got := normalizeSettlementSendRequest(shopeeSettlementSendRequest{
		DocFormatCode: " RC ",
		PassbookCode:  " 68667871 ",
		ExpenseCode:   " 5015 ",
		Remark:        "  ทดสอบ  ",
		DocDate:       " 2026-05-26 ",
		DocTime:       " 15:10 ",
	})
	if got.DocFormatCode != "RC" || got.PassbookCode != "68667871" || got.ExpenseCode != "5015" {
		t.Fatalf("not trimmed: %+v", got)
	}
	if got.Remark != "ทดสอบ" {
		t.Fatalf("Remark = %q", got.Remark)
	}
}

func TestHumanizeSettlementErrorHidesRawConnectionNoise(t *testing.T) {
	got := humanizeSettlementError(fmt.Errorf("context deadline exceeded"))
	if !strings.Contains(got, "ใช้เวลานาน") {
		t.Fatalf("message = %q", got)
	}
}

func TestSettlementRunStatusFromCounts(t *testing.T) {
	tests := []struct {
		name     string
		ready    int
		blocked  int
		sent     int
		current  string
		expected string
	}{
		{name: "ready wins when any item can send", ready: 1, blocked: 3, expected: "ready"},
		{name: "all blocked becomes partial", ready: 0, blocked: 4, expected: "partial"},
		{name: "empty preview is not ready", ready: 0, blocked: 0, expected: "partial"},
		{name: "sent is preserved", ready: 0, blocked: 2, sent: 2, current: "sent", expected: "sent"},
		{name: "failed is preserved", ready: 2, blocked: 0, current: "failed", expected: "failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := settlementRunStatusFromCounts(tt.ready, tt.blocked, tt.sent, tt.current)
			if got != tt.expected {
				t.Fatalf("status = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestParseShopeeSettlementReleaseRangeUsesReleaseCopy(t *testing.T) {
	_, _, err := parseShopeeSettlementReleaseRange("2026-05-01", "2026-05-16")
	if err == nil {
		t.Fatal("expected max range error")
	}
	if !strings.Contains(err.Error(), "Shopee release เงิน") || !strings.Contains(err.Error(), "15 วัน") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestSettlementRunSummarySerializesEmptyItemsArray(t *testing.T) {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows([]string{
		"id", "connection_id", "shop_id", "shop_label",
		"release_time_from", "release_time_to", "status", "total_count", "ready_count", "blocked_count", "sent_count",
		"invoice_amount_total", "payout_amount_total", "difference_amount_total",
		"ready_invoice_amount", "ready_payout_amount", "ready_difference_amount",
		"blocked_invoice_amount", "blocked_payout_amount", "blocked_difference_amount",
		"rc_doc_no", "error_msg", "selected_doc_format_code", "selected_passbook_code", "selected_passbook_name",
		"selected_bank_code", "selected_bank_branch", "selected_expense_code", "selected_expense_name",
		"started_at", "finished_at", "created_at", "updated_at", "hidden_at", "hidden_by", "hidden_reason",
	}).AddRow(
		"00000000-0000-0000-0000-000000000001", "", int64(264993963), "Henna.milkford",
		now, now, "running", 0, 0, 0, 0,
		0.0, 0.0, 0.0,
		0.0, 0.0, 0.0,
		0.0, 0.0, 0.0,
		"", "", "", "", "",
		"", "", "", "",
		nil, nil, now, now, nil, "", "",
	)

	mock.ExpectQuery("SELECT settlement summary").WillReturnRows(rows)
	sqlRows, err := db.Query("SELECT settlement summary")
	if err != nil {
		t.Fatalf("query summary: %v", err)
	}
	defer sqlRows.Close()

	if !sqlRows.Next() {
		t.Fatal("expected one row")
	}
	run, err := scanSettlementRunSummary(sqlRows)
	if err != nil {
		t.Fatalf("scanSettlementRunSummary: %v", err)
	}
	b, err := json.Marshal(run)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	if !strings.Contains(string(b), `"items":[]`) {
		t.Fatalf("items must serialize as [], got %s", string(b))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestParseSettlementListFiltersUsesPagedDefaultsAndCapsPerPage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/shopee-settlements?page=3&per_page=250&status=ready&shop_id=264993963&search=RC26050010", nil)

	got := parseSettlementListFilters(c)
	if got.Page != 3 {
		t.Fatalf("Page = %d, want 3", got.Page)
	}
	if got.PerPage != settlementRunMaxPerPage {
		t.Fatalf("PerPage = %d, want cap %d", got.PerPage, settlementRunMaxPerPage)
	}
	if got.offset() != 200 {
		t.Fatalf("offset = %d, want 200", got.offset())
	}
	if got.Status != "ready" || got.ShopID != "264993963" || got.Search != "RC26050010" {
		t.Fatalf("filters not parsed: %+v", got)
	}
}

func TestParseSettlementListFiltersSupportsLegacyLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/shopee-settlements?limit=35", nil)

	got := parseSettlementListFilters(c)
	if got.Page != settlementRunDefaultPage {
		t.Fatalf("Page = %d, want default %d", got.Page, settlementRunDefaultPage)
	}
	if got.PerPage != 35 {
		t.Fatalf("PerPage = %d, want legacy limit 35", got.PerPage)
	}
}

func TestAppendSettlementRunFiltersHiddenModes(t *testing.T) {
	tests := []struct {
		name       string
		hidden     string
		want       string
		shouldMiss string
	}{
		{name: "default hides hidden runs", want: "r.hidden_at IS NULL", shouldMiss: "r.hidden_at IS NOT NULL"},
		{name: "only hidden", hidden: "only", want: "r.hidden_at IS NOT NULL", shouldMiss: "r.hidden_at IS NULL"},
		{name: "all hidden mode has no hidden predicate", hidden: "all", shouldMiss: "r.hidden_at IS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sb strings.Builder
			args := []any{}
			appendSettlementRunFilters(&sb, &args, settlementListFilters{Hidden: tt.hidden}, true)
			got := sb.String()
			if tt.want != "" && !strings.Contains(got, tt.want) {
				t.Fatalf("filter = %q, want %q", got, tt.want)
			}
			if tt.shouldMiss != "" && strings.Contains(got, tt.shouldMiss) {
				t.Fatalf("filter = %q, should not contain %q", got, tt.shouldMiss)
			}
		})
	}
}

func TestCanHideSettlementRunGuardsActiveSentAndReadyRuns(t *testing.T) {
	tests := []struct {
		name    string
		run     shopeeSettlementRunView
		wantErr bool
	}{
		{name: "partial no ready allowed", run: shopeeSettlementRunView{Status: "partial", ReadyCount: 0, BlockedCount: 2}},
		{name: "failed no ready allowed", run: shopeeSettlementRunView{Status: "failed", ReadyCount: 0}},
		{name: "active running blocked", run: shopeeSettlementRunView{Status: "running", ReadyCount: 0}, wantErr: true},
		{name: "sent blocked", run: shopeeSettlementRunView{Status: "sent", ReadyCount: 0}, wantErr: true},
		{name: "ready items blocked", run: shopeeSettlementRunView{Status: "partial", ReadyCount: 1}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := canHideSettlementRun(&tt.run)
			if tt.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestPrepareSettlementSendRequiresAtomicStatusLock(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	runID := "00000000-0000-0000-0000-000000000001"
	mock.ExpectQuery("FROM shopee_settlement_items").
		WithArgs(runID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "shop_id", "order_sn", "payout_amount", "invoice_doc_no", "cust_code",
			"invoice_amount", "difference_amount", "status",
		}).AddRow("11111111-1111-1111-1111-111111111111", int64(264993963), "260515FQ1FT0SJ", 230, "NX-INV26050043", "AB-2604-0013", 300, 70, "ready"))
	mock.ExpectExec("UPDATE shopee_settlement_runs").
		WithArgs(runID, "RC", "68667871", "", "", "", "5015", "").
		WillReturnResult(sqlmock.NewResult(0, 0))

	h := &ShopeeImportHandler{db: db}
	err = h.prepareSettlementSend(context.Background(), runID, shopeeSettlementSendRequest{
		DocFormatCode: "RC",
		PassbookCode:  "68667871",
		ExpenseCode:   "5015",
	})
	if err == nil || !strings.Contains(err.Error(), "กำลังส่งหรือส่งไปแล้ว") {
		t.Fatalf("error = %v, want lock failure", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestReconcileSettlementItemStatusBlocksAlreadyReceived(t *testing.T) {
	h := &ShopeeImportHandler{}
	status, reason, docNo, err := h.reconcileSettlementItemStatus(context.Background(), shopeeSettlementItemView{
		OrderSN:       "260514CWEJ7BN9",
		PayoutAmount:  218,
		InvoiceAmount: 300,
	}, settlementCandidate{
		OrderSN:              "260514CWEJ7BN9",
		InvoiceDocNo:         "NX-INV26050009",
		InvoiceAmount:        300,
		AlreadyReceived:      true,
		ExistingReceiptDocNo: "RC26050008",
		Status:               "found",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "blocked" || docNo != "RC26050008" || !strings.Contains(reason, "RC26050008") {
		t.Fatalf("status=%q reason=%q docNo=%q", status, reason, docNo)
	}
}

func TestAuditSettlementRunWritesSummaryWithoutSensitiveFields(t *testing.T) {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	runID := "00000000-0000-0000-0000-000000000001"
	userID := "11111111-1111-1111-1111-111111111111"
	mock.ExpectQuery("FROM shopee_settlement_runs").
		WithArgs(runID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "connection_id", "shop_id", "shop_label",
			"release_time_from", "release_time_to", "status", "total_count", "ready_count", "blocked_count", "sent_count",
			"rc_doc_no", "error_msg", "selected_doc_format_code", "selected_passbook_code", "selected_passbook_name",
			"selected_bank_code", "selected_bank_branch", "selected_expense_code", "selected_expense_name",
			"started_at", "finished_at", "created_at", "updated_at", "hidden_at", "hidden_by", "hidden_reason",
		}).AddRow(
			runID, "", int64(264993963), "Henna.milkford",
			now, now.Add(24*time.Hour), "ready", 3, 2, 1, 0,
			"", "", "RC", "68667871", "ธนาคารทดสอบ",
			"", "", "5015", "ค่าธรรมเนียม Shopee",
			now, nil, now, now, nil, "", "",
		))
	mock.ExpectQuery("FROM shopee_settlement_items").
		WithArgs(runID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "order_sn", "escrow_release_time", "payout_amount", "escrow_amount", "buyer_total_amount",
			"invoice_doc_no", "invoice_doc_date", "cust_code", "invoice_amount", "difference_amount",
			"status", "block_reason", "receipt_doc_no", "existing_receipt_doc_no",
		}))
	mock.ExpectExec("INSERT INTO audit_logs").
		WithArgs(
			"shopee_settlement_preview_completed",
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			"shopee_settlement",
			"info",
			nil,
			nil,
			auditDetailMatcher{
				t: t,
				required: map[string]interface{}{
					"run_id":            runID,
					"shop_label":        "Henna.milkford",
					"release_date_from": "2026-05-27",
					"release_date_to":   "2026-05-28",
					"ready_count":       2,
					"blocked_count":     1,
					"doc_format_code":   "RC",
					"passbook_code":     "68667871",
					"expense_code":      "5015",
					"message":           "ดึงเสร็จ",
				},
				forbidden: []string{"access_token", "refresh_token", "connection string", "password"},
			},
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	h := &ShopeeImportHandler{db: db, auditRepo: repository.NewAuditLogRepo(db)}
	h.auditSettlementRun(context.Background(), "shopee_settlement_preview_completed", runID, &userID, "info", map[string]interface{}{"message": "ดึงเสร็จ"})

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

type auditDetailMatcher struct {
	t         *testing.T
	required  map[string]interface{}
	forbidden []string
}

func (m auditDetailMatcher) Match(v driver.Value) bool {
	var raw []byte
	switch x := v.(type) {
	case []byte:
		raw = x
	case string:
		raw = []byte(x)
	default:
		m.t.Errorf("audit detail type = %T", v)
		return false
	}
	lower := strings.ToLower(string(raw))
	for _, forbidden := range m.forbidden {
		if strings.Contains(lower, forbidden) {
			m.t.Errorf("audit detail contains forbidden token %q: %s", forbidden, string(raw))
			return false
		}
	}
	var detail map[string]interface{}
	if err := json.Unmarshal(raw, &detail); err != nil {
		m.t.Errorf("audit detail json: %v", err)
		return false
	}
	for key, want := range m.required {
		got, ok := detail[key]
		if !ok {
			m.t.Errorf("audit detail missing %s in %v", key, detail)
			return false
		}
		if !auditMatcherEqual(got, want) {
			m.t.Errorf("audit detail[%s] = %v (%T), want %v (%T)", key, got, got, want, want)
			return false
		}
	}
	return true
}

func auditMatcherEqual(got interface{}, want interface{}) bool {
	switch w := want.(type) {
	case int:
		g, ok := got.(float64)
		return ok && math.Abs(g-float64(w)) < 0.0001
	case float64:
		g, ok := got.(float64)
		return ok && math.Abs(g-w) < 0.0001
	default:
		return fmt.Sprint(got) == fmt.Sprint(want)
	}
}
