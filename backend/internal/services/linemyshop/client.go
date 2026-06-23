package linemyshop

import (
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

const DefaultBaseURL = "https://developers-oaplus.line.biz"

type Client struct {
	baseURL    string
	apiKey     string
	userAgent  string
	httpClient *http.Client
}

type ListOrdersQuery struct {
	Search         string
	Page           int
	PerPage        int
	SortBy         string
	OrderBy        string
	OrderStatus    []string
	PaymentStatus  []string
	PaymentMethod  []string
	ShipmentStatus []string
	OrderType      []string
	StartAt        string
	EndAt          string
}

type APIError struct {
	StatusCode int
	Body       string
}

func (e APIError) Error() string {
	if strings.TrimSpace(e.Body) == "" {
		return fmt.Sprintf("line myshop api returned HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("line myshop api returned HTTP %d: %s", e.StatusCode, e.Body)
}

func NewClient(apiKey string, opts ...Option) *Client {
	c := &Client{
		baseURL:   DefaultBaseURL,
		apiKey:    strings.TrimSpace(apiKey),
		userAgent: "Nexflow LINE MyShop",
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type Option func(*Client)

func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		if v := strings.TrimRight(strings.TrimSpace(baseURL), "/"); v != "" {
			c.baseURL = v
		}
	}
}

func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		if client != nil {
			c.httpClient = client
		}
	}
}

func WithUserAgent(userAgent string) Option {
	return func(c *Client) {
		if v := strings.TrimSpace(userAgent); v != "" {
			c.userAgent = v
		}
	}
}

func (c *Client) ListOrders(ctx context.Context, q ListOrdersQuery) (json.RawMessage, error) {
	values := url.Values{}
	if q.Search != "" {
		values.Set("search", q.Search)
	}
	if q.Page > 0 {
		values.Set("page", strconv.Itoa(q.Page))
	}
	if q.PerPage > 0 {
		values.Set("perPage", strconv.Itoa(q.PerPage))
	}
	if q.SortBy != "" {
		values.Set("sortBy", q.SortBy)
	}
	if q.OrderBy != "" {
		values.Set("orderBy", q.OrderBy)
	}
	if q.StartAt != "" {
		values.Set("startAt", q.StartAt)
	}
	if q.EndAt != "" {
		values.Set("endAt", q.EndAt)
	}
	addList(values, "orderStatus", q.OrderStatus)
	addList(values, "paymentStatus", q.PaymentStatus)
	addList(values, "paymentMethod", q.PaymentMethod)
	addList(values, "shipmentStatus", q.ShipmentStatus)
	addList(values, "orderType", q.OrderType)
	path := "/myshop/v1/orders"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	return c.do(ctx, http.MethodGet, path, nil)
}

func (c *Client) GetOrder(ctx context.Context, orderNo string) (json.RawMessage, error) {
	orderNo = strings.TrimSpace(orderNo)
	if orderNo == "" {
		return nil, fmt.Errorf("order_no is required")
	}
	return c.do(ctx, http.MethodGet, "/myshop/v1/orders/"+url.PathEscape(orderNo), nil)
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader) (json.RawMessage, error) {
	if strings.TrimSpace(c.apiKey) == "" {
		return nil, fmt.Errorf("line myshop api key is required")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-KEY", c.apiKey)
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, APIError{StatusCode: resp.StatusCode, Body: string(data)}
	}
	return json.RawMessage(data), nil
}

func addList(values url.Values, key string, items []string) {
	for _, item := range items {
		if v := strings.TrimSpace(item); v != "" {
			values.Add(key, v)
		}
	}
}
