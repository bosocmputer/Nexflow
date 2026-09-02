package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/repository"
)

func TestChannelDefaultPreviewIsReadOnlyAndResolvesProfileFields(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	h := NewChannelDefaultsHandler(repository.NewChannelDefaultRepo(db), nil, false, "shadow", zap.NewNop())
	body := validChannelDefaultJSON(map[string]any{
		"remark":   "  {{channel}} | {{order_ref}} | {{bill_no}}  ",
		"remark_2": "ส่งจาก Nexflow",
		"preview_context": map[string]any{
			"channel": "Shopee API", "order_ref": "ORDER-DEMO", "bill_no": "BF-DEMO",
		},
	})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/settings/channel-defaults/preview", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	h.Preview(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Resolved struct {
			Remark string `json:"remark"`
		} `json:"resolved"`
		ProfileMode    string         `json:"profile_mode"`
		ProfileVersion string         `json:"profile_version"`
		RouteSignature string         `json:"route_signature"`
		SystemFields   map[string]any `json:"system_fields"`
		Payload        map[string]any `json:"payload"`
		Missing        []string       `json:"missing_prerequisites"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Resolved.Remark != "{{channel}} | {{order_ref}} | {{bill_no}}" {
		t.Fatalf("literal remark=%q", response.Resolved.Remark)
	}
	if response.ProfileMode != "shadow" || response.RouteSignature == "" || len(response.Missing) != 0 {
		t.Fatalf("unexpected preview: %+v", response)
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"missing_prerequisites":[]`)) {
		t.Fatalf("empty prerequisites must be a JSON array, body=%s", recorder.Body.String())
	}
	if response.ProfileVersion != "sml-document-v1" || response.Payload["document_profile_version"] != "" {
		t.Fatalf("shadow must validate V1 without opting the wire payload in: %+v", response)
	}
	if response.SystemFields["creator_code"] != "BILLFLOW" || response.SystemFields["currency_code"] != "THB" {
		t.Fatalf("unexpected system fields: %+v", response.SystemFields)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("preview must not query or write the database: %v", err)
	}
}

func TestChannelDefaultUpsertReturnsVersionConflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT .* FROM channel_defaults").
		WithArgs("shopee_realtime", "sale").
		WillReturnRows(sqlmock.NewRows(channelDefaultHandlerColumns()).AddRow(
			"shopee_realtime", "sale", "AR-1", "", "", "", "", "SI", "/api/v1/ic/sale-invoices",
			"BF-INV", "@YYMM####", "", "", "", "", false, "", "", "", "", "", "", "", "",
			"AB-1", "001", 1, float64(7), -1, "", "", int64(1), nil, time.Now(),
		))
	mock.ExpectQuery("UPDATE channel_defaults").WillReturnRows(sqlmock.NewRows(channelDefaultHandlerColumns()))

	h := NewChannelDefaultsHandler(repository.NewChannelDefaultRepo(db), nil, false, "off", zap.NewNop())
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("user_id", "user-1")
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/settings/channel-defaults", bytes.NewReader(validChannelDefaultJSON(nil)))
	ctx.Request.Header.Set("Content-Type", "application/json")

	h.Upsert(ctx)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["code"] != "config_version_conflict" {
		t.Fatalf("response=%v", response)
	}
}

func TestChannelDefaultUpsertRejectsControlCharacterInRemarkBeforeDatabaseWrite(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	h := NewChannelDefaultsHandler(repository.NewChannelDefaultRepo(db), nil, false, "off", zap.NewNop())

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/settings/channel-defaults", bytes.NewReader(validChannelDefaultJSON(map[string]any{
		"remark": "บรรทัดแรก\nบรรทัดสอง",
	})))
	ctx.Request.Header.Set("Content-Type", "application/json")
	h.Upsert(ctx)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unsafe value must be rejected before SQL: %v", err)
	}
}

func validChannelDefaultJSON(overrides map[string]any) []byte {
	body := map[string]any{
		"channel": "shopee_realtime", "bill_type": "sale", "party_code": "AR-1",
		"doc_format_code": "SI", "endpoint": "/api/v1/ic/sale-invoices",
		"doc_prefix": "BF-INV", "doc_running_format": "@YYMM####",
		"wh_code": "AB-1", "shelf_code": "001", "vat_type": 1, "vat_rate": 7,
		"inquiry_type": -1, "expected_config_version": 1,
	}
	for key, value := range overrides {
		body[key] = value
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return encoded
}

func channelDefaultHandlerColumns() []string {
	return []string{
		"channel", "bill_type", "party_code", "party_name", "party_phone", "party_address", "party_tax_id",
		"doc_format_code", "endpoint", "doc_prefix", "doc_running_format", "branch_code", "sale_code", "unit_code", "doc_time",
		"shipping_item_enabled", "shipping_item_code", "shipping_item_unit_code", "passbook_code", "passbook_name",
		"bank_code", "bank_branch", "expense_code", "expense_name", "wh_code", "shelf_code", "vat_type", "vat_rate",
		"inquiry_type", "remark", "remark_2", "config_version", "updated_by", "updated_at",
	}
}
