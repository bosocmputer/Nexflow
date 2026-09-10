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
	GatewayOAuthPath        = "/internal/v1/tiktok-shop/oauth/auth-url"
	GatewayConnectionsPath  = "/internal/v1/tiktok-shop/connections"
	GatewayOrderSearchPath  = "/internal/v1/tiktok-shop/orders/search"
	GatewayOrderDetailsPath = "/internal/v1/tiktok-shop/orders/detail"
	maxGatewayResponseSize  = 8 << 20
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

type GatewayOrderSearchRequest struct {
	ShopID string              `json:"shop_id"`
	Search SearchOrdersRequest `json:"search"`
}

type GatewayOrderDetailsRequest struct {
	ShopID   string   `json:"shop_id"`
	OrderIDs []string `json:"order_ids"`
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

type GatewayError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable,omitempty"`
	RequestID string `json:"request_id,omitempty"`
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
