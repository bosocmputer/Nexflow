package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/services/diagnostics"
)

func billCoreCreated(bill *models.Bill, attempt *models.BillSMLAttempt) bool {
	if bill != nil {
		if bill.Status == "sent" || strings.EqualFold(bill.SMLCoreStatus, "created") || strings.EqualFold(bill.SMLCoreStatus, "already_exists") {
			return true
		}
	}
	return attempt != nil && attempt.State == "sent"
}

func buildBillSMLSummary(bill *models.Bill, attempt *models.BillSMLAttempt, failureCount int) diagnostics.StaffSMLSummary {
	summary := diagnostics.StaffSMLSummary{FailureCount: max(0, failureCount)}
	if bill != nil {
		if bill.CurrentSMLAttemptID != nil {
			summary.AttemptID = strings.TrimSpace(*bill.CurrentSMLAttemptID)
		}
		if bill.SMLDocNo != nil {
			summary.DocumentNumber = strings.TrimSpace(*bill.SMLDocNo)
		}
		summary.CoreStatus = strings.TrimSpace(bill.SMLCoreStatus)
		summary.ProfileStatus = strings.TrimSpace(bill.SMLProfileStatus)
		summary.StockStatus = strings.TrimSpace(bill.SMLStockJobStatus)
	}
	if attempt != nil {
		summary.AttemptID = attempt.ID
		summary.DocumentNumber = attempt.DocNo
		summary.Route = attempt.Route
	}
	created := billCoreCreated(bill, attempt)
	switch {
	case created && summary.FailureCount > 0:
		summary.ResolutionStatus = "resolved"
	case created:
		summary.ResolutionStatus = "succeeded"
	case attempt != nil && (attempt.State == "unknown" || attempt.State == "sending"):
		summary.ResolutionStatus = "unknown"
	default:
		summary.ResolutionStatus = "unresolved"
	}
	current := bill != nil && bill.CurrentSMLAttemptID != nil && attempt != nil && *bill.CurrentSMLAttemptID == attempt.ID
	summary.CanRetry = current && attempt.State == "failed_exact_retry" && !created
	return summary
}

func billForRole(bill *models.Bill, role string) *models.Bill {
	if bill == nil {
		return nil
	}
	copyBill := *bill
	copyBill.ShopeeEvents = append([]models.ShopeeOrderEvent(nil), bill.ShopeeEvents...)
	if role == "admin" {
		return &copyBill
	}
	copyBill.RawData = nil
	copyBill.SMLPayload = nil
	copyBill.SMLResponse = nil
	for index := range copyBill.ShopeeEvents {
		copyBill.ShopeeEvents[index].RawData = nil
	}
	if copyBill.ShopeeStatus != nil {
		status := *copyBill.ShopeeStatus
		status.RawData = nil
		copyBill.ShopeeStatus = &status
	}
	return &copyBill
}

var staffHiddenAuditFields = map[string]struct{}{
	"raw_data": {}, "sml_payload": {}, "sml_response": {}, "payload": {},
	"request": {}, "response": {}, "headers": {}, "request_headers": {},
	"response_headers": {}, "body": {},
}

func pruneStaffAuditValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if _, hidden := staffHiddenAuditFields[strings.ToLower(key)]; hidden {
				delete(typed, key)
				continue
			}
			typed[key] = pruneStaffAuditValue(child)
		}
		return typed
	case []any:
		for index := range typed {
			typed[index] = pruneStaffAuditValue(typed[index])
		}
		return typed
	default:
		return value
	}
}

func sanitizeAuditLogsForRole(logs []models.AuditLog, role string) []models.AuditLog {
	result := make([]models.AuditLog, len(logs))
	for index := range logs {
		result[index] = logs[index]
		if logs[index].Actor != nil {
			actor := *logs[index].Actor
			if role != "admin" {
				actor.Email = ""
			}
			result[index].Actor = &actor
		}
		if len(logs[index].Detail) == 0 {
			continue
		}
		sanitized := diagnostics.SanitizeJSON(logs[index].Detail)
		if !sanitized.ValidJSON {
			result[index].Detail = nil
			continue
		}
		if role == "admin" {
			result[index].Detail = json.RawMessage(sanitized.Body)
			continue
		}
		var detail map[string]any
		if json.Unmarshal(sanitized.Body, &detail) != nil {
			result[index].Detail = nil
			continue
		}
		pruneStaffAuditValue(detail)
		body, err := json.Marshal(detail)
		if err != nil {
			result[index].Detail = nil
			continue
		}
		result[index].Detail = json.RawMessage(body)
	}
	return result
}

