package shopeegateway

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/services/gatewayauth"
	"nexflow/internal/services/shopeeapi"
	"nexflow/internal/services/shopeepush"
)

const maxInternalRequestBytes = 2 << 20

type GatewayService interface {
	CreateAuthURL(ctx context.Context, tenantSlug, userID, returnURL string) (*shopeeapi.GatewayAuthURLResponse, error)
	CompleteOAuth(ctx context.Context, code, state string, callbackShopID int64) (*OAuthCallbackResult, error)
	Execute(ctx context.Context, tenantSlug string, req GatewayExecuteRequest) (json.RawMessage, error)
}

type InternalRequestVerifier interface {
	Verify(ctx context.Context, req *http.Request, body []byte) (*gatewayauth.Identity, error)
}

type APILogRecorder interface {
	RecordAPIResult(ctx context.Context, tenant, nonce, operation string, statusCode, durationMS int, errorCode, requestID string) error
}

type PushReceiver interface {
	AcceptPushEvent(ctx context.Context, input PushEventInput) (*PushEventResult, error)
}

type Handler struct {
	service  GatewayService
	verifier InternalRequestVerifier
	audit    APILogRecorder
	push     PushReceiver
	config   Config
	logger   *zap.Logger
}

type authURLRequest struct {
	UserID    string `json:"user_id"`
	ReturnURL string `json:"return_url"`
}

func NewHandler(service GatewayService, verifier InternalRequestVerifier, audit APILogRecorder, push PushReceiver, config Config, logger *zap.Logger) *Handler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Handler{service: service, verifier: verifier, audit: audit, push: push, config: config, logger: logger}
}

func (h *Handler) Register(r *gin.Engine) {
	r.GET("/health", h.Health)
	r.GET("/api/shopee/callback", h.OAuthCallback)
	r.POST("/webhook/shopee", h.PushWebhook)
	r.POST(shopeeapi.GatewayOAuthPath, h.CreateAuthURL)
	r.POST(shopeeapi.GatewayExecutePath, h.Execute)
}

func (h *Handler) PushWebhook(c *gin.Context) {
	if h.push == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "push receiver is not ready"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid push payload"})
		return
	}
	signature := c.GetHeader("Authorization")
	if strings.TrimSpace(signature) == "" {
		signature = c.GetHeader("X-Shopee-Signature")
	}
	if err := shopeepush.Verify(h.config.PushSecret, c.Query("token"), signature, h.pushCallbackURLs(c), body); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid push signature"})
		return
	}
	event, err := shopeepush.Parse(body)
	if err != nil {
		// Shopee verification probes may not contain an actionable event. ACK
		// authenticated probes without guessing a tenant.
		h.logger.Info("shopee_gateway_push_probe", zap.String("payload_hash", hashBody(body)))
		c.JSON(http.StatusOK, gin.H{"success": true, "queued": false, "diagnostic": true})
		return
	}
	result, err := h.push.AcceptPushEvent(c.Request.Context(), PushEventInput{
		ShopID: event.ShopID, OrderSN: event.OrderSN, PushCode: event.Code,
		EventStatus: event.Status, DedupeKey: event.DedupeKey, RawPayload: json.RawMessage(body),
	})
	if err != nil {
		h.logger.Warn("shopee_gateway_push_store_failed", zap.Int64("shop_id", event.ShopID), zap.Int("push_code", event.Code), zap.Error(err))
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "push queue unavailable"})
		return
	}
	tenant := ""
	if result.Tenant != nil {
		tenant = result.Tenant.Slug
	}
	h.logger.Info("shopee_gateway_push_accepted", zap.Int64("shop_id", event.ShopID), zap.Int("push_code", event.Code), zap.String("tenant", tenant), zap.Bool("inserted", result.Inserted))
	c.JSON(http.StatusOK, gin.H{"success": true, "queued": result.Inserted && result.Tenant != nil})
}

func (h *Handler) pushCallbackURLs(c *gin.Context) []string {
	requestURI := c.Request.URL.RequestURI()
	urls := []string{strings.TrimRight(h.config.PublicBaseURL, "/") + requestURI}
	scheme := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto"))
	if scheme == "" {
		scheme = "https"
	}
	host := strings.TrimSpace(c.GetHeader("X-Forwarded-Host"))
	if host == "" {
		host = c.Request.Host
	}
	if host != "" {
		urls = append(urls, scheme+"://"+host+requestURI)
	}
	return urls
}

func (h *Handler) Health(c *gin.Context) {
	if checker, ok := h.push.(interface{ Ping(context.Context) error }); ok {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := checker.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "service": "nexflow-shopee-gateway", "database": "unavailable"})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "nexflow-shopee-gateway", "database": "ok"})
}

