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

// PurchaseOrderConfig holds parameters for the SML purchaseorder REST API (v3).
// Most fields mirror InvoiceConfig — the only differences are DocFormat (defaults
// to "PO") and CustCode (semantically the supplier code on a PO).
type PurchaseOrderConfig struct {
	BaseURL      string
	GUID         string
	Provider     string
	ConfigFile   string
	Database     string
	DocFormat    string // e.g. "PO"
	CustCode     string // supplier code on a PO
	SupplierName string
	SaleCode     string
	BranchCode   string
	WHCode       string
	ShelfCode    string
	UnitCode     string
	VATType      int
	VATRate      float64
	DocTime      string
}

// PurchaseOrderClient is the REST client for SML purchaseorder API.
type PurchaseOrderClient struct {
	cfg        PurchaseOrderConfig
	httpClient *http.Client
	logger     *zap.Logger
}

func NewPurchaseOrderClient(cfg PurchaseOrderConfig, logger *zap.Logger) *PurchaseOrderClient {
	return &PurchaseOrderClient{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		logger:     logger,
	}
}

func (c *PurchaseOrderClient) headers() map[string]string {
	return map[string]string{
		"guid":           c.cfg.GUID,
		"provider":       c.cfg.Provider,
		"configFileName": c.cfg.ConfigFile,
		"databaseName":   c.cfg.Database,
		"Content-Type":   "application/json; charset=utf-8",
	}
}

// ─── Payload ──────────────────────────────────────────────────────────────────

