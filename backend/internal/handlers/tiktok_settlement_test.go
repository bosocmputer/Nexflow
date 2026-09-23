package handlers

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"nexflow/internal/services/tiktokshop"
)

func TestParseTikTokIncomeExportKeepsWithdrawalAndOrdersSeparate(t *testing.T) {
	f := excelize.NewFile()
	orders := "รายละเอียดคำสั่งซื้อ"
	f.SetSheetName(f.GetSheetName(0), orders)
	writeTikTokIncomeRow(t, f, orders, 1, []string{
		"หมายเลขคำสั่งซื้อ/การปรับ", "ประเภทธุรกรรม", "เวลาที่สร้างคำสั่งซื้อ", "เวลาที่ชำระคำสั่งซื้อ", "สกุลเงิน", "ยอดการชำระเงินทั้งหมด", "รายได้ทั้งหมด", "ยอดรวมเงินคืนหลังหักส่วนลดจากผู้ขาย", "ค่าธรรมเนียมทั้งหมด", "ยอดรวมค่าจัดส่งที่ร้านค้าจ่ายจริง",
	})
	writeTikTokIncomeRow(t, f, orders, 2, []string{"ORDER-1", "คำสั่งซื้อ", "2026/09/20", "2026/09/21", "THB", "80.00", "100.00", "0", "-20.00", "0"})
	writeTikTokIncomeRow(t, f, orders, 3, []string{"ORDER-2", "คำสั่งซื้อ", "2026/09/20", "2026/09/21", "THB", "100.00", "120.00", "0", "-20.00", "0"})
	report, err := f.NewSheet("รายงาน")
	if err != nil || report == -1 {
		t.Fatalf("new report sheet: %v", err)
	}
	if err := f.SetCellValue("รายงาน", "B2", "ยอดการชำระเงินทั้งหมด"); err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellValue("รายงาน", "F2", "180.00"); err != nil {
		t.Fatal(err)
	}
	withdrawals, err := f.NewSheet("บันทึกการถอน")
	if err != nil || withdrawals == -1 {
		t.Fatalf("new withdrawals sheet: %v", err)
	}
	writeTikTokIncomeRow(t, f, "บันทึกการถอน", 1, []string{"ประเภทธุรกรรม", "ID อ้างอิง", "เวลาส่งคำขอ", "จำนวน", "สถานะ", "เวลาที่สำเร็จ", "บัญชีธนาคาร"})
	writeTikTokIncomeRow(t, f, "บันทึกการถอน", 2, []string{"Earnings", "WITHDRAW-1", "2026/09/22", "180.00", "Transferred", "2026/09/22", "/"})

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	export, err := parseTikTokIncomeExport(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("parse income export: %v", err)
	}
	if len(export.Orders) != 2 || export.OrderPaymentTotalCents != 18_000 || export.ReportPaymentTotalCents != 18_000 {
		t.Fatalf("export totals = %#v", export)
	}
	if len(export.Withdrawals) != 1 || export.Withdrawals[0].ID != "WITHDRAW-1" {
		t.Fatalf("withdrawals = %#v", export.Withdrawals)
	}
	selection, err := export.SelectWithdrawal("WITHDRAW-1")
	if err != nil {
		t.Fatalf("select withdrawal: %v", err)
	}
	if !selection.RequiresOperatorAttestation || selection.CanCreateReceipt {
		t.Fatalf("selection must require bank/operator evidence, got %#v", selection)
	}
}

func TestTikTokIncomeExportBlocksReceiptWhenTotalsDoNotProveOneWithdrawalScope(t *testing.T) {
	export := tikTokIncomeExport{
		Currency:                "THB",
		OrderPaymentTotalCents:  18_000,
		ReportPaymentTotalCents: 18_000,
		Orders:                  []tikTokIncomeOrder{{OrderID: "ORDER-1", Currency: "THB", PaymentCents: 18_000}},
		Withdrawals:             []tikTokIncomeWithdrawal{{ID: "WITHDRAW-1", AmountCents: 17_999, Status: "Transferred", Currency: "THB"}},
	}
	selection, err := export.SelectWithdrawal("WITHDRAW-1")
	if err != nil {
		t.Fatal(err)
	}
	if selection.CanCreateReceipt || !strings.Contains(selection.BlockReason, "ไม่ตรง") {
		t.Fatalf("mismatched withdrawal must block receipt, got %#v", selection)
	}
}

func TestParseTikTokIncomeExportRequiresAllFinanceSheets(t *testing.T) {
	f := excelize.NewFile()
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	if _, err := parseTikTokIncomeExport(bytes.NewReader(buf.Bytes())); err == nil || !strings.Contains(err.Error(), "รายละเอียดคำสั่งซื้อ") {
		t.Fatalf("missing finance sheets error = %v", err)
	}
}

func TestValidateTikTokIncomeReceiptAttestationRejectsUnconfirmedOrNonOrderRows(t *testing.T) {
	export := tikTokIncomeExport{
		Currency:                "THB",
		OrderPaymentTotalCents:  8_000,
		ReportPaymentTotalCents: 8_000,
		Orders:                  []tikTokIncomeOrder{{OrderID: "ORDER-1", TransactionType: "คำสั่งซื้อ", Currency: "THB", PaymentCents: 8_000}},
		Withdrawals:             []tikTokIncomeWithdrawal{{ID: "WITHDRAW-1", AmountCents: 8_000, Status: "Transferred", Currency: "THB"}},
	}
	if err := validateTikTokIncomeReceiptAttestation(export, "WITHDRAW-1", ""); err == nil || !strings.Contains(err.Error(), "ยืนยัน") {
		t.Fatalf("missing confirmation error = %v", err)
	}
	if err := validateTikTokIncomeReceiptAttestation(export, "WITHDRAW-1", tikTokIncomeBankReceiptConfirmation); err != nil {
		t.Fatalf("valid attestation rejected: %v", err)
	}
	export.Orders[0].TransactionType = "การปรับยอด"
	if err := validateTikTokIncomeReceiptAttestation(export, "WITHDRAW-1", tikTokIncomeBankReceiptConfirmation); err == nil || !strings.Contains(err.Error(), "ปรับยอด") {
		t.Fatalf("non-order row must block receipt: %v", err)
	}
}

