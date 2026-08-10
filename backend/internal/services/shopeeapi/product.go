package shopeeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const (
	PathProductGetItemList     = "/api/v2/product/get_item_list"
	PathProductGetItemBaseInfo = "/api/v2/product/get_item_base_info"
	PathProductGetModelList    = "/api/v2/product/get_model_list"
	PathShopGetWarehouseDetail = "/api/v2/shop/get_warehouse_detail"
	PathProductUpdateStock     = "/api/v2/product/update_stock"
)

type StringID string

func (id *StringID) UnmarshalJSON(data []byte) error {
	raw := bytes.TrimSpace(data)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		*id = ""
		return nil
	}
	if raw[0] == '"' {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		*id = StringID(strings.TrimSpace(value))
		return nil
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil {
		return fmt.Errorf("invalid identifier: %w", err)
	}
	*id = StringID(number.String())
	return nil
}

type ItemListRequest struct {
	Offset         int      `json:"offset"`
	PageSize       int      `json:"page_size"`
	ItemStatuses   []string `json:"item_status,omitempty"`
	UpdateTimeFrom int64    `json:"update_time_from,omitempty"`
	UpdateTimeTo   int64    `json:"update_time_to,omitempty"`
}

type ItemListEntry struct {
	ItemID     int64  `json:"item_id"`
	ItemStatus string `json:"item_status"`
	UpdateTime int64  `json:"update_time"`
}

type ItemListResponse struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Response  struct {
		Item        []ItemListEntry `json:"item"`
		TotalCount  int             `json:"total_count"`
		HasNextPage bool            `json:"has_next_page"`
		NextOffset  int             `json:"next_offset"`
	} `json:"response"`
}

type SellerStock struct {
	LocationID StringID `json:"location_id,omitempty"`
	Stock      int64    `json:"stock"`
}

type StockInfoV2 struct {
	SummaryInfo struct {
		TotalReservedStock  int64 `json:"total_reserved_stock"`
		TotalAvailableStock int64 `json:"total_available_stock"`
	} `json:"summary_info"`
	SellerStock []SellerStock `json:"seller_stock"`
}

func SellerStockAtLocation(info StockInfoV2, locationID StringID) (int64, bool) {
	if len(info.SellerStock) == 0 {
		return 0, false
	}
	if locationID == "" {
		var total int64
		for _, item := range info.SellerStock {
			total += item.Stock
		}
		return total, true
	}
	for _, item := range info.SellerStock {
		if item.LocationID == locationID {
			return item.Stock, true
		}
	}
	return 0, false
}

func CurrentSellerStock(info StockInfoV2, locationID StringID) int64 {
	if stock, ok := SellerStockAtLocation(info, locationID); ok {
		return stock
	}
	// Older shops may omit seller_stock. Preserve the readable summary as a
	// compatibility fallback, but write read-back requires seller_stock.
	return info.SummaryInfo.TotalAvailableStock
}

type ItemBaseInfo struct {
	ItemID      int64       `json:"item_id"`
	ItemName    string      `json:"item_name"`
	ItemSKU     string      `json:"item_sku"`
	ItemStatus  string      `json:"item_status"`
	HasModel    bool        `json:"has_model"`
	UpdateTime  int64       `json:"update_time"`
	StockInfoV2 StockInfoV2 `json:"stock_info_v2"`
}

type ItemBaseInfoResponse struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Response  struct {
		ItemList []ItemBaseInfo `json:"item_list"`
	} `json:"response"`
}

type ProductModel struct {
	ModelID     int64       `json:"model_id"`
	ModelName   string      `json:"model_name"`
	ModelSKU    string      `json:"model_sku"`
	ModelStatus string      `json:"model_status"`
	StockInfoV2 StockInfoV2 `json:"stock_info_v2"`
}

type ModelListResponse struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Response  struct {
		Model []ProductModel `json:"model"`
	} `json:"response"`
}

type WarehouseDetail struct {
	LocationID    StringID `json:"location_id"`
	WarehouseID   StringID `json:"warehouse_id"`
	WarehouseName string   `json:"warehouse_name"`
	AddressID     StringID `json:"address_id"`
	Region        string   `json:"region"`
	State         string   `json:"state"`
	City          string   `json:"city"`
	District      string   `json:"district"`
	Town          string   `json:"town"`
	Address       string   `json:"address"`
	Zipcode       string   `json:"zipcode"`
}

type WarehouseDetailResponse struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Response  struct {
		WarehouseList []WarehouseDetail `json:"warehouse_list"`
		LocationList  []WarehouseDetail `json:"location_list"`
	} `json:"response"`
}

func (r *WarehouseDetailResponse) Locations() []WarehouseDetail {
	if r == nil {
		return nil
	}
	if len(r.Response.LocationList) > 0 {
		return r.Response.LocationList
	}
	return r.Response.WarehouseList
}

type ModelStock struct {
	ModelID     int64         `json:"model_id"`
	SellerStock []SellerStock `json:"seller_stock"`
}

type UpdateStockRequest struct {
	ItemID    int64        `json:"item_id"`
	StockList []ModelStock `json:"stock_list"`
}

