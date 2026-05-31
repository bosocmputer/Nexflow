package handlers

import (
	"context"
	"database/sql"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/config"
	"nexflow/internal/repository"
	"nexflow/internal/services/shopeeapi"
)

func TestParseShopeeAPIRangeRejectsInvalidDate(t *testing.T) {
	_, _, err := parseShopeeAPIRange("2026/05/01", "2026-05-02")
	if err == nil {
		t.Fatal("expected invalid date error")
	}
}

func TestParseShopeeAPIRangeRejectsInvertedRange(t *testing.T) {
	_, _, err := parseShopeeAPIRange("2026-05-03", "2026-05-02")
	if err == nil {
		t.Fatal("expected inverted range error")
	}
}

func TestParseShopeeAPIRangeRejectsMoreThan15Days(t *testing.T) {
	_, _, err := parseShopeeAPIRange("2026-05-01", "2026-05-17")
	if err == nil {
		t.Fatal("expected max range error")
	}
}

func TestParseShopeeAPIRangeAccepts15DayWindow(t *testing.T) {
	from, to, err := parseShopeeAPIRange("2026-05-01", "2026-05-15")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if from.Format("2006-01-02") != "2026-05-01" || to.Format("2006-01-02") != "2026-05-15" {
		t.Fatalf("range = %s..%s", from.Format(time.RFC3339), to.Format(time.RFC3339))
	}
}

func TestValidateShopeeAPITimeFieldRejectsPayTime(t *testing.T) {
	if _, err := validateShopeeAPITimeField("pay_time"); err == nil || !strings.Contains(err.Error(), "pay_time") {
		t.Fatalf("expected readable pay_time error, got %v", err)
	}
	if got, err := validateShopeeAPITimeField(""); err != nil || got != "create_time" {
		t.Fatalf("default time field = %q, err=%v", got, err)
	}
	if got, err := validateShopeeAPITimeField("update_time"); err != nil || got != "update_time" {
		t.Fatalf("update time field = %q, err=%v", got, err)
	}
}

func TestShopeeAPIOrderStatusPlanReadyGroupFiltersLocally(t *testing.T) {
	got, err := shopeeAPIOrderStatusPlan("ready_to_bill")
	if err != nil {
		t.Fatalf("ready_to_bill error = %v", err)
	}
	for _, status := range shopeeAPIReadyToBillStatuses {
		if !got.LocalStatuses[status] {
			t.Fatalf("ready local statuses = %v, missing %s", got.LocalStatuses, status)
		}
	}
	if len(got.RequestStatuses) != 0 {
		t.Fatalf("ready request statuses = %v, want fetch-all + local filter", got.RequestStatuses)
	}
	all, err := shopeeAPIOrderStatusPlan("all")
	if err != nil {
		t.Fatalf("all error = %v", err)
	}
	if len(all.RequestStatuses) != 0 || len(all.LocalStatuses) != 0 {
		t.Fatalf("all plan = %+v, want no request/local filter", all)
	}
	shipped, err := shopeeAPIOrderStatusPlan("SHIPPED")
	if err != nil {
		t.Fatalf("SHIPPED error = %v", err)
	}
	if strings.Join(shipped.RequestStatuses, ",") != "SHIPPED" || len(shipped.LocalStatuses) != 0 {
		t.Fatalf("SHIPPED plan = %+v, want request filter only", shipped)
	}
}

func TestShopeeAPIOrdersToPreviewMapsShippingPackageAndNoFalseMismatch(t *testing.T) {
	h := &ShopeeImportHandler{}
	orders, warnings := h.shopeeAPIOrdersToPreview([]shopeeapi.OrderDetail{
		{
			OrderSN:              "260520UDVHA1W7",
			OrderStatus:          "SHIPPED",
			CreateTime:           time.Date(2026, 5, 20, 9, 17, 9, 0, time.Local).Unix(),
			TotalAmount:          257,
			PaymentMethod:        "Cash on Delivery",
			ActualShippingFee:    38,
			ShippingCarrier:      "EMS - Thailand Post",
			COD:                  true,
			PackageList:          []shopeeapi.OrderPackage{{PackageNumber: "OFG232942632252692"}},
			TrackingNumber:       "",
			EstimatedShippingFee: 0,
			ItemList: []shopeeapi.OrderItem{
				{
					ItemID:                 123,
					ItemName:               "สีเพ้นคิ้วเฮนน่า",
					ModelName:              "C.น้ำตาลดำ",
					ModelQuantityPurchased: 1,
					ModelOriginalPrice:     250,
					ModelDiscountedPrice:   239,
				},
			},
		},
	})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	if len(orders) != 1 {
		t.Fatalf("orders = %d", len(orders))
	}
	got := orders[0]
	if got.ShippingAmount != 38 || got.DiscountAmount != 20 || got.AmountMismatch {
		t.Fatalf("amounts shipping=%v discount=%v mismatch=%v", got.ShippingAmount, got.DiscountAmount, got.AmountMismatch)
	}
	if got.TrackingNo != "OFG232942632252692" || got.PackageNumber != "OFG232942632252692" || got.ShippingCarrier != "EMS - Thailand Post" || !got.COD {
		t.Fatalf("logistics tracking=%q package=%q carrier=%q cod=%v", got.TrackingNo, got.PackageNumber, got.ShippingCarrier, got.COD)
	}
	if !got.HasNoSKU || got.NoSKUItemCount != 1 || got.Items[0].SKU != "" || !strings.Contains(got.Items[0].RawName, " / C.น้ำตาลดำ") {
		t.Fatalf("no sku mapping = %+v item=%+v", got, got.Items[0])
	}
}

