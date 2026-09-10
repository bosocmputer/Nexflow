package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"nexflow/internal/config"
	"nexflow/internal/services/tiktokshop"
)

type tenantTikTokGatewayFake struct {
	configured  bool
	auth        *tiktokshop.GatewayAuthURLResponse
	connections []tiktokshop.GatewayConnection
	err         error
	authInput   tiktokshop.GatewayAuthURLRequest
	listCalls   int
	search      *tiktokshop.GatewayOrderSearchResponse
	details     *tiktokshop.GatewayOrderDetailsResponse
	searchInput tiktokshop.GatewayOrderSearchRequest
	detailInput tiktokshop.GatewayOrderDetailsRequest
}

func (f *tenantTikTokGatewayFake) Configured() bool { return f.configured }
func (f *tenantTikTokGatewayFake) CreateAuthURL(_ context.Context, input tiktokshop.GatewayAuthURLRequest) (*tiktokshop.GatewayAuthURLResponse, error) {
	f.authInput = input
	return f.auth, f.err
}
func (f *tenantTikTokGatewayFake) ListConnections(context.Context) ([]tiktokshop.GatewayConnection, error) {
	f.listCalls++
	return append([]tiktokshop.GatewayConnection(nil), f.connections...), f.err
}
func (f *tenantTikTokGatewayFake) SearchOrders(_ context.Context, input tiktokshop.GatewayOrderSearchRequest) (*tiktokshop.GatewayOrderSearchResponse, error) {
	f.searchInput = input
	return f.search, f.err
}
func (f *tenantTikTokGatewayFake) GetOrderDetails(_ context.Context, input tiktokshop.GatewayOrderDetailsRequest) (*tiktokshop.GatewayOrderDetailsResponse, error) {
	f.detailInput = input
	return f.details, f.err
}

type tenantTikTokStoreFake struct {
	connections []tiktokshop.GatewayConnection
	err         error
}

func (f *tenantTikTokStoreFake) Sync(_ context.Context, connections []tiktokshop.GatewayConnection) error {
	f.connections = append([]tiktokshop.GatewayConnection(nil), connections...)
	return f.err
}

func TestTikTokShopAPIHandlerCreatesGatewayAuthorizationURLForCurrentAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gateway := &tenantTikTokGatewayFake{configured: true, auth: &tiktokshop.GatewayAuthURLResponse{
		AuthURL: "https://services.tiktokshop.com/open/authorize?state=signed", RedirectURL: "https://gateway.example/api/tiktok-shop/callback",
	}}
	handler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: true, PublicBaseURL: "https://nexflow-aoy.nextstep-soft.com/"}, gateway, &tenantTikTokStoreFake{}, nil)
	router := gin.New()
	router.POST("/auth-url", func(c *gin.Context) { c.Set("user_id", "admin-1"); handler.CreateAuthURL(c) })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/auth-url", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "services.tiktokshop.com") {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if gateway.authInput.UserID != "admin-1" || gateway.authInput.ReturnURL != "https://nexflow-aoy.nextstep-soft.com/settings/tiktok-shop?connected=1" {
		t.Fatalf("gateway auth input = %+v", gateway.authInput)
	}
}

func TestTikTokShopAPIHandlerListsAndPersistsEveryGatewayConnection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gateway := &tenantTikTokGatewayFake{configured: true, connections: []tiktokshop.GatewayConnection{
		{GatewayConnectionID: "11111111-1111-4111-8111-111111111111", ShopID: "7000714532876273420", ShopName: "AOY Main"},
		{GatewayConnectionID: "22222222-2222-4222-8222-222222222222", ShopID: "7000714532876273421", ShopName: "AOY Outlet"},
	}}
	store := &tenantTikTokStoreFake{}
	handler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: true}, gateway, store, nil)
	router := gin.New()
	router.GET("/connections", handler.ListConnections)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/connections", nil))
	if response.Code != http.StatusOK || len(store.connections) != 2 || !strings.Contains(response.Body.String(), "AOY Outlet") {
		t.Fatalf("status = %d, stored = %+v, body = %s", response.Code, store.connections, response.Body.String())
	}
}

func TestTikTokShopAPIHandlerFailsClosedWhenTenantFeatureIsDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gateway := &tenantTikTokGatewayFake{configured: true}
	handler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: false}, gateway, &tenantTikTokStoreFake{}, nil)
	router := gin.New()
	router.GET("/connections", handler.ListConnections)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/connections", nil))
	if response.Code != http.StatusNotFound || gateway.listCalls != 0 {
		t.Fatalf("status = %d, calls = %d, body = %s", response.Code, gateway.listCalls, response.Body.String())
	}
}

func TestTikTokShopAPIHandlerSearchesOrdersReadOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gateway := &tenantTikTokGatewayFake{configured: true, search: &tiktokshop.GatewayOrderSearchResponse{
		UpstreamRequestID: "tts-list", TotalCount: 1,
		Orders: []tiktokshop.Order{{ID: "order-1", Status: tiktokshop.OrderStatusAwaitingShipment}},
	}}
	handler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: true}, gateway, &tenantTikTokStoreFake{}, nil)
	router := gin.New()
	router.POST("/orders/search", handler.SearchOrders)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/orders/search", strings.NewReader(`{"shop_id":"shop-1","search":{"page_size":20,"sort_field":"update_time","filters":{"update_time_ge":1724990000}}}`)))

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"id":"order-1"`) || gateway.searchInput.ShopID != "shop-1" || gateway.searchInput.Search.PageSize != 20 {
		t.Fatalf("status=%d body=%s input=%+v", response.Code, response.Body.String(), gateway.searchInput)
	}
}

func TestTikTokShopAPIHandlerGetsOrderDetailsReadOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gateway := &tenantTikTokGatewayFake{configured: true, details: &tiktokshop.GatewayOrderDetailsResponse{
		UpstreamRequestID: "tts-detail", Orders: []tiktokshop.Order{{ID: "order-1", Status: tiktokshop.OrderStatusCompleted}},
	}}
	handler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: true}, gateway, &tenantTikTokStoreFake{}, nil)
	router := gin.New()
	router.POST("/orders/detail", handler.GetOrderDetails)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/orders/detail", strings.NewReader(`{"shop_id":"shop-1","order_ids":["order-1"]}`)))

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"COMPLETED"`) || len(gateway.detailInput.OrderIDs) != 1 {
		t.Fatalf("status=%d body=%s input=%+v", response.Code, response.Body.String(), gateway.detailInput)
	}
}

func TestTikTokShopAPIHandlerBlocksOrderReadsWhenFeatureDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gateway := &tenantTikTokGatewayFake{configured: true}
	handler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: false}, gateway, &tenantTikTokStoreFake{}, nil)
	router := gin.New()
	router.POST("/orders/search", handler.SearchOrders)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/orders/search", strings.NewReader(`{"shop_id":"shop-1","search":{"page_size":20,"filters":{}}}`)))

	if response.Code != http.StatusNotFound || gateway.searchInput.ShopID != "" {
		t.Fatalf("status=%d body=%s input=%+v", response.Code, response.Body.String(), gateway.searchInput)
	}
}
