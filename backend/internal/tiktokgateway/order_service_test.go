package tiktokgateway

import (
	"context"
	"errors"
	"testing"

	"nexflow/internal/services/tiktokshop"
)

func TestOrderServiceSearchesWithTenantScopedCredential(t *testing.T) {
	credentials := &fakeOrderCredentialProvider{credential: &AccessCredential{AccessToken: "access-secret", ShopID: "shop-1", ShopCipher: "cipher-1"}}
	reader := &fakeTikTokOrderReader{searchResult: &tiktokshop.SearchOrdersResult{
		NextPageToken: "next", TotalCount: 1, Orders: []tiktokshop.Order{{ID: "order-1", Status: tiktokshop.OrderStatusAwaitingShipment}},
	}, requestID: "tts-request-1"}
	service, err := NewOrderService(credentials, reader)
	if err != nil {
		t.Fatal(err)
	}
	input := tiktokshop.SearchOrdersRequest{PageSize: 20, SortField: tiktokshop.OrderSortFieldUpdateTime}
	result, err := service.SearchOrders(context.Background(), "AOY", "shop-1", input)
	if err != nil {
		t.Fatalf("SearchOrders() error = %v", err)
	}
	if result.UpstreamRequestID != "tts-request-1" || result.TotalCount != 1 || len(result.Orders) != 1 {
		t.Fatalf("result = %+v", result)
	}
	if credentials.tenant != "aoy" || credentials.shopID != "shop-1" || reader.accessToken != "access-secret" || reader.shopCipher != "cipher-1" || reader.searchInput.PageSize != 20 {
		t.Fatalf("credentials=%+v reader=%+v", credentials, reader)
	}
}

func TestOrderServiceGetsCurrentOrderDetails(t *testing.T) {
	credentials := &fakeOrderCredentialProvider{credential: &AccessCredential{AccessToken: "access-secret", ShopID: "shop-1", ShopCipher: "cipher-1"}}
	reader := &fakeTikTokOrderReader{details: []tiktokshop.Order{{ID: "order-1", Status: tiktokshop.OrderStatusCompleted}}, requestID: "tts-request-2"}
	service, err := NewOrderService(credentials, reader)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.GetOrderDetails(context.Background(), "aoy", "shop-1", []string{"order-1"})
	if err != nil || result.UpstreamRequestID != "tts-request-2" || len(result.Orders) != 1 || result.Orders[0].Status != tiktokshop.OrderStatusCompleted {
		t.Fatalf("GetOrderDetails() = %+v, %v", result, err)
	}
}

func TestOrderServiceGetsSingleShipmentRecipientWithTenantScopedCredential(t *testing.T) {
	credentials := &fakeOrderCredentialProvider{credential: &AccessCredential{AccessToken: "access-secret", ShopID: "shop-1", ShopCipher: "cipher-1"}}
	reader := &fakeTikTokOrderReader{
		recipient: &tiktokshop.ShipmentRecipient{OrderID: "order-1", Name: "Recipient", Address: "Bangkok", Telephone: "0900000000"},
		requestID: "tts-request-recipient",
	}
	service, err := NewOrderService(credentials, reader)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.GetShipmentRecipient(context.Background(), "AOY", "shop-1", "order-1")
	if err != nil || result.UpstreamRequestID != "tts-request-recipient" || result.Recipient == nil || result.Recipient.OrderID != "order-1" {
		t.Fatalf("GetShipmentRecipient() = %+v, %v", result, err)
	}
	if credentials.tenant != "aoy" || reader.accessToken != "access-secret" || reader.shopCipher != "cipher-1" || reader.priceOrderID != "order-1" {
		t.Fatalf("credentials=%+v reader=%+v", credentials, reader)
	}
}

func TestOrderServicePreservesShipmentRecipientDiagnostics(t *testing.T) {
	credentials := &fakeOrderCredentialProvider{credential: &AccessCredential{AccessToken: "access-secret", ShopID: "shop-1", ShopCipher: "cipher-1"}}
	reader := &fakeTikTokOrderReader{
		requestID: "tts-request-masked",
		err: &tiktokshop.ShipmentRecipientUnavailableError{
			UpstreamRequestID: "tts-request-masked",
			Name:              tiktokshop.RecipientFieldPresent,
			Address:           tiktokshop.RecipientFieldMasked,
			Telephone:         tiktokshop.RecipientFieldMissing,
		},
	}
	service, err := NewOrderService(credentials, reader)
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.GetShipmentRecipient(context.Background(), "aoy", "shop-1", "order-1")
	var unavailable *tiktokshop.ShipmentRecipientUnavailableError
	if !errors.As(err, &unavailable) || unavailable.UpstreamRequestID != "tts-request-masked" || unavailable.Address != tiktokshop.RecipientFieldMasked {
		t.Fatalf("error = %v unavailable = %+v", err, unavailable)
	}
}

