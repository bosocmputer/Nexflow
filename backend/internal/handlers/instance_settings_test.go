package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNormalizeInstanceURL(t *testing.T) {
	got, msg := normalizeInstanceURL(" http://192.168.2.109:8200/ ")
	if msg != "" {
		t.Fatalf("normalizeInstanceURL() error = %q, want none", msg)
	}
	if got != "http://192.168.2.109:8200" {
		t.Fatalf("url = %q, want normalized base URL", got)
	}
}

func TestNormalizeInstanceURLRejectsInvalidScheme(t *testing.T) {
	if _, msg := normalizeInstanceURL("ftp://192.168.2.109:8200"); msg == "" {
		t.Fatal("normalizeInstanceURL() returned empty error, want invalid scheme error")
	}
}

func TestNormalizeInstanceSettingDatabaseName(t *testing.T) {
	def := settingDef{Key: "sml.database"}
	got, msg := normalizeInstanceSetting(def, " SML1_2026 ")
	if msg != "" {
		t.Fatalf("normalizeInstanceSetting() error = %q, want none", msg)
	}
	if got != "SML1_2026" {
		t.Fatalf("database = %q, want trimmed name", got)
	}
}

func TestNormalizeInstanceSettingSMLProvider(t *testing.T) {
	def := settingDef{Key: "sml.provider"}
	got, msg := normalizeInstanceSetting(def, " DATA ")
	if msg != "" {
		t.Fatalf("normalizeInstanceSetting() error = %q, want none", msg)
	}
	if got != "DATA" {
		t.Fatalf("provider = %q, want DATA", got)
	}
	if _, msg := normalizeInstanceSetting(def, "DATA;DROP"); msg == "" {
		t.Fatal("normalizeInstanceSetting() accepted unsafe provider")
	}
}

func TestNormalizeInstanceSettingSMLConfigFile(t *testing.T) {
	def := settingDef{Key: "sml.config_file"}
	got, msg := normalizeInstanceSetting(def, " SMLConfigDATA.xml ")
	if msg != "" {
		t.Fatalf("normalizeInstanceSetting() error = %q, want none", msg)
	}
	if got != "SMLConfigDATA.xml" {
		t.Fatalf("config file = %q, want SMLConfigDATA.xml", got)
	}
	if _, msg := normalizeInstanceSetting(def, "../SMLConfigDATA.xml"); msg == "" {
		t.Fatal("normalizeInstanceSetting() accepted unsafe config path")
	}
}

func TestNormalizeInstanceSettingRejectsUnsafeDatabaseName(t *testing.T) {
	def := settingDef{Key: "sml.database"}
	if _, msg := normalizeInstanceSetting(def, "SML1_2026;DROP"); msg == "" {
		t.Fatal("normalizeInstanceSetting() returned empty error, want database validation error")
	}
}

func TestNormalizeInstanceSettingAutoConfirmThreshold(t *testing.T) {
	def := settingDef{Key: "automation.auto_confirm_threshold"}
	got, msg := normalizeInstanceSetting(def, "0.85")
	if msg != "" {
		t.Fatalf("normalizeInstanceSetting() error = %q, want none", msg)
	}
	if got != "0.85" {
		t.Fatalf("threshold = %q, want 0.85", got)
	}
	if _, msg := normalizeInstanceSetting(def, "1.5"); msg == "" {
		t.Fatal("normalizeInstanceSetting() accepted threshold > 1")
	}
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
