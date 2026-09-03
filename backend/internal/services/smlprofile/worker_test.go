package smlprofile

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexflow/internal/config"
	"nexflow/internal/repository"
	"nexflow/internal/services/sml"
)

func TestReconciliationWorkerReplaysImmutablePayloadAndCompletesProfileOnly(t *testing.T) {
	wantBody := []byte(`{"document_profile_version":"sml-document-v1","doc_no":"BF-1"}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ := io.ReadAll(r.Body)
		if string(got) != string(wantBody) {
			t.Errorf("immutable body changed: %s", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"doc_no":"BF-1","status":"already_exists","payload_hash":"hash-1","core_status":"already_exists","profile_status":"complete","required_checks":["core","erp_log"],"completed_checks":["core","erp_log"],"reconciliation_required":false}}`))
	}))
	defer server.Close()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE sml_document_profile_reconciliation_jobs SET`)).
		WithArgs("job-1", "worker-1", int64(2), "hash-1", []byte(`["core","erp_log"]`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE bill_sml_attempts SET`)).
		WithArgs("attempt-1", "hash-1", []byte(`["core","erp_log"]`), Version).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	cfg := &config.Config{SMLDocumentProfileMode: ModeActive, ShopeeGatewayTenant: "aoy"}
	worker := NewReconciliationWorker(repository.NewBillRepo(db), sml.NewInvoiceClient(sml.InvoiceConfig{BaseURL: server.URL}, nil), nil, cfg, nil, nil)
	job := &repository.SMLProfileReconciliationJob{
		ID: "job-1", TenantKey: "aoy", SMLAttemptID: "attempt-1", ProfileVersion: Version,
		PayloadHash: "hash-1", LeaseOwner: "worker-1", LeaseToken: 2, AttemptCount: 1, MaxAttempts: 10,
		PayloadBytes: wantBody, RouteSettings: []byte(`{}`),
	}
	if failure := worker.process(context.Background(), job); failure != nil {
		t.Fatalf("failure=%+v", failure)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReconciliationWorkerReplaysSaleOrderWithoutResendingAnotherRoute(t *testing.T) {
	wantBody := []byte(`{"document_profile_version":"sml-document-v1","doc_no":"BF-SO1","items":[]}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ic/sale-orders" {
			t.Errorf("path=%s", r.URL.Path)
		}
		got, _ := io.ReadAll(r.Body)
		if string(got) != string(wantBody) {
			t.Errorf("immutable body changed: %s", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"doc_no":"BF-SO1","status":"already_exists","payload_hash":"hash-so","core_status":"already_exists","profile_status":"complete","required_checks":["core","erp_log","main_log"],"completed_checks":["core","erp_log","main_log"],"reconciliation_required":false}}`))
	}))
	defer server.Close()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE sml_document_profile_reconciliation_jobs SET`)).
		WithArgs("job-so", "worker-so", int64(3), "hash-so", []byte(`["core","erp_log","main_log"]`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE bill_sml_attempts SET`)).
		WithArgs("attempt-so", "hash-so", []byte(`["core","erp_log","main_log"]`), Version).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	cfg := &config.Config{ShopeeGatewayTenant: "aoy", SMLDocumentProfileRouteModes: map[string]string{"saleorder": ModeActive}}
	worker := NewReconciliationWorker(repository.NewBillRepo(db), nil, sml.NewSaleOrderClient(sml.SaleOrderConfig{BaseURL: server.URL}, nil), cfg, nil, nil)
	job := &repository.SMLProfileReconciliationJob{
		ID: "job-so", TenantKey: "aoy", SMLAttemptID: "attempt-so", ProfileVersion: Version,
		PayloadHash: "hash-so", LeaseOwner: "worker-so", LeaseToken: 3, AttemptCount: 1, MaxAttempts: 10,
		Route: "SaleOrder", PayloadBytes: wantBody, RouteSettings: []byte(`{}`),
	}
	if failure := worker.process(context.Background(), job); failure != nil {
		t.Fatalf("failure=%+v", failure)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReconciliationWorkerRejectsCrossTenantBeforeGateway(t *testing.T) {
	worker := NewReconciliationWorker(nil, nil, nil, &config.Config{SMLDocumentProfileMode: ModeActive, ShopeeGatewayTenant: "aoy"}, nil, nil)
	failure := worker.process(context.Background(), &repository.SMLProfileReconciliationJob{TenantKey: "demo"})
	if failure == nil || failure.Code != "tenant_mismatch" || !failure.Terminal {
		t.Fatalf("failure=%+v", failure)
	}
}

func TestReconciliationWorkerPreservesSafeGatewayRecoveryMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"doc_no":"BF-1","payload_hash":"hash-1","core_status":"already_exists","profile_status":"needs_reconciliation","reconciliation_required":true,"log_warning":"บันทึก SML erp_logs ไม่สำเร็จ: เชื่อมต่อฐานข้อมูล logs ไม่ได้"}}`))
	}))
	defer server.Close()

	cfg := &config.Config{SMLDocumentProfileMode: ModeActive, ShopeeGatewayTenant: "aoy"}
	worker := NewReconciliationWorker(nil, sml.NewInvoiceClient(sml.InvoiceConfig{BaseURL: server.URL}, nil), nil, cfg, nil, nil)
	failure := worker.process(context.Background(), &repository.SMLProfileReconciliationJob{
		TenantKey: "aoy", ProfileVersion: Version, PayloadHash: "hash-1",
		PayloadBytes: []byte(`{"document_profile_version":"sml-document-v1","doc_no":"BF-1"}`), RouteSettings: []byte(`{}`),
	})
	if failure == nil || failure.Code != "profile_incomplete" || !strings.Contains(failure.Message, "erp_logs") {
		t.Fatalf("failure=%+v", failure)
	}
}

func TestProfileAlertReasonsUseOnlyBoundedOperationalDimensions(t *testing.T) {
	reasons := profileAlertReasons(&repository.SMLProfileQueueMetrics{
		PayloadMismatchCount: 1, TerminalCount: 2, OldestAgeSeconds: 601,
	}, []RequestMetricSnapshot{{Profile: Version, P95MS: 2001}})
	want := []string{"payload_mismatch", "terminal_failure", "queue_oldest_over_10m", "gateway_p95_over_2s"}
	if strings.Join(reasons, ",") != strings.Join(want, ",") {
		t.Fatalf("reasons=%v", reasons)
	}
}