func (h *Handler) CreateAuthURL(c *gin.Context) {
	body, identity, ok := h.authenticate(c)
	if !ok {
		return
	}
	startedAt := time.Now()
	requestID := requestID()
	operation := "oauth_auth_url"
	statusCode := http.StatusOK
	errorCode := ""
	defer func() { h.record(c, identity, operation, statusCode, startedAt, errorCode, requestID) }()

	var req authURLRequest
	if err := decodeJSONBody(body, &req); err != nil || strings.TrimSpace(req.UserID) == "" || strings.TrimSpace(req.ReturnURL) == "" {
		statusCode = http.StatusBadRequest
		errorCode = "invalid_request"
		h.respondError(c, statusCode, serviceError(errorCode, "user_id และ return_url จำเป็นต้องระบุ", statusCode, false, err), requestID)
		return
	}
	out, err := h.service.CreateAuthURL(c.Request.Context(), identity.Tenant, req.UserID, req.ReturnURL)
	if err != nil {
		statusCode, errorCode = serviceErrorMeta(err)
		h.respondError(c, statusCode, err, requestID)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *Handler) Execute(c *gin.Context) {
	body, identity, ok := h.authenticate(c)
	if !ok {
		return
	}
	startedAt := time.Now()
	requestID := requestID()
	statusCode := http.StatusOK
	errorCode := ""
	operation := "unknown"
	defer func() { h.record(c, identity, operation, statusCode, startedAt, errorCode, requestID) }()

	var req GatewayExecuteRequest
	if err := decodeJSONBody(body, &req); err != nil {
		statusCode = http.StatusBadRequest
		errorCode = "invalid_request"
		h.respondError(c, statusCode, serviceError(errorCode, "request ไม่ถูกต้อง", statusCode, false, err), requestID)
		return
	}
	operation = strings.ToLower(strings.TrimSpace(req.Operation))
	callCtx, cancel := context.WithTimeout(c.Request.Context(), 35*time.Second)
	defer cancel()
	out, err := h.service.Execute(callCtx, identity.Tenant, req)
	if err != nil {
		statusCode, errorCode = serviceErrorMeta(err)
		h.respondError(c, statusCode, err, requestID)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": json.RawMessage(out)})
}

func (h *Handler) OAuthCallback(c *gin.Context) {
	code := strings.TrimSpace(c.Query("code"))
	state := strings.TrimSpace(c.Query("state"))
	shopID, _ := strconv.ParseInt(strings.TrimSpace(c.Query("shop_id")), 10, 64)
	result, err := h.service.CompleteOAuth(c.Request.Context(), code, state, shopID)
	if err != nil {
		status, _ := serviceErrorMeta(err)
		h.renderCallback(c, status, "เชื่อมต่อ Shopee ไม่สำเร็จ", err.Error())
		return
	}
	c.Redirect(http.StatusSeeOther, result.ReturnURL)
}

func (h *Handler) authenticate(c *gin.Context) ([]byte, *gatewayauth.Identity, bool) {
	if h == nil || h.service == nil || h.verifier == nil {
		h.respondError(c, http.StatusServiceUnavailable, serviceError("gateway_not_ready", "Shopee gateway ยังไม่พร้อม", 503, true, nil), requestID())
		return nil, nil, false
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxInternalRequestBytes+1))
	if err != nil || len(body) > maxInternalRequestBytes {
		h.respondError(c, http.StatusRequestEntityTooLarge, serviceError("request_too_large", "request มีขนาดใหญ่เกินกำหนด", 413, false, err), requestID())
		return nil, nil, false
	}
	identity, err := h.verifier.Verify(c.Request.Context(), c.Request, body)
	if err != nil {
		code := "invalid_internal_auth"
		status := http.StatusUnauthorized
		if errors.Is(err, gatewayauth.ErrReplay) {
			code = "replayed_request"
			status = http.StatusConflict
		}
		h.respondError(c, status, serviceError(code, "ยืนยันตัวตนระหว่าง Nexflow และ gateway ไม่สำเร็จ", status, false, err), requestID())
		return nil, nil, false
	}
	return body, identity, true
}

func (h *Handler) respondError(c *gin.Context, status int, err error, requestID string) {
	var serviceErr *ServiceError
	if !errors.As(err, &serviceErr) {
		serviceErr = serviceError("internal_error", "Shopee gateway ประมวลผลไม่สำเร็จ", status, false, err)
	}
	c.JSON(status, gin.H{"error": gin.H{
		"code":       serviceErr.Code,
		"message":    serviceErr.Message,
		"retryable":  serviceErr.Retryable,
		"request_id": requestID,
	}})
}

func (h *Handler) record(c *gin.Context, identity *gatewayauth.Identity, operation string, status int, started time.Time, errorCode, requestID string) {
	if h.audit != nil && identity != nil {
		duration := int(time.Since(started).Milliseconds())
		if err := h.audit.RecordAPIResult(c.Request.Context(), identity.Tenant, identity.Nonce, operation, status, duration, errorCode, requestID); err != nil {
			h.logger.Warn("shopee_gateway_api_audit_failed", zap.String("tenant", identity.Tenant), zap.String("operation", operation), zap.Error(err))
		}
	}
	h.logger.Info("shopee_gateway_api_request",
		zap.String("tenant", identityTenant(identity)),
		zap.String("operation", operation),
		zap.Int("status_code", status),
		zap.String("error_code", errorCode),
		zap.String("request_id", requestID),
	)
}

func (h *Handler) renderCallback(c *gin.Context, status int, title, message string) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(status, `<!doctype html><html lang="th"><head><meta charset="utf-8"><title>%s</title></head><body><main><h1>%s</h1><p>%s</p><p>กลับไปที่ Nexflow แล้วกดเชื่อมต่อใหม่</p></main></body></html>`, html.EscapeString(title), html.EscapeString(title), html.EscapeString(message))
}

func decodeJSONBody(body []byte, out interface{}) error {
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("request contains trailing JSON")
	}
	return nil
}

func serviceErrorMeta(err error) (int, string) {
	var serviceErr *ServiceError
	if errors.As(err, &serviceErr) {
		status := serviceErr.HTTPStatus
		if status <= 0 {
			status = http.StatusInternalServerError
		}
		return status, serviceErr.Code
	}
	return http.StatusInternalServerError, "internal_error"
}

func identityTenant(identity *gatewayauth.Identity) string {
	if identity == nil {
		return ""
	}
	return identity.Tenant
}

func requestID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b)
}

func hashBody(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
