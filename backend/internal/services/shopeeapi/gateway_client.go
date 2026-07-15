package shopeeapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"nexflow/internal/services/gatewayauth"
)

const (
	GatewayExecutePath = "/internal/v1/shopee/execute"
	GatewayOAuthPath   = "/internal/v1/shopee/oauth/auth-url"
)

var ErrGatewayManagedCredentials = errors.New("Shopee credentials are managed by the gateway")

type GatewayConfig struct {
	BaseURL      string
	Tenant       string
	SharedSecret string
	HTTPClient   *http.Client
	Now          func() time.Time
}

type GatewayClient struct {
	baseURL      string
	tenant       string
	sharedSecret string
	httpClient   *http.Client
	now          func() time.Time
}

type gatewayExecuteRequest struct {
	Operation string          `json:"operation"`
	ShopID    int64           `json:"shop_id"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type gatewayResponse struct {
	Data  json.RawMessage `json:"data"`
	Error *GatewayError   `json:"error,omitempty"`
}

type GatewayError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

func (e *GatewayError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code == "" {
		return e.Message
	}
	return fmt.Sprintf("shopee gateway %s: %s", e.Code, e.Message)
}

type GatewayAuthURLRequest struct {
	UserID    string `json:"user_id"`
	ReturnURL string `json:"return_url"`
}

type GatewayAuthURLResponse struct {
	AuthURL     string `json:"auth_url"`
	RedirectURL string `json:"redirect_url"`
}

type gatewayDownloadResponse struct {
	ContentType string `json:"content_type"`
	Content     string `json:"content_base64"`
}

func NewGateway(cfg GatewayConfig) *GatewayClient {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &GatewayClient{
		baseURL:      strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		tenant:       strings.TrimSpace(cfg.Tenant),
		sharedSecret: strings.TrimSpace(cfg.SharedSecret),
		httpClient:   httpClient,
		now:          now,
	}
}

func (c *GatewayClient) Configured() bool {
	return c != nil && c.baseURL != "" && c.tenant != "" && c.sharedSecret != ""
}

func (c *GatewayClient) CreateAuthURL(ctx context.Context, req GatewayAuthURLRequest) (*GatewayAuthURLResponse, error) {
	var out GatewayAuthURLResponse
	if err := c.call(ctx, GatewayOAuthPath, req, &out); err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.AuthURL) == "" {
		return nil, errors.New("shopee gateway returned an empty auth URL")
	}
	return &out, nil
}

func (c *GatewayClient) AuthURL(string, string, time.Time) (string, error) {
	return "", ErrGatewayManagedCredentials
}

func (c *GatewayClient) GetToken(context.Context, string, int64) (*TokenResponse, error) {
	return nil, ErrGatewayManagedCredentials
}

func (c *GatewayClient) RefreshToken(context.Context, string, int64) (*TokenResponse, error) {
	return nil, ErrGatewayManagedCredentials
}

func (c *GatewayClient) GetShopInfo(ctx context.Context, _ string, shopID int64) (*ShopInfoResponse, error) {
	var out ShopInfoResponse
	return &out, c.execute(ctx, "get_shop_info", shopID, nil, &out)
}

func (c *GatewayClient) GetShopProfile(ctx context.Context, _ string, shopID int64) (*ShopProfileResponse, error) {
	var out ShopProfileResponse
	return &out, c.execute(ctx, "get_shop_profile", shopID, nil, &out)
}

func (c *GatewayClient) GetOrderList(ctx context.Context, _ string, shopID int64, req OrderListRequest) (*OrderListResponse, error) {
	var out OrderListResponse
	return &out, c.execute(ctx, "get_order_list", shopID, req, &out)
}

func (c *GatewayClient) GetEscrowList(ctx context.Context, _ string, shopID int64, req EscrowListRequest) (*EscrowListResponse, error) {
	var out EscrowListResponse
	return &out, c.execute(ctx, "get_escrow_list", shopID, req, &out)
}

func (c *GatewayClient) GetEscrowDetail(ctx context.Context, _ string, shopID int64, orderSN string) (*EscrowDetailResponse, error) {
	var out EscrowDetailResponse
	return &out, c.execute(ctx, "get_escrow_detail", shopID, map[string]string{"order_sn": orderSN}, &out)
}

func (c *GatewayClient) GetOrderDetail(ctx context.Context, _ string, shopID int64, orderSNs []string, optionalFields []string) (*OrderDetailResponse, error) {
	var out OrderDetailResponse
	payload := map[string]interface{}{"order_sn_list": orderSNs, "optional_fields": optionalFields}
	return &out, c.execute(ctx, "get_order_detail", shopID, payload, &out)
}

func (c *GatewayClient) GetShippingParameter(ctx context.Context, _ string, shopID int64, orderSN, packageNumber string) (*ShippingParameterResponse, error) {
	var out ShippingParameterResponse
	payload := map[string]string{"order_sn": orderSN, "package_number": packageNumber}
	return &out, c.execute(ctx, "get_shipping_parameter", shopID, payload, &out)
}

func (c *GatewayClient) ShipOrder(ctx context.Context, _ string, shopID int64, req ShipOrderRequest) (*ShipOrderResponse, error) {
	var out ShipOrderResponse
	return &out, c.execute(ctx, "ship_order", shopID, req, &out)
}

func (c *GatewayClient) GetTrackingNumber(ctx context.Context, _ string, shopID int64, orderSN, packageNumber string) (*TrackingNumberResponse, error) {
	var out TrackingNumberResponse
	payload := map[string]string{"order_sn": orderSN, "package_number": packageNumber}
	return &out, c.execute(ctx, "get_tracking_number", shopID, payload, &out)
}

func (c *GatewayClient) GetTrackingInfo(ctx context.Context, _ string, shopID int64, orderSN, packageNumber string) (*TrackingInfoResponse, error) {
	var out TrackingInfoResponse
	payload := map[string]string{"order_sn": orderSN, "package_number": packageNumber}
	return &out, c.execute(ctx, "get_tracking_info", shopID, payload, &out)
}

func (c *GatewayClient) GetShippingDocumentInfo(ctx context.Context, _ string, shopID int64, orderSN, packageNumber string) (*ShippingDocumentInfoResponse, error) {
	var out ShippingDocumentInfoResponse
	payload := map[string]string{"order_sn": orderSN, "package_number": packageNumber}
	return &out, c.execute(ctx, "get_shipping_document_info", shopID, payload, &out)
}

func (c *GatewayClient) GetShippingDocumentParameter(ctx context.Context, _ string, shopID int64, orderSN, packageNumber string) (*ShippingDocumentResponse, error) {
	var out ShippingDocumentResponse
	payload := map[string]string{"order_sn": orderSN, "package_number": packageNumber}
	return &out, c.execute(ctx, "get_shipping_document_parameter", shopID, payload, &out)
}

func (c *GatewayClient) CreateShippingDocument(ctx context.Context, _ string, shopID int64, orderSN, packageNumber, documentType, trackingNumber string) (*ShippingDocumentResponse, error) {
	var out ShippingDocumentResponse
	payload := map[string]string{"order_sn": orderSN, "package_number": packageNumber, "document_type": documentType, "tracking_number": trackingNumber}
	return &out, c.execute(ctx, "create_shipping_document", shopID, payload, &out)
}

func (c *GatewayClient) GetShippingDocumentResult(ctx context.Context, _ string, shopID int64, orderSN, packageNumber, documentType string) (*ShippingDocumentResponse, error) {
	var out ShippingDocumentResponse
	payload := map[string]string{"order_sn": orderSN, "package_number": packageNumber, "document_type": documentType}
	return &out, c.execute(ctx, "get_shipping_document_result", shopID, payload, &out)
}

func (c *GatewayClient) DownloadShippingDocument(ctx context.Context, _ string, shopID int64, orderSN, packageNumber, documentType string) ([]byte, string, error) {
	var out gatewayDownloadResponse
	payload := map[string]string{"order_sn": orderSN, "package_number": packageNumber, "document_type": documentType}
	if err := c.execute(ctx, "download_shipping_document", shopID, payload, &out); err != nil {
		return nil, "", err
	}
	b, err := base64.StdEncoding.DecodeString(out.Content)
	if err != nil {
		return nil, "", fmt.Errorf("decode gateway shipping document: %w", err)
	}
	return b, out.ContentType, nil
}

func (c *GatewayClient) execute(ctx context.Context, operation string, shopID int64, payload interface{}, out interface{}) error {
	if shopID <= 0 {
		return errors.New("shop_id is required")
	}
	payloadRaw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if payload == nil {
		payloadRaw = nil
	}
	return c.call(ctx, GatewayExecutePath, gatewayExecuteRequest{Operation: operation, ShopID: shopID, Payload: payloadRaw}, out)
}

func (c *GatewayClient) call(ctx context.Context, path string, payload interface{}, out interface{}) error {
	if !c.Configured() {
		return errors.New("shopee gateway is not configured")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if err := gatewayauth.Apply(req, c.tenant, c.sharedSecret, body, c.now(), ""); err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	var envelope gatewayResponse
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return fmt.Errorf("decode shopee gateway response: %w", err)
	}
	if envelope.Error != nil {
		return envelope.Error
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("shopee gateway http %d", resp.StatusCode)
	}
	if out == nil || len(envelope.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return fmt.Errorf("decode shopee gateway data: %w", err)
	}
	return nil
}

var _ APIClient = (*GatewayClient)(nil)
