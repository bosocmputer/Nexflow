package handlers

import (
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/config"
	"nexflow/internal/repository"
)

type instanceAuditDetailMatcher struct{}

func (instanceAuditDetailMatcher) Match(value driver.Value) bool {
	bytes, ok := value.([]byte)
	if !ok {
		return false
	}
	detail := string(bytes)
	return strings.Contains(detail, "instance.support_contact") && !strings.Contains(detail, "private-contact")
}

func TestInstanceSettingsGetExposesOnlyCustomerEditableProfile(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT key, value, is_secret, updated_at::text").
		WillReturnRows(sqlmock.NewRows([]string{"key", "value", "is_secret", "updated_at"}).
			AddRow("instance.name", "AOY", false, "2026-08-10").
			AddRow("instance.slug", "aoy", false, "2026-08-10").
			AddRow("instance.support_contact", "ทีมหน้าร้าน", false, "2026-08-10").
			AddRow("sml.rest_base_url", "http://172.17.0.1:8200", false, "2026-08-10").
			AddRow("sml.database", "aoy", false, "2026-08-10").
			AddRow("line.notify_channel_access_token", "secret-token", true, "2026-08-10"))

	h := &InstanceSettingsHandler{
		repo: repository.NewAppSettingsRepo(db),
		cfg:  &config.Config{},
		log:  zap.NewNop(),
	}
	router := gin.New()
	router.GET("/api/settings/instance", h.Get)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/settings/instance", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Settings []struct {
			Key string `json:"key"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	got := make([]string, 0, len(response.Settings))
	for _, setting := range response.Settings {
		got = append(got, setting.Key)
	}
	if strings.Join(got, ",") != "instance.name,instance.support_contact" {
		t.Fatalf("settings = %v, want only customer-editable profile", got)
	}
	if strings.Contains(recorder.Body.String(), "172.17.0.1") || strings.Contains(recorder.Body.String(), "secret-token") || strings.Contains(recorder.Body.String(), "instance.slug") {
		t.Fatalf("response leaked infrastructure data: %s", recorder.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestInstanceSettingsUpdateRejectsInfrastructureKeysWithoutPartialWrite(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	h := &InstanceSettingsHandler{
		repo: repository.NewAppSettingsRepo(db),
		cfg:  &config.Config{},
		log:  zap.NewNop(),
	}
	router := gin.New()
	router.PUT("/api/settings/instance", h.Update)
	recorder := httptest.NewRecorder()
	body := `{"settings":{"instance.name":"AOY","sml.database":"demo"}}`
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/api/settings/instance", strings.NewReader(body)))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "setting_not_editable") {
		t.Fatalf("body = %s, want setting_not_editable", recorder.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestInstanceSettingsUpdateSkipsUnchangedValues(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT key, value, is_secret, updated_at::text").
		WillReturnRows(sqlmock.NewRows([]string{"key", "value", "is_secret", "updated_at"}).
			AddRow("instance.name", "AOY", false, "2026-08-10").
			AddRow("instance.support_contact", "ทีมหน้าร้าน", false, "2026-08-10"))

	h := &InstanceSettingsHandler{
		repo: repository.NewAppSettingsRepo(db),
		cfg:  &config.Config{},
		log:  zap.NewNop(),
	}
	router := gin.New()
	router.PUT("/api/settings/instance", h.Update)
	recorder := httptest.NewRecorder()
	body := `{"settings":{"instance.name":"  AOY  ","instance.support_contact":"ทีมหน้าร้าน"}}`
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/api/settings/instance", strings.NewReader(body)))

	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"updated":0`) {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestInstanceSettingsUpdateAuditsOnlyChangedFieldNames(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT key, value, is_secret, updated_at::text").
		WillReturnRows(sqlmock.NewRows([]string{"key", "value", "is_secret", "updated_at"}).
			AddRow("instance.name", "AOY", false, "2026-08-10").
			AddRow("instance.support_contact", "old-contact", false, "2026-08-10"))
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO app_settings").
		WithArgs("instance.support_contact", "private-contact", false, "admin-1").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectExec("INSERT INTO audit_logs").
		WithArgs(
			"instance_profile_updated", "instance_profile", "admin-1", "instance_settings",
			"info", nil, nil, instanceAuditDetailMatcher{},
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	h := &InstanceSettingsHandler{
		repo:  repository.NewAppSettingsRepo(db),
		audit: repository.NewAuditLogRepo(db),
		cfg:   &config.Config{},
		log:   zap.NewNop(),
	}
	router := gin.New()
	router.PUT("/api/settings/instance", func(c *gin.Context) {
		c.Set("user_id", "admin-1")
		h.Update(c)
	})
	recorder := httptest.NewRecorder()
	body := `{"settings":{"instance.support_contact":"private-contact"}}`
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/api/settings/instance", strings.NewReader(body)))

	if recorder.Code != http.StatusOK || strings.Contains(recorder.Body.String(), "private-contact") {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestInstanceConnectionTestIgnoresClientSuppliedURLs(t *testing.T) {
	var safeHits atomic.Int32
	safeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		safeHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer safeServer.Close()

	var overrideHits atomic.Int32
	overrideServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		overrideHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer overrideServer.Close()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT key, value, is_secret, updated_at::text").
		WillReturnRows(sqlmock.NewRows([]string{"key", "value", "is_secret", "updated_at"}))

	h := &InstanceSettingsHandler{
		repo: repository.NewAppSettingsRepo(db),
		cfg: &config.Config{
			ShopeeSMLURL:      safeServer.URL,
			ShopeeSMLGUID:     "test-guid",
			ShopeeSMLDatabase: "aoy",
		},
		log: zap.NewNop(),
	}
	router := gin.New()
	router.POST("/api/settings/instance/test-connection", h.TestConnection)
	recorder := httptest.NewRecorder()
	body := `{"settings":{"sml.rest_base_url":"` + overrideServer.URL + `"}}`
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/settings/instance/test-connection", strings.NewReader(body)))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if overrideHits.Load() != 0 {
		t.Fatalf("client supplied URL was called %d times", overrideHits.Load())
	}
	if safeHits.Load() < 2 {
		t.Fatalf("runtime SML URL hits = %d, want health and tenant checks", safeHits.Load())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestInstanceProfileValidation(t *testing.T) {
	if _, err := normalizeInstanceProfileValue("instance.name", ""); err == nil {
		t.Fatal("empty instance name should be rejected")
	}
	if _, err := normalizeInstanceProfileValue("instance.name", strings.Repeat("a", 121)); err == nil {
		t.Fatal("instance name longer than 120 characters should be rejected")
	}
	if got, err := normalizeInstanceProfileValue("instance.support_contact", "  ทีมหน้าร้าน  "); err != nil || got != "ทีมหน้าร้าน" {
		t.Fatalf("support contact = %q, err=%v", got, err)
	}
	if _, err := normalizeInstanceProfileValue("instance.support_contact", strings.Repeat("a", 201)); err == nil {
		t.Fatal("support contact longer than 200 characters should be rejected")
	}
}

func TestInstanceRestartEndpointIsCompatibilityNoop(t *testing.T) {
	h := &InstanceSettingsHandler{log: zap.NewNop()}
	router := gin.New()
	router.POST("/api/settings/instance/restart", h.Restart)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/settings/instance/restart", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"deprecated":true`) || !strings.Contains(recorder.Body.String(), `"restarted":false`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
	time.Sleep(600 * time.Millisecond)
}

func TestCheckSMLStockURLSkipsEmptyOptionalURL(t *testing.T) {
	result := checkSMLStockURL(&http.Client{Timeout: 10 * time.Millisecond}, "")
	if !result.OK || !result.Skipped {
		t.Fatalf("stock check = %#v, want ok skipped", result)
	}
	if !strings.Contains(result.Detail, "ข้ามการคำนวณต้นทุน") {
		t.Fatalf("detail = %q, want skip explanation", result.Detail)
	}
}

func TestCheckSMLStockURLUsesReadOnlyReachability(t *testing.T) {
	var method string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		w.WriteHeader(http.StatusNotFound)
	}))
	defer upstream.Close()

	result := checkSMLStockURL(upstream.Client(), upstream.URL)
	if !result.OK {
		t.Fatalf("stock check = %#v, want reachable ok", result)
	}
	if method != http.MethodGet {
		t.Fatalf("method = %s, want GET read-only probe", method)
	}
	if !strings.Contains(result.Detail, "ยังไม่ได้ POST processstockrequest") {
		t.Fatalf("detail = %q, want no-post explanation", result.Detail)
	}
}

func TestCheckSMLTenantLookupSeparatesAuthAndTenantErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		want       string
	}{
		{name: "unauthorized guid", statusCode: http.StatusUnauthorized, want: "guid"},
		{name: "forbidden tenant", statusCode: http.StatusForbidden, want: "tenant 'aoy'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/ic/products" {
					t.Fatalf("path = %s, want product lookup", r.URL.Path)
				}
				if got := r.Header.Get("X-Tenant"); got != "aoy" {
					t.Fatalf("X-Tenant = %q, want aoy", got)
				}
				w.WriteHeader(tt.statusCode)
			}))
			defer upstream.Close()

			result := checkSMLTenantLookup(upstream.Client(), upstream.URL, "secret-guid", "aoy")
			if result.OK || !strings.Contains(result.Error, tt.want) {
				t.Fatalf("tenant check = %#v, want error containing %q", result, tt.want)
			}
		})
	}
}

func TestCheckSMLTenantLookupTimeoutExplainsDownstreamLayer(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	client := &http.Client{Timeout: 1 * time.Millisecond}
	result := checkSMLTenantLookup(client, upstream.URL, "secret-guid", "aoy")
	if result.OK {
		t.Fatalf("tenant check = %#v, want timeout failure", result)
	}
	if !strings.Contains(result.Error, "sml-api-byboss ไม่ตอบ") || !strings.Contains(result.Error, "tenant 'aoy'") {
		t.Fatalf("error = %q, want layer-specific timeout", result.Error)
	}
}

func TestCombineSMLDiagnosticsAllowsSkippedStockURL(t *testing.T) {
	result := combineSMLDiagnostics(
		checkResult{OK: true, Layer: "sml_proxy"},
		checkResult{OK: true, Layer: "sml_tenant"},
		checkResult{OK: true, Skipped: true, Layer: "sml_stock_request"},
	)
	if !result.OK {
		t.Fatalf("combined check = %#v, want ok when stock is skipped", result)
	}
}
