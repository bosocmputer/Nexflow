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
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	PathFinanceStatements            = "/finance/202309/statements"
	PathFinanceWithdrawals           = "/finance/202309/withdrawals"
	PathFinancePayments              = "/finance/202605/payments"
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

// WithdrawalType is deliberately limited to the values published by TikTok
// Shop.  A withdrawal record is a payout-side observation only; it must never
// be treated as evidence that a particular Statement or order was included.
type WithdrawalType string

const (
	WithdrawalTypeWithdraw WithdrawalType = "WITHDRAW"
	WithdrawalTypeSettle   WithdrawalType = "SETTLE"
	WithdrawalTypeTransfer WithdrawalType = "TRANSFER"
	WithdrawalTypeReverse  WithdrawalType = "REVERSE"
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

// SearchWithdrawalsRequest mirrors Get Withdrawals.  We keep this separate
// from Statement search because the public API does not document a stable
// withdrawal_id -> statement_id relationship.
type SearchWithdrawalsRequest struct {
	Types        []WithdrawalType `json:"types"`
	PageSize     int              `json:"page_size"`
	PageToken    string           `json:"page_token,omitempty"`
	CreateTimeGE int64            `json:"create_time_ge,omitempty"`
	CreateTimeLT int64            `json:"create_time_lt,omitempty"`
}

// SearchPaymentsRequest mirrors TikTok Shop's Get Payments endpoint.  It is
// deliberately read-only: a payment record is evidence of marketplace income,
// not proof that it was included in a particular withdrawal.
type SearchPaymentsRequest struct {
	PageSize     int    `json:"page_size"`
	PageToken    string `json:"page_token,omitempty"`
	CreateTimeGE int64  `json:"create_time_ge,omitempty"`
	CreateTimeLT int64  `json:"create_time_lt,omitempty"`
	SortField    string `json:"sort_field"`
	SortOrder    string `json:"sort_order,omitempty"`
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

// Withdrawal intentionally excludes bank-card and buyer information.  The
// fields are sufficient for an operator to identify a TikTok withdrawal round
// without persisting sensitive payout metadata.
type Withdrawal struct {
	WithdrawalID string         `json:"id"`
	Type         WithdrawalType `json:"type"`
	Amount       string         `json:"amount"`
	Currency     string         `json:"currency"`
	Status       string         `json:"status"`
	CreateTime   int64          `json:"create_time"`
}

type SearchWithdrawalsResult struct {
	Withdrawals   []Withdrawal `json:"withdrawals"`
	NextPageToken string       `json:"next_page_token,omitempty"`
	TotalCount    int64        `json:"total_count,omitempty"`
}

// Payment retains only financial and identity fields that may be needed for a
// later, explicit reconciliation.  Unknown fields are never retained, which
// keeps buyer and payout data from crossing the Gateway boundary by accident.
// ObservedFields contains field names only and helps us safely establish the
// official response shape during AOY's read-only UAT.
type Payment struct {
	PaymentID      string   `json:"payment_id"`
	Amount         string   `json:"amount,omitempty"`
	Currency       string   `json:"currency,omitempty"`
	Status         string   `json:"status,omitempty"`
	PaymentTime    int64    `json:"payment_time,omitempty"`
	CreateTime     int64    `json:"create_time,omitempty"`
	OrderIDs       []string `json:"order_ids,omitempty"`
	StatementIDs   []string `json:"statement_ids,omitempty"`
	ObservedFields []string `json:"-"`
}

// UnmarshalJSON accepts the documented and observed identity aliases without
// preserving the original payload.  This protects against a future response
// adding buyer or bank fields that Nexflow must not store or display.
func (p *Payment) UnmarshalJSON(data []byte) error {
	type paymentWire struct {
		ID            string   `json:"id"`
		PaymentID     string   `json:"payment_id"`
		Amount        string   `json:"amount"`
		Currency      string   `json:"currency"`
		Status        string   `json:"status"`
		PaymentStatus string   `json:"payment_status"`
		PaymentTime   int64    `json:"payment_time"`
		CreateTime    int64    `json:"create_time"`
		OrderIDs      []string `json:"order_ids"`
		StatementIDs  []string `json:"statement_ids"`
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var wire paymentWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	fields := make([]string, 0, len(raw))
	for key := range raw {
		key = strings.TrimSpace(key)
		if len(key) > 64 || key == "" {
			continue
		}
		fields = append(fields, key)
	}
	sort.Strings(fields)
	if len(fields) > 40 {
		fields = fields[:40]
	}
	p.PaymentID = strings.TrimSpace(wire.PaymentID)
	if p.PaymentID == "" {
		p.PaymentID = strings.TrimSpace(wire.ID)
	}
	p.Amount = strings.TrimSpace(wire.Amount)
	p.Currency = strings.ToUpper(strings.TrimSpace(wire.Currency))
	p.Status = strings.TrimSpace(wire.Status)
	if p.Status == "" {
		p.Status = strings.TrimSpace(wire.PaymentStatus)
	}
	p.PaymentTime = wire.PaymentTime
	p.CreateTime = wire.CreateTime
	p.OrderIDs = cleanFinanceIDs(wire.OrderIDs)
	p.StatementIDs = cleanFinanceIDs(wire.StatementIDs)
	p.ObservedFields = fields
	return nil
}

func cleanFinanceIDs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	clean := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 128 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		clean = append(clean, value)
		if len(clean) == 100 {
			break
		}
	}
	return clean
}

type SearchPaymentsResult struct {
	Payments      []Payment `json:"payments"`
	NextPageToken string    `json:"next_page_token,omitempty"`
	TotalCount    int64     `json:"total_count,omitempty"`
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

// SearchWithdrawals calls TikTok's read-only Get Withdrawals endpoint.  It
// does not infer statement membership from date or amount; callers must expose
// such records as candidates pending an explicit reconciliation step.
func (c *FinanceClient) SearchWithdrawals(ctx context.Context, accessToken, shopCipher string, input SearchWithdrawalsRequest) (*SearchWithdrawalsResult, string, error) {
	if c == nil || c.baseURL == nil || strings.TrimSpace(accessToken) == "" || strings.TrimSpace(shopCipher) == "" || input.Validate() != nil {
		return nil, "", ErrInvalidOrderInput
	}
	query := c.baseQuery(shopCipher)
	query.Set("page_size", strconv.Itoa(input.PageSize))
	types := make([]string, 0, len(input.Types))
	for _, typ := range input.Types {
		types = append(types, string(typ))
	}
	query.Set("types", strings.Join(types, ","))
	query.Set("create_time_ge", strconv.FormatInt(input.CreateTimeGE, 10))
	query.Set("create_time_lt", strconv.FormatInt(input.CreateTimeLT, 10))
	if input.PageToken != "" {
		query.Set("page_token", input.PageToken)
	}
	var output SearchWithdrawalsResult
	requestID, err := c.do(ctx, http.MethodGet, PathFinanceWithdrawals, query, nil, accessToken, &output)
	if err != nil {
		return nil, requestID, err
	}
	if err := validateWithdrawals(output.Withdrawals); err != nil || output.TotalCount < int64(len(output.Withdrawals)) {
		return nil, requestID, ErrInvalidOrderResponse
	}
	if output.Withdrawals == nil {
		output.Withdrawals = []Withdrawal{}
	}
	return &output, requestID, nil
}

// SearchPayments calls TikTok Shop's read-only Get Payments endpoint.  It
// intentionally returns no raw payload and makes no withdrawal or RC
// inference; that association is only allowed after an official identifier is
// present in the response.
func (c *FinanceClient) SearchPayments(ctx context.Context, accessToken, shopCipher string, input SearchPaymentsRequest) (*SearchPaymentsResult, string, error) {
	if c == nil || c.baseURL == nil || strings.TrimSpace(accessToken) == "" || strings.TrimSpace(shopCipher) == "" || input.Validate() != nil {
		return nil, "", ErrInvalidOrderInput
	}
	query := c.baseQuery(shopCipher)
	query.Set("page_size", strconv.Itoa(input.PageSize))
	query.Set("create_time_ge", strconv.FormatInt(input.CreateTimeGE, 10))
	query.Set("create_time_lt", strconv.FormatInt(input.CreateTimeLT, 10))
	query.Set("sort_field", input.SortField)
	query.Set("sort_order", input.SortOrder)
	if input.PageToken != "" {
		query.Set("page_token", input.PageToken)
	}
	var output SearchPaymentsResult
	requestID, err := c.do(ctx, http.MethodGet, PathFinancePayments, query, nil, accessToken, &output)
	if err != nil {
		return nil, requestID, err
	}
	if err := validatePayments(output.Payments); err != nil || output.TotalCount < int64(len(output.Payments)) {
		return nil, requestID, ErrInvalidOrderResponse
	}
	if output.Payments == nil {
		output.Payments = []Payment{}
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

func (input *SearchWithdrawalsRequest) Validate() error {
	if input == nil || input.PageSize < 1 || input.PageSize > 100 || input.CreateTimeGE < 0 || input.CreateTimeLT < 0 || input.CreateTimeGE >= input.CreateTimeLT || len(input.Types) == 0 || len(input.Types) > 4 {
		return ErrInvalidOrderInput
	}
	input.PageToken = strings.TrimSpace(input.PageToken)
	seen := make(map[WithdrawalType]struct{}, len(input.Types))
	for _, typ := range input.Types {
		switch typ {
		case WithdrawalTypeWithdraw, WithdrawalTypeSettle, WithdrawalTypeTransfer, WithdrawalTypeReverse:
		default:
			return ErrInvalidOrderInput
		}
		if _, ok := seen[typ]; ok {
			return ErrInvalidOrderInput
		}
		seen[typ] = struct{}{}
	}
	return nil
}

func (input *SearchPaymentsRequest) Validate() error {
	if input == nil || input.PageSize < 1 || input.PageSize > 100 || input.CreateTimeGE < 0 || input.CreateTimeLT < 0 || input.CreateTimeGE >= input.CreateTimeLT {
		return ErrInvalidOrderInput
	}
	input.PageToken = strings.TrimSpace(input.PageToken)
	input.SortField = strings.TrimSpace(input.SortField)
	input.SortOrder = strings.ToUpper(strings.TrimSpace(input.SortOrder))
	if input.SortField != "create_time" || (input.SortOrder != "" && input.SortOrder != "ASC" && input.SortOrder != "DESC") {
		return ErrInvalidOrderInput
	}
	if input.SortOrder == "" {
		input.SortOrder = "DESC"
	}
	return nil
}

func validateStatements(values []Statement) error {
	if len(values) > 100 {
		return fmt.Errorf("statement response exceeds page limit: %w", ErrInvalidOrderResponse)
	}
	seen := map[string]struct{}{}
	for i := range values {
		v := &values[i]
		v.StatementID = strings.TrimSpace(v.StatementID)
		v.PaymentID = strings.TrimSpace(v.PaymentID)
		v.Currency = strings.ToUpper(strings.TrimSpace(v.Currency))
		v.SettlementAmount = strings.TrimSpace(v.SettlementAmount)
		if v.StatementID == "" {
			return fmt.Errorf("statement response is missing id: %w", ErrInvalidOrderResponse)
		}
		if v.Currency == "" {
			return fmt.Errorf("statement %q is missing currency: %w", v.StatementID, ErrInvalidOrderResponse)
		}
		if v.SettlementAmount == "" {
			return fmt.Errorf("statement %q is missing settlement amount: %w", v.StatementID, ErrInvalidOrderResponse)
		}
		if v.StatementTime <= 0 {
			return fmt.Errorf("statement %q is missing statement time: %w", v.StatementID, ErrInvalidOrderResponse)
		}
		if _, ok := seen[v.StatementID]; ok {
			return fmt.Errorf("statement response contains duplicate id: %w", ErrInvalidOrderResponse)
		}
		seen[v.StatementID] = struct{}{}
	}
	return nil
}

func validateWithdrawals(values []Withdrawal) error {
	if len(values) > 100 {
		return ErrInvalidOrderResponse
	}
	seen := map[string]struct{}{}
	for i := range values {
		v := &values[i]
		v.WithdrawalID = strings.TrimSpace(v.WithdrawalID)
		v.Amount = strings.TrimSpace(v.Amount)
		v.Currency = strings.ToUpper(strings.TrimSpace(v.Currency))
		v.Status = strings.TrimSpace(v.Status)
		if v.WithdrawalID == "" || v.Amount == "" || v.Currency == "" || v.Status == "" || v.CreateTime <= 0 {
			return ErrInvalidOrderResponse
		}
		switch v.Type {
		case WithdrawalTypeWithdraw, WithdrawalTypeSettle, WithdrawalTypeTransfer, WithdrawalTypeReverse:
		default:
			return ErrInvalidOrderResponse
		}
		if _, ok := seen[v.WithdrawalID]; ok {
			return ErrInvalidOrderResponse
		}
		seen[v.WithdrawalID] = struct{}{}
	}
	return nil
}

func validatePayments(values []Payment) error {
	if len(values) > 100 {
		return ErrInvalidOrderResponse
	}
	seen := map[string]struct{}{}
	for i := range values {
		v := &values[i]
		v.PaymentID = strings.TrimSpace(v.PaymentID)
		if v.PaymentID == "" {
			return ErrInvalidOrderResponse
		}
		if _, ok := seen[v.PaymentID]; ok {
			return ErrInvalidOrderResponse
		}
		seen[v.PaymentID] = struct{}{}
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