func TestOrderServiceGetsPriceDetailWithTenantScopedCredential(t *testing.T) {
	credentials := &fakeOrderCredentialProvider{credential: &AccessCredential{AccessToken: "access-secret", ShopID: "shop-1", ShopCipher: "cipher-1"}}
	reader := &fakeTikTokOrderReader{priceDetail: &tiktokshop.PriceDetail{Currency: "THB", Payment: "307.49"}, requestID: "tts-request-price"}
	service, err := NewOrderService(credentials, reader)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.GetPriceDetail(context.Background(), "AOY", "shop-1", "order-1")
	if err != nil || result.UpstreamRequestID != "tts-request-price" || result.PriceDetail == nil || result.PriceDetail.Payment != "307.49" {
		t.Fatalf("GetPriceDetail() = %+v, %v", result, err)
	}
	if credentials.tenant != "aoy" || reader.accessToken != "access-secret" || reader.shopCipher != "cipher-1" || reader.priceOrderID != "order-1" {
		t.Fatalf("credentials=%+v reader=%+v", credentials, reader)
	}
}

func TestOrderServiceStopsBeforeTikTokWhenCredentialFails(t *testing.T) {
	credentials := &fakeOrderCredentialProvider{err: ErrRefreshTokenExpired}
	reader := &fakeTikTokOrderReader{}
	service, err := NewOrderService(credentials, reader)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.SearchOrders(context.Background(), "aoy", "shop-1", tiktokshop.SearchOrdersRequest{PageSize: 20})
	if !errors.Is(err, ErrRefreshTokenExpired) || reader.searchCalls != 0 {
		t.Fatalf("error=%v calls=%d", err, reader.searchCalls)
	}
}

type fakeOrderCredentialProvider struct {
	credential     *AccessCredential
	err            error
	tenant, shopID string
}

func (f *fakeOrderCredentialProvider) AccessCredential(_ context.Context, tenant, shopID string) (*AccessCredential, error) {
	f.tenant, f.shopID = tenant, shopID
	return f.credential, f.err
}

type fakeTikTokOrderReader struct {
	searchResult *tiktokshop.SearchOrdersResult
	details      []tiktokshop.Order
	priceDetail  *tiktokshop.PriceDetail
	recipient    *tiktokshop.ShipmentRecipient
	requestID    string
	err          error
	accessToken  string
	shopCipher   string
	priceOrderID string
	searchInput  tiktokshop.SearchOrdersRequest
	searchCalls  int
}

func (f *fakeTikTokOrderReader) SearchOrders(_ context.Context, accessToken, shopCipher string, input tiktokshop.SearchOrdersRequest) (*tiktokshop.SearchOrdersResult, string, error) {
	f.searchCalls++
	f.accessToken, f.shopCipher, f.searchInput = accessToken, shopCipher, input
	return f.searchResult, f.requestID, f.err
}

func (f *fakeTikTokOrderReader) GetOrderDetails(_ context.Context, accessToken, shopCipher string, orderIDs []string) ([]tiktokshop.Order, string, error) {
	f.accessToken, f.shopCipher = accessToken, shopCipher
	return append([]tiktokshop.Order(nil), f.details...), f.requestID, f.err
}

func (f *fakeTikTokOrderReader) GetShipmentRecipient(_ context.Context, accessToken, shopCipher, orderID string) (*tiktokshop.ShipmentRecipient, string, error) {
	f.accessToken, f.shopCipher, f.priceOrderID = accessToken, shopCipher, orderID
	return f.recipient, f.requestID, f.err
}

func (f *fakeTikTokOrderReader) GetPriceDetail(_ context.Context, accessToken, shopCipher, orderID string) (*tiktokshop.PriceDetail, string, error) {
	f.accessToken, f.shopCipher, f.priceOrderID = accessToken, shopCipher, orderID
	return f.priceDetail, f.requestID, f.err
}
