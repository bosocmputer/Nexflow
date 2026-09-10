package tiktokshop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultOpenAPIBaseURL  = "https://open-api.tiktokglobalshop.com"
	PathGetAuthorizedShops = "/authorization/202309/shops"
)

var (
	ErrInvalidShopInput    = errors.New("invalid TikTok Shop authorized-shops input")
	ErrInvalidShopResponse = errors.New("invalid TikTok Shop authorized-shops response")
)

type ShopClientConfig struct {
	BaseURL    string
	AppKey     string
	AppSecret  string
	HTTPClient *http.Client
	Now        func() time.Time
}

type ShopClient struct {
	baseURL   *url.URL
	appKey    string
	appSecret string
	http      *http.Client
	now       func() time.Time
}

type AuthorizedShop struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Region     string `json:"region"`
	SellerType string `json:"seller_type"`
	Cipher     string `json:"cipher"`
	Code       string `json:"code"`
}

func NewShopClient(config ShopClientConfig) (*ShopClient, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultOpenAPIBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("TikTok Shop Open API base URL must be an absolute HTTPS URL")
	}
	if strings.TrimSpace(config.AppKey) == "" || strings.TrimSpace(config.AppSecret) == "" {
		return nil, ErrInvalidShopInput
	}
	client := config.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &ShopClient{
		baseURL: parsed, appKey: strings.TrimSpace(config.AppKey), appSecret: strings.TrimSpace(config.AppSecret),
		http: client, now: now,
	}, nil
}

func (c *ShopClient) GetAuthorizedShops(ctx context.Context, accessToken string) ([]AuthorizedShop, string, error) {
	accessToken = strings.TrimSpace(accessToken)
	if c == nil || c.baseURL == nil || accessToken == "" {
		return nil, "", ErrInvalidShopInput
	}
	query := url.Values{
		"app_key":   []string{c.appKey},
		"timestamp": []string{strconv.FormatInt(c.now().Unix(), 10)},
	}
	signature, err := SignRequest(c.appSecret, PathGetAuthorizedShops, query, nil, false)
	if err != nil {
		return nil, "", fmt.Errorf("sign TikTok Shop authorized-shops request: %w", err)
	}
	query.Set("sign", signature)

	requestURL := *c.baseURL
	requestURL.Path = strings.TrimRight(requestURL.Path, "/") + PathGetAuthorizedShops
	requestURL.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, "", fmt.Errorf("create TikTok Shop authorized-shops request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-tts-access-token", accessToken)

	response, err := c.http.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("call TikTok Shop authorized-shops API: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, "", fmt.Errorf("read TikTok Shop authorized-shops response: %w", err)
	}
	var payload authorizedShopsResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, "", fmt.Errorf("decode TikTok Shop authorized-shops response: %w", err)
	}
	requestID := strings.TrimSpace(payload.RequestID)
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices || payload.Code != 0 {
		return nil, requestID, &APIError{Code: payload.Code, RequestID: requestID, Message: "TikTok Shop rejected the authorized-shops request"}
	}
	if len(payload.Data.Shops) == 0 {
		return nil, requestID, ErrInvalidShopResponse
	}
	shops := make([]AuthorizedShop, len(payload.Data.Shops))
	for i, shop := range payload.Data.Shops {
		shop.ID = strings.TrimSpace(shop.ID)
		shop.Name = strings.TrimSpace(shop.Name)
		shop.Region = strings.TrimSpace(shop.Region)
		shop.SellerType = strings.TrimSpace(shop.SellerType)
		shop.Cipher = strings.TrimSpace(shop.Cipher)
		shop.Code = strings.TrimSpace(shop.Code)
		if shop.ID == "" || shop.Cipher == "" {
			return nil, requestID, ErrInvalidShopResponse
		}
		shops[i] = shop
	}
	return shops, requestID, nil
}

type authorizedShopsResponse struct {
	Code      int    `json:"code"`
	RequestID string `json:"request_id"`
	Data      struct {
		Shops []AuthorizedShop `json:"shops"`
	} `json:"data"`
}
