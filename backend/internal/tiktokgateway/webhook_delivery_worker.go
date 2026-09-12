package tiktokgateway

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
	"nexflow/internal/services/tiktokshop"
)

type WebhookDeliveryStore interface {
	LeaseWebhookDeliveries(context.Context, int) ([]WebhookDeliveryJob, error)
	MarkWebhookDeliveryDone(context.Context, WebhookDeliveryJob) error
	MarkWebhookDeliveryFailed(context.Context, WebhookDeliveryJob, string, time.Time) error
	RecordWebhookDeliveryResult(context.Context, WebhookDeliveryJob, string, int, int, string, string) error
}

type WebhookDeliveryWorker struct {
	store     WebhookDeliveryStore
	masterKey string
	client    *http.Client
	logger    *zap.Logger
	now       func() time.Time
}

func NewWebhookDeliveryWorker(config Config, store WebhookDeliveryStore, logger *zap.Logger) *WebhookDeliveryWorker {
	if logger == nil {
		logger = zap.NewNop()
	}
	timeout := config.TenantHTTPTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &WebhookDeliveryWorker{
		store: store, masterKey: config.InternalMasterKey,
		client: &http.Client{Timeout: timeout}, logger: logger, now: time.Now,
	}
}

func (w *WebhookDeliveryWorker) Start(ctx context.Context, interval time.Duration, batchSize int) {
	if w == nil {
		return
	}
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
				w.logger.Warn("tiktok_gateway_webhook_delivery_batch_failed", zap.String("error_code", "lease_failed"))
			}
		}
	}
}

func (w *WebhookDeliveryWorker) ProcessBatch(ctx context.Context, batchSize int) (int, error) {
	if w == nil || w.store == nil {
		return 0, errors.New("TikTok webhook delivery worker is not configured")
	}
	jobs, err := w.store.LeaseWebhookDeliveries(ctx, batchSize)
	if err != nil {
		return 0, err
	}
	for _, job := range jobs {
		w.process(ctx, job)
	}
	return len(jobs), nil
}

func (w *WebhookDeliveryWorker) process(ctx context.Context, job WebhookDeliveryJob) {
	startedAt := w.now()
	statusCode, errorCode, requestID, nonce, err := w.deliver(ctx, job)
	durationMS := int(w.now().Sub(startedAt).Milliseconds())
	if auditErr := w.store.RecordWebhookDeliveryResult(ctx, job, nonce, statusCode, durationMS, errorCode, requestID); auditErr != nil {
		w.logger.Warn("tiktok_gateway_webhook_delivery_audit_failed", zap.String("tenant", job.TenantSlug), zap.String("error_code", "audit_failed"))
	}
	if err == nil {
		if markErr := w.store.MarkWebhookDeliveryDone(ctx, job); markErr != nil {
			w.logger.Warn("tiktok_gateway_webhook_delivery_mark_done_failed", zap.String("tenant", job.TenantSlug), zap.String("error_code", "mark_done_failed"))
			return
		}
		w.logger.Info("tiktok_gateway_webhook_delivered", zap.String("tenant", job.TenantSlug), zap.String("gateway_event_id", job.WebhookEventID), zap.Int("attempt", job.Attempts), zap.String("request_id", requestID))
		return
	}
	nextRunAt := w.now().Add(webhookDeliveryBackoff(job.Attempts))
	if markErr := w.store.MarkWebhookDeliveryFailed(ctx, job, errorCode, nextRunAt); markErr != nil {
		w.logger.Warn("tiktok_gateway_webhook_delivery_mark_failed", zap.String("tenant", job.TenantSlug), zap.String("error_code", "mark_failed"))
	}
	w.logger.Warn("tiktok_gateway_webhook_delivery_failed", zap.String("tenant", job.TenantSlug), zap.String("gateway_event_id", job.WebhookEventID), zap.Int("attempt", job.Attempts), zap.String("error_code", errorCode), zap.String("request_id", requestID))
}

func (w *WebhookDeliveryWorker) deliver(ctx context.Context, job WebhookDeliveryJob) (statusCode int, errorCode, requestID, nonce string, err error) {
	secret, err := DeriveTenantSecret(w.masterKey, job.TenantSlug)
	if err != nil {
		return 0, "tenant_secret_error", "", "", err
	}
	nonce = webhookRandomHex(18)
	requestID = webhookRandomHex(12)
	endpoint := strings.TrimRight(job.BackendURL, "/") + tiktokshop.GatewayWebhookDeliveryPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(job.Payload))
	if err != nil {
		return 0, "request_build_error", requestID, nonce, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", requestID)
	if err := gatewayauth.Apply(req, job.TenantSlug, secret, job.Payload, w.now(), nonce); err != nil {
		return 0, "request_sign_error", requestID, nonce, err
	}
	response, err := w.client.Do(req)
	if err != nil {
		return 0, "tenant_unavailable", requestID, nonce, err
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if readErr != nil {
		return response.StatusCode, "tenant_response_error", requestID, nonce, readErr
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return response.StatusCode, "", requestID, nonce, nil
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &envelope)
	errorCode = strings.TrimSpace(envelope.Error.Code)
	if errorCode == "" {
		errorCode = fmt.Sprintf("tenant_http_%d", response.StatusCode)
	}
	return response.StatusCode, errorCode, requestID, nonce, errors.New("tenant rejected TikTok webhook delivery")
}

func webhookDeliveryBackoff(attempt int) time.Duration {
	delays := []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute, time.Hour}
	if attempt <= 0 {
		return delays[0]
	}
	if attempt > len(delays) {
		return delays[len(delays)-1]
	}
	return delays[attempt-1]
}

func webhookRandomHex(size int) string {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(buffer)
}
