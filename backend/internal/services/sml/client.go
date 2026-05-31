package sml

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Config holds all SML connection parameters
type Config struct {
	BaseURL string // e.g. http://192.168.2.213:3248
}

type Client struct {
	cfg        Config
	httpClient *http.Client
}

func New(cfg Config) *Client {
	return &Client{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// SMLItem is a line item in a sale order
type SMLItem struct {
	ItemCode string  `json:"item_code"`
	Qty      float64 `json:"qty"`
	UnitCode string  `json:"unit_code"`
	Price    float64 `json:"price"`
}

// SaleOrderRequest is the high-level request from the handler
type SaleOrderRequest struct {
	ContactName  string    `json:"contact_name"`
	ContactPhone string    `json:"contact_phone"`
	Remark       string    `json:"remark,omitempty"`
	Items        []SMLItem `json:"items"`
}

// SaleReserveRequest kept as alias for backward compatibility with handler code
type SaleReserveRequest = SaleOrderRequest

// SMLResult is the parsed response from SML
type SMLResult struct {
	Success bool   `json:"success"`
	DocNo   string `json:"doc_no"`
	Message string `json:"message"`
}

// ── JSON-RPC internal types ───────────────────────────────────────────────────

type jsonRPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int           `json:"id"`
	Method  string        `json:"method"`
	Params  jsonRPCParams `json:"params"`
}

type jsonRPCParams struct {
	Name      string          `json:"name"`
	Arguments saleReserveArgs `json:"arguments"`
}

type saleReserveArgs struct {
	ContactName  string         `json:"contact_name"`
	ContactPhone string         `json:"contact_phone"`
	Items        []saleItemArgs `json:"items"`
}

type saleItemArgs struct {
	ItemCode   string  `json:"item_code"`
	Qty        float64 `json:"qty"`
	UnitCode   string  `json:"unit_code"`
	Price      float64 `json:"price"`
	IsGetPrice int     `json:"is_get_price"`
}

// SSE response: event: message\ndata: {"result":{"content":[{"type":"text","text":"..."}]},...}
type sseResult struct {
	Result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"result"`
}

// ── Public API ────────────────────────────────────────────────────────────────

// CreateSaleReserve sends a Sale Reserve (ใบสั่งจอง) to SML via MCP /api/sale_reserve
// with retry (max 3 attempts, backoff 1s/3s/5s)
func (c *Client) CreateSaleReserve(req SaleOrderRequest) (*SMLResult, error) {
	var lastErr error
	backoffs := []time.Duration{1 * time.Second, 3 * time.Second, 5 * time.Second}
	for i := 0; i < 3; i++ {
		result, err := c.doCreate(req)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if i < len(backoffs) {
			time.Sleep(backoffs[i])
		}
	}
	return nil, fmt.Errorf("SML failed after 3 attempts: %w", lastErr)
}

// CreateSaleOrder is an alias for backward compatibility
func (c *Client) CreateSaleOrder(req SaleOrderRequest) (*SMLResult, error) {
	return c.CreateSaleReserve(req)
}

// CreateSaleQuotation is an alias for backward compatibility
func (c *Client) CreateSaleQuotation(req SaleOrderRequest) (*SMLResult, error) {
	return c.CreateSaleReserve(req)
}

func (c *Client) doCreate(req SaleOrderRequest) (*SMLResult, error) {
	rpcReq := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params: jsonRPCParams{
			Name: "create_sale_reserve",
			Arguments: saleReserveArgs{
				ContactName:  req.ContactName,
				ContactPhone: req.ContactPhone,
				Items:        buildItems(req.Items),
			},
		},
	}

	bodyBytes, err := marshalASCII(rpcReq)
	if err != nil {
		return nil, err
	}

	url := c.cfg.BaseURL + "/api/sale_reserve"
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json; charset=utf-8")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	httpReq.Header.Set("mcp-access-mode", "sales")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("SML request: %w", err)
	}
	defer resp.Body.Close()

	// Response is SSE: "event: message\ndata: {...}\n\n"
	// Find the "data:" line and parse it
	dataLine, err := extractSSEData(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read SML SSE response: %w", err)
	}

	// Outer JSON-RPC envelope
	var sse sseResult
	if err := json.Unmarshal([]byte(dataLine), &sse); err != nil {
		return nil, fmt.Errorf("parse SML SSE envelope: %w — data: %s", err, dataLine)
	}
	if len(sse.Result.Content) == 0 {
		return nil, fmt.Errorf("SML SSE: empty content — data: %s", dataLine)
	}

	// Inner JSON string inside content[0].text
	var inner SMLResult
	if err := json.Unmarshal([]byte(sse.Result.Content[0].Text), &inner); err != nil {
		return nil, fmt.Errorf("parse SML inner result: %w — text: %s", err, sse.Result.Content[0].Text)
	}

	if !inner.Success {
		return nil, fmt.Errorf("SML error: %s", inner.Message)
	}
	return &inner, nil
}

// extractSSEData reads SSE stream and returns the first "data:" line content
func extractSSEData(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			return strings.TrimPrefix(line, "data:"), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("no data line found in SSE response")
}

func buildItems(items []SMLItem) []saleItemArgs {
	out := make([]saleItemArgs, len(items))
	for i, it := range items {
		out[i] = saleItemArgs{
			ItemCode:   it.ItemCode,
			Qty:        it.Qty,
			UnitCode:   it.UnitCode,
			Price:      it.Price,
			IsGetPrice: 1,
		}
	}
	return out
}
