package tiktokshop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	PathSearchProducts      = "/product/202502/products/search"
	PathGetProductBase      = "/product/202309/products"
	PathInventorySearch     = "/product/202309/inventory/search"
	PathInventoryUpdateBase = "/product/202309/products"
)

var (
	ErrInvalidProductInput    = errors.New("invalid TikTok Shop product input")
	ErrInvalidProductResponse = errors.New("invalid TikTok Shop product response")
	tiktokResourceIDPattern   = regexp.MustCompile(`^[0-9]{1,64}$`)
)

type ProductStatus string

const (
	ProductStatusAll                 ProductStatus = "ALL"
	ProductStatusDraft               ProductStatus = "DRAFT"
	ProductStatusPending             ProductStatus = "PENDING"
	ProductStatusFailed              ProductStatus = "FAILED"
	ProductStatusActivate            ProductStatus = "ACTIVATE"
	ProductStatusSellerDeactivated   ProductStatus = "SELLER_DEACTIVATED"
	ProductStatusPlatformDeactivated ProductStatus = "PLATFORM_DEACTIVATED"
	ProductStatusFreeze              ProductStatus = "FREEZE"
	ProductStatusDeleted             ProductStatus = "DELETED"
)

type ProductClientConfig struct {
	BaseURL    string
	AppKey     string
	AppSecret  string
	HTTPClient *http.Client
	Now        func() time.Time
}

// ProductClient reuses the single-byte serialization and signing transport
// proven by OrderClient. Public product types remain allowlisted so unknown
// TikTok fields, including any future seller data, do not cross the gateway.
type ProductClient struct {
	transport *OrderClient
}

type SearchProductsRequest struct {
	PageSize  int           `json:"page_size"`
	PageToken string        `json:"page_token,omitempty"`
	Status    ProductStatus `json:"status,omitempty"`
}

type SearchProductsResult struct {
	NextPageToken string    `json:"next_page_token"`
	TotalCount    int64     `json:"total_count"`
	Products      []Product `json:"products"`
}

type Product struct {
	ID         string        `json:"id"`
	Title      string        `json:"title"`
	Status     ProductStatus `json:"status"`
	CreateTime int64         `json:"create_time,omitempty"`
	UpdateTime int64         `json:"update_time,omitempty"`
	SKUs       []ProductSKU  `json:"skus"`
}

type ProductSKU struct {
	ID        string             `json:"id"`
	SellerSKU string             `json:"seller_sku"`
	Price     ProductPrice       `json:"price"`
	Inventory []ProductInventory `json:"inventory"`
}

type ProductPrice struct {
	Currency          string `json:"currency"`
	TaxExclusivePrice string `json:"tax_exclusive_price,omitempty"`
	SalePrice         string `json:"sale_price,omitempty"`
	StartingBidPrice  string `json:"starting_bid_price,omitempty"`
}

type ProductInventory struct {
	WarehouseID string `json:"warehouse_id"`
	Quantity    int64  `json:"quantity"`
}

type InventorySearchRequest struct {
	ProductIDs []string `json:"product_ids,omitempty"`
	SKUIDs     []string `json:"sku_ids,omitempty"`
}

type InventorySearchResult struct {
	Inventory []ProductInventoryRecord `json:"inventory"`
}

type ProductInventoryRecord struct {
	ProductID string               `json:"product_id"`
	SKUs      []InventorySearchSKU `json:"skus"`
}

type InventorySearchSKU struct {
	ID                     string               `json:"id"`
	SellerSKU              string               `json:"seller_sku"`
	TotalAvailableQuantity int64                `json:"total_available_quantity"`
	TotalCommittedQuantity int64                `json:"total_committed_quantity"`
	WarehouseInventory     []WarehouseInventory `json:"warehouse_inventory"`
}

type WarehouseInventory struct {
	WarehouseID       string `json:"warehouse_id"`
	AvailableQuantity int64  `json:"available_quantity"`
	CommittedQuantity int64  `json:"committed_quantity"`
}

type UpdateInventoryRequest struct {
	SKUs []InventorySKUUpdate `json:"skus"`
}

type InventorySKUUpdate struct {
	ID        string                     `json:"id"`
	Inventory []WarehouseInventoryUpdate `json:"inventory"`
}

type WarehouseInventoryUpdate struct {
	WarehouseID string `json:"warehouse_id"`
	Quantity    int64  `json:"quantity"`
}

type UpdateInventoryResult struct {
	Errors []InventoryUpdateError `json:"errors"`
}

type InventoryUpdateError struct {
	Code    int                        `json:"code"`
	Message string                     `json:"message"`
	Detail  InventoryUpdateErrorDetail `json:"detail"`
}

type InventoryUpdateErrorDetail struct {
	SKUID       string                    `json:"sku_id"`
	ExtraErrors []InventoryWarehouseError `json:"extra_errors"`
}

type InventoryWarehouseError struct {
	WarehouseID string `json:"warehouse_id"`
	Code        int    `json:"code"`
	Message     string `json:"message"`
}

func NewProductClient(config ProductClientConfig) (*ProductClient, error) {
	transport, err := NewOrderClient(OrderClientConfig(config))
	if err != nil {
		return nil, err
	}
	return &ProductClient{transport: transport}, nil
}

