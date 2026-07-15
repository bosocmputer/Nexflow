package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/config"
	"nexflow/internal/services/gatewayauth"
	"nexflow/internal/services/shopeeapi"
	"nexflow/internal/services/shopeepush"
)

const gatewayInternalBodyLimit = 2 << 20

type ShopeeGatewayInternalHandler struct {
	db       *sql.DB
	cfg      *config.Config
	realtime *ShopeeRealtimeHandler
	verify   gatewayauth.Verifier
	logger   *zap.Logger
}

type tenantGatewayNonceStore struct{ db *sql.DB }

func (s tenantGatewayNonceStore) Consume(ctx context.Context, tenant, nonce string, expiresAt time.Time) error {
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO shopee_gateway_request_nonces (tenant_slug, nonce, expires_at)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (tenant_slug, nonce) DO NOTHING`, tenant, nonce, expiresAt)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return gatewayauth.ErrReplay
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM shopee_gateway_request_nonces WHERE expires_at < NOW() - INTERVAL '1 day'`)
	return nil
}

func NewShopeeGatewayInternalHandler(db *sql.DB, cfg *config.Config, realtime *ShopeeRealtimeHandler, logger *zap.Logger) *ShopeeGatewayInternalHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	handler := &ShopeeGatewayInternalHandler{db: db, cfg: cfg, realtime: realtime, logger: logger}
	handler.verify = gatewayauth.Verifier{
		ResolveSecret: func(_ context.Context, tenant string) (string, error) {
			if cfg == nil || !strings.EqualFold(strings.TrimSpace(tenant), strings.TrimSpace(cfg.ShopeeGatewayTenant)) || strings.TrimSpace(cfg.ShopeeGatewayInternalSecret) == "" {
				return "", gatewayauth.ErrInvalidSignature
			}
			return strings.TrimSpace(cfg.ShopeeGatewayInternalSecret), nil
		},
		Nonces:  tenantGatewayNonceStore{db: db},
		MaxSkew: 5 * time.Minute,
	}
	return handler
}

func (h *ShopeeGatewayInternalHandler) Register(r *gin.Engine) {
	r.POST("/internal/v1/shopee-gateway/connections/upsert", h.UpsertConnection)
	r.POST("/internal/v1/shopee-gateway/push", h.Push)
	r.POST(shopeeapi.GatewayTenantRoutesPath, h.Routes)
}

func (h *ShopeeGatewayInternalHandler) UpsertConnection(c *gin.Context) {
	body, ok := h.authenticate(c, true)
	if !ok {
		return
	}
	var payload shopeeapi.GatewayConnectionPayload
	if err := strictDecode(body, &payload); err != nil || payload.ShopID <= 0 || strings.TrimSpace(payload.GatewayConnectionID) == "" {
		h.error(c, http.StatusBadRequest, "invalid_connection", "ข้อมูลร้านจาก gateway ไม่ถูกต้อง")
		return
	}
	accessExpiry, err := time.Parse(time.RFC3339, payload.AccessExpiresAt)
	if err != nil {
		h.error(c, http.StatusBadRequest, "invalid_connection", "เวลา access token ไม่ถูกต้อง")
		return
	}
	refreshExpiry, err := time.Parse(time.RFC3339, payload.RefreshExpiresAt)
	if err != nil {
		h.error(c, http.StatusBadRequest, "invalid_connection", "เวลา refresh token ไม่ถูกต้อง")
		return
	}
	if !strings.EqualFold(strings.TrimSpace(payload.Environment), defaultShopeeAPIEnv(h.cfg.ShopeeOpenAPIEnv)) {
		h.error(c, http.StatusConflict, "environment_mismatch", "environment ของ gateway ไม่ตรงกับ tenant")
		return
	}
	merchantID := int64(0)
	if payload.MerchantID != nil {
		merchantID = *payload.MerchantID
	}
	shopName := strings.TrimSpace(payload.ShopName)
	label := shopName
	if label == "" {
		label = defaultShopeeShopLabel(payload.ShopID)
	}
	_, err = h.db.ExecContext(c.Request.Context(),
		`INSERT INTO shopee_api_connections
		   (shop_id, merchant_id, shop_name, label, access_token, refresh_token,
		    access_expires_at, refresh_expires_at, environment, credential_mode,
		    gateway_connection_id, gateway_token_state, gateway_access_expires_at,
		    gateway_refresh_expires_at)
		 VALUES ($1, NULLIF($2, 0), $3, $4, '__gateway_managed__', '__gateway_managed__',
		         $5, $6, $7, 'gateway', $8::uuid, 'managed', $5, $6)
		 ON CONFLICT (shop_id) DO UPDATE
		    SET merchant_id = COALESCE(EXCLUDED.merchant_id, shopee_api_connections.merchant_id),
		        shop_name = COALESCE(NULLIF(EXCLUDED.shop_name, ''), shopee_api_connections.shop_name),
		        label = CASE
		          WHEN shopee_api_connections.label = '' OR shopee_api_connections.label = $9
		          THEN EXCLUDED.label ELSE shopee_api_connections.label END,
		        environment = EXCLUDED.environment,
		        credential_mode = 'gateway',
		        gateway_connection_id = EXCLUDED.gateway_connection_id,
		        gateway_token_state = 'managed',
		        gateway_access_expires_at = EXCLUDED.gateway_access_expires_at,
		        gateway_refresh_expires_at = EXCLUDED.gateway_refresh_expires_at,
		        disabled_at = NULL,
		        connected_at = NOW(), updated_at = NOW(),
		        last_sync_status = '', last_sync_error = '', last_error_code = ''
		  WHERE shopee_api_connections.credential_mode <> 'gateway'
		     OR COALESCE(shopee_api_connections.gateway_access_expires_at, '-infinity'::timestamptz) <= EXCLUDED.gateway_access_expires_at`,
		payload.ShopID, merchantID, shopName, label, accessExpiry, refreshExpiry,
		defaultShopeeAPIEnv(payload.Environment), payload.GatewayConnectionID, defaultShopeeShopLabel(payload.ShopID),
	)
	if err != nil {
		h.logger.Warn("shopee_gateway_connection_upsert_failed", zap.Int64("shop_id", payload.ShopID), zap.Error(err))
		h.error(c, http.StatusInternalServerError, "connection_store_failed", "บันทึกข้อมูลร้าน Shopee ไม่สำเร็จ")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *ShopeeGatewayInternalHandler) Push(c *gin.Context) {
	// Signed gateway push remains available in direct mode so tenants that have
	// not moved their API credentials yet still receive the app-wide callback.
	body, ok := h.authenticate(c, false)
	if !ok {
		return
	}
	if h.realtime == nil {
		h.error(c, http.StatusServiceUnavailable, "push_ingest_unavailable", "ระบบรับ Shopee push ของ tenant ยังไม่พร้อม")
		return
	}
	var payload shopeeapi.GatewayPushDelivery
	if err := strictDecode(body, &payload); err != nil || payload.ShopID <= 0 || !json.Valid(payload.RawPayload) {
		h.error(c, http.StatusBadRequest, "invalid_push", "ข้อมูล push จาก gateway ไม่ถูกต้อง")
		return
	}
	event, err := shopeepush.Parse(payload.RawPayload)
	if err != nil || event.ShopID != payload.ShopID {
		h.error(c, http.StatusBadRequest, "push_shop_mismatch", "shop_id ใน push ไม่ตรงกัน")
		return
	}
	result, err := h.realtime.IngestGatewayPush(c.Request.Context(), payload.RawPayload)
	if err != nil {
		h.logger.Warn("shopee_gateway_push_ingest_failed", zap.Int64("shop_id", payload.ShopID), zap.Error(err))
		h.error(c, http.StatusServiceUnavailable, "push_ingest_failed", "บันทึก Shopee push ไม่สำเร็จ")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "queued": result.Queued})
}

