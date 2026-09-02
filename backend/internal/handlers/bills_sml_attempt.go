package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/sml"
	"nexflow/internal/services/smlprofile"
)

const smlAttemptLeaseDuration = 5 * time.Minute

type smlAttemptRouteSnapshot struct {
	URLOverride     string                             `json:"url_override,omitempty"`
	Config          any                                `json:"config,omitempty"`
	DocumentProfile *smlAttemptDocumentProfileSnapshot `json:"document_profile,omitempty"`
}

type smlAttemptDocumentProfileSnapshot struct {
	Mode            string `json:"mode"`
	Version         string `json:"version,omitempty"`
	ConfigVersion   int64  `json:"config_version"`
	RouteSignature  string `json:"route_signature"`
	ValidationState string `json:"validation_state"`
	ValidationError string `json:"validation_error,omitempty"`
}

func newSMLAttemptLeaseOwner() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err == nil {
		return hex.EncodeToString(buf[:])
	}
	return fmt.Sprintf("lease-%d", time.Now().UnixNano())
}

func (h *BillHandler) createSMLAttempt(
	ctx context.Context,
	bill *models.Bill,
	docNo, route string,
	payload any,
	urlOverride string,
	routeConfig any,
	leaseOwner string,
	opts retrySendOptions,
) (*models.BillSMLAttempt, error) {
	payloadBytes, err := sml.MarshalASCII(payload)
	if err != nil {
		return nil, err
	}
	if len(payloadBytes) > sml.MaxInvoiceDocumentBytes {
		return nil, fmt.Errorf("SML request exceeds %d bytes", sml.MaxInvoiceDocumentBytes)
	}
	mappingRevisions, unitGeneration, setHashes, err := smlAttemptDependencySnapshot(bill)
	if err != nil {
		return nil, err
	}
	routeSnapshot := smlAttemptRouteSnapshot{URLOverride: urlOverride, Config: routeConfig}
	var invoicePayload *sml.InvoicePayload
	switch value := payload.(type) {
	case sml.InvoicePayload:
		invoicePayload = &value
	case *sml.InvoicePayload:
		invoicePayload = value
	}
	if invoicePayload != nil && invoicePayload.ProfileMode != "" && invoicePayload.ProfileMode != "off" {
		validationState := "valid"
		if invoicePayload.ProfileValidationError != "" {
			validationState = "invalid"
		}
		routeSnapshot.DocumentProfile = &smlAttemptDocumentProfileSnapshot{
			Mode: invoicePayload.ProfileMode, Version: invoicePayload.DocumentProfileVersion,
			ConfigVersion: invoicePayload.ProfileConfigVersion, RouteSignature: invoicePayload.ProfileRouteSignature,
			ValidationState: validationState, ValidationError: invoicePayload.ProfileValidationError,
		}
	}
	routeSettings, err := json.Marshal(routeSnapshot)
	if err != nil {
		return nil, err
	}
	tenantKey := ""
	if h.cfg != nil {
		tenantKey = strings.TrimSpace(h.cfg.ShopeeGatewayTenant)
		if tenantKey == "" {
			tenantKey = strings.TrimSpace(h.cfg.ShopeeSMLDatabase)
		}
	}
	var actor *string
	if opts.UserID != "" {
		actor = &opts.UserID
	}
	return h.billRepo.CreateSMLAttempt(ctx, repository.SMLAttemptCreate{
		TenantKey: tenantKey, BillID: bill.ID, DocNo: docNo, Route: route,
		PayloadBytes: payloadBytes, PayloadJSON: json.RawMessage(payloadBytes),
		RouteSettings: routeSettings, MappingRevisions: mappingRevisions,
		UnitCatalogGeneration: unitGeneration, SetDefinitionHashes: setHashes,
		LeaseOwner: leaseOwner, LeaseDuration: smlAttemptLeaseDuration, CreatedBy: actor,
		ExpectedBillRevision: bill.MutationRevision,
	})
}

