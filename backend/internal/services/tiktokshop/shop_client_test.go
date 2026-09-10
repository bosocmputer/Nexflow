package tiktokshop

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestShopClientGetsEveryAuthorizedShopWithSignedRequest(t *testing.T) {
	fixedNow := time.Date(2026, time.September, 10, 9, 30, 0, 0, time.UTC)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != PathGetAuthorizedShops {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("x-tts-access-token") != "access-token" {
			t.Fatalf("access token header = %q", r.Header.Get("x-tts-access-token"))
		}
		query := r.URL.Query()
		if query.Get("app_key") != "app-key" || query.Get("timestamp") != "1789032600" {
			t.Fatalf("query = %v", query)
		}
		receivedSign := query.Get("sign")
		query.Del("sign")
		expectedSign, err := SignRequest("app-secret", PathGetAuthorizedShops, query, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		if receivedSign != expectedSign {
			t.Fatalf("sign = %q, want %q", receivedSign, expectedSign)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"Success","request_id":"request-3","data":{"shops":[{"id":"7000714532876273420","name":"AOY Main","region":"TH","seller_type":"LOCAL","cipher":"TTP_abc","code":"THAOY1"},{"id":"7000714532876273421","name":"AOY Outlet","region":"TH","seller_type":"LOCAL","cipher":"TTP_def","code":"THAOY2"}]}}`))
	}))
	defer server.Close()

	client, err := NewShopClient(ShopClientConfig{
		BaseURL: server.URL, AppKey: "app-key", AppSecret: "app-secret",
		HTTPClient: server.Client(), Now: func() time.Time { return fixedNow },
	})
	if err != nil {
		t.Fatalf("NewShopClient() error = %v", err)
	}
	shops, requestID, err := client.GetAuthorizedShops(context.Background(), "access-token")
	if err != nil {
		t.Fatalf("GetAuthorizedShops() error = %v", err)
	}
	if requestID != "request-3" || len(shops) != 2 {
		t.Fatalf("shops = %+v, requestID = %q", shops, requestID)
	}
	if shops[0].ID != "7000714532876273420" || shops[0].Cipher != "TTP_abc" || shops[1].Name != "AOY Outlet" {
		t.Fatalf("shops = %+v", shops)
	}
}

func TestShopClientRejectsMissingAccessTokenBeforeNetwork(t *testing.T) {
	client, err := NewShopClient(ShopClientConfig{BaseURL: "https://open-api.tiktokglobalshop.com", AppKey: "app-key", AppSecret: "app-secret"})
	if err != nil {
		t.Fatalf("NewShopClient() error = %v", err)
	}
	if _, _, err := client.GetAuthorizedShops(context.Background(), " "); !errors.Is(err, ErrInvalidShopInput) {
		t.Fatalf("GetAuthorizedShops() error = %v", err)
	}
}

func TestShopClientRejectsMalformedShopWithoutReturningPartialResults(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"Success","request_id":"request-4","data":{"shops":[{"id":"7000714532876273420","name":"AOY","region":"TH","cipher":"TTP_abc"},{"id":"7000714532876273421","name":"Broken","region":"TH","cipher":""}]}}`))
	}))
	defer server.Close()

	client, err := NewShopClient(ShopClientConfig{BaseURL: server.URL, AppKey: "app-key", AppSecret: "app-secret", HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("NewShopClient() error = %v", err)
	}
	shops, _, err := client.GetAuthorizedShops(context.Background(), "access-token")
	if !errors.Is(err, ErrInvalidShopResponse) || shops != nil {
		t.Fatalf("shops = %+v, error = %v", shops, err)
	}
}

func TestShopClientReturnsSanitizedTikTokError(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":36009004,"message":"seller secret detail","request_id":"request-5"}`))
	}))
	defer server.Close()

	client, err := NewShopClient(ShopClientConfig{BaseURL: server.URL, AppKey: "app-key", AppSecret: "app-secret", HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("NewShopClient() error = %v", err)
	}
	_, _, err = client.GetAuthorizedShops(context.Background(), "access-token")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 36009004 || apiErr.RequestID != "request-5" {
		t.Fatalf("error = %#v", err)
	}
}
