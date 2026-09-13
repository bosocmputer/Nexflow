package tiktokshop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"nexflow/internal/services/gatewayauth"
)

const (
	GatewayOAuthPath             = "/internal/v1/tiktok-shop/oauth/auth-url"
	GatewayConnectionsPath       = "/internal/v1/tiktok-shop/connections"
	GatewayOrderSearchPath       = "/internal/v1/tiktok-shop/orders/search"
	GatewayOrderDetailsPath      = "/internal/v1/tiktok-shop/orders/detail"
	GatewayShipmentRecipientPath = "/internal/v1/tiktok-shop/orders/shipment-recipient"
	GatewayOrderPriceDetailPath  = "/internal/v1/tiktok-shop/orders/price-detail"
	GatewayWebhookDeliveryPath   = "/internal/v1/tiktok-shop/webhooks/order-status"
	GatewayWebhookConfigurePath  = "/internal/v1/tiktok-shop/webhooks/order-status/configure"
	GatewayProductSearchPath     = "/internal/v1/tiktok-shop/products/search"
	GatewayProductDetailPath     = "/internal/v1/tiktok-shop/products/detail"
	GatewayInventorySearchPath   = "/internal/v1/tiktok-shop/inventory/search"
	GatewayInventoryUpdatePath   = "/internal/v1/tiktok-shop/inventory/update"
	maxGatewayResponseSize       = 8 << 20
)

var (
	ErrGatewayNotConfigured = errors.New("TikTok Shop gateway is not configured")
	ErrInvalidGatewayInput  = errors.New("invalid TikTok Shop gateway input")
)

type GatewayClientConfig struct {
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

type GatewayAuthURLRequest struct {
	UserID    string `json:"user_id"`
	ReturnURL string `json:"return_url"`
}

type GatewayAuthURLResponse struct {
	AuthURL     string `json:"auth_url"`
	RedirectURL string `json:"redirect_url"`
	ExpiresAt   string `json:"expires_at"`
}

type GatewayConnection struct {
	GatewayConnectionID string   `json:"gateway_connection_id"`
	ShopID              string   `json:"shop_id"`
	ShopName            string   `json:"shop_name"`
	ShopRegion          string   `json:"shop_region"`
	SellerType          string   `json:"seller_type"`
	ShopCode            string   `json:"shop_code"`
	GrantedScopes       []string `json:"granted_scopes"`
	AccessExpiresAt     string   `json:"access_expires_at"`
	RefreshExpiresAt    string   `json:"refresh_expires_at"`
	Disabled            bool     `json:"disabled"`
	ConnectedAt         string   `json:"connected_at"`
	UpdatedAt           string   `json:"updated_at"`
}

type GatewayWebhookDelivery struct {
	GatewayEventID string `json:"gateway_event_id"`
	NotificationID string `json:"tts_notification_id"`
	ShopID         string `json:"shop_id"`
	OrderID        string `json:"order_id"`
	OrderStatus    string `json:"order_status"`
	Timestamp      string `json:"timestamp"`
	OrderUpdateAt  string `json:"order_update_at"`
}

type GatewayOrderSearchRequest struct {
	ShopID string              `json:"shop_id"`
	Search SearchOrdersRequest `json:"search"`
}

type GatewayOrderDetailsRequest struct {
	ShopID   string   `json:"shop_id"`
	OrderIDs []string `json:"order_ids"`
}

type GatewayOrderPriceDetailRequest struct {
	ShopID  string `json:"shop_id"`
	OrderID string `json:"order_id"`
}

type GatewayShipmentRecipientRequest struct {
	ShopID  string `json:"shop_id"`
	OrderID string `json:"order_id"`
}

type GatewayProductSearchRequest struct {
	ShopID string                `json:"shop_id"`
	Search SearchProductsRequest `json:"search"`
}

type GatewayProductDetailRequest struct {
	ShopID    string `json:"shop_id"`
	ProductID string `json:"product_id"`
}

type GatewayInventorySearchRequest struct {
	ShopID string                 `json:"shop_id"`
	Search InventorySearchRequest `json:"search"`
}

type GatewayInventoryUpdateRequest struct {
	ShopID    string                 `json:"shop_id"`
	ProductID string                 `json:"product_id"`
	Update    UpdateInventoryRequest `json:"update"`
}

type GatewayOrderSearchResponse struct {
	UpstreamRequestID string  `json:"upstream_request_id"`
	NextPageToken     string  `json:"next_page_token"`
	TotalCount        int64   `json:"total_count"`
	Orders            []Order `json:"orders"`
}

type GatewayOrderDetailsResponse struct {
	UpstreamRequestID string  `json:"upstream_request_id"`
	Orders            []Order `json:"orders"`
}

type GatewayOrderPriceDetailResponse struct {
	UpstreamRequestID string       `json:"upstream_request_id"`
	PriceDetail       *PriceDetail `json:"price_detail"`
}

type GatewayShipmentRecipientResponse struct {
	UpstreamRequestID string             `json:"upstream_request_id"`
	Recipient         *ShipmentRecipient `json:"recipient"`
}

type GatewayProductSearchResponse struct {
	UpstreamRequestID string    `json:"upstream_request_id"`
	NextPageToken     string    `json:"next_page_token"`
	TotalCount        int64     `json:"total_count"`
	Products          []Product `json:"products"`
}

type GatewayProductDetailResponse struct {
	UpstreamRequestID string   `json:"upstream_request_id"`
	Product           *Product `json:"product"`
}

type GatewayInventorySearchResponse struct {
	UpstreamRequestID string                   `json:"upstream_request_id"`
	Inventory         []ProductInventoryRecord `json:"inventory"`
}

type GatewayInventoryUpdateResponse struct {
	UpstreamRequestID string                 `json:"upstream_request_id"`
	Errors            []InventoryUpdateError `json:"errors"`
}

type GatewayError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	data      json.RawMessage
}

