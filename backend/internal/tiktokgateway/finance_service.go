package tiktokgateway

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"nexflow/internal/services/tiktokshop"
)

const TikTokFinanceScope = "seller.finance.info"

var (
	ErrFinanceServiceNotConfigured = errors.New("TikTok Shop finance service is not configured")
	ErrFinanceScopeUnavailable     = errors.New("TikTok Shop Finance Information scope is unavailable")
)

type TikTokFinanceReader interface {
	SearchStatements(context.Context, string, string, tiktokshop.SearchStatementsRequest) (*tiktokshop.SearchStatementsResult, string, error)
	GetStatementTransactions(context.Context, string, string, string, string, int) (*tiktokshop.StatementTransactionsResult, string, error)
}

type FinanceService struct {
	credentials OrderCredentialProvider
	finance     TikTokFinanceReader
}
type FinanceStatementsResult struct {
	UpstreamRequestID string                 `json:"upstream_request_id"`
	NextPageToken     string                 `json:"next_page_token"`
	TotalCount        int64                  `json:"total_count"`
	Statements        []tiktokshop.Statement `json:"statements"`
}
type FinanceTransactionsResult struct {
	Currency                 string `json:"currency"`
	UpstreamRequestID        string `json:"upstream_request_id"`
	NextPageToken            string `json:"next_page_token"`
	TotalCount               int64  `json:"total_count"`
	TotalSettlementAmount    string `json:"total_settlement_amount"`
	TotalReserveAmount       string `json:"total_reserve_amount"`
	TotalSettlementBreakdown struct {
		TotalAdjustmentAmount string `json:"total_adjustment_amount"`
	} `json:"total_settlement_breakdown"`
	Transactions []tiktokshop.StatementTransaction `json:"transactions"`
}

func NewFinanceService(credentials OrderCredentialProvider, finance TikTokFinanceReader) (*FinanceService, error) {
	if credentials == nil || finance == nil {
		return nil, ErrFinanceServiceNotConfigured
	}
	return &FinanceService{credentials: credentials, finance: finance}, nil
}

func (s *FinanceService) SearchStatements(ctx context.Context, tenant, shopID string, input tiktokshop.SearchStatementsRequest) (*FinanceStatementsResult, error) {
	if input.Validate() != nil {
		return nil, tiktokshop.ErrInvalidOrderInput
	}
	credential, err := s.credential(ctx, tenant, shopID)
	if err != nil {
		return nil, err
	}
	result, requestID, err := s.finance.SearchStatements(ctx, credential.AccessToken, credential.ShopCipher, input)
	if err != nil {
		return nil, fmt.Errorf("search TikTok Shop finance statements: %w", err)
	}
	if result == nil {
		return nil, tiktokshop.ErrInvalidOrderResponse
	}
	return &FinanceStatementsResult{UpstreamRequestID: strings.TrimSpace(requestID), NextPageToken: result.NextPageToken, TotalCount: result.TotalCount, Statements: append([]tiktokshop.Statement(nil), result.Statements...)}, nil
}

func (s *FinanceService) GetStatementTransactions(ctx context.Context, tenant, shopID, statementID, pageToken string, pageSize int) (*FinanceTransactionsResult, error) {
	if strings.TrimSpace(statementID) == "" || pageSize < 1 || pageSize > 100 {
		return nil, tiktokshop.ErrInvalidOrderInput
	}
	credential, err := s.credential(ctx, tenant, shopID)
	if err != nil {
		return nil, err
	}
	result, requestID, err := s.finance.GetStatementTransactions(ctx, credential.AccessToken, credential.ShopCipher, strings.TrimSpace(statementID), strings.TrimSpace(pageToken), pageSize)
	if err != nil {
		return nil, fmt.Errorf("get TikTok Shop statement transactions: %w", err)
	}
	if result == nil {
		return nil, tiktokshop.ErrInvalidOrderResponse
	}
	out := &FinanceTransactionsResult{UpstreamRequestID: strings.TrimSpace(requestID), Currency: result.Currency, NextPageToken: result.NextPageToken, TotalCount: result.TotalCount, TotalSettlementAmount: result.TotalSettlementAmount, TotalReserveAmount: result.TotalReserveAmount, Transactions: append([]tiktokshop.StatementTransaction(nil), result.Transactions...)}
	out.TotalSettlementBreakdown.TotalAdjustmentAmount = result.TotalSettlementBreakdown.TotalAdjustmentAmount
	return out, nil
}

func (s *FinanceService) credential(ctx context.Context, tenant, shopID string) (*AccessCredential, error) {
	if s == nil || s.credentials == nil || s.finance == nil {
		return nil, ErrFinanceServiceNotConfigured
	}
	tenant, shopID = strings.ToLower(strings.TrimSpace(tenant)), strings.TrimSpace(shopID)
	if tenant == "" || shopID == "" {
		return nil, tiktokshop.ErrInvalidOrderInput
	}
	credential, err := s.credentials.AccessCredential(ctx, tenant, shopID)
	if err != nil {
		return nil, err
	}
	if credential == nil || credential.ShopID != shopID || strings.TrimSpace(credential.AccessToken) == "" || strings.TrimSpace(credential.ShopCipher) == "" {
		return nil, ErrInvalidTokenCredential
	}
	// granted_scopes is a useful display hint only.  TikTok can change or
	// refresh the persisted metadata independently of the effective token, so
	// preflight must make the signed Finance request and let TikTok decide.
	return credential, nil
}
