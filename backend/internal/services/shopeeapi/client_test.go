package shopeeapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"
)

func TestAuthURLSignsPartnerPathTimestampAndState(t *testing.T) {
	client := New(Config{
		BaseURL:    DefaultSandboxBaseURL + "/",
		PartnerID:  1233790,
		PartnerKey: "secret",
	})
	now := time.Unix(1779180000, 0)
	got, err := client.AuthURL("https://example.com/api/shopee-api/callback", "state-1", now)
	if err != nil {
		t.Fatalf("AuthURL() error = %v", err)
	}
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	q := parsed.Query()
	if q.Get("state") != "state-1" {
		t.Fatalf("state = %q", q.Get("state"))
	}
	if q.Get("redirect") != "https://example.com/api/shopee-api/callback" {
		t.Fatalf("redirect = %q", q.Get("redirect"))
	}
	wantSign := testShopeeSign("secret", "1233790"+PathAuthPartner+"1779180000")
	if q.Get("sign") != wantSign {
		t.Fatalf("sign = %q, want %q", q.Get("sign"), wantSign)
	}
}

func TestShopSignatureIncludesAccessTokenAndShopID(t *testing.T) {
	client := New(Config{PartnerID: 1233790, PartnerKey: "secret", BaseURL: DefaultSandboxBaseURL})
	got := client.sign(PathOrderList, 1779180000, "access-token", 987654)
	base := strconv.FormatInt(1233790, 10) + PathOrderList + "1779180000" + "access-token" + "987654"
	want := testShopeeSign("secret", base)
	if got != want {
		t.Fatalf("shop sign = %q, want %q", got, want)
	}
}

func TestGetOrderListUsesShopSignatureAndDecodesResponse(t *testing.T) {
	var gotPath string
	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"response": map[string]interface{}{
				"more":        true,
				"next_cursor": "cursor-2",
				"order_list": []map[string]string{
					{"order_sn": "250520ABC", "order_status": "READY_TO_SHIP"},
				},
			},
		})
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, PartnerID: 1233790, PartnerKey: "secret"})
	out, err := client.GetOrderList(t.Context(), "access-token", 987654, OrderListRequest{
		TimeRangeField: "update_time",
		TimeFrom:       1779100000,
		TimeTo:         1779180000,
		PageSize:       20,
		Cursor:         "cursor-1",
		OrderStatus:    "READY_TO_SHIP",
	})
	if err != nil {
		t.Fatalf("GetOrderList() error = %v", err)
	}
	if gotPath != PathOrderList {
		t.Fatalf("path = %q, want %q", gotPath, PathOrderList)
	}
	if gotQuery.Get("partner_id") != "1233790" || gotQuery.Get("access_token") != "access-token" || gotQuery.Get("shop_id") != "987654" {
		t.Fatalf("missing auth query values: %v", gotQuery)
	}
	if gotQuery.Get("time_range_field") != "update_time" || gotQuery.Get("cursor") != "cursor-1" || gotQuery.Get("order_status") != "READY_TO_SHIP" {
		t.Fatalf("missing order query values: %v", gotQuery)
	}
	ts, err := strconv.ParseInt(gotQuery.Get("timestamp"), 10, 64)
	if err != nil || ts <= 0 {
		t.Fatalf("timestamp = %q", gotQuery.Get("timestamp"))
	}
	base := "1233790" + PathOrderList + gotQuery.Get("timestamp") + "access-token" + "987654"
	if gotQuery.Get("sign") != testShopeeSign("secret", base) {
		t.Fatalf("sign = %q", gotQuery.Get("sign"))
	}
	if !out.Response.More || out.Response.NextCursor != "cursor-2" || len(out.Response.OrderList) != 1 {
		t.Fatalf("decoded response = %+v", out.Response)
	}
	if out.Response.OrderList[0].OrderSN != "250520ABC" {
		t.Fatalf("order_sn = %q", out.Response.OrderList[0].OrderSN)
	}
}

