package sml

import (
	"bytes"
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

// SaleInvoiceCancelClient calls sml-api-bybos endpoints that create the SML
// "ขาย -> ยกเลิกขายสินค้าและบริการ" document for an existing sale invoice.
// Nexflow owns workflow/idempotency; sml-api-bybos owns DB-specific inserts.
type SaleInvoiceCancelClient struct {
	cfg        PartyConfig
	httpClient *http.Client
	log        *zap.Logger
}

type SaleInvoiceCancelRequest struct {
	DocDate       string `json:"doc_date,omitempty"`
	DocFormatCode string `json:"doc_format_code,omitempty"`
	DocNo         string `json:"doc_no,omitempty"`
	Remark        string `json:"remark,omitempty"`
}

type SaleInvoiceCancelResponse struct {
	Success       bool            `json:"success"`
	Status        string          `json:"status"`
	Code          string          `json:"code,omitempty"`
	Message       string          `json:"message,omitempty"`
	AlreadyExists bool            `json:"already_exists,omitempty"`
	Error         any             `json:"error,omitempty"`
	Data          json.RawMessage `json:"data,omitempty"`
	raw           json.RawMessage
}

func NewSaleInvoiceCancelClient(cfg PartyConfig, log *zap.Logger) *SaleInvoiceCancelClient {
	return &SaleInvoiceCancelClient{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		log:        log,
	}
}

func (c *SaleInvoiceCancelClient) IsConfigured() bool {
	return c != nil &&
		strings.TrimSpace(c.cfg.BaseURL) != "" &&
		strings.TrimSpace(c.cfg.GUID) != "" &&
		strings.TrimSpace(c.cfg.Database) != ""
}

func (c *SaleInvoiceCancelClient) Preview(ctx context.Context, saleDocNo string, req SaleInvoiceCancelRequest) (int, *SaleInvoiceCancelResponse, error) {
	return c.post(ctx, saleDocNo, "preview", req)
}

func (c *SaleInvoiceCancelClient) Create(ctx context.Context, saleDocNo string, req SaleInvoiceCancelRequest) (int, *SaleInvoiceCancelResponse, error) {
	return c.post(ctx, saleDocNo, "", req)
}

func (c *SaleInvoiceCancelClient) post(ctx context.Context, saleDocNo, suffix string, payload SaleInvoiceCancelRequest) (int, *SaleInvoiceCancelResponse, error) {
	if !c.IsConfigured() {
		return 0, nil, fmt.Errorf("SML sale invoice cancel client not configured")
	}
	saleDocNo = strings.TrimSpace(saleDocNo)
	if saleDocNo == "" {
		return 0, nil, fmt.Errorf("sale invoice doc_no is required")
	}
	body, err := marshalASCII(payload)
	if err != nil {
		return 0, nil, err
	}
	path := "/api/v1/ic/sale-invoices/" + url.PathEscape(saleDocNo) + "/cancel"
	if strings.TrimSpace(suffix) == "preview" {
		path += "/preview"
	}
	rawURL := strings.TrimRight(c.cfg.BaseURL, "/") + path
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	for k, v := range c.headers() {
		httpReq.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return 0, nil, fmt.Errorf("sml saleinvoice cancel %s: %w", saleDocNo, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	out := &SaleInvoiceCancelResponse{raw: append(json.RawMessage(nil), respBody...)}
	_ = json.Unmarshal(respBody, out)
	if c.log != nil {
		fields := []zap.Field{
			zap.String("sale_doc_no", saleDocNo),
			zap.Int("status_code", resp.StatusCode),
			zap.Int64("duration_ms", time.Since(start).Milliseconds()),
		}
		if cancelDocNo := out.CancelDocNo(); cancelDocNo != "" {
			fields = append(fields, zap.String("cancel_doc_no", cancelDocNo))
		}
		if out.IsSuccess() {
			c.log.Info("sml_saleinvoice_cancel_response", fields...)
		} else {
			fields = append(fields, zap.String("message", out.GetMessage()))
			c.log.Warn("sml_saleinvoice_cancel_failed", fields...)
		}
	}
	return resp.StatusCode, out, nil
}

func (c *SaleInvoiceCancelClient) headers() map[string]string {
	return map[string]string{
		"guid":           c.cfg.GUID,
		"X-Api-Key":      c.cfg.GUID,
		"provider":       c.cfg.Provider,
		"configFileName": c.cfg.ConfigFile,
		"databaseName":   c.cfg.Database,
		"X-Tenant":       c.cfg.Database,
		"Accept":         "application/json",
		"Content-Type":   "application/json; charset=utf-8",
	}
}

func (r *SaleInvoiceCancelResponse) IsSuccess() bool {
	if r == nil {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(r.Status))
	code := strings.ToLower(strings.TrimSpace(r.Code))
	return r.Success || status == "success" || status == "ok" || status == "already_exists" || code == "already_exists" || r.AlreadyExists
}

func (r *SaleInvoiceCancelResponse) GetMessage() string {
	if r == nil {
		return ""
	}
	if strings.TrimSpace(r.Message) != "" {
		return strings.TrimSpace(r.Message)
	}
	if s := saleInvoiceCancelString(r.Error); s != "" {
		return s
	}
	if len(r.raw) > 0 {
		return strings.TrimSpace(string(r.raw))
	}
	return ""
}

func (r *SaleInvoiceCancelResponse) Raw() json.RawMessage {
	if r == nil || len(r.raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), r.raw...)
}

func (r *SaleInvoiceCancelResponse) CancelDocNo() string {
	if r == nil {
		return ""
	}
	var root map[string]any
	if len(r.raw) > 0 {
		_ = json.Unmarshal(r.raw, &root)
	}
	for _, key := range []string{"cancel_sml_doc_no", "cancel_doc_no", "cn_doc_no", "doc_no"} {
		if s := saleInvoiceCancelFindString(root, key); s != "" {
			return s
		}
	}
	return ""
}

func saleInvoiceCancelFindString(v any, key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	switch node := v.(type) {
	case map[string]any:
		for k, child := range node {
			if strings.ToLower(strings.TrimSpace(k)) == key {
				if s := saleInvoiceCancelString(child); s != "" {
					return s
				}
			}
		}
		for _, child := range node {
			if s := saleInvoiceCancelFindString(child, key); s != "" {
				return s
			}
		}
	case []any:
		for _, child := range node {
			if s := saleInvoiceCancelFindString(child, key); s != "" {
				return s
			}
		}
	}
	return ""
}

func saleInvoiceCancelString(v any) string {
	switch value := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(value)
	case json.Number:
		return strings.TrimSpace(value.String())
	case float64:
		return strings.TrimSpace(fmt.Sprintf("%.0f", value))
	case map[string]any:
		for _, key := range []string{"message", "error", "code"} {
			if s := saleInvoiceCancelFindString(value, key); s != "" {
				return s
			}
		}
	}
	return strings.TrimSpace(fmt.Sprint(v))
}
