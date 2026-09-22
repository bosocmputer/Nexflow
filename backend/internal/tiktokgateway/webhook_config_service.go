package tiktokgateway

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"nexflow/internal/services/tiktokshop"
)

var ErrWebhookConfigServiceNotConfigured = errors.New("TikTok Shop webhook configuration service is not configured")

type ShopWebhookUpdater interface {
	UpdateOrderStatusWebhook(context.Context, string, string, string) (string, error)
	UpdateCancellationStatusWebhook(context.Context, string, string, string) (string, error)
}

type WebhookConfigService struct {
	credentials OrderCredentialProvider
	webhooks    ShopWebhookUpdater
}

type WebhookConfigResult struct {
	ShopID            string `json:"shop_id"`
	EventType         string `json:"event_type"`
	Address           string `json:"address"`
	UpstreamRequestID string `json:"upstream_request_id"`
}

func NewWebhookConfigService(credentials OrderCredentialProvider, webhooks ShopWebhookUpdater) (*WebhookConfigService, error) {
	if credentials == nil || webhooks == nil {
		return nil, ErrWebhookConfigServiceNotConfigured
	}
	return &WebhookConfigService{credentials: credentials, webhooks: webhooks}, nil
}

func (s *WebhookConfigService) ConfigureOrderStatus(ctx context.Context, tenantSlug, shopID, address string) (*WebhookConfigResult, error) {
	return s.configure(ctx, tenantSlug, shopID, address, tiktokshop.EventTypeOrderStatusChange)
}

// ConfigureCancellationStatus is intentionally only a prepared Gateway action.
// The tenant must keep its cancellation gate off until a controlled canary is
// explicitly approved, so exposing this method never enables the tenant flow.
func (s *WebhookConfigService) ConfigureCancellationStatus(ctx context.Context, tenantSlug, shopID, address string) (*WebhookConfigResult, error) {
	return s.configure(ctx, tenantSlug, shopID, address, tiktokshop.EventTypeCancellationStatusChange)
}

func (s *WebhookConfigService) configure(ctx context.Context, tenantSlug, shopID, address, eventType string) (*WebhookConfigResult, error) {
	if s == nil || s.credentials == nil || s.webhooks == nil {
		return nil, ErrWebhookConfigServiceNotConfigured
	}
	tenantSlug = strings.ToLower(strings.TrimSpace(tenantSlug))
	shopID = strings.TrimSpace(shopID)
	address = strings.TrimSpace(address)
	if tenantSlug == "" || shopID == "" || address == "" {
		return nil, ErrInvalidTokenCredential
	}
	credential, err := s.credentials.AccessCredential(ctx, tenantSlug, shopID)
	if err != nil {
		return nil, err
	}
	if credential == nil || credential.ShopID != shopID || strings.TrimSpace(credential.AccessToken) == "" || strings.TrimSpace(credential.ShopCipher) == "" {
		return nil, ErrInvalidTokenCredential
	}
	var requestID string
	if eventType == tiktokshop.EventTypeCancellationStatusChange {
		requestID, err = s.webhooks.UpdateCancellationStatusWebhook(ctx, credential.AccessToken, credential.ShopCipher, address)
	} else {
		requestID, err = s.webhooks.UpdateOrderStatusWebhook(ctx, credential.AccessToken, credential.ShopCipher, address)
	}
	if err != nil {
		return nil, fmt.Errorf("configure TikTok Shop webhook: %w", err)
	}
	if strings.TrimSpace(requestID) == "" {
		return nil, ErrInvalidTokenCredential
	}
	return &WebhookConfigResult{
		ShopID: shopID, EventType: eventType, Address: address,
		UpstreamRequestID: strings.TrimSpace(requestID),
	}, nil
}
