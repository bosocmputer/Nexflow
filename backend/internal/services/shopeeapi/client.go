package shopeeapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	PathAuthPartner                   = "/api/v2/shop/auth_partner"
	PathTokenGet                      = "/api/v2/auth/token/get"
	PathAccessTokenGet                = "/api/v2/auth/access_token/get"
	PathShopInfo                      = "/api/v2/shop/get_shop_info"
	PathShopProfile                   = "/api/v2/shop/get_profile"
	PathOrderList                     = "/api/v2/order/get_order_list"
	PathOrderDetail                   = "/api/v2/order/get_order_detail"
	PathLogisticsGetShippingParameter = "/api/v2/logistics/get_shipping_parameter"
	PathLogisticsShipOrder            = "/api/v2/logistics/ship_order"
	PathLogisticsGetTrackingNumber    = "/api/v2/logistics/get_tracking_number"
	PathLogisticsGetTrackingInfo      = "/api/v2/logistics/get_tracking_info"
	PathLogisticsGetShippingDocInfo   = "/api/v2/logistics/get_shipping_document_info"
	PathLogisticsGetShippingDocParam  = "/api/v2/logistics/get_shipping_document_parameter"
	PathLogisticsCreateShippingDoc    = "/api/v2/logistics/create_shipping_document"
	PathLogisticsGetShippingDocResult = "/api/v2/logistics/get_shipping_document_result"
	PathLogisticsDownloadShippingDoc  = "/api/v2/logistics/download_shipping_document"
	PathPaymentEscrowList             = "/api/v2/payment/get_escrow_list"
	PathPaymentEscrowDetail           = "/api/v2/payment/get_escrow_detail"
	DefaultSandboxBaseURL             = "https://openplatform.sandbox.test-stable.shopee.sg"
	DefaultLiveBaseURL                = "https://partner.shopeemobile.com"
)

type Config struct {
	BaseURL    string
	PartnerID  int64
	PartnerKey string
	HTTPClient *http.Client
}

type Client struct {
	baseURL    string
	partnerID  int64
	partnerKey string
	httpClient *http.Client
}

func New(cfg Config) *Client {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	return &Client{
		baseURL:    baseURL,
		partnerID:  cfg.PartnerID,
		partnerKey: cfg.PartnerKey,
		httpClient: httpClient,
	}
}

func (c *Client) Configured() bool {
	return c != nil && c.partnerID > 0 && strings.TrimSpace(c.partnerKey) != "" && strings.TrimSpace(c.baseURL) != ""
}

func (c *Client) AuthURL(redirectURL, state string, now time.Time) (string, error) {
	if !c.Configured() {
		return "", fmt.Errorf("shopee open api is not configured")
	}
	if strings.TrimSpace(redirectURL) == "" {
		return "", fmt.Errorf("redirect URL is required")
	}
	ts := now.Unix()
	q := url.Values{}
	q.Set("partner_id", strconv.FormatInt(c.partnerID, 10))
	q.Set("timestamp", strconv.FormatInt(ts, 10))
	q.Set("sign", c.sign(PathAuthPartner, ts, "", 0))
	q.Set("redirect", redirectURL)
	if state != "" {
		q.Set("state", state)
	}
	return c.baseURL + PathAuthPartner + "?" + q.Encode(), nil
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpireIn     int64  `json:"expire_in"`
	RequestID    string `json:"request_id"`
	ShopID       int64  `json:"shop_id"`
	MerchantID   int64  `json:"merchant_id"`
	Error        string `json:"error"`
	Message      string `json:"message"`
}

func (c *Client) GetToken(ctx context.Context, code string, shopID int64) (*TokenResponse, error) {
	body := map[string]interface{}{
		"code":       code,
		"partner_id": c.partnerID,
		"shop_id":    shopID,
	}
	var out TokenResponse
	if err := c.postPublic(ctx, PathTokenGet, body, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("shopee token/get: %s %s", out.Error, out.Message)
	}
	if out.AccessToken == "" || out.RefreshToken == "" {
		return nil, fmt.Errorf("shopee token/get: empty token response")
	}
	return &out, nil
}