// PurchaseOrderDetail mirrors InvoiceDetail one-for-one (the v3 PO endpoint
// accepts the same per-line shape as saleinvoice). If SML rejects any field
// for purchase orders we'll iterate based on actual error responses.
type PurchaseOrderDetail struct {
	DocRef           string  `json:"doc_ref,omitempty"`
	ItemCode         string  `json:"item_code"`
	ItemName         string  `json:"item_name,omitempty"`
	LineNumber       int     `json:"line_number"`
	IsPremium        int     `json:"is_permium"` // typo intentional (matches SML)
	IsGetPrice       int     `json:"is_get_price"`
	UnitCode         string  `json:"unit_code"`
	WHCode           string  `json:"wh_code"`
	ShelfCode        string  `json:"shelf_code"`
	WHCode2          string  `json:"wh_code_2,omitempty"`
	ShelfCode2       string  `json:"shelf_code_2,omitempty"`
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

// PurchaseOrderPayload is the body for POST /api/v1/ic/purchase-orders
type PurchaseOrderPayload struct {
	DocNo          string                `json:"doc_no,omitempty"` // empty → SML generates
	ApproveStatus  int                   `json:"approve_status"`
	NotApprove1    int                   `json:"not_approve_1"`
	UserApprove    string                `json:"user_approve"`
	UserRequest    string                `json:"user_request"`
	DocDate        string                `json:"doc_date"`
	DocTime        string                `json:"doc_time"`
	DocRef         string                `json:"doc_ref,omitempty"`
	DocRefDate     string                `json:"doc_ref_date,omitempty"`
	DocFormatCode  string                `json:"doc_format_code"`
	CustCode       string                `json:"cust_code"` // supplier on a PO
	SupplierName   string                `json:"supplier_name,omitempty"`
	SaleCode       string                `json:"sale_code"`
	BranchCode     string                `json:"branch_code"`
	WHCode         string                `json:"wh_code,omitempty"`
	ShelfCode      string                `json:"shelf_code,omitempty"`
	WHFrom         string                `json:"wh_from,omitempty"`
	LocationFrom   string                `json:"location_from,omitempty"`
	BuyType        int                   `json:"buy_type"` // 0 (PO equivalent of sale_type)
	InquiryType    int                   `json:"inquiry_type"`
	VATType        int                   `json:"vat_type"`
	VATRate        float64               `json:"vat_rate"`
	TotalValue     float64               `json:"total_value"`
	TotalDiscount  float64               `json:"total_discount"`
	TotalBeforeVAT float64               `json:"total_before_vat"`
	TotalVATValue  float64               `json:"total_vat_value"`
	TotalExceptVAT float64               `json:"total_except_vat"`
	TotalAfterVAT  float64               `json:"total_after_vat"`
	TotalAmount    float64               `json:"total_amount"`
	CashAmount     float64               `json:"cash_amount"`
	ChqAmount      float64               `json:"chq_amount"`
	CreditAmount   float64               `json:"credit_amount"`
	TransferAmount float64               `json:"tranfer_amount"` // typo intentional
	Items          []PurchaseOrderDetail `json:"items"`
	PayDetails     []interface{}         `json:"paydetails"`
	Remark         string                `json:"remark,omitempty"`
	Remark2        string                `json:"remark_2,omitempty"`
	Remark5        string                `json:"remark_5,omitempty"`
}

// ─── Response ─────────────────────────────────────────────────────────────────

// PurchaseOrderResponse handles both v3 and legacy formats.
type PurchaseOrderResponse struct {
	Success bool   `json:"success"`
	Status  string `json:"status"` // v3: "success" | "error"
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

func (r *PurchaseOrderResponse) IsSuccess() bool {
	return r.Success || r.Status == "success"
}

func (r *PurchaseOrderResponse) GetDocNo() string {
	if r.Data.DocNo != "" {
		return r.Data.DocNo
	}
	return r.DocNo
}

func (r *PurchaseOrderResponse) GetMessage() string {
	if r.Message != "" {
		return r.Message
	}
	return apiErrorMessage(r.Error)
}

// ─── Create ───────────────────────────────────────────────────────────────────

// CreatePurchaseOrder posts a PO with built-in retry (3 attempts, backoff 1/3/5s).
// urlOverride: empty = default; absolute URL = as-is; path = cfg.BaseURL + path.
func (c *PurchaseOrderClient) CreatePurchaseOrder(payload PurchaseOrderPayload, urlOverride string) (int, *PurchaseOrderResponse, error) {
	body, err := marshalASCII(payload)
	if err != nil {
		return 0, nil, err
	}

	url := resolveSMLURL(c.cfg.BaseURL, "/api/v1/ic/purchase-orders", urlOverride)

	backoffs := []time.Duration{0, 1 * time.Second, 3 * time.Second, 5 * time.Second}
	var lastResp *PurchaseOrderResponse
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
			c.logger.Info("sml_purchaseorder_request",
				zap.String("url", url),
				zap.String("doc_date", payload.DocDate),
				zap.Int("items_count", len(payload.Items)),
				zap.Int("attempt", attempt+1),
			)
		}

		start := time.Now()
		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("sml purchaseorder: %w", err)
			if c.logger != nil {
				c.logger.Warn("sml_purchaseorder_failed",
					zap.String("url", url),
					zap.Error(err),
					zap.Int("attempt", attempt+1),
				)
			}
			continue
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		durMs := time.Since(start).Milliseconds()

		var r PurchaseOrderResponse
		_ = json.Unmarshal(respBody, &r)
		lastResp = &r
		lastStatus = resp.StatusCode
		lastErr = nil

		if r.IsSuccess() {
			if c.logger != nil {
				c.logger.Info("sml_purchaseorder_response",
					zap.Int("status_code", resp.StatusCode),
					zap.String("doc_no", r.GetDocNo()),
					zap.Int64("duration_ms", durMs),
				)
			}
			return resp.StatusCode, &r, nil
		}

		if c.logger != nil {
			c.logger.Warn("sml_purchaseorder_response_failed",
				zap.Int("status_code", resp.StatusCode),
				zap.String("message", r.Message),
				zap.String("body", string(respBody)),
				zap.Int64("duration_ms", durMs),
				zap.Int("attempt", attempt+1),
			)
		}

		// HTTP 4xx (other than 429) → don't retry; the server rejected the payload
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 429 {
			break
		}
	}

	return lastStatus, lastResp, lastErr
}

// ─── Builder ──────────────────────────────────────────────────────────────────

// POItem is one parsed line item from an upstream source (Shopee email, etc.)
type POItem struct {
	ItemCode       string // resolved SML code (post-mapping)
	ItemName       string // human-readable name (for SML display)
	Qty            float64
	Price          float64 // per-unit price before line discount
	DiscountAmount float64 // total discount for this line
	UnitCode       string  // resolved unit (post-mapping); falls back to cfg.UnitCode if empty
	WHCode         string  // resolved warehouse; falls back to cfg.WHCode if empty
	ShelfCode      string  // resolved shelf; falls back to cfg.ShelfCode if empty
}

type PurchaseOrderHeaderOptions struct {
	Remark      string
	Remark2     string
	Remark5     string
	InquiryType int
}

func clampLineDiscount(discount, grossAmount float64) float64 {
	discount = round2(discount)
	grossAmount = round2(grossAmount)
	if discount < 0 {
		return 0
	}
	if discount > grossAmount {
		return grossAmount
	}
	return discount
}

