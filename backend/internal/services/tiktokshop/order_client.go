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
	"strconv"
	"strings"
	"time"
)

const (
	PathSearchOrders     = "/order/202309/orders/search"
	PathGetOrderDetails  = "/order/202507/orders"
	maxOrderResponseSize = 8 << 20
)

var (
	ErrInvalidOrderInput    = errors.New("invalid TikTok Shop order input")
	ErrInvalidOrderResponse = errors.New("invalid TikTok Shop order response")
)

type OrderStatus string

const (
	OrderStatusUnpaid             OrderStatus = "UNPAID"
	OrderStatusOnHold             OrderStatus = "ON_HOLD"
	OrderStatusAwaitingShipment   OrderStatus = "AWAITING_SHIPMENT"
	OrderStatusPartiallyShipping  OrderStatus = "PARTIALLY_SHIPPING"
	OrderStatusAwaitingCollection OrderStatus = "AWAITING_COLLECTION"
	OrderStatusInTransit          OrderStatus = "IN_TRANSIT"
	OrderStatusDelivered          OrderStatus = "DELIVERED"
	OrderStatusCompleted          OrderStatus = "COMPLETED"
	OrderStatusCancelled          OrderStatus = "CANCELLED"
)

type ShippingType string

const (
	ShippingTypeTikTok        ShippingType = "TIKTOK"
	ShippingTypeSeller        ShippingType = "SELLER"
	ShippingTypeTikTokDigital ShippingType = "TIKTOK_DIGITAL"
)

type OrderSortField string

const (
	OrderSortFieldCreateTime OrderSortField = "create_time"
	OrderSortFieldUpdateTime OrderSortField = "update_time"
)

type OrderSortOrder string

const (
	OrderSortAscending  OrderSortOrder = "ASC"
	OrderSortDescending OrderSortOrder = "DESC"
)

type OrderClientConfig struct {
	BaseURL    string
	AppKey     string
	AppSecret  string
	HTTPClient *http.Client
	Now        func() time.Time
}

type OrderClient struct {
	baseURL   *url.URL
	appKey    string
	appSecret string
	http      *http.Client
	now       func() time.Time
}

// SearchOrdersRequest intentionally omits buyer_user_id. Nexflow's polling and
// reconciliation do not require buyer identifiers, so accepting that filter
// would expand the PII surface without an operational need.
type SearchOrdersRequest struct {
	PageSize  int
	PageToken string
	SortField OrderSortField
	SortOrder OrderSortOrder
	Filters   OrderSearchFilters
}

type OrderSearchFilters struct {
	OrderStatus          OrderStatus  `json:"order_status,omitempty"`
	CreateTimeGE         int64        `json:"create_time_ge,omitempty"`
	CreateTimeLT         int64        `json:"create_time_lt,omitempty"`
	UpdateTimeGE         int64        `json:"update_time_ge,omitempty"`
	UpdateTimeLT         int64        `json:"update_time_lt,omitempty"`
	ShippingType         ShippingType `json:"shipping_type,omitempty"`
	IsBuyerRequestCancel *bool        `json:"is_buyer_request_cancel,omitempty"`
	WarehouseIDs         []string     `json:"warehouse_ids,omitempty"`
}

type SearchOrdersResult struct {
	NextPageToken string  `json:"next_page_token"`
	TotalCount    int64   `json:"total_count"`
	Orders        []Order `json:"orders"`
}

