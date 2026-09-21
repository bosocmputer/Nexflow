package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"nexflow/internal/config"
	"nexflow/internal/models"
	"nexflow/internal/repository"
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
	price       *tiktokshop.GatewayOrderPriceDetailResponse
	searchInput tiktokshop.GatewayOrderSearchRequest
	detailInput tiktokshop.GatewayOrderDetailsRequest
	priceInput  tiktokshop.GatewayOrderPriceDetailRequest
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
func (f *tenantTikTokGatewayFake) GetPriceDetail(_ context.Context, input tiktokshop.GatewayOrderPriceDetailRequest) (*tiktokshop.GatewayOrderPriceDetailResponse, error) {
	f.priceInput = input
	return f.price, f.err
}

type tenantTikTokStoreFake struct {
	connections []tiktokshop.GatewayConnection
	err         error
}

type tenantTikTokSnapshotterFake struct {
	input  tiktokshop.TikTokOrderSnapshotRequest
	result *tiktokshop.TikTokOrderSnapshotResult
	err    error
	calls  int
}

type tenantTikTokReconcilerFake struct {
	input  tiktokshop.TikTokOrderReconcileRequest
	result *tiktokshop.TikTokOrderReconcileResult
	err    error
	calls  int
}

type tenantTikTokOrderSyncSettingsFake struct {
	settings    []tiktokshop.TikTokOrderSyncSetting
	updateInput tiktokshop.TikTokOrderSyncSettingUpdate
	updateShop  string
	err         error
}

type tenantTikTokOrderReaderFake struct {
	filter tiktokshop.TikTokOrderSnapshotListFilter
	result *tiktokshop.TikTokOrderSnapshotListResult
	err    error
	calls  int
}

type tenantTikTokBillShadowPreviewerFake struct {
	shopID  string
	orderID string
	result  *tiktokshop.TikTokBillShadowPreview
	results map[string]*tiktokshop.TikTokBillShadowPreview
	err     error
	calls   int
}

type tenantTikTokBillShadowMapperFake struct {
	previewInput  tiktokshop.TikTokBillShadowMappingSelection
	confirmInput  tiktokshop.TikTokBillShadowMappingConfirmation
	actorID       string
	previewResult models.MarketplaceAliasImpact
	confirmResult *repository.MarketplaceAliasCommitResult
	err           error
	previewCalls  int
	confirmCalls  int
}

type tenantTikTokReviewedBillCreatorFake struct {
	input  tiktokshop.TikTokReviewedBillInput
	result *tiktokshop.TikTokReviewedBillResult
	err    error
	calls  int
}

type tenantTikTokAutoSMLSettingsFake struct {
	settings []models.TikTokAutoSMLSetting
	updated  repository.TikTokAutoSMLSettingUpdate
	err      error
}

func (f *tenantTikTokAutoSMLSettingsFake) ListSettings(context.Context) ([]models.TikTokAutoSMLSetting, error) {
	return append([]models.TikTokAutoSMLSetting(nil), f.settings...), f.err
}

func (f *tenantTikTokAutoSMLSettingsFake) GetSetting(_ context.Context, shopID string) (*models.TikTokAutoSMLSetting, error) {
	for i := range f.settings {
		if f.settings[i].ShopID == shopID {
			value := f.settings[i]
			return &value, f.err
		}
	}
	return nil, sql.ErrNoRows
}

func (f *tenantTikTokAutoSMLSettingsFake) UpdateSetting(_ context.Context, input repository.TikTokAutoSMLSettingUpdate) (*models.TikTokAutoSMLSetting, error) {
	f.updated = input
	if f.err != nil {
		return nil, f.err
	}
	return &models.TikTokAutoSMLSetting{ShopID: input.ShopID, Enabled: input.Enabled, ConfigVersion: input.ExpectedConfigVersion + 1}, nil
}

func (f *tenantTikTokAutoSMLSettingsFake) RetryJob(context.Context, string, string, string, string) error {
	return f.err
}

func (f *tenantTikTokReviewedBillCreatorFake) Create(_ context.Context, input tiktokshop.TikTokReviewedBillInput) (*tiktokshop.TikTokReviewedBillResult, error) {
	f.calls++
	f.input = input
	return f.result, f.err
}

func (f *tenantTikTokBillShadowMapperFake) Preview(_ context.Context, input tiktokshop.TikTokBillShadowMappingSelection) (models.MarketplaceAliasImpact, error) {
	f.previewCalls++
	f.previewInput = input
	return f.previewResult, f.err
}

func (f *tenantTikTokBillShadowMapperFake) Confirm(_ context.Context, input tiktokshop.TikTokBillShadowMappingConfirmation, actorID string) (*repository.MarketplaceAliasCommitResult, error) {
	f.confirmCalls++
	f.confirmInput = input
	f.actorID = actorID
	return f.confirmResult, f.err
}

func (f *tenantTikTokBillShadowPreviewerFake) Preview(_ context.Context, shopID, orderID string) (*tiktokshop.TikTokBillShadowPreview, error) {
	f.calls++
	f.shopID = shopID
	f.orderID = orderID
	if f.results != nil {
		if result, ok := f.results[orderID]; ok {
			return result, f.err
		}
	}
	return f.result, f.err
}

func (f *tenantTikTokOrderReaderFake) List(_ context.Context, filter tiktokshop.TikTokOrderSnapshotListFilter) (*tiktokshop.TikTokOrderSnapshotListResult, error) {
	f.calls++
	f.filter = filter
	return f.result, f.err
}

func (f *tenantTikTokOrderSyncSettingsFake) ListSettings(context.Context) ([]tiktokshop.TikTokOrderSyncSetting, error) {
	return append([]tiktokshop.TikTokOrderSyncSetting(nil), f.settings...), f.err
}

func (f *tenantTikTokOrderSyncSettingsFake) UpdateSetting(_ context.Context, shopID string, input tiktokshop.TikTokOrderSyncSettingUpdate) (*tiktokshop.TikTokOrderSyncSetting, error) {
	f.updateShop = shopID
	f.updateInput = input
	if f.err != nil {
		return nil, f.err
	}
	return &tiktokshop.TikTokOrderSyncSetting{ShopID: shopID, Enabled: input.Enabled, IntervalSeconds: input.IntervalSeconds, OverlapSeconds: input.OverlapSeconds, ConfigVersion: input.ConfigVersion + 1}, nil
}

func (f *tenantTikTokReconcilerFake) Reconcile(_ context.Context, input tiktokshop.TikTokOrderReconcileRequest) (*tiktokshop.TikTokOrderReconcileResult, error) {
	f.calls++
	f.input = input
	return f.result, f.err
}

