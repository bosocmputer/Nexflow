package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestCreateProductRejectsHiddenItemCodeBeforeSML(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := &CatalogHandler{logger: zap.NewNop()}
	router := gin.New()
	router.POST("/api/catalog/products", h.CreateProduct)

	body := `{"code":"\u0E3ABILLS002","name":"test product","unit_code":"ชิ้น"}`
	req := httptest.NewRequest(http.MethodPost, "/api/catalog/products", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"clean_item_code":"BILLS002"`) {
		t.Fatalf("response missing clean suggestion: %s", rec.Body.String())
	}
}
