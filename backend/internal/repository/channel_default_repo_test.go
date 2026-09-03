package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"nexflow/internal/models"
)

func TestUpdateExpectedReturnsVersionConflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(`(?s)WITH updated AS.*UPDATE channel_defaults.*paused_reason='route_changed'.*config_version=config_version\+1`).
		WithArgs(
			"AR-1", "ลูกค้า", "", "", "", "SI", "/api/v1/ic/sale-invoices",
			"BF-INV", "@YYMM####", "", "", "", "", false, "", "", "", "", "", "", "", "",
			"AB-1", "001", 1, float64(7), -1, "{{order_ref}}", "ข้อความ", "user-1",
			"shopee_realtime", "sale", int64(2), true,
		).
		WillReturnRows(sqlmock.NewRows(channelDefaultColumnNames()))

	_, err = NewChannelDefaultRepo(db).UpdateExpected(&models.ChannelDefault{
		Channel: "shopee_realtime", BillType: "sale", PartyCode: "AR-1", PartyName: "ลูกค้า",
		DocFormatCode: "SI", Endpoint: "/api/v1/ic/sale-invoices", DocPrefix: "BF-INV",
		DocRunningFormat: "@YYMM####", WHCode: "AB-1", ShelfCode: "001", VATType: 1,
		VATRate: 7, InquiryType: -1, Remark: "{{order_ref}}", Remark2: "ข้อความ",
	}, "user-1", 2, true)
	if !errors.Is(err, ErrConfigVersionConflict) {
		t.Fatalf("error = %v, want ErrConfigVersionConflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateShopeeSMLRouteBundleIsAtomicAndPausesAutomation(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now()
	main := &models.ChannelDefault{
		Channel: "shopee_realtime", BillType: "sale", PartyCode: "AR-1", DocFormatCode: "SI",
		Endpoint: "/api/v1/ic/sale-invoices", DocPrefix: "BF-INV", DocRunningFormat: "YYMM####",
		WHCode: "AB-1", ShelfCode: "001", VATType: 1, VATRate: 7, InquiryType: 0,
	}
	cancelRoute := &models.ChannelDefault{
		Channel: "shopee_realtime_cancel", BillType: "sale", DocFormatCode: "CN",
		Endpoint: "/api/v1/ic/sale-invoices/:doc_no/cancel", DocPrefix: "CN", DocRunningFormat: "YYMM####",
		VATType: -1, VATRate: -1, InquiryType: -1,
	}
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)INSERT INTO channel_defaults.*WHERE \$33::bigint=0.*ON CONFLICT.*RETURNING`).
		WillReturnRows(sqlmock.NewRows(channelDefaultColumnNames()).AddRow(
			"shopee_realtime", "sale", "AR-1", "", "", "", "", "SI", "/api/v1/ic/sale-invoices",
			"BF-INV", "YYMM####", "", "", "", "", false, "", "", "", "", "", "", "", "",
			"AB-1", "001", 1, float64(7), 0, "", "", int64(3), nil, now,
		))
	mock.ExpectQuery(`(?s)INSERT INTO channel_defaults.*WHERE \$33::bigint=0.*ON CONFLICT.*RETURNING`).
		WillReturnRows(sqlmock.NewRows(channelDefaultColumnNames()).AddRow(
			"shopee_realtime_cancel", "sale", "", "", "", "", "", "CN", "/api/v1/ic/sale-invoices/:doc_no/cancel",
			"CN", "YYMM####", "", "", "", "", false, "", "", "", "", "", "", "", "",
			"", "", -1, float64(-1), -1, "", "", int64(4), nil, now,
		))
	mock.ExpectExec(`UPDATE shopee_auto_sml_settings.*paused_reason='route_changed'`).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`INSERT INTO audit_logs.*shopee_sml_route_bundle_updated`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	result, err := NewChannelDefaultRepo(db).UpdateShopeeSMLRouteBundle(context.Background(), ShopeeSMLRouteBundleUpdate{
		Main: main, Cancellation: cancelRoute, ExpectedMainVersion: 2, ExpectedCancelVersion: 3,
		AuditDetail: map[string]interface{}{"safe": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Main.ConfigVersion != 3 || result.Cancellation.ConfigVersion != 4 || result.PausedShops != 2 {
		t.Fatalf("result=%+v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateShopeeSMLRouteBundleRollsBackBothRoutesOnVersionRace(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now()
	main := &models.ChannelDefault{Channel: "shopee_realtime", BillType: "sale", VATType: 1, VATRate: 7, InquiryType: 0}
	cancelRoute := &models.ChannelDefault{Channel: "shopee_realtime_cancel", BillType: "sale", VATType: -1, VATRate: -1, InquiryType: -1}
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)INSERT INTO channel_defaults.*RETURNING`).
		WillReturnRows(sqlmock.NewRows(channelDefaultColumnNames()).AddRow(
			"shopee_realtime", "sale", "", "", "", "", "", "", "", "", "", "", "", "", "",
			false, "", "", "", "", "", "", "", "", "", "", 1, float64(7), 0, "", "", int64(3), nil, now,
		))
	mock.ExpectQuery(`(?s)INSERT INTO channel_defaults.*RETURNING`).
		WillReturnRows(sqlmock.NewRows(channelDefaultColumnNames()))
	mock.ExpectRollback()

	_, err = NewChannelDefaultRepo(db).UpdateShopeeSMLRouteBundle(context.Background(), ShopeeSMLRouteBundleUpdate{
		Main: main, Cancellation: cancelRoute, ExpectedMainVersion: 2, ExpectedCancelVersion: 99,
	})
	if !errors.Is(err, ErrConfigVersionConflict) {
		t.Fatalf("error=%v, want version conflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func channelDefaultColumnNames() []string {
	return []string{
		"channel", "bill_type", "party_code", "party_name", "party_phone",
		"party_address", "party_tax_id", "doc_format_code", "endpoint",
		"doc_prefix", "doc_running_format", "branch_code", "sale_code", "unit_code", "doc_time",
		"shipping_item_enabled", "shipping_item_code", "shipping_item_unit_code",
		"passbook_code", "passbook_name", "bank_code", "bank_branch", "expense_code", "expense_name",
		"wh_code", "shelf_code", "vat_type", "vat_rate", "inquiry_type", "remark", "remark_2",
		"config_version", "updated_by", "updated_at",
	}
}
