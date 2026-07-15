package shopeegateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"nexflow/internal/services/gatewayauth"
)

const (
	TenantConnectionPath = "/internal/v1/shopee-gateway/connections/upsert"
	TenantPushPath       = "/internal/v1/shopee-gateway/push"
)

type DeliveryStore interface {
	LeaseDeliveries(ctx context.Context, limit int) ([]DeliveryJob, error)
	MarkDeliveryDone(ctx context.Context, job DeliveryJob) error
	MarkDeliveryFailed(ctx context.Context, job DeliveryJob, errorCode string, nextRunAt time.Time) error
	RecordOutboundAPIResult(ctx context.Context, job DeliveryJob, nonce, operation string, statusCode, durationMS int, errorCode, requestID string) error
}

type DeliveryWorker struct {
	store     DeliveryStore
	masterKey string
	client    *http.Client
	logger    *zap.Logger
	now       func() time.Time
}

func NewDeliveryWorker(cfg Config, store DeliveryStore, logger *zap.Logger) *DeliveryWorker {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &DeliveryWorker{
		store:     store,
		masterKey: cfg.InternalMasterKey,
		client:    &http.Client{Timeout: cfg.TenantHTTPTimeout},
		logger:    logger,
		now:       time.Now,
	}
}

func (w *DeliveryWorker) Start(ctx context.Context, interval time.Duration, batchSize int) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := w.ProcessBatch(ctx, batchSize); err != nil && !errors.Is(err, context.Canceled) {
				w.logger.Warn("shopee_gateway_delivery_batch_failed", zap.Error(err))
			}
		}
	}
}

func (w *DeliveryWorker) ProcessBatch(ctx context.Context, batchSize int) (int, error) {
	if w == nil || w.store == nil {
		return 0, errors.New("delivery worker is not configured")
	}
	jobs, err := w.store.LeaseDeliveries(ctx, batchSize)
	if err != nil {
		return 0, err
	}
	for _, job := range jobs {
		w.process(ctx, job)
	}
	return len(jobs), nil
}

func (w *DeliveryWorker) process(ctx context.Context, job DeliveryJob) {
	startedAt := w.now()
	statusCode, errorCode, requestID, nonce, err := w.deliver(ctx, job)
	durationMS := int(w.now().Sub(startedAt).Milliseconds())
	if auditErr := w.store.RecordOutboundAPIResult(ctx, job, nonce, job.EventType, statusCode, durationMS, errorCode, requestID); auditErr != nil {
		w.logger.Warn("shopee_gateway_delivery_audit_failed", zap.String("tenant", job.TenantSlug), zap.String("event_type", job.EventType), zap.Error(auditErr))
	}
	if err == nil {
		if markErr := w.store.MarkDeliveryDone(ctx, job); markErr != nil {
			w.logger.Warn("shopee_gateway_delivery_mark_done_failed", zap.String("tenant", job.TenantSlug), zap.String("event_type", job.EventType), zap.Error(markErr))
		}
		return
	}
	nextRunAt := w.now().Add(deliveryBackoff(job.Attempts))
	if markErr := w.store.MarkDeliveryFailed(ctx, job, errorCode, nextRunAt); markErr != nil {
		w.logger.Warn("shopee_gateway_delivery_mark_failed", zap.String("tenant", job.TenantSlug), zap.String("event_type", job.EventType), zap.Error(markErr))
	}
	w.logger.Warn("shopee_gateway_delivery_failed",
		zap.String("tenant", job.TenantSlug),
		zap.String("event_type", job.EventType),
		zap.Int("attempt", job.Attempts),
		zap.String("error_code", errorCode),
		zap.String("request_id", requestID),
	)
}

func (w *DeliveryWorker) deliver(ctx context.Context, job DeliveryJob) (statusCode int, errorCode, requestID, nonce string, err error) {
	path := ""
	switch job.EventType {
	case "connection_upsert":
		path = TenantConnectionPath
	case "push_event":
		path = TenantPushPath
	default:
		return 0, "unsupported_event_type", "", "", errors.New("unsupported delivery event type")
	}
	secret, err := DeriveTenantSecret(w.masterKey, job.TenantSlug)
	if err != nil {
		return 0, "tenant_secret_error", "", "", err
	}
	nonce = randomHex(18)
	requestID = randomHex(12)
	endpoint := strings.TrimRight(job.BackendURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(job.Payload))
	if err != nil {
		return 0, "request_build_error", requestID, nonce, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", requestID)
	if err := gatewayauth.Apply(req, job.TenantSlug, secret, job.Payload, w.now(), nonce); err != nil {
		return 0, "request_sign_error", requestID, nonce, err
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return 0, "tenant_unavailable", requestID, nonce, err
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if readErr != nil {
		return resp.StatusCode, "tenant_response_error", requestID, nonce, readErr
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp.StatusCode, "", requestID, nonce, nil
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &envelope)
	errorCode = strings.TrimSpace(envelope.Error.Code)
	if errorCode == "" {
		errorCode = fmt.Sprintf("tenant_http_%d", resp.StatusCode)
	}
	return resp.StatusCode, errorCode, requestID, nonce, errors.New("tenant rejected gateway delivery")
}

func deliveryBackoff(attempt int) time.Duration {
	delays := []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute, time.Hour}
	if attempt <= 0 {
		return delays[0]
	}
	if attempt > len(delays) {
		return delays[len(delays)-1]
	}
	return delays[attempt-1]
}

func randomHex(size int) string {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(buffer)
}