func (c *Client) RefreshToken(ctx context.Context, refreshToken string, shopID int64) (*TokenResponse, error) {
	body := map[string]interface{}{
		"partner_id":    c.partnerID,
		"refresh_token": refreshToken,
		"shop_id":       shopID,
	}
	var out TokenResponse
	if err := c.postPublic(ctx, PathAccessTokenGet, body, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("shopee access_token/get: %s %s", out.Error, out.Message)
	}
	if out.AccessToken == "" || out.RefreshToken == "" {
		return nil, fmt.Errorf("shopee access_token/get: empty token response")
	}
	return &out, nil
}

type OrderListRequest struct {
	TimeRangeField         string
	TimeFrom               int64
	TimeTo                 int64
	PageSize               int
	Cursor                 string
	OrderStatus            string
	ResponseOptionalFields string
}

type EscrowListRequest struct {
	ReleaseTimeFrom int64
	ReleaseTimeTo   int64
	PageNo          int
	PageSize        int
}

type ShopInfoResponse struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Response  struct {
		ShopName string `json:"shop_name"`
		Region   string `json:"region"`
		Status   string `json:"status"`
	} `json:"response"`
}

type ShopProfileResponse struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Response  struct {
		ShopName    string `json:"shop_name"`
		Description string `json:"description"`
		ShopLogo    string `json:"shop_logo"`
	} `json:"response"`
}

func (c *Client) GetShopInfo(ctx context.Context, accessToken string, shopID int64) (*ShopInfoResponse, error) {
	var out ShopInfoResponse
	if err := c.getShop(ctx, PathShopInfo, accessToken, shopID, url.Values{}, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("shopee get_shop_info: %s %s", out.Error, out.Message)
	}
	return &out, nil
}

func (c *Client) GetShopProfile(ctx context.Context, accessToken string, shopID int64) (*ShopProfileResponse, error) {
	var out ShopProfileResponse
	if err := c.getShop(ctx, PathShopProfile, accessToken, shopID, url.Values{}, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("shopee get_profile: %s %s", out.Error, out.Message)
	}
	return &out, nil
}

type OrderListResponse struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Response  struct {
		More       bool   `json:"more"`
		NextCursor string `json:"next_cursor"`
		OrderList  []struct {
			OrderSN     string `json:"order_sn"`
			OrderStatus string `json:"order_status"`
		} `json:"order_list"`
	} `json:"response"`
}

func (c *Client) GetOrderList(ctx context.Context, accessToken string, shopID int64, req OrderListRequest) (*OrderListResponse, error) {
	q := url.Values{}
	q.Set("time_range_field", defaultString(req.TimeRangeField, "create_time"))
	q.Set("time_from", strconv.FormatInt(req.TimeFrom, 10))
	q.Set("time_to", strconv.FormatInt(req.TimeTo, 10))
	pageSize := req.PageSize
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 50
	}
	q.Set("page_size", strconv.Itoa(pageSize))
	if req.Cursor != "" {
		q.Set("cursor", req.Cursor)
	}
	if req.OrderStatus != "" {
		q.Set("order_status", req.OrderStatus)
	}
	if req.ResponseOptionalFields != "" {
		q.Set("response_optional_fields", req.ResponseOptionalFields)
	}
	var out OrderListResponse
	if err := c.getShop(ctx, PathOrderList, accessToken, shopID, q, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("shopee get_order_list: %s %s", out.Error, out.Message)
	}
	return &out, nil
}

type EscrowListResponse struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Response  struct {
		More       bool             `json:"more"`
		EscrowList []EscrowListItem `json:"escrow_list"`
	} `json:"response"`
}

type EscrowListItem struct {
	OrderSN           string  `json:"order_sn"`
	PayoutAmount      float64 `json:"payout_amount"`
	EscrowReleaseTime int64   `json:"escrow_release_time"`
}

