package tiktokshop

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestOrderClientSearchOrdersSignsAndSendsExactBody(t *testing.T) {
	now := time.Unix(1_725_000_000, 0)
	var receivedBody []byte
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != PathSearchOrders {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("x-tts-access-token") != "seller-access-token" || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("headers = %#v", r.Header)
		}
		receivedBody = readTestBody(t, r)
		queryWithoutSign := cloneValues(r.URL.Query())
		providedSign := queryWithoutSign.Get("sign")
		queryWithoutSign.Del("sign")
		expectedSign, err := SignRequest("app-secret", PathSearchOrders, queryWithoutSign, receivedBody, false)
		if err != nil {
			t.Fatal(err)
		}
		if providedSign != expectedSign {
			t.Fatalf("sign = %q, want %q", providedSign, expectedSign)
		}
		if queryWithoutSign.Get("app_key") != "app-key" || queryWithoutSign.Get("shop_cipher") != "shop-cipher" || queryWithoutSign.Get("page_size") != "50" || queryWithoutSign.Get("sort_field") != "update_time" || queryWithoutSign.Get("sort_order") != "ASC" || queryWithoutSign.Get("timestamp") != "1725000000" {
			t.Fatalf("query = %v", queryWithoutSign)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"Success","request_id":"req-list","data":{"next_page_token":"next-token","total_count":1,"orders":[{"id":"576461413038785752","status":"AWAITING_SHIPMENT","create_time":1724990000,"update_time":1724999000,"recipient_address":{"name":"must not be exposed","phone_number":"secret"},"buyer_email":"secret@example.com","payment":{"currency":"THB","sub_total":"100.00","shipping_fee":"20.00","seller_discount":"5.00","platform_discount":"10.00","payment_platform_discount":"2.00","payment_discount_service_fee":"1.00","total_amount":"105.00","original_total_product_price":"100.00","small_order_fee":"7.49","buyer_service_fee":"3.00","handling_fee":"4.00","shipping_insurance_fee":"5.00","item_insurance_fee":"6.00"},"line_items":[{"id":"line-1","product_id":"product-1","sku_id":"sku-1","seller_sku":"AOY-001","product_name":"Product","sku_name":"Red","currency":"THB","original_price":"100.00","sale_price":"95.00","seller_discount":"5.00","platform_discount":"0.00"}]}]}}`))
	}))
	defer server.Close()

	client := newTestOrderClient(t, server, now)
	result, requestID, err := client.SearchOrders(context.Background(), "seller-access-token", "shop-cipher", SearchOrdersRequest{
		PageSize:  50,
		SortField: OrderSortFieldUpdateTime,
		SortOrder: OrderSortAscending,
		Filters:   OrderSearchFilters{OrderStatus: OrderStatusAwaitingShipment, UpdateTimeGE: 1_724_990_000, UpdateTimeLT: 1_725_000_000},
	})
	if err != nil {
		t.Fatalf("SearchOrders() error = %v", err)
	}
	if requestID != "req-list" || result.NextPageToken != "next-token" || result.TotalCount != 1 || len(result.Orders) != 1 {
		t.Fatalf("SearchOrders() = %+v, requestID=%q", result, requestID)
	}
	order := result.Orders[0]
	if order.ID != "576461413038785752" || order.Payment.TotalAmount != "105.00" || len(order.LineItems) != 1 || order.LineItems[0].SellerSKU != "AOY-001" {
		t.Fatalf("order = %+v", order)
	}
	if order.Payment.SmallOrderFee != "7.49" || order.Payment.BuyerServiceFee != "3.00" || order.Payment.PaymentPlatformDiscount != "2.00" || order.Payment.ShippingInsuranceFee != "5.00" {
		t.Fatalf("payment fee breakdown = %+v", order.Payment)
	}
	encoded, err := json.Marshal(order)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "must not be exposed") || strings.Contains(string(encoded), "secret@example.com") {
		t.Fatalf("safe order leaked buyer PII: %s", encoded)
	}
	if string(receivedBody) != `{"order_status":"AWAITING_SHIPMENT","update_time_ge":1724990000,"update_time_lt":1725000000}` {
		t.Fatalf("body = %s", receivedBody)
	}
}

func TestOrderClientGetOrderDetailsUsesCurrentVersionAndCapsIDs(t *testing.T) {
	now := time.Unix(1_725_000_000, 0)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != PathGetOrderDetails {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		query := r.URL.Query()
		providedSign := query.Get("sign")
		query.Del("sign")
		expectedSign, err := SignRequest("app-secret", PathGetOrderDetails, query, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		if providedSign != expectedSign || query.Get("ids") != "order-1,order-2" || query.Get("shop_cipher") != "shop-cipher" {
			t.Fatalf("query = %v, sign = %q", query, providedSign)
		}
		_, _ = w.Write([]byte(`{"code":0,"message":"Success","request_id":"req-detail","data":{"orders":[{"id":"order-1","status":"COMPLETED"},{"id":"order-2","status":"CANCELLED"}]}}`))
	}))
	defer server.Close()

	client := newTestOrderClient(t, server, now)
	orders, requestID, err := client.GetOrderDetails(context.Background(), "seller-access-token", "shop-cipher", []string{" order-1 ", "order-2"})
	if err != nil || requestID != "req-detail" || len(orders) != 2 || orders[1].Status != OrderStatusCancelled {
		t.Fatalf("GetOrderDetails() = %+v, requestID=%q, err=%v", orders, requestID, err)
	}

	tooMany := make([]string, 51)
	for i := range tooMany {
		tooMany[i] = "order-" + time.Unix(int64(i+1), 0).Format("150405")
	}
	if _, _, err := client.GetOrderDetails(context.Background(), "seller-access-token", "shop-cipher", tooMany); !errors.Is(err, ErrInvalidOrderInput) {
		t.Fatalf("too many IDs error = %v", err)
	}
	if _, _, err := client.GetOrderDetails(context.Background(), "seller-access-token", "shop-cipher", []string{"order-1", "order-1"}); !errors.Is(err, ErrInvalidOrderInput) {
		t.Fatalf("duplicate IDs error = %v", err)
	}
}

func TestOrderClientGetsOnlyValidatedShipmentRecipient(t *testing.T) {
	now := time.Unix(1_725_000_000, 0)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != PathGetOrderDetails || r.URL.Query().Get("ids") != "order-1" {
			t.Fatalf("request = %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"code":0,"message":"Success","request_id":"req-recipient","data":{"orders":[{"id":"order-1","buyer_email":"must-not-cross-gateway@example.com","recipient_address":{"name":"Recipient","phone_number":"0900000000","full_address":"Bangkok 10000"}}]}}`))
	}))
	defer server.Close()

	client := newTestOrderClient(t, server, now)
	recipient, requestID, err := client.GetShipmentRecipient(context.Background(), "seller-access-token", "shop-cipher", "order-1")
	if err != nil || requestID != "req-recipient" || recipient == nil || recipient.OrderID != "order-1" || recipient.Name != "Recipient" || recipient.Address != "Bangkok 10000" || recipient.Telephone != "0900000000" {
		t.Fatalf("GetShipmentRecipient() = %+v, requestID=%q, err=%v", recipient, requestID, err)
	}
	encoded, _ := json.Marshal(recipient)
	if strings.Contains(string(encoded), "buyer_email") || strings.Contains(string(encoded), "must-not-cross") {
		t.Fatalf("recipient response widened PII boundary: %s", encoded)
	}
}

