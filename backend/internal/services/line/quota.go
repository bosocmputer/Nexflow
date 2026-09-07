package lineservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const maxQuotaResponseBytes = 64 << 10

// MessageQuota is the validated subset of LINE's monthly message quota APIs.
// Pointer fields remain nil when LINE does not return a numeric limit.
type MessageQuota struct {
	Type      string `json:"quota_type"`
	Limit     *int64 `json:"limit"`
	Used      int64  `json:"used"`
	Remaining *int64 `json:"remaining"`
}

// QuotaAPIError is intentionally body-free. LINE access tokens and raw error
// bodies must never travel into application logs or API responses.
type QuotaAPIError struct {
	Code       string
	HTTPStatus int
}

func (e *QuotaAPIError) Error() string {
	if e == nil {
		return "LINE quota request failed"
	}
	if e.HTTPStatus > 0 {
		return fmt.Sprintf("LINE quota request failed (%s, HTTP %d)", e.Code, e.HTTPStatus)
	}
	return fmt.Sprintf("LINE quota request failed (%s)", e.Code)
}

func QuotaErrorCode(err error) string {
	var apiErr *QuotaAPIError
	if errors.As(err, &apiErr) && apiErr.Code != "" {
		return apiErr.Code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "line_timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "request_cancelled"
	}
	return "line_unavailable"
}

type quotaLimitResponse struct {
	Type  string `json:"type"`
	Value *int64 `json:"value"`
}

type quotaConsumptionResponse struct {
	TotalUsage *int64 `json:"totalUsage"`
}

// GetMessageQuota calls both LINE quota endpoints. Each outbound attempt is
// bounded to five seconds. A 429 or 5xx response is retried once, while
// authentication and validation failures fail immediately.
func (s *Service) GetMessageQuota(ctx context.Context) (MessageQuota, error) {
	if s == nil || strings.TrimSpace(s.accessToken) == "" {
		return MessageQuota{}, &QuotaAPIError{Code: "line_token_missing"}
	}
	var limit quotaLimitResponse
	if err := s.getQuotaJSON(ctx, "/v2/bot/message/quota", &limit); err != nil {
		return MessageQuota{}, err
	}
	limit.Type = strings.ToLower(strings.TrimSpace(limit.Type))
	switch limit.Type {
	case "limited":
		if limit.Value == nil || *limit.Value < 0 {
			return MessageQuota{}, &QuotaAPIError{Code: "invalid_quota_response"}
		}
	case "unlimited", "none":
		if limit.Value != nil && *limit.Value < 0 {
			return MessageQuota{}, &QuotaAPIError{Code: "invalid_quota_response"}
		}
		limit.Value = nil
	default:
		return MessageQuota{}, &QuotaAPIError{Code: "unsupported_quota_type"}
	}

	var consumption quotaConsumptionResponse
	if err := s.getQuotaJSON(ctx, "/v2/bot/message/quota/consumption", &consumption); err != nil {
		return MessageQuota{}, err
	}
	if consumption.TotalUsage == nil || *consumption.TotalUsage < 0 {
		return MessageQuota{}, &QuotaAPIError{Code: "invalid_consumption_response"}
	}

	result := MessageQuota{Type: limit.Type, Limit: limit.Value, Used: *consumption.TotalUsage}
	if limit.Type == "limited" && limit.Value != nil {
		remaining := *limit.Value - result.Used
		if remaining < 0 {
			remaining = 0
		}
		result.Remaining = &remaining
	}
	return result, nil
}

func (s *Service) getQuotaJSON(ctx context.Context, path string, target any) error {
	for attempt := 0; attempt < 2; attempt++ {
		status, retryAfter, err := s.getQuotaJSONOnce(ctx, path, target)
		if err == nil {
			return nil
		}
		if attempt > 0 || (status != http.StatusTooManyRequests && status < 500) {
			return err
		}
		if retryAfter > 0 {
			timer := time.NewTimer(min(retryAfter, 2*time.Second))
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return &QuotaAPIError{Code: "line_unavailable"}
}

func (s *Service) getQuotaJSONOnce(ctx context.Context, path string, target any) (int, time.Duration, error) {
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	baseURL := strings.TrimRight(strings.TrimSpace(s.apiBaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.line.me"
	}
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return 0, 0, &QuotaAPIError{Code: "invalid_line_request"}
	}
	req.Header.Set("Authorization", "Bearer "+s.accessToken)
	client := s.httpClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		if requestCtx.Err() != nil {
			return 0, 0, requestCtx.Err()
		}
		return 0, 0, &QuotaAPIError{Code: "line_transport_error"}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxQuotaResponseBytes))
		return resp.StatusCode, parseRetryAfter(resp.Header.Get("Retry-After")), &QuotaAPIError{
			Code:       quotaHTTPErrorCode(resp.StatusCode),
			HTTPStatus: resp.StatusCode,
		}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxQuotaResponseBytes+1))
	if err != nil {
		return resp.StatusCode, 0, &QuotaAPIError{Code: "line_response_read_failed"}
	}
	if len(body) > maxQuotaResponseBytes {
		return resp.StatusCode, 0, &QuotaAPIError{Code: "line_response_too_large"}
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	if err := decoder.Decode(target); err != nil {
		return resp.StatusCode, 0, &QuotaAPIError{Code: "invalid_line_json"}
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return resp.StatusCode, 0, &QuotaAPIError{Code: "invalid_line_json"}
	}
	return resp.StatusCode, 0, nil
}

func quotaHTTPErrorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "line_bad_request"
	case http.StatusUnauthorized:
		return "line_token_invalid"
	case http.StatusForbidden:
		return "line_forbidden"
	case http.StatusTooManyRequests:
		return "line_rate_limited"
	default:
		if status >= 500 {
			return "line_server_error"
		}
		return "line_http_error"
	}
}

func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	if retryAt, err := http.ParseTime(value); err == nil {
		return max(time.Until(retryAt), 0)
	}
	return 0
}