func (c *Client) GetEscrowList(ctx context.Context, accessToken string, shopID int64, req EscrowListRequest) (*EscrowListResponse, error) {
	q := url.Values{}
	q.Set("release_time_from", strconv.FormatInt(req.ReleaseTimeFrom, 10))
	q.Set("release_time_to", strconv.FormatInt(req.ReleaseTimeTo, 10))
	pageNo := req.PageNo
	if pageNo <= 0 {
		pageNo = 1
	}
	pageSize := req.PageSize
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 50
	}
	q.Set("page_no", strconv.Itoa(pageNo))
	q.Set("page_size", strconv.Itoa(pageSize))
	var out EscrowListResponse
	if err := c.getShop(ctx, PathPaymentEscrowList, accessToken, shopID, q, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("shopee get_escrow_list: %s %s", out.Error, out.Message)
	}
	return &out, nil
}

type EscrowDetailResponse struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Response  struct {
		OrderSN     string            `json:"order_sn"`
		OrderIncome EscrowOrderIncome `json:"order_income"`
	} `json:"response"`
}

type EscrowOrderIncome struct {
	EscrowAmount           float64 `json:"escrow_amount"`
	BuyerTotalAmount       float64 `json:"buyer_total_amount"`
	OriginalPrice          float64 `json:"original_price"`
	SellerDiscount         float64 `json:"seller_discount"`
	ShopeeDiscount         float64 `json:"shopee_discount"`
	CommissionFee          float64 `json:"commission_fee"`
	ServiceFee             float64 `json:"service_fee"`
	SellerTransactionFee   float64 `json:"seller_transaction_fee"`
	FinalShippingFee       float64 `json:"final_shipping_fee"`
	ActualShippingFee      float64 `json:"actual_shipping_fee"`
	EscrowTax              float64 `json:"escrow_tax"`
	WithholdingTax         float64 `json:"withholding_tax"`
	VoucherFromSeller      float64 `json:"voucher_from_seller"`
	VoucherFromShopee      float64 `json:"voucher_from_shopee"`
	ReverseShippingFee     float64 `json:"reverse_shipping_fee"`
	BuyerPaidShippingFee   float64 `json:"buyer_paid_shipping_fee"`
	ShopeeShippingRebate   float64 `json:"shopee_shipping_rebate"`
	SellerShippingDiscount float64 `json:"seller_shipping_discount"`
	Coin                   float64 `json:"coin"`
}

func (c *Client) GetEscrowDetail(ctx context.Context, accessToken string, shopID int64, orderSN string) (*EscrowDetailResponse, error) {
	q := url.Values{}
	q.Set("order_sn", strings.TrimSpace(orderSN))
	var out EscrowDetailResponse
	if err := c.getShop(ctx, PathPaymentEscrowDetail, accessToken, shopID, q, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("shopee get_escrow_detail: %s %s", out.Error, out.Message)
	}
	if out.Response.OrderSN == "" {
		out.Response.OrderSN = strings.TrimSpace(orderSN)
	}
	return &out, nil
}

type OrderDetailResponse struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Response  struct {
		OrderList []OrderDetail `json:"order_list"`
	} `json:"response"`
}

type OrderDetail struct {
	OrderSN                 string         `json:"order_sn"`
	OrderStatus             string         `json:"order_status"`
	BuyerUsername           string         `json:"buyer_username"`
	CreateTime              int64          `json:"create_time"`
	PayTime                 int64          `json:"pay_time"`
	UpdateTime              int64          `json:"update_time"`
	TotalAmount             float64        `json:"total_amount"`
	Currency                string         `json:"currency"`
	PaymentMethod           string         `json:"payment_method"`
	TrackingNumber          string         `json:"tracking_number"`
	ActualShippingFee       float64        `json:"actual_shipping_fee"`
	EstimatedShippingFee    float64        `json:"estimated_shipping_fee"`
	ReverseShippingFee      float64        `json:"reverse_shipping_fee"`
	ShippingCarrier         string         `json:"shipping_carrier"`
	CheckoutShippingCarrier string         `json:"checkout_shipping_carrier"`
	COD                     bool           `json:"cod"`
	PackageList             []OrderPackage `json:"package_list"`
	RecipientAddress        struct {
		Name        string `json:"name"`
		Phone       string `json:"phone"`
		FullAddress string `json:"full_address"`
	} `json:"recipient_address"`
	ItemList []OrderItem `json:"item_list"`
}