func (h *BillHandler) loadBillSMLSummary(ctx context.Context, bill *models.Bill) (diagnostics.StaffSMLSummary, *models.BillSMLAttempt) {
	if h == nil || h.billRepo == nil || bill == nil {
		return diagnostics.StaffSMLSummary{}, nil
	}
	attempt, err := h.billRepo.FindCurrentSMLAttempt(ctx, bill.ID)
	if err != nil {
		if h.log != nil {
			h.log.Warn("load_bill_sml_attempt_summary_failed", zap.String("bill_id", bill.ID), zap.Error(err))
		}
		return buildBillSMLSummary(bill, nil, 0), nil
	}
	failureCount := 0
	if attempt != nil {
		failureCount, err = h.billRepo.CountSMLAttemptFailures(ctx, bill.ID, attempt.ID)
		if err != nil && h.log != nil {
			h.log.Warn("count_bill_sml_failures_failed", zap.String("bill_id", bill.ID), zap.Error(err))
		}
	}
	return buildBillSMLSummary(bill, attempt, failureCount), attempt
}

func (h *BillHandler) SMLDiagnostics(c *gin.Context) {
	if c.GetString("user_role") != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}
	bill, err := h.billRepo.FindByID(c.Param("id"))
	if err != nil || bill == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "bill or SML attempt not found"})
		return
	}
	attempt, err := h.billRepo.FindCurrentSMLAttempt(c.Request.Context(), bill.ID)
	if err != nil || attempt == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "bill or SML attempt not found"})
		return
	}
	failureCount, _ := h.billRepo.CountSMLAttemptFailures(c.Request.Context(), bill.ID, attempt.ID)
	exchanges, err := h.billRepo.ListSMLAttemptExchanges(c.Request.Context(), attempt.ID, 200)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "load SML diagnostics failed"})
		return
	}
	payload := diagnostics.SanitizeJSON(attempt.PayloadBytes)
	packageExchanges := make([]diagnostics.ExchangeEvidence, 0, len(exchanges))
	for _, exchange := range exchanges {
		responseJSON := exchange.ResponseJSON
		if len(responseJSON) > 0 {
			response := diagnostics.SanitizeJSON(responseJSON)
			if response.ValidJSON {
				responseJSON = json.RawMessage(response.Body)
			} else {
				responseJSON = nil
			}
		}
		status := exchange.Status
		summary := exchange.SafeErrorSummary
		if status == "started" {
			summary = "ผลการเรียกครั้งนี้ไม่แน่นอน เนื่องจาก process หยุดก่อนบันทึกผล"
		}
		packageExchanges = append(packageExchanges, diagnostics.ExchangeEvidence{
			ID: exchange.ID, Sequence: exchange.Sequence, TraceID: exchange.TraceID, Status: status,
			StartedAt: exchange.StartedAt, FinishedAt: exchange.FinishedAt, DurationMS: exchange.DurationMS,
			Request: diagnostics.RequestMetadata{
				Method: exchange.RequestMethod, CanonicalPath: exchange.RequestPath,
				ContentType: exchange.RequestContentType, CorrelationID: exchange.CorrelationID,
			},
			ResponseStatus: exchange.HTTPStatus, ResponseHeaders: exchange.ResponseHeaders,
			ResponseJSON: responseJSON, ResponseHash: exchange.ResponseHash,
			ResponseSize: exchange.ResponseSize, ResponseTruncated: exchange.ResponseTruncated,
			ErrorCode: exchange.ErrorCode, ErrorClass: exchange.ErrorClass,
			SafeErrorSummary: summary, DiagnosticAvailable: true,
		})
	}
	requestPayload := json.RawMessage(nil)
	if payload.ValidJSON {
		requestPayload = json.RawMessage(payload.Body)
	}
	c.JSON(http.StatusOK, diagnostics.DiagnosticPackage{
		Version: diagnostics.DiagnosticPackageVersion, GeneratedAt: time.Now().UTC(), BillID: bill.ID,
		Summary: buildBillSMLSummary(bill, attempt, failureCount), RequestPayload: requestPayload,
		Exchanges: packageExchanges,
	})
}