func (f *tenantTikTokSnapshotterFake) Sync(_ context.Context, input tiktokshop.TikTokOrderSnapshotRequest) (*tiktokshop.TikTokOrderSnapshotResult, error) {
	f.calls++
	f.input = input
	return f.result, f.err
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
	handler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: true, PublicBaseURL: "https://nexflow-aoy.nextstep-soft.com/"}, gateway, &tenantTikTokStoreFake{}, nil, nil)
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
	handler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: true}, gateway, store, nil, nil)
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
	handler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: false}, gateway, &tenantTikTokStoreFake{}, nil, nil)
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
	handler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: true}, gateway, &tenantTikTokStoreFake{}, nil, nil)
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
	handler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: true}, gateway, &tenantTikTokStoreFake{}, nil, nil)
	router := gin.New()
	router.POST("/orders/detail", handler.GetOrderDetails)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/orders/detail", strings.NewReader(`{"shop_id":"shop-1","order_ids":["order-1"]}`)))

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"COMPLETED"`) || len(gateway.detailInput.OrderIDs) != 1 {
		t.Fatalf("status=%d body=%s input=%+v", response.Code, response.Body.String(), gateway.detailInput)
	}
}

func TestTikTokShopAPIHandlerGetsOrderPriceDetailReadOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gateway := &tenantTikTokGatewayFake{configured: true, price: &tiktokshop.GatewayOrderPriceDetailResponse{
		UpstreamRequestID: "tts-price", PriceDetail: &tiktokshop.PriceDetail{Currency: "THB", Payment: "307.49"},
	}}
	handler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: true}, gateway, &tenantTikTokStoreFake{}, nil, nil)
	router := gin.New()
	router.POST("/orders/price-detail", handler.GetOrderPriceDetail)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/orders/price-detail", strings.NewReader(`{"shop_id":"shop-1","order_id":"order-1"}`)))

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"payment":"307.49"`) || gateway.priceInput.OrderID != "order-1" {
		t.Fatalf("status=%d body=%s input=%+v", response.Code, response.Body.String(), gateway.priceInput)
	}
}

func TestTikTokShopAPIHandlerBlocksOrderReadsWhenFeatureDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gateway := &tenantTikTokGatewayFake{configured: true}
	handler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: false}, gateway, &tenantTikTokStoreFake{}, nil, nil)
	router := gin.New()
	router.POST("/orders/search", handler.SearchOrders)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/orders/search", strings.NewReader(`{"shop_id":"shop-1","search":{"page_size":20,"filters":{}}}`)))

	if response.Code != http.StatusNotFound || gateway.searchInput.ShopID != "" {
		t.Fatalf("status=%d body=%s input=%+v", response.Code, response.Body.String(), gateway.searchInput)
	}
}

func TestTikTokShopAPIHandlerPersistsBoundedReadOnlySnapshots(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gateway := &tenantTikTokGatewayFake{configured: true}
	snapshotter := &tenantTikTokSnapshotterFake{result: &tiktokshop.TikTokOrderSnapshotResult{
		ShopID: "7000714532876273420", SyncedCount: 1,
		Snapshots: []tiktokshop.TikTokOrderSnapshotSummary{{OrderID: "585684843131602849", OrderStatus: tiktokshop.OrderStatusCompleted, ItemCount: 2, SKUCount: 1}},
	}}
	handler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: true}, gateway, &tenantTikTokStoreFake{}, snapshotter, nil)
	router := gin.New()
	router.POST("/orders/snapshot", handler.SnapshotOrders)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/orders/snapshot", strings.NewReader(`{"shop_id":"7000714532876273420","order_ids":["585684843131602849"]}`)))

	if response.Code != http.StatusOK || snapshotter.calls != 1 || snapshotter.input.ShopID != "7000714532876273420" || !strings.Contains(response.Body.String(), `"synced_count":1`) {
		t.Fatalf("status=%d calls=%d input=%+v body=%s", response.Code, snapshotter.calls, snapshotter.input, response.Body.String())
	}
}

func TestTikTokShopAPIHandlerRejectsSnapshotBatchOverTwentyBeforeService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gateway := &tenantTikTokGatewayFake{configured: true}
	snapshotter := &tenantTikTokSnapshotterFake{}
	handler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: true}, gateway, &tenantTikTokStoreFake{}, snapshotter, nil)
	router := gin.New()
	router.POST("/orders/snapshot", handler.SnapshotOrders)
	response := httptest.NewRecorder()
	body := `{"shop_id":"7000714532876273420","order_ids":["1","2","3","4","5","6","7","8","9","10","11","12","13","14","15","16","17","18","19","20","21"]}`
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/orders/snapshot", strings.NewReader(body)))

	if response.Code != http.StatusBadRequest || snapshotter.calls != 0 {
		t.Fatalf("status=%d calls=%d body=%s", response.Code, snapshotter.calls, response.Body.String())
	}
}

func TestTikTokShopAPIHandlerRunsBoundedOrderReconciliation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gateway := &tenantTikTokGatewayFake{configured: true}
	reconciler := &tenantTikTokReconcilerFake{result: &tiktokshop.TikTokOrderReconcileResult{
		RunID: "run-1", ShopID: "7494619203789490654", PageCount: 2, DiscoveredCount: 3, SnapshottedCount: 3,
	}}
	handler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: true}, gateway, &tenantTikTokStoreFake{}, nil, nil).
		WithOrderReconciler(reconciler)
	router := gin.New()
	router.POST("/orders/reconcile", handler.ReconcileOrders)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/orders/reconcile", strings.NewReader(
		`{"shop_id":"7494619203789490654","update_time_ge":1789000000,"update_time_lt":1789000100}`,
	)))

	if response.Code != http.StatusOK || reconciler.calls != 1 || reconciler.input.UpdateTimeGE != 1_789_000_000 ||
		!strings.Contains(response.Body.String(), `"snapshotted_count":3`) {
		t.Fatalf("status=%d calls=%d input=%+v body=%s", response.Code, reconciler.calls, reconciler.input, response.Body.String())
	}
}

func TestTikTokShopAPIHandlerRejectsUnboundedOrderReconciliation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gateway := &tenantTikTokGatewayFake{configured: true}
	reconciler := &tenantTikTokReconcilerFake{}
	handler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: true}, gateway, &tenantTikTokStoreFake{}, nil, nil).
		WithOrderReconciler(reconciler)
	router := gin.New()
	router.POST("/orders/reconcile", handler.ReconcileOrders)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/orders/reconcile", strings.NewReader(
		`{"shop_id":"7494619203789490654","update_time_ge":1789000000,"update_time_lt":1790000000}`,
	)))

	if response.Code != http.StatusBadRequest || reconciler.calls != 0 {
		t.Fatalf("status=%d calls=%d body=%s", response.Code, reconciler.calls, response.Body.String())
	}
}