type OrderPackage struct {
	PackageNumber              string  `json:"package_number"`
	LogisticsStatus            string  `json:"logistics_status"`
	ShippingCarrier            string  `json:"shipping_carrier"`
	TrackingNumber             string  `json:"tracking_number"`
	ParcelChargeableWeightGram float64 `json:"parcel_chargeable_weight_gram"`
}

type OrderItem struct {
	ItemID                 int64   `json:"item_id"`
	ItemName               string  `json:"item_name"`
	ItemSKU                string  `json:"item_sku"`
	ModelID                int64   `json:"model_id"`
	ModelName              string  `json:"model_name"`
	ModelSKU               string  `json:"model_sku"`
	ModelQuantityPurchased float64 `json:"model_quantity_purchased"`
	ModelOriginalPrice     float64 `json:"model_original_price"`
	ModelDiscountedPrice   float64 `json:"model_discounted_price"`
	ImageInfo              struct {
		ImageURL string `json:"image_url"`
	} `json:"image_info"`
}

type LogisticsID struct {
	raw   json.RawMessage
	value string
}

func (id *LogisticsID) UnmarshalJSON(data []byte) error {
	raw := bytes.TrimSpace(data)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		id.raw = nil
		id.value = ""
		return nil
	}
	switch raw[0] {
	case '"':
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return err
		}
		id.value = strings.TrimSpace(s)
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		var n json.Number
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&n); err != nil {
			return err
		}
		id.value = strings.TrimSpace(n.String())
	default:
		return fmt.Errorf("unsupported logistics id JSON type: %s", string(raw))
	}
	id.raw = append(id.raw[:0], raw...)
	return nil
}

func (id LogisticsID) MarshalJSON() ([]byte, error) {
	if len(id.raw) > 0 {
		return id.raw, nil
	}
	return json.Marshal(id.value)
}

func (id LogisticsID) String() string {
	return strings.TrimSpace(id.value)
}

type ShippingParameterResponse struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Response  struct {
		InfoNeeded struct {
			Pickup        []string `json:"pickup"`
			Dropoff       []string `json:"dropoff"`
			NonIntegrated []string `json:"non_integrated"`
		} `json:"info_needed"`
		Dropoff struct {
			BranchList []struct {
				BranchID  LogisticsID `json:"branch_id"`
				Name      string      `json:"name"`
				Address   string      `json:"address"`
				Latitude  LogisticsID `json:"latitude"`
				Longitude LogisticsID `json:"longitude"`
				Lat       LogisticsID `json:"lat"`
				Lng       LogisticsID `json:"lng"`
				Distance  LogisticsID `json:"distance"`
			} `json:"branch_list"`
		} `json:"dropoff"`
		Pickup struct {
			AddressList []struct {
				AddressID    LogisticsID `json:"address_id"`
				Address      string      `json:"address"`
				TimeSlotList []struct {
					PickupTimeID LogisticsID `json:"pickup_time_id"`
					Date         int64       `json:"date"`
				} `json:"time_slot_list"`
			} `json:"address_list"`
		} `json:"pickup"`
	} `json:"response"`
}

type ShipOrderRequest struct {
	OrderSN       string                 `json:"order_sn"`
	PackageNumber string                 `json:"package_number,omitempty"`
	Pickup        map[string]interface{} `json:"pickup,omitempty"`
	Dropoff       map[string]interface{} `json:"dropoff,omitempty"`
	NonIntegrated map[string]interface{} `json:"non_integrated,omitempty"`
}

type ShipOrderResponse struct {
	Error     string                 `json:"error"`
	Message   string                 `json:"message"`
	RequestID string                 `json:"request_id"`
	Response  map[string]interface{} `json:"response"`
}

