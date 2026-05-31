package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"nexflow/internal/models"
)

func TestResolveEndpointUsesExplicitEndpointKeyword(t *testing.T) {
	tests := []struct {
		name         string
		endpoint     string
		wantKind     string
		wantOverride string
	}{
		{
			name:         "saleorder keyword path",
			endpoint:     "/SMLJavaRESTService/v3/api/saleorder",
			wantKind:     "saleorder",
			wantOverride: "/SMLJavaRESTService/v3/api/saleorder",
		},
		{
			name:         "saleinvoice keyword path",
			endpoint:     "/SMLJavaRESTService/saleinvoice/v4",
			wantKind:     "saleinvoice",
			wantOverride: "/SMLJavaRESTService/saleinvoice/v4",
		},
		{
			name:         "purchaseorder keyword url",
			endpoint:     "http://sml.local/SMLJavaRESTService/v3/api/purchaseorder",
			wantKind:     "purchaseorder",
			wantOverride: "http://sml.local/SMLJavaRESTService/v3/api/purchaseorder",
		},
		{
			name:         "legacy sale reserve path now falls back to saleorder",
			endpoint:     "/api/sale_reserve",
			wantKind:     "saleorder",
			wantOverride: "",
		},
		{
			name:         "bare saleinvoice token",
			endpoint:     " saleinvoice ",
			wantKind:     "saleinvoice",
			wantOverride: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := &models.ChannelDefault{Endpoint: tt.endpoint}
			gotKind, gotOverride := resolveEndpoint(def, "line", "sale")
			if gotKind != tt.wantKind || gotOverride != tt.wantOverride {
				t.Fatalf("resolveEndpoint() = (%q, %q), want (%q, %q)", gotKind, gotOverride, tt.wantKind, tt.wantOverride)
			}
		})
	}
}

type testSMLMessageResponse struct {
	message string
}

func (r testSMLMessageResponse) GetMessage() string {
	return r.message
}

func TestSMLSendErrorMessageExplainsEmpty404(t *testing.T) {
	got := smlSendErrorMessage(http.StatusNotFound, testSMLMessageResponse{}, nil)
	want := "HTTP 404 — ไม่พบ endpoint SML ที่ตั้งไว้ กรุณาตรวจ SML REST URL ใน /settings/instance และปลายทางใน /settings/channels"
	if got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}

func TestValidateResolvedSendFieldsRequiresVisibleConfig(t *testing.T) {
	h := &BillHandler{}
	if err := h.validateResolvedSendFields("", "WH", "SH", "09:00", 0, 7); err == nil {
		t.Fatal("missing doc_format should be rejected")
	}
	if err := h.validateResolvedSendFields("PO", "WH", "SH", "09:00", 0, 7); err != nil {
		t.Fatalf("complete visible config rejected: %v", err)
	}
}

func TestExtractSMLERPLogWarningFromNativeResponse(t *testing.T) {
	raw := []byte(`{"success":true,"data":{"doc_no":"PO26050001","log_status":"warning","log_warning":"ไม่พบฐานข้อมูล data1_test_logs"}}`)
	got := extractSMLERPLogWarning(raw)
	if got != "ไม่พบฐานข้อมูล data1_test_logs" {
		t.Fatalf("warning = %q", got)
	}
}

func TestResolveEndpointFallsBackBySourceAndBillType(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		billType string
		wantKind string
	}{
		{name: "shopee excel sale defaults to saleorder", source: "shopee", billType: "sale", wantKind: "saleorder"},
		{name: "shopee email sale defaults to saleorder", source: "shopee_email", billType: "sale", wantKind: "saleorder"},
		{name: "lazada excel sale defaults to saleorder", source: "lazada", billType: "sale", wantKind: "saleorder"},
		{name: "tiktok excel sale defaults to saleorder", source: "tiktok", billType: "sale", wantKind: "saleorder"},
		{name: "shopee shipped defaults to purchaseorder", source: "shopee_shipped", billType: "purchase", wantKind: "purchaseorder"},
		{name: "purchase bill defaults to purchaseorder", source: "email", billType: "purchase", wantKind: "purchaseorder"},
		{name: "line sale defaults to saleorder", source: "line", billType: "sale", wantKind: "saleorder"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotKind, gotOverride := resolveEndpoint(nil, tt.source, tt.billType)
			if gotKind != tt.wantKind || gotOverride != "" {
				t.Fatalf("resolveEndpoint() = (%q, %q), want (%q, \"\")", gotKind, gotOverride, tt.wantKind)
			}
		})
	}
}

