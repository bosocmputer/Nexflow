package shopeegateway

import (
	"bytes"
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"nexflow/internal/services/shopeeapi"
)

const (
	accessTokenRefreshSkew  = 10 * time.Minute
	refreshTokenTTL         = 30 * 24 * time.Hour
	tenantRequestsPerSecond = 20
)

type Store interface {
	TenantBySlug(ctx context.Context, slug string) (*Tenant, error)
	TenantByID(ctx context.Context, id string) (*Tenant, error)
	CreateOAuthState(ctx context.Context, record OAuthStateRecord) error
	ConsumeOAuthState(ctx context.Context, stateHash string) (*OAuthStateRecord, error)
	Connection(ctx context.Context, tenantSlug string, shopID int64) (*EncryptedConnection, error)
	UpsertConnection(ctx context.Context, conn EncryptedConnection) error
	UpdateConnectionTokens(ctx context.Context, conn EncryptedConnection) error
	EnqueueDelivery(ctx context.Context, tenantID, eventType, dedupeKey string, payload interface{}) error
}

type Service struct {
	cfg         Config
	store       Store
	cipher      *TokenCipher
	state       *OAuthStateSigner
	provider    shopeeapi.APIClient
	logger      *zap.Logger
	now         func() time.Time
	globalSem   chan struct{}
	tenantMu    sync.Mutex
	tenantSems  map[string]chan struct{}
	tenantRates map[string]*tenantRateWindow
	shopLocks   sync.Map
}

type tenantRateWindow struct {
	startedAt time.Time
	count     int
}

