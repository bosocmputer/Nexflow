package sml

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
)

func TestPartyClientCreateCustomer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/ar/customers" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-Tenant"); got != "sml1_2026" {
			t.Fatalf("X-Tenant = %q", got)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"success":true,"data":{"code":"ARNEW","name_1":"ลูกค้าใหม่","status":0}}`))
	}))
	defer srv.Close()

	client := NewPartyClient(PartyConfig{BaseURL: srv.URL, GUID: "smlx", Database: "sml1_2026"}, zap.NewNop())
	status, party, err := client.CreateCustomer(context.Background(), CustomerCreateInput{
		Code:  " ARNEW ",
		Name1: " ลูกค้าใหม่ ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusCreated {
		t.Fatalf("status = %d", status)
	}
	if party.Code != "ARNEW" || party.Name != "ลูกค้าใหม่" {
		t.Fatalf("party = %+v", party)
	}
}

func TestPartyClientCreateCustomerSendsExpandedPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/ar/customers" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var got CustomerCreateInput
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got.ARStatus == nil || *got.ARStatus != 0 || got.BranchType == nil || *got.BranchType != 1 {
			t.Fatalf("payload statuses = ar:%v branch:%v", got.ARStatus, got.BranchType)
		}
		if got.Code != "ARNEW" || got.FirstName != "สมหญิง" || got.LastName != "ใจดี" || got.BranchCode != "00002" || got.CardID != "1234567890123" {
			t.Fatalf("payload = %+v", got)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"success":true,"data":{"code":"ARNEW","first_name":"สมหญิง","last_name":"ใจดี","ar_status":0,"branch_type":1,"branch_code":"00002","status":0}}`))
	}))
	defer srv.Close()

	arStatus := 0
	branchType := 1
	client := NewPartyClient(PartyConfig{BaseURL: srv.URL, GUID: "smlx", Database: "sml1_2026"}, zap.NewNop())
	status, party, err := client.CreateCustomer(context.Background(), CustomerCreateInput{
		Code:       " ARNEW ",
		ARStatus:   &arStatus,
		FirstName:  " สมหญิง ",
		LastName:   " ใจดี ",
		BranchType: &branchType,
		BranchCode: " 00002 ",
		CardID:     " 1234567890123 ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusCreated {
		t.Fatalf("status = %d", status)
	}
	if party.Code != "ARNEW" || party.Name != "สมหญิง ใจดี" || party.ARStatus != 0 || party.BranchCode != "00002" {
		t.Fatalf("party = %+v", party)
	}
}

func TestPartyClientCreateSupplierDuplicate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"success":false,"error":{"code":"duplicate_supplier_code","message":"supplier code 'V001' already exists"}}`))
	}))
	defer srv.Close()

	client := NewPartyClient(PartyConfig{BaseURL: srv.URL, GUID: "smlx", Database: "sml1_2026"}, zap.NewNop())
	apStatus := 1
	branchType := 0
	status, party, err := client.CreateSupplier(context.Background(), SupplierCreateInput{
		Code:       "V001",
		APStatus:   &apStatus,
		Name1:      "ผู้ขายเดิม",
		BranchType: &branchType,
	})
	if err == nil {
		t.Fatal("expected duplicate error")
	}
	if status != http.StatusConflict {
		t.Fatalf("status = %d", status)
	}
	if party != nil {
		t.Fatalf("party = %+v, want nil", party)
	}
}

func TestPartyClientCreateSupplierSendsExpandedPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/ap/suppliers" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var got SupplierCreateInput
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got.APStatus == nil || *got.APStatus != 0 || got.BranchType == nil || *got.BranchType != 1 {
			t.Fatalf("payload statuses = ap:%v branch:%v", got.APStatus, got.BranchType)
		}
		if got.Code != "VNEW" || got.Firstname != "สมชาย" || got.Lastname != "ใจดี" || got.BranchCode != "00002" || got.CardID != "1234567890123" {
			t.Fatalf("payload = %+v", got)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"success":true,"data":{"code":"VNEW","firstname":"สมชาย","lastname":"ใจดี","ap_status":0,"branch_type":1,"branch_code":"00002","status":0}}`))
	}))
	defer srv.Close()

	apStatus := 0
	branchType := 1
	client := NewPartyClient(PartyConfig{BaseURL: srv.URL, GUID: "smlx", Database: "sml1_2026"}, zap.NewNop())
	status, party, err := client.CreateSupplier(context.Background(), SupplierCreateInput{
		Code:       " VNEW ",
		APStatus:   &apStatus,
		Firstname:  " สมชาย ",
		Lastname:   " ใจดี ",
		BranchType: &branchType,
		BranchCode: " 00002 ",
		CardID:     " 1234567890123 ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusCreated {
		t.Fatalf("status = %d", status)
	}
	if party.Code != "VNEW" || party.Name != "สมชาย ใจดี" || party.APStatus != 0 || party.BranchCode != "00002" {
		t.Fatalf("party = %+v", party)
	}
}