func (c *ProductClient) SearchProducts(ctx context.Context, accessToken, shopCipher string, input SearchProductsRequest) (*SearchProductsResult, string, error) {
	if c == nil || c.transport == nil || !validProductCredential(accessToken, shopCipher) || input.Validate() != nil {
		return nil, "", ErrInvalidProductInput
	}
	body, err := json.Marshal(struct {
		Status ProductStatus `json:"status,omitempty"`
	}{Status: input.Status})
	if err != nil {
		return nil, "", fmt.Errorf("encode TikTok Shop product search: %w", err)
	}
	query := c.transport.baseQuery(strings.TrimSpace(shopCipher))
	query.Set("page_size", strconv.Itoa(input.PageSize))
	if input.PageToken != "" {
		query.Set("page_token", input.PageToken)
	}
	var result SearchProductsResult
	requestID, err := c.transport.do(ctx, http.MethodPost, PathSearchProducts, query, body, strings.TrimSpace(accessToken), &result)
	if err != nil {
		return nil, requestID, err
	}
	if err := validateProducts(result.Products, 100); err != nil || result.TotalCount < int64(len(result.Products)) {
		return nil, requestID, ErrInvalidProductResponse
	}
	result.NextPageToken = strings.TrimSpace(result.NextPageToken)
	return &result, requestID, nil
}

func (c *ProductClient) GetProduct(ctx context.Context, accessToken, shopCipher, productID string) (*Product, string, error) {
	productID = strings.TrimSpace(productID)
	if c == nil || c.transport == nil || !validProductCredential(accessToken, shopCipher) || !validTikTokResourceID(productID) {
		return nil, "", ErrInvalidProductInput
	}
	path := PathGetProductBase + "/" + productID
	var product Product
	requestID, err := c.transport.do(ctx, http.MethodGet, path, c.transport.baseQuery(strings.TrimSpace(shopCipher)), nil, strings.TrimSpace(accessToken), &product)
	if err != nil {
		return nil, requestID, err
	}
	if product.ID != productID || validateProducts([]Product{product}, 1) != nil {
		return nil, requestID, ErrInvalidProductResponse
	}
	return &product, requestID, nil
}

func (c *ProductClient) SearchInventory(ctx context.Context, accessToken, shopCipher string, input InventorySearchRequest) (*InventorySearchResult, string, error) {
	if c == nil || c.transport == nil || !validProductCredential(accessToken, shopCipher) || input.Validate() != nil {
		return nil, "", ErrInvalidProductInput
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, "", fmt.Errorf("encode TikTok Shop inventory search: %w", err)
	}
	var result InventorySearchResult
	requestID, err := c.transport.do(ctx, http.MethodPost, PathInventorySearch, c.transport.baseQuery(strings.TrimSpace(shopCipher)), body, strings.TrimSpace(accessToken), &result)
	if err != nil {
		return nil, requestID, err
	}
	if validateInventorySearchResult(result, input) != nil {
		return nil, requestID, ErrInvalidProductResponse
	}
	return &result, requestID, nil
}

func (c *ProductClient) UpdateInventory(ctx context.Context, accessToken, shopCipher, productID string, input UpdateInventoryRequest) (*UpdateInventoryResult, string, error) {
	productID = strings.TrimSpace(productID)
	if c == nil || c.transport == nil || !validProductCredential(accessToken, shopCipher) || !validTikTokResourceID(productID) || input.Validate() != nil {
		return nil, "", ErrInvalidProductInput
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, "", fmt.Errorf("encode TikTok Shop inventory update: %w", err)
	}
	path := PathInventoryUpdateBase + "/" + productID + "/inventory/update"
	var result UpdateInventoryResult
	requestID, err := c.transport.do(ctx, http.MethodPost, path, c.transport.baseQuery(strings.TrimSpace(shopCipher)), body, strings.TrimSpace(accessToken), &result)
	if err != nil {
		return nil, requestID, err
	}
	if result.Errors == nil {
		result.Errors = []InventoryUpdateError{}
	}
	if !validInventoryUpdateErrors(result.Errors) {
		return nil, requestID, ErrInvalidProductResponse
	}
	return &result, requestID, nil
}

func (input *SearchProductsRequest) Validate() error {
	if input == nil || input.PageSize < 1 || input.PageSize > 100 {
		return ErrInvalidProductInput
	}
	input.PageToken = strings.TrimSpace(input.PageToken)
	if input.Status == "" {
		input.Status = ProductStatusAll
	}
	switch input.Status {
	case ProductStatusAll, ProductStatusDraft, ProductStatusPending, ProductStatusFailed, ProductStatusActivate,
		ProductStatusSellerDeactivated, ProductStatusPlatformDeactivated, ProductStatusFreeze, ProductStatusDeleted:
		return nil
	default:
		return ErrInvalidProductInput
	}
}

