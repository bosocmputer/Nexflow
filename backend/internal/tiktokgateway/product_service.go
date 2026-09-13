package tiktokgateway

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"nexflow/internal/services/tiktokshop"
)

const (
	TikTokProductBasicScope = "seller.product.basic"
	TikTokProductWriteScope = "seller.product.write"
)

var (
	ErrProductServiceNotConfigured  = errors.New("TikTok Shop product service is not configured")
	ErrProductBasicScopeUnavailable = errors.New("TikTok Shop Product Basic scope is unavailable")
	ErrProductWriteScopeUnavailable = errors.New("TikTok Shop Product Modify scope is unavailable")
)

type TikTokProductReader interface {
	SearchProducts(context.Context, string, string, tiktokshop.SearchProductsRequest) (*tiktokshop.SearchProductsResult, string, error)
	GetProduct(context.Context, string, string, string) (*tiktokshop.Product, string, error)
	SearchInventory(context.Context, string, string, tiktokshop.InventorySearchRequest) (*tiktokshop.InventorySearchResult, string, error)
	UpdateInventory(context.Context, string, string, string, tiktokshop.UpdateInventoryRequest) (*tiktokshop.UpdateInventoryResult, string, error)
}

type ProductService struct {
	credentials OrderCredentialProvider
	products    TikTokProductReader
}

type ProductSearchResult struct {
	UpstreamRequestID string               `json:"upstream_request_id"`
	NextPageToken     string               `json:"next_page_token"`
	TotalCount        int64                `json:"total_count"`
	Products          []tiktokshop.Product `json:"products"`
}

type ProductDetailResult struct {
	UpstreamRequestID string              `json:"upstream_request_id"`
	Product           *tiktokshop.Product `json:"product"`
}

type InventorySearchResult struct {
	UpstreamRequestID string                              `json:"upstream_request_id"`
	Inventory         []tiktokshop.ProductInventoryRecord `json:"inventory"`
}

type InventoryUpdateResult struct {
	UpstreamRequestID string                            `json:"upstream_request_id"`
	Errors            []tiktokshop.InventoryUpdateError `json:"errors"`
}

func NewProductService(credentials OrderCredentialProvider, products TikTokProductReader) (*ProductService, error) {
	if credentials == nil || products == nil {
		return nil, ErrProductServiceNotConfigured
	}
	return &ProductService{credentials: credentials, products: products}, nil
}

func (s *ProductService) SearchProducts(ctx context.Context, tenantSlug, shopID string, input tiktokshop.SearchProductsRequest) (*ProductSearchResult, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	credential, err := s.credential(ctx, tenantSlug, shopID, TikTokProductBasicScope)
	if err != nil {
		return nil, err
	}
	result, requestID, err := s.products.SearchProducts(ctx, credential.AccessToken, credential.ShopCipher, input)
	if err != nil {
		return nil, fmt.Errorf("search TikTok Shop products: %w", err)
	}
	if result == nil {
		return nil, tiktokshop.ErrInvalidProductResponse
	}
	return &ProductSearchResult{
		UpstreamRequestID: strings.TrimSpace(requestID), NextPageToken: result.NextPageToken,
		TotalCount: result.TotalCount, Products: append([]tiktokshop.Product(nil), result.Products...),
	}, nil
}

func (s *ProductService) GetProduct(ctx context.Context, tenantSlug, shopID, productID string) (*ProductDetailResult, error) {
	credential, err := s.credential(ctx, tenantSlug, shopID, TikTokProductBasicScope)
	if err != nil {
		return nil, err
	}
	product, requestID, err := s.products.GetProduct(ctx, credential.AccessToken, credential.ShopCipher, productID)
	if err != nil {
		return nil, fmt.Errorf("get TikTok Shop product: %w", err)
	}
	if product == nil {
		return nil, tiktokshop.ErrInvalidProductResponse
	}
	copyProduct := *product
	return &ProductDetailResult{UpstreamRequestID: strings.TrimSpace(requestID), Product: &copyProduct}, nil
}

func (s *ProductService) SearchInventory(ctx context.Context, tenantSlug, shopID string, input tiktokshop.InventorySearchRequest) (*InventorySearchResult, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	credential, err := s.credential(ctx, tenantSlug, shopID, TikTokProductBasicScope)
	if err != nil {
		return nil, err
	}
	result, requestID, err := s.products.SearchInventory(ctx, credential.AccessToken, credential.ShopCipher, input)
	if err != nil {
		return nil, fmt.Errorf("search TikTok Shop inventory: %w", err)
	}
	if result == nil {
		return nil, tiktokshop.ErrInvalidProductResponse
	}
	return &InventorySearchResult{UpstreamRequestID: strings.TrimSpace(requestID), Inventory: append([]tiktokshop.ProductInventoryRecord(nil), result.Inventory...)}, nil
}

func (s *ProductService) UpdateInventory(ctx context.Context, tenantSlug, shopID, productID string, input tiktokshop.UpdateInventoryRequest) (*InventoryUpdateResult, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	credential, err := s.credential(ctx, tenantSlug, shopID, TikTokProductWriteScope)
	if err != nil {
		return nil, err
	}
	result, requestID, err := s.products.UpdateInventory(ctx, credential.AccessToken, credential.ShopCipher, productID, input)
	if err != nil {
		return nil, fmt.Errorf("update TikTok Shop inventory: %w", err)
	}
	if result == nil {
		return nil, tiktokshop.ErrInvalidProductResponse
	}
	return &InventoryUpdateResult{UpstreamRequestID: strings.TrimSpace(requestID), Errors: append([]tiktokshop.InventoryUpdateError(nil), result.Errors...)}, nil
}

func (s *ProductService) credential(ctx context.Context, tenantSlug, shopID, requiredScope string) (*AccessCredential, error) {
	if s == nil || s.credentials == nil || s.products == nil {
		return nil, ErrProductServiceNotConfigured
	}
	tenantSlug = strings.ToLower(strings.TrimSpace(tenantSlug))
	shopID = strings.TrimSpace(shopID)
	if tenantSlug == "" || shopID == "" {
		return nil, tiktokshop.ErrInvalidProductInput
	}
	credential, err := s.credentials.AccessCredential(ctx, tenantSlug, shopID)
	if err != nil {
		return nil, err
	}
	if credential == nil || strings.TrimSpace(credential.AccessToken) == "" || strings.TrimSpace(credential.ShopCipher) == "" || credential.ShopID != shopID {
		return nil, ErrInvalidTokenCredential
	}
	if !containsScope(credential.GrantedScopes, requiredScope) {
		if requiredScope == TikTokProductWriteScope {
			return nil, ErrProductWriteScopeUnavailable
		}
		return nil, ErrProductBasicScopeUnavailable
	}
	return credential, nil
}

func containsScope(scopes []string, expected string) bool {
	for _, scope := range scopes {
		if strings.TrimSpace(scope) == expected {
			return true
		}
	}
	return false
}
