package tiktokgateway

import (
	"context"
	"errors"
	"testing"
)

type webhookUpdaterFake struct {
	accessToken, shopCipher, address string
	requestID                        string
	err                              error
}

func (f *webhookUpdaterFake) UpdateOrderStatusWebhook(_ context.Context, accessToken, shopCipher, address string) (string, error) {
	f.accessToken, f.shopCipher, f.address = accessToken, shopCipher, address
	return f.requestID, f.err
}

func TestWebhookConfigServiceUsesTenantScopedCredential(t *testing.T) {
	credentials := &fakeOrderCredentialProvider{credential: &AccessCredential{AccessToken: "access-secret", ShopID: "shop-1", ShopCipher: "cipher-1"}}
	updater := &webhookUpdaterFake{requestID: "tts-event-request-1"}
	service, err := NewWebhookConfigService(credentials, updater)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ConfigureOrderStatus(t.Context(), "AOY", "shop-1", "https://gateway.example.com/webhook/tiktok-shop")
	if err != nil {
		t.Fatal(err)
	}
	if credentials.tenant != "aoy" || credentials.shopID != "shop-1" || updater.accessToken != "access-secret" || updater.shopCipher != "cipher-1" {
		t.Fatalf("credentials=%+v updater=%+v", credentials, updater)
	}
	if result.ShopID != "shop-1" || result.EventType != "ORDER_STATUS_CHANGE" || result.UpstreamRequestID != "tts-event-request-1" {
		t.Fatalf("result=%+v", result)
	}
}

func TestWebhookConfigServiceStopsBeforeUpdateWhenCredentialFails(t *testing.T) {
	credentials := &fakeOrderCredentialProvider{err: ErrRefreshTokenExpired}
	updater := &webhookUpdaterFake{}
	service, err := NewWebhookConfigService(credentials, updater)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfigureOrderStatus(t.Context(), "aoy", "shop-1", "https://gateway.example.com/webhook/tiktok-shop"); !errors.Is(err, ErrRefreshTokenExpired) {
		t.Fatalf("error=%v", err)
	}
	if updater.address != "" {
		t.Fatalf("updater called: %+v", updater)
	}
}
