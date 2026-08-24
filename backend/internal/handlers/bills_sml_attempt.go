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
)

const smlAttemptLeaseDuration = 5 * time.Minute

type smlAttemptRouteSnapshot struct {
	URLOverride string `json:"url_override,omitempty"`
	Config      any    `json:"config,omitempty"`
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
	mappingRevisions, unitGeneration, setHashes, err := smlAttemptDependencySnapshot(bill)
	if err != nil {
		return nil, err
	}
	routeSettings, err := json.Marshal(smlAttemptRouteSnapshot{URLOverride: urlOverride, Config: routeConfig})
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

	start := time.Now()
	stopHeartbeat, leaseLost := h.startSMLAttemptHeartbeat(attempt.ID, leaseOwner)
	var statusCode int
	var responseBytes []byte
	var responseMessage string
	var responseDocNo string
	var responseSuccess bool
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
		}
	case "SaleInvoice":
		if h.invoiceClient == nil {
			sendErr = errors.New("saleinvoice client not configured")
			break
		}
		var response *sml.InvoiceResponse
		statusCode, response, responseBytes, sendErr = h.invoiceClient.CreateInvoiceBytes(attempt.PayloadBytes, routeSnapshot.URLOverride)
		if response != nil {
			responseMessage = response.GetMessage()
			responseDocNo = response.GetDocNo()
			responseSuccess = response.IsSuccess()
		}
	default:
		sendErr = fmt.Errorf("immutable retry route %q is unsupported", attempt.Route)
	}
	stopHeartbeat()
	if leaseLost.Load() {
		return retrySendResult{HTTPStatus: http.StatusConflict, Error: "สิทธิ์ส่ง SML หมดอายุระหว่างทำงาน ระบบหยุดบันทึกผลเพื่อป้องกันข้อมูลซ้ำ", Route: attempt.Route, Skipped: true}
	}

	if sendErr == nil && responseSuccess && responseDocNo != "" && responseDocNo != attempt.DocNo {
		errMessage := "doc_no_payload_mismatch"
		_ = h.billRepo.FinishSMLAttempt(ctx, attempt.ID, leaseOwner, "stale_requires_reconciliation", "needs_review", responseBytes, errMessage)
		return retrySendResult{HTTPStatus: http.StatusConflict, Error: "SML ตอบเลขเอกสารไม่ตรงกับ payload ต้องตรวจสอบก่อน retry", Route: attempt.Route, DocNoAttempted: attempt.DocNo, Skipped: true}
	}
	if sendErr == nil && responseSuccess {
		responseDocNo = strings.TrimSpace(responseDocNo)
		if responseDocNo == "" {
			responseDocNo = attempt.DocNo
		}
		if err := h.billRepo.FinishSMLAttempt(ctx, attempt.ID, leaseOwner, "sent", "sent", responseBytes, ""); err != nil {
			return retrySendResult{HTTPStatus: http.StatusConflict, Error: "SML รับเอกสารแล้วแต่บันทึกผลไม่ได้ กรุณาตรวจสอบเอกสารก่อน retry", Route: attempt.Route, DocNoAttempted: attempt.DocNo, Skipped: true}
		}
		h.recordSuccessForSend(bill.ID, bill.Source, responseBytes, responseDocNo, attempt.Route, start, opts)
		h.triggerStockRecalculation(bill.ID, responseDocNo, attempt.Route, opts.BulkJobID, billItemCodes(bill))
		return retrySendResult{HTTPStatus: http.StatusOK, Message: "bill sent to SML (immutable payload)", DocNo: responseDocNo, DocNoAttempted: attempt.DocNo, Route: attempt.Route, LogWarning: extractSMLERPLogWarning(responseBytes)}
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
	if err := h.billRepo.FinishSMLAttempt(ctx, attempt.ID, leaseOwner, attemptState, billStatus, responseBytes, string(failureJSON)); err != nil {
		return retrySendResult{HTTPStatus: http.StatusConflict, Error: "ผลการส่ง SML ไม่แน่นอนและ lease เปลี่ยนแล้ว ต้องตรวจสอบก่อน retry", Route: attempt.Route, DocNoAttempted: attempt.DocNo, Skipped: true}
	}
	h.recordSMLAttemptFailureAudit(bill, attempt, opts, start, errMessage, failureClass)
	return retrySendResult{HTTPStatus: http.StatusBadGateway, Error: "SML send failed: " + errMessage, FailureClass: failureClass, DocNoAttempted: attempt.DocNo, Route: attempt.Route}
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
