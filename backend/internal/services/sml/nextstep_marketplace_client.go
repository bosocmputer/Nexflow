package sml

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"
)

const defaultNextStepMarketplaceTimeout = 5 * time.Second

type NextStepMarketplaceClient struct {
	cfg        PartyConfig
	httpClient *http.Client
	log        *zap.Logger
}

func NewNextStepMarketplaceClient(cfg PartyConfig, log *zap.Logger) *NextStepMarketplaceClient {
	return &NextStepMarketplaceClient{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: defaultNextStepMarketplaceTimeout,
		},
		log: log,
	}
}

func (c *NextStepMarketplaceClient) WithHTTPClient(client *http.Client) *NextStepMarketplaceClient {
	if client != nil {
		c.httpClient = client
	}
	return c
}

func (c *NextStepMarketplaceClient) IsConfigured() bool {
	return c != nil &&
		strings.TrimSpace(c.cfg.BaseURL) != "" &&
		strings.TrimSpace(c.cfg.GUID) != "" &&
		strings.TrimSpace(c.cfg.Database) != ""
}

type NextStepMarketplaceRequest struct {
	CustCode string
	DateFrom string
	DateTo   string
	Search   string
	Page     int
	Size     int
}

type NextStepMarketplaceData struct {
	Summary NextStepMarketplaceSummary `json:"summary"`
	Orders  []NextStepMarketplaceOrder `json:"orders"`
	Meta    NextStepMarketplaceMeta    `json:"meta"`
}

type NextStepMarketplaceSummary struct {
	TotalOrders    int            `json:"total_orders"`
	TotalAmount    float64        `json:"total_amount"`
	CNTotalAmount  float64        `json:"cn_total_amount"`
	TotalExceptVAT float64        `json:"total_except_vat"`
	TotalAfterVAT  float64        `json:"total_after_vat"`
	TotalVATValue  float64        `json:"total_vat_value"`
	StatusCounts   map[string]int `json:"status_counts"`
	PendingCount   int            `json:"pending_count"`
	PackingCount   int            `json:"packing_count"`
	PaymentCount   int            `json:"payment_count"`
	SuccessCount   int            `json:"success_count"`
	CancelCount    int            `json:"cancel_count"`
}

type NextStepMarketplaceOrder struct {
	Remark5        string  `json:"remark_5"`
	InvDocNo       string  `json:"inv_doc_no"`
	InvDocDate     string  `json:"inv_doc_date"`
	WalletAmount   float64 `json:"wallet_amount"`
	RemarkQT       string  `json:"remark_qt"`
	RemarkCancel   string  `json:"remark_cancel"`
	RemarkInv      string  `json:"remark_inv"`
	DocNo          string  `json:"doc_no"`
	DocDate        string  `json:"doc_date"`
	DocTime        string  `json:"doc_time"`
	CustCode       string  `json:"cust_code"`
	SendType       int     `json:"send_type"`
	EmpCode        string  `json:"emp_code"`
	EmpName        string  `json:"emp_name"`
	TotalAmount    float64 `json:"total_amount"`
	CNTotalAmount  float64 `json:"cn_total_amount"`
	TotalExceptVAT float64 `json:"total_except_vat"`
	TotalAfterVAT  float64 `json:"total_after_vat"`
	TotalVATValue  float64 `json:"total_vat_value"`
	Balance        float64 `json:"balance"`
	Status         string  `json:"status"`
}

type NextStepMarketplaceMeta struct {
	Tenant    string `json:"tenant"`
	CustCode  string `json:"cust_code"`
	DocPrefix string `json:"doc_prefix"`
	DateFrom  string `json:"date_from"`
	DateTo    string `json:"date_to"`
	DateBasis string `json:"date_basis"`
	Source    string `json:"source"`
	Search    string `json:"search,omitempty"`
	Page      int    `json:"page"`
	Size      int    `json:"size"`
	Total     int    `json:"total"`
}

type nextStepMarketplaceEnvelope struct {
	Success bool                    `json:"success"`
	Data    NextStepMarketplaceData `json:"data"`
	Error   *smlAPIErrorBody        `json:"error,omitempty"`
}

type smlAPIErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (c *NextStepMarketplaceClient) Fetch(ctx context.Context, req NextStepMarketplaceRequest) (*NextStepMarketplaceData, error) {
	if !c.IsConfigured() {
		return nil, fmt.Errorf("sml marketplace client is not configured")
	}
	custCode := strings.TrimSpace(req.CustCode)
	if custCode == "" {
		return nil, fmt.Errorf("cust_code is required")
	}
	if strings.TrimSpace(req.DateFrom) == "" || strings.TrimSpace(req.DateTo) == "" {
		return nil, fmt.Errorf("date_from and date_to are required")
	}
	page := req.Page
	if page < 1 {
		page = 1
	}
	size := req.Size
	if size < 1 {
		size = 5
	}

	u, err := url.Parse(strings.TrimRight(c.cfg.BaseURL, "/") + "/api/v1/marketplace/nextstep/orders")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("cust_code", custCode)
	q.Set("date_from", strings.TrimSpace(req.DateFrom))
	q.Set("date_to", strings.TrimSpace(req.DateTo))
	if search := strings.TrimSpace(req.Search); search != "" {
		q.Set("search", search)
	}
	q.Set("page", fmt.Sprintf("%d", page))
	q.Set("size", fmt.Sprintf("%d", size))
	u.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("X-Tenant", c.cfg.Database)
	httpReq.Header.Set("X-Api-Key", c.cfg.GUID)
	httpReq.Header.Set("guid", c.cfg.GUID)
	httpReq.Header.Set("provider", c.cfg.Provider)
	httpReq.Header.Set("configFileName", c.cfg.ConfigFile)
	httpReq.Header.Set("databaseName", c.cfg.Database)
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("nextstep marketplace request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("nextstep marketplace read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nextstep marketplace HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var envelope nextStepMarketplaceEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("nextstep marketplace decode: %w", err)
	}
	if !envelope.Success {
		if envelope.Error != nil {
			return nil, fmt.Errorf("nextstep marketplace %s: %s", envelope.Error.Code, envelope.Error.Message)
		}
		return nil, fmt.Errorf("nextstep marketplace success=false")
	}
	if envelope.Data.Orders == nil {
		envelope.Data.Orders = []NextStepMarketplaceOrder{}
	}
	if envelope.Data.Summary.StatusCounts == nil {
		envelope.Data.Summary.StatusCounts = map[string]int{}
	}
	return &envelope.Data, nil
}
