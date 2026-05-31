package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/config"
	"nexflow/internal/repository"
)

func TestDecodeSMLUnitsPayloadSupportsNestedAndDeduplicates(t *testing.T) {
	payload := `{"success":true,"data":{"units":[
		{"code":" ชิ้น ","name_1":" ชิ้น "},
		{"code":"ชิ้น","name_1":"duplicate"},
		{"code":"ถุง","name_1":""}
	]}}`

	units, err := decodeSMLUnitsPayload(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("decode units: %v", err)
	}
	if len(units) != 2 {
		t.Fatalf("units len = %d, want 2", len(units))
	}
	if units[0].Code != "ชิ้น" || units[0].Name1 != "ชิ้น" {
		t.Fatalf("first unit = %+v, want trimmed ชิ้น", units[0])
	}
	if units[1].Code != "ถุง" || units[1].Name1 != "ถุง" {
		t.Fatalf("fallback name = %+v, want code used as name", units[1])
	}
}

func TestCatalogGetUnitsProxyForwardsSMLHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ic/units" {
			t.Fatalf("path = %s, want /api/v1/ic/units", r.URL.Path)
		}
		if got := r.URL.Query().Get("size"); got != "50" {
			t.Fatalf("size = %q, want 50", got)
		}
		if got := r.URL.Query().Get("search"); got != "ชิ้น" {
			t.Fatalf("search = %q, want ชิ้น", got)
		}
		for key, want := range map[string]string{
			"guid":           "guid-1",
			"provider":       "provider-1",
			"configFileName": "cfg-1",
			"databaseName":   "sml1_2026",
			"X-Tenant":       "sml1_2026",
		} {
			if got := r.Header.Get(key); got != want {
				t.Fatalf("%s header = %q, want %q", key, got, want)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"units":[{"code":"ชิ้น","name_1":"ชิ้น"}]}}`))
	}))
	defer upstream.Close()

	h := &CatalogHandler{
		cfg: &config.Config{
			ShopeeSMLURL:        upstream.URL,
			ShopeeSMLGUID:       "guid-1",
			ShopeeSMLProvider:   "provider-1",
			ShopeeSMLConfigFile: "cfg-1",
			ShopeeSMLDatabase:   "sml1_2026",
		},
		logger: zap.NewNop(),
	}
	router := gin.New()
	router.GET("/api/sml/units", h.GetUnits)

	req := httptest.NewRequest(http.MethodGet, "/api/sml/units?search=%E0%B8%8A%E0%B8%B4%E0%B9%89%E0%B8%99&limit=50", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Units []catalogUnitOption `json:"units"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Units) != 1 || body.Units[0].Code != "ชิ้น" {
		t.Fatalf("units = %+v, want one ชิ้น", body.Units)
	}
}

func TestCatalogGetProductUnitsReturnsEmptyWhenUpstreamNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ic/products/NO-SUCH/units" {
			t.Fatalf("path = %s, want product units path", r.URL.Path)
		}
		http.Error(w, `{"error":{"message":"not found"}}`, http.StatusNotFound)
	}))
	defer upstream.Close()

	h := &CatalogHandler{
		cfg:    &config.Config{ShopeeSMLURL: upstream.URL},
		logger: zap.NewNop(),
	}
	router := gin.New()
	router.GET("/api/catalog/:code/units", h.GetProductUnits)

	req := httptest.NewRequest(http.MethodGet, "/api/catalog/NO-SUCH/units", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Units []catalogUnitOption `json:"units"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Units) != 0 {
		t.Fatalf("units = %+v, want empty", body.Units)
	}
}

func TestCatalogGetProductUnitsFallsBackToLocalCatalogUnit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ic/products/SHIP_POL/units" {
			t.Fatalf("path = %s, want product units path", r.URL.Path)
		}
		http.Error(w, `{"error":{"message":"not found"}}`, http.StatusNotFound)
	}))
	defer upstream.Close()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	now := time.Now()
	mock.ExpectQuery("SELECT item_code, item_name, item_name2, unit_code, wh_code, shelf_code,").
		WithArgs("SHIP_POL").
		WillReturnRows(sqlmock.NewRows(catalogUnitFallbackColumns()).
			AddRow("SHIP_POL", "ค่าขนส่งสินค้า", "", "บาท", "", "", nil, "", nil, "pending", nil, 0, nil, "", nil, nil, now, now))

	h := &CatalogHandler{
		cfg:         &config.Config{ShopeeSMLURL: upstream.URL},
		catalogRepo: repository.NewSMLCatalogRepo(db),
		logger:      zap.NewNop(),
	}
	router := gin.New()
	router.GET("/api/catalog/:code/units", h.GetProductUnits)

	req := httptest.NewRequest(http.MethodGet, "/api/catalog/SHIP_POL/units", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Units []catalogUnitOption `json:"units"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Units) != 1 || body.Units[0].Code != "บาท" || !body.Units[0].IsDefault {
		t.Fatalf("units = %+v, want local default บาท", body.Units)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet db expectations: %v", err)
	}
}

func catalogUnitFallbackColumns() []string {
	return []string{
		"item_code",
		"item_name",
		"item_name2",
		"unit_code",
		"wh_code",
		"shelf_code",
		"price",
		"group_code",
		"balance_qty",
		"embedding_status",
		"embedded_at",
		"image_count",
		"primary_image_roworder",
		"primary_image_guid",
		"primary_image_bytes",
		"image_synced_at",
		"synced_at",
		"created_at",
	}
}