func smlAttemptDependencySnapshot(bill *models.Bill) (json.RawMessage, *string, json.RawMessage, error) {
	mappingRevisions := make(map[string]int64)
	setHashes := make(map[string]string)
	generations := make(map[string]struct{})
	if bill != nil {
		for _, item := range bill.Items {
			key := item.ID
			if key == "" {
				key = item.SourceItemID + ":" + item.SourceVariantID
			}
			if item.MappingRevisionSnapshot != nil {
				mappingRevisions[key] = *item.MappingRevisionSnapshot
			}
			generation := ""
			if item.UnitCatalogGenerationSnapshot != nil {
				generation = strings.TrimSpace(*item.UnitCatalogGenerationSnapshot)
			}
			if generation != "" {
				generations[generation] = struct{}{}
			}
			if hash := strings.TrimSpace(item.SetDefinitionHashSnapshot); hash != "" {
				setHashes[key] = hash
			}
		}
	}
	if len(generations) > 1 {
		return nil, nil, nil, errors.New("unit_catalog_generation_mismatch")
	}
	var generation *string
	for value := range generations {
		valueCopy := value
		generation = &valueCopy
	}
	revisionsJSON, err := json.Marshal(mappingRevisions)
	if err != nil {
		return nil, nil, nil, err
	}
	setHashesJSON, err := json.Marshal(setHashes)
	if err != nil {
		return nil, nil, nil, err
	}
	return revisionsJSON, generation, setHashesJSON, nil
}

func (h *BillHandler) claimExistingSMLAttempt(ctx context.Context, bill *models.Bill, leaseOwner string) (*models.BillSMLAttempt, error) {
	if bill == nil || bill.CurrentSMLAttemptID == nil || strings.TrimSpace(*bill.CurrentSMLAttemptID) == "" {
		return nil, nil
	}
	return h.billRepo.ClaimExistingSMLAttempt(ctx, bill.ID, leaseOwner, smlAttemptLeaseDuration)
}

