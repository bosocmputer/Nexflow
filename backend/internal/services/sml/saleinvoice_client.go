package sml

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
)

// InvoiceConfig holds all parameters for the SML saleinvoice REST API (SML 224).
type InvoiceConfig struct {
	BaseURL    string  // e.g. http://192.168.2.224:8080
	GUID       string  // guid header
	Provider   string  // provider header
	ConfigFile string  // configFileName header
	Database   string  // databaseName header
	DocFormat  string  // doc_format_code (e.g. RU)
	CustCode   string  // default cust_code for all Shopee orders
	SaleCode   string  // sale_code (optional)
	BranchCode string  // branch_code (required for fiscal period lookup, e.g. "001")
	WHCode     string  // fallback warehouse
	ShelfCode  string  // fallback shelf
	UnitCode   string  // fallback unit
	VATType    int     // 0=แยกนอก, 1=รวมใน, 2=ศูนย์%
	VATRate    float64 // e.g. 7
	DocTime    string  // e.g. "09:00"
}

// InvoiceClient is the REST client for SML saleinvoice API.
type InvoiceClient struct {
	cfg        InvoiceConfig
	httpClient *http.Client
	logger     *zap.Logger
}

func NewInvoiceClient(cfg InvoiceConfig, logger *zap.Logger) *InvoiceClient {
	return &InvoiceClient{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		logger:     logger,
	}
}

func (c *InvoiceClient) headers() map[string]string {
	return map[string]string{
		"guid":           c.cfg.GUID,
		"provider":       c.cfg.Provider,
		"configFileName": c.cfg.ConfigFile,
		"databaseName":   c.cfg.Database,
		"Content-Type":   "application/json; charset=utf-8",
	}
}

// ─── Product ──────────────────────────────────────────────────────────────────

// ProductInfo holds the fields we need from GET /api/v1/ic/products/{code}
type ProductInfo struct {
	Code           string `json:"code"`
	UnitStandard   string `json:"unit_standard"`
	StartSaleUnit  string `json:"start_sale_unit"`
	StartSaleWH    string `json:"start_sale_wh"`
	StartSaleShelf string `json:"start_sale_shelf"`
}

type productV4Response struct {
	Success bool `json:"success"`
	Data    struct {
		Code             string `json:"code"`
		UnitStandard     string `json:"unit_standard"`
		UnitStandardName string `json:"unit_standard_name"`
		StartSaleUnit    string `json:"start_sale_unit"`
		StartSaleWH      string `json:"start_sale_wh"`
		StartSaleShelf   string `json:"start_sale_shelf"`
	} `json:"data"`
}

// GetProduct fetches product info by SKU/item_code.
// Returns nil (no error) if product is not found (404).
func (c *InvoiceClient) GetProduct(sku string) (*ProductInfo, error) {
	url := fmt.Sprintf("%s/api/v1/ic/products/%s", c.cfg.BaseURL, sku)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range c.headers() {
		req.Header.Set(k, v)
	}
	start := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if c.logger != nil {
			c.logger.Error("sml_product_lookup_failed",
				zap.String("sku", sku),
				zap.Error(err),
			)
		}
		return nil, fmt.Errorf("sml product lookup: %w", err)
	}
	defer resp.Body.Close()

	durMs := time.Since(start).Milliseconds()

	if resp.StatusCode == http.StatusNotFound {
		if c.logger != nil {
			c.logger.Info("sml_product_not_found",
				zap.String("sku", sku),
				zap.Int64("duration_ms", durMs),
			)
		}
		return nil, nil // ไม่พบสินค้า — ไม่ใช่ error
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if c.logger != nil {
			c.logger.Error("sml_product_lookup_failed",
				zap.String("sku", sku),
				zap.Int("status_code", resp.StatusCode),
				zap.Int64("duration_ms", durMs),
			)
		}
		return nil, fmt.Errorf("sml product %s: HTTP %d — %s", sku, resp.StatusCode, string(body))
	}

	var r productV4Response
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("sml product decode: %w", err)
	}
	if !r.Success {
		if c.logger != nil {
			c.logger.Info("sml_product_not_found",
				zap.String("sku", sku),
				zap.Int64("duration_ms", durMs),
			)
		}
		return nil, nil
	}

	if c.logger != nil {
		c.logger.Info("sml_product_found",
			zap.String("sku", sku),
			zap.String("unit", r.Data.UnitStandard),
			zap.Int64("duration_ms", durMs),
		)
	}

	return &ProductInfo{
		Code:           r.Data.Code,
		UnitStandard:   r.Data.UnitStandard,
		StartSaleUnit:  r.Data.StartSaleUnit,
		StartSaleWH:    r.Data.StartSaleWH,
		StartSaleShelf: r.Data.StartSaleShelf,
	}, nil
}