type TrackingNumberResponse struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Response  struct {
		TrackingNumber string `json:"tracking_number"`
		Hint           string `json:"hint"`
	} `json:"response"`
}

type TrackingInfoResponse struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Response  struct {
		LogisticsStatus string          `json:"logistics_status"`
		OrderSN         string          `json:"order_sn"`
		PackageNumber   string          `json:"package_number"`
		TrackingInfo    []TrackingEvent `json:"tracking_info"`
	} `json:"response"`
}

type TrackingEvent struct {
	UpdateTime      int64  `json:"update_time"`
	Description     string `json:"description"`
	LogisticsStatus string `json:"logistics_status"`
	ReturnCode      string `json:"return_code"`
}

type ShippingDocumentInfoResponse struct {
	Error     string          `json:"error"`
	Message   string          `json:"message"`
	RequestID string          `json:"request_id"`
	Response  json.RawMessage `json:"response"`
}

type ShippingDocumentOrderEntry struct {
	OrderSN              string `json:"order_sn"`
	PackageNumber        string `json:"package_number,omitempty"`
	TrackingNumber       string `json:"tracking_number,omitempty"`
	ShippingDocumentType string `json:"shipping_document_type,omitempty"`
}

type ShippingDocumentResponse struct {
	Error     string                       `json:"error"`
	Message   string                       `json:"message"`
	RequestID string                       `json:"request_id"`
	Warning   []ShippingDocumentOrderEntry `json:"warning,omitempty"`
	Response  json.RawMessage              `json:"response"`
}

func (c *Client) GetOrderDetail(ctx context.Context, accessToken string, shopID int64, orderSNs []string, optionalFields []string) (*OrderDetailResponse, error) {
	if len(orderSNs) == 0 {
		return &OrderDetailResponse{}, nil
	}
	if len(orderSNs) > 50 {
		return nil, fmt.Errorf("shopee get_order_detail supports at most 50 orders per request")
	}
	q := url.Values{}
	q.Set("order_sn_list", strings.Join(orderSNs, ","))
	if len(optionalFields) > 0 {
		q.Set("response_optional_fields", strings.Join(optionalFields, ","))
	}
	var out OrderDetailResponse
	if err := c.getShop(ctx, PathOrderDetail, accessToken, shopID, q, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("shopee get_order_detail: %s %s", out.Error, out.Message)
	}
	return &out, nil
}

func (c *Client) GetShippingParameter(ctx context.Context, accessToken string, shopID int64, orderSN, packageNumber string) (*ShippingParameterResponse, error) {
	q := url.Values{}
	q.Set("order_sn", strings.TrimSpace(orderSN))
	if strings.TrimSpace(packageNumber) != "" {
		q.Set("package_number", strings.TrimSpace(packageNumber))
	}
	var out ShippingParameterResponse
	if err := c.getShop(ctx, PathLogisticsGetShippingParameter, accessToken, shopID, q, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("shopee get_shipping_parameter: %s %s", out.Error, out.Message)
	}
	return &out, nil
}

func (c *Client) ShipOrder(ctx context.Context, accessToken string, shopID int64, req ShipOrderRequest) (*ShipOrderResponse, error) {
	req.OrderSN = strings.TrimSpace(req.OrderSN)
	req.PackageNumber = strings.TrimSpace(req.PackageNumber)
	var out ShipOrderResponse
	if err := c.postShop(ctx, PathLogisticsShipOrder, accessToken, shopID, req, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("shopee ship_order: %s %s", out.Error, out.Message)
	}
	return &out, nil
}

func (c *Client) GetTrackingNumber(ctx context.Context, accessToken string, shopID int64, orderSN, packageNumber string) (*TrackingNumberResponse, error) {
	q := url.Values{}
	q.Set("order_sn", strings.TrimSpace(orderSN))
	if strings.TrimSpace(packageNumber) != "" {
		q.Set("package_number", strings.TrimSpace(packageNumber))
	}
	var out TrackingNumberResponse
	if err := c.getShop(ctx, PathLogisticsGetTrackingNumber, accessToken, shopID, q, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("shopee get_tracking_number: %s %s", out.Error, out.Message)
	}
	return &out, nil
}

