package tiktokgateway

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"nexflow/internal/services/tiktokshop"
)

var ErrOrderServiceNotConfigured = errors.New("TikTok Shop order service is not configured")

type OrderCredentialProvider interface {
	AccessCredential(context.Context, string, string) (*AccessCredential, error)
}

type TikTokOrderReader interface {
	SearchOrders(context.Context, string, string, tiktokshop.SearchOrdersRequest) (*tiktokshop.SearchOrdersResult, string, error)
	GetOrderDetails(context.Context, string, string, []string) ([]tiktokshop.Order, string, error)
	GetShipmentRecipient(context.Context, string, string, string) (*tiktokshop.ShipmentRecipient, string, error)
	GetPriceDetail(context.Context, string, string, string) (*tiktokshop.PriceDetail, string, error)
}

type OrderService struct {
	credentials OrderCredentialProvider
	orders      TikTokOrderReader
}

type OrderSearchResult struct {
	UpstreamRequestID string             `json:"upstream_request_id"`
	NextPageToken     string             `json:"next_page_token"`
	TotalCount        int64              `json:"total_count"`
	Orders            []tiktokshop.Order `json:"orders"`
}

type OrderDetailsResult struct {
	UpstreamRequestID string             `json:"upstream_request_id"`
	Orders            []tiktokshop.Order `json:"orders"`
}

type OrderPriceDetailResult struct {
	UpstreamRequestID string                  `json:"upstream_request_id"`
	PriceDetail       *tiktokshop.PriceDetail `json:"price_detail"`
}

type ShipmentRecipientResult struct {
	UpstreamRequestID string                        `json:"upstream_request_id"`
	Recipient         *tiktokshop.ShipmentRecipient `json:"recipient"`
}

func NewOrderService(credentials OrderCredentialProvider, orders TikTokOrderReader) (*OrderService, error) {
	if credentials == nil || orders == nil {
		return nil, ErrOrderServiceNotConfigured
	}
	return &OrderService{credentials: credentials, orders: orders}, nil
}

func (s *OrderService) SearchOrders(ctx context.Context, tenantSlug, shopID string, input tiktokshop.SearchOrdersRequest) (*OrderSearchResult, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	credential, err := s.credential(ctx, tenantSlug, shopID)
	if err != nil {
		return nil, err
	}
	result, upstreamRequestID, err := s.orders.SearchOrders(ctx, credential.AccessToken, credential.ShopCipher, input)
	if err != nil {
		return nil, fmt.Errorf("search TikTok Shop orders: %w", err)
	}
	if result == nil {
		return nil, tiktokshop.ErrInvalidOrderResponse
	}
	return &OrderSearchResult{
		UpstreamRequestID: strings.TrimSpace(upstreamRequestID), NextPageToken: result.NextPageToken,
		TotalCount: result.TotalCount, Orders: append([]tiktokshop.Order(nil), result.Orders...),
	}, nil
}

func (s *OrderService) GetOrderDetails(ctx context.Context, tenantSlug, shopID string, orderIDs []string) (*OrderDetailsResult, error) {
	credential, err := s.credential(ctx, tenantSlug, shopID)
	if err != nil {
		return nil, err
	}
	orders, upstreamRequestID, err := s.orders.GetOrderDetails(ctx, credential.AccessToken, credential.ShopCipher, orderIDs)
	if err != nil {
		return nil, fmt.Errorf("get TikTok Shop order details: %w", err)
	}
	return &OrderDetailsResult{UpstreamRequestID: strings.TrimSpace(upstreamRequestID), Orders: append([]tiktokshop.Order(nil), orders...)}, nil
}

func (s *OrderService) GetShipmentRecipient(ctx context.Context, tenantSlug, shopID, orderID string) (*ShipmentRecipientResult, error) {
	credential, err := s.credential(ctx, tenantSlug, shopID)
	if err != nil {
		return nil, err
	}
	recipient, upstreamRequestID, err := s.orders.GetShipmentRecipient(ctx, credential.AccessToken, credential.ShopCipher, orderID)
	if err != nil {
		return nil, fmt.Errorf("get TikTok Shop shipment recipient: %w", err)
	}
	if recipient == nil || strings.TrimSpace(recipient.OrderID) != strings.TrimSpace(orderID) {
		return nil, tiktokshop.ErrInvalidOrderResponse
	}
	return &ShipmentRecipientResult{UpstreamRequestID: strings.TrimSpace(upstreamRequestID), Recipient: recipient}, nil
}

func (s *OrderService) GetPriceDetail(ctx context.Context, tenantSlug, shopID, orderID string) (*OrderPriceDetailResult, error) {
	credential, err := s.credential(ctx, tenantSlug, shopID)
	if err != nil {
		return nil, err
	}
	detail, upstreamRequestID, err := s.orders.GetPriceDetail(ctx, credential.AccessToken, credential.ShopCipher, orderID)
	if err != nil {
		return nil, fmt.Errorf("get TikTok Shop order price detail: %w", err)
	}
	if detail == nil {
		return nil, tiktokshop.ErrInvalidOrderResponse
	}
	return &OrderPriceDetailResult{UpstreamRequestID: strings.TrimSpace(upstreamRequestID), PriceDetail: detail}, nil
}

func (s *OrderService) credential(ctx context.Context, tenantSlug, shopID string) (*AccessCredential, error) {
	if s == nil || s.credentials == nil || s.orders == nil {
		return nil, ErrOrderServiceNotConfigured
	}
	tenantSlug = strings.ToLower(strings.TrimSpace(tenantSlug))
	shopID = strings.TrimSpace(shopID)
	if tenantSlug == "" || shopID == "" {
		return nil, tiktokshop.ErrInvalidOrderInput
	}
	credential, err := s.credentials.AccessCredential(ctx, tenantSlug, shopID)
	if err != nil {
		return nil, err
	}
	if credential == nil || strings.TrimSpace(credential.AccessToken) == "" || strings.TrimSpace(credential.ShopCipher) == "" || credential.ShopID != shopID {
		return nil, ErrInvalidTokenCredential
	}
	return credential, nil
}
