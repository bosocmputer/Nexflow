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

// SaleOrderConfig holds parameters for the SML saleorder REST API.
// Mirrors PurchaseOrderConfig — saleorder is the sale-side counterpart that
// posts to /api/v1/ic/sale-orders and uses sale_type instead of buy_type.
type SaleOrderConfig struct {
	BaseURL    string
	GUID       string
	Provider   string
	ConfigFile string
	Database   string
	DocFormat  string // e.g. "SR"
	CustCode   string // customer code (set per-call from channel_defaults)
	SaleCode   string
	BranchCode string
	WHCode     string
	ShelfCode  string
	UnitCode   string
	VATType    int
	VATRate    float64
	DocTime    string
}

// SaleOrderClient is the REST client for SML saleorder API (ใบสั่งขาย).
type SaleOrderClient struct {
	cfg        SaleOrderConfig
	httpClient *http.Client
	logger     *zap.Logger
}

func NewSaleOrderClient(cfg SaleOrderConfig, logger *zap.Logger) *SaleOrderClient {
	return &SaleOrderClient{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		logger:     logger,
	}
}

func (c *SaleOrderClient) headers() map[string]string {
	return map[string]string{
		"guid":           c.cfg.GUID,
		"provider":       c.cfg.Provider,
		"configFileName": c.cfg.ConfigFile,
		"databaseName":   c.cfg.Database,
		"Content-Type":   "application/json; charset=utf-8",
	}
}

// ─── Payload ──────────────────────────────────────────────────────────────────

// SaleOrderItem is one line item on a saleorder. Field names match the v3
// schema verified against SML 248 — note the JSON key is `items` (plural)
// at the payload level but each item has the same per-line shape as
// saleinvoice/purchaseorder details.
type SaleOrderItem struct {
	ItemCode         string  `json:"item_code"`
	ItemName         string  `json:"item_name,omitempty"`
	LineNumber       int     `json:"line_number"`
	IsPremium        int     `json:"is_permium"` // typo intentional (matches SML)
	IsGetPrice       int     `json:"is_get_price"`
	UnitCode         string  `json:"unit_code"`
	WHCode           string  `json:"wh_code"`
	ShelfCode        string  `json:"shelf_code"`
	Qty              float64 `json:"qty"`
	Price            float64 `json:"price"`
	PriceExcludeVAT  float64 `json:"price_exclude_vat"`
	DiscountAmount   float64 `json:"discount_amount"`
	SumAmount        float64 `json:"sum_amount"`
	VATAmount        float64 `json:"vat_amount"`
	TaxType          int     `json:"tax_type"`
	VATType          int     `json:"vat_type"`
	SumAmountExclVAT float64 `json:"sum_amount_exclude_vat"`
}

// SaleOrderPayload is the body for POST /api/v1/ic/sale-orders.
type SaleOrderPayload struct {
	DocNo          string          `json:"doc_no"` // required (non-empty), client-generated
	DocDate        string          `json:"doc_date"`
	DocTime        string          `json:"doc_time"`
	DocRef         string          `json:"doc_ref,omitempty"`
	DocRefDate     string          `json:"doc_ref_date,omitempty"`
	DocFormatCode  string          `json:"doc_format_code"`
	CustCode       string          `json:"cust_code"`
	SaleCode       string          `json:"sale_code"`
	BranchCode     string          `json:"branch_code,omitempty"`
	SaleType       int             `json:"sale_type"`
	VATType        int             `json:"vat_type"`
	VATRate        float64         `json:"vat_rate"`
	TotalValue     float64         `json:"total_value"`
	TotalDiscount  float64         `json:"total_discount"`
	TotalBeforeVAT float64         `json:"total_before_vat"`
	TotalVATValue  float64         `json:"total_vat_value"`
	TotalExceptVAT float64         `json:"total_except_vat"`
	TotalAfterVAT  float64         `json:"total_after_vat"`
	TotalAmount    float64         `json:"total_amount"`
	Items          []SaleOrderItem `json:"items"`
	Remark         string          `json:"remark,omitempty"`
	Remark2        string          `json:"remark_2,omitempty"`
}