func TestGetShopInfoUsesShopSignatureAndDecodesName(t *testing.T) {
	var gotPath string
	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"response": map[string]interface{}{
				"shop_name": "semicolon.con",
				"region":    "TH",
				"status":    "NORMAL",
			},
		})
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, PartnerID: 1233790, PartnerKey: "secret"})
	out, err := client.GetShopInfo(t.Context(), "access-token", 987654)
	if err != nil {
		t.Fatalf("GetShopInfo() error = %v", err)
	}
	if gotPath != PathShopInfo {
		t.Fatalf("path = %q, want %q", gotPath, PathShopInfo)
	}
	if gotQuery.Get("partner_id") != "1233790" || gotQuery.Get("access_token") != "access-token" || gotQuery.Get("shop_id") != "987654" {
		t.Fatalf("missing auth query values: %v", gotQuery)
	}
	base := "1233790" + PathShopInfo + gotQuery.Get("timestamp") + "access-token" + "987654"
	if gotQuery.Get("sign") != testShopeeSign("secret", base) {
		t.Fatalf("sign = %q", gotQuery.Get("sign"))
	}
	if out.Response.ShopName != "semicolon.con" || out.Response.Region != "TH" || out.Response.Status != "NORMAL" {
		t.Fatalf("decoded response = %+v", out.Response)
	}
}

func TestGetShopProfileUsesShopSignatureAndDecodesName(t *testing.T) {
	var gotPath string
	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"response": map[string]interface{}{
				"shop_name":   "Henna.milkford",
				"description": "hello",
				"shop_logo":   "https://example.com/logo.jpg",
			},
		})
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, PartnerID: 1233790, PartnerKey: "secret"})
	out, err := client.GetShopProfile(t.Context(), "access-token", 987654)
	if err != nil {
		t.Fatalf("GetShopProfile() error = %v", err)
	}
	if gotPath != PathShopProfile {
		t.Fatalf("path = %q, want %q", gotPath, PathShopProfile)
	}
	base := "1233790" + PathShopProfile + gotQuery.Get("timestamp") + "access-token" + "987654"
	if gotQuery.Get("sign") != testShopeeSign("secret", base) {
		t.Fatalf("sign = %q", gotQuery.Get("sign"))
	}
	if out.Response.ShopName != "Henna.milkford" {
		t.Fatalf("shop_name = %q", out.Response.ShopName)
	}
}

func TestGetOrderListReturnsShopeeBusinessError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":   "error_sign",
			"message": "wrong sign",
		})
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, PartnerID: 1233790, PartnerKey: "secret"})
	_, err := client.GetOrderList(t.Context(), "access-token", 987654, OrderListRequest{
		TimeFrom: 1779100000,
		TimeTo:   1779180000,
	})
	if err == nil {
		t.Fatal("expected Shopee business error")
	}
	if got := err.Error(); got != "shopee get_order_list: error_sign wrong sign" {
		t.Fatalf("error = %q", got)
	}
}

func TestGetOrderDetailRejectsMoreThan50BeforeHTTP(t *testing.T) {
	hit := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, PartnerID: 1233790, PartnerKey: "secret"})
	orderSNs := make([]string, 51)
	for i := range orderSNs {
		orderSNs[i] = "order-" + strconv.Itoa(i)
	}
	_, err := client.GetOrderDetail(t.Context(), "access-token", 987654, orderSNs, nil)
	if err == nil {
		t.Fatal("expected batch-size error")
	}
	if hit {
		t.Fatal("HTTP server should not be called when order_sn_list exceeds 50")
	}
}

func TestGetTokenRejectsEmptyTokenResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != PathTokenGet {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"shop_id":   987654,
			"expire_in": 14400,
		})
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, PartnerID: 1233790, PartnerKey: "secret"})
	_, err := client.GetToken(t.Context(), "code-1", 987654)
	if err == nil {
		t.Fatal("expected empty token error")
	}
	if got := err.Error(); got != "shopee token/get: empty token response" {
		t.Fatalf("error = %q", got)
	}
}

func testShopeeSign(key, base string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(base))
	return hex.EncodeToString(mac.Sum(nil))
}
