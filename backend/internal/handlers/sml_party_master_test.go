package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nexflow/internal/services/sml"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestSMLPartyHandlerBranchesProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/erp/branches" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "secret-guid" {
			t.Fatalf("x-api-key = %q", got)
		}
		if got := r.Header.Get("x-tenant"); got != "sml_test" {
			t.Fatalf("x-tenant = %q", got)
		}
		q := r.URL.Query()
		if q.Get("search") != "B01" || q.Get("page") != "1" || q.Get("size") != "5" {
			t.Fatalf("query = %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":[{"code":"B01","name_1":"สำนักงานใหญ่"}],"meta":{"total":1,"page":1,"size":5}}`))
	}))
	defer upstream.Close()

	h := NewSMLPartyHandler(nil, nil, nil, zap.NewNop())
	h.SetSMLConfig(upstream.URL, "secret-guid", "sml_test")
	r := gin.New()
	r.GET("/branches", h.Branches)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/branches?search=B01&limit=5", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var got struct {
		Data  []SMLMasterItem `json:"data"`
		Total int             `json:"total"`
		Page  int             `json:"page"`
		Size  int             `json:"size"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Total != 1 || got.Page != 1 || got.Size != 5 || len(got.Data) != 1 || got.Data[0].Code != "B01" {
		t.Fatalf("response = %+v", got)
	}
}

func TestSMLPartyHandlerSalesProxyHumanizesSMLDBError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/erp/users" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"success":false,"error":{"code":"users_count_failed","message":"count users failed","details":"context deadline exceeded"}}`))
	}))
	defer upstream.Close()

	h := NewSMLPartyHandler(nil, nil, nil, zap.NewNop())
	h.SetSMLConfig(upstream.URL, "secret-guid", "sml_test")
	r := gin.New()
	r.GET("/sales", h.Sales)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sales", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "เชื่อมต่อฐานข้อมูล SML ของร้านนี้ไม่ได้") || strings.Contains(body, "context deadline exceeded") {
		t.Fatalf("body should be friendly and hide raw timeout: %s", body)
	}
}

func TestSMLPartyHandlerCreateSupplierForwardsExpandedPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ap/suppliers" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("x-tenant"); got != "sml_test" {
			t.Fatalf("x-tenant = %q", got)
		}
		var got sml.SupplierCreateInput
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got.APStatus == nil || *got.APStatus != 1 || got.BranchType == nil || *got.BranchType != 0 {
			t.Fatalf("payload statuses = ap:%v branch:%v", got.APStatus, got.BranchType)
		}
		if got.Code != "VNEW" || got.Name1 != "บริษัท ใหม่" || got.TaxID != "0105559000000" || got.CardID != "1234567890123" {
			t.Fatalf("payload = %+v", got)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"success":true,"data":{"code":"VNEW","name_1":"บริษัท ใหม่","tax_id":"0105559000000","ap_status":1,"branch_type":0,"branch_code":"00000","status":0}}`))
	}))
	defer upstream.Close()

	client := sml.NewPartyClient(sml.PartyConfig{
		BaseURL:    upstream.URL,
		GUID:       "secret-guid",
		Provider:   "p",
		ConfigFile: "c",
		Database:   "sml_test",
	}, zap.NewNop())
	h := NewSMLPartyHandler(nil, client, nil, zap.NewNop())
	r := gin.New()
	r.POST("/suppliers", h.CreateSupplier)

	apStatus := 1
	branchType := 0
	body, _ := json.Marshal(map[string]any{
		"code":        "VNEW",
		"ap_status":   apStatus,
		"name_1":      "บริษัท ใหม่",
		"tax_id":      "0105559000000",
		"branch_type": branchType,
		"card_id":     "1234567890123",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/suppliers", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "บริษัท ใหม่") {
		t.Fatalf("body = %s", w.Body.String())
	}
}

func TestSMLPartyHandlerCreateCustomerForwardsExpandedPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ar/customers" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("x-tenant"); got != "sml_test" {
			t.Fatalf("x-tenant = %q", got)
		}
		var got sml.CustomerCreateInput
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got.ARStatus == nil || *got.ARStatus != 0 || got.BranchType == nil || *got.BranchType != 1 {
			t.Fatalf("payload statuses = ar:%v branch:%v", got.ARStatus, got.BranchType)
		}
		if got.Code != "ARNEW" || got.FirstName != "สมหญิง" || got.LastName != "ใจดี" || got.TaxID != "0105559000000" || got.CardID != "1234567890123" {
			t.Fatalf("payload = %+v", got)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"success":true,"data":{"code":"ARNEW","first_name":"สมหญิง","last_name":"ใจดี","tax_id":"0105559000000","ar_status":0,"branch_type":1,"branch_code":"00002","status":0}}`))
	}))
	defer upstream.Close()

	client := sml.NewPartyClient(sml.PartyConfig{
		BaseURL:    upstream.URL,
		GUID:       "secret-guid",
		Provider:   "p",
		ConfigFile: "c",
		Database:   "sml_test",
	}, zap.NewNop())
	h := NewSMLPartyHandler(nil, client, nil, zap.NewNop())
	r := gin.New()
	r.POST("/customers", h.CreateCustomer)

	arStatus := 0
	branchType := 1
	body, _ := json.Marshal(map[string]any{
		"code":        "ARNEW",
		"ar_status":   arStatus,
		"first_name":  "สมหญิง",
		"last_name":   "ใจดี",
		"tax_id":      "0105559000000",
		"branch_type": branchType,
		"branch_code": "00002",
		"card_id":     "1234567890123",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/customers", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "สมหญิง") {
		t.Fatalf("body = %s", w.Body.String())
	}
}