func (input *InventorySearchRequest) Validate() error {
	if input == nil || (len(input.ProductIDs) == 0) == (len(input.SKUIDs) == 0) || len(input.ProductIDs) > 100 || len(input.SKUIDs) > 600 {
		return ErrInvalidProductInput
	}
	if len(input.ProductIDs) > 0 {
		input.ProductIDs = normalizeTikTokResourceIDs(input.ProductIDs)
		if len(input.ProductIDs) == 0 {
			return ErrInvalidProductInput
		}
	}
	if len(input.SKUIDs) > 0 {
		input.SKUIDs = normalizeTikTokResourceIDs(input.SKUIDs)
		if len(input.SKUIDs) == 0 {
			return ErrInvalidProductInput
		}
	}
	return nil
}

func (input *UpdateInventoryRequest) Validate() error {
	if input == nil || len(input.SKUs) == 0 || len(input.SKUs) > 50 {
		return ErrInvalidProductInput
	}
	seenSKUs := make(map[string]struct{}, len(input.SKUs))
	for index := range input.SKUs {
		sku := &input.SKUs[index]
		sku.ID = strings.TrimSpace(sku.ID)
		if !validTikTokResourceID(sku.ID) || len(sku.Inventory) == 0 || len(sku.Inventory) > 50 {
			return ErrInvalidProductInput
		}
		if _, exists := seenSKUs[sku.ID]; exists {
			return ErrInvalidProductInput
		}
		seenSKUs[sku.ID] = struct{}{}
		seenWarehouses := make(map[string]struct{}, len(sku.Inventory))
		for inventoryIndex := range sku.Inventory {
			inventory := &sku.Inventory[inventoryIndex]
			inventory.WarehouseID = strings.TrimSpace(inventory.WarehouseID)
			if !validTikTokResourceID(inventory.WarehouseID) || inventory.Quantity < 0 {
				return ErrInvalidProductInput
			}
			if _, exists := seenWarehouses[inventory.WarehouseID]; exists {
				return ErrInvalidProductInput
			}
			seenWarehouses[inventory.WarehouseID] = struct{}{}
		}
	}
	return nil
}

func validProductCredential(accessToken, shopCipher string) bool {
	return strings.TrimSpace(accessToken) != "" && strings.TrimSpace(shopCipher) != ""
}

func validTikTokResourceID(value string) bool {
	return tiktokResourceIDPattern.MatchString(strings.TrimSpace(value))
}

func normalizeTikTokResourceIDs(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if !validTikTokResourceID(value) {
			return nil
		}
		if _, exists := seen[value]; exists {
			return nil
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func validateProducts(products []Product, max int) error {
	if len(products) > max {
		return ErrInvalidProductResponse
	}
	seenProducts := make(map[string]struct{}, len(products))
	for _, product := range products {
		if !validTikTokResourceID(product.ID) || strings.TrimSpace(product.Title) == "" {
			return ErrInvalidProductResponse
		}
		if _, exists := seenProducts[product.ID]; exists {
			return ErrInvalidProductResponse
		}
		seenProducts[product.ID] = struct{}{}
		seenSKUs := make(map[string]struct{}, len(product.SKUs))
		for _, sku := range product.SKUs {
			if !validTikTokResourceID(sku.ID) {
				return ErrInvalidProductResponse
			}
			if _, exists := seenSKUs[sku.ID]; exists {
				return ErrInvalidProductResponse
			}
			seenSKUs[sku.ID] = struct{}{}
			for _, inventory := range sku.Inventory {
				if !validTikTokResourceID(inventory.WarehouseID) || inventory.Quantity < 0 {
					return ErrInvalidProductResponse
				}
			}
		}
	}
	return nil
}

func validateInventorySearchResult(result InventorySearchResult, input InventorySearchRequest) error {
	requestedSKUs := make(map[string]struct{}, len(input.SKUIDs))
	for _, id := range input.SKUIDs {
		requestedSKUs[id] = struct{}{}
	}
	for _, product := range result.Inventory {
		if !validTikTokResourceID(product.ProductID) {
			return ErrInvalidProductResponse
		}
		for _, sku := range product.SKUs {
			if !validTikTokResourceID(sku.ID) || sku.TotalAvailableQuantity < 0 || sku.TotalCommittedQuantity < 0 {
				return ErrInvalidProductResponse
			}
			if len(requestedSKUs) > 0 {
				if _, ok := requestedSKUs[sku.ID]; !ok {
					return ErrInvalidProductResponse
				}
			}
			for _, warehouse := range sku.WarehouseInventory {
				if !validTikTokResourceID(warehouse.WarehouseID) || warehouse.AvailableQuantity < 0 || warehouse.CommittedQuantity < 0 {
					return ErrInvalidProductResponse
				}
			}
		}
	}
	return nil
}

func validInventoryUpdateErrors(updateErrors []InventoryUpdateError) bool {
	for _, updateError := range updateErrors {
		if updateError.Code == 0 || strings.TrimSpace(updateError.Message) == "" || !validTikTokResourceID(updateError.Detail.SKUID) {
			return false
		}
		for _, extra := range updateError.Detail.ExtraErrors {
			if extra.Code == 0 || strings.TrimSpace(extra.Message) == "" || strings.TrimSpace(extra.WarehouseID) == "" {
				return false
			}
		}
	}
	return true
}
