package tiktokshop

// This file deliberately models only finance fields needed to reconcile a
// marketplace payout with existing SML sales documents.  It does not carry
// buyer, address, phone, or token data across the Gateway boundary.

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

const (
	PathFinanceStatements            = "/finance/202309/statements"
	PathFinanceStatementTransactions = "/finance/202501/statements/%s/statement_transactions"
	PathFinanceOrderTransactions     = "/finance/202501/orders/%s/statement_transactions"
	maxFinanceResponseSize           = 8 << 20
)

type StatementStatus string

const (
	StatementStatusPaid       StatementStatus = "PAID"
	StatementStatusProcessing StatementStatus = "PROCESSING"
	StatementStatusFailed     StatementStatus = "FAILED"
)

type FinanceClientConfig struct {
	BaseURL    string
	AppKey     string
	AppSecret  string
	HTTPClient *http.Client
	Now        func() time.Time
}

type FinanceClient struct {
	baseURL   *url.URL
	appKey    string
	appSecret string
	http      *http.Client
	now       func() time.Time
}

type SearchStatementsRequest struct {
	PageSize        int             `json:"page_size"`
	PageToken       string          `json:"page_token,omitempty"`
	StatementTimeGE int64           `json:"statement_time_ge,omitempty"`
	StatementTimeLT int64           `json:"statement_time_lt,omitempty"`
	StatementStatus StatementStatus `json:"statement_status,omitempty"`
}

type Statement struct {
	StatementID      string          `json:"id"`
	PaymentID        string          `json:"payment_id"`
	Status           StatementStatus `json:"payment_status"`
	Currency         string          `json:"currency"`
	PaymentTime      int64           `json:"payment_time"`
	StatementTime    int64           `json:"statement_time,omitempty"`
	SettlementAmount string          `json:"settlement_amount"`
	RevenueAmount    string          `json:"revenue_amount,omitempty"`
	FeeAmount        string          `json:"fee_amount,omitempty"`
	ShippingAmount   string          `json:"shipping_cost_amount,omitempty"`
	AdjustmentAmount string          `json:"adjustment_amount,omitempty"`
	RefundAmount     string          `json:"refund_amount,omitempty"`
	FailureReason    string          `json:"failure_reason,omitempty"`
}

type SearchStatementsResult struct {
	Statements    []Statement `json:"statements"`
	NextPageToken string      `json:"next_page_token,omitempty"`
	TotalCount    int64       `json:"total_count,omitempty"`
}

type StatementTransaction struct {
	ID                string `json:"id"`
	OrderID           string `json:"order_id"`
	AssociatedOrderID string `json:"associated_order_id,omitempty"`
	Currency          string `json:"currency"`
	SettlementAmount  string `json:"settlement_amount"`
	RevenueAmount     string `json:"revenue_amount,omitempty"`
	FeeAmount         string `json:"fee_tax_amount,omitempty"`
	ShippingAmount    string `json:"shipping_cost_amount,omitempty"`
	AdjustmentAmount  string `json:"adjustment_amount,omitempty"`
	ReserveAmount     string `json:"reserve_amount,omitempty"`
	TransactionType   string `json:"type,omitempty"`
}

type StatementTransactionsResult struct {
	Currency                 string                 `json:"currency,omitempty"`
	Transactions             []StatementTransaction `json:"transactions"`
	NextPageToken            string                 `json:"next_page_token,omitempty"`
	TotalCount               int64                  `json:"total_count,omitempty"`
	TotalSettlementAmount    string                 `json:"total_settlement_amount,omitempty"`
	TotalReserveAmount       string                 `json:"total_reserve_amount,omitempty"`
	TotalAdjustmentAmount    string                 `json:"-"`
	TotalSettlementBreakdown struct {
		TotalAdjustmentAmount string `json:"total_adjustment_amount,omitempty"`
	} `json:"total_settlement_breakdown"`
}

func NewFinanceClient(config FinanceClientConfig) (*FinanceClient, error) {
	baseURL, err := url.Parse(strings.TrimSpace(config.BaseURL))
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" || baseURL.User != nil {
		return nil, ErrInvalidOrderInput
	}
	if strings.TrimSpace(config.AppKey) == "" || strings.TrimSpace(config.AppSecret) == "" {
		return nil, ErrInvalidOrderInput
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &FinanceClient{baseURL: baseURL, appKey: strings.TrimSpace(config.AppKey), appSecret: strings.TrimSpace(config.AppSecret), http: client, now: now}, nil
}

