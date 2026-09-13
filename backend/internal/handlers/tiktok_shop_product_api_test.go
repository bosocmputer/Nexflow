package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"nexflow/internal/config"
	"nexflow/internal/models"
	"nexflow/internal/services/tiktokshop"
)

type tenantTikTokProductCatalogFake struct {
	result    *tiktokshop.TikTokCatalogRunResult
	list      *tiktokshop.TikTokCatalogListResult
	err       error
	shopID    string
	trigger   string
	filter    tiktokshop.TikTokCatalogListFilter
	syncCalls int
	listCalls int
}

func (f *tenantTikTokProductCatalogFake) Sync(_ context.Context, shopID, trigger string) (*tiktokshop.TikTokCatalogRunResult, error) {
	f.syncCalls++
	f.shopID, f.trigger = shopID, trigger
	return f.result, f.err
}

func (f *tenantTikTokProductCatalogFake) List(_ context.Context, filter tiktokshop.TikTokCatalogListFilter) (*tiktokshop.TikTokCatalogListResult, error) {
	f.listCalls++
	f.filter = filter
	return f.list, f.err
}

type tenantTikTokAuditFake struct {
	entries []models.AuditEntry
}

func (f *tenantTikTokAuditFake) Log(entry models.AuditEntry) error {
	f.entries = append(f.entries, entry)
	return nil
}

func TestTikTokProductCatalogSyncRequiresFeatureGate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	catalog := &tenantTikTokProductCatalogFake{}
	handler := NewTikTokShopAPIHandler(
		&config.Config{TikTokShopOpenAPIEnabled: true},
		&tenantTikTokGatewayFake{configured: true}, nil, nil, nil,
	).WithProductCatalog(catalog, catalog)
	router := gin.New()
	router.POST("/sync", handler.SyncProductCatalog)
	request := httptest.NewRequest(http.MethodPost, "/sync", strings.NewReader(`{"shop_id":"7494619203789490654"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || catalog.syncCalls != 0 || !strings.Contains(response.Body.String(), "feature_disabled") {
		t.Fatalf("status=%d body=%s calls=%d", response.Code, response.Body.String(), catalog.syncCalls)
	}
}

func TestTikTokProductCatalogSyncAuditsSuccessfulManualRefresh(t *testing.T) {
	gin.SetMode(gin.TestMode)
	catalog := &tenantTikTokProductCatalogFake{result: &tiktokshop.TikTokCatalogRunResult{
		ID: "0d0615a1-789c-44ca-95eb-56343232dd3d", ShopID: "7494619203789490654",
		Status: "succeeded", PageCount: 1, ProductCount: 4, SKUCount: 5, WarehouseCount: 5,
	}}
	audit := &tenantTikTokAuditFake{}
	handler := NewTikTokShopAPIHandler(
		&config.Config{TikTokShopOpenAPIEnabled: true, TikTokShopProductCatalogEnabled: true},
		&tenantTikTokGatewayFake{configured: true}, nil, nil, nil,
	).WithProductCatalog(catalog, catalog).WithAuditLogger(audit)
	router := gin.New()
	router.POST("/sync", func(c *gin.Context) {
		c.Set("user_id", "91e80d9f-aba7-4d9e-89db-e7e4e6d262ef")
		handler.SyncProductCatalog(c)
	})
	request := httptest.NewRequest(http.MethodPost, "/sync", strings.NewReader(`{"shop_id":"7494619203789490654"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || catalog.shopID != "7494619203789490654" || catalog.trigger != "manual" {
		t.Fatalf("status=%d body=%s catalog=%+v", response.Code, response.Body.String(), catalog)
	}
	if len(audit.entries) != 1 || audit.entries[0].Action != "tiktok_shop_product_catalog_synced" || audit.entries[0].Source != "tiktok_shop" {
		t.Fatalf("audit=%+v", audit.entries)
	}
}

func TestTikTokProductCatalogListUsesBoundedFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	catalog := &tenantTikTokProductCatalogFake{list: &tiktokshop.TikTokCatalogListResult{
		Data: []tiktokshop.TikTokCatalogListItem{{ShopID: "7494619203789490654", ProductID: "1001", SKUID: "2001"}},
		Page: 2, PageSize: 20, TotalItems: 21, TotalPages: 2,
	}}
	handler := NewTikTokShopAPIHandler(
		&config.Config{TikTokShopOpenAPIEnabled: true, TikTokShopProductCatalogEnabled: true},
		&tenantTikTokGatewayFake{configured: true}, nil, nil, nil,
	).WithProductCatalog(catalog, catalog)
	router := gin.New()
	router.GET("/products", handler.ListProductCatalog)
	request := httptest.NewRequest(http.MethodGet, "/products?shop_id=7494619203789490654&q=AOY&page=2&page_size=20&status=ACTIVATE", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || catalog.listCalls != 1 || catalog.filter.Page != 2 || catalog.filter.PageSize != 20 || catalog.filter.Status != tiktokshop.ProductStatusActivate {
		t.Fatalf("status=%d body=%s catalog=%+v", response.Code, response.Body.String(), catalog)
	}
}