// Order contains only fields needed for marketplace identity, amount mapping,
// and lifecycle reconciliation. Recipient address, buyer messages, contact
// details, tax identifiers, and buyer profile fields are deliberately not
// represented so they cannot accidentally cross the gateway boundary.
type Order struct {
	ID                   string          `json:"id"`
	Status               OrderStatus     `json:"status"`
	CreateTime           int64           `json:"create_time,omitempty"`
	UpdateTime           int64           `json:"update_time,omitempty"`
	PaidTime             int64           `json:"paid_time,omitempty"`
	CancelTime           int64           `json:"cancel_time,omitempty"`
	ShippingType         ShippingType    `json:"shipping_type,omitempty"`
	FulfillmentType      string          `json:"fulfillment_type,omitempty"`
	DeliveryType         string          `json:"delivery_type,omitempty"`
	OrderType            string          `json:"order_type,omitempty"`
	CommercePlatform     string          `json:"commerce_platform,omitempty"`
	IsCOD                bool            `json:"is_cod,omitempty"`
	IsSampleOrder        bool            `json:"is_sample_order,omitempty"`
	IsBuyerRequestCancel bool            `json:"is_buyer_request_cancel,omitempty"`
	Payment              OrderPayment    `json:"payment"`
	LineItems            []OrderLineItem `json:"line_items"`
	Packages             []OrderPackage  `json:"packages,omitempty"`
}

type OrderPayment struct {
	Currency                  string `json:"currency"`
	SubTotal                  string `json:"sub_total"`
	ShippingFee               string `json:"shipping_fee"`
	SellerDiscount            string `json:"seller_discount"`
	PlatformDiscount          string `json:"platform_discount"`
	TotalAmount               string `json:"total_amount"`
	OriginalTotalProductPrice string `json:"original_total_product_price"`
	OriginalShippingFee       string `json:"original_shipping_fee,omitempty"`
	Tax                       string `json:"tax,omitempty"`
	ShippingFeeTax            string `json:"shipping_fee_tax,omitempty"`
	ProductTax                string `json:"product_tax,omitempty"`
}

type OrderLineItem struct {
	ID                  string               `json:"id"`
	ProductID           string               `json:"product_id"`
	SKUID               string               `json:"sku_id"`
	SellerSKU           string               `json:"seller_sku"`
	ProductName         string               `json:"product_name"`
	SKUName             string               `json:"sku_name"`
	Currency            string               `json:"currency"`
	OriginalPrice       string               `json:"original_price"`
	SalePrice           string               `json:"sale_price"`
	SellerDiscount      string               `json:"seller_discount"`
	PlatformDiscount    string               `json:"platform_discount"`
	DisplayStatus       string               `json:"display_status,omitempty"`
	PackageID           string               `json:"package_id,omitempty"`
	PackageStatus       string               `json:"package_status,omitempty"`
	Quantity            int                  `json:"quantity,omitempty"`
	CombinedListingSKUs []CombinedListingSKU `json:"combined_listing_skus,omitempty"`
}

type CombinedListingSKU struct {
	SKUID     string `json:"sku_id"`
	SKUCount  int    `json:"sku_count"`
	ProductID string `json:"product_id"`
	SellerSKU string `json:"seller_sku"`
}

type OrderPackage struct {
	ID string `json:"id"`
}

type orderResponse struct {
	Code      int                `json:"code"`
	Message   string             `json:"message"`
	RequestID string             `json:"request_id"`
	Data      SearchOrdersResult `json:"data"`
}

func NewOrderClient(config OrderClientConfig) (*OrderClient, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultOpenAPIBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("TikTok Shop Open API base URL must be an absolute HTTPS URL")
	}
	if strings.TrimSpace(config.AppKey) == "" || strings.TrimSpace(config.AppSecret) == "" {
		return nil, ErrInvalidOrderInput
	}
	client := config.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &OrderClient{
		baseURL: parsed, appKey: strings.TrimSpace(config.AppKey), appSecret: strings.TrimSpace(config.AppSecret),
		http: client, now: now,
	}, nil
}