type GatewayExecuteRequest struct {
	Operation string          `json:"operation"`
	ShopID    int64           `json:"shop_id"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type OAuthCallbackResult struct {
	TenantSlug string
	ShopID     int64
	ShopName   string
	ReturnURL  string
}

type ServiceError struct {
	Code       string
	Message    string
	HTTPStatus int
	Retryable  bool
	Cause      error
}

func (e *ServiceError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *ServiceError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func NewService(cfg Config, store Store, provider shopeeapi.APIClient, logger *zap.Logger) (*Service, error) {
	if store == nil || provider == nil || !provider.Configured() {
		return nil, errors.New("Shopee gateway dependencies are not configured")
	}
	cipher, err := NewTokenCipher(cfg.TokenEncryptionKey)
	if err != nil {
		return nil, err
	}
	state, err := NewOAuthStateSigner(cfg.OAuthSigningKey)
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Service{
		cfg:         cfg,
		store:       store,
		cipher:      cipher,
		state:       state,
		provider:    provider,
		logger:      logger,
		now:         time.Now,
		globalSem:   make(chan struct{}, 20),
		tenantSems:  map[string]chan struct{}{},
		tenantRates: map[string]*tenantRateWindow{},
	}, nil
}

func (s *Service) CreateAuthURL(ctx context.Context, tenantSlug, userID, returnURL string) (*shopeeapi.GatewayAuthURLResponse, error) {
	tenant, err := s.store.TenantBySlug(ctx, strings.ToLower(strings.TrimSpace(tenantSlug)))
	if err != nil || tenant == nil || !tenant.Enabled {
		return nil, serviceError("tenant_not_found", "ไม่พบ Nexflow tenant ที่เปิดใช้งาน", 404, false, err)
	}
	if err := ValidateTenantReturnURL(tenant.PublicBaseURL, returnURL); err != nil {
		return nil, serviceError("invalid_return_url", "return URL ไม่ตรงกับ domain ของ tenant", 400, false, err)
	}
	state, claims, err := s.state.Create(tenant.Slug, userID, returnURL, s.now())
	if err != nil {
		return nil, serviceError("oauth_state_error", "สร้าง OAuth state ไม่สำเร็จ", 500, false, err)
	}
	if err := s.store.CreateOAuthState(ctx, OAuthStateRecord{
		StateHash:   HashOAuthState(state),
		TenantID:    tenant.ID,
		UserID:      claims.UserID,
		ReturnURL:   claims.ReturnURL,
		Nonce:       claims.Nonce,
		Environment: s.cfg.Environment,
		ExpiresAt:   time.Unix(claims.ExpiresAt, 0),
	}); err != nil {
		return nil, serviceError("oauth_state_store_error", "บันทึก OAuth state ไม่สำเร็จ", 500, false, err)
	}
	callbackURL, err := oauthCallbackURLWithState(s.cfg.OAuthCallbackURL(), state)
	if err != nil {
		return nil, serviceError("oauth_state_error", "เตรียม Shopee OAuth callback ไม่สำเร็จ", 500, false, err)
	}
	// Shopee does not echo the top-level state query parameter on the shop OAuth
	// callback. Carry the same signed, one-time state in redirect as well so the
	// callback can identify a tenant without falling back to a guessed pending row.
	authURL, err := s.provider.AuthURL(callbackURL, state, s.now())
	if err != nil {
		return nil, serviceError("shopee_auth_url_error", "สร้าง Shopee OAuth URL ไม่สำเร็จ", 502, isRetryableShopeeError(err), err)
	}
	return &shopeeapi.GatewayAuthURLResponse{AuthURL: authURL, RedirectURL: s.cfg.OAuthCallbackURL()}, nil
}

func oauthCallbackURLWithState(callbackURL, state string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(callbackURL))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return "", errors.New("OAuth callback URL must be an absolute HTTPS URL")
	}
	if strings.TrimSpace(state) == "" {
		return "", ErrInvalidOAuthState
	}
	query := u.Query()
	query.Set("state", state)
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func (s *Service) CompleteOAuth(ctx context.Context, code, state string, callbackShopID int64) (*OAuthCallbackResult, error) {
	code = strings.TrimSpace(code)
	state = strings.TrimSpace(state)
	if code == "" || state == "" || callbackShopID <= 0 {
		return nil, serviceError("invalid_oauth_callback", "Shopee callback ไม่มี code, state หรือ shop_id ครบ", 400, false, nil)
	}
	claims, err := s.state.Verify(state, s.now())
	if err != nil {
		return nil, serviceError("invalid_oauth_state", "OAuth state ไม่ถูกต้อง หมดอายุ หรือถูกแก้ไข", 400, false, err)
	}
	record, err := s.store.ConsumeOAuthState(ctx, HashOAuthState(state))
	if err != nil {
		return nil, serviceError("oauth_state_used", "OAuth state หมดอายุหรือถูกใช้ไปแล้ว", 409, false, err)
	}
	if !sameOAuthState(record, claims, s.cfg.Environment) {
		return nil, serviceError("oauth_state_mismatch", "OAuth state ไม่ตรงกับ session ที่เริ่มเชื่อมต่อ", 400, false, nil)
	}
	tenant, err := s.store.TenantByID(ctx, record.TenantID)
	if err != nil || tenant == nil || !tenant.Enabled || tenant.Slug != claims.Tenant {
		return nil, serviceError("tenant_not_found", "ไม่พบ Nexflow tenant ที่เปิดใช้งาน", 404, false, err)
	}
	token, err := s.provider.GetToken(ctx, code, callbackShopID)
	if err != nil {
		return nil, serviceError("token_exchange_failed", "แลก Shopee token ไม่สำเร็จ", 502, isRetryableShopeeError(err), err)
	}
	if token == nil || strings.TrimSpace(token.AccessToken) == "" || strings.TrimSpace(token.RefreshToken) == "" {
		return nil, serviceError("invalid_token_response", "Shopee ส่งข้อมูล token ไม่ครบ กรุณาเชื่อมร้านใหม่", 502, false, nil)
	}
	if token.ShopID > 0 && token.ShopID != callbackShopID {
		return nil, serviceError("token_shop_mismatch", "ร้าน Shopee ใน token ไม่ตรงกับ OAuth callback", 409, false, nil)
	}
	shopID := callbackShopID
	if token.ShopID > 0 {
		shopID = token.ShopID
	}
	shopName := s.fetchShopName(ctx, token.AccessToken, shopID)
	accessExpiresAt := s.now().Add(time.Duration(token.ExpireIn) * time.Second)
	if token.ExpireIn <= 0 {
		accessExpiresAt = s.now().Add(4 * time.Hour)
	}
	refreshExpiresAt := s.now().Add(refreshTokenTTL)
	conn, err := s.encryptConnection(tenant, shopID, token.MerchantID, shopName, token.AccessToken, token.RefreshToken, accessExpiresAt, refreshExpiresAt)
	if err != nil {
		return nil, serviceError("token_encryption_failed", "เข้ารหัส Shopee token ไม่สำเร็จ", 500, false, err)
	}
	if err := s.store.UpsertConnection(ctx, conn); err != nil {
		if errors.Is(err, ErrShopAlreadyOwned) {
			return nil, serviceError("shop_owned_by_other_tenant", "ร้าน Shopee นี้เชื่อมกับ Nexflow tenant อื่นอยู่แล้ว", 409, false, err)
		}
		return nil, serviceError("connection_store_failed", "บันทึกการเชื่อมต่อ Shopee ไม่สำเร็จ", 500, false, err)
	}
	stored, err := s.store.Connection(ctx, tenant.Slug, shopID)
	if err != nil {
		return nil, serviceError("connection_store_failed", "อ่านการเชื่อมต่อ Shopee หลังบันทึกไม่สำเร็จ", 500, false, err)
	}
	payload := connectionPayload(stored)
	if err := s.store.EnqueueDelivery(ctx, tenant.ID, "connection_upsert", connectionDeliveryKey(tenant.Slug, shopID, stored.AccessExpiresAt), payload); err != nil {
		return nil, serviceError("connection_delivery_failed", "จัดคิวข้อมูลร้านไปยัง Nexflow tenant ไม่สำเร็จ กรุณาเชื่อมใหม่", 503, true, err)
	}
	return &OAuthCallbackResult{TenantSlug: tenant.Slug, ShopID: shopID, ShopName: shopName, ReturnURL: addOAuthResult(record.ReturnURL, shopID)}, nil
}

func (s *Service) Execute(ctx context.Context, tenantSlug string, req GatewayExecuteRequest) (json.RawMessage, error) {
	operation := strings.ToLower(strings.TrimSpace(req.Operation))
	if req.ShopID <= 0 || operation == "" {
		return nil, serviceError("invalid_request", "operation และ shop_id จำเป็นต้องระบุ", 400, false, nil)
	}
	if !s.allowTenantRequest(tenantSlug) {
		return nil, serviceError("rate_limited", "Shopee gateway จำกัดคำขอชั่วคราว กรุณาลองใหม่", 429, true, nil)
	}
	release, err := s.acquire(ctx, tenantSlug)
	if err != nil {
		return nil, serviceError("gateway_busy", "Shopee gateway กำลังประมวลผลงานจำนวนมาก กรุณาลองใหม่", 503, true, err)
	}
	defer release()

	conn, accessToken, err := s.accessToken(ctx, tenantSlug, req.ShopID, false)
	if err != nil {
		return nil, err
	}
	result, err := s.executeOperationWithRetry(ctx, operation, conn.ShopID, accessToken, req.Payload)
	if err != nil && isShopeeTokenError(err) {
		conn, accessToken, refreshErr := s.accessToken(ctx, tenantSlug, req.ShopID, true)
		if refreshErr != nil {
			return nil, refreshErr
		}
		result, err = s.executeOperationWithRetry(ctx, operation, conn.ShopID, accessToken, req.Payload)
	}
	if err != nil {
		var known *ServiceError
		if errors.As(err, &known) {
			return nil, known
		}
		var businessErr *shopeeapi.BusinessError
		if errors.As(err, &businessErr) {
			s.logger.Warn("shopee_gateway_upstream_error",
				zap.String("tenant", strings.TrimSpace(tenantSlug)),
				zap.String("operation", operation),
				zap.Int64("shop_id", req.ShopID),
				zap.String("upstream_error_code", businessErr.Code),
				zap.String("upstream_request_id", businessErr.RequestID),
			)
		}
		lower := strings.ToLower(err.Error())
		if strings.Contains(lower, "required") || strings.Contains(lower, "invalid operation payload") || strings.Contains(lower, "must contain") {
			return nil, serviceError("invalid_operation_payload", "ข้อมูลสำหรับ Shopee operation ไม่ถูกต้อง", 400, false, err)
		}
		return nil, serviceError(shopeeErrorCode(err), "Shopee API ประมวลผลไม่สำเร็จ", 502, isRetryableShopeeError(err), err)
	}
	body, err := json.Marshal(result)
	if err != nil {
		return nil, serviceError("response_encode_failed", "เตรียมผลลัพธ์ Shopee ไม่สำเร็จ", 500, false, err)
	}
	return body, nil
}

func (s *Service) executeOperationWithRetry(ctx context.Context, operation string, shopID int64, accessToken string, payload json.RawMessage) (interface{}, error) {
	maxAttempts := 1
	if retryableOperation(operation) {
		maxAttempts = 3
	}
	var result interface{}
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		result, err = s.executeOperation(ctx, operation, shopID, accessToken, payload)
		if err == nil || isShopeeTokenError(err) || !isRetryableShopeeError(err) || attempt == maxAttempts-1 {
			return result, err
		}
		delay := time.Duration(1<<attempt)*250*time.Millisecond + time.Duration(rand.IntN(150))*time.Millisecond
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	return result, err
}

func retryableOperation(operation string) bool {
	switch operation {
	case "get_shop_info", "get_shop_profile", "get_order_list", "get_order_detail",
		"get_item_list", "get_item_base_info", "get_model_list", "get_warehouse_detail",
		"get_escrow_list", "get_escrow_detail", "get_shipping_parameter",
		"get_tracking_number", "get_tracking_info", "get_shipping_document_info",
		"get_shipping_document_parameter", "get_shipping_document_result", "download_shipping_document":
		return true
	default:
		return false
	}
}

func (s *Service) allowTenantRequest(tenant string) bool {
	now := s.now()
	s.tenantMu.Lock()
	defer s.tenantMu.Unlock()
	window := s.tenantRates[tenant]
	if window == nil || now.Sub(window.startedAt) >= time.Second {
		s.tenantRates[tenant] = &tenantRateWindow{startedAt: now, count: 1}
		return true
	}
	if window.count >= tenantRequestsPerSecond {
		return false
	}
	window.count++
	return true
}

func (s *Service) executeOperation(ctx context.Context, operation string, shopID int64, accessToken string, payload json.RawMessage) (interface{}, error) {
	switch operation {
	case "get_shop_info":
		return s.provider.GetShopInfo(ctx, accessToken, shopID)
	case "get_shop_profile":
		return s.provider.GetShopProfile(ctx, accessToken, shopID)
	case "get_order_list":
		var req shopeeapi.OrderListRequest
		if err := decodePayload(payload, &req); err != nil {
			return nil, err
		}
		return s.provider.GetOrderList(ctx, accessToken, shopID, req)
	case "get_order_detail":
		var req struct {
			OrderSNs       []string `json:"order_sn_list"`
			OptionalFields []string `json:"optional_fields"`
		}
		if err := decodePayload(payload, &req); err != nil || len(req.OrderSNs) == 0 || len(req.OrderSNs) > 50 {
			return nil, errors.New("order_sn_list must contain 1-50 orders")
		}
		return s.provider.GetOrderDetail(ctx, accessToken, shopID, req.OrderSNs, req.OptionalFields)
	case "get_item_list":
		var req shopeeapi.ItemListRequest
		if err := decodePayload(payload, &req); err != nil {
			return nil, err
		}
		return s.provider.GetItemList(ctx, accessToken, shopID, req)
	case "get_item_base_info":
		var req struct {
			ItemIDs []int64 `json:"item_id_list"`
		}
		if err := decodePayload(payload, &req); err != nil || len(req.ItemIDs) == 0 || len(req.ItemIDs) > 50 {
			return nil, errors.New("item_id_list must contain 1-50 items")
		}
		return s.provider.GetItemBaseInfo(ctx, accessToken, shopID, req.ItemIDs)
	case "get_model_list":
		var req struct {
			ItemID int64 `json:"item_id"`
		}
		if err := decodePayload(payload, &req); err != nil || req.ItemID <= 0 {
			return nil, errors.New("item_id is required")
		}
		return s.provider.GetModelList(ctx, accessToken, shopID, req.ItemID)
	case "get_warehouse_detail":
		if err := decodePayload(payload, &struct{}{}); err != nil {
			return nil, err
		}
		return s.provider.GetWarehouseDetail(ctx, accessToken, shopID)
	case "update_stock":
		var req shopeeapi.UpdateStockRequest
		if err := decodePayload(payload, &req); err != nil {
			return nil, err
		}
		if err := shopeeapi.ValidateUpdateStockRequest(req); err != nil {
			return nil, err
		}
		return s.provider.UpdateStock(ctx, accessToken, shopID, req)
	case "get_escrow_list":
		var req shopeeapi.EscrowListRequest
		if err := decodePayload(payload, &req); err != nil {
			return nil, err
		}
		return s.provider.GetEscrowList(ctx, accessToken, shopID, req)
	case "get_escrow_detail":
		var req orderPackagePayload
		if err := decodePayload(payload, &req); err != nil || req.OrderSN == "" {
			return nil, errors.New("order_sn is required")
		}
		return s.provider.GetEscrowDetail(ctx, accessToken, shopID, req.OrderSN)
	case "get_shipping_parameter":
		var req orderPackagePayload
		if err := decodePayload(payload, &req); err != nil || req.OrderSN == "" {
			return nil, errors.New("order_sn is required")
		}
		return s.provider.GetShippingParameter(ctx, accessToken, shopID, req.OrderSN, req.PackageNumber)
	case "ship_order":
		var req shopeeapi.ShipOrderRequest
		if err := decodePayload(payload, &req); err != nil || strings.TrimSpace(req.OrderSN) == "" {
			return nil, errors.New("valid ship_order payload is required")
		}
		return s.provider.ShipOrder(ctx, accessToken, shopID, req)
	case "get_tracking_number":
		var req orderPackagePayload
		if err := decodePayload(payload, &req); err != nil || req.OrderSN == "" {
			return nil, errors.New("order_sn is required")
		}
		return s.provider.GetTrackingNumber(ctx, accessToken, shopID, req.OrderSN, req.PackageNumber)
	case "get_tracking_info":
		var req orderPackagePayload
		if err := decodePayload(payload, &req); err != nil || req.OrderSN == "" {
			return nil, errors.New("order_sn is required")
		}
		return s.provider.GetTrackingInfo(ctx, accessToken, shopID, req.OrderSN, req.PackageNumber)
	case "get_shipping_document_info":
		var req orderPackagePayload
		if err := decodePayload(payload, &req); err != nil || req.OrderSN == "" {
			return nil, errors.New("order_sn is required")
		}
		return s.provider.GetShippingDocumentInfo(ctx, accessToken, shopID, req.OrderSN, req.PackageNumber)
	case "get_shipping_document_parameter":
		var req orderPackagePayload
		if err := decodePayload(payload, &req); err != nil || req.OrderSN == "" {
			return nil, errors.New("order_sn is required")
		}
		return s.provider.GetShippingDocumentParameter(ctx, accessToken, shopID, req.OrderSN, req.PackageNumber)
	case "create_shipping_document":
		var req shippingDocumentPayload
		if err := decodePayload(payload, &req); err != nil || req.OrderSN == "" || req.DocumentType == "" {
			return nil, errors.New("valid shipping document payload is required")
		}
		return s.provider.CreateShippingDocument(ctx, accessToken, shopID, req.OrderSN, req.PackageNumber, req.DocumentType, req.TrackingNumber)
	case "get_shipping_document_result":
		var req shippingDocumentPayload
		if err := decodePayload(payload, &req); err != nil || req.OrderSN == "" || req.DocumentType == "" {
			return nil, errors.New("valid shipping document payload is required")
		}
		return s.provider.GetShippingDocumentResult(ctx, accessToken, shopID, req.OrderSN, req.PackageNumber, req.DocumentType)
	case "download_shipping_document":
		var req shippingDocumentPayload
		if err := decodePayload(payload, &req); err != nil || req.OrderSN == "" || req.DocumentType == "" {
			return nil, errors.New("valid shipping document payload is required")
		}
		body, contentType, err := s.provider.DownloadShippingDocument(ctx, accessToken, shopID, req.OrderSN, req.PackageNumber, req.DocumentType)
		if err != nil {
			return nil, err
		}
		return map[string]string{"content_type": contentType, "content_base64": encodeBase64(body)}, nil
	default:
		return nil, serviceError("operation_not_allowed", "Shopee operation นี้ไม่ได้รับอนุญาต", 400, false, nil)
	}
}

type orderPackagePayload struct {
	OrderSN       string `json:"order_sn"`
	PackageNumber string `json:"package_number"`
}

type shippingDocumentPayload struct {
	OrderSN        string `json:"order_sn"`
	PackageNumber  string `json:"package_number"`
	DocumentType   string `json:"document_type"`
	TrackingNumber string `json:"tracking_number"`
}

func (s *Service) accessToken(ctx context.Context, tenantSlug string, shopID int64, forceRefresh bool) (*EncryptedConnection, string, error) {
	lockValue, _ := s.shopLocks.LoadOrStore(tenantSlug+":"+strconv.FormatInt(shopID, 10), &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	conn, err := s.store.Connection(ctx, tenantSlug, shopID)
	if err != nil {
		return nil, "", serviceError("connection_not_found", "ไม่พบร้าน Shopee ของ tenant นี้", 404, false, err)
	}
	if conn.DisabledAt.Valid {
		return nil, "", serviceError("connection_disabled", "การเชื่อมต่อร้าน Shopee ถูกปิดใช้งาน", 409, false, nil)
	}
	if !forceRefresh && s.now().Before(conn.AccessExpiresAt.Add(-accessTokenRefreshSkew)) {
		accessToken, err := s.cipher.Decrypt(conn.AccessTokenCipher, conn.AccessTokenNonce, tokenAAD(conn.TenantSlug, conn.ShopID, "access"))
		if err != nil {
			return nil, "", serviceError("token_decryption_failed", "อ่าน Shopee token ไม่สำเร็จ", 500, false, err)
		}
		return conn, accessToken, nil
	}
	if !s.now().Before(conn.RefreshExpiresAt) {
		return nil, "", serviceError("refresh_token_expired", "Shopee refresh token หมดอายุ ต้องเชื่อมร้านใหม่", 409, false, nil)
	}
	refreshToken, err := s.cipher.Decrypt(conn.RefreshTokenCipher, conn.RefreshTokenNonce, tokenAAD(conn.TenantSlug, conn.ShopID, "refresh"))
	if err != nil {
		return nil, "", serviceError("token_decryption_failed", "อ่าน Shopee refresh token ไม่สำเร็จ", 500, false, err)
	}
	token, err := s.provider.RefreshToken(ctx, refreshToken, conn.ShopID)
	if err != nil {
		return nil, "", serviceError("token_refresh_failed", "ต่ออายุ Shopee token ไม่สำเร็จ", 502, isRetryableShopeeError(err), err)
	}
	if token == nil || strings.TrimSpace(token.AccessToken) == "" {
		return nil, "", serviceError("invalid_token_response", "Shopee ส่ง access token ใหม่ไม่ครบ", 502, false, nil)
	}
	conn.AccessExpiresAt = s.now().Add(time.Duration(token.ExpireIn) * time.Second)
	if token.ExpireIn <= 0 {
		conn.AccessExpiresAt = s.now().Add(4 * time.Hour)
	}
	rotatedRefreshToken := strings.TrimSpace(token.RefreshToken)
	if rotatedRefreshToken != "" {
		refreshToken = rotatedRefreshToken
		conn.RefreshExpiresAt = s.now().Add(refreshTokenTTL)
	}
	conn.AccessTokenCipher, conn.AccessTokenNonce, err = s.cipher.Encrypt(token.AccessToken, tokenAAD(conn.TenantSlug, conn.ShopID, "access"))
	if err != nil {
		return nil, "", serviceError("token_encryption_failed", "เข้ารหัส Shopee token ไม่สำเร็จ", 500, false, err)
	}
	conn.RefreshTokenCipher, conn.RefreshTokenNonce, err = s.cipher.Encrypt(refreshToken, tokenAAD(conn.TenantSlug, conn.ShopID, "refresh"))
	if err != nil {
		return nil, "", serviceError("token_encryption_failed", "เข้ารหัส Shopee token ไม่สำเร็จ", 500, false, err)
	}
	if err := s.store.UpdateConnectionTokens(ctx, *conn); err != nil {
		return nil, "", serviceError("token_store_failed", "บันทึก Shopee token ใหม่ไม่สำเร็จ", 500, false, err)
	}
	if err := s.store.EnqueueDelivery(ctx, conn.TenantID, "connection_upsert", connectionDeliveryKey(conn.TenantSlug, conn.ShopID, conn.AccessExpiresAt), connectionPayload(conn)); err != nil {
		s.logger.Warn("shopee_gateway_refresh_delivery_enqueue_failed", zap.String("tenant", conn.TenantSlug), zap.Int64("shop_id", conn.ShopID), zap.Error(err))
	}
	return conn, token.AccessToken, nil
}

func (s *Service) encryptConnection(tenant *Tenant, shopID, merchantID int64, shopName, accessToken, refreshToken string, accessExpiresAt, refreshExpiresAt time.Time) (EncryptedConnection, error) {
	accessCipher, accessNonce, err := s.cipher.Encrypt(accessToken, tokenAAD(tenant.Slug, shopID, "access"))
	if err != nil {
		return EncryptedConnection{}, err
	}
	refreshCipher, refreshNonce, err := s.cipher.Encrypt(refreshToken, tokenAAD(tenant.Slug, shopID, "refresh"))
	if err != nil {
		return EncryptedConnection{}, err
	}
	return EncryptedConnection{
		TenantID:             tenant.ID,
		TenantSlug:           tenant.Slug,
		ShopID:               shopID,
		MerchantID:           sql.NullInt64{Int64: merchantID, Valid: merchantID > 0},
		ShopName:             strings.TrimSpace(shopName),
		Environment:          s.cfg.Environment,
		AccessTokenCipher:    accessCipher,
		AccessTokenNonce:     accessNonce,
		RefreshTokenCipher:   refreshCipher,
		RefreshTokenNonce:    refreshNonce,
		EncryptionKeyVersion: 1,
		AccessExpiresAt:      accessExpiresAt,
		RefreshExpiresAt:     refreshExpiresAt,
	}, nil
}

func (s *Service) fetchShopName(ctx context.Context, accessToken string, shopID int64) string {
	info, err := s.provider.GetShopInfo(ctx, accessToken, shopID)
	if err == nil && info != nil && strings.TrimSpace(info.Response.ShopName) != "" {
		return strings.TrimSpace(info.Response.ShopName)
	}
	profile, profileErr := s.provider.GetShopProfile(ctx, accessToken, shopID)
	if profileErr == nil && profile != nil {
		return strings.TrimSpace(profile.Response.ShopName)
	}
	if err != nil {
		s.logger.Warn("shopee_gateway_shop_lookup_failed", zap.Int64("shop_id", shopID), zap.String("error_code", shopeeErrorCode(err)))
	}
	return ""
}

func (s *Service) acquire(ctx context.Context, tenant string) (func(), error) {
	s.tenantMu.Lock()
	sem := s.tenantSems[tenant]
	if sem == nil {
		sem = make(chan struct{}, 5)
		s.tenantSems[tenant] = sem
	}
	s.tenantMu.Unlock()
	select {
	case s.globalSem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case sem <- struct{}{}:
		return func() { <-sem; <-s.globalSem }, nil
	case <-ctx.Done():
		<-s.globalSem
		return nil, ctx.Err()
	}
}

func decodePayload(raw json.RawMessage, out interface{}) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("invalid operation payload: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("invalid operation payload")
	}
	return nil
}

func sameOAuthState(record *OAuthStateRecord, claims OAuthStateClaims, environment string) bool {
	if record == nil {
		return false
	}
	values := [][2]string{
		{record.UserID, claims.UserID},
		{record.ReturnURL, claims.ReturnURL},
		{record.Nonce, claims.Nonce},
		{record.Environment, environment},
	}
	for _, pair := range values {
		if subtle.ConstantTimeCompare([]byte(pair[0]), []byte(pair[1])) != 1 {
			return false
		}
	}
	return true
}

func connectionPayload(conn *EncryptedConnection) shopeeapi.GatewayConnectionPayload {
	var merchantID *int64
	if conn.MerchantID.Valid {
		value := conn.MerchantID.Int64
		merchantID = &value
	}
	return shopeeapi.GatewayConnectionPayload{
		GatewayConnectionID: conn.ID,
		ShopID:              conn.ShopID,
		MerchantID:          merchantID,
		ShopName:            conn.ShopName,
		Environment:         conn.Environment,
		AccessExpiresAt:     conn.AccessExpiresAt.UTC().Format(time.RFC3339),
		RefreshExpiresAt:    conn.RefreshExpiresAt.UTC().Format(time.RFC3339),
	}
}

func tokenAAD(tenant string, shopID int64, kind string) []byte {
	return []byte(strings.ToLower(strings.TrimSpace(tenant)) + ":" + strconv.FormatInt(shopID, 10) + ":" + kind)
}

func addOAuthResult(raw string, shopID int64) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	query := u.Query()
	query.Set("shopee", "connected")
	query.Set("shop_id", strconv.FormatInt(shopID, 10))
	u.RawQuery = query.Encode()
	return u.String()
}

func connectionDeliveryKey(tenant string, shopID int64, expiresAt time.Time) string {
	return "connection:" + tenant + ":" + strconv.FormatInt(shopID, 10) + ":" + strconv.FormatInt(expiresAt.UTC().Unix(), 10)
}

func serviceError(code, message string, status int, retryable bool, cause error) *ServiceError {
	return &ServiceError{Code: code, Message: message, HTTPStatus: status, Retryable: retryable, Cause: cause}
}

func isShopeeTokenError(err error) bool {
	lower := strings.ToLower(errorString(err))
	return strings.Contains(lower, "access_token") || strings.Contains(lower, "refresh token") || strings.Contains(lower, "error_auth")
}

func isRetryableShopeeError(err error) bool {
	lower := strings.ToLower(errorString(err))
	return strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline") || strings.Contains(lower, "http 429") || strings.Contains(lower, "too many") || strings.Contains(lower, "http 500") || strings.Contains(lower, "http 502") || strings.Contains(lower, "http 503") || strings.Contains(lower, "connection reset")
}

func shopeeErrorCode(err error) string {
	var businessErr *shopeeapi.BusinessError
	if errors.As(err, &businessErr) {
		code := strings.ToLower(strings.TrimSpace(businessErr.Code))
		switch {
		case strings.Contains(code, "source_ip_undeclared"):
			return "source_ip_undeclared"
		case strings.Contains(code, "permission"), strings.Contains(code, "forbidden"), strings.Contains(code, "no_authorization"):
			return "permission_denied"
		case strings.Contains(code, "rate"), strings.Contains(code, "too_many"):
			return "rate_limited"
		case code != "":
			return code
		}
	}
	lower := strings.ToLower(errorString(err))
	switch {
	case isShopeeTokenError(err):
		return "token_error"
	case strings.Contains(lower, "source_ip_undeclared"):
		return "source_ip_undeclared"
	case strings.Contains(lower, "http 429") || strings.Contains(lower, "too many"):
		return "rate_limited"
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline"):
		return "shopee_timeout"
	case strings.Contains(lower, "permission") || strings.Contains(lower, "forbidden"):
		return "permission_denied"
	default:
		return "shopee_api_error"
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func encodeBase64(body []byte) string {
	return base64.StdEncoding.EncodeToString(body)
}
