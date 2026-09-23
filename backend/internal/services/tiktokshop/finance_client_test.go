package tiktokshop

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFinanceClientSearchStatementsSignsRequestAndKeepsOnlyFinancialFields(t *testing.T) {
	now := time.Unix(1_725_000_000, 0)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != PathFinanceStatements {
			t.Fatalf("request=%s %s", r.Method, r.URL.Path)
		}
		query := r.URL.Query()
		provided := query.Get("sign")
		query.Del("sign")
		body := readTestBody(t, r)
		if len(body) != 0 {
			t.Fatalf("Get Statements must not send a request body: %q", body)
		}
		expected, err := SignRequest("secret", PathFinanceStatements, query, nil, false)
		if err != nil || provided != expected {
			t.Fatalf("signature=%q expected=%q err=%v", provided, expected, err)
		}
		_, _ = w.Write([]byte(`{"code":0,"request_id":"finance-request","data":{"total_count":1,"statements":[{"id":"statement-1","payment_id":"payment-1","payment_status":"PAID","currency":"THB","statement_time":1725000000,"settlement_amount":"95.00","buyer_name":"must-not-cross"}]}}`))
	}))
	defer server.Close()
	client, err := NewFinanceClient(FinanceClientConfig{BaseURL: server.URL, AppKey: "key", AppSecret: "secret", HTTPClient: server.Client(), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	result, requestID, err := client.SearchStatements(context.Background(), "token", "cipher", SearchStatementsRequest{PageSize: 1, StatementTimeGE: now.Add(-time.Hour).Unix(), StatementTimeLT: now.Unix(), StatementStatus: StatementStatusPaid})
	if err != nil || requestID != "finance-request" || len(result.Statements) != 1 || result.Statements[0].SettlementAmount != "95.00" {
		t.Fatalf("result=%+v requestID=%q err=%v", result, requestID, err)
	}
	if result.Statements[0].StatementID != "statement-1" || result.Statements[0].Currency != "THB" {
		t.Fatalf("statement=%+v", result.Statements[0])
	}
}

func TestFinanceClientRejectsInvalidRange(t *testing.T) {
	client, err := NewFinanceClient(FinanceClientConfig{BaseURL: "https://example.test", AppKey: "key", AppSecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.SearchStatements(context.Background(), "token", "cipher", SearchStatementsRequest{PageSize: 1, StatementTimeGE: 2, StatementTimeLT: 1}); err == nil {
		t.Fatal("expected invalid range")
	}
}

func TestFinanceClientUsesOfficialAllStatusDefaultWhenStatusIsOmitted(t *testing.T) {
	now := time.Unix(1_725_000_000, 0)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("payment_status"); got != "" {
			t.Fatalf("payment_status=%q; expected omitted parameter", got)
		}
		_, _ = w.Write([]byte(`{"code":0,"request_id":"all-statuses","data":{"total_count":0,"statements":[]}}`))
	}))
	defer server.Close()
	client, err := NewFinanceClient(FinanceClientConfig{BaseURL: server.URL, AppKey: "key", AppSecret: "secret", HTTPClient: server.Client(), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	result, requestID, err := client.SearchStatements(context.Background(), "token", "cipher", SearchStatementsRequest{PageSize: 1, StatementTimeGE: now.Add(-time.Hour).Unix(), StatementTimeLT: now.Unix()})
	if err != nil || requestID != "all-statuses" || len(result.Statements) != 0 {
		t.Fatalf("result=%+v requestID=%q err=%v", result, requestID, err)
	}
}

func TestFinanceClientSearchesWithdrawalRoundsWithoutSensitivePayoutFields(t *testing.T) {
	now := time.Unix(1_725_000_000, 0)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != PathFinanceWithdrawals {
			t.Fatalf("request=%s %s", r.Method, r.URL.Path)
		}
		query := r.URL.Query()
		provided := query.Get("sign")
		query.Del("sign")
		if got := query.Get("types"); got != "WITHDRAW" {
			t.Fatalf("types=%q", got)
		}
		expected, err := SignRequest("secret", PathFinanceWithdrawals, query, nil, false)
		if err != nil || provided != expected {
			t.Fatalf("signature=%q expected=%q err=%v", provided, expected, err)
		}
		_, _ = w.Write([]byte(`{"code":0,"request_id":"withdrawal-request","data":{"total_count":1,"withdrawals":[{"id":"withdrawal-1","type":"WITHDRAW","amount":"100.00","currency":"THB","status":"SUCCESS","create_time":1725000000,"bank_account":"must-not-cross"}]}}`))
	}))
	defer server.Close()
	client, err := NewFinanceClient(FinanceClientConfig{BaseURL: server.URL, AppKey: "key", AppSecret: "secret", HTTPClient: server.Client(), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	result, requestID, err := client.SearchWithdrawals(context.Background(), "token", "cipher", SearchWithdrawalsRequest{Types: []WithdrawalType{WithdrawalTypeWithdraw}, PageSize: 1, CreateTimeGE: now.Add(-time.Hour).Unix(), CreateTimeLT: now.Unix()})
	if err != nil || requestID != "withdrawal-request" || len(result.Withdrawals) != 1 {
		t.Fatalf("result=%+v requestID=%q err=%v", result, requestID, err)
	}
	if result.Withdrawals[0].WithdrawalID != "withdrawal-1" || result.Withdrawals[0].Amount != "100.00" || result.Withdrawals[0].Currency != "THB" {
		t.Fatalf("withdrawal=%+v", result.Withdrawals[0])
	}
}

func TestFinanceClientRejectsWithdrawalSearchWithoutARealRoundWindow(t *testing.T) {
	client, err := NewFinanceClient(FinanceClientConfig{BaseURL: "https://example.test", AppKey: "key", AppSecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = client.SearchWithdrawals(context.Background(), "token", "cipher", SearchWithdrawalsRequest{Types: []WithdrawalType{WithdrawalTypeWithdraw}, PageSize: 1, CreateTimeGE: 2, CreateTimeLT: 2})
	if err == nil {
		t.Fatal("expected invalid withdrawal range")
	}
}

func TestFinanceClientRetriesRateLimitAndPropagatesStatementTransactionCurrency(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if r.URL.Path != "/finance/202501/statements/statement-1/statement_transactions" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		if attempts == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"code":0,"request_id":"rate-limited","data":null}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"request_id":"finance-request","data":{"currency":"THB","total_count":1,"transactions":[{"id":"txn-1","order_id":"order-1","settlement_amount":"95.00"}]}}`))
	}))
	defer server.Close()
	client, err := NewFinanceClient(FinanceClientConfig{BaseURL: server.URL, AppKey: "key", AppSecret: "secret", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	result, requestID, err := client.GetStatementTransactions(context.Background(), "token", "cipher", "statement-1", "", 100)
	if err != nil || attempts != 2 || requestID != "finance-request" {
		t.Fatalf("attempts=%d requestID=%q err=%v", attempts, requestID, err)
	}
	if len(result.Transactions) != 1 || result.Transactions[0].Currency != "THB" {
		t.Fatalf("transactions=%+v", result.Transactions)
	}
}
