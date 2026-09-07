package handlers

import (
	"context"
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
	"nexflow/internal/models"
	"nexflow/internal/repository"
	lineservice "nexflow/internal/services/line"
)

func TestLineNotificationStatusReturnsOnlyReadinessCounts(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT COUNT\\(\\*\\), COUNT\\(\\*\\) FILTER \\(WHERE enabled = TRUE\\).*FROM line_oa_accounts").
		WillReturnRows(sqlmock.NewRows([]string{"total", "enabled"}).AddRow(2, 1))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\), COUNT\\(\\*\\) FILTER \\(WHERE r.enabled = TRUE AND oa.enabled = TRUE\\).*FROM line_notification_recipients r.*LEFT JOIN line_oa_accounts oa").
		WillReturnRows(sqlmock.NewRows([]string{"total", "enabled"}).AddRow(3, 2))

	h := &LineNotificationHandler{
		lineOARepo: repository.NewLineOAAccountRepo(db),
		repo:       repository.NewLineNotificationRepo(db),
		logger:     zap.NewNop(),
	}
	router := gin.New()
	router.GET("/api/settings/line-notifications/status", h.Status)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/settings/line-notifications/status", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["ready"] != true || response["enabled_sender_count"] != float64(1) || response["enabled_recipient_count"] != float64(2) {
		t.Fatalf("unexpected status response: %#v", response)
	}
	for _, forbidden := range []string{"channel_access_token", "destination_id", "deliveries", "candidates"} {
		if strings.Contains(recorder.Body.String(), forbidden) {
			t.Fatalf("status response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLineNotificationSampleMessageMatchesRichShopeeFallback(t *testing.T) {
	h := &LineNotificationHandler{cfg: &config.Config{PublicBaseURL: "https://animal-galvanize-tameness.ngrok-free.dev"}}

	msg := h.sampleMessage()

	for _, want := range []string{
		"มีออเดอร์ Shopee ใหม่",
		"Henna.milkford",
		"260621NDVGSKMA",
		"ยอดรวม: 245.00",
		"Credit Card/Debit Card",
		"ยอดสุทธิตาม Shopee escrow: 263.00",
		"ส่วนต่างจากยอดลูกค้าชำระ: -18.00",
		"ค่าส่งประมาณการ: 35.00",
		"EMS - Thailand Post",
		"OFG235736492235190",
		"21/06/2026 17:21",
		"เปิดใน Nexflow: https://animal-galvanize-tameness.ngrok-free.dev/shopee-operations?order=260621NDVGSKMA",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("sample message missing %q:\n%s", want, msg)
		}
	}
	for _, leak := range []string{"buyer-secret", "secret-name", "0999999999", "full_address", "buyer_username"} {
		if strings.Contains(msg, leak) {
			t.Fatalf("sample message leaked %q:\n%s", leak, msg)
		}
	}
	if strings.Contains(msg, "฿") {
		t.Fatalf("sample message should use LINE money format without currency symbol:\n%s", msg)
	}
}

func TestLineNotificationNextStepSampleMessage(t *testing.T) {
	h := &LineNotificationHandler{cfg: &config.Config{PublicBaseURL: "https://nexflow-aoy.nextstep-soft.com"}}

	msg := h.sampleMessageForSource("nextstep_marketplace")

	for _, want := range []string{
		"มีออเดอร์ NextStep Marketplace ใหม่",
		"MQT20260709-SAMPLE",
		"วันที่: 09/07/2026 14:30",
		"สถานะ: รอดำเนินการ",
		"ยอดรวม: 1,280.00",
		"https://nexflow-aoy.nextstep-soft.com/nextstep-marketplace?from_date=2026-07-09",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("nextstep sample missing %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "฿") {
		t.Fatalf("nextstep sample should use LINE money format without currency symbol:\n%s", msg)
	}
}

func TestLineNotificationDestinationFromWebhookSource(t *testing.T) {
	tests := []struct {
		name     string
		source   lineSource
		wantType string
		wantID   string
	}{
		{
			name:     "user",
			source:   lineSource{Type: "user", UserID: "U111"},
			wantType: "user",
			wantID:   "U111",
		},
		{
			name:     "group uses group id",
			source:   lineSource{Type: "group", UserID: "U111", GroupID: "C222"},
			wantType: "group",
			wantID:   "C222",
		},
		{
			name:     "room uses room id",
			source:   lineSource{Type: "room", UserID: "U111", RoomID: "R333"},
			wantType: "room",
			wantID:   "R333",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotID := lineNotificationDestination(tt.source)
			if gotType != tt.wantType || gotID != tt.wantID {
				t.Fatalf("lineNotificationDestination = (%q, %q), want (%q, %q)", gotType, gotID, tt.wantType, tt.wantID)
			}
		})
	}
}

func TestLineNotificationQuotaKeepsOAResultsIndependent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	now := time.Now()
	rows := sqlmock.NewRows([]string{
		"id", "name", "channel_secret", "channel_access_token", "bot_user_id",
		"admin_user_id", "greeting", "enabled", "mark_as_read_enabled", "created_at", "updated_at",
	}).
		AddRow("oa-ok", "OA พร้อมใช้", "secret-one", "token-one", "U1", "", "", true, false, now, now).
		AddRow("oa-bad", "OA ต้องตรวจสอบ", "secret-two", "token-two", "U2", "", "", true, false, now, now)
	mock.ExpectQuery("SELECT .* FROM line_oa_accounts ORDER BY name").WillReturnRows(rows)

	limit, remaining := int64(300), int64(282)
	h := &LineNotificationHandler{
		lineOARepo: repository.NewLineOAAccountRepo(db),
		logger:     zap.NewNop(),
		quotaCache: lineservice.NewQuotaCache(),
		quotaFetch: func(_ context.Context, account *models.LineOAAccount) (lineservice.MessageQuota, error) {
			if account.ID == "oa-bad" {
				return lineservice.MessageQuota{}, &lineservice.QuotaAPIError{Code: "line_token_invalid", HTTPStatus: http.StatusUnauthorized}
			}
			return lineservice.MessageQuota{Type: "limited", Limit: &limit, Used: 18, Remaining: &remaining}, nil
		},
	}
	router := gin.New()
	router.GET("/api/settings/line-notifications/quota", h.Quota)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/settings/line-notifications/quota", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Data []lineOAQuotaDTO `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Data) != 2 {
		t.Fatalf("data=%#v", response.Data)
	}
	byID := map[string]lineOAQuotaDTO{}
	for _, item := range response.Data {
		byID[item.LineOAID] = item
	}
	if got := byID["oa-ok"]; got.Status != "ok" || got.Used == nil || *got.Used != 18 || got.Remaining == nil || *got.Remaining != 282 {
		t.Fatalf("ok item=%#v", got)
	}
	if got := byID["oa-bad"]; got.Status != "error" || got.ErrorCode != "line_token_invalid" {
		t.Fatalf("bad item=%#v", got)
	}
	for _, forbidden := range []string{"token-one", "token-two", "secret-one", "secret-two", "channel_access_token", "channel_secret"} {
		if strings.Contains(recorder.Body.String(), forbidden) {
			t.Fatalf("quota response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLineNotificationQuotaValidatesRefreshAndTarget(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	h := &LineNotificationHandler{lineOARepo: repository.NewLineOAAccountRepo(db), logger: zap.NewNop()}
	router := gin.New()
	router.GET("/quota", h.Quota)

	invalid := httptest.NewRecorder()
	router.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/quota?refresh=yes", nil))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid refresh status=%d", invalid.Code)
	}

	now := time.Now()
	mock.ExpectQuery("SELECT .* FROM line_oa_accounts ORDER BY name").WillReturnRows(
		sqlmock.NewRows([]string{
			"id", "name", "channel_secret", "channel_access_token", "bot_user_id",
			"admin_user_id", "greeting", "enabled", "mark_as_read_enabled", "created_at", "updated_at",
		}).AddRow("oa-1", "OA", "secret", "token", "U1", "", "", true, false, now, now),
	)
	missing := httptest.NewRecorder()
	router.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/quota?oa_id=not-found", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing status=%d body=%s", missing.Code, missing.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