func TestTikTokShopAPIHandlerListsPerShopOrderSyncSettings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settings := &tenantTikTokOrderSyncSettingsFake{settings: []tiktokshop.TikTokOrderSyncSetting{{ShopID: "7494619203789490654", ShopName: "AOY", ConfigVersion: 1}}}
	handler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: true, TikTokShopOrderSyncEnabled: false}, &tenantTikTokGatewayFake{configured: true}, &tenantTikTokStoreFake{}, nil, nil).
		WithOrderSyncSettings(settings)
	router := gin.New()
	router.GET("/order-sync-settings", handler.ListOrderSyncSettings)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/order-sync-settings", nil))

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"worker_enabled":false`) || !strings.Contains(response.Body.String(), `"shop_name":"AOY"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestTikTokShopAPIHandlerListsLocalOrderSnapshotsWithBoundedFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reader := &tenantTikTokOrderReaderFake{result: &tiktokshop.TikTokOrderSnapshotListResult{
		Data: []tiktokshop.TikTokOrderSnapshotListItem{{OrderID: "585684843131602849", ShopID: "7494619203789490654", ShopName: "henna_milkford"}},
		Page: 1, PageSize: 20, TotalItems: 1, TotalPages: 1,
		StatusCounts: tiktokshop.TikTokOrderSnapshotStatusCounts{Total: 4, Completed: 1},
	}}
	handler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: true}, &tenantTikTokGatewayFake{configured: true}, &tenantTikTokStoreFake{}, nil, nil).
		WithOrderReader(reader)
	router := gin.New()
	router.GET("/orders", handler.ListOrders)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/orders?shop_id=7494619203789490654&status_group=completed&order_id=5856&page=1&page_size=20", nil))

	if response.Code != http.StatusOK || reader.calls != 1 || reader.filter.OrderIDPrefix != "5856" || reader.filter.Status != "" || reader.filter.StatusGroup != tiktokshop.TikTokOrderStatusGroupCompleted {
		t.Fatalf("status=%d calls=%d filter=%+v body=%s", response.Code, reader.calls, reader.filter, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"status_counts":{"total":4`) {
		t.Fatalf("response missing status counts: %s", response.Body.String())
	}
	for _, forbidden := range []string{"safe_order", "request_id", "source_hash", "buyer"} {
		if strings.Contains(strings.ToLower(response.Body.String()), forbidden) {
			t.Fatalf("response leaked %q: %s", forbidden, response.Body.String())
		}
	}
}

func TestTikTokShopAPIHandlerReturnsPIISafeBillShadowPreview(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previewer := &tenantTikTokBillShadowPreviewerFake{result: &tiktokshop.TikTokBillShadowPreview{
		ShadowMode: true, CanCreateBill: false, ReadyForReviewedBill: false,
		ShopID: "7494619203789490654", OrderID: "586030483469993439", Currency: "THB",
		Blockers: []tiktokshop.TikTokBillShadowBlocker{{Code: tiktokshop.TikTokBillShadowBlockerMappingMissing, Message: "ยังไม่ได้จับคู่สินค้า"}},
	}}
	handler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: true}, &tenantTikTokGatewayFake{configured: true}, &tenantTikTokStoreFake{}, nil, nil).
		WithBillShadowPreviewer(previewer)
	router := gin.New()
	router.GET("/orders/:shop_id/:order_id/bill-shadow-preview", handler.GetBillShadowPreview)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/orders/7494619203789490654/586030483469993439/bill-shadow-preview", nil))

	if response.Code != http.StatusOK || previewer.calls != 1 || previewer.shopID != "7494619203789490654" ||
		previewer.orderID != "586030483469993439" || !strings.Contains(response.Body.String(), `"shadow_mode":true`) ||
		!strings.Contains(response.Body.String(), `"can_create_bill":false`) {
		t.Fatalf("status=%d calls=%d body=%s", response.Code, previewer.calls, response.Body.String())
	}
	for _, forbidden := range []string{"safe_order", "line_ids", "request_id", "source_hash", "token", "signature"} {
		if strings.Contains(strings.ToLower(response.Body.String()), forbidden) {
			t.Fatalf("response leaked %q: %s", forbidden, response.Body.String())
		}
	}
}

func TestTikTokShopAPIHandlerEnablesReviewedBillOnlyWhenReadyAndFeatureEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previewer := &tenantTikTokBillShadowPreviewerFake{result: &tiktokshop.TikTokBillShadowPreview{
		ShadowMode: true, ReadyForReviewedBill: true, ReviewDigest: strings.Repeat("a", 64),
		ShopID: "7494619203789490654", OrderID: "586030483469993439", Currency: "THB",
	}}
	handler := NewTikTokShopAPIHandler(&config.Config{
		TikTokShopOpenAPIEnabled: true, TikTokShopReviewedBillEnabled: true,
	}, &tenantTikTokGatewayFake{configured: true}, &tenantTikTokStoreFake{}, nil, nil).
		WithBillShadowPreviewer(previewer)
	router := gin.New()
	router.GET("/orders/:shop_id/:order_id/bill-shadow-preview", handler.GetBillShadowPreview)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet,
		"/orders/7494619203789490654/586030483469993439/bill-shadow-preview", nil))

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"can_create_bill":true`) ||
		!strings.Contains(response.Body.String(), `"review_digest":"`+strings.Repeat("a", 64)+`"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestTikTokShopAPIHandlerCreatesReviewedBillWithExplicitConfirmation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	digest := strings.Repeat("a", 64)
	creator := &tenantTikTokReviewedBillCreatorFake{result: &tiktokshop.TikTokReviewedBillResult{
		BillID: "11111111-1111-4111-8111-111111111111", Status: "pending", DocumentRoute: "saleinvoice",
		ReviewPath: "/sale-invoices", Message: "สร้าง Bill ใน Nexflow แล้ว ยังไม่ได้ส่งเข้า SML",
	}}
	handler := NewTikTokShopAPIHandler(&config.Config{
		TikTokShopOpenAPIEnabled: true, TikTokShopReviewedBillEnabled: true,
	}, &tenantTikTokGatewayFake{configured: true}, &tenantTikTokStoreFake{}, nil, nil).
		WithReviewedBillCreator(creator)
	router := gin.New()
	router.POST("/orders/:shop_id/:order_id/reviewed-bill", func(c *gin.Context) {
		c.Set("user_id", "91e80d9f-aba7-4d9e-89db-e7e4e6d262ef")
		c.Set("trace_id", "trace-reviewed-bill")
		handler.CreateReviewedBill(c)
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost,
		"/orders/7494619203789490654/586030483469993439/reviewed-bill",
		strings.NewReader(`{"confirm":"CREATE_REVIEWED_BILL","review_digest":"`+digest+`"}`)))

	if response.Code != http.StatusCreated || creator.calls != 1 || creator.input.ShopID != "7494619203789490654" ||
		creator.input.OrderID != "586030483469993439" || creator.input.ReviewDigest != digest ||
		creator.input.ActorID != "91e80d9f-aba7-4d9e-89db-e7e4e6d262ef" || creator.input.TraceID != "trace-reviewed-bill" ||
		!strings.Contains(response.Body.String(), `"sml_created":false`) {
		t.Fatalf("status=%d input=%+v body=%s", response.Code, creator.input, response.Body.String())
	}
}

func TestTikTokShopAPIHandlerReviewedBillFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	creator := &tenantTikTokReviewedBillCreatorFake{}
	handler := NewTikTokShopAPIHandler(&config.Config{
		TikTokShopOpenAPIEnabled: true, TikTokShopReviewedBillEnabled: false,
	}, &tenantTikTokGatewayFake{configured: true}, &tenantTikTokStoreFake{}, nil, nil).
		WithReviewedBillCreator(creator)
	router := gin.New()
	router.POST("/orders/:shop_id/:order_id/reviewed-bill", func(c *gin.Context) {
		c.Set("user_id", "91e80d9f-aba7-4d9e-89db-e7e4e6d262ef")
		handler.CreateReviewedBill(c)
	})

	disabled := httptest.NewRecorder()
	router.ServeHTTP(disabled, httptest.NewRequest(http.MethodPost,
		"/orders/7494619203789490654/586030483469993439/reviewed-bill",
		strings.NewReader(`{"confirm":"CREATE_REVIEWED_BILL","review_digest":"`+strings.Repeat("a", 64)+`"}`)))
	if disabled.Code != http.StatusNotFound || creator.calls != 0 {
		t.Fatalf("disabled status=%d calls=%d body=%s", disabled.Code, creator.calls, disabled.Body.String())
	}

	handler.config.TikTokShopReviewedBillEnabled = true
	invalid := httptest.NewRecorder()
	router.ServeHTTP(invalid, httptest.NewRequest(http.MethodPost,
		"/orders/7494619203789490654/586030483469993439/reviewed-bill",
		strings.NewReader(`{"confirm":"YES","review_digest":"bad"}`)))
	if invalid.Code != http.StatusBadRequest || creator.calls != 0 {
		t.Fatalf("invalid status=%d calls=%d body=%s", invalid.Code, creator.calls, invalid.Body.String())
	}

	creator.err = tiktokshop.ErrTikTokReviewedBillReviewChanged
	conflict := httptest.NewRecorder()
	router.ServeHTTP(conflict, httptest.NewRequest(http.MethodPost,
		"/orders/7494619203789490654/586030483469993439/reviewed-bill",
		strings.NewReader(`{"confirm":"CREATE_REVIEWED_BILL","review_digest":"`+strings.Repeat("a", 64)+`"}`)))
	if conflict.Code != http.StatusConflict || creator.calls != 1 || !strings.Contains(conflict.Body.String(), "bill_review_changed") {
		t.Fatalf("conflict status=%d calls=%d body=%s", conflict.Code, creator.calls, conflict.Body.String())
	}
}

func TestTikTokShopAPIHandlerCancellationCreateIsDormantByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: true}, &tenantTikTokGatewayFake{configured: true}, &tenantTikTokStoreFake{}, nil, nil).
		WithCancellation(&TikTokCancellationCoordinator{createEnabled: false})
	router := gin.New()
	router.POST("/orders/:shop_id/:order_id/cancellation", func(c *gin.Context) {
		c.Set("user_id", "91e80d9f-aba7-4d9e-89db-e7e4e6d262ef")
		handler.CreateCancellation(c)
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost,
		"/orders/7494619203789490654/586030483469993439/cancellation",
		strings.NewReader(`{"confirm":"CREATE_TIKTOK_SML_CANCEL_DOCUMENT","review_digest":"`+strings.Repeat("a", 64)+`"}`)))
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "feature_flag_disabled") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestTikTokShopAPIHandlerBillShadowPreviewValidatesPathAndMapsNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previewer := &tenantTikTokBillShadowPreviewerFake{err: tiktokshop.ErrTikTokBillShadowNotFound}
	handler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: true}, &tenantTikTokGatewayFake{configured: true}, &tenantTikTokStoreFake{}, nil, nil).
		WithBillShadowPreviewer(previewer)
	router := gin.New()
	router.GET("/orders/:shop_id/:order_id/bill-shadow-preview", handler.GetBillShadowPreview)

	invalid := httptest.NewRecorder()
	router.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/orders/not-a-shop/586030483469993439/bill-shadow-preview", nil))
	if invalid.Code != http.StatusBadRequest || previewer.calls != 0 {
		t.Fatalf("invalid status=%d calls=%d body=%s", invalid.Code, previewer.calls, invalid.Body.String())
	}

	missing := httptest.NewRecorder()
	router.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/orders/7494619203789490654/586030483469993439/bill-shadow-preview", nil))
	if missing.Code != http.StatusNotFound || previewer.calls != 1 || !strings.Contains(missing.Body.String(), "snapshot_not_found") {
		t.Fatalf("missing status=%d calls=%d body=%s", missing.Code, previewer.calls, missing.Body.String())
	}
}

func TestTikTokShopAPIHandlerPreviewsExactScopedBillShadowMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mapper := &tenantTikTokBillShadowMapperFake{previewResult: models.MarketplaceAliasImpact{
		ConversionStatus: "ready", ImpactDigest: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}}
	handler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: true}, &tenantTikTokGatewayFake{configured: true}, &tenantTikTokStoreFake{}, nil, nil).
		WithBillShadowMapper(mapper)
	router := gin.New()
	router.POST("/orders/:shop_id/:order_id/bill-shadow-mapping/impact-preview", handler.PreviewBillShadowMapping)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost,
		"/orders/7494619203789490654/586030483469993439/bill-shadow-mapping/impact-preview",
		strings.NewReader(`{"product_id":"1729429119195974110","sku_id":"1729429118580984286","item_code":"AH-0006","unit_code":"แท่ง","quantity_multiplier":2,"shop_id":"malicious"}`)))

	if response.Code != http.StatusOK || mapper.previewCalls != 1 || mapper.confirmCalls != 0 ||
		mapper.previewInput.ShopID != "7494619203789490654" || mapper.previewInput.OrderID != "586030483469993439" ||
		mapper.previewInput.ProductID != "1729429119195974110" || mapper.previewInput.SKUID != "1729429118580984286" ||
		mapper.previewInput.ItemCode != "AH-0006" || mapper.previewInput.UnitCode != "แท่ง" || mapper.previewInput.QuantityMultiplier != 2 ||
		!strings.Contains(response.Body.String(), `"conversion_status":"ready"`) {
		t.Fatalf("status=%d input=%+v body=%s", response.Code, mapper.previewInput, response.Body.String())
	}
}

func TestTikTokShopAPIHandlerConfirmsOnlyReviewedBillShadowMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)
	digest := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	mapper := &tenantTikTokBillShadowMapperFake{confirmResult: &repository.MarketplaceAliasCommitResult{
		Alias: &models.MarketplaceItemAlias{ID: "11111111-1111-4111-8111-111111111111", AccountKey: "shop:7494619203789490654"},
		Job:   models.MarketplaceMappingJob{AliasID: "11111111-1111-4111-8111-111111111111", TargetRevision: 1},
	}}
	handler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: true}, &tenantTikTokGatewayFake{configured: true}, &tenantTikTokStoreFake{}, nil, nil).
		WithBillShadowMapper(mapper)
	router := gin.New()
	router.POST("/orders/:shop_id/:order_id/bill-shadow-mapping/confirm", func(c *gin.Context) {
		c.Set("user_id", "91e80d9f-aba7-4d9e-89db-e7e4e6d262ef")
		handler.ConfirmBillShadowMapping(c)
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost,
		"/orders/7494619203789490654/586030483469993439/bill-shadow-mapping/confirm",
		strings.NewReader(`{"product_id":"1729429119195974110","sku_id":"1729429118580984286","item_code":"AH-0006","unit_code":"แท่ง","quantity_multiplier":1,"expected_mapping_revision":0,"impact_digest":"`+digest+`"}`)))

	if response.Code != http.StatusAccepted || mapper.confirmCalls != 1 || mapper.previewCalls != 0 ||
		mapper.actorID != "91e80d9f-aba7-4d9e-89db-e7e4e6d262ef" || mapper.confirmInput.ImpactDigest != digest ||
		mapper.confirmInput.Selection.ShopID != "7494619203789490654" ||
		!strings.Contains(response.Body.String(), `"account_key":"shop:7494619203789490654"`) {
		t.Fatalf("status=%d actor=%q input=%+v body=%s", response.Code, mapper.actorID, mapper.confirmInput, response.Body.String())
	}
}

func TestTikTokShopAPIHandlerBillShadowMappingFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mapper := &tenantTikTokBillShadowMapperFake{err: repository.ErrMarketplaceImpactChanged}
	handler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: true}, &tenantTikTokGatewayFake{configured: true}, &tenantTikTokStoreFake{}, nil, nil).
		WithBillShadowMapper(mapper)
	router := gin.New()
	router.POST("/orders/:shop_id/:order_id/bill-shadow-mapping/confirm", handler.ConfirmBillShadowMapping)

	invalid := httptest.NewRecorder()
	router.ServeHTTP(invalid, httptest.NewRequest(http.MethodPost,
		"/orders/not-a-shop/586030483469993439/bill-shadow-mapping/confirm", strings.NewReader(`{}`)))
	if invalid.Code != http.StatusBadRequest || mapper.confirmCalls != 0 {
		t.Fatalf("invalid status=%d calls=%d body=%s", invalid.Code, mapper.confirmCalls, invalid.Body.String())
	}

	conflict := httptest.NewRecorder()
	router.ServeHTTP(conflict, httptest.NewRequest(http.MethodPost,
		"/orders/7494619203789490654/586030483469993439/bill-shadow-mapping/confirm",
		strings.NewReader(`{"product_id":"1729429119195974110","sku_id":"1729429118580984286","item_code":"AH-0006","unit_code":"แท่ง","quantity_multiplier":1,"expected_mapping_revision":0,"impact_digest":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}`)))
	if conflict.Code != http.StatusConflict || mapper.confirmCalls != 1 || !strings.Contains(conflict.Body.String(), "mapping_changed") {
		t.Fatalf("conflict status=%d calls=%d body=%s", conflict.Code, mapper.confirmCalls, conflict.Body.String())
	}

	mapper.err = errors.New("database unavailable")
	failed := httptest.NewRecorder()
	router.ServeHTTP(failed, httptest.NewRequest(http.MethodPost,
		"/orders/7494619203789490654/586030483469993439/bill-shadow-mapping/confirm",
		strings.NewReader(`{"product_id":"1729429119195974110","sku_id":"1729429118580984286","item_code":"AH-0006","unit_code":"แท่ง","quantity_multiplier":1,"expected_mapping_revision":0,"impact_digest":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}`)))
	if failed.Code != http.StatusInternalServerError || !strings.Contains(failed.Body.String(), "mapping_failed") {
		t.Fatalf("failed status=%d body=%s", failed.Code, failed.Body.String())
	}

	mapper.err = nil
	mapper.confirmResult = nil
	incomplete := httptest.NewRecorder()
	router.ServeHTTP(incomplete, httptest.NewRequest(http.MethodPost,
		"/orders/7494619203789490654/586030483469993439/bill-shadow-mapping/confirm",
		strings.NewReader(`{"product_id":"1729429119195974110","sku_id":"1729429118580984286","item_code":"AH-0006","unit_code":"แท่ง","quantity_multiplier":1,"expected_mapping_revision":0,"impact_digest":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}`)))
	if incomplete.Code != http.StatusInternalServerError || !strings.Contains(incomplete.Body.String(), "mapping_failed") {
		t.Fatalf("incomplete status=%d body=%s", incomplete.Code, incomplete.Body.String())
	}
}

func TestTikTokShopAPIHandlerRejectsInvalidOrderListFilterBeforeStore(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reader := &tenantTikTokOrderReaderFake{}
	handler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: true}, &tenantTikTokGatewayFake{configured: true}, &tenantTikTokStoreFake{}, nil, nil).
		WithOrderReader(reader)
	router := gin.New()
	router.GET("/orders", handler.ListOrders)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/orders?page_size=500&status_group=unknown&order_id=5856%25%27", nil))

	if response.Code != http.StatusBadRequest || reader.calls != 0 || !strings.Contains(response.Body.String(), "invalid_request") {
		t.Fatalf("status=%d calls=%d body=%s", response.Code, reader.calls, response.Body.String())
	}

	invalidGroupResponse := httptest.NewRecorder()
	router.ServeHTTP(invalidGroupResponse, httptest.NewRequest(http.MethodGet, "/orders?status_group=unknown", nil))
	if invalidGroupResponse.Code != http.StatusBadRequest || reader.calls != 0 || !strings.Contains(invalidGroupResponse.Body.String(), "invalid_request") {
		t.Fatalf("invalid group status=%d calls=%d body=%s", invalidGroupResponse.Code, reader.calls, invalidGroupResponse.Body.String())
	}
}

func TestTikTokShopAPIHandlerUpdatesOneShopOnlyWhenGlobalWorkerEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settings := &tenantTikTokOrderSyncSettingsFake{}
	handler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: true, TikTokShopOrderSyncEnabled: true}, &tenantTikTokGatewayFake{configured: true}, &tenantTikTokStoreFake{}, nil, nil).
		WithOrderSyncSettings(settings)
	router := gin.New()
	router.PUT("/order-sync-settings/:shop_id", handler.UpdateOrderSyncSetting)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/order-sync-settings/7494619203789490654", strings.NewReader(
		`{"enabled":true,"interval_seconds":300,"overlap_seconds":900,"config_version":1}`,
	)))

	if response.Code != http.StatusOK || settings.updateShop != "7494619203789490654" || !settings.updateInput.Enabled ||
		!strings.Contains(response.Body.String(), `"config_version":2`) {
		t.Fatalf("status=%d shop=%q input=%+v body=%s", response.Code, settings.updateShop, settings.updateInput, response.Body.String())
	}

	disabledStore := &tenantTikTokOrderSyncSettingsFake{}
	disabledHandler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: true, TikTokShopOrderSyncEnabled: false}, &tenantTikTokGatewayFake{configured: true}, &tenantTikTokStoreFake{}, nil, nil).
		WithOrderSyncSettings(disabledStore)
	disabledRouter := gin.New()
	disabledRouter.PUT("/order-sync-settings/:shop_id", disabledHandler.UpdateOrderSyncSetting)
	disabledResponse := httptest.NewRecorder()
	disabledRouter.ServeHTTP(disabledResponse, httptest.NewRequest(http.MethodPut, "/order-sync-settings/7494619203789490654", strings.NewReader(
		`{"enabled":true,"interval_seconds":300,"overlap_seconds":900,"config_version":1}`,
	)))
	if disabledResponse.Code != http.StatusConflict || disabledStore.updateShop != "" {
		t.Fatalf("disabled status=%d shop=%q body=%s", disabledResponse.Code, disabledStore.updateShop, disabledResponse.Body.String())
	}
}

func TestTikTokShopAPIHandlerDiagnosticsUsesSafeOperationalEvidence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gateway := &tenantTikTokGatewayFake{configured: true, connections: []tiktokshop.GatewayConnection{{
		ShopID: "7494619203789490654", ShopName: "henna_milkford", GrantedScopes: []string{"seller.order.info"},
	}}}
	settings := &tenantTikTokOrderSyncSettingsFake{settings: []tiktokshop.TikTokOrderSyncSetting{{
		ShopID: "7494619203789490654", ShopName: "henna_milkford", Enabled: true, IntervalSeconds: 300,
	}}}
	reader := &tenantTikTokOrderReaderFake{result: &tiktokshop.TikTokOrderSnapshotListResult{
		Data: []tiktokshop.TikTokOrderSnapshotListItem{{
			ShopID: "7494619203789490654", ShopName: "henna_milkford", OrderID: "586030483469993439",
			OrderStatus: tiktokshop.OrderStatusAwaitingCollection,
		}},
		Page: 1, PageSize: 20, TotalItems: 1, TotalPages: 1,
	}}
	previewer := &tenantTikTokBillShadowPreviewerFake{result: &tiktokshop.TikTokBillShadowPreview{
		ShopID: "7494619203789490654", OrderID: "586030483469993439",
		OrderStatus: tiktokshop.OrderStatusAwaitingCollection, ReadyForReviewedBill: true,
		Route: tiktokshop.TikTokBillShadowRoute{Ready: true, SemanticRoute: "sale_invoice", DocFormatCode: "SI", ShippingReady: true},
		Items: []tiktokshop.TikTokBillShadowItem{{Mapping: tiktokshop.TikTokBillShadowItemMapping{Status: tiktokshop.TikTokBillShadowMappingReady}}},
	}}
	handler := NewTikTokShopAPIHandler(&config.Config{
		TikTokShopOpenAPIEnabled: true, TikTokShopOrderSyncEnabled: true, TikTokShopWebhookEnabled: true,
		TikTokShopReviewedBillEnabled: true, TikTokShopSMLSendEnabled: true,
		TikTokShopInAppEnabled: true, TikTokShopInAppEligibleAfter: time.Date(2026, 9, 21, 4, 0, 0, 0, time.UTC),
	}, gateway, &tenantTikTokStoreFake{}, nil, nil).
		WithOrderSyncSettings(settings).
		WithOrderReader(reader).
		WithBillShadowPreviewer(previewer)
	router := gin.New()
	router.GET("/diagnostics", handler.Diagnostics)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/diagnostics?shop_id=7494619203789490654", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Overall string `json:"overall"`
		API     struct {
			ConnectedShops int `json:"connected_shops"`
		} `json:"api"`
		Coverage struct {
			SampledOrders int `json:"sampled_orders"`
			ReadyOrders   int `json:"ready_orders"`
			BlockedOrders int `json:"blocked_orders"`
		} `json:"coverage"`
		AutoSML struct {
			GlobalEnabled bool `json:"global_enabled"`
			CanEnable     bool `json:"can_enable"`
		} `json:"auto_sml"`
		Cancellation struct {
			DocumentCreateEnabled bool `json:"document_create_enabled"`
			WebhookEnabled        bool `json:"webhook_enabled"`
		} `json:"cancellation"`
		InAppNotifications struct {
			Enabled       bool   `json:"enabled"`
			EligibleAfter string `json:"eligible_after"`
		} `json:"in_app_notifications"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode diagnostics: %v body=%s", err, response.Body.String())
	}
	if body.Overall != "ready_for_controlled_enablement" || body.API.ConnectedShops != 1 ||
		body.Coverage.SampledOrders != 1 || body.Coverage.ReadyOrders != 1 || body.Coverage.BlockedOrders != 0 ||
		body.AutoSML.GlobalEnabled || body.AutoSML.CanEnable || body.Cancellation.DocumentCreateEnabled || body.Cancellation.WebhookEnabled ||
		!body.InAppNotifications.Enabled || body.InAppNotifications.EligibleAfter != "2026-09-21T04:00:00Z" {
		t.Fatalf("unexpected diagnostics: %+v body=%s", body, response.Body.String())
	}
	for _, forbidden := range []string{"buyer", "recipient", "phone", "address", "token", "secret", "signature"} {
		if strings.Contains(strings.ToLower(response.Body.String()), forbidden) {
			t.Fatalf("diagnostics leaked %q: %s", forbidden, response.Body.String())
		}
	}
}

func TestTikTokShopAPIHandlerDiagnosticsFailsClosedWhenEvidenceIsBlocked(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reader := &tenantTikTokOrderReaderFake{result: &tiktokshop.TikTokOrderSnapshotListResult{
		Data: []tiktokshop.TikTokOrderSnapshotListItem{{
			ShopID: "7494619203789490654", OrderID: "586030483469993439",
			OrderStatus: tiktokshop.OrderStatusAwaitingCollection,
		}}, Page: 1, PageSize: 20, TotalItems: 1, TotalPages: 1,
	}}
	previewer := &tenantTikTokBillShadowPreviewerFake{result: &tiktokshop.TikTokBillShadowPreview{
		ShopID: "7494619203789490654", OrderID: "586030483469993439",
		Blockers: []tiktokshop.TikTokBillShadowBlocker{{Code: tiktokshop.TikTokBillShadowBlockerMappingMissing, Message: "ยังไม่ได้จับคู่สินค้า"}},
	}}
	handler := NewTikTokShopAPIHandler(&config.Config{
		TikTokShopOpenAPIEnabled: true, TikTokShopOrderSyncEnabled: true, TikTokShopWebhookEnabled: true,
		TikTokShopReviewedBillEnabled: true, TikTokShopSMLSendEnabled: true, TikTokShopAutoSMLEnabled: true,
	}, &tenantTikTokGatewayFake{configured: true, connections: []tiktokshop.GatewayConnection{{ShopID: "7494619203789490654"}}}, &tenantTikTokStoreFake{}, nil, nil).
		WithOrderSyncSettings(&tenantTikTokOrderSyncSettingsFake{settings: []tiktokshop.TikTokOrderSyncSetting{{ShopID: "7494619203789490654", Enabled: true}}}).
		WithOrderReader(reader).
		WithBillShadowPreviewer(previewer)
	router := gin.New()
	router.GET("/diagnostics", handler.Diagnostics)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/diagnostics", nil))

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"overall":"needs_attention"`) ||
		!strings.Contains(response.Body.String(), `"mapping_missing":1`) ||
		!strings.Contains(response.Body.String(), `"can_enable":false`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestTikTokShopAPIHandlerDiagnosticsAllowsControlledEnablementWithReadySampleAndHistoricalBlocker(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const shopID = "7494619203789490654"
	readyOrderID := "586174698013623377"
	blockedOrderID := "585770512786294451"
	reader := &tenantTikTokOrderReaderFake{result: &tiktokshop.TikTokOrderSnapshotListResult{
		Data: []tiktokshop.TikTokOrderSnapshotListItem{
			{ShopID: shopID, OrderID: readyOrderID, OrderStatus: tiktokshop.OrderStatusAwaitingCollection},
			{ShopID: shopID, OrderID: blockedOrderID, OrderStatus: tiktokshop.OrderStatusCompleted},
		},
		Page: 1, PageSize: 20, TotalItems: 2, TotalPages: 1,
	}}
	previewer := &tenantTikTokBillShadowPreviewerFake{results: map[string]*tiktokshop.TikTokBillShadowPreview{
		readyOrderID: {
			ShopID: shopID, OrderID: readyOrderID, OrderStatus: tiktokshop.OrderStatusAwaitingCollection,
			ReadyForReviewedBill: true,
			Route:                tiktokshop.TikTokBillShadowRoute{Ready: true, ShippingReady: true, SemanticRoute: "sale_invoice", DocFormatCode: "SI"},
			Items:                []tiktokshop.TikTokBillShadowItem{{Mapping: tiktokshop.TikTokBillShadowItemMapping{Status: tiktokshop.TikTokBillShadowMappingReady}}},
		},
		blockedOrderID: {
			ShopID: shopID, OrderID: blockedOrderID, OrderStatus: tiktokshop.OrderStatusCompleted,
			Blockers: []tiktokshop.TikTokBillShadowBlocker{{Code: tiktokshop.TikTokBillShadowBlockerMappingMissing, Message: "ยังไม่ได้จับคู่สินค้า"}},
		},
	}}
	handler := NewTikTokShopAPIHandler(&config.Config{
		TikTokShopOpenAPIEnabled: true, TikTokShopOrderSyncEnabled: true, TikTokShopWebhookEnabled: true,
		TikTokShopReviewedBillEnabled: true, TikTokShopSMLSendEnabled: true, TikTokShopAutoSMLEnabled: true,
	}, &tenantTikTokGatewayFake{configured: true, connections: []tiktokshop.GatewayConnection{{ShopID: shopID}}}, &tenantTikTokStoreFake{}, nil, nil).
		WithOrderSyncSettings(&tenantTikTokOrderSyncSettingsFake{settings: []tiktokshop.TikTokOrderSyncSetting{{ShopID: shopID, Enabled: true}}}).
		WithOrderReader(reader).
		WithBillShadowPreviewer(previewer)
	router := gin.New()
	router.GET("/diagnostics", handler.Diagnostics)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/diagnostics?shop_id="+shopID, nil))

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"overall":"ready_for_controlled_enablement"`) ||
		!strings.Contains(response.Body.String(), `"ready_orders":1`) || !strings.Contains(response.Body.String(), `"blocked_orders":1`) ||
		!strings.Contains(response.Body.String(), `"mapping_missing":1`) || !strings.Contains(response.Body.String(), `"can_enable":true`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestTikTokShopAPIHandlerListsDormantAutoSMLSettings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &tenantTikTokAutoSMLSettingsFake{settings: []models.TikTokAutoSMLSetting{{
		ShopID: "7494619203789490654", ShopName: "henna_milkford", TriggerStatus: models.TikTokAutoSMLTriggerAwaitingCollection, ConfigVersion: 1,
	}}}
	handler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: true}, &tenantTikTokGatewayFake{configured: true}, &tenantTikTokStoreFake{}, nil, nil).
		WithAutoSML(store)
	router := gin.New()
	router.GET("/auto-sml/settings", handler.AutoSMLSettings)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/auto-sml/settings", nil))

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"global_enabled":false`) ||
		!strings.Contains(response.Body.String(), `"historical_backfill":false`) ||
		!strings.Contains(response.Body.String(), `"trigger_status":"AWAITING_COLLECTION"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestTikTokShopAPIHandlerCannotEnableAutoSMLWhileGlobalGateIsOff(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &tenantTikTokAutoSMLSettingsFake{}
	handler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: true, TikTokShopAutoSMLEnabled: false}, &tenantTikTokGatewayFake{configured: true}, &tenantTikTokStoreFake{}, nil, nil).
		WithAutoSML(store)
	router := gin.New()
	router.PUT("/auto-sml/settings/:shop_id", handler.UpdateAutoSMLSetting)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/auto-sml/settings/7494619203789490654", strings.NewReader(
		`{"enabled":true,"expected_config_version":1,"confirm":"ENABLE_TIKTOK_AUTO_SML"}`,
	)))

	if response.Code != http.StatusConflict || store.updated.ShopID != "" || !strings.Contains(response.Body.String(), "auto_sml_global_disabled") {
		t.Fatalf("status=%d update=%+v body=%s", response.Code, store.updated, response.Body.String())
	}
}