func (c *OrderClient) SearchOrders(ctx context.Context, accessToken, shopCipher string, input SearchOrdersRequest) (*SearchOrdersResult, string, error) {
	accessToken = strings.TrimSpace(accessToken)
	shopCipher = strings.TrimSpace(shopCipher)
	if c == nil || c.baseURL == nil || accessToken == "" || shopCipher == "" || validateSearchOrdersRequest(&input) != nil {
		return nil, "", ErrInvalidOrderInput
	}
	body, err := json.Marshal(input.Filters)
	if err != nil {
		return nil, "", fmt.Errorf("encode TikTok Shop order search: %w", err)
	}
	query := c.baseQuery(shopCipher)
	query.Set("page_size", strconv.Itoa(input.PageSize))
	if input.PageToken != "" {
		query.Set("page_token", input.PageToken)
	}
	if input.SortField != "" {
		query.Set("sort_field", string(input.SortField))
	}
	if input.SortOrder != "" {
		query.Set("sort_order", string(input.SortOrder))
	}
	payload, requestID, err := c.do(ctx, http.MethodPost, PathSearchOrders, query, body, accessToken)
	if err != nil {
		return nil, requestID, err
	}
	if err := validateOrders(payload.Data.Orders, 100, nil); err != nil || payload.Data.TotalCount < int64(len(payload.Data.Orders)) {
		return nil, requestID, ErrInvalidOrderResponse
	}
	payload.Data.NextPageToken = strings.TrimSpace(payload.Data.NextPageToken)
	return &payload.Data, requestID, nil
}

func (c *OrderClient) GetOrderDetails(ctx context.Context, accessToken, shopCipher string, orderIDs []string) ([]Order, string, error) {
	accessToken = strings.TrimSpace(accessToken)
	shopCipher = strings.TrimSpace(shopCipher)
	ids, err := normalizeOrderIDs(orderIDs)
	if c == nil || c.baseURL == nil || accessToken == "" || shopCipher == "" || err != nil {
		return nil, "", ErrInvalidOrderInput
	}
	query := c.baseQuery(shopCipher)
	query.Set("ids", strings.Join(ids, ","))
	payload, requestID, err := c.do(ctx, http.MethodGet, PathGetOrderDetails, query, nil, accessToken)
	if err != nil {
		return nil, requestID, err
	}
	expected := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		expected[id] = struct{}{}
	}
	if err := validateOrders(payload.Data.Orders, 50, expected); err != nil || len(payload.Data.Orders) != len(ids) {
		return nil, requestID, ErrInvalidOrderResponse
	}
	return payload.Data.Orders, requestID, nil
}

func (c *OrderClient) baseQuery(shopCipher string) url.Values {
	return url.Values{
		"app_key":     []string{c.appKey},
		"shop_cipher": []string{shopCipher},
		"timestamp":   []string{strconv.FormatInt(c.now().Unix(), 10)},
	}
}

func (c *OrderClient) do(ctx context.Context, method, path string, query url.Values, body []byte, accessToken string) (*orderResponse, string, error) {
	signature, err := SignRequest(c.appSecret, path, query, body, false)
	if err != nil {
		return nil, "", fmt.Errorf("sign TikTok Shop order request: %w", err)
	}
	query.Set("sign", signature)
	requestURL := *c.baseURL
	requestURL.Path = strings.TrimRight(requestURL.Path, "/") + path
	requestURL.RawQuery = query.Encode()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, requestURL.String(), reader)
	if err != nil {
		return nil, "", fmt.Errorf("create TikTok Shop order request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-tts-access-token", accessToken)
	response, err := c.http.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("call TikTok Shop order API: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxOrderResponseSize+1))
	if err != nil {
		return nil, "", fmt.Errorf("read TikTok Shop order response: %w", err)
	}
	if len(responseBody) > maxOrderResponseSize {
		return nil, "", ErrInvalidOrderResponse
	}
	var payload orderResponse
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return nil, "", fmt.Errorf("decode TikTok Shop order response: %w", err)
	}
	requestID := strings.TrimSpace(payload.RequestID)
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices || payload.Code != 0 {
		return nil, requestID, &APIError{Code: payload.Code, RequestID: requestID, Message: "TikTok Shop rejected the order request"}
	}
	return &payload, requestID, nil
}

