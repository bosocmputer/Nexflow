package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestTikTokCancellationEvidenceLoadsExactSaleAttemptOwnership(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, 9, 20, 3, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)FROM tiktok_shop_order_snapshots s.*LEFT JOIN LATERAL.*bill_sml_attempts`).
		WithArgs("7494619203789490654", "586030483469993439").
		WillReturnRows(sqlmock.NewRows([]string{
			"shop_id", "order_id", "order_status", "source_hash", "last_synced_at",
			"bill_id", "bill_source", "source_account_key", "source_flow", "bill_status", "document_route", "sml_doc_no",
			"sml_attempt_id", "sml_attempt_state", "sml_attempt_route", "sml_attempt_doc_no",
		}).AddRow(
			"7494619203789490654", "586030483469993439", "CANCELLED", string64("s"), now,
			"03ee1216-acb4-4a88-842c-7edc6eb44292", "tiktok", "shop:7494619203789490654", "tiktok_shop_api_reviewed", "sent", "saleinvoice", "BF-INV26090001",
			"11111111-1111-1111-1111-111111111111", "sent", "saleinvoice", "BF-INV26090001",
		))

	got, err := NewTikTokCancellationRepo(db).Evidence(t.Context(), "7494619203789490654", "586030483469993439")
	if err != nil {
		t.Fatal(err)
	}
	if got.SourceHash != string64("s") || got.BillSourceFlow != "tiktok_shop_api_reviewed" || got.SMLAttemptState != "sent" {
		t.Fatalf("evidence = %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTikTokCancellationStartCreateUsesReviewedEvidenceAndIsIdempotent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, 9, 20, 3, 0, 0, 0, time.UTC)
	input := TikTokCancellationAttemptInput{
		ShopID: "7494619203789490654", OrderID: "586030483469993439",
		BillID: "03ee1216-acb4-4a88-842c-7edc6eb44292", SMLAttemptID: "11111111-1111-1111-1111-111111111111",
		SaleSMLDocNo: "BF-INV26090001", CancelSMLDocNo: "SIC26090001", SourceHash: string64("s"),
		ReviewDigest: string64("d"), RouteEndpoint: "/api/v1/ic/sale-invoices/:doc_no/void",
		RouteConfigVersion: 3, RouteSignature: string64("r"), RequestPayload: json.RawMessage(`{"doc_no":"SIC26090001"}`),
		CreatedBy: "91e80d9f-aba7-4d9e-89db-e7e4e6d262ef",
	}
	mock.ExpectBegin()
	mock.ExpectExec("pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`(?s)SELECT .* FROM tiktok_shop_sml_cancellations.*FOR UPDATE`).
		WithArgs(input.ShopID, input.OrderID, input.SMLAttemptID).
		WillReturnRows(sqlmock.NewRows(tikTokCancellationColumns()).AddRow(
			"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", input.ShopID, input.OrderID, input.BillID, input.SMLAttemptID,
			input.SaleSMLDocNo, "", "previewed", input.SourceHash, input.ReviewDigest, input.RouteEndpoint,
			input.RouteConfigVersion, input.RouteSignature, []byte(`{}`), []byte(`{}`), "", "", input.CreatedBy,
			"not_required", "", 0, nil, nil, nil, now, now,
		))
	mock.ExpectQuery(`(?s)UPDATE tiktok_shop_sml_cancellations.*status='creating'.*request_payload`).
		WithArgs("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", input.CancelSMLDocNo, string(input.RequestPayload), input.CreatedBy).
		WillReturnRows(sqlmock.NewRows(tikTokCancellationColumns()).AddRow(
			"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", input.ShopID, input.OrderID, input.BillID, input.SMLAttemptID,
			input.SaleSMLDocNo, input.CancelSMLDocNo, "creating", input.SourceHash, input.ReviewDigest, input.RouteEndpoint,
			input.RouteConfigVersion, input.RouteSignature, input.RequestPayload, []byte(`{}`), "", "", input.CreatedBy,
			"not_required", "", 0, nil, nil, nil, now, now,
		))
	mock.ExpectCommit()

	result, state, err := NewTikTokCancellationRepo(db).StartCreate(context.Background(), input)
	if err != nil || state != TikTokCancellationStartStarted || result.CancelSMLDocNo != input.CancelSMLDocNo {
		t.Fatalf("result=%+v state=%q err=%v", result, state, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTikTokCancellationCompleteQueuesStockRecalculationOnce(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`(?s)UPDATE tiktok_shop_sml_cancellations.*stock_recalc_status=.*'pending'.*RETURNING`).
		WithArgs("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "created", "SIC26090001", `{"success":true}`, "", "").
		WillReturnRows(sqlmock.NewRows(tikTokCancellationColumns()).AddRow(
			"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "7494619203789490654", "586030483469993439",
			"03ee1216-acb4-4a88-842c-7edc6eb44292", "11111111-1111-1111-1111-111111111111",
			"BF-INV26090001", "SIC26090001", "created", string64("s"), string64("d"),
			"/api/v1/ic/sale-invoices/:doc_no/void", int64(3), string64("r"), []byte(`{"doc_no":"SIC26090001"}`),
			[]byte(`{"success":true}`), "", "", nil, "pending", "", 0, time.Now(), nil, time.Now(), time.Now(), time.Now(),
		))

	got, err := NewTikTokCancellationRepo(db).Complete(t.Context(), "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "created", "SIC26090001", json.RawMessage(`{"success":true}`), "", "")
	if err != nil || got.StockRecalcStatus != "pending" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTikTokCancellationRequestPayloadComparisonIgnoresJSONKeyOrder(t *testing.T) {
	left := json.RawMessage(`{"kind":"void","doc_no":"SIC1"}`)
	right := json.RawMessage(`{"doc_no":"SIC1","kind":"void"}`)
	if !jsonMessagesEqual(left, right) {
		t.Fatal("equivalent JSON payloads must compare equal")
	}
	if jsonMessagesEqual(left, json.RawMessage(`{"doc_no":"SIC2","kind":"void"}`)) {
		t.Fatal("different JSON payloads must not compare equal")
	}
}

func TestTikTokCancellationRecoversStaleCreateAsUnknown(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectExec(`UPDATE tiktok_shop_sml_cancellations`).
		WithArgs(int64(300)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	recovered, err := NewTikTokCancellationRepo(db).RecoverStaleCreates(t.Context(), 5*time.Minute)
	if err != nil || recovered != 1 {
		t.Fatalf("recovered=%d err=%v", recovered, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