type StockUpdateSuccess struct {
	ModelID int64 `json:"model_id"`
}

type StockUpdateFailure struct {
	ModelID      int64  `json:"model_id"`
	FailedReason string `json:"failed_reason"`
}

type UpdateStockResponse struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Response  struct {
		SuccessList []StockUpdateSuccess `json:"success_list"`
		FailureList []StockUpdateFailure `json:"failure_list"`
	} `json:"response"`
}

func (c *Client) GetItemList(ctx context.Context, accessToken string, shopID int64, req ItemListRequest) (*ItemListResponse, error) {
	q := url.Values{}
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}
	pageSize := req.PageSize
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 100
	}
	q.Set("offset", strconv.Itoa(offset))
	q.Set("page_size", strconv.Itoa(pageSize))
	if len(req.ItemStatuses) > 0 {
		q.Set("item_status", strings.Join(req.ItemStatuses, ","))
	}
	if req.UpdateTimeFrom > 0 {
		q.Set("update_time_from", strconv.FormatInt(req.UpdateTimeFrom, 10))
	}
	if req.UpdateTimeTo > 0 {
		q.Set("update_time_to", strconv.FormatInt(req.UpdateTimeTo, 10))
	}
	var out ItemListResponse
	if err := c.getShop(ctx, PathProductGetItemList, accessToken, shopID, q, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("shopee get_item_list: %s %s", out.Error, out.Message)
	}
	return &out, nil
}

func (c *Client) GetItemBaseInfo(ctx context.Context, accessToken string, shopID int64, itemIDs []int64) (*ItemBaseInfoResponse, error) {
	if len(itemIDs) == 0 || len(itemIDs) > 50 {
		return nil, errors.New("item_id_list must contain 1-50 items")
	}
	values := make([]string, 0, len(itemIDs))
	for _, id := range itemIDs {
		if id <= 0 {
			return nil, errors.New("item_id_list contains an invalid item")
		}
		values = append(values, strconv.FormatInt(id, 10))
	}
	q := url.Values{}
	q.Set("item_id_list", strings.Join(values, ","))
	q.Set("need_tax_info", "false")
	q.Set("need_complaint_policy", "false")
	var out ItemBaseInfoResponse
	if err := c.getShop(ctx, PathProductGetItemBaseInfo, accessToken, shopID, q, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("shopee get_item_base_info: %s %s", out.Error, out.Message)
	}
	return &out, nil
}

func (c *Client) GetModelList(ctx context.Context, accessToken string, shopID, itemID int64) (*ModelListResponse, error) {
	if itemID <= 0 {
		return nil, errors.New("item_id is required")
	}
	q := url.Values{}
	q.Set("item_id", strconv.FormatInt(itemID, 10))
	var out ModelListResponse
	if err := c.getShop(ctx, PathProductGetModelList, accessToken, shopID, q, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("shopee get_model_list: %s %s", out.Error, out.Message)
	}
	return &out, nil
}

func (c *Client) GetWarehouseDetail(ctx context.Context, accessToken string, shopID int64) (*WarehouseDetailResponse, error) {
	var out WarehouseDetailResponse
	if err := c.getShop(ctx, PathShopGetWarehouseDetail, accessToken, shopID, url.Values{}, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("shopee get_warehouse_detail: %s %s", out.Error, out.Message)
	}
	return &out, nil
}

func (c *Client) UpdateStock(ctx context.Context, accessToken string, shopID int64, req UpdateStockRequest) (*UpdateStockResponse, error) {
	if err := ValidateUpdateStockRequest(req); err != nil {
		return nil, err
	}
	var out UpdateStockResponse
	if err := c.postShop(ctx, PathProductUpdateStock, accessToken, shopID, req, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("shopee update_stock: %s %s", out.Error, out.Message)
	}
	return &out, nil
}

func ValidateUpdateStockRequest(req UpdateStockRequest) error {
	if req.ItemID <= 0 {
		return errors.New("item_id is required")
	}
	if len(req.StockList) == 0 || len(req.StockList) > 50 {
		return errors.New("stock_list must contain 1-50 models")
	}
	seenModels := map[int64]struct{}{}
	for _, model := range req.StockList {
		if model.ModelID < 0 {
			return errors.New("model_id must be zero or positive")
		}
		if _, exists := seenModels[model.ModelID]; exists {
			return fmt.Errorf("duplicate model_id %d", model.ModelID)
		}
		seenModels[model.ModelID] = struct{}{}
		if len(model.SellerStock) == 0 {
			return fmt.Errorf("model_id %d must contain seller_stock", model.ModelID)
		}
		seenLocations := map[StringID]struct{}{}
		for _, sellerStock := range model.SellerStock {
			if sellerStock.Stock < 0 {
				return fmt.Errorf("model_id %d stock must not be negative", model.ModelID)
			}
			if _, exists := seenLocations[sellerStock.LocationID]; exists {
				return fmt.Errorf("model_id %d contains a duplicate location_id", model.ModelID)
			}
			seenLocations[sellerStock.LocationID] = struct{}{}
		}
	}
	return nil
}