func (h *BillHandler) executeSMLAttempt(
	ctx context.Context,
	bill *models.Bill,
	attempt *models.BillSMLAttempt,
	leaseOwner string,
	opts retrySendOptions,
) retrySendResult {
	if attempt == nil {
		return retrySendResult{HTTPStatus: http.StatusInternalServerError, Error: "SML attempt is missing"}
	}
	if attempt.State == "sent" {
		return retrySendResult{HTTPStatus: http.StatusOK, Message: "เอกสารนี้ส่งเข้า SML แล้ว", DocNo: attempt.DocNo, Route: attempt.Route, Skipped: true}
	}
	var routeSnapshot smlAttemptRouteSnapshot
	if err := json.Unmarshal(attempt.RouteSettings, &routeSnapshot); err != nil {
		return retrySendResult{HTTPStatus: http.StatusConflict, Error: "route snapshot ของเอกสารไม่สมบูรณ์ ต้องตรวจสอบก่อนส่งซ้ำ", Route: attempt.Route, Skipped: true}
	}
	if routeSnapshot.DocumentProfile != nil && routeSnapshot.DocumentProfile.Version != "" {
		h.recordSMLProfileAudit(bill, attempt, opts, "profile_requested", "info", map[string]any{
			"profile_version": routeSnapshot.DocumentProfile.Version,
			"config_version":  routeSnapshot.DocumentProfile.ConfigVersion,
			"status":          "requested",
		})
	}

	start := time.Now()
	stopHeartbeat, leaseLost := h.startSMLAttemptHeartbeat(attempt.ID, leaseOwner)
	var statusCode int
	var responseBytes []byte
	var responseMessage string
	var responseDocNo string
	var responseSuccess bool
	var responseCode string
	var profileResult sml.InvoiceDocumentProfileResult
	var sendErr error
	switch attempt.Route {
	case "SaleOrder":
		if h.saleOrderClient == nil {
			sendErr = errors.New("saleorder client not configured")
			break
		}
		var response *sml.SaleOrderResponse
		statusCode, response, responseBytes, sendErr = h.saleOrderClient.CreateSaleOrderBytes(attempt.PayloadBytes, routeSnapshot.URLOverride)
		if response != nil {
			responseMessage = response.GetMessage()
			responseDocNo = response.GetDocNo()
			responseSuccess = response.IsSuccess()
			responseCode = response.GetCode()
		}
	case "SaleInvoice":
		if h.invoiceClient == nil {
			sendErr = errors.New("saleinvoice client not configured")
			break
		}
		var response *sml.InvoiceResponse
		statusCode, response, responseBytes, sendErr = h.invoiceClient.CreateInvoiceBytesWithCorrelation(attempt.PayloadBytes, routeSnapshot.URLOverride, opts.TraceID)
		if response != nil {
			responseMessage = response.GetMessage()
			responseDocNo = response.GetDocNo()
			responseSuccess = response.IsSuccess()
			responseCode = response.GetCode()
			if routeSnapshot.DocumentProfile != nil {
				profileResult = response.DocumentProfileResult(routeSnapshot.DocumentProfile.Version)
			}
		}
	default:
		sendErr = fmt.Errorf("immutable retry route %q is unsupported", attempt.Route)
	}
	httpSuccess := statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices
	stopHeartbeat()
	if routeSnapshot.DocumentProfile != nil && routeSnapshot.DocumentProfile.Version != "" {
		metricStatus := strings.TrimSpace(profileResult.ProfileStatus)
		if metricStatus == "" {
			metricStatus = strings.TrimSpace(responseCode)
		}
		if metricStatus == "" && sendErr != nil {
			metricStatus = "transport_error"
		}
		if metricStatus == "" {
			metricStatus = "unknown"
		}
		tenant := ""
		if h.cfg != nil {
			tenant = firstNonEmpty(strings.TrimSpace(h.cfg.ShopeeGatewayTenant), strings.TrimSpace(h.cfg.ShopeeSMLDatabase))
		}
		smlprofile.DefaultMetrics.ObserveRequest(tenant, attempt.Route, routeSnapshot.DocumentProfile.Version, metricStatus, time.Since(start), sendErr != nil || !httpSuccess || !responseSuccess)
	}
	if leaseLost.Load() {
		return retrySendResult{HTTPStatus: http.StatusConflict, Error: "สิทธิ์ส่ง SML หมดอายุระหว่างทำงาน ระบบหยุดบันทึกผลเพื่อป้องกันข้อมูลซ้ำ", Route: attempt.Route, Skipped: true}
	}

	if routeSnapshot.DocumentProfile != nil && responseCode == "doc_no_payload_mismatch" {
		profileCompletion := &repository.SMLProfileCompletion{
			Version: routeSnapshot.DocumentProfile.Version, CoreStatus: "existing_document_conflict",
			ProfileStatus: "terminal_failure", ReconciliationRequired: false, CorrelationID: opts.TraceID,
		}
		if err := h.billRepo.FinishSMLAttempt(ctx, attempt.ID, leaseOwner, "stale_requires_reconciliation", "needs_review", responseBytes, "doc_no_payload_mismatch", profileCompletion); err != nil {
			return retrySendResult{HTTPStatus: http.StatusConflict, Error: "พบ payload ไม่ตรงและบันทึกสถานะป้องกันไม่สำเร็จ ต้องหยุดส่งและตรวจเอกสารทันที", Route: attempt.Route, DocNoAttempted: attempt.DocNo, Skipped: true}
		}
		_, _ = h.billRepo.PauseShopeeAutoSMLForBill(context.Background(), bill.ID, "profile_payload_mismatch")
		h.recordSMLProfileAudit(bill, attempt, opts, "profile_terminal_failure", "error", map[string]any{
			"profile_version": routeSnapshot.DocumentProfile.Version, "error_code": "doc_no_payload_mismatch",
			"impact": "auto_sml_paused_core_not_resent",
		})
		return retrySendResult{HTTPStatus: http.StatusConflict, Error: "เลขเอกสารนี้มี payload คนละชุด ระบบหยุด Auto SML และห้ามส่ง Core ซ้ำจนกว่าจะตรวจสอบ", Route: attempt.Route, DocNoAttempted: attempt.DocNo, CoreStatus: "existing_document_conflict", ProfileStatus: "terminal_failure", Skipped: true}
	}
	if sendErr == nil && httpSuccess && responseSuccess && responseDocNo != "" && responseDocNo != attempt.DocNo {
		errMessage := "doc_no_payload_mismatch"
		_ = h.billRepo.FinishSMLAttempt(ctx, attempt.ID, leaseOwner, "stale_requires_reconciliation", "needs_review", responseBytes, errMessage, nil)
		return retrySendResult{HTTPStatus: http.StatusConflict, Error: "SML ตอบเลขเอกสารไม่ตรงกับ payload ต้องตรวจสอบก่อน retry", Route: attempt.Route, DocNoAttempted: attempt.DocNo, Skipped: true}
	}
	if sendErr == nil && httpSuccess && responseSuccess {
		responseDocNo = strings.TrimSpace(responseDocNo)
		if responseDocNo == "" {
			responseDocNo = attempt.DocNo
		}
		var profileCompletion *repository.SMLProfileCompletion
		if profileResult.Version != "" {
			profileCompletion = &repository.SMLProfileCompletion{
				Version: profileResult.Version, PayloadHash: profileResult.PayloadHash,
				CoreStatus: profileResult.CoreStatus, ProfileStatus: profileResult.ProfileStatus,
				RequiredChecks: profileResult.RequiredChecks, CompletedChecks: profileResult.CompletedChecks,
				ReconciliationRequired: profileResult.ReconciliationRequired, CorrelationID: opts.TraceID,
			}
		}
		if err := h.billRepo.FinishSMLAttempt(ctx, attempt.ID, leaseOwner, "sent", "sent", responseBytes, "", profileCompletion); err != nil {
			return retrySendResult{HTTPStatus: http.StatusConflict, Error: "SML รับเอกสารแล้วแต่บันทึกผลไม่ได้ กรุณาตรวจสอบเอกสารก่อน retry", Route: attempt.Route, DocNoAttempted: attempt.DocNo, Skipped: true}
		}
		if profileCompletion != nil {
			h.recordSMLProfileAudit(bill, attempt, opts, "core_committed", "info", map[string]any{
				"profile_version": profileCompletion.Version, "core_status": profileCompletion.CoreStatus,
				"profile_status": profileCompletion.ProfileStatus,
			})
			if profileCompletion.ReconciliationRequired {
				h.recordSMLProfileAudit(bill, attempt, opts, "reconcile_queued", "warn", map[string]any{
					"profile_version": profileCompletion.Version, "profile_status": profileCompletion.ProfileStatus,
					"required_checks": profileCompletion.RequiredChecks, "completed_checks": profileCompletion.CompletedChecks,
				})
			} else if profileCompletion.ProfileStatus == "complete" {
				h.recordSMLProfileAudit(bill, attempt, opts, "profile_complete", "info", map[string]any{
					"profile_version": profileCompletion.Version, "profile_status": "complete",
					"completed_checks": profileCompletion.CompletedChecks,
				})
			}
		}
		h.recordSuccessForSend(bill.ID, bill.Source, responseBytes, responseDocNo, attempt.Route, start, opts)
		if h.cfg == nil || !h.cfg.MarketplaceReservationLedgerEnabled {
			h.triggerStockRecalculation(bill.ID, responseDocNo, attempt.Route, opts.BulkJobID, billItemCodes(bill))
		}
		return retrySendResult{
			HTTPStatus: http.StatusOK, Message: "bill sent to SML (immutable payload)", DocNo: responseDocNo,
			DocNoAttempted: attempt.DocNo, Route: attempt.Route, LogWarning: extractSMLERPLogWarning(responseBytes),
			SMLAttemptID: attempt.ID, PayloadHash: profileResult.PayloadHash, CoreStatus: profileResult.CoreStatus,
			ProfileStatus: profileResult.ProfileStatus, RequiredChecks: profileResult.RequiredChecks,
			CompletedChecks: profileResult.CompletedChecks, ReconciliationRequired: profileResult.ReconciliationRequired,
		}
	}

	errMessage := strings.TrimSpace(responseMessage)
	if sendErr != nil {
		errMessage = sendErr.Error()
	}
	if errMessage == "" {
		errMessage = fmt.Sprintf("HTTP %d", statusCode)
	}
	failureClass := classifySMLSendFailure(statusCode, sendErr)
	attemptState := "failed_exact_retry"
	if failureClass == "transient" {
		attemptState = "unknown"
	}
	billStatus := "failed"
	if isSetProductFailure(errMessage) {
		billStatus = "needs_review"
	}
	failure := failureDetail{Route: attempt.Route, DocNoAttempted: attempt.DocNo, Error: errMessage, OccurredAt: time.Now().UTC().Format(time.RFC3339)}
	failureJSON, _ := json.Marshal(failure)
	if err := h.billRepo.FinishSMLAttempt(ctx, attempt.ID, leaseOwner, attemptState, billStatus, responseBytes, string(failureJSON), nil); err != nil {
		return retrySendResult{HTTPStatus: http.StatusConflict, Error: "ผลการส่ง SML ไม่แน่นอนและ lease เปลี่ยนแล้ว ต้องตรวจสอบก่อน retry", Route: attempt.Route, DocNoAttempted: attempt.DocNo, Skipped: true}
	}
	h.recordSMLAttemptFailureAudit(bill, attempt, opts, start, errMessage, failureClass)
	return retrySendResult{HTTPStatus: http.StatusBadGateway, Error: "SML send failed: " + errMessage, FailureClass: failureClass, DocNoAttempted: attempt.DocNo, Route: attempt.Route}
}

