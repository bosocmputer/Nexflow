package shopeeapi

import (
	"context"
	"time"
)

// APIClient is the Shopee capability surface used by Nexflow handlers. Direct
// and gateway clients implement the same typed contract so rollout can switch
// per instance without changing marketplace behavior.
type APIClient interface {
	Configured() bool
	AuthURL(redirectURL, state string, now time.Time) (string, error)
	GetToken(ctx context.Context, code string, shopID int64) (*TokenResponse, error)
	RefreshToken(ctx context.Context, refreshToken string, shopID int64) (*TokenResponse, error)
	GetShopInfo(ctx context.Context, accessToken string, shopID int64) (*ShopInfoResponse, error)
	GetShopProfile(ctx context.Context, accessToken string, shopID int64) (*ShopProfileResponse, error)
	GetOrderList(ctx context.Context, accessToken string, shopID int64, req OrderListRequest) (*OrderListResponse, error)
	GetEscrowList(ctx context.Context, accessToken string, shopID int64, req EscrowListRequest) (*EscrowListResponse, error)
	GetEscrowDetail(ctx context.Context, accessToken string, shopID int64, orderSN string) (*EscrowDetailResponse, error)
	GetOrderDetail(ctx context.Context, accessToken string, shopID int64, orderSNs []string, optionalFields []string) (*OrderDetailResponse, error)
	GetShippingParameter(ctx context.Context, accessToken string, shopID int64, orderSN, packageNumber string) (*ShippingParameterResponse, error)
	ShipOrder(ctx context.Context, accessToken string, shopID int64, req ShipOrderRequest) (*ShipOrderResponse, error)
	GetTrackingNumber(ctx context.Context, accessToken string, shopID int64, orderSN, packageNumber string) (*TrackingNumberResponse, error)
	GetTrackingInfo(ctx context.Context, accessToken string, shopID int64, orderSN, packageNumber string) (*TrackingInfoResponse, error)
	GetShippingDocumentInfo(ctx context.Context, accessToken string, shopID int64, orderSN, packageNumber string) (*ShippingDocumentInfoResponse, error)
	GetShippingDocumentParameter(ctx context.Context, accessToken string, shopID int64, orderSN, packageNumber string) (*ShippingDocumentResponse, error)
	CreateShippingDocument(ctx context.Context, accessToken string, shopID int64, orderSN, packageNumber, documentType, trackingNumber string) (*ShippingDocumentResponse, error)
	GetShippingDocumentResult(ctx context.Context, accessToken string, shopID int64, orderSN, packageNumber, documentType string) (*ShippingDocumentResponse, error)
	DownloadShippingDocument(ctx context.Context, accessToken string, shopID int64, orderSN, packageNumber, documentType string) ([]byte, string, error)
}

var _ APIClient = (*Client)(nil)
