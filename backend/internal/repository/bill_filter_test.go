package repository

import (
	"encoding/json"
	"strings"
	"testing"

	"nexflow/internal/models"
)

func TestBillWhereDefaultsToActiveDocuments(t *testing.T) {
	where, _, _ := billWhere(models.BillListFilter{})
	if !strings.Contains(where, "b.archived_at IS NULL") {
		t.Fatalf("default where = %q, want active archived filter", where)
	}
}

func TestBillOrderByDocumentDateDesc(t *testing.T) {
	got := billOrderBy(models.BillListFilter{Sort: "document_date_desc"}, false)
	if !strings.Contains(got, "raw_data->>'doc_date'") || !strings.Contains(got, "b.created_at DESC") {
		t.Fatalf("document order = %q", got)
	}
	if got := billOrderBy(models.BillListFilter{Sort: "document_date_desc"}, true); got != "b.created_at DESC, b.id DESC" {
		t.Fatalf("cursor order = %q", got)
	}
}

func TestBillWhereArchivedModes(t *testing.T) {
	where, _, _ := billWhere(models.BillListFilter{Archived: "include"})
	if strings.Contains(where, "archived_at") {
		t.Fatalf("include where = %q, should not constrain archived_at", where)
	}

	where, _, _ = billWhere(models.BillListFilter{Archived: "only"})
	if !strings.Contains(where, "b.archived_at IS NOT NULL") {
		t.Fatalf("only where = %q, want archived_at IS NOT NULL", where)
	}
}

func TestBillWhereDateAndShopeeStatusFilters(t *testing.T) {
	where, args, _ := billWhere(models.BillListFilter{
		DateFrom:     "2026-05-01",
		DateTo:       "2026-05-18",
		ShopeeStatus: "shipped",
	})
	for _, want := range []string{"b.created_at >= $", "b.created_at < ($", "shopee_order_events", "soe.bill_id = b.id"} {
		if !strings.Contains(where, want) {
			t.Fatalf("where = %q, missing %q", where, want)
		}
	}
	if len(args) != 3 {
		t.Fatalf("args len = %d, want 3", len(args))
	}
}

func TestBillWhereInputChannelFilters(t *testing.T) {
	tests := []struct {
		name        string
		channel     string
		wantSource  string
		wantFlow    string
		wantExclude bool
	}{
		{name: "Shopee API and realtime", channel: "shopee", wantSource: "shopee", wantFlow: "shopee_excel", wantExclude: true},
		{name: "Shopee Excel", channel: "shopee_excel", wantSource: "shopee", wantFlow: "shopee_excel"},
		{name: "Lazada Excel", channel: "lazada_excel", wantSource: "lazada", wantFlow: "lazada_excel"},
		{name: "TikTok Excel", channel: "tiktok_excel", wantSource: "tiktok", wantFlow: "tiktok_excel"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			where, args, _ := billWhere(models.BillListFilter{InputChannel: tt.channel})
			if !strings.Contains(where, "b.source = $") || !strings.Contains(where, "b.raw_data->>'flow'") {
				t.Fatalf("where = %q, missing input channel predicates", where)
			}
			if tt.wantExclude && !strings.Contains(where, "<>") {
				t.Fatalf("where = %q, Shopee channel must exclude Excel imports", where)
			}
			if len(args) != 2 || args[0] != tt.wantSource || args[1] != tt.wantFlow {
				t.Fatalf("args = %#v, want [%q %q]", args, tt.wantSource, tt.wantFlow)
			}
		})
	}
}

func TestBillWhereOrderLikeSearchUsesExactOrderPredicate(t *testing.T) {
	where, args, _ := billWhere(models.BillListFilter{Search: "260518Q4C1HSMB"})
	for _, want := range []string{
		"b.id IN",
		"TRIM(LEADING '#' FROM COALESCE(sml_order_id",
		"TRIM(LEADING '#' FROM COALESCE(raw_data->>'order_id'",
		"shopee_order_events",
		"UPPER(order_id)",
	} {
		if !strings.Contains(where, want) {
			t.Fatalf("where = %q, missing %q", where, want)
		}
	}
	for _, want := range []string{
		"b.raw_data->>'subject' ILIKE",
		"b.raw_data->>'email_message_id' ILIKE",
	} {
		if strings.Contains(where, want) {
			t.Fatalf("where = %q, should not use broad email metadata search for exact order id", where)
		}
	}
	if len(args) != 1 || args[0] != "260518Q4C1HSMB" {
		t.Fatalf("args = %#v, want normalized exact order id", args)
	}
}

func TestBillWhereFreeTextSearchIncludesEmailMetadata(t *testing.T) {
	where, args, _ := billWhere(models.BillListFilter{Search: "info@mail.shopee.co.th"})
	for _, want := range []string{
		"b.raw_data->>'email_message_id' ILIKE",
		"b.raw_data->>'message_id' ILIKE",
		"b.raw_data->>'subject' ILIKE",
		"b.raw_data->>'from' ILIKE",
	} {
		if !strings.Contains(where, want) {
			t.Fatalf("where = %q, missing %q", where, want)
		}
	}
	if len(args) != 1 || args[0] != "%info@mail.shopee.co.th%" {
		t.Fatalf("args = %#v, want one fuzzy search arg", args)
	}
}

func TestBillEmailGroupHelpers(t *testing.T) {
	raw, err := json.Marshal(map[string]interface{}{
		"email_message_id": "  message@example.test  ",
		"subject":          "คำสั่งซื้อ #A ถูกจัดส่งแล้ว",
		"from":             "Shopee <noreply@shopee.test>",
	})
	if err != nil {
		t.Fatal(err)
	}
	b := &models.Bill{RawData: raw}
	if got := billEmailMessageID(b); got != "message@example.test" {
		t.Fatalf("message id = %q", got)
	}
	if got := billEmailSubject(b); got != "คำสั่งซื้อ #A ถูกจัดส่งแล้ว" {
		t.Fatalf("subject = %q", got)
	}
	if got := billEmailFrom(b); got != "Shopee <noreply@shopee.test>" {
		t.Fatalf("from = %q", got)
	}
	if got := emailGroupKey("message@example.test"); len(got) != 6 || got != strings.ToUpper(got) {
		t.Fatalf("group key = %q, want 6 uppercase hex chars", got)
	}
}

func TestPrintableEmailKind(t *testing.T) {
	for _, kind := range []string{"email_html", "email_text"} {
		if !isPrintableEmailKind(kind) {
			t.Fatalf("kind %q should be printable", kind)
		}
	}
	for _, kind := range []string{"email_envelope", "application_json", "xlsx", ""} {
		if isPrintableEmailKind(kind) {
			t.Fatalf("kind %q should not be printable", kind)
		}
	}
}
