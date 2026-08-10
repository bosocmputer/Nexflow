package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/config"
	"nexflow/internal/repository"
)

func TestLineNotificationStatusReturnsOnlyReadinessCounts(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT COUNT\\(\\*\\), COUNT\\(\\*\\) FILTER \\(WHERE enabled = TRUE\\).*FROM line_oa_accounts").
		WillReturnRows(sqlmock.NewRows([]string{"total", "enabled"}).AddRow(2, 1))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\), COUNT\\(\\*\\) FILTER \\(WHERE enabled = TRUE\\).*FROM line_notification_recipients").
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