func validateSearchOrdersRequest(input *SearchOrdersRequest) error {
	if input == nil || input.PageSize < 1 || input.PageSize > 100 {
		return ErrInvalidOrderInput
	}
	input.PageToken = strings.TrimSpace(input.PageToken)
	if input.SortField != "" && input.SortField != OrderSortFieldCreateTime && input.SortField != OrderSortFieldUpdateTime {
		return ErrInvalidOrderInput
	}
	if input.SortOrder != "" && input.SortOrder != OrderSortAscending && input.SortOrder != OrderSortDescending {
		return ErrInvalidOrderInput
	}
	if !validOrderStatus(input.Filters.OrderStatus) || !validShippingType(input.Filters.ShippingType) {
		return ErrInvalidOrderInput
	}
	if !validTimeRange(input.Filters.CreateTimeGE, input.Filters.CreateTimeLT) || !validTimeRange(input.Filters.UpdateTimeGE, input.Filters.UpdateTimeLT) {
		return ErrInvalidOrderInput
	}
	if len(input.Filters.WarehouseIDs) > 100 {
		return ErrInvalidOrderInput
	}
	seen := make(map[string]struct{}, len(input.Filters.WarehouseIDs))
	for i, warehouseID := range input.Filters.WarehouseIDs {
		warehouseID = strings.TrimSpace(warehouseID)
		if warehouseID == "" {
			return ErrInvalidOrderInput
		}
		if _, exists := seen[warehouseID]; exists {
			return ErrInvalidOrderInput
		}
		seen[warehouseID] = struct{}{}
		input.Filters.WarehouseIDs[i] = warehouseID
	}
	return nil
}

func validOrderStatus(status OrderStatus) bool {
	switch status {
	case "", OrderStatusUnpaid, OrderStatusOnHold, OrderStatusAwaitingShipment, OrderStatusPartiallyShipping,
		OrderStatusAwaitingCollection, OrderStatusInTransit, OrderStatusDelivered, OrderStatusCompleted, OrderStatusCancelled:
		return true
	default:
		return false
	}
}

func validShippingType(shippingType ShippingType) bool {
	switch shippingType {
	case "", ShippingTypeTikTok, ShippingTypeSeller, ShippingTypeTikTokDigital:
		return true
	default:
		return false
	}
}

func validTimeRange(from, to int64) bool {
	return from >= 0 && to >= 0 && (from == 0 || to == 0 || from < to)
}

func normalizeOrderIDs(input []string) ([]string, error) {
	if len(input) == 0 || len(input) > 50 {
		return nil, ErrInvalidOrderInput
	}
	output := make([]string, len(input))
	seen := make(map[string]struct{}, len(input))
	for i, id := range input {
		id = strings.TrimSpace(id)
		if id == "" {
			return nil, ErrInvalidOrderInput
		}
		if _, exists := seen[id]; exists {
			return nil, ErrInvalidOrderInput
		}
		seen[id] = struct{}{}
		output[i] = id
	}
	return output, nil
}

func validateOrders(orders []Order, max int, expected map[string]struct{}) error {
	if len(orders) > max {
		return ErrInvalidOrderResponse
	}
	seen := make(map[string]struct{}, len(orders))
	for i := range orders {
		orders[i].ID = strings.TrimSpace(orders[i].ID)
		if orders[i].ID == "" || !validOrderStatus(orders[i].Status) {
			return ErrInvalidOrderResponse
		}
		if _, exists := seen[orders[i].ID]; exists {
			return ErrInvalidOrderResponse
		}
		if expected != nil {
			if _, ok := expected[orders[i].ID]; !ok {
				return ErrInvalidOrderResponse
			}
		}
		seen[orders[i].ID] = struct{}{}
	}
	return nil
}