// ─── Invoice ──────────────────────────────────────────────────────────────────

// InvoiceDetail is one line item in the saleinvoice payload.
type InvoiceDetail struct {
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

// InvoicePayload is the full body for POST /api/v1/ic/sale-invoices
type InvoicePayload struct {
	DocNo          string          `json:"doc_no,omitempty"`
	DocDate        string          `json:"doc_date"`
	DocTime        string          `json:"doc_time"`
	DocRef         string          `json:"doc_ref,omitempty"`
	DocRefDate     string          `json:"doc_ref_date,omitempty"`
	DocFormatCode  string          `json:"doc_format_code"`
	CustCode       string          `json:"cust_code"`
	SaleCode       string          `json:"sale_code"` // must always be present (even empty)
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
	CashAmount     float64         `json:"cash_amount"`
	ChqAmount      float64         `json:"chq_amount"`
	CreditAmount   float64         `json:"credit_amount"`
	TransferAmount float64         `json:"tranfer_amount"` // typo intentional (matches SML)
	Details        []InvoiceDetail `json:"details"`
	PayDetails     []interface{}   `json:"paydetails"`
	Remark         string          `json:"remark,omitempty"`
	Remark2        string          `json:"remark_2,omitempty"`
}

// InvoiceResponse handles both old restapi format and new v3 format.
// v3 success: {"status":"success","data":{"doc_no":"..."}}
// v3 error:   {"error":true,"message":"...","code":"..."}
type InvoiceResponse struct {
	Success bool   `json:"success"` // old restapi format
	Status  string `json:"status"`  // v3 format: "success" | "error"
	DocNo   string `json:"doc_no"`  // old restapi format
	Message string `json:"message"`
	Error   any    `json:"error,omitempty"`
	Code    string `json:"code,omitempty"`
	Data    struct {
		DocNo      string `json:"doc_no"`
		LogStatus  string `json:"log_status,omitempty"`
		LogWarning string `json:"log_warning,omitempty"`
	} `json:"data"`
}

// IsSuccess returns true for both old and v3 success responses.
func (r *InvoiceResponse) IsSuccess() bool {
	return r.Success || r.Status == "success"
}

// GetDocNo returns the doc_no from either old or v3 response format.
func (r *InvoiceResponse) GetDocNo() string {
	if r.Data.DocNo != "" {
		return r.Data.DocNo
	}
	return r.DocNo
}

func (r *InvoiceResponse) GetMessage() string {
	if r.Message != "" {
		return r.Message
	}
	return apiErrorMessage(r.Error)
}

// CreateInvoice posts a saleinvoice to SML and returns the response.
// urlOverride: empty = default; absolute URL = as-is; path = cfg.BaseURL + path.
func (c *InvoiceClient) CreateInvoice(payload InvoicePayload, urlOverride string) (int, *InvoiceResponse, error) {
	body, err := marshalASCII(payload)
	if err != nil {
		return 0, nil, err
	}

	url := resolveSMLURL(c.cfg.BaseURL, "/api/v1/ic/sale-invoices", urlOverride)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	for k, v := range c.headers() {
		req.Header.Set(k, v)
	}

	if c.logger != nil {
		c.logger.Info("sml_invoice_request",
			zap.String("url", url),
			zap.String("doc_date", payload.DocDate),
			zap.Int("items_count", len(payload.Details)),
		)
	}

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if c.logger != nil {
			c.logger.Error("sml_invoice_failed",
				zap.String("url", url),
				zap.Error(err),
			)
		}
		return 0, nil, fmt.Errorf("sml saleinvoice: %w", err)
	}
	defer resp.Body.Close()

	durMs := time.Since(start).Milliseconds()
	respBody, _ := io.ReadAll(resp.Body)
	var r InvoiceResponse
	_ = json.Unmarshal(respBody, &r)

	if c.logger != nil {
		if r.IsSuccess() {
			c.logger.Info("sml_invoice_response",
				zap.Int("status_code", resp.StatusCode),
				zap.String("doc_no", r.GetDocNo()),
				zap.Int64("duration_ms", durMs),
			)
		} else {
			c.logger.Warn("sml_invoice_response_failed",
				zap.Int("status_code", resp.StatusCode),
				zap.String("message", r.Message),
				zap.Int64("duration_ms", durMs),
			)
		}
	}

	return resp.StatusCode, &r, nil
}

// ─── VAT Calculator ───────────────────────────────────────────────────────────

// CalcVATResult holds the per-item VAT breakdown.
type CalcVATResult struct {
	PriceExcludeVAT  float64
	SumAmount        float64
	VATAmount        float64
	SumAmountExclVAT float64
}