func (c *Client) GetTrackingInfo(ctx context.Context, accessToken string, shopID int64, orderSN, packageNumber string) (*TrackingInfoResponse, error) {
	q := url.Values{}
	q.Set("order_sn", strings.TrimSpace(orderSN))
	if strings.TrimSpace(packageNumber) != "" {
		q.Set("package_number", strings.TrimSpace(packageNumber))
	}
	var out TrackingInfoResponse
	if err := c.getShop(ctx, PathLogisticsGetTrackingInfo, accessToken, shopID, q, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("shopee get_tracking_info: %s %s", out.Error, out.Message)
	}
	return &out, nil
}

func (c *Client) GetShippingDocumentInfo(ctx context.Context, accessToken string, shopID int64, orderSN, packageNumber string) (*ShippingDocumentInfoResponse, error) {
	q := url.Values{}
	q.Set("order_sn", strings.TrimSpace(orderSN))
	if strings.TrimSpace(packageNumber) != "" {
		q.Set("package_number", strings.TrimSpace(packageNumber))
	}
	var out ShippingDocumentInfoResponse
	if err := c.getShop(ctx, PathLogisticsGetShippingDocInfo, accessToken, shopID, q, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("shopee get_shipping_document_info: %s %s", out.Error, out.Message)
	}
	return &out, nil
}

func (c *Client) GetShippingDocumentParameter(ctx context.Context, accessToken string, shopID int64, orderSN, packageNumber string) (*ShippingDocumentResponse, error) {
	var out ShippingDocumentResponse
	payload := shippingDocumentPayload(orderSN, packageNumber, "", "")
	if err := c.postShop(ctx, PathLogisticsGetShippingDocParam, accessToken, shopID, payload, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("shopee get_shipping_document_parameter: %s %s", out.Error, out.Message)
	}
	return &out, nil
}

func (c *Client) CreateShippingDocument(ctx context.Context, accessToken string, shopID int64, orderSN, packageNumber, documentType, trackingNumber string) (*ShippingDocumentResponse, error) {
	var out ShippingDocumentResponse
	payload := shippingDocumentPayload(orderSN, packageNumber, documentType, trackingNumber)
	if err := c.postShop(ctx, PathLogisticsCreateShippingDoc, accessToken, shopID, payload, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("shopee create_shipping_document: %s %s", out.Error, out.Message)
	}
	return &out, nil
}

func (c *Client) GetShippingDocumentResult(ctx context.Context, accessToken string, shopID int64, orderSN, packageNumber, documentType string) (*ShippingDocumentResponse, error) {
	var out ShippingDocumentResponse
	payload := shippingDocumentPayload(orderSN, packageNumber, documentType, "")
	if err := c.postShop(ctx, PathLogisticsGetShippingDocResult, accessToken, shopID, payload, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("shopee get_shipping_document_result: %s %s", out.Error, out.Message)
	}
	return &out, nil
}

func (c *Client) DownloadShippingDocument(ctx context.Context, accessToken string, shopID int64, orderSN, packageNumber, documentType string) ([]byte, string, error) {
	payload := shippingDocumentPayload(orderSN, packageNumber, documentType, "")
	b, contentType, err := c.postShopBytes(ctx, PathLogisticsDownloadShippingDoc, accessToken, shopID, payload)
	if err != nil {
		return nil, "", err
	}
	if isShopeeJSONContentType(contentType) {
		var out ShippingDocumentResponse
		if json.Unmarshal(b, &out) == nil && out.Error != "" {
			return nil, contentType, fmt.Errorf("shopee download_shipping_document: %s %s", out.Error, out.Message)
		}
	}
	return b, contentType, nil
}