func TestValidateTikTokIncomeReceiptAttestationRejectsDuplicateOrder(t *testing.T) {
	export := tikTokIncomeExport{
		Currency:                "THB",
		OrderPaymentTotalCents:  8_000,
		ReportPaymentTotalCents: 8_000,
		Orders: []tikTokIncomeOrder{
			{OrderID: "ORDER-1", TransactionType: "คำสั่งซื้อ", Currency: "THB", PaymentCents: 4_000},
			{OrderID: "ORDER-1", TransactionType: "คำสั่งซื้อ", Currency: "THB", PaymentCents: 4_000},
		},
		Withdrawals: []tikTokIncomeWithdrawal{{ID: "WITHDRAW-1", AmountCents: 8_000, Status: "Transferred", Currency: "THB"}},
	}
	if err := validateTikTokIncomeReceiptAttestation(export, "WITHDRAW-1", tikTokIncomeBankReceiptConfirmation); err == nil || !strings.Contains(err.Error(), "ซ้ำ") {
		t.Fatalf("duplicate order must block receipt: %v", err)
	}
}

func TestTikTokIncomeCentsDecimalPreservesExactCents(t *testing.T) {
	for _, test := range []struct {
		cents int64
		want  string
	}{{0, "0.00"}, {123, "1.23"}, {-123, "-1.23"}} {
		if got := tikTokIncomeCentsDecimal(test.cents); got != test.want {
			t.Fatalf("tikTokIncomeCentsDecimal(%d) = %q, want %q", test.cents, got, test.want)
		}
	}
}

func TestTikTokIncomePreviewIsUserScopedOneTimeAndExpires(t *testing.T) {
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	h := &TikTokSettlementHandler{}
	preview := &tikTokIncomePendingPreview{export: &tikTokIncomeExport{}, userID: "user-a", createdAt: now}
	if !h.storeIncomePreview("preview-1", preview, now) {
		t.Fatal("store preview")
	}
	if _, ok := h.consumeIncomePreview("preview-1", "user-b", now); ok {
		t.Fatal("different user must not consume preview")
	}
	if _, ok := h.consumeIncomePreview("preview-1", "user-a", now); ok {
		t.Fatal("preview must be one-time even after invalid user attempt")
	}
	if !h.storeIncomePreview("preview-2", preview, now) {
		t.Fatal("store second preview")
	}
	if _, ok := h.consumeIncomePreview("preview-2", "user-a", now.Add(tikTokIncomePreviewTTL+time.Second)); ok {
		t.Fatal("expired preview must not be consumed")
	}
}

func writeTikTokIncomeRow(t *testing.T, f *excelize.File, sheet string, row int, values []string) {
	t.Helper()
	for col, value := range values {
		cell, err := excelize.CoordinatesToCellName(col+1, row)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.SetCellValue(sheet, cell, value); err != nil {
			t.Fatal(err)
		}
	}
}

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

func TestTikTokSettlementAllStatusFallbackOnlyHandlesRetryableUpstreamFilterFailures(t *testing.T) {
	if !shouldUseAllTikTokStatementStatuses(&tiktokshop.GatewayError{Code: "internal_error", Retryable: true}) {
		t.Fatal("expected retryable internal error to use the documented all-status fallback")
	}
	if shouldUseAllTikTokStatementStatuses(&tiktokshop.GatewayError{Code: "permission_denied", Retryable: false}) {
		t.Fatal("permission errors must never bypass the scoped status request")
	}
	if shouldUseAllTikTokStatementStatuses(&tiktokshop.GatewayError{Code: "rate_limited", Retryable: true}) {
		t.Fatal("rate limits must wait for the next manual import, not amplify traffic")
	}
}

func TestTikTokSettlementAllowsPaidOnlyImportWhenNonPaidStatusesAreUnavailable(t *testing.T) {
	upstreamFailure := &tiktokshop.GatewayError{Code: "internal_error", Retryable: true}
	if !shouldCompleteTikTokSettlementPaidOnlyImport(tiktokshop.StatementStatusProcessing, true, upstreamFailure) {
		t.Fatal("expected a successful PAID import to remain usable when TikTok cannot return informational PROCESSING statements")
	}
	if shouldCompleteTikTokSettlementPaidOnlyImport(tiktokshop.StatementStatusPaid, true, upstreamFailure) {
		t.Fatal("PAID must never be treated as optional because it is settlement evidence")
	}
	if shouldCompleteTikTokSettlementPaidOnlyImport(tiktokshop.StatementStatusFailed, false, upstreamFailure) {
		t.Fatal("non-PAID statuses cannot be skipped before PAID was read successfully")
	}
	if shouldCompleteTikTokSettlementPaidOnlyImport(tiktokshop.StatementStatusProcessing, true, &tiktokshop.GatewayError{Code: "permission_denied", Retryable: false}) {
		t.Fatal("permission errors must remain visible instead of returning a partial import")
	}
}
