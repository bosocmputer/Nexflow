package tiktokshop

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestEventClientUpdatesOnlyOrderStatusWebhook(t *testing.T) {
	now := time.Unix(1789218000, 0)
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPut || request.URL.Path != PathShopWebhooks {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		var input updateShopWebhookRequest
		if err := json.Unmarshal(body, &input); err != nil {
			t.Fatal(err)
		}
		if input.Address != "https://gateway.example.com/webhook/tiktok-shop" || input.EventType != EventTypeOrderStatusChange {
			t.Fatalf("input = %+v", input)
		}
		query := request.URL.Query()
		gotSignature := query.Get("sign")
		query.Del("sign")
		wantSignature, err := SignRequest("app-secret", PathShopWebhooks, query, body, false)
		if err != nil || gotSignature != wantSignature {
			t.Fatalf("signature = %q, want %q, error=%v", gotSignature, wantSignature, err)
		}
		if request.Header.Get("x-tts-access-token") != "access-secret" || query.Get("shop_cipher") != "shop-cipher" {
			t.Fatalf("token/cipher missing")
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"code":0,"data":{},"message":"Success","request_id":"tts-event-request-1"}`))
	}))
	defer server.Close()
	baseURL, _ := url.Parse(server.URL)
	client, err := NewEventClient(EventClientConfig{
		BaseURL: baseURL.String(), AppKey: "app-key", AppSecret: "app-secret", HTTPClient: server.Client(), Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	requestID, err := client.UpdateOrderStatusWebhook(context.Background(), "access-secret", "shop-cipher", "https://gateway.example.com/webhook/tiktok-shop")
	if err != nil || requestID != "tts-event-request-1" {
		t.Fatalf("requestID=%q error=%v", requestID, err)
	}
}

func TestEventClientRejectsUnsafeWebhookAddressBeforeRequest(t *testing.T) {
	client, err := NewEventClient(EventClientConfig{BaseURL: "https://open-api.example.com", AppKey: "app-key", AppSecret: "app-secret"})
	if err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{"", "http://gateway.example.com/webhook", "https://user@gateway.example.com/webhook", "https://gateway.example.com/webhook?token=1"} {
		if _, err := client.UpdateOrderStatusWebhook(context.Background(), "token", "cipher", address); err == nil {
			t.Fatalf("address %q accepted", address)
		}
	}
}