func (c *Client) postPublic(ctx context.Context, path string, payload interface{}, out interface{}) error {
	ts := time.Now().Unix()
	q := url.Values{}
	q.Set("partner_id", strconv.FormatInt(c.partnerID, 10))
	q.Set("timestamp", strconv.FormatInt(ts, 10))
	q.Set("sign", c.sign(path, ts, "", 0))
	return c.doJSON(ctx, http.MethodPost, c.baseURL+path+"?"+q.Encode(), payload, out)
}

func (c *Client) postShop(ctx context.Context, path, accessToken string, shopID int64, payload interface{}, out interface{}) error {
	ts := time.Now().Unix()
	q := url.Values{}
	q.Set("partner_id", strconv.FormatInt(c.partnerID, 10))
	q.Set("timestamp", strconv.FormatInt(ts, 10))
	q.Set("access_token", accessToken)
	q.Set("shop_id", strconv.FormatInt(shopID, 10))
	q.Set("sign", c.sign(path, ts, accessToken, shopID))
	return c.doJSON(ctx, http.MethodPost, c.baseURL+path+"?"+q.Encode(), payload, out)
}

func (c *Client) postShopBytes(ctx context.Context, path, accessToken string, shopID int64, payload interface{}) ([]byte, string, error) {
	ts := time.Now().Unix()
	q := url.Values{}
	q.Set("partner_id", strconv.FormatInt(c.partnerID, 10))
	q.Set("timestamp", strconv.FormatInt(ts, 10))
	q.Set("access_token", accessToken)
	q.Set("shop_id", strconv.FormatInt(shopID, 10))
	q.Set("sign", c.sign(path, ts, accessToken, shopID))
	return c.doRaw(ctx, http.MethodPost, c.baseURL+path+"?"+q.Encode(), payload)
}

func (c *Client) getShop(ctx context.Context, path, accessToken string, shopID int64, q url.Values, out interface{}) error {
	ts := time.Now().Unix()
	q.Set("partner_id", strconv.FormatInt(c.partnerID, 10))
	q.Set("timestamp", strconv.FormatInt(ts, 10))
	q.Set("access_token", accessToken)
	q.Set("shop_id", strconv.FormatInt(shopID, 10))
	q.Set("sign", c.sign(path, ts, accessToken, shopID))
	return c.doJSON(ctx, http.MethodGet, c.baseURL+path+"?"+q.Encode(), nil, out)
}

func (c *Client) doJSON(ctx context.Context, method, rawURL string, payload interface{}, out interface{}) error {
	b, _, err := c.doRaw(ctx, method, rawURL, payload)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("decode shopee response: %w", err)
	}
	return nil
}

func (c *Client) doRaw(ctx context.Context, method, rawURL string, payload interface{}) ([]byte, string, error) {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, "", err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, "", err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json, application/pdf, application/octet-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 24<<20))
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.Header.Get("Content-Type"), fmt.Errorf("shopee http %d: %s", resp.StatusCode, string(b))
	}
	return b, resp.Header.Get("Content-Type"), nil
}

func (c *Client) sign(path string, timestamp int64, accessToken string, shopID int64) string {
	base := strconv.FormatInt(c.partnerID, 10) + path + strconv.FormatInt(timestamp, 10)
	if accessToken != "" && shopID > 0 {
		base += accessToken + strconv.FormatInt(shopID, 10)
	}
	mac := hmac.New(sha256.New, []byte(c.partnerKey))
	mac.Write([]byte(base))
	return hex.EncodeToString(mac.Sum(nil))
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func shippingDocumentPayload(orderSN, packageNumber, documentType, trackingNumber string) map[string]interface{} {
	order := ShippingDocumentOrderEntry{
		OrderSN:              strings.TrimSpace(orderSN),
		PackageNumber:        strings.TrimSpace(packageNumber),
		ShippingDocumentType: strings.TrimSpace(documentType),
		TrackingNumber:       strings.TrimSpace(trackingNumber),
	}
	return map[string]interface{}{"order_list": []ShippingDocumentOrderEntry{order}}
}

func isShopeeJSONContentType(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "json")
}