func (h *BillHandler) recordSMLProfileAudit(bill *models.Bill, attempt *models.BillSMLAttempt, opts retrySendOptions, action, level string, detail map[string]any) {
	if h == nil || h.auditRepo == nil || bill == nil || attempt == nil {
		return
	}
	var actor *string
	if opts.UserID != "" {
		actor = &opts.UserID
	}
	detail["attempt_id"] = attempt.ID
	detail["route"] = attempt.Route
	detail["via"] = opts.Via
	_ = h.auditRepo.Log(models.AuditEntry{
		Action: action, TargetID: &bill.ID, UserID: actor, Source: "sml",
		Level: level, TraceID: opts.TraceID, Detail: detail,
	})
}

func (h *BillHandler) startSMLAttemptHeartbeat(attemptID, leaseOwner string) (func(), *atomic.Bool) {
	ctx, cancel := context.WithCancel(context.Background())
	lost := &atomic.Bool{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				renewCtx, renewCancel := context.WithTimeout(context.Background(), 5*time.Second)
				err := h.billRepo.RenewSMLAttemptLease(renewCtx, attemptID, leaseOwner, smlAttemptLeaseDuration)
				renewCancel()
				if err != nil {
					lost.Store(true)
					return
				}
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}, lost
}

func billItemCodes(bill *models.Bill) []string {
	seen := make(map[string]struct{})
	var codes []string
	if bill == nil {
		return codes
	}
	for _, item := range bill.Items {
		if item.ItemCode == nil {
			continue
		}
		code := strings.TrimSpace(*item.ItemCode)
		if code == "" {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		codes = append(codes, code)
	}
	return codes
}

func (h *BillHandler) recordSMLAttemptFailureAudit(bill *models.Bill, attempt *models.BillSMLAttempt, opts retrySendOptions, start time.Time, message, failureClass string) {
	if h.auditRepo != nil && bill != nil && attempt != nil {
		var actor *string
		if opts.UserID != "" {
			actor = &opts.UserID
		}
		duration := int(time.Since(start).Milliseconds())
		_ = h.auditRepo.Log(models.AuditEntry{
			Action: "sml_failed", TargetID: &bill.ID, UserID: actor, Source: bill.Source,
			Level: "error", TraceID: opts.TraceID, DurationMs: &duration,
			Detail: map[string]any{
				"attempt_id": attempt.ID, "payload_hash": attempt.PayloadHash, "doc_no": attempt.DocNo,
				"route": attempt.Route, "message": message, "failure_class": failureClass,
				"error_code": inferSMLFailureCode(message), "via": opts.Via,
			},
		})
	}
	if h.log != nil {
		h.log.Error("SML immutable attempt failed", zap.String("bill", bill.ID), zap.String("attempt", attempt.ID), zap.String("route", attempt.Route), zap.String("failure_class", failureClass), zap.String("message", message))
	}
	if h.lineSvc != nil && !opts.SuppressLineAlert {
		_ = h.lineSvc.PushAdmin(fmt.Sprintf("⚠️ Bill retry SML failed (%s)\nBill: %s\nError: %s", attempt.Route, bill.ID, message))
	}
}