// CalcItemVAT mirrors the Python calc_item_vat().
// vatType: 0=แยกนอก, 1=รวมใน, 2=ศูนย์%
func CalcItemVAT(price, qty float64, vatType int, vatRate float64) CalcVATResult {
	rate := vatRate / 100
	sumAmount := round2(price * qty)

	var priceExc, vatAmount, sumExc float64
	switch vatType {
	case 1: // รวมใน
		priceExc = roundN(price/(1+rate), 6)
		sumExc = round2(priceExc * qty)
		vatAmount = round2(sumAmount - sumExc)
	case 2: // ศูนย์%
		priceExc = price
		vatAmount = 0
		sumExc = sumAmount
	default: // 0 แยกนอก
		priceExc = price
		vatAmount = round2(sumAmount * rate)
		sumExc = sumAmount
	}

	return CalcVATResult{
		PriceExcludeVAT:  priceExc,
		SumAmount:        sumAmount,
		VATAmount:        vatAmount,
		SumAmountExclVAT: sumExc,
	}
}

// resolveSMLURL combines a base URL with the per-call override.
//   - override = ""  → baseURL + defaultPath
//   - override starts with "http://" or "https://" → use as-is (absolute)
//   - override starts with "/" → baseURL + override (path swap)
//   - anything else → treat as path (prepend "/")
//
// Lets admins on /settings/channels point a channel at any SML host/path
// without redeploying — see handlers/bills.go:resolveEndpoint for keyword
// detection that picks which client to call.
func resolveSMLURL(baseURL, defaultPath, override string) string {
	if override == "" {
		return baseURL + defaultPath
	}
	if strings.HasPrefix(override, "http://") || strings.HasPrefix(override, "https://") {
		return override
	}
	if !strings.HasPrefix(override, "/") {
		override = "/" + override
	}
	return baseURL + override
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
func roundN(v float64, n int) float64 {
	p := math.Pow(10, float64(n))
	return math.Round(v*p) / p
}

// ─── Payload Builder ──────────────────────────────────────────────────────────

// ShopeeOrderItem is one row from the parsed Shopee Excel.
type ShopeeOrderItem struct {
	SKU         string  `json:"sku"`
	ProductName string  `json:"product_name"`
	Price       float64 `json:"price"`
	Qty         float64 `json:"qty"`
}

type InvoiceHeaderOptions struct {
	Remark2 string
}

// BuildInvoicePayload constructs an InvoicePayload from a Shopee order.
// productCache maps sku → *ProductInfo (may be nil if not found).
func BuildInvoicePayload(
	docNo string,
	docDate string,
	docRef string,
	docRefDate string,
	items []ShopeeOrderItem,
	cfg InvoiceConfig,
	productCache map[string]*ProductInfo,
	remark string,
	opts ...InvoiceHeaderOptions,
) InvoicePayload {
	var details []InvoiceDetail
	var totalValue, totalVAT, totalExc float64
	var header InvoiceHeaderOptions
	if len(opts) > 0 {
		header = opts[0]
	}

	for i, item := range items {
		prod := productCache[item.SKU]

		unitCode := cfg.UnitCode
		whCode := cfg.WHCode
		shelfCode := cfg.ShelfCode
		if prod != nil {
			if prod.StartSaleUnit != "" {
				unitCode = prod.StartSaleUnit
			} else if prod.UnitStandard != "" {
				unitCode = prod.UnitStandard
			}
			if prod.StartSaleWH != "" {
				whCode = prod.StartSaleWH
			}
			if prod.StartSaleShelf != "" {
				shelfCode = prod.StartSaleShelf
			}
		}

		v := CalcItemVAT(item.Price, item.Qty, cfg.VATType, cfg.VATRate)
		totalValue += v.SumAmount
		totalVAT += v.VATAmount
		totalExc += v.SumAmountExclVAT

		details = append(details, InvoiceDetail{
			ItemCode:         item.SKU,
			ItemName:         item.ProductName,
			LineNumber:       i,
			IsPremium:        0,
			IsGetPrice:       1,
			UnitCode:         unitCode,
			WHCode:           whCode,
			ShelfCode:        shelfCode,
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
	case 1: // รวมใน
		totalBeforeVAT = totalExc
		totalAfterVAT = totalValue
		totalAmount = totalValue
	case 2: // ศูนย์%
		totalBeforeVAT = totalValue
		totalAfterVAT = totalValue
		totalAmount = totalValue
	default: // 0 แยกนอก
		totalBeforeVAT = totalValue
		totalAfterVAT = round2(totalValue + totalVAT)
		totalAmount = totalAfterVAT
	}

	return InvoicePayload{
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
		CashAmount:     0,
		ChqAmount:      0,
		CreditAmount:   0,
		TransferAmount: 0,
		Details:        details,
		PayDetails:     []interface{}{},
		Remark:         remark,
		Remark2:        header.Remark2,
	}
}
