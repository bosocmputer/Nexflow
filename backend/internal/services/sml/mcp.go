package sml

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// MCPClient calls SML MCP tools via POST /call
type MCPClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewMCPClient(baseURL string) *MCPClient {
	return &MCPClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// ProductResult is a single product from search_product
type ProductResult struct {
	Code           string  `json:"code"`
	Name           string  `json:"name"`
	Name2          string  `json:"name_2"`
	UnitStandard   string  `json:"unit_standard"`
	RelevanceScore float64 `json:"relevance_score"`
}

// PriceEntry is one price row from get_product_price
type PriceEntry struct {
	UnitCode string  `json:"unit_code"`
	UnitName string  `json:"unit_name"`
	Price    float64 `json:"price"`
}

// SearchProduct searches for products by keyword using MCP search_product tool.
// Returns up to limit results (max 10 recommended).
// NOTE: call SearchProduct and GetProductPrice only — do NOT call search_customer
// (broken: pg_trgm extension missing on SML server).
func (m *MCPClient) SearchProduct(keyword string, limit int) ([]ProductResult, error) {
	if limit <= 0 {
		limit = 5
	}
	args := map[string]interface{}{
		"keyword": keyword,
		"limit":   limit,
	}
	text, err := m.callTool("search_product", args)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Products []ProductResult `json:"products"`
	}
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		return nil, fmt.Errorf("parse search_product: %w — text: %s", err, text)
	}
	return resp.Products, nil
}

// GetProductPrice retrieves prices for a product code.
// Returns the first available price entry, or nil if not found.
func (m *MCPClient) GetProductPrice(code string) (*PriceEntry, error) {
	args := map[string]interface{}{
		"code": code,
	}
	text, err := m.callTool("get_product_price", args)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Error    bool   `json:"error"`
		Message  string `json:"message"`
		Products []struct {
			Code   string       `json:"code"`
			Prices []PriceEntry `json:"prices"`
		} `json:"products"`
	}
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		return nil, fmt.Errorf("parse get_product_price: %w — text: %s", err, text)
	}
	if resp.Error {
		return nil, fmt.Errorf("get_product_price: %s", resp.Message)
	}
	if len(resp.Products) == 0 || len(resp.Products[0].Prices) == 0 {
		return nil, nil // not found — caller handles gracefully
	}
	p := resp.Products[0].Prices[0]
	return &p, nil
}

// callTool posts to /call and returns the text content from the response
func (m *MCPClient) callTool(name string, arguments map[string]interface{}) (string, error) {
	payload := map[string]interface{}{
		"name":      name,
		"arguments": arguments,
	}
	body, err := marshalASCII(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", m.baseURL+"/call", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("mcp-access-mode", "sales")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("MCP call %s: %w", name, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read MCP response %s: %w", name, err)
	}

	var result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("parse MCP envelope %s: %w — body: %s", name, err, string(raw))
	}
	if result.IsError {
		if len(result.Content) > 0 {
			return "", fmt.Errorf("MCP tool %s error: %s", name, result.Content[0].Text)
		}
		return "", fmt.Errorf("MCP tool %s: unknown error", name)
	}
	if len(result.Content) == 0 {
		return "", fmt.Errorf("MCP tool %s: empty content", name)
	}
	return result.Content[0].Text, nil
}