func TestOrderClientRejectsMaskedOrMismatchedShipmentRecipient(t *testing.T) {
	responses := []string{
		`{"code":0,"data":{"orders":[{"id":"other-order","recipient_address":{"name":"Recipient","phone_number":"0900000000","full_address":"Bangkok"}}]}}`,
		`{"code":0,"data":{"orders":[{"id":"order-1","recipient_address":{"name":"Rec***","phone_number":"0900***000","full_address":"Bangkok"}}]}}`,
	}
	for _, responseBody := range responses {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(responseBody)) }))
		client := newTestOrderClient(t, server, time.Unix(1_725_000_000, 0))
		_, _, err := client.GetShipmentRecipient(context.Background(), "seller-access-token", "shop-cipher", "order-1")
		server.Close()
		if !errors.Is(err, ErrInvalidOrderResponse) {
			t.Fatalf("error = %v, want ErrInvalidOrderResponse", err)
		}
	}
}

func TestOrderClientDescribesUnavailableShipmentRecipientWithoutPII(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"request_id":"req-recipient-masked","data":{"orders":[{"id":"order-1","recipient_address":{"name":"Rec***","phone_number":"","full_address":"Bangkok 10***"}}]}}`))
	}))
	defer server.Close()
	client := newTestOrderClient(t, server, time.Unix(1_725_000_000, 0))

	_, requestID, err := client.GetShipmentRecipient(context.Background(), "seller-access-token", "shop-cipher", "order-1")
	var unavailable *ShipmentRecipientUnavailableError
	if requestID != "req-recipient-masked" || !errors.As(err, &unavailable) || !errors.Is(err, ErrInvalidOrderResponse) {
		t.Fatalf("requestID=%q error=%v unavailable=%+v", requestID, err, unavailable)
	}
	if unavailable.UpstreamRequestID != "req-recipient-masked" || unavailable.Name != RecipientFieldMasked || unavailable.Address != RecipientFieldMasked || unavailable.Telephone != RecipientFieldMissing {
		t.Fatalf("unavailable = %+v", unavailable)
	}
	if strings.Contains(err.Error(), "Rec") || strings.Contains(err.Error(), "Bangkok") {
		t.Fatalf("error leaked recipient PII: %v", err)
	}
}

func TestOrderClientGetsPriceDetailForOneOrder(t *testing.T) {
	now := time.Unix(1_725_000_000, 0)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/order/202407/orders/order-1/price_detail" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		query := r.URL.Query()
		providedSign := query.Get("sign")
		query.Del("sign")
		expectedSign, err := SignRequest("app-secret", r.URL.Path, query, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		if providedSign != expectedSign || query.Get("shop_cipher") != "shop-cipher" {
			t.Fatalf("query = %v, sign = %q", query, providedSign)
		}
		_, _ = w.Write([]byte(`{"code":0,"message":"Success","request_id":"req-price","data":{"currency":"THB","total":"329.00","payment":"307.49","sku_list_price":"300.00","sku_sale_price":"300.00","subtotal":"300.00","shipping_list_price":"29.00","shipping_sale_price":"0.00","tax_amount":"0.00","small_order_fee":"7.49","line_items":[{"currency":"THB","total":"300.00","payment":"300.00","sku_list_price":"300.00","sku_sale_price":"300.00","subtotal":"300.00"}]}}`))
	}))
	defer server.Close()

	client := newTestOrderClient(t, server, now)
	detail, requestID, err := client.GetPriceDetail(context.Background(), "seller-access-token", "shop-cipher", " order-1 ")
	if err != nil {
		t.Fatalf("GetPriceDetail() error = %v", err)
	}
	if requestID != "req-price" || detail.Currency != "THB" || detail.Payment != "307.49" || detail.SKUSalePrice != "300.00" || detail.SmallOrderFee != "7.49" || len(detail.LineItems) != 1 {
		t.Fatalf("GetPriceDetail() = %+v, requestID=%q", detail, requestID)
	}
	if _, _, err := client.GetPriceDetail(context.Background(), "seller-access-token", "shop-cipher", " "); !errors.Is(err, ErrInvalidOrderInput) {
		t.Fatalf("empty order ID error = %v", err)
	}
}

func TestOrderClientSearchOrdersRejectsInvalidFiltersBeforeNetwork(t *testing.T) {
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	client := newTestOrderClient(t, server, time.Now())

	cases := []SearchOrdersRequest{
		{PageSize: 0},
		{PageSize: 101},
		{PageSize: 20, SortField: "invalid"},
		{PageSize: 20, SortOrder: "invalid"},
		{PageSize: 20, Filters: OrderSearchFilters{OrderStatus: "INVALID"}},
		{PageSize: 20, Filters: OrderSearchFilters{ShippingType: "INVALID"}},
		{PageSize: 20, Filters: OrderSearchFilters{WarehouseIDs: make([]string, 101)}},
	}
	for _, input := range cases {
		if _, _, err := client.SearchOrders(context.Background(), "token", "cipher", input); !errors.Is(err, ErrInvalidOrderInput) {
			t.Fatalf("SearchOrders(%+v) error = %v", input, err)
		}
	}
	if calls != 0 {
		t.Fatalf("network calls = %d", calls)
	}
}

func TestOrderClientReturnsSanitizedAPIError(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":10002014,"message":"buyer secret leaked upstream","request_id":"req-error"}`))
	}))
	defer server.Close()
	client := newTestOrderClient(t, server, time.Now())

	_, _, err := client.GetOrderDetails(context.Background(), "seller-access-token", "shop-cipher", []string{"order-1"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 10002014 || apiErr.RequestID != "req-error" || strings.Contains(err.Error(), "buyer secret") {
		t.Fatalf("error = %#v", err)
	}
}

func TestValidateOrdersRejectsMissingStatus(t *testing.T) {
	err := validateOrders([]Order{{ID: "order-1"}}, 50, map[string]struct{}{"order-1": {}})
	if !errors.Is(err, ErrInvalidOrderResponse) {
		t.Fatalf("validateOrders() error = %v, want ErrInvalidOrderResponse", err)
	}
}

func newTestOrderClient(t *testing.T, server *httptest.Server, now time.Time) *OrderClient {
	t.Helper()
	client, err := NewOrderClient(OrderClientConfig{
		BaseURL: server.URL, AppKey: "app-key", AppSecret: "app-secret", HTTPClient: server.Client(), Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewOrderClient() error = %v", err)
	}
	return client
}

func cloneValues(input url.Values) url.Values {
	output := make(url.Values, len(input))
	for key, values := range input {
		output[key] = append([]string(nil), values...)
	}
	return output
}

func readTestBody(t *testing.T, r *http.Request) []byte {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