func (h *ShopeeGatewayInternalHandler) Routes(c *gin.Context) {
	_, ok := h.authenticate(c, false)
	if !ok {
		return
	}
	rows, err := h.db.QueryContext(c.Request.Context(),
		`SELECT DISTINCT shop_id
		   FROM shopee_api_connections
		  WHERE shop_id > 0
		    AND environment = $1
		    AND disabled_at IS NULL
		  ORDER BY shop_id
		  LIMIT 1001`,
		defaultShopeeAPIEnv(h.cfg.ShopeeOpenAPIEnv),
	)
	if err != nil {
		h.logger.Warn("shopee_gateway_routes_query_failed", zap.Error(err))
		h.error(c, http.StatusInternalServerError, "routes_query_failed", "อ่านเส้นทางร้าน Shopee ไม่สำเร็จ")
		return
	}
	defer rows.Close()
	shopIDs := make([]int64, 0)
	for rows.Next() {
		var shopID int64
		if err := rows.Scan(&shopID); err != nil {
			h.error(c, http.StatusInternalServerError, "routes_query_failed", "อ่านเส้นทางร้าน Shopee ไม่สำเร็จ")
			return
		}
		shopIDs = append(shopIDs, shopID)
	}
	if err := rows.Err(); err != nil {
		h.error(c, http.StatusInternalServerError, "routes_query_failed", "อ่านเส้นทางร้าน Shopee ไม่สำเร็จ")
		return
	}
	if len(shopIDs) > 1000 {
		h.error(c, http.StatusConflict, "route_limit_exceeded", "จำนวนร้าน Shopee ของ tenant เกินขีดจำกัด")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": shopeeapi.GatewayTenantRoutesResponse{ShopIDs: shopIDs}})
}

func (h *ShopeeGatewayInternalHandler) authenticate(c *gin.Context, requireGatewayMode bool) ([]byte, bool) {
	if h == nil || h.db == nil || h.cfg == nil || strings.TrimSpace(h.cfg.ShopeeGatewayTenant) == "" || strings.TrimSpace(h.cfg.ShopeeGatewayInternalSecret) == "" {
		h.error(c, http.StatusNotFound, "gateway_identity_disabled", "tenant นี้ยังไม่ได้ตั้งค่า Shopee gateway identity")
		return nil, false
	}
	if requireGatewayMode && !strings.EqualFold(strings.TrimSpace(h.cfg.ShopeeOpenAPIMode), "gateway") {
		h.error(c, http.StatusNotFound, "gateway_mode_disabled", "tenant นี้ยังไม่ได้เปิด Shopee gateway mode")
		return nil, false
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, gatewayInternalBodyLimit+1))
	if err != nil || len(body) > gatewayInternalBodyLimit {
		h.error(c, http.StatusRequestEntityTooLarge, "request_too_large", "request มีขนาดใหญ่เกินกำหนด")
		return nil, false
	}
	if _, err := h.verify.Verify(c.Request.Context(), c.Request, body); err != nil {
		status := http.StatusUnauthorized
		code := "invalid_internal_auth"
		if errors.Is(err, gatewayauth.ErrReplay) {
			status = http.StatusConflict
			code = "replayed_request"
		}
		h.error(c, status, code, "ยืนยัน Shopee gateway ไม่สำเร็จ")
		return nil, false
	}
	return body, true
}

func (h *ShopeeGatewayInternalHandler) error(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}

func strictDecode(body []byte, output interface{}) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}