func (c *FinanceClient) SearchStatements(ctx context.Context, accessToken, shopCipher string, input SearchStatementsRequest) (*SearchStatementsResult, string, error) {
	if c == nil || c.baseURL == nil || strings.TrimSpace(accessToken) == "" || strings.TrimSpace(shopCipher) == "" || input.Validate() != nil {
		return nil, "", ErrInvalidOrderInput
	}
	query := c.baseQuery(shopCipher)
	query.Set("page_size", strconv.Itoa(input.PageSize))
	if input.StatementStatus != "" {
		query.Set("payment_status", string(input.StatementStatus))
	}
	query.Set("statement_time_ge", strconv.FormatInt(input.StatementTimeGE, 10))
	query.Set("statement_time_lt", strconv.FormatInt(input.StatementTimeLT, 10))
	query.Set("sort_field", "statement_time")
	query.Set("sort_order", "DESC")
	if input.PageToken != "" {
		query.Set("page_token", input.PageToken)
	}
	var output SearchStatementsResult
	requestID, err := c.do(ctx, http.MethodGet, PathFinanceStatements, query, nil, accessToken, &output)
	if err != nil {
		return nil, requestID, err
	}
	if err := validateStatements(output.Statements); err != nil || output.TotalCount < int64(len(output.Statements)) {
		return nil, requestID, ErrInvalidOrderResponse
	}
	if output.Statements == nil {
		output.Statements = []Statement{}
	}
	return &output, requestID, nil
}

func (c *FinanceClient) GetStatementTransactions(ctx context.Context, accessToken, shopCipher, statementID, pageToken string, pageSize int) (*StatementTransactionsResult, string, error) {
	return c.getTransactions(ctx, accessToken, shopCipher, fmt.Sprintf(PathFinanceStatementTransactions, url.PathEscape(strings.TrimSpace(statementID))), pageToken, pageSize)
}

func (c *FinanceClient) GetOrderStatementTransactions(ctx context.Context, accessToken, shopCipher, orderID, pageToken string, pageSize int) (*StatementTransactionsResult, string, error) {
	return c.getTransactions(ctx, accessToken, shopCipher, fmt.Sprintf(PathFinanceOrderTransactions, url.PathEscape(strings.TrimSpace(orderID))), pageToken, pageSize)
}

func (c *FinanceClient) getTransactions(ctx context.Context, accessToken, shopCipher, path, pageToken string, pageSize int) (*StatementTransactionsResult, string, error) {
	if c == nil || c.baseURL == nil || strings.TrimSpace(accessToken) == "" || strings.TrimSpace(shopCipher) == "" || strings.Contains(path, "//") || pageSize < 1 || pageSize > 100 {
		return nil, "", ErrInvalidOrderInput
	}
	query := c.baseQuery(shopCipher)
	query.Set("page_size", strconv.Itoa(pageSize))
	if strings.TrimSpace(pageToken) != "" {
		query.Set("page_token", strings.TrimSpace(pageToken))
	}
	var output StatementTransactionsResult
	requestID, err := c.do(ctx, http.MethodGet, path, query, nil, accessToken, &output)
	if err != nil {
		return nil, requestID, err
	}
	output.Currency = strings.ToUpper(strings.TrimSpace(output.Currency))
	for i := range output.Transactions {
		if strings.TrimSpace(output.Transactions[i].Currency) == "" {
			output.Transactions[i].Currency = output.Currency
		}
	}
	if err := validateStatementTransactions(output.Transactions); err != nil || output.TotalCount < int64(len(output.Transactions)) {
		return nil, requestID, ErrInvalidOrderResponse
	}
	if output.Transactions == nil {
		output.Transactions = []StatementTransaction{}
	}
	return &output, requestID, nil
}

func (c *FinanceClient) baseQuery(shopCipher string) url.Values {
	return url.Values{"app_key": []string{c.appKey}, "shop_cipher": []string{strings.TrimSpace(shopCipher)}, "timestamp": []string{strconv.FormatInt(c.now().Unix(), 10)}}
}