func TestMapSourceToChannelMatchesRetryLookupKey(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		{source: "shopee", want: "shopee"},
		{source: "shopee_email", want: "shopee_email"},
		{source: "shopee_shipped", want: "shopee_shipped"},
		{source: "lazada", want: "lazada"},
		{source: "tiktok", want: "tiktok"},
		{source: "email", want: "email"},
		{source: "line", want: "line"},
		{source: "manual", want: "line"},
	}

	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			if got := mapSourceToChannel(tt.source); got != tt.want {
				t.Fatalf("mapSourceToChannel(%q) = %q, want %q", tt.source, got, tt.want)
			}
		})
	}
}

func TestValidateBulkBillIDsGuardsProductionBatch(t *testing.T) {
	validA := "11111111-1111-1111-1111-111111111111"
	validB := "22222222-2222-2222-2222-222222222222"
	if err := validateBulkBillIDs([]string{validA, validB}); err != nil {
		t.Fatalf("valid ids rejected: %v", err)
	}
	if err := validateBulkBillIDs(nil); err == nil {
		t.Fatal("empty batch should be rejected")
	}
	if err := validateBulkBillIDs([]string{"not-a-uuid"}); err == nil {
		t.Fatal("invalid UUID should be rejected")
	}
	if err := validateBulkBillIDs([]string{validA, validA}); err == nil {
		t.Fatal("duplicate bill id should be rejected")
	}
	tooMany := make([]string, 101)
	for i := range tooMany {
		tooMany[i] = "11111111-1111-1111-1111-111111111111"
	}
	if err := validateBulkBillIDs(tooMany); err == nil {
		t.Fatal("batch over 100 should be rejected")
	}
}

func TestAppendRetryOfJobKeepsFilterSnapshot(t *testing.T) {
	raw := json.RawMessage(`{"source":"shopee_shipped","page":3}`)
	out := appendRetryOfJob(raw, "job-123")
	var got map[string]interface{}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal retry filter: %v", err)
	}
	if got["source"] != "shopee_shipped" || got["retry_of_job_id"] != "job-123" {
		t.Fatalf("filter snapshot = %#v", got)
	}
}

func TestBulkJobMatchesSnapshotFilterScopesShopeeShop(t *testing.T) {
	snapshot := json.RawMessage(`{"source":"shopee","shopee_shop_id":"1029622928"}`)
	if !bulkJobMatchesSnapshotFilter(snapshot, "shopee_shop_id", "1029622928") {
		t.Fatal("expected matching shopee_shop_id to resume active job")
	}
	if bulkJobMatchesSnapshotFilter(snapshot, "shopee_shop_id", "999") {
		t.Fatal("different shopee_shop_id should not resume another shop's active job")
	}
	if !bulkJobMatchesSnapshotFilter(snapshot, "shopee_shop_id", "") {
		t.Fatal("empty filter should keep legacy active-job behavior")
	}
	if bulkJobMatchesSnapshotFilter(json.RawMessage(`{"source":"shopee"}`), "shopee_shop_id", "1029622928") {
		t.Fatal("missing shopee_shop_id should not match a shop-specific filter")
	}
}

func TestValidBulkJobStatus(t *testing.T) {
	for _, status := range []string{"queued", "running", "completed", "completed_with_errors", "failed"} {
		if !validBulkJobStatus(status) {
			t.Fatalf("expected %q to be valid", status)
		}
	}
	for _, status := range []string{"", "sent", "pending", "bad"} {
		if validBulkJobStatus(status) {
			t.Fatalf("expected %q to be invalid", status)
		}
	}
}

func TestValidateRemark2AllowsOnlyDocumentStatusCodes(t *testing.T) {
	for _, value := range []string{"", "tax", "notax", "re"} {
		if err := validateRemark2(value); err != nil {
			t.Fatalf("validateRemark2(%q) rejected: %v", value, err)
		}
	}
	for _, value := range []string{"vat", "taxinvoice", " tax "} {
		if err := validateRemark2(value); err == nil {
			t.Fatalf("validateRemark2(%q) should reject invalid code", value)
		}
	}
}

func TestValidateBulkSendPayloadChecksRemark2ForSaleAndPurchase(t *testing.T) {
	if err := validateBulkSendPayload("sale", "saleorder", RetryRequest{Remark2: "vat"}); err == nil {
		t.Fatal("sale bulk payload with invalid remark_2 should be rejected")
	}
	if err := validateBulkSendPayload("purchase", "purchaseorder", RetryRequest{Remark2: "tax"}); err == nil {
		t.Fatal("purchase bulk still requires inquiry_type")
	}
	inquiryType := 1
	if err := validateBulkSendPayload("purchase", "purchaseorder", RetryRequest{
		Remark2:     "tax",
		InquiryType: &inquiryType,
	}); err != nil {
		t.Fatalf("valid purchase bulk payload rejected: %v", err)
	}
}