func TestShopeeAPIReadinessBlocksBadLiveBaseURL(t *testing.T) {
	status := ShopeeAPIStatus{
		Enabled:     true,
		Configured:  true,
		Environment: "live",
		BaseURL:     "https://openplatform.sandbox.test-stable.shopee.sg",
		RedirectURL: "https://example.com/api/shopee-api/callback",
	}
	status.finalizeReadiness(time.Date(2026, 5, 20, 9, 0, 0, 0, time.UTC))
	if status.CanConnect {
		t.Fatal("CanConnect should be false when live env still points at sandbox base URL")
	}
	if !strings.Contains(status.BlockingReason, "environment=live") {
		t.Fatalf("BlockingReason = %q", status.BlockingReason)
	}
}

func TestShopeeAPIReadinessAllowsRefreshRequiredToken(t *testing.T) {
	now := time.Date(2026, 5, 20, 9, 0, 0, 0, time.UTC)
	status := ShopeeAPIStatus{
		Enabled:          true,
		Configured:       true,
		Environment:      "live",
		BaseURL:          "https://partner.shopeemobile.com",
		RedirectURL:      "https://example.com/api/shopee-api/callback",
		Connected:        true,
		ShopID:           123,
		AccessExpiresAt:  now.Add(-time.Minute).Format(time.RFC3339),
		RefreshExpiresAt: now.Add(24 * time.Hour).Format(time.RFC3339),
	}
	status.finalizeReadiness(now)
	if !status.CanFetch {
		t.Fatal("CanFetch should be true because backend can refresh the access token")
	}
	if status.TokenState != "refresh_required" {
		t.Fatalf("TokenState = %q", status.TokenState)
	}
}

func TestShopeeAPIErrorMessageMapsRateLimit(t *testing.T) {
	got := shopeeAPIErrorMessage(nil, "shopee http 429: too many requests")
	if got.Code != "rate_limited" || !got.Retryable {
		t.Fatalf("error view = %+v", got)
	}
}

func TestShopeeAPIConnectionDisplayLabelPrefersShopNameOverDefaultLabel(t *testing.T) {
	conn := &ShopeeAPIConnection{
		ShopID:   264993963,
		ShopName: "ร้านใหม่",
		Label:    "Shop 264993963",
	}
	if got := conn.DisplayLabel(); got != "ร้านใหม่" {
		t.Fatalf("DisplayLabel() = %q", got)
	}

	conn.Label = "ชื่อที่ตั้งเอง"
	if got := conn.DisplayLabel(); got != "ชื่อที่ตั้งเอง" {
		t.Fatalf("custom DisplayLabel() = %q", got)
	}
}

func TestAuditShopeeAPIPreviewWritesTraceableMetadata(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("user_id", "8bdb5d26-86fc-4a58-a6a9-0376a48180a1")
	c.Set("trace_id", "trace-shopee-preview")

	handler := &ShopeeImportHandler{
		auditRepo: repository.NewAuditLogRepo(db),
		logger:    zap.NewNop(),
	}
	conn := &ShopeeAPIConnection{
		ID:       "33333333-3333-3333-3333-333333333333",
		ShopID:   264993963,
		ShopName: "Henna.milkford",
		Label:    "Shop 264993963",
	}
	from := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 21, 23, 59, 59, 0, time.UTC)

	mock.ExpectExec("INSERT INTO audit_logs").
		WithArgs(
			"shopee_api_preview_requested",
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			"shopee_api",
			"info",
			sqlmock.AnyArg(),
			"trace-shopee-preview",
			sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	handler.auditShopeeAPIPreview(
		c,
		conn,
		ShopeeAPIPreviewRequest{OrderStatus: "ready_to_bill"},
		from,
		to,
		"create_time",
		shopeeAPIOrderStatusPlanForStatuses(shopeeAPIReadyToBillStatuses),
		"ok",
		"",
		123*time.Millisecond,
		map[string]interface{}{"returned_orders": 0},
	)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}

