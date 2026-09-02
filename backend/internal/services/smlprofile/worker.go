package smlprofile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"nexflow/internal/config"
	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/sml"
)

const profileReconciliationLease = 2 * time.Minute

type ReconciliationWorker struct {
	bills    *repository.BillRepo
	client   *sml.InvoiceClient
	config   *config.Config
	audit    *repository.AuditLogRepo
	workerID string
	log      *zap.Logger
}

type reconciliationRouteSnapshot struct {
	URLOverride string `json:"url_override"`
}

type reconciliationFailure struct {
	Code     string
	Message  string
	Terminal bool
}

func NewReconciliationWorker(bills *repository.BillRepo, client *sml.InvoiceClient, cfg *config.Config, audit *repository.AuditLogRepo, log *zap.Logger) *ReconciliationWorker {
	tenant := profileInstanceTenant(cfg)
	return &ReconciliationWorker{
		bills: bills, client: client, config: cfg, audit: audit,
		workerID: fmt.Sprintf("%s-sml-profile-%d", firstNonEmptyValue(tenant, "nexflow"), time.Now().UnixNano()), log: log,
	}
}

func (w *ReconciliationWorker) Start(ctx context.Context) {
	if w == nil || w.bills == nil || w.client == nil || w.config == nil || w.config.SMLDocumentProfileMode != ModeActive {
		return
	}
	go func() {
		w.drain(ctx, 3)
		workTicker := time.NewTicker(10 * time.Second)
		alertTicker := time.NewTicker(time.Minute)
		defer workTicker.Stop()
		defer alertTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-workTicker.C:
				w.drain(ctx, 3)
			case <-alertTicker.C:
				w.emitOperationalAlerts(ctx)
			}
		}
	}()
}

func (w *ReconciliationWorker) emitOperationalAlerts(ctx context.Context) {
	if w == nil || w.bills == nil || w.log == nil {
		return
	}
	queue, err := w.bills.SMLProfileQueueMetrics(ctx)
	if err != nil {
		w.warn("sml_profile_alert_metrics_failed", "metrics_unavailable", err)
		return
	}
	reasons := profileAlertReasons(queue, DefaultMetrics.Snapshot())
	if len(reasons) == 0 {
		return
	}
	w.log.Warn("sml_profile_alert",
		zap.String("tenant", queue.TenantKey), zap.Strings("reasons", reasons),
		zap.Int64("queue_depth", queue.QueueDepth), zap.Float64("queue_oldest_seconds", queue.OldestAgeSeconds),
		zap.Float64("queue_age_p95_seconds", queue.QueueAgeP95Seconds), zap.Int64("terminal_count", queue.TerminalCount),
		zap.Int64("payload_mismatch_count", queue.PayloadMismatchCount),
	)
}

func profileAlertReasons(queue *repository.SMLProfileQueueMetrics, requests []RequestMetricSnapshot) []string {
	if queue == nil {
		return nil
	}
	reasons := make([]string, 0, 4)
	if queue.PayloadMismatchCount > 0 {
		reasons = append(reasons, "payload_mismatch")
	}
	if queue.TerminalCount > 0 {
		reasons = append(reasons, "terminal_failure")
	}
	if queue.OldestAgeSeconds > 600 {
		reasons = append(reasons, "queue_oldest_over_10m")
	}
	for _, item := range requests {
		if item.Profile == Version && item.P95MS > 2000 {
			reasons = append(reasons, "gateway_p95_over_2s")
			break
		}
	}
	return reasons
}

func (w *ReconciliationWorker) drain(ctx context.Context, limit int) {
	for i := 0; i < limit; i++ {
		job, err := w.bills.ClaimSMLProfileReconciliationJob(ctx, w.workerID, profileReconciliationLease)
		if err != nil {
			w.warn("claim SML document profile reconciliation", "claim_failed", err)
			return
		}
		if job == nil {
			return
		}
		runCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		failure := w.process(runCtx, job)
		cancel()
		if failure == nil {
			continue
		}
		persisted := true
		if err := w.bills.FailSMLProfileReconciliationJob(context.Background(), job, failure.Code, failure.Message, failure.Terminal); err != nil {
			persisted = false
			w.warn("persist SML document profile reconciliation failure", failure.Code, err)
		}
		terminal := failure.Terminal || job.AttemptCount >= job.MaxAttempts
		if terminal && persisted {
			if _, err := w.bills.PauseShopeeAutoSMLForBill(context.Background(), job.BillID, "profile_terminal_failure"); err != nil {
				w.warn("pause Auto SML after terminal profile failure", "auto_pause_failed", err)
			}
			w.auditEvent(job, "profile_terminal_failure", "error", map[string]any{
				"error_code": failure.Code, "attempt_count": job.AttemptCount, "max_attempts": job.MaxAttempts,
			})
		} else if persisted {
			paused, err := w.bills.PauseShopeeAutoSMLAfterConsecutiveProfileFailures(context.Background(), job.BillID, 3)
			if err != nil {
				w.warn("check consecutive SML document profile failures", "auto_pause_check_failed", err)
			} else if paused > 0 && w.log != nil {
				w.log.Warn("sml_profile_auto_paused",
					zap.String("tenant", job.TenantKey), zap.String("reason", "profile_consecutive_failures"),
					zap.Int("consecutive_jobs", 3), zap.String("correlation_id", job.CorrelationID))
			}
		}
		w.warn("SML document profile reconciliation failed", failure.Code, errors.New(failure.Code))
	}
}

