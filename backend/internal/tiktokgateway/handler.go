package tiktokgateway

import (
	"context"
	"crypto/rand"
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
)

const (
	GatewayOAuthPath       = "/internal/v1/tiktok-shop/oauth/auth-url"
	maxInternalRequestSize = 1 << 20
)

type OAuthGatewayService interface {
	BeginAuthorization(context.Context, string, string, string) (*AuthorizationStart, error)
	CompleteAuthorization(context.Context, string, string, string) (*AuthorizationResult, error)
}

type InternalRequestVerifier interface {
	Verify(context.Context, *http.Request, []byte) (*gatewayauth.Identity, error)
}

type APILogRecorder interface {
	RecordAPIResult(context.Context, string, string, string, int, int, string, string) error
}

type Handler struct {
	service  OAuthGatewayService
	verifier InternalRequestVerifier
	audit    APILogRecorder
	config   Config
	logger   *zap.Logger
}

type authURLRequest struct {
	UserID    string `json:"user_id"`
	ReturnURL string `json:"return_url"`
}

func NewHandler(service OAuthGatewayService, verifier InternalRequestVerifier, audit APILogRecorder, config Config, logger *zap.Logger) *Handler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Handler{service: service, verifier: verifier, audit: audit, config: config, logger: logger}
}

func (h *Handler) Register(router *gin.Engine) {
	router.GET("/health", h.Health)
	router.GET("/api/tiktok-shop/callback", h.OAuthCallback)
	router.POST(GatewayOAuthPath, h.CreateAuthURL)
}

func (h *Handler) Health(c *gin.Context) {
	if checker, ok := h.audit.(interface{ Ping(context.Context) error }); ok {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := checker.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "service": "nexflow-tiktok-shop-gateway", "database": "unavailable"})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "nexflow-tiktok-shop-gateway", "database": "ok"})
}

func (h *Handler) CreateAuthURL(c *gin.Context) {
	body, identity, ok := h.authenticate(c)
	if !ok {
		return
	}
	startedAt := time.Now()
	requestID := newRequestID()
	statusCode, errorCode := http.StatusOK, ""
	defer func() { h.record(c, identity, "oauth_auth_url", statusCode, startedAt, errorCode, requestID) }()

	var input authURLRequest
	if err := decodeStrictJSON(body, &input); err != nil || strings.TrimSpace(input.UserID) == "" || strings.TrimSpace(input.ReturnURL) == "" {
		statusCode, errorCode = http.StatusBadRequest, "invalid_request"
		h.respondError(c, statusCode, errorCode, "กรุณาระบุผู้ใช้และหน้าที่จะกลับใน Nexflow", false, requestID)
		return
	}
	result, err := h.service.BeginAuthorization(c.Request.Context(), identity.Tenant, input.UserID, input.ReturnURL)
	if err != nil {
		statusCode, errorCode = oauthErrorMeta(err)
		h.respondError(c, statusCode, errorCode, oauthErrorMessage(errorCode), false, requestID)
		return
	}
	if result == nil || strings.TrimSpace(result.AuthorizationURL) == "" {
		statusCode, errorCode = http.StatusInternalServerError, "internal_error"
		h.respondError(c, statusCode, errorCode, oauthErrorMessage(errorCode), false, requestID)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"auth_url": result.AuthorizationURL, "redirect_url": h.config.OAuthCallbackURL(), "expires_at": result.ExpiresAt.UTC().Format(time.RFC3339),
	}})
}

func (h *Handler) OAuthCallback(c *gin.Context) {
	if h == nil || h.service == nil {
		h.renderCallback(c, http.StatusServiceUnavailable, "เชื่อมต่อ TikTok Shop ไม่สำเร็จ", "Gateway ยังไม่พร้อมใช้งาน กรุณาลองใหม่ภายหลัง")
		return
	}
	result, err := h.service.CompleteAuthorization(
		c.Request.Context(), strings.TrimSpace(c.Query("code")), strings.TrimSpace(c.Query("state")), strings.TrimSpace(c.Query("error")),
	)
	if err != nil {
		status, code := oauthErrorMeta(err)
		h.logger.Warn("tiktok_gateway_oauth_callback_failed", zap.String("error_code", code))
		h.renderCallback(c, status, "เชื่อมต่อ TikTok Shop ไม่สำเร็จ", oauthErrorMessage(code))
		return
	}
	if result == nil || strings.TrimSpace(result.ReturnURL) == "" {
		h.renderCallback(c, http.StatusInternalServerError, "เชื่อมต่อ TikTok Shop ไม่สำเร็จ", "Gateway ไม่สามารถยืนยันหน้าปลายทางได้")
		return
	}
	h.logger.Info("tiktok_gateway_oauth_connected", zap.String("tenant", result.TenantSlug), zap.Int("shop_count", len(result.Shops)))
	c.Redirect(http.StatusSeeOther, result.ReturnURL)
}

