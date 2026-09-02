package repository

import (
	"errors"
	"testing"

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