func (e *GatewayError) Error() string {
	if e == nil {
		return "TikTok Shop gateway error"
	}
	return fmt.Sprintf("TikTok Shop gateway %s", strings.TrimSpace(e.Code))
}

type gatewayEnvelope struct {
	Data  json.RawMessage `json:"data"`
	Error *GatewayError   `json:"error,omitempty"`
}

func NewGatewayClient(config GatewayClientConfig) *GatewayClient {
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &GatewayClient{
		baseURL: strings.TrimRight(strings.TrimSpace(config.BaseURL), "/"), tenant: strings.ToLower(strings.TrimSpace(config.Tenant)),
		sharedSecret: strings.TrimSpace(config.SharedSecret), httpClient: httpClient, now: now,
	}
}

func (c *GatewayClient) Configured() bool {
	if c == nil || c.tenant == "" || c.sharedSecret == "" {
		return false
	}
	baseURL, err := url.Parse(c.baseURL)
	return err == nil &&
		(baseURL.Scheme == "http" || baseURL.Scheme == "https") &&
		baseURL.Host != "" && baseURL.User == nil &&
		baseURL.RawQuery == "" && baseURL.Fragment == ""
}

func (c *GatewayClient) CreateAuthURL(ctx context.Context, input GatewayAuthURLRequest) (*GatewayAuthURLResponse, error) {
	if strings.TrimSpace(input.UserID) == "" || strings.TrimSpace(input.ReturnURL) == "" {
		return nil, ErrInvalidGatewayInput
	}
	var output GatewayAuthURLResponse
	if err := c.call(ctx, GatewayOAuthPath, input, &output); err != nil {
		return nil, err
	}
	if strings.TrimSpace(output.AuthURL) == "" || strings.TrimSpace(output.RedirectURL) == "" {
		return nil, errors.New("TikTok Shop gateway returned an invalid authorization response")
	}
	return &output, nil
}

func (c *GatewayClient) ListConnections(ctx context.Context) ([]GatewayConnection, error) {
	var output []GatewayConnection
	if err := c.call(ctx, GatewayConnectionsPath, struct{}{}, &output); err != nil {
		return nil, err
	}
	if output == nil {
		output = make([]GatewayConnection, 0)
	}
	return output, nil
}