func TestTikTokShopAPIHandlerRequiresExplicitConfirmationToDisableAutoSML(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &tenantTikTokAutoSMLSettingsFake{settings: []models.TikTokAutoSMLSetting{{
		ShopID: "7494619203789490654", Enabled: true, ConfigVersion: 3,
	}}}
	handler := NewTikTokShopAPIHandler(&config.Config{TikTokShopOpenAPIEnabled: true, TikTokShopAutoSMLEnabled: true}, &tenantTikTokGatewayFake{configured: true}, &tenantTikTokStoreFake{}, nil, nil).
		WithAutoSML(store)
	router := gin.New()
	router.PUT("/auto-sml/settings/:shop_id", handler.UpdateAutoSMLSetting)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/auto-sml/settings/7494619203789490654", strings.NewReader(
		`{"enabled":false,"expected_config_version":3}`,
	)))
	if response.Code != http.StatusBadRequest || store.updated.ShopID != "" || !strings.Contains(response.Body.String(), "confirmation_required") {
		t.Fatalf("status=%d update=%+v body=%s", response.Code, store.updated, response.Body.String())
	}

	confirmed := httptest.NewRecorder()
	router.ServeHTTP(confirmed, httptest.NewRequest(http.MethodPut, "/auto-sml/settings/7494619203789490654", strings.NewReader(
		`{"enabled":false,"expected_config_version":3,"confirm":"DISABLE_TIKTOK_AUTO_SML"}`,
	)))
	if confirmed.Code != http.StatusOK || store.updated.ShopID != "7494619203789490654" || store.updated.Enabled {
		t.Fatalf("status=%d update=%+v body=%s", confirmed.Code, store.updated, confirmed.Body.String())
	}
}

