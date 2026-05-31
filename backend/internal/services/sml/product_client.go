package sml

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// ProductClient creates new SML products via the v3 product API.
// Reuses the same Shopee SML config (URL, GUID, provider, configFileName, databaseName).
type ProductClient struct {
	baseURL    string
	baseHeaders map[string]string
	httpClient *http.Client
	logger     *zap.Logger
}

func NewProductClient(baseURL, guid, provider, configFile, database string, logger *zap.Logger) *ProductClient {
	return &ProductClient{
		baseURL: baseURL,
		baseHeaders: map[string]string{
			"guid":           guid,
			"provider":       provider,
			"configFileName": configFile,
			"databaseName":   database,
			"Content-Type":   "application/json; charset=utf-8",
		},
		httpClient: &http.Client{Timeout: 30 * time.Second},
		logger:     logger,
	}
}

func (c *ProductClient) headers() map[string]string {
	h := make(map[string]string, len(c.baseHeaders))
	for k, v := range c.baseHeaders {
		h[k] = v
	}
	return h
}

// ─── Payload ──────────────────────────────────────────────────────────────────

type ProductUnit struct {
	UnitCode    string  `json:"unit_code"`
	UnitName    string  `json:"unit_name"`
	StandValue  float64 `json:"stand_value"`
	DivideValue float64 `json:"divide_value"`
}

type ProductPriceFormula struct {
	UnitCode      string `json:"unit_code"`
	SaleType      int    `json:"sale_type"`
	Price0        string `json:"price_0"` // SML expects string per the sample request
	TaxType       int    `json:"tax_type"`
	PriceCurrency int    `json:"price_currency"`
}

// CreateProductRequest is the body for POST /api/v1/ic/products.
// Fields use omitempty for optional values so the JSON payload matches what
// SML accepts; required fields are always included.
type CreateProductRequest struct {
	Code          string                `json:"code"`
	Name          string                `json:"name"`
	Units         []ProductUnit         `json:"units"`
	NameEng       string                `json:"name_eng,omitempty"`
	NameEng2      string                `json:"name_eng_2,omitempty"`
	TaxType       int                   `json:"tax_type"`
	ItemType      int                   `json:"item_type"`
	UnitType      int                   `json:"unit_type"`
	UnitCost      string                `json:"unit_cost,omitempty"`
	UnitStandard  string                `json:"unit_standard,omitempty"`
	ItemCategory  string                `json:"item_category,omitempty"`
	CategoryName  string                `json:"category_name,omitempty"`
	GroupMain     string                `json:"group_main,omitempty"`
	GroupMainName string                `json:"group_main_name,omitempty"`
	GroupSub      string                `json:"group_sub,omitempty"`
	PurchasePoint int                   `json:"purchase_point"`
	PriceFormulas []ProductPriceFormula `json:"price_formulas,omitempty"`
}

// CreateProductResponse handles both success and error shapes from the v3 API.
type CreateProductResponse struct {
	Success bool   `json:"success"`
	Error   any    `json:"error,omitempty"`
	Code    string `json:"code,omitempty"` // error code on error path
	Message string `json:"message,omitempty"`
	Data    struct {
		Code string `json:"code"` // SML-assigned product code (may differ from request)
	} `json:"data"`
}

func (r *CreateProductResponse) GetMessage() string {
	if r.Message != "" {
		return r.Message
	}
	return apiErrorMessage(r.Error)
}

// ─── Create ───────────────────────────────────────────────────────────────────

// CreateProduct posts a new product. Single attempt — duplicate-code errors
// must NOT be retried, and most other errors are user-fixable validation issues
// (better to surface them immediately).
func (c *ProductClient) CreateProduct(req CreateProductRequest) (int, *CreateProductResponse, error) {
	body, err := marshalASCII(req)
	if err != nil {
		return 0, nil, err
	}

	url := c.baseURL + "/api/v1/ic/products"
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	for k, v := range c.headers() {
		httpReq.Header.Set(k, v)
	}

	if c.logger != nil {
		c.logger.Info("sml_product_create_request",
			zap.String("url", url),
			zap.String("code", req.Code),
			zap.String("name", req.Name),
			zap.Int("units_count", len(req.Units)),
		)
	}

	start := time.Now()
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if c.logger != nil {
			c.logger.Error("sml_product_create_failed", zap.Error(err))
		}
		return 0, nil, fmt.Errorf("sml product create: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	durMs := time.Since(start).Milliseconds()

	var r CreateProductResponse
	if err := json.Unmarshal(respBody, &r); err != nil {
		// Server returned non-JSON
		if c.logger != nil {
			c.logger.Warn("sml_product_create_decode_failed",
				zap.Int("status_code", resp.StatusCode),
				zap.String("body", string(respBody)),
			)
		}
		return resp.StatusCode, &CreateProductResponse{Message: string(respBody)}, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	if c.logger != nil {
		if r.Success {
			c.logger.Info("sml_product_created",
				zap.Int("status_code", resp.StatusCode),
				zap.String("code", r.Data.Code),
				zap.Int64("duration_ms", durMs),
			)
		} else {
			c.logger.Warn("sml_product_create_rejected",
				zap.Int("status_code", resp.StatusCode),
				zap.String("error_code", r.Code),
				zap.String("message", r.GetMessage()),
				zap.Int64("duration_ms", durMs),
			)
		}
	}

	return resp.StatusCode, &r, nil
}