func (c *GatewayClient) SearchOrders(ctx context.Context, input GatewayOrderSearchRequest) (*GatewayOrderSearchResponse, error) {
	input.ShopID = strings.TrimSpace(input.ShopID)
	if input.ShopID == "" || input.Search.Validate() != nil {
		return nil, ErrInvalidGatewayInput
	}
	var output GatewayOrderSearchResponse
	if err := c.call(ctx, GatewayOrderSearchPath, input, &output); err != nil {
		return nil, err
	}
	if err := validateOrders(output.Orders, 100, nil); err != nil || output.TotalCount < int64(len(output.Orders)) {
		return nil, errors.New("TikTok Shop gateway returned invalid order search data")
	}
	if output.Orders == nil {
		output.Orders = make([]Order, 0)
	}
	return &output, nil
}

func (c *GatewayClient) GetOrderDetails(ctx context.Context, input GatewayOrderDetailsRequest) (*GatewayOrderDetailsResponse, error) {
	input.ShopID = strings.TrimSpace(input.ShopID)
	orderIDs, err := normalizeOrderIDs(input.OrderIDs)
	if input.ShopID == "" || err != nil {
		return nil, ErrInvalidGatewayInput
	}
	input.OrderIDs = orderIDs
	var output GatewayOrderDetailsResponse
	if err := c.call(ctx, GatewayOrderDetailsPath, input, &output); err != nil {
		return nil, err
	}
	expected := make(map[string]struct{}, len(orderIDs))
	for _, orderID := range orderIDs {
		expected[orderID] = struct{}{}
	}
	if err := validateOrders(output.Orders, 50, expected); err != nil || len(output.Orders) != len(orderIDs) {
		return nil, errors.New("TikTok Shop gateway returned invalid order detail data")
	}
	return &output, nil
}

func (c *GatewayClient) GetShipmentRecipient(ctx context.Context, input GatewayShipmentRecipientRequest) (*GatewayShipmentRecipientResponse, error) {
	input.ShopID = strings.TrimSpace(input.ShopID)
	input.OrderID = strings.TrimSpace(input.OrderID)
	if input.ShopID == "" || input.OrderID == "" || strings.ContainsAny(input.OrderID, ",/?#") {
		return nil, ErrInvalidGatewayInput
	}
	var output GatewayShipmentRecipientResponse
	if err := c.call(ctx, GatewayShipmentRecipientPath, input, &output); err != nil {
		return nil, err
	}
	if output.Recipient == nil || strings.TrimSpace(output.Recipient.OrderID) != input.OrderID || !validShipmentRecipient(output.Recipient) {
		return nil, errors.New("TikTok Shop gateway returned invalid shipment recipient data")
	}
	return &output, nil
}

func (c *GatewayClient) GetPriceDetail(ctx context.Context, input GatewayOrderPriceDetailRequest) (*GatewayOrderPriceDetailResponse, error) {
	input.ShopID = strings.TrimSpace(input.ShopID)
	input.OrderID = strings.TrimSpace(input.OrderID)
	if input.ShopID == "" || input.OrderID == "" || strings.ContainsAny(input.OrderID, "/?#") {
		return nil, ErrInvalidGatewayInput
	}
	var output GatewayOrderPriceDetailResponse
	if err := c.call(ctx, GatewayOrderPriceDetailPath, input, &output); err != nil {
		return nil, err
	}
	if output.PriceDetail == nil || strings.TrimSpace(output.PriceDetail.Currency) == "" || strings.TrimSpace(output.PriceDetail.Payment) == "" {
		return nil, errors.New("TikTok Shop gateway returned invalid order price detail data")
	}
	return &output, nil
}

func (c *GatewayClient) SearchProducts(ctx context.Context, input GatewayProductSearchRequest) (*GatewayProductSearchResponse, error) {
	input.ShopID = strings.TrimSpace(input.ShopID)
	if input.ShopID == "" || input.Search.Validate() != nil {
		return nil, ErrInvalidGatewayInput
	}
	var output GatewayProductSearchResponse
	if err := c.call(ctx, GatewayProductSearchPath, input, &output); err != nil {
		return nil, err
	}
	if validateProducts(output.Products, 100) != nil || output.TotalCount < int64(len(output.Products)) {
		return nil, ErrInvalidProductResponse
	}
	if output.Products == nil {
		output.Products = []Product{}
	}
	return &output, nil
}