func TestTikTokShopAPIHandlerOperationsSummaryAvoidsOrderPreviews(t *testing.T) {
	gin.SetMode(gin.TestMode)
	shopID := "7494619203789490654"
	reader := &tenantTikTokOrderReaderFake{result: &tiktokshop.TikTokOrderSnapshotListResult{}}
	previewer := &tenantTikTokBillShadowPreviewerFake{}
	handler := NewTikTokShopAPIHandler(&config.Config{
		TikTokShopOpenAPIEnabled: true, TikTokShopOrderSyncEnabled: true,
		TikTokShopWebhookEnabled: true, TikTokShopSMLSendEnabled: true,
		TikTokShopAutoSMLEnabled: true,
	}, &tenantTikTokGatewayFake{configured: true}, &tenantTikTokStoreFake{}, nil, nil).
		WithOrderSyncSettings(&tenantTikTokOrderSyncSettingsFake{settings: []tiktokshop.TikTokOrderSyncSetting{{ShopID: shopID, ShopName: "ร้านทดสอบ", Enabled: true, IntervalSeconds: 300}}}).
		WithOrderReader(reader).
		WithBillShadowPreviewer(previewer).
		WithAutoSML(&tenantTikTokAutoSMLSettingsFake{settings: []models.TikTokAutoSMLSetting{{ShopID: shopID, ShopName: "ร้านทดสอบ", Enabled: true, ConfigVersion: 2}}})
	router := gin.New()
	router.GET("/operations-summary", handler.OperationsSummary)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/operations-summary?shop_id="+shopID, nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"shop_id":"`+shopID+`"`) || !strings.Contains(response.Body.String(), `"webhook_enabled":true`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if reader.calls != 0 || previewer.calls != 0 {
		t.Fatalf("summary must not read order evidence: reader=%d previewer=%d", reader.calls, previewer.calls)
	}
}

