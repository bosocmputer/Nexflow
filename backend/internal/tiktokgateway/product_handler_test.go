package tiktokgateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"nexflow/internal/services/tiktokshop"
)

type handlerProductServiceFake struct {
	searchResult    *ProductSearchResult
	inventoryResult *InventorySearchResult
	updateResult    *InventoryUpdateResult
	err             error
	tenant          string
	shopID          string
	productID       string
	searchInput     tiktokshop.SearchProductsRequest
	updateInput     tiktokshop.UpdateInventoryRequest
}

func (f *handlerProductServiceFake) SearchProducts(_ context.Context, tenant, shopID string, input tiktokshop.SearchProductsRequest) (*ProductSearchResult, error) {
	f.tenant, f.shopID, f.searchInput = tenant, shopID, input
	return f.searchResult, f.err
}

func (f *handlerProductServiceFake) GetProduct(_ context.Context, tenant, shopID, productID string) (*ProductDetailResult, error) {
	f.tenant, f.shopID, f.productID = tenant, shopID, productID
	return nil, f.err
}

func (f *handlerProductServiceFake) SearchInventory(_ context.Context, tenant, shopID string, _ tiktokshop.InventorySearchRequest) (*InventorySearchResult, error) {
	f.tenant, f.shopID = tenant, shopID
	return f.inventoryResult, f.err
}

func (f *handlerProductServiceFake) UpdateInventory(_ context.Context, tenant, shopID, productID string, input tiktokshop.UpdateInventoryRequest) (*InventoryUpdateResult, error) {
	f.tenant, f.shopID, f.productID, f.updateInput = tenant, shopID, productID, input
	return f.updateResult, f.err
}

func TestTikTokGatewayHandlerSearchesProductsAndAuditsOperation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	products := &handlerProductServiceFake{searchResult: &ProductSearchResult{
		UpstreamRequestID: "req-products", TotalCount: 1,
		Products: []tiktokshop.Product{{ID: "1729429119195974110", Title: "AOY Product", Status: tiktokshop.ProductStatusActivate}},
	}}
	audit := &handlerAuditFake{}
	handler := NewHandler(&handlerOAuthServiceFake{}, handlerVerifierFake{}, audit, Config{}, nil, WithProductGatewayService(products))
	router := gin.New()
	handler.Register(router)
	request := httptest.NewRequest(http.MethodPost, tiktokshop.GatewayProductSearchPath, strings.NewReader(`{"shop_id":"7494619203789490654","search":{"page_size":100,"status":"ALL"}}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "1729429119195974110") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if products.tenant != "aoy" || products.searchInput.PageSize != 100 || audit.operation != "product_search" {
		t.Fatalf("products=%+v audit=%+v", products, audit)
	}
}

func TestTikTokGatewayHandlerRejectsProductReadWhenScopeUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	products := &handlerProductServiceFake{err: ErrProductBasicScopeUnavailable}
	handler := NewHandler(&handlerOAuthServiceFake{}, handlerVerifierFake{}, nil, Config{}, nil, WithProductGatewayService(products))
	router := gin.New()
	handler.Register(router)
	request := httptest.NewRequest(http.MethodPost, tiktokshop.GatewayProductSearchPath, strings.NewReader(`{"shop_id":"7494619203789490654","search":{"page_size":100}}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "product_scope_required") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestTikTokGatewayHandlerReturnsPerSKUInventoryWriteFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	products := &handlerProductServiceFake{updateResult: &InventoryUpdateResult{
		UpstreamRequestID: "req-write", Errors: []tiktokshop.InventoryUpdateError{{Code: 12052990, Message: "Check failed", Detail: tiktokshop.InventoryUpdateErrorDetail{SKUID: "1729429119195974111"}}},
	}}
	handler := NewHandler(&handlerOAuthServiceFake{}, handlerVerifierFake{}, nil, Config{}, nil, WithProductGatewayService(products))
	router := gin.New()
	handler.Register(router)
	request := httptest.NewRequest(http.MethodPost, tiktokshop.GatewayInventoryUpdatePath, strings.NewReader(`{"shop_id":"7494619203789490654","product_id":"1729429119195974110","update":{"skus":[{"id":"1729429119195974111","inventory":[{"warehouse_id":"7068517275539719942","quantity":7}]}]}}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "inventory_item_rejected") || products.productID != "1729429119195974110" {
		t.Fatalf("status=%d body=%s products=%+v", response.Code, response.Body.String(), products)
	}
}
