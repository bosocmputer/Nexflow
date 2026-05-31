package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/repository"
	"nexflow/internal/services/catalog"
)

func TestCatalogRefreshBatchPartialSuccessAndDuplicate(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/ic/products/SHIP_POL":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"data":{"code":"SHIP_POL","name":"ค่าขนส่งสินค้า","unit_standard":"บาท","group_code":"SERVICE","balance_qty":0}}`))
		case "/api/v1/ic/products/NO_SUCH":
			http.NotFound(w, r)
		default:
			t.Fatalf("unexpected upstream path: %s", r.URL.Path)
		}
	}))
	defer upstream.Close()

	expectCatalogGetOneForBatch(mock, "SHIP_POL").WillReturnRows(sqlmock.NewRows(catalogUnitFallbackColumns()))
	mock.ExpectExec("INSERT INTO sml_catalog").
		WithArgs(
			"SHIP_POL", "ค่าขนส่งสินค้า", "", "บาท", "", "",
			nil, "SERVICE", sqlmock.AnyArg(), 0, nil, "", nil, true,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	now := time.Now()
	expectCatalogGetOneForBatch(mock, "SHIP_POL").WillReturnRows(
		sqlmock.NewRows(catalogUnitFallbackColumns()).
			AddRow("SHIP_POL", "ค่าขนส่งสินค้า", "", "บาท", "", "", nil, "SERVICE", float64(0), "pending", nil, 0, nil, "", nil, nil, now, now),
	)

	repo := repository.NewSMLCatalogRepo(db)
	h := &CatalogHandler{
		catalogSvc: catalog.NewSMLCatalogService(repo, upstream.URL, nil, zap.NewNop()),
		logger:     zap.NewNop(),
	}
	router := gin.New()
	router.POST("/api/catalog/refresh-batch", h.RefreshBatch)

	req := httptest.NewRequest(http.MethodPost, "/api/catalog/refresh-batch", strings.NewReader(`{"codes":["SHIP_POL","NO_SUCH","SHIP_POL"]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Summary catalogRefreshBatchSummary  `json:"summary"`
		Results []catalogRefreshBatchResult `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Summary.Total != 3 || body.Summary.Success != 1 || body.Summary.NotFound != 1 || body.Summary.Duplicate != 1 || body.Summary.Failed != 0 {
		t.Fatalf("summary = %+v", body.Summary)
	}
	statuses := map[string]int{}
	for _, result := range body.Results {
		statuses[result.Status]++
	}
	if statuses["success"] != 1 || statuses["not_found"] != 1 || statuses["duplicate"] != 1 {
		t.Fatalf("statuses = %+v, results = %+v", statuses, body.Results)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogRefreshBatchRejectsTooManyCodes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := &CatalogHandler{logger: zap.NewNop()}
	router := gin.New()
	router.POST("/api/catalog/refresh-batch", h.RefreshBatch)

	codes := make([]string, catalogRefreshBatchLimit+1)
	for i := range codes {
		codes[i] = fmt.Sprintf("SKU%03d", i)
	}
	body, _ := json.Marshal(map[string][]string{"codes": codes})
	req := httptest.NewRequest(http.MethodPost, "/api/catalog/refresh-batch", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestCatalogRefreshBatchSanitizesSMLError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"password":"secret","stack":"trace"}`, http.StatusInternalServerError)
	}))
	defer upstream.Close()

	h := &CatalogHandler{
		catalogSvc: catalog.NewSMLCatalogService(repository.NewSMLCatalogRepo(db), upstream.URL, nil, zap.NewNop()),
		logger:     zap.NewNop(),
	}
	router := gin.New()
	router.POST("/api/catalog/refresh-batch", h.RefreshBatch)

	req := httptest.NewRequest(http.MethodPost, "/api/catalog/refresh-batch", strings.NewReader("{\"codes\":[\"\\uFEFFSKU500\"]}"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), upstream.URL) || strings.Contains(rec.Body.String(), "secret") || strings.Contains(rec.Body.String(), "stack") {
		t.Fatalf("response leaked raw upstream error: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"status":"failed"`) {
		t.Fatalf("response missing failed status: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"hidden_char_kinds":["bom"]`) {
		t.Fatalf("response missing hidden char kind: %s", rec.Body.String())
	}
}

func TestCatalogHiddenCodesEndpointReturnsLimitedDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Now()
	mock.ExpectQuery("SELECT item_code, item_name, item_name2, unit_code, wh_code, shelf_code,").
		WillReturnRows(sqlmock.NewRows(catalogUnitFallbackColumns()).
			AddRow("\uFEFFITEM001", "สินค้าทดสอบ 1", "", "ชิ้น", "", "", nil, "", nil, "pending", nil, 0, nil, "", nil, nil, now, now).
			AddRow("ITEM002", "สินค้าปกติ", "", "ชิ้น", "", "", nil, "", nil, "pending", nil, 0, nil, "", nil, nil, now, now).
			AddRow("ITEM\u200B003", "สินค้าทดสอบ 3", "", "ชิ้น", "", "", nil, "", nil, "pending", nil, 0, nil, "", nil, nil, now, now))

	h := &CatalogHandler{
		catalogRepo: repository.NewSMLCatalogRepo(db),
		logger:      zap.NewNop(),
	}
	router := gin.New()
	router.GET("/api/catalog/hidden-codes", h.HiddenCodes)

	req := httptest.NewRequest(http.MethodGet, "/api/catalog/hidden-codes?limit=1", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body hiddenCatalogCodesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Total != 2 || body.Limit != 1 || !body.HasMore || len(body.Data) != 1 {
		t.Fatalf("body = %+v", body)
	}
	if body.Data[0].CleanItemCode != "ITEM001" || len(body.Data[0].HiddenCharKinds) != 1 || body.Data[0].HiddenCharKinds[0] != "bom" {
		t.Fatalf("hidden metadata = %+v", body.Data[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func expectCatalogGetOneForBatch(mock sqlmock.Sqlmock, code string) *sqlmock.ExpectedQuery {
	return mock.ExpectQuery("SELECT item_code, item_name, item_name2, unit_code, wh_code, shelf_code,").
		WithArgs(code)
}
