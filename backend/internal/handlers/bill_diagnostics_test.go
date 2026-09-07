package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"nexflow/internal/models"
)

func TestBuildBillSMLSummaryMarksHistoricalFailuresResolved(t *testing.T) {
	docNo := "BF-INV26090002"
	attemptID := "attempt-1"
	bill := &models.Bill{
		ID: "bill-1", Status: "sent", SMLDocNo: &docNo, CurrentSMLAttemptID: &attemptID,
		SMLCoreStatus: "created", SMLProfileStatus: "complete", SMLStockJobStatus: "completed",
	}
	attempt := &models.BillSMLAttempt{ID: attemptID, State: "sent", Route: "SaleInvoice", DocNo: docNo}

	got := buildBillSMLSummary(bill, attempt, 6)
	if got.ResolutionStatus != "resolved" || got.FailureCount != 6 || got.CanRetry {
		t.Fatalf("summary = %#v", got)
	}
}

func TestBuildBillSMLSummaryAllowsOnlyCurrentExactCoreRetry(t *testing.T) {
	attemptID := "attempt-1"
	bill := &models.Bill{ID: "bill-1", Status: "failed", CurrentSMLAttemptID: &attemptID}
	attempt := &models.BillSMLAttempt{ID: attemptID, State: "failed_exact_retry", Route: "SaleInvoice"}
	if got := buildBillSMLSummary(bill, attempt, 1); !got.CanRetry || got.ResolutionStatus != "unresolved" {
		t.Fatalf("summary = %#v", got)
	}

	attempt.State = "unknown"
	if got := buildBillSMLSummary(bill, attempt, 1); got.CanRetry || got.ResolutionStatus != "unknown" {
		t.Fatalf("unknown summary = %#v", got)
	}

	bill.SMLCoreStatus = "created"
	attempt.State = "failed_exact_retry"
	if got := buildBillSMLSummary(bill, attempt, 1); got.CanRetry || got.ResolutionStatus != "resolved" {
		t.Fatalf("core-created summary = %#v", got)
	}
}

func TestSanitizeAuditLogsForRoleRemovesRawFieldsAndSecrets(t *testing.T) {
	detail := json.RawMessage(`{
		"attempt_id":"attempt-1",
		"doc_no":"BF-INV26090002",
		"sml_payload":{"buyer_phone":"0812345678"},
		"response":{"access_token":"secret"},
		"message":"contact buyer@example.test"
	}`)
	logs := []models.AuditLog{{
		ID: "log-1", Action: "sml_failed", Detail: detail,
		Actor: &models.AuditActor{Name: "Admin", Email: "admin@example.test", Type: "user"},
	}}

	staff := sanitizeAuditLogsForRole(logs, "staff")
	if strings.Contains(string(staff[0].Detail), "sml_payload") || strings.Contains(string(staff[0].Detail), "response") {
		t.Fatalf("staff raw detail leaked: %s", staff[0].Detail)
	}
	if staff[0].Actor.Email != "" {
		t.Fatalf("staff actor email leaked: %#v", staff[0].Actor)
	}
	admin := sanitizeAuditLogsForRole(logs, "admin")
	if strings.Contains(string(admin[0].Detail), "0812345678") || strings.Contains(string(admin[0].Detail), "secret") {
		t.Fatalf("admin diagnostics leaked unsafe values: %s", admin[0].Detail)
	}
}

func TestBillForRoleReturnsRawBusinessFieldsOnlyToAdmin(t *testing.T) {
	bill := &models.Bill{
		RawData: json.RawMessage(`{"raw":true}`), SMLPayload: json.RawMessage(`{"doc_no":"BF-1"}`),
		SMLResponse:  json.RawMessage(`{"success":true}`),
		ShopeeEvents: []models.ShopeeOrderEvent{{RawData: json.RawMessage(`{"buyer":"private"}`)}},
	}
	staff := billForRole(bill, "staff")
	if len(staff.RawData)+len(staff.SMLPayload)+len(staff.SMLResponse)+len(staff.ShopeeEvents[0].RawData) != 0 {
		t.Fatalf("staff bill leaked raw fields: %#v", staff)
	}
	admin := billForRole(bill, "admin")
	if len(admin.RawData) == 0 || len(admin.SMLPayload) == 0 || len(admin.SMLResponse) == 0 {
		t.Fatalf("admin business fields missing: %#v", admin)
	}
}

func TestSMLDiagnosticsRejectsStaffBeforeRepositoryAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/bills/bill-1/sml-diagnostics", nil)
	context.Set("user_role", "staff")

	(&BillHandler{}).SMLDiagnostics(context)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
}