func TestConsumeLatestPendingShopeeOAuthStateConsumesNewestState(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	redirectURL := "https://example.com/api/shopee-api/callback"
	handler := &ShopeeImportHandler{
		billRepo: repository.NewBillRepo(db),
		cfg: &config.Config{
			ShopeeOpenAPIEnv:      "live",
			ShopeeOpenAPIRedirect: redirectURL,
		},
		logger: zap.NewNop(),
	}

	mock.ExpectQuery("WITH picked AS").
		WithArgs("live", redirectURL).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "environment", "redirect_url"}).
			AddRow("8bdb5d26-86fc-4a58-a6a9-0376a48180a1", "live", redirectURL))

	got, err := handler.consumeLatestPendingShopeeOAuthState(context.Background())
	if err != nil {
		t.Fatalf("consumeLatestPendingShopeeOAuthState: %v", err)
	}
	if got.UserID != "8bdb5d26-86fc-4a58-a6a9-0376a48180a1" || got.Environment != "live" || got.RedirectURL != redirectURL {
		t.Fatalf("state = %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}

func TestConsumeLatestPendingShopeeOAuthStateRejectsMissingState(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	redirectURL := "https://example.com/api/shopee-api/callback"
	handler := &ShopeeImportHandler{
		billRepo: repository.NewBillRepo(db),
		cfg: &config.Config{
			ShopeeOpenAPIEnv:      "live",
			ShopeeOpenAPIRedirect: redirectURL,
		},
		logger: zap.NewNop(),
	}

	mock.ExpectQuery("WITH picked AS").
		WithArgs("live", redirectURL).
		WillReturnError(sql.ErrNoRows)

	if _, err := handler.consumeLatestPendingShopeeOAuthState(context.Background()); err == nil {
		t.Fatal("expected missing pending state error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}

func TestResolveShopeeAPIConnectionRequiresSelectionWhenMultipleActive(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	handler := &ShopeeImportHandler{
		billRepo: repository.NewBillRepo(db),
		cfg:      &config.Config{ShopeeOpenAPIEnv: "live"},
		logger:   zap.NewNop(),
	}

	now := time.Date(2026, 5, 21, 9, 0, 0, 0, time.UTC)
	mock.ExpectQuery("FROM shopee_api_connections").
		WithArgs("live").
		WillReturnRows(newShopeeConnectionRows().
			AddRow("11111111-1111-1111-1111-111111111111", int64(1001), nil, "Shop A", "Shop A", "access-a", "refresh-a", now.Add(time.Hour), now.Add(24*time.Hour), "live", nil, nil, "", "", "", now, now).
			AddRow("22222222-2222-2222-2222-222222222222", int64(1002), nil, "Shop B", "Shop B", "access-b", "refresh-b", now.Add(time.Hour), now.Add(24*time.Hour), "live", nil, nil, "", "", "", now, now))

	_, err = handler.resolveShopeeAPIConnection(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "เลือกร้าน") {
		t.Fatalf("expected explicit shop selection error, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}

func TestResolveShopeeAPIConnectionByIDReturnsSelectedShop(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	handler := &ShopeeImportHandler{
		billRepo: repository.NewBillRepo(db),
		cfg:      &config.Config{ShopeeOpenAPIEnv: "live"},
		logger:   zap.NewNop(),
	}

	now := time.Date(2026, 5, 21, 9, 0, 0, 0, time.UTC)
	connectionID := "33333333-3333-3333-3333-333333333333"
	mock.ExpectQuery("WHERE id = \\$1::uuid").
		WithArgs(connectionID, "live").
		WillReturnRows(newShopeeConnectionRows().
			AddRow(connectionID, int64(1029622928), int64(555), "semicolon.con", "Semicolon Main", "access", "refresh", now.Add(time.Hour), now.Add(24*time.Hour), "live", nil, now, "ok", "", "", now, now))

	got, err := handler.resolveShopeeAPIConnection(context.Background(), connectionID)
	if err != nil {
		t.Fatalf("resolveShopeeAPIConnection: %v", err)
	}
	if got.ShopID != 1029622928 || got.Label != "Semicolon Main" {
		t.Fatalf("connection = %+v", got)
	}
	if !got.MerchantID.Valid || got.MerchantID.Int64 != 555 {
		t.Fatalf("merchant_id = %+v", got.MerchantID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}

func newShopeeConnectionRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		"shop_id",
		"merchant_id",
		"shop_name",
		"label",
		"access_token",
		"refresh_token",
		"access_expires_at",
		"refresh_expires_at",
		"environment",
		"disabled_at",
		"last_sync_at",
		"last_sync_status",
		"last_sync_error",
		"last_error_code",
		"connected_at",
		"updated_at",
	})
}
