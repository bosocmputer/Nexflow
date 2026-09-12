package tiktokshop

import (
	"bytes"
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
	PathShopWebhooks           = "/event/202309/webhooks"
	EventTypeOrderStatusChange = "ORDER_STATUS_CHANGE"
	maxEventResponseSize       = 1 << 20
)

var (
	ErrInvalidEventInput    = errors.New("invalid TikTok Shop event input")
	ErrInvalidEventResponse = errors.New("invalid TikTok Shop event response")
)

type EventClientConfig struct {
	BaseURL    string
	AppKey     string
	AppSecret  string
	HTTPClient *http.Client
	Now        func() time.Time
}

type EventClient struct {
	baseURL   *url.URL
	appKey    string
	appSecret string
	http      *http.Client
	now       func() time.Time
}

type updateShopWebhookRequest struct {
	Address   string `json:"address"`
	EventType string `json:"event_type"`
}

func NewEventClient(config EventClientConfig) (*EventClient, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultOpenAPIBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, ErrInvalidEventInput
	}
	if strings.TrimSpace(config.AppKey) == "" || strings.TrimSpace(config.AppSecret) == "" {
		return nil, ErrInvalidEventInput
	}
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &EventClient{
		baseURL: parsed, appKey: strings.TrimSpace(config.AppKey), appSecret: strings.TrimSpace(config.AppSecret),
		http: httpClient, now: now,
	}, nil
}

func (c *EventClient) UpdateOrderStatusWebhook(ctx context.Context, accessToken, shopCipher, address string) (string, error) {
	accessToken = strings.TrimSpace(accessToken)
	shopCipher = strings.TrimSpace(shopCipher)
	address = strings.TrimSpace(address)
	if c == nil || c.baseURL == nil || accessToken == "" || shopCipher == "" || !validWebhookAddress(address) {
		return "", ErrInvalidEventInput
	}
	body, err := json.Marshal(updateShopWebhookRequest{Address: address, EventType: EventTypeOrderStatusChange})
	if err != nil {
		return "", fmt.Errorf("encode TikTok Shop webhook update: %w", err)
	}
	query := url.Values{
		"app_key":     []string{c.appKey},
		"shop_cipher": []string{shopCipher},
		"timestamp":   []string{strconv.FormatInt(c.now().Unix(), 10)},
	}
	signature, err := SignRequest(c.appSecret, PathShopWebhooks, query, body, false)
	if err != nil {
		return "", fmt.Errorf("sign TikTok Shop webhook update: %w", err)
	}
	query.Set("sign", signature)
	requestURL := *c.baseURL
	requestURL.Path = strings.TrimRight(requestURL.Path, "/") + PathShopWebhooks
	requestURL.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, requestURL.String(), bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create TikTok Shop webhook update: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("x-tts-access-token", accessToken)
	response, err := c.http.Do(request)
	if err != nil {
		return "", fmt.Errorf("call TikTok Shop webhook API: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxEventResponseSize+1))
	if err != nil || len(responseBody) > maxEventResponseSize {
		return "", ErrInvalidEventResponse
	}
	var payload apiResponse
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return "", fmt.Errorf("decode TikTok Shop webhook response: %w", err)
	}
	requestID := strings.TrimSpace(payload.RequestID)
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices || payload.Code != 0 {
		return requestID, &APIError{Code: payload.Code, RequestID: requestID, Message: "TikTok Shop rejected the webhook update"}
	}
	if requestID == "" {
		return "", ErrInvalidEventResponse
	}
	return requestID, nil
}

func validWebhookAddress(address string) bool {
	if address == "" || len(address) > 255 {
		return false
	}
	parsed, err := url.Parse(address)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == ""
}
