package sml

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxStockAPIResponseBytes = 32 << 20

type StockSyncClient struct {
	cfg        PartyConfig
	httpClient *http.Client
}

func NewStockSyncClient(cfg PartyConfig) *StockSyncClient {
	return &StockSyncClient{cfg: cfg, httpClient: &http.Client{Timeout: 15 * time.Second}}
}

func (c *StockSyncClient) WithHTTPClient(client *http.Client) *StockSyncClient {
	if client != nil {
		c.httpClient = client
	}
	return c
}

func (c *StockSyncClient) IsConfigured() bool {
	return c != nil && strings.TrimSpace(c.cfg.BaseURL) != "" && strings.TrimSpace(c.cfg.GUID) != "" && strings.TrimSpace(c.cfg.Database) != ""
}

type StockLocationPair struct {
	Warehouse string `json:"warehouse"`
	Location  string `json:"location"`
}

type StockLocation struct {
	WarehouseCode string `json:"warehouse_code"`
	WarehouseName string `json:"warehouse_name"`
	LocationCode  string `json:"location_code"`
	LocationName  string `json:"location_name"`
}

type StockLocationDiagnostic struct {
	Warehouse string  `json:"warehouse"`
	Location  string  `json:"location"`
	Balance   float64 `json:"balance_qty"`
	Code      string  `json:"code"`
}

type StockLocationsResponse struct {
	AsOfDate    string                    `json:"as_of_date"`
	Locations   []StockLocation           `json:"locations"`
	Diagnostics []StockLocationDiagnostic `json:"diagnostics"`
	CheckedAt   string                    `json:"checked_at"`
}

type StockCatalogUnit struct {
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	StandValue  float64 `json:"stand_value"`
	DivideValue float64 `json:"divide_value"`
	Ratio       float64 `json:"ratio"`
	RowOrder    int     `json:"row_order"`
	LineNumber  int     `json:"line_number"`
}

type StockCatalogBarcode struct {
	Barcode   string `json:"barcode"`
	UnitCode  string `json:"unit_code"`
	Warehouse string `json:"warehouse"`
	Location  string `json:"location"`
}

type StockCatalogItem struct {
	ItemCode     string                `json:"item_code"`
	ItemName     string                `json:"item_name"`
	StandardUnit string                `json:"standard_unit"`
	UpdatedAt    time.Time             `json:"updated_at"`
	Units        []StockCatalogUnit    `json:"units"`
	Barcodes     []StockCatalogBarcode `json:"barcodes"`
}

type StockCatalogPage struct {
	Items []StockCatalogItem
	Total int
	Page  int
	Size  int
}

type StockBalanceScopeRequest struct {
	ScopeID   string              `json:"scope_id"`
	ItemCodes []string            `json:"item_codes"`
	ScopeMode string              `json:"scope_mode"`
	Locations []StockLocationPair `json:"locations,omitempty"`
}

type StockBalanceBatchRequest struct {
	AsOfDate string                     `json:"as_of_date"`
	Scopes   []StockBalanceScopeRequest `json:"scopes"`
}

type StockBalanceItem struct {
	ItemCode           string  `json:"item_code"`
	ItemName           string  `json:"item_name"`
	UnitCode           string  `json:"unit_code"`
	RawBalanceQty      float64 `json:"raw_balance_qty"`
	BalanceQty         float64 `json:"balance_qty"`
	ExcludedBalanceQty float64 `json:"excluded_balance_qty"`
	MinQty             float64 `json:"min_qty"`
	MaxQty             float64 `json:"max_qty"`
	NegativeClamped    bool    `json:"negative_clamped"`
}

type StockBalanceScopeResult struct {
	ScopeID string             `json:"scope_id"`
	Items   []StockBalanceItem `json:"items"`
}

type StockBalanceBatchResponse struct {
	AsOfDate  string                    `json:"as_of_date"`
	Scopes    []StockBalanceScopeResult `json:"scopes"`
	CheckedAt string                    `json:"checked_at"`
}

type stockAPIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type stockAPIEnvelope[T any] struct {
	Success bool `json:"success"`
	Data    T    `json:"data"`
	Meta    struct {
		Total int `json:"total"`
		Page  int `json:"page"`
		Size  int `json:"size"`
	} `json:"meta"`
	Error *stockAPIError `json:"error,omitempty"`
}

func (c *StockSyncClient) Locations(ctx context.Context, asOfDate string) (*StockLocationsResponse, error) {
	var envelope stockAPIEnvelope[StockLocationsResponse]
	path := "/api/v1/ic/stock-locations?as_of_date=" + url.QueryEscape(strings.TrimSpace(asOfDate))
	if err := c.do(ctx, http.MethodGet, path, nil, &envelope); err != nil {
		return nil, err
	}
	return &envelope.Data, nil
}

func (c *StockSyncClient) Catalog(ctx context.Context, page, size int) (*StockCatalogPage, error) {
	return c.CatalogRange(ctx, page, size, nil, nil)
}

func (c *StockSyncClient) CatalogRange(ctx context.Context, page, size int, updatedFrom, updatedTo *time.Time) (*StockCatalogPage, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 500 {
		size = 500
	}
	var envelope stockAPIEnvelope[[]StockCatalogItem]
	path := "/api/v1/ic/stock-catalog?page=" + strconv.Itoa(page) + "&size=" + strconv.Itoa(size)
	if updatedFrom != nil {
		path += "&updated_from=" + url.QueryEscape(updatedFrom.UTC().Format(time.RFC3339))
	}
	if updatedTo != nil {
		path += "&updated_to=" + url.QueryEscape(updatedTo.UTC().Format(time.RFC3339))
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &envelope); err != nil {
		return nil, err
	}
	if envelope.Data == nil {
		envelope.Data = []StockCatalogItem{}
	}
	return &StockCatalogPage{Items: envelope.Data, Total: envelope.Meta.Total, Page: envelope.Meta.Page, Size: envelope.Meta.Size}, nil
}

func (c *StockSyncClient) BalancesBatch(ctx context.Context, request StockBalanceBatchRequest) (*StockBalanceBatchResponse, error) {
	var envelope stockAPIEnvelope[StockBalanceBatchResponse]
	if err := c.do(ctx, http.MethodPost, "/api/v1/ic/stock-balances/batch", request, &envelope); err != nil {
		return nil, err
	}
	return &envelope.Data, nil
}

func (c *StockSyncClient) do(ctx context.Context, method, path string, payload any, out any) error {
	if !c.IsConfigured() {
		return fmt.Errorf("sml stock client is not configured")
	}
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode SML stock request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.cfg.BaseURL, "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("X-Tenant", c.cfg.Database)
	req.Header.Set("X-Api-Key", c.cfg.GUID)
	req.Header.Set("guid", c.cfg.GUID)
	req.Header.Set("provider", c.cfg.Provider)
	req.Header.Set("configFileName", c.cfg.ConfigFile)
	req.Header.Set("databaseName", c.cfg.Database)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("SML stock request: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxStockAPIResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read SML stock response: %w", err)
	}
	if len(responseBody) > maxStockAPIResponseBytes {
		return fmt.Errorf("SML stock response exceeds %d bytes", maxStockAPIResponseBytes)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("SML stock HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	if err := json.Unmarshal(responseBody, out); err != nil {
		return fmt.Errorf("decode SML stock response: %w", err)
	}
	switch envelope := out.(type) {
	case *stockAPIEnvelope[StockLocationsResponse]:
		if !envelope.Success {
			return stockEnvelopeError(envelope.Error)
		}
	case *stockAPIEnvelope[[]StockCatalogItem]:
		if !envelope.Success {
			return stockEnvelopeError(envelope.Error)
		}
	case *stockAPIEnvelope[StockBalanceBatchResponse]:
		if !envelope.Success {
			return stockEnvelopeError(envelope.Error)
		}
	}
	return nil
}

func stockEnvelopeError(apiErr *stockAPIError) error {
	if apiErr == nil {
		return fmt.Errorf("SML stock API returned success=false")
	}
	return fmt.Errorf("SML stock %s: %s", apiErr.Code, apiErr.Message)
}