// BuildPurchaseOrderPayload mirrors BuildInvoicePayload structure.
// VAT calculation reuses CalcItemVAT (saleinvoice_client.go).
func BuildPurchaseOrderPayload(
	docNo string,
	docDate string,
	docRef string,
	docRefDate string,
	items []POItem,
	cfg PurchaseOrderConfig,
	remark string,
	opts ...PurchaseOrderHeaderOptions,
) PurchaseOrderPayload {
	var details []PurchaseOrderDetail
	var totalValue, totalDiscount, totalNet, totalVAT, totalExc float64
	header := PurchaseOrderHeaderOptions{
		Remark:      remark,
		InquiryType: 0,
	}
	if len(opts) > 0 {
		header = opts[0]
		if header.Remark == "" {
			header.Remark = remark
		}
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

		grossAmount := round2(item.Price * item.Qty)
		discountAmount := clampLineDiscount(item.DiscountAmount, grossAmount)
		netAmount := round2(grossAmount - discountAmount)
		v := CalcItemVAT(netAmount, 1, cfg.VATType, cfg.VATRate)
		unitVAT := CalcItemVAT(item.Price, 1, cfg.VATType, cfg.VATRate)
		totalValue += v.SumAmount
		totalNet += v.SumAmount
		totalVAT += v.VATAmount
		totalExc += v.SumAmountExclVAT

		details = append(details, PurchaseOrderDetail{
			DocRef:           docRef,
			ItemCode:         item.ItemCode,
			ItemName:         item.ItemName,
			LineNumber:       i,
			IsPremium:        0,
			IsGetPrice:       1,
			UnitCode:         unit,
			WHCode:           wh,
			ShelfCode:        shelf,
			WHCode2:          wh,
			ShelfCode2:       shelf,
			Qty:              item.Qty,
			Price:            round2(item.Price),
			PriceExcludeVAT:  roundN(unitVAT.PriceExcludeVAT, 4),
			DiscountAmount:   discountAmount,
			SumAmount:        round2(v.SumAmount),
			VATAmount:        round2(v.VATAmount),
			TaxType:          0,
			VATType:          cfg.VATType,
			SumAmountExclVAT: round2(v.SumAmountExclVAT),
		})
	}

	totalValue = round2(totalValue)
	totalDiscount = round2(totalDiscount)
	totalNet = round2(totalNet)
	totalVAT = round2(totalVAT)
	totalExc = round2(totalExc)

	var totalBeforeVAT, totalAfterVAT, totalAmount float64
	switch cfg.VATType {
	case 1:
		totalBeforeVAT = totalExc
		totalAfterVAT = totalNet
		totalAmount = totalNet
	case 2:
		totalBeforeVAT = totalNet
		totalAfterVAT = totalNet
		totalAmount = totalNet
	default:
		totalBeforeVAT = totalNet
		totalAfterVAT = round2(totalNet + totalVAT)
		totalAmount = totalAfterVAT
	}

	return PurchaseOrderPayload{
		DocNo:          docNo,
		ApproveStatus:  0,
		NotApprove1:    0,
		UserApprove:    "",
		UserRequest:    "",
		DocDate:        docDate,
		DocTime:        cfg.DocTime,
		DocRef:         docRef,
		DocRefDate:     docRefDate,
		DocFormatCode:  cfg.DocFormat,
		CustCode:       cfg.CustCode,
		SupplierName:   cfg.SupplierName,
		SaleCode:       cfg.SaleCode,
		BranchCode:     cfg.BranchCode,
		WHCode:         cfg.WHCode,
		ShelfCode:      cfg.ShelfCode,
		WHFrom:         cfg.WHCode,
		LocationFrom:   cfg.ShelfCode,
		BuyType:        0,
		InquiryType:    header.InquiryType,
		VATType:        cfg.VATType,
		VATRate:        cfg.VATRate,
		TotalValue:     totalValue,
		TotalDiscount:  totalDiscount,
		TotalBeforeVAT: totalBeforeVAT,
		TotalVATValue:  totalVAT,
		TotalExceptVAT: 0,
		TotalAfterVAT:  totalAfterVAT,
		TotalAmount:    totalAmount,
		CashAmount:     0,
		ChqAmount:      0,
		CreditAmount:   0,
		TransferAmount: 0,
		Items:          details,
		PayDetails:     []interface{}{},
		Remark:         header.Remark,
		Remark2:        header.Remark2,
		Remark5:        header.Remark5,
	}
}
