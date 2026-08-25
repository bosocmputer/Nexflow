package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/repository"
)

func TestCatalogMarketplaceLinksReturnsBoundedPageAndCursor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)FROM marketplace_item_aliases a.*a.item_code=\$1.*ORDER BY a.source,a.account_key,a.id`).
		WithArgs("SKU-1", 2).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source", "account_key", "account_name", "product_name", "variant_name",
			"source_sku", "external_item_id", "external_variant_id", "unit_code",
			"quantity_multiplier", "conversion_status", "scope_confirmed", "updated_at",
		}).
			AddRow("alias-1", "shopee", "shop:1", "AOY", "สินค้า A", "สีแดง", "SKU-A-R", "100", "200", "ชิ้น", 1, "ready", true, now).
			AddRow("alias-2", "tiktok", "default", "", "สินค้า B", "แบบปกติ", "SKU-B", "300", "400", "ชิ้น", 2, "needs_review", true, now))

	h := &CatalogHandler{catalogRepo: repository.NewSMLCatalogRepo(db), logger: zap.NewNop()}
	router := gin.New()
	router.GET("/api/catalog/:code/marketplace-links", h.MarketplaceLinks)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/catalog/SKU-1/marketplace-links?limit=1", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Data       []models.CatalogMarketplaceLink `json:"data"`
		HasMore    bool                            `json:"has_more"`
		NextCursor string                          `json:"next_cursor"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Data) != 1 || !response.HasMore || response.NextCursor == "" {
		t.Fatalf("response = %#v", response)
	}
	if response.Data[0].ProductName != "สินค้า A" {
		t.Fatalf("first link = %#v", response.Data[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestCatalogMarketplaceLinksRejectsInvalidCursor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &CatalogHandler{logger: zap.NewNop()}
	router := gin.New()
	router.GET("/api/catalog/:code/marketplace-links", h.MarketplaceLinks)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/catalog/SKU-1/marketplace-links?cursor=not-base64", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}