func (c *GatewayClient) GetProduct(ctx context.Context, input GatewayProductDetailRequest) (*GatewayProductDetailResponse, error) {
	input.ShopID = strings.TrimSpace(input.ShopID)
	input.ProductID = strings.TrimSpace(input.ProductID)
	if input.ShopID == "" || !validTikTokResourceID(input.ProductID) {
		return nil, ErrInvalidGatewayInput
	}
	var output GatewayProductDetailResponse
	if err := c.call(ctx, GatewayProductDetailPath, input, &output); err != nil {
		return nil, err
	}
	if output.Product == nil || output.Product.ID != input.ProductID || validateProducts([]Product{*output.Product}, 1) != nil {
		return nil, ErrInvalidProductResponse
	}
	return &output, nil
}

func (c *GatewayClient) SearchInventory(ctx context.Context, input GatewayInventorySearchRequest) (*GatewayInventorySearchResponse, error) {
	input.ShopID = strings.TrimSpace(input.ShopID)
	if input.ShopID == "" || input.Search.Validate() != nil {
		return nil, ErrInvalidGatewayInput
	}
	var output GatewayInventorySearchResponse
	if err := c.call(ctx, GatewayInventorySearchPath, input, &output); err != nil {
		return nil, err
	}
	if validateInventorySearchResult(InventorySearchResult{Inventory: output.Inventory}, input.Search) != nil {
		return nil, ErrInvalidProductResponse
	}
	if output.Inventory == nil {
		output.Inventory = []ProductInventoryRecord{}
	}
	return &output, nil
}

func (c *GatewayClient) UpdateInventory(ctx context.Context, input GatewayInventoryUpdateRequest) (*GatewayInventoryUpdateResponse, error) {
	input.ShopID = strings.TrimSpace(input.ShopID)
	input.ProductID = strings.TrimSpace(input.ProductID)
	if input.ShopID == "" || !validTikTokResourceID(input.ProductID) || input.Update.Validate() != nil {
		return nil, ErrInvalidGatewayInput
	}
	var output GatewayInventoryUpdateResponse
	if err := c.call(ctx, GatewayInventoryUpdatePath, input, &output); err != nil {
		var gatewayError *GatewayError
		if errors.As(err, &gatewayError) && gatewayError.Code == "inventory_item_rejected" && len(gatewayError.data) > 0 {
			if decodeErr := json.Unmarshal(gatewayError.data, &output); decodeErr == nil && validInventoryUpdateErrors(output.Errors) {
				return &output, err
			}
		}
		return nil, err
	}
	if !validInventoryUpdateErrors(output.Errors) {
		return nil, ErrInvalidProductResponse
	}
	if output.Errors == nil {
		output.Errors = []InventoryUpdateError{}
	}
	return &output, nil
}

func (c *GatewayClient) call(ctx context.Context, path string, input, output any) error {
	if !c.Configured() {
		return ErrGatewayNotConfigured
	}
	baseURL, err := url.Parse(c.baseURL)
	if err != nil {
		return ErrGatewayNotConfigured
	}
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	requestURL := *baseURL
	requestURL.Path = strings.TrimRight(requestURL.Path, "/") + path
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create TikTok Shop gateway request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	if err := gatewayauth.Apply(request, c.tenant, c.sharedSecret, body, c.now(), ""); err != nil {
		return fmt.Errorf("sign TikTok Shop gateway request: %w", err)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("call TikTok Shop gateway: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxGatewayResponseSize+1))
	if err != nil {
		return fmt.Errorf("read TikTok Shop gateway response: %w", err)
	}
	if len(responseBody) > maxGatewayResponseSize {
		return errors.New("TikTok Shop gateway response is too large")
	}
	var envelope gatewayEnvelope
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return fmt.Errorf("decode TikTok Shop gateway response: %w", err)
	}
	if envelope.Error != nil {
		envelope.Error.data = append(json.RawMessage(nil), envelope.Data...)
		return envelope.Error
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("TikTok Shop gateway HTTP %d", response.StatusCode)
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return errors.New("TikTok Shop gateway returned no data")
	}
	if err := json.Unmarshal(envelope.Data, output); err != nil {
		return fmt.Errorf("decode TikTok Shop gateway data: %w", err)
	}
	return nil
}