// ─── Response ─────────────────────────────────────────────────────────────────

// SaleOrderResponse handles both the v3 success ({status,data.doc_no}) and
// error ({error,message,code}) shapes.
type SaleOrderResponse struct {
	Success bool   `json:"success"`
	Status  string `json:"status"`
	DocNo   string `json:"doc_no"`
	Message string `json:"message"`
	Error   any    `json:"error,omitempty"`
	Code    string `json:"code,omitempty"`
	Data    struct {
		DocNo      string `json:"doc_no"`
		LogStatus  string `json:"log_status,omitempty"`
		LogWarning string `json:"log_warning,omitempty"`
	} `json:"data"`
}

func (r *SaleOrderResponse) IsSuccess() bool {
	return r.Success || r.Status == "success"
}

func (r *SaleOrderResponse) GetDocNo() string {
	if r.Data.DocNo != "" {
		return r.Data.DocNo
	}
	return r.DocNo
}

func (r *SaleOrderResponse) GetMessage() string {
	if r.Message != "" {
		return r.Message
	}
	return apiErrorMessage(r.Error)
}

// ─── Create ───────────────────────────────────────────────────────────────────

// CreateSaleOrder posts a saleorder with built-in retry (3 attempts, 1/3/5s backoff).
// urlOverride: empty = use cfg.BaseURL + default path; absolute URL (http://...) = use as-is;
// path (starting with "/") = use cfg.BaseURL + path.
func (c *SaleOrderClient) CreateSaleOrder(payload SaleOrderPayload, urlOverride string) (int, *SaleOrderResponse, error) {
	body, err := marshalASCII(payload)
	if err != nil {
		return 0, nil, err
	}

	url := resolveSMLURL(c.cfg.BaseURL, "/api/v1/ic/sale-orders", urlOverride)

	backoffs := []time.Duration{0, 1 * time.Second, 3 * time.Second, 5 * time.Second}
	var lastResp *SaleOrderResponse
	var lastStatus int
	var lastErr error

	for attempt, wait := range backoffs {
		if wait > 0 {
			time.Sleep(wait)
		}

		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return 0, nil, err
		}
		for k, v := range c.headers() {
			req.Header.Set(k, v)
		}

		if c.logger != nil {
			c.logger.Info("sml_saleorder_request",
				zap.String("url", url),
				zap.String("doc_no", payload.DocNo),
				zap.String("doc_date", payload.DocDate),
				zap.Int("items_count", len(payload.Items)),
				zap.Int("attempt", attempt+1),
			)
		}

		start := time.Now()
		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("sml saleorder: %w", err)
			if c.logger != nil {
				c.logger.Warn("sml_saleorder_failed",
					zap.Error(err),
					zap.Int("attempt", attempt+1),
				)
			}
			continue
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		durMs := time.Since(start).Milliseconds()

		var r SaleOrderResponse
		_ = json.Unmarshal(respBody, &r)
		lastResp = &r
		lastStatus = resp.StatusCode
		lastErr = nil

		if r.IsSuccess() {
			if c.logger != nil {
				c.logger.Info("sml_saleorder_response",
					zap.Int("status_code", resp.StatusCode),
					zap.String("doc_no", r.GetDocNo()),
					zap.Int64("duration_ms", durMs),
				)
			}
			return resp.StatusCode, &r, nil
		}

		if c.logger != nil {
			c.logger.Warn("sml_saleorder_response_failed",
				zap.Int("status_code", resp.StatusCode),
				zap.String("message", r.Message),
				zap.String("body", string(respBody)),
				zap.Int64("duration_ms", durMs),
				zap.Int("attempt", attempt+1),
			)
		}

		// HTTP 4xx (other than 429) → don't retry
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 429 {
			break
		}
	}

	return lastStatus, lastResp, lastErr
}

// ─── Builder ──────────────────────────────────────────────────────────────────