func (w *ReconciliationWorker) process(ctx context.Context, job *repository.SMLProfileReconciliationJob) (failure *reconciliationFailure) {
	started := time.Now()
	defer func() {
		status := "complete"
		failed := false
		if failure != nil {
			status = failure.Code
			failed = true
		}
		DefaultMetrics.ObserveRequest(job.TenantKey, "saleinvoice_reconciliation", job.ProfileVersion, status, time.Since(started), failed)
	}()
	if err := repository.ValidateSMLProfileJobTenant(job.TenantKey, profileInstanceTenant(w.config)); err != nil {
		return &reconciliationFailure{Code: "tenant_mismatch", Message: err.Error(), Terminal: true}
	}
	var immutable struct {
		Version string `json:"document_profile_version"`
	}
	if err := json.Unmarshal(job.PayloadBytes, &immutable); err != nil || immutable.Version != job.ProfileVersion || immutable.Version != Version {
		return &reconciliationFailure{Code: "immutable_payload_invalid", Message: "immutable payload profile version is invalid", Terminal: true}
	}
	var route reconciliationRouteSnapshot
	if err := json.Unmarshal(job.RouteSettings, &route); err != nil {
		return &reconciliationFailure{Code: "route_snapshot_invalid", Message: "immutable route snapshot is invalid", Terminal: true}
	}
	statusCode, response, _, err := w.client.CreateInvoiceBytesWithCorrelation(job.PayloadBytes, route.URLOverride, job.CorrelationID)
	if err != nil {
		return &reconciliationFailure{Code: "gateway_unavailable", Message: "SML Gateway is unavailable"}
	}
	if response == nil {
		return &reconciliationFailure{Code: "gateway_response_missing", Message: "SML Gateway response is missing"}
	}
	if statusCode == http.StatusConflict && response.GetCode() == "doc_no_payload_mismatch" {
		return &reconciliationFailure{Code: "doc_no_payload_mismatch", Message: "SML document payload does not match the immutable attempt", Terminal: true}
	}
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices || !response.IsSuccess() {
		terminal := statusCode >= http.StatusBadRequest && statusCode < http.StatusInternalServerError && statusCode != http.StatusTooManyRequests
		code := firstNonEmptyValue(response.GetCode(), "gateway_rejected")
		return &reconciliationFailure{Code: code, Message: fmt.Sprintf("SML Gateway rejected profile reconciliation (HTTP %d)", statusCode), Terminal: terminal}
	}
	result := response.DocumentProfileResult(job.ProfileVersion)
	if result.PayloadHash == "" {
		return &reconciliationFailure{Code: "profile_hash_missing", Message: "SML Gateway did not return the canonical payload hash"}
	}
	if job.PayloadHash != "" && result.PayloadHash != job.PayloadHash {
		return &reconciliationFailure{Code: "profile_hash_mismatch", Message: "SML Gateway canonical payload hash changed", Terminal: true}
	}
	if result.ProfileStatus != "complete" || result.ReconciliationRequired {
		message := "SML document profile is still incomplete"
		if warning := strings.TrimSpace(response.Data.LogWarning); warning != "" {
			message = warning
		}
		return &reconciliationFailure{Code: "profile_incomplete", Message: message}
	}
	if err := w.bills.CompleteSMLProfileReconciliationJob(ctx, job, result.PayloadHash, result.CompletedChecks); err != nil {
		return &reconciliationFailure{Code: "completion_persist_failed", Message: "persist profile completion failed"}
	}
	if w.log != nil {
		w.log.Info("profile_complete", zap.String("tenant", job.TenantKey), zap.String("profile", job.ProfileVersion),
			zap.String("status", "complete"), zap.Int("attempt_count", job.AttemptCount), zap.String("correlation_id", job.CorrelationID))
	}
	w.auditEvent(job, "profile_complete", "info", map[string]any{
		"attempt_count": job.AttemptCount, "completed_checks": result.CompletedChecks,
	})
	return nil
}

func (w *ReconciliationWorker) auditEvent(job *repository.SMLProfileReconciliationJob, action, level string, detail map[string]any) {
	if w == nil || w.audit == nil || job == nil || strings.TrimSpace(job.BillID) == "" {
		return
	}
	detail["attempt_id"] = job.SMLAttemptID
	detail["profile_version"] = job.ProfileVersion
	detail["status"] = strings.TrimPrefix(action, "profile_")
	_ = w.audit.Log(models.AuditEntry{
		Action: action, TargetID: &job.BillID, Source: "sml", Level: level,
		TraceID: job.CorrelationID, Detail: detail,
	})
}

func (w *ReconciliationWorker) warn(event, code string, err error) {
	if w != nil && w.log != nil {
		w.log.Warn(event, zap.String("code", code), zap.Error(err))
	}
}

func profileInstanceTenant(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return firstNonEmptyValue(strings.TrimSpace(cfg.ShopeeGatewayTenant), strings.TrimSpace(cfg.ShopeeSMLDatabase))
}

func firstNonEmptyValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