func (h *Handler) authenticate(c *gin.Context) ([]byte, *gatewayauth.Identity, bool) {
	if h == nil || h.service == nil || h.verifier == nil {
		h.respondError(c, http.StatusServiceUnavailable, "gateway_not_ready", "TikTok Shop Gateway ยังไม่พร้อมใช้งาน", true, newRequestID())
		return nil, nil, false
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxInternalRequestSize+1))
	if err != nil || len(body) > maxInternalRequestSize {
		h.respondError(c, http.StatusRequestEntityTooLarge, "request_too_large", "คำขอมีขนาดใหญ่เกินกำหนด", false, newRequestID())
		return nil, nil, false
	}
	identity, err := h.verifier.Verify(c.Request.Context(), c.Request, body)
	if err != nil {
		status, code := http.StatusUnauthorized, "invalid_internal_auth"
		if errors.Is(err, gatewayauth.ErrReplay) {
			status, code = http.StatusConflict, "replayed_request"
		}
		h.respondError(c, status, code, "ยืนยันตัวตนระหว่าง Nexflow และ Gateway ไม่สำเร็จ", false, newRequestID())
		return nil, nil, false
	}
	return body, identity, true
}

func (h *Handler) respondError(c *gin.Context, status int, code, message string, retryable bool, requestID string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message, "retryable": retryable, "request_id": requestID}})
}

func (h *Handler) record(c *gin.Context, identity *gatewayauth.Identity, operation string, status int, started time.Time, errorCode, requestID string) {
	if h.audit != nil && identity != nil {
		if err := h.audit.RecordAPIResult(c.Request.Context(), identity.Tenant, identity.Nonce, operation, status, int(time.Since(started).Milliseconds()), errorCode, requestID); err != nil {
			h.logger.Warn("tiktok_gateway_api_audit_failed", zap.String("tenant", identity.Tenant), zap.Error(err))
		}
	}
	h.logger.Info("tiktok_gateway_api_request", zap.String("tenant", identityTenant(identity)), zap.String("operation", operation), zap.Int("status_code", status), zap.String("error_code", errorCode), zap.String("request_id", requestID))
}

func (h *Handler) renderCallback(c *gin.Context, status int, title, message string) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(status, `<!doctype html><html lang="th"><head><meta charset="utf-8"><title>%s</title></head><body><main><h1>%s</h1><p>%s</p><p>กลับไปที่ Nexflow แล้วเริ่มเชื่อมต่อใหม่ได้</p></main></body></html>`, html.EscapeString(title), html.EscapeString(title), html.EscapeString(message))
}

func oauthErrorMeta(err error) (int, string) {
	switch {
	case errors.Is(err, ErrAuthorizationDenied):
		return http.StatusBadRequest, "authorization_denied"
	case errors.Is(err, ErrInvalidOAuthCallback):
		return http.StatusBadRequest, "invalid_oauth_callback"
	case errors.Is(err, ErrInvalidOAuthRequest):
		return http.StatusBadRequest, "invalid_request"
	case errors.Is(err, ErrRequiredScopeMissing):
		return http.StatusBadRequest, "required_scope_missing"
	case errors.Is(err, ErrUnexpectedSellerUser):
		return http.StatusBadRequest, "seller_account_required"
	case errors.Is(err, ErrTenantNotAvailable):
		return http.StatusNotFound, "tenant_not_available"
	case errors.Is(err, ErrOAuthServiceNotConfigured):
		return http.StatusServiceUnavailable, "gateway_not_ready"
	default:
		return http.StatusInternalServerError, "internal_error"
	}
}

func oauthErrorMessage(code string) string {
	switch code {
	case "authorization_denied":
		return "ร้านค้าไม่ได้อนุญาตให้ Nexflow เชื่อมต่อ"
	case "invalid_oauth_callback":
		return "ลิงก์เชื่อมต่อหมดอายุหรือถูกใช้งานแล้ว กรุณาเริ่มใหม่จาก Nexflow"
	case "invalid_request":
		return "ข้อมูลเริ่มเชื่อมต่อไม่ถูกต้อง"
	case "required_scope_missing":
		return "แอปยังไม่ได้รับสิทธิ์อ่านข้อมูลร้านและคำสั่งซื้อที่จำเป็น"
	case "seller_account_required":
		return "กรุณาอนุญาตด้วยบัญชีผู้ขาย TikTok Shop"
	case "tenant_not_available":
		return "ไม่พบระบบ Nexflow ของร้านนี้หรือยังไม่ได้เปิดใช้งาน"
	case "gateway_not_ready":
		return "TikTok Shop Gateway ยังไม่พร้อมใช้งาน"
	default:
		return "Gateway ประมวลผลไม่สำเร็จ กรุณาลองใหม่ภายหลัง"
	}
}

func decodeStrictJSON(body []byte, output any) error {
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("request contains trailing JSON")
	}
	return nil
}

func identityTenant(identity *gatewayauth.Identity) string {
	if identity == nil {
		return ""
	}
	return identity.Tenant
}

func newRequestID() string {
	body := make([]byte, 12)
	if _, err := rand.Read(body); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(body)
}