func TestTikTokAutoSMLRouteSignatureChangesWithRouteEvidence(t *testing.T) {
	base := tiktokshop.TikTokBillShadowRoute{
		Ready: true, ShippingReady: true, SemanticRoute: "sale_invoice", DocFormatCode: "SI",
		ConfigVersion: 3, ShippingItemCode: "AH-0061", ShippingItemUnitCode: "ชิ้น",
	}
	first := tikTokAutoSMLRouteSignature(base)
	if len(first) != 64 {
		t.Fatalf("signature=%q", first)
	}
	base.ConfigVersion++
	if next := tikTokAutoSMLRouteSignature(base); next == first {
		t.Fatal("route config version change must invalidate the signature")
	}
	base.Ready = false
	if next := tikTokAutoSMLRouteSignature(base); next != "" {
		t.Fatalf("not-ready route signature=%q, want empty", next)
	}
}

func TestTikTokAutoSMLBillFingerprintIgnoresForwardStatusButDetectsBillChanges(t *testing.T) {
	preview := &tiktokshop.TikTokBillShadowPreview{
		ShopID: "7494619203789490654", OrderID: "586030483469993439", Currency: "THB",
		OrderStatus: tiktokshop.OrderStatusAwaitingCollection,
		Amounts:     tiktokshop.TikTokBillShadowAmounts{ProductSubtotal: "100.00", Shipping: "15.00", ProposedDocumentTotal: "115.00"},
		Items: []tiktokshop.TikTokBillShadowItem{{
			ProductID: "product-1", SKUID: "sku-1", Quantity: 1, UnitSalePrice: "100.00", LineTotal: "100.00",
			Mapping: tiktokshop.TikTokBillShadowItemMapping{Status: tiktokshop.TikTokBillShadowMappingReady, ItemCode: "AH-0001", UnitCode: "ชิ้น", SMLQuantity: "1", MappingRevision: 2},
		}},
	}
	first, err := tikTokAutoSMLBillFingerprint(preview)
	if err != nil || len(first) != 64 {
		t.Fatalf("fingerprint=%q err=%v", first, err)
	}
	preview.OrderStatus = tiktokshop.OrderStatusInTransit
	second, err := tikTokAutoSMLBillFingerprint(preview)
	if err != nil || second != first {
		t.Fatalf("forward status changed fingerprint: first=%s second=%s err=%v", first, second, err)
	}
	preview.Items[0].Mapping.MappingRevision++
	third, err := tikTokAutoSMLBillFingerprint(preview)
	if err != nil || third == first {
		t.Fatalf("mapping change did not invalidate fingerprint: first=%s third=%s err=%v", first, third, err)
	}
}
