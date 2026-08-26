package repository

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"nexflow/internal/models"
)

func TestInsertPushEventNormalizesEmptyJSONHeaders(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	raw := json.RawMessage(`{"shop_id":99,"code":3,"data":{"ordersn":"ORDER-1"}}`)
	mock.ExpectExec("INSERT INTO shopee_push_events").
		WithArgs(
			int64(99), "ORDER-1", 3, "order_status_push", "READY_TO_SHIP",
			nil, nil, "dedupe-1", string(raw), "{}",
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := NewShopeeRealtimeRepo(db)
	inserted, err := repo.InsertPushEvent(context.Background(), ShopeePushEventInput{
		ShopID: 99, OrderSN: "ORDER-1", PushCode: 3, PushName: "order_status_push",
		EventStatus: "ready_to_ship", DedupeKey: "dedupe-1", RawPayload: raw,
	})
	if err != nil {
		t.Fatalf("InsertPushEvent() error = %v", err)
	}
	if !inserted {
		t.Fatal("InsertPushEvent() inserted = false, want true")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOrderStatusTransitionAtReturnsFirstReadyToShipEvidence(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewShopeeRealtimeRepo(db)

	readyAt := time.Date(2026, 8, 24, 7, 16, 0, 0, time.UTC)
	mock.ExpectQuery("WITH requested\\(shop_id,order_sn\\) AS").
		WithArgs(int64(264993963), "2608247BT82QQM", "READY_TO_SHIP").
		WillReturnRows(sqlmock.NewRows([]string{"shop_id", "order_sn", "transition_at"}).
			AddRow(int64(264993963), "2608247BT82QQM", readyAt))

	got, err := repo.OrderStatusTransitionAt(context.Background(), 264993963, "2608247BT82QQM", "READY_TO_SHIP")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || !got.Equal(readyAt) {
		t.Fatalf("transition = %v, want %v", got, readyAt)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOrderStatusTransitionTimesBatchesOrders(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewShopeeRealtimeRepo(db)

	firstAt := time.Date(2026, 8, 24, 7, 16, 0, 0, time.UTC)
	secondAt := firstAt.Add(time.Minute)
	mock.ExpectQuery("WITH requested\\(shop_id,order_sn\\) AS").
		WithArgs(int64(1), "ORDER-1", int64(1), "ORDER-2", "READY_TO_SHIP").
		WillReturnRows(sqlmock.NewRows([]string{"shop_id", "order_sn", "transition_at"}).
			AddRow(int64(1), "ORDER-1", firstAt).
			AddRow(int64(1), "ORDER-2", secondAt))

	got, err := repo.OrderStatusTransitionTimes(context.Background(), []ShopeeSnapshotRef{
		{ShopID: 1, OrderSN: "ORDER-1"},
		{ShopID: 1, OrderSN: "ORDER-2"},
	}, "READY_TO_SHIP")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got[ShopeeSnapshotRef{ShopID: 1, OrderSN: "ORDER-1"}].Equal(firstAt) {
		t.Fatalf("transitions = %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestShopeeSnapshotStatusGroupWhere(t *testing.T) {
	tests := []struct {
		name  string
		group string
		want  string
	}{
		{name: "all", group: "all", want: ""},
		{name: "empty", group: "", want: ""},
		{name: "unpaid", group: "unpaid", want: "s.order_status = 'UNPAID'"},
		{name: "to ship", group: "to_ship", want: "s.order_status = 'READY_TO_SHIP'"},
		{name: "shipping", group: "shipping", want: "s.order_status IN ('PROCESSED','SHIPPED')"},
		{name: "completed", group: "completed", want: "s.order_status = 'COMPLETED'"},
		{name: "cancelled", group: "cancelled", want: "s.order_status IN ('CANCELLED','IN_CANCEL')"},
		{name: "unknown", group: "bad", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shopeeSnapshotStatusGroupWhere(tt.group); got != tt.want {
				t.Fatalf("where = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrepareSMLCancellationCreatePersistsAttemptBeforeWrite(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewShopeeRealtimeRepo(db)

	mock.ExpectExec(`(?s)UPDATE shopee_sml_cancellations.*cancel_sml_doc_no=\$2.*request_payload=\$3::jsonb.*status='creating'`).
		WithArgs("11111111-1111-1111-1111-111111111111", "CN26080002", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = repo.PrepareSMLCancellationCreate(
		context.Background(),
		"11111111-1111-1111-1111-111111111111",
		"CN26080002",
		json.RawMessage(`{"doc_no":"CN26080002","doc_format_code":"CN"}`),
	)
	if err != nil {
		t.Fatalf("PrepareSMLCancellationCreate() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEnqueueAutoSMLCancellationIsIdempotent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewShopeeRealtimeRepo(db)

	mock.ExpectExec(`(?s)INSERT INTO shopee_sml_cancellations.*trigger_source.*ON CONFLICT.*DO NOTHING`).
		WithArgs(
			int64(264993963), "ORDER-1", "11111111-1111-1111-1111-111111111111",
			"BF-INV26080055", "/api/v1/ic/sale-invoices/:doc_no/cancel", sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	inserted, err := repo.EnqueueAutoSMLCancellation(context.Background(), ShopeeSMLCancellationInput{
		ShopID: 264993963, OrderSN: "ORDER-1", BillID: "11111111-1111-1111-1111-111111111111",
		SaleSMLDocNo: "BF-INV26080055", RouteEndpoint: "/api/v1/ic/sale-invoices/:doc_no/cancel",
		RouteSignature: "route-signature",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !inserted {
		t.Fatal("first final cancellation transition must enqueue")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSupersedeAutoSMLCancellationClosesOnlyRunningAutoAttempt(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewShopeeRealtimeRepo(db)
	mock.ExpectExec(`(?s)UPDATE shopee_sml_cancellations.*status='superseded'.*trigger_source='auto'.*status='creating'`).
		WithArgs("11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.SupersedeAutoSMLCancellation(context.Background(),
		"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCompleteSMLCancellationStockRecalcRequiresOwnedRunningJob(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewShopeeRealtimeRepo(db)
	mock.ExpectExec(`(?s)UPDATE shopee_sml_cancellations.*stock_recalc_status='succeeded'.*stock_recalc_status='running'`).
		WithArgs("11111111-1111-1111-1111-111111111111").
		WillReturnResult(sqlmock.NewResult(0, 0))
	if err := repo.CompleteSMLCancellationStockRecalc(context.Background(), "11111111-1111-1111-1111-111111111111"); err == nil {
		t.Fatal("CompleteSMLCancellationStockRecalc() error = nil, want lost lease")
	}
}

func TestStartSMLCancellationCreateResumesImmutableAttempt(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewShopeeRealtimeRepo(db)
	now := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
	columns := []string{
		"id", "shop_id", "order_sn", "bill_id", "sale_sml_doc_no",
		"cancel_sml_doc_no", "status", "error", "response", "created_by",
		"created_at", "updated_at", "completed_at", "request_payload",
		"trigger_source", "route_endpoint", "route_signature", "error_code",
		"attempts", "next_run_at", "lease_until",
		"stock_recalc_status", "stock_recalc_error", "stock_recalc_attempts",
		"stock_recalc_next_run_at", "stock_recalc_lease_until",
	}
	attempt := []driver.Value{
		"11111111-1111-1111-1111-111111111111", int64(264993963), "ORDER-1",
		"22222222-2222-2222-2222-222222222222", "BF-INV26080055", "CN26080001",
		"failed", "timeout", []byte(`{"status":"unknown"}`), nil, now, now, now,
		[]byte(`{"doc_no":"CN26080001"}`), "auto", "/api/v1/ic/sale-invoices/:doc_no/cancel",
		"route-signature", "sml_cancel_transient", 3, now, nil,
		"not_required", "", 0, nil, nil,
	}
	resumed := append([]driver.Value(nil), attempt...)
	resumed[6] = "creating"
	resumed[7] = ""
	resumed[12] = nil
	resumed[17] = ""

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`(?s)FROM shopee_sml_cancellations.*status IN`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`(?s)FROM shopee_sml_cancellations.*status = 'creating'`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`(?s)FROM shopee_sml_cancellations.*request_payload`).
		WillReturnRows(sqlmock.NewRows(columns).AddRow(attempt...))
	mock.ExpectQuery(`(?s)UPDATE shopee_sml_cancellations.*status='creating'.*RETURNING`).
		WithArgs("11111111-1111-1111-1111-111111111111", "33333333-3333-3333-3333-333333333333").
		WillReturnRows(sqlmock.NewRows(columns).AddRow(resumed...))
	mock.ExpectCommit()

	record, state, err := repo.StartSMLCancellationCreate(context.Background(), ShopeeSMLCancellationInput{
		ShopID: 264993963, OrderSN: "ORDER-1", BillID: "22222222-2222-2222-2222-222222222222",
		SaleSMLDocNo: "BF-INV26080055", CreatedBy: "33333333-3333-3333-3333-333333333333",
		RouteEndpoint: "/api/v1/ic/sale-invoices/:doc_no/cancel", RouteSignature: "route-signature",
	})
	if err != nil {
		t.Fatal(err)
	}
	if state != "resumed" || record == nil || record.CancelSMLDocNo != "CN26080001" ||
		string(record.RequestPayload) != `{"doc_no":"CN26080001"}` {
		t.Fatalf("record=%+v state=%q, want immutable resumed attempt", record, state)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareSMLCancellationCreateRejectsStateRace(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewShopeeRealtimeRepo(db)

	mock.ExpectExec(`(?s)UPDATE shopee_sml_cancellations.*status='creating'`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	err = repo.PrepareSMLCancellationCreate(
		context.Background(),
		"11111111-1111-1111-1111-111111111111",
		"SIC26080002",
		json.RawMessage(`{"doc_no":"SIC26080002","doc_format_code":"SIC"}`),
	)
	if err == nil {
		t.Fatal("PrepareSMLCancellationCreate() error = nil, want state conflict")
	}
}

func TestSMLCancellationDocNoExistsUsesAttemptedStates(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewShopeeRealtimeRepo(db)

	mock.ExpectQuery(`(?s)SELECT EXISTS.*cancel_sml_doc_no=\$1.*status IN`).
		WithArgs("SIC26080002").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	exists, err := repo.SMLCancellationDocNoExists(context.Background(), "SIC26080002")
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("SMLCancellationDocNoExists() = false, want true")
	}
}

func TestShopeeTimelineTitle(t *testing.T) {
	tests := []struct {
		name   string
		kind   string
		title  string
		status string
		want   string
	}{
		{name: "create done", kind: "create_document", status: "done", want: "สร้างเอกสารใน Nexflow แล้ว"},
		{name: "create blocked", kind: "create_document", status: "blocked", want: "สร้างเอกสารถูกบล็อก"},
		{name: "shipping reconcile", kind: "reconcile_shipping", want: "ตรวจสถานะจัดส่งจาก Shopee"},
		{name: "fallback title", kind: "push", title: "order_status_push", want: "order_status_push"},
		{name: "default", kind: "unknown", want: "Shopee update"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shopeeTimelineTitle(tt.kind, tt.title, tt.status); got != tt.want {
				t.Fatalf("title = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestShopeeTimelineSource(t *testing.T) {
	tests := []struct {
		name   string
		source string
		kind   string
		want   string
	}{
		{name: "tracking", kind: "tracking", want: "Seller Center"},
		{name: "push", kind: "push", want: "Push"},
		{name: "snapshot", kind: "snapshot", want: "Sync"},
		{name: "create document", kind: "create_document", want: "Nexflow"},
		{name: "source fallback", source: "Shopee Console", kind: "unknown", want: "Shopee Console"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shopeeTimelineSource(tt.source, tt.kind); got != tt.want {
				t.Fatalf("source = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildShopeeStatusTimelineUsesSnapshotAsCurrentStatus(t *testing.T) {
	unpaidAt := time.Date(2026, 6, 4, 8, 18, 30, 0, time.UTC)
	readyAt := time.Date(2026, 6, 4, 8, 48, 30, 0, time.UTC)
	syncedAt := time.Date(2026, 6, 4, 10, 6, 29, 0, time.UTC)
	steps := buildShopeeStatusTimeline(&models.ShopeeOrderSnapshot{
		OrderStatus:       "READY_TO_SHIP",
		LastUpdateSource:  "shipping",
		LastOrderUpdateAt: &readyAt,
		LastSyncedAt:      syncedAt,
	}, map[string]shopeeStatusEvidence{
		"unpaid": {
			Status:     "UNPAID",
			Source:     "push",
			Confidence: "confirmed",
			OccurredAt: &unpaidAt,
		},
		"to_ship": {
			Status:     "READY_TO_SHIP",
			Source:     "push",
			Confidence: "confirmed",
			OccurredAt: &readyAt,
		},
		"shipping": {
			Status:     "SHIPPED",
			Source:     "push",
			Confidence: "confirmed",
			OccurredAt: ptrTime(readyAt.Add(30 * time.Minute)),
		},
	})

	if got := stepByKey(steps, "to_ship"); got == nil || !got.Current || got.State != "current" {
		t.Fatalf("to_ship step = %+v, want current state", got)
	}
	if got := stepByKey(steps, "shipping"); got == nil || got.Current || got.State != "upcoming" {
		t.Fatalf("shipping step = %+v, want upcoming despite later push evidence", got)
	}
}

func TestBuildShopeeStatusTimelineDoesNotInventMissingHistory(t *testing.T) {
	completedAt := time.Date(2026, 6, 2, 7, 36, 27, 0, time.UTC)
	steps := buildShopeeStatusTimeline(&models.ShopeeOrderSnapshot{
		OrderStatus:       "COMPLETED",
		LastUpdateSource:  "sync",
		LastOrderUpdateAt: &completedAt,
		LastSyncedAt:      completedAt.Add(2 * time.Hour),
	}, nil)

	if got := stepByKey(steps, "completed"); got == nil || got.State != "current" || got.Confidence != "inferred" || got.Source != "sync" {
		t.Fatalf("completed step = %+v, want inferred sync current", got)
	}
	if got := stepByKey(steps, "unpaid"); got == nil || got.OccurredAt != nil || got.Confidence != "missing" {
		t.Fatalf("unpaid step = %+v, want missing time", got)
	}
}

func TestBuildShopeeStatusTimelineCancelledBranch(t *testing.T) {
	cancelAt := time.Date(2026, 6, 4, 9, 30, 0, 0, time.UTC)
	steps := buildShopeeStatusTimeline(&models.ShopeeOrderSnapshot{
		OrderStatus:      "CANCELLED",
		LastUpdateSource: "push",
		LastSyncedAt:     cancelAt,
	}, map[string]shopeeStatusEvidence{
		"cancelled": {
			Status:     "CANCELLED",
			Source:     "push",
			Confidence: "confirmed",
			OccurredAt: &cancelAt,
		},
	})

	if got := stepByKey(steps, "cancelled"); got == nil || !got.Current || !got.Terminal || got.State != "current" {
		t.Fatalf("cancelled step = %+v, want terminal current", got)
	}
	if got := stepByKey(steps, "to_ship"); got == nil || got.State != "skipped" {
		t.Fatalf("to_ship step = %+v, want skipped before cancellation without evidence", got)
	}
}

func stepByKey(steps []models.ShopeeOrderStatusTimelineStep, key string) *models.ShopeeOrderStatusTimelineStep {
	for i := range steps {
		if steps[i].Key == key {
			return &steps[i]
		}
	}
	return nil
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