// SOItem is one parsed line item from an upstream source (Shopee Excel, etc.)
type SOItem struct {
	ItemCode  string // resolved SML code (post-mapping)
	ItemName  string // human-readable name (for SML display)
	Qty       float64
	Price     float64 // per-unit price
	UnitCode  string  // resolved unit; falls back to cfg.UnitCode if empty
	WHCode    string  // resolved warehouse; falls back to cfg.WHCode
	ShelfCode string  // resolved shelf; falls back to cfg.ShelfCode
}

type SaleOrderHeaderOptions struct {
	Remark2 string
}

// BuildSaleOrderPayload mirrors BuildPurchaseOrderPayload (same VAT math via
// CalcItemVAT, just emitting `items` instead of `details` and sale_type=0).
func BuildSaleOrderPayload(
	docNo string,
	docDate string,
	docRef string,
	docRefDate string,
	items []SOItem,
	cfg SaleOrderConfig,
	remark string,
	opts ...SaleOrderHeaderOptions,
) SaleOrderPayload {
	var lineItems []SaleOrderItem
	var totalValue, totalVAT, totalExc float64
	var header SaleOrderHeaderOptions
	if len(opts) > 0 {
		header = opts[0]
	}

	for i, item := range items {
		unit := item.UnitCode
		if unit == "" {
			unit = cfg.UnitCode
		}
		wh := item.WHCode
		if wh == "" {
			wh = cfg.WHCode
		}
		shelf := item.ShelfCode
		if shelf == "" {
			shelf = cfg.ShelfCode
		}

		v := CalcItemVAT(item.Price, item.Qty, cfg.VATType, cfg.VATRate)
		totalValue += v.SumAmount
		totalVAT += v.VATAmount
		totalExc += v.SumAmountExclVAT

		lineItems = append(lineItems, SaleOrderItem{
			ItemCode:         item.ItemCode,
			ItemName:         item.ItemName,
			LineNumber:       i,
			IsPremium:        0,
			IsGetPrice:       1,
			UnitCode:         unit,
			WHCode:           wh,
			ShelfCode:        shelf,
			Qty:              item.Qty,
			Price:            round2(item.Price),
			PriceExcludeVAT:  roundN(v.PriceExcludeVAT, 4),
			DiscountAmount:   0,
			SumAmount:        round2(v.SumAmount),
			VATAmount:        round2(v.VATAmount),
			TaxType:          0,
			VATType:          cfg.VATType,
			SumAmountExclVAT: round2(v.SumAmountExclVAT),
		})
	}

	totalValue = round2(totalValue)
	totalVAT = round2(totalVAT)
	totalExc = round2(totalExc)

	var totalBeforeVAT, totalAfterVAT, totalAmount float64
	switch cfg.VATType {
	case 1:
		totalBeforeVAT = totalExc
		totalAfterVAT = totalValue
		totalAmount = totalValue
	case 2:
		totalBeforeVAT = totalValue
		totalAfterVAT = totalValue
		totalAmount = totalValue
	default:
		totalBeforeVAT = totalValue
		totalAfterVAT = round2(totalValue + totalVAT)
		totalAmount = totalAfterVAT
	}

	return SaleOrderPayload{
		DocNo:          docNo,
		DocDate:        docDate,
		DocTime:        cfg.DocTime,
		DocRef:         docRef,
		DocRefDate:     docRefDate,
		DocFormatCode:  cfg.DocFormat,
		CustCode:       cfg.CustCode,
		SaleCode:       cfg.SaleCode,
		BranchCode:     cfg.BranchCode,
		SaleType:       0,
		VATType:        cfg.VATType,
		VATRate:        cfg.VATRate,
		TotalValue:     totalValue,
		TotalDiscount:  0,
		TotalBeforeVAT: totalBeforeVAT,
		TotalVATValue:  totalVAT,
		TotalExceptVAT: 0,
		TotalAfterVAT:  totalAfterVAT,
		TotalAmount:    totalAmount,
		Items:          lineItems,
		Remark:         remark,
		Remark2:        header.Remark2,
	}
}