func (c *FinanceClient) do(ctx context.Context, method, path string, query url.Values, body []byte, accessToken string, output any) (string, error) {
	signature, err := SignRequest(c.appSecret, path, query, body, false)
	if err != nil {
		return "", fmt.Errorf("sign TikTok Shop finance request: %w", err)
	}
	query.Set("sign", signature)
	requestURL := *c.baseURL
	requestURL.Path = strings.TrimRight(requestURL.Path, "/") + path
	requestURL.RawQuery = query.Encode()
	for attempt := 0; attempt < 3; attempt++ {
		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, requestURL.String(), reader)
		if err != nil {
			return "", fmt.Errorf("create TikTok Shop finance request: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-tts-access-token", strings.TrimSpace(accessToken))

		response, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() == nil && attempt < 2 {
				if waitErr := waitFinanceRetry(ctx, financeRetryDelay("", attempt)); waitErr == nil {
					continue
				}
			}
			return "", fmt.Errorf("call TikTok Shop finance API: %w", err)
		}
		responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, maxFinanceResponseSize+1))
		response.Body.Close()
		if readErr != nil {
			return "", fmt.Errorf("read TikTok Shop finance response: %w", readErr)
		}
		if len(responseBody) > maxFinanceResponseSize {
			return "", ErrInvalidOrderResponse
		}
		if (response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500) && attempt < 2 {
			if waitErr := waitFinanceRetry(ctx, financeRetryDelay(response.Header.Get("Retry-After"), attempt)); waitErr == nil {
				continue
			}
		}

		var payload apiResponse
		if err := json.Unmarshal(responseBody, &payload); err != nil {
			return "", fmt.Errorf("decode TikTok Shop finance response: %w", err)
		}
		requestID := strings.TrimSpace(payload.RequestID)
		if response.StatusCode < 200 || response.StatusCode >= 300 || payload.Code != 0 {
			apiCode := payload.Code
			if apiCode == 0 && (response.StatusCode < 200 || response.StatusCode >= 300) {
				apiCode = response.StatusCode
			}
			return requestID, &APIError{Code: apiCode, RequestID: requestID, Message: "TikTok Shop rejected the finance request"}
		}
		if len(payload.Data) == 0 || string(payload.Data) == "null" || output == nil {
			return requestID, ErrInvalidOrderResponse
		}
		if err := json.Unmarshal(payload.Data, output); err != nil {
			return requestID, fmt.Errorf("decode TikTok Shop finance data: %w", err)
		}
		return requestID, nil
	}
	return "", ErrInvalidOrderResponse
}

func financeRetryDelay(retryAfter string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && seconds >= 0 {
		delay := time.Duration(seconds) * time.Second
		if delay > 2*time.Second {
			return 2 * time.Second
		}
		return delay
	}
	delay := 200 * time.Millisecond * time.Duration(1<<attempt)
	if delay > time.Second {
		return time.Second
	}
	return delay
}

func waitFinanceRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (input *SearchStatementsRequest) Validate() error {
	if input == nil || input.PageSize < 1 || input.PageSize > 100 || input.StatementTimeGE < 0 || input.StatementTimeLT < 0 || (input.StatementTimeGE != 0 && input.StatementTimeLT != 0 && input.StatementTimeGE >= input.StatementTimeLT) {
		return ErrInvalidOrderInput
	}
	input.PageToken = strings.TrimSpace(input.PageToken)
	switch input.StatementStatus {
	case "", StatementStatusPaid, StatementStatusProcessing, StatementStatusFailed:
	default:
		return ErrInvalidOrderInput
	}
	return nil
}

func validateStatements(values []Statement) error {
	if len(values) > 100 {
		return ErrInvalidOrderResponse
	}
	seen := map[string]struct{}{}
	for i := range values {
		v := &values[i]
		v.StatementID = strings.TrimSpace(v.StatementID)
		v.PaymentID = strings.TrimSpace(v.PaymentID)
		v.Currency = strings.ToUpper(strings.TrimSpace(v.Currency))
		v.SettlementAmount = strings.TrimSpace(v.SettlementAmount)
		if v.StatementID == "" || v.Currency == "" || v.SettlementAmount == "" || v.StatementTime <= 0 {
			return ErrInvalidOrderResponse
		}
		if _, ok := seen[v.StatementID]; ok {
			return ErrInvalidOrderResponse
		}
		seen[v.StatementID] = struct{}{}
	}
	return nil
}

func validateStatementTransactions(values []StatementTransaction) error {
	if len(values) > 100 {
		return ErrInvalidOrderResponse
	}
	seen := map[string]struct{}{}
	for i := range values {
		v := &values[i]
		v.ID = strings.TrimSpace(v.ID)
		v.OrderID = strings.TrimSpace(v.OrderID)
		v.AssociatedOrderID = strings.TrimSpace(v.AssociatedOrderID)
		v.Currency = strings.ToUpper(strings.TrimSpace(v.Currency))
		v.SettlementAmount = strings.TrimSpace(v.SettlementAmount)
		if v.ID == "" || v.Currency == "" || v.SettlementAmount == "" {
			return ErrInvalidOrderResponse
		}
		if _, ok := seen[v.ID]; ok {
			return ErrInvalidOrderResponse
		}
		seen[v.ID] = struct{}{}
	}
	return nil
}
