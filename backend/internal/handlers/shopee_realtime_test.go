package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"

	"nexflow/internal/config"
	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/shopeeapi"
	"nexflow/internal/services/sml"
	"nexflow/internal/services/smlprofile"
)

func TestParseShopeePushPayloadAllowsShopLevelAuthorizationEvent(t *testing.T) {
	event, err := parseShopeePushPayload([]byte(`{"code":12,"shop_id":264993963,"timestamp":1779180000}`))
	if err != nil {
		t.Fatalf("parseShopeePushPayload() error = %v", err)
	}
	if event.ShopID != 264993963 || event.OrderSN != "" || event.PushName != "open_api_authorization_expiry" {
		t.Fatalf("event = %+v", event)
	}
}

func TestShopeeRealtimeBillIdentityRequiresScopedRealtimeOrder(t *testing.T) {
	bill := &models.Bill{Source: "shopee", RawData: json.RawMessage(`{
		"flow":"shopee_realtime","shopee_shop_id":"264993963","shopee_order_id":"ORDER-1"
	}`)}
	shopID, orderSN, ok := shopeeRealtimeBillIdentity(bill)
	if !ok || shopID != 264993963 || orderSN != "ORDER-1" {
		t.Fatalf("unexpected identity: shop=%d order=%q ok=%v", shopID, orderSN, ok)
	}
	bill.RawData = json.RawMessage(`{"flow":"shopee_excel","shopee_shop_id":"264993963","shopee_order_id":"ORDER-1"}`)
	if _, _, ok := shopeeRealtimeBillIdentity(bill); ok {
		t.Fatal("Shopee Excel must not claim the realtime ERP send lease")
	}
}

func TestAutoSMLReviewClassification(t *testing.T) {
	for _, message := range []string{
		"ยังไม่มีข้อมูลสินค้า SML กรุณารีเฟรชสินค้า SML ก่อน",
		"กรุณาตั้งค่ารายการค่าขนส่ง",
		"ยอดจาก Marketplace มีส่วนต่าง",
		"สินค้าชุดยังไม่พร้อม",
	} {
		if !autoSMLUserActionError(message) {
			t.Fatalf("expected actionable error for %q", message)
		}
	}
	if autoSMLUserActionError("SML request timeout") {
		t.Fatal("timeout must remain retryable")
	}
	if got := autoSMLReviewCode("กรุณาตั้งค่ารายการค่าขนส่ง"); got != "shipping_config_missing" {
		t.Fatalf("unexpected review code %q", got)
	}
}

func TestShopeeRealtimeRouteSignatureChangesForDocumentRoutingFields(t *testing.T) {
	cfg := ShopeeConfigRequest{
		Endpoint: "saleinvoice", DocFormat: "SI", CustCode: "AR-001",
		WHCode: "AB-1", ShelfCode: "001", VATType: 1, VATRate: 7,
	}
	base := &models.ChannelDefault{
		Endpoint: "saleinvoice", DocFormatCode: "SI", PartyCode: "AR-001",
		DocPrefix: "BF-INV", DocRunningFormat: "YYMM####", WHCode: "AB-1",
		ShelfCode: "001", VATType: 1, VATRate: 7,
	}
	want := shopeeRealtimeRouteSignature(cfg, base)

	changed := *base
	changed.DocPrefix = "BF-SI"
	if got := shopeeRealtimeRouteSignature(cfg, &changed); got == want {
		t.Fatal("route signature must change when document prefix changes")
	}

	changed = *base
	changed.ShippingItemCode = "AH-SHIP"
	changed.ShippingItemEnabled = true
	if got := shopeeRealtimeRouteSignature(cfg, &changed); got == want {
		t.Fatal("route signature must change when shipping configuration changes")
	}

	changed = *base
	changed.DocTime = "23:59"
	cfg.DocTime = "12:00"
	if got := shopeeRealtimeRouteSignature(cfg, &changed); got != want {
		t.Fatal("dynamic document time must not change route signature")
	}
}

func TestShopeeRealtimeRouteMissingFields(t *testing.T) {
	cfg := ShopeeConfigRequest{
		Endpoint: "saleinvoice", DocFormat: "SI", CustCode: "AR-001",
		WHCode: "AB-1", ShelfCode: "001", VATType: 1, VATRate: 7,
	}
	def := &models.ChannelDefault{DocPrefix: "BF-INV", DocRunningFormat: "YYMM####"}
	if missing := shopeeRealtimeRouteMissingFields(cfg, def); len(missing) != 0 {
		t.Fatalf("ready route missing = %v", missing)
	}

	def.ShippingItemEnabled = true
	if missing := shopeeRealtimeRouteMissingFields(cfg, def); !slices.Equal(missing, []string{"สินค้าค่าจัดส่ง", "หน่วยค่าจัดส่ง"}) {
		t.Fatalf("shipping route missing = %v", missing)
	}
	def.ShippingItemCode = "AH-0061"
	def.ShippingItemUnitCode = "ชิ้น"
	if missing := shopeeRealtimeRouteMissingFields(cfg, def); len(missing) != 0 {
		t.Fatalf("complete shipping route missing = %v", missing)
	}
}

func TestAutoSMLDocumentTimeUsesBangkok(t *testing.T) {
	startedAt := time.Date(2026, 8, 24, 4, 35, 42, 0, time.UTC)
	if got := autoSMLDocumentTime(startedAt); got != "11:35" {
		t.Fatalf("autoSMLDocumentTime() = %q, want 11:35", got)
	}
}

func TestClassifyAutoSMLJobTrigger(t *testing.T) {
	transitionAt := time.Date(2026, 8, 26, 13, 30, 0, 0, time.UTC)
	tests := []struct {
		name     string
		job      models.ShopeeAutoSMLJob
		status   string
		decision autoSMLTriggerDecision
		code     string
	}{
		{
			name:   "ready trigger may continue after packing",
			job:    models.ShopeeAutoSMLJob{TriggerStatusSnapshot: models.ShopeeAutoSMLTriggerReadyToShip, TriggerTransitionAt: &transitionAt},
			status: "PROCESSED", decision: autoSMLTriggerProceed,
		},
		{
			name:   "processed trigger waits for processed",
			job:    models.ShopeeAutoSMLJob{TriggerStatusSnapshot: models.ShopeeAutoSMLTriggerProcessed, TriggerTransitionAt: &transitionAt},
			status: "READY_TO_SHIP", decision: autoSMLTriggerReview, code: "trigger_status_not_reached",
		},
		{
			name:   "missing transition proof fails closed",
			job:    models.ShopeeAutoSMLJob{TriggerStatusSnapshot: models.ShopeeAutoSMLTriggerReadyToShip},
			status: "READY_TO_SHIP", decision: autoSMLTriggerReview, code: "missing_trigger_transition",
		},
		{
			name:   "cancellation stops the job",
			job:    models.ShopeeAutoSMLJob{TriggerStatusSnapshot: models.ShopeeAutoSMLTriggerReadyToShip, TriggerTransitionAt: &transitionAt},
			status: "IN_CANCEL", decision: autoSMLTriggerCancel, code: "status_changed",
		},
		{
			name:   "unknown lifecycle fails closed",
			job:    models.ShopeeAutoSMLJob{TriggerStatusSnapshot: models.ShopeeAutoSMLTriggerReadyToShip, TriggerTransitionAt: &transitionAt},
			status: "FUTURE_STATUS", decision: autoSMLTriggerReview, code: "unknown_order_status",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, code, _ := classifyAutoSMLJobTrigger(tt.job, tt.status)
			if decision != tt.decision || code != tt.code {
				t.Fatalf("decision/code = %s/%s, want %s/%s", decision, code, tt.decision, tt.code)
			}
		})
	}
}

func TestAutoSMLTriggerTransitionFallbackRequiresExactObservedTransition(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	h := &ShopeeRealtimeHandler{repo: repository.NewShopeeRealtimeRepo(db)}
	updateUnix := int64(1787731200)
	before := &models.ShopeeOrderSnapshot{ShopID: 264993963, OrderSN: "ORDER-1", OrderStatus: "READY_TO_SHIP"}
	processed := &models.ShopeeOrderSnapshot{ShopID: 264993963, OrderSN: "ORDER-1", OrderStatus: "PROCESSED"}

	mock.ExpectQuery("WITH requested\\(shop_id,order_sn\\) AS").
		WithArgs(int64(264993963), "ORDER-1", models.ShopeeAutoSMLTriggerProcessed).
		WillReturnRows(sqlmock.NewRows([]string{"shop_id", "order_sn", "transition_at"}))
	got, err := h.autoSMLTriggerTransitionAt(t.Context(), models.ShopeeAutoSMLTriggerProcessed,
		shopeeapi.OrderDetail{UpdateTime: updateUnix}, before, processed)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Unix() != updateUnix {
		t.Fatalf("transition = %v, want exact observed get_order_detail update_time", got)
	}

	shipped := &models.ShopeeOrderSnapshot{ShopID: 264993963, OrderSN: "ORDER-1", OrderStatus: "SHIPPED"}
	mock.ExpectQuery("WITH requested\\(shop_id,order_sn\\) AS").
		WithArgs(int64(264993963), "ORDER-1", models.ShopeeAutoSMLTriggerProcessed).
		WillReturnRows(sqlmock.NewRows([]string{"shop_id", "order_sn", "transition_at"}))
	got, err = h.autoSMLTriggerTransitionAt(t.Context(), models.ShopeeAutoSMLTriggerProcessed,
		shopeeapi.OrderDetail{UpdateTime: updateUnix + 60}, processed, shipped)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("later state without exact PROCESSED push evidence returned %v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRequiredAutoSMLSettingConfirmation(t *testing.T) {
	tests := []struct {
		name          string
		beforeEnabled bool
		beforeTrigger string
		afterEnabled  bool
		afterTrigger  string
		want          string
	}{
		{"enable with selected trigger", false, "READY_TO_SHIP", true, "PROCESSED", "ENABLE_AUTO_SML"},
		{"change trigger while enabled", true, "READY_TO_SHIP", true, "PROCESSED", "UPDATE_AUTO_SML_TRIGGER"},
		{"no-op", true, "READY_TO_SHIP", true, "READY_TO_SHIP", ""},
		{"disable", true, "PROCESSED", false, "PROCESSED", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := models.ShopeeAutoSMLSetting{Enabled: tt.beforeEnabled, TriggerStatus: tt.beforeTrigger}
			if got := requiredAutoSMLSettingConfirmation(before, tt.afterEnabled, tt.afterTrigger); got != tt.want {
				t.Fatalf("confirmation = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAutoSMLNotificationItemsPreferShopeeSnapshotAndOmitPII(t *testing.T) {
	snap := &models.ShopeeOrderSnapshot{
		ItemCount: 1,
		RawDetail: json.RawMessage(`{
			"buyer_username":"buyer-secret",
			"recipient_address":{"name":"secret-name","phone":"0999999999"},
			"item_list":[{
				"item_name":"สีเพ้นคิ้วเฮนน่า",
				"model_name":"3.น้ำตาลดำ",
				"model_quantity_purchased":2
			}]
		}`),
	}
	bill := &models.Bill{Items: []models.BillItem{{RawName: "fallback bill item", Qty: 99}}}

	items, total := autoSMLNotificationItems(snap, bill)
	if total != 1 || len(items) != 1 {
		t.Fatalf("items=%+v total=%d, want one Shopee item", items, total)
	}
	if items[0].Name != "สีเพ้นคิ้วเฮนน่า" || items[0].Variant != "3.น้ำตาลดำ" || items[0].Qty != 2 {
		t.Fatalf("unexpected Shopee item: %+v", items[0])
	}
	buf, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("marshal notification items: %v", err)
	}
	for _, leak := range []string{"buyer-secret", "secret-name", "0999999999", "fallback bill item"} {
		if strings.Contains(string(buf), leak) {
			t.Fatalf("notification items leaked %q: %s", leak, buf)
		}
	}
}

func TestAutoSMLNotificationItemsFallbackToBillAndSkipShipping(t *testing.T) {
	bill := &models.Bill{Items: []models.BillItem{
		{RawName: "สินค้า A / สีแดง", Qty: 3},
		{RawName: "ค่าจัดส่ง Shopee", SourceSKU: models.ShopeeShippingSourceSKU, Qty: 1},
	}}

	items, total := autoSMLNotificationItems(nil, bill)
	if total != 1 || len(items) != 1 || items[0].Name != "สินค้า A / สีแดง" || items[0].Qty != 3 {
		t.Fatalf("fallback items=%+v total=%d", items, total)
	}
}

func TestAutoSMLNotificationShippingUsesFinalBillItemOnly(t *testing.T) {
	itemCode := "AH-0061"
	unitCode := "ชิ้น"
	price := 15.0
	gross := 15.0
	bill := &models.Bill{Items: []models.BillItem{
		{RawName: "สินค้า A", Qty: 1},
		{
			RawName: "ค่าจัดส่ง Shopee", SourceSKU: models.ShopeeShippingSourceSKU,
			ItemCode: &itemCode, UnitCode: &unitCode, Qty: 1, Price: &price, GrossAmount: &gross,
		},
	}}

	lines := autoSMLNotificationShippingLines(bill)
	if len(lines) != 1 {
		t.Fatalf("shipping lines = %+v, want one", lines)
	}
	if lines[0].Amount != 15 || lines[0].ItemCode != "AH-0061" || lines[0].Qty != 1 || lines[0].UnitCode != "ชิ้น" {
		t.Fatalf("unexpected shipping line: %+v", lines[0])
	}
	if got := autoSMLNotificationShippingLines(&models.Bill{Items: []models.BillItem{{RawName: "สินค้า A", Qty: 1}}}); len(got) != 0 {
		t.Fatalf("bill without a shipping item returned %+v", got)
	}
}

func TestValidateShopeeRealtimeAutoDefaults(t *testing.T) {
	base := models.ChannelDefaultUpsert{
		Channel: "shopee_realtime", BillType: "sale", Endpoint: "/api/v1/ic/sale-invoices",
		DocFormatCode: "BF-INV", DocPrefix: "BF-INV", DocRunningFormat: "YYMM####",
		PartyCode: "AR-001", WHCode: "AB-2", ShelfCode: "002", VATType: 0, VATRate: 7,
	}
	if err := validateShopeeRealtimeAutoDefaults(base); err != nil {
		t.Fatalf("complete defaults rejected: %v", err)
	}
	base.PartyCode = ""
	if err := validateShopeeRealtimeAutoDefaults(base); err == nil || !strings.Contains(err.Error(), "ลูกค้า SML") {
		t.Fatalf("missing party error = %v", err)
	}
}

func TestValidateShopeeRealtimeCancelDefaults(t *testing.T) {
	tests := []struct {
		name    string
		in      models.ChannelDefaultUpsert
		wantErr string
	}{
		{
			name: "sale invoice cancellation",
			in: models.ChannelDefaultUpsert{
				Channel: "shopee_realtime_cancel", BillType: "sale",
				Endpoint:      "/api/v1/ic/sale-invoices/:doc_no/void",
				DocFormatCode: "SIC", DocPrefix: "SIC", DocRunningFormat: "YYMM####",
			},
		},
		{
			name: "credit note",
			in: models.ChannelDefaultUpsert{
				Channel: "shopee_realtime_cancel", BillType: "sale",
				Endpoint:      "/api/v1/ic/sale-invoices/:doc_no/cancel",
				DocFormatCode: "CN", DocPrefix: "CN", DocRunningFormat: "YYMM####",
			},
		},
		{
			name: "unsupported endpoint",
			in: models.ChannelDefaultUpsert{
				Channel: "shopee_realtime_cancel", BillType: "sale",
				Endpoint: "/api/v1/ic/sale-invoices", DocFormatCode: "CN",
				DocPrefix: "CN", DocRunningFormat: "YYMM####",
			},
			wantErr: "ปลายทางยกเลิก SML",
		},
		{
			name: "missing doc format",
			in: models.ChannelDefaultUpsert{
				Channel: "shopee_realtime_cancel", BillType: "sale",
				Endpoint:  "/api/v1/ic/sale-invoices/:doc_no/cancel",
				DocPrefix: "CN", DocRunningFormat: "YYMM####",
			},
			wantErr: "รูปแบบเอกสาร",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateShopeeRealtimeCancelDefaults(tt.in)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("complete defaults rejected: %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestResolveShopeeSMLCancellationRoute(t *testing.T) {
	voidRoute, err := resolveShopeeSMLCancellationRoute("/api/v1/ic/sale-invoices/:doc_no/void")
	if err != nil {
		t.Fatal(err)
	}
	if voidRoute.Kind != sml.SaleInvoiceCancelKindVoid || voidRoute.TransFlag != 45 || voidRoute.DocNoRoute != "saleinvoicecancel" {
		t.Fatalf("void route = %+v", voidRoute)
	}
	creditRoute, err := resolveShopeeSMLCancellationRoute("/api/v1/ic/sale-invoices/:doc_no/cancel")
	if err != nil {
		t.Fatal(err)
	}
	if creditRoute.Kind != sml.SaleInvoiceCancelKindCreditNote || creditRoute.TransFlag != 48 || creditRoute.DocNoRoute != "creditnote" {
		t.Fatalf("credit route = %+v", creditRoute)
	}
	if _, err := resolveShopeeSMLCancellationRoute("/api/v1/ic/sale-invoices"); err == nil {
		t.Fatal("unsupported endpoint should fail closed")
	}
}

func TestApplyDocumentOverridesMatchesShopeeRealtimeAndManualSend(t *testing.T) {
	branch, sale, unit, docTime := "ENV-BR", "ENV-SALE", "ENV-UNIT", "09:00"
	def := &models.ChannelDefault{
		BranchCode: "BR-1", SaleCode: "SALE-1", UnitCode: "ชิ้น", DocTime: "14:30",
	}
	applyDocumentOverrides(def, &branch, &sale, &unit, &docTime)
	if branch != "BR-1" || sale != "SALE-1" || unit != "ชิ้น" || docTime != "14:30" {
		t.Fatalf("resolved document config = %q/%q/%q/%q", branch, sale, unit, docTime)
	}
}

func TestClassifySMLSendFailure(t *testing.T) {
	tests := []struct {
		name   string
		status int
		err    error
		want   string
	}{
		{name: "timeout", status: http.StatusRequestTimeout, want: "transient"},
		{name: "rate limit", status: http.StatusTooManyRequests, want: "transient"},
		{name: "server error", status: http.StatusBadGateway, want: "transient"},
		{name: "network error", err: errors.New("connection reset"), want: "transient"},
		{name: "validation", status: http.StatusBadRequest, want: "user_action"},
		{name: "business rejection body", status: http.StatusOK, want: "user_action"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifySMLSendFailure(tt.status, tt.err); got != tt.want {
				t.Fatalf("classifySMLSendFailure(%d, %v) = %q, want %q", tt.status, tt.err, got, tt.want)
			}
		})
	}
}

func TestShopeeShippingActionsDisabledByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	h := &ShopeeRealtimeHandler{cfg: &config.Config{}}

	if !h.shippingActionsDisabled(c) {
		t.Fatal("expected shipping action guard to block when flag is off")
	}
	if rec.Code != 403 {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "shipping_actions_disabled") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestShopeeShippingActionsGuardAllowsWhenEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	h := &ShopeeRealtimeHandler{cfg: &config.Config{ShopeeShippingActionsEnabled: true}}

	if h.shippingActionsDisabled(c) {
		t.Fatal("expected shipping action guard to allow when flag is on")
	}
	if rec.Code != 200 {
		t.Fatalf("status = %d, want untouched recorder default 200", rec.Code)
	}
}

func TestShopeeOrderIsCancelledIncludesInCancel(t *testing.T) {
	for _, status := range []string{"CANCELLED", "IN_CANCEL"} {
		if !shopeeOrderIsCancelled(status) {
			t.Fatalf("expected %s to be treated as cancelled", status)
		}
	}
	if shopeeOrderIsCancelled("READY_TO_SHIP") {
		t.Fatal("READY_TO_SHIP should not be treated as cancelled")
	}
}

func TestShouldEnqueueAutoSMLCancellationRequiresFinalCancellationTransition(t *testing.T) {
	billID := "11111111-1111-1111-1111-111111111111"
	base := &models.ShopeeOrderSnapshot{
		ShopID: 264993963, OrderSN: "ORDER-1", OrderStatus: "SHIPPED",
		BillID: &billID, SMLDocNo: "BF-INV26080055",
	}
	cancelled := *base
	cancelled.OrderStatus = "CANCELLED"

	if !shouldEnqueueAutoSMLCancellation(true, base, &cancelled) {
		t.Fatal("final CANCELLED transition after SML must enqueue")
	}
	inCancel := cancelled
	inCancel.OrderStatus = "IN_CANCEL"
	if shouldEnqueueAutoSMLCancellation(true, base, &inCancel) {
		t.Fatal("IN_CANCEL is not final and must not reverse SML")
	}
	if shouldEnqueueAutoSMLCancellation(false, base, &cancelled) {
		t.Fatal("disabled automation must not enqueue")
	}
	if shouldEnqueueAutoSMLCancellation(true, nil, &cancelled) {
		t.Fatal("historical first-seen cancelled orders must not be backfilled automatically")
	}
	alreadyCancelled := cancelled
	if shouldEnqueueAutoSMLCancellation(true, &alreadyCancelled, &cancelled) {
		t.Fatal("duplicate CANCELLED reconcile must not enqueue again")
	}
	missingDoc := cancelled
	missingDoc.SMLDocNo = ""
	if shouldEnqueueAutoSMLCancellation(true, base, &missingDoc) {
		t.Fatal("orders without a sent SML document must not enqueue")
	}
	missingBill := cancelled
	missingBill.BillID = nil
	if shouldEnqueueAutoSMLCancellation(true, base, &missingBill) {
		t.Fatal("orders without a linked Nexflow bill must not enqueue")
	}
}

func TestClassifySMLCancellationFailure(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		err    error
		want   string
	}{
		{name: "network", err: errors.New("connection reset"), want: "transient"},
		{name: "timeout", status: http.StatusRequestTimeout, want: "transient"},
		{name: "rate limit", status: http.StatusTooManyRequests, want: "transient"},
		{name: "gateway", status: http.StatusBadGateway, want: "transient"},
		{name: "validation", status: http.StatusBadRequest, want: "blocked"},
		{name: "conflict", status: http.StatusConflict, want: "blocked"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifySMLCancellationFailure(tt.status, tt.err); got != tt.want {
				t.Fatalf("classifySMLCancellationFailure(%d, %v) = %q, want %q", tt.status, tt.err, got, tt.want)
			}
		})
	}
}

func TestAutoSMLCancellationRetryRestoresImmutablePayloadAndRouteKind(t *testing.T) {
	h := &ShopeeRealtimeHandler{}
	job := models.ShopeeSMLCancellation{
		CancelSMLDocNo: "SIC26080002",
		RouteEndpoint:  "/api/v1/ic/sale-invoices/:doc_no/void",
		RequestPayload: json.RawMessage(`{"doc_no":"SIC26080002","doc_date":"2026-08-26","doc_time":"11:20","doc_format_code":"SIC"}`),
	}
	req, err := h.smlCancellationRequestForAttempt(context.Background(), &shopeeSMLCancelDocumentContext{}, job)
	if err != nil {
		t.Fatal(err)
	}
	if req.Kind != sml.SaleInvoiceCancelKindVoid || req.DocNo != "SIC26080002" || req.DocTime != "11:20" {
		t.Fatalf("restored request = %+v", req)
	}

	job.CancelSMLDocNo = "SIC26080003"
	if _, err := h.smlCancellationRequestForAttempt(context.Background(), &shopeeSMLCancelDocumentContext{}, job); err == nil {
		t.Fatal("mismatched persisted doc_no must fail closed")
	}
}

func TestShopeeSMLCancellationRouteSignatureCoversImmutableRoutingFields(t *testing.T) {
	base := &models.ChannelDefault{
		Endpoint: "/api/v1/ic/sale-invoices/:doc_no/cancel", DocFormatCode: "CN",
		DocPrefix: "CN", DocRunningFormat: "YYMM####",
	}
	want := shopeeSMLCancellationRouteSignature(base)
	for _, mutate := range []func(*models.ChannelDefault){
		func(v *models.ChannelDefault) { v.Endpoint = "/api/v1/ic/sale-invoices/:doc_no/void" },
		func(v *models.ChannelDefault) { v.DocFormatCode = "CN-ONLINE" },
		func(v *models.ChannelDefault) { v.DocPrefix = "SIC" },
		func(v *models.ChannelDefault) { v.DocRunningFormat = "YYYYMM####" },
		func(v *models.ChannelDefault) { v.Remark = "ยกเลิก {{order_ref}}" },
		func(v *models.ChannelDefault) { v.ConfigVersion = 2 },
	} {
		changed := *base
		mutate(&changed)
		if got := shopeeSMLCancellationRouteSignature(&changed); got == want {
			t.Fatalf("route signature did not change for %+v", changed)
		}
	}
	if shopeeSMLCancellationRouteSignature(base, smlprofile.ModeActive) == want {
		t.Fatal("profile mode must change cancellation route signature")
	}
}

func TestSaleInvoiceCancelRequestUsesSavedRemarkAsLiteralText(t *testing.T) {
	h := &ShopeeRealtimeHandler{}
	cancelCtx := &shopeeSMLCancelDocumentContext{
		Snapshot:  &models.ShopeeOrderSnapshot{OrderSN: "ORDER-1"},
		RouteDef:  &models.ChannelDefault{DocFormatCode: "CN", Remark: "{{channel}}|{{order_ref}}|{{bill_no}}"},
		RouteMeta: shopeeSMLCancellationRoute{Kind: sml.SaleInvoiceCancelKindCreditNote},
	}
	req, err := h.saleInvoiceCancelRequest(cancelCtx, "CN-1", time.Date(2026, 9, 2, 10, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if req.Remark != "{{channel}}|{{order_ref}}|{{bill_no}}" || req.UserRequest != "NEXFLOW" {
		t.Fatalf("request=%+v", req)
	}
}

func TestSMLCancellationItemCodesAreBoundedToUniqueMappedItems(t *testing.T) {
	ah1, ah2 := " AH-0002 ", "AH-0001"
	blank := " "
	bill := &models.Bill{Items: []models.BillItem{
		{ItemCode: &ah1}, {ItemCode: &ah2}, {ItemCode: &ah1}, {ItemCode: &blank}, {ItemCode: nil},
	}}
	got := smlCancellationItemCodes(bill)
	want := []string{"AH-0001", "AH-0002"}
	if !slices.Equal(got, want) {
		t.Fatalf("smlCancellationItemCodes() = %v, want %v", got, want)
	}
}

func TestShopeePushCodeMappingMatchesConsole(t *testing.T) {
	cases := []struct {
		code            int
		name            string
		shopLevel       bool
		requiresOrderSN bool
	}{
		{1, "shop_authorization_push", true, false},
		{2, "shop_authorization_canceled_push", true, false},
		{3, "order_status_push", false, true},
		{4, "order_trackingno_push", false, true},
		{12, "open_api_authorization_expiry", true, false},
		{15, "shipping_document_status_push", false, true},
		{23, "booking_status_push", false, true},
		{24, "booking_trackingno_push", false, true},
		{25, "booking_shipping_document_status_push", false, true},
		{30, "package_fulfillment_status_push", false, true},
		{47, "package_info_push", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shopeePushName(tc.code); got != tc.name {
				t.Fatalf("shopeePushName(%d) = %q", tc.code, got)
			}
			if got := isShopeeShopLevelPush(tc.code); got != tc.shopLevel {
				t.Fatalf("isShopeeShopLevelPush(%d) = %v", tc.code, got)
			}
			if got := shopeePushCodeMeta[tc.code].RequiresOrderSN; got != tc.requiresOrderSN {
				t.Fatalf("RequiresOrderSN(%d) = %v", tc.code, got)
			}
		})
	}
}

func TestParseShopeePushPayloadStoresUnknownCodeWithoutOrderSN(t *testing.T) {
	event, err := parseShopeePushPayload([]byte(`{"code":999,"shop_id":264993963,"timestamp":1779180000}`))
	if err != nil {
		t.Fatalf("parseShopeePushPayload() error = %v", err)
	}
	if event.PushName != "unknown" || event.OrderSN != "" {
		t.Fatalf("event = %+v", event)
	}
}

func TestParseShopeePushPayloadBuildsEnabledOrderEvents(t *testing.T) {
	for code, name := range map[int]string{
		3:  "order_status_push",
		4:  "order_trackingno_push",
		15: "shipping_document_status_push",
		23: "booking_status_push",
		24: "booking_trackingno_push",
		25: "booking_shipping_document_status_push",
		30: "package_fulfillment_status_push",
		47: "package_info_push",
	} {
		t.Run(name, func(t *testing.T) {
			event, err := parseShopeePushPayload([]byte(fmt.Sprintf(`{
				"code":%d,
				"shop_id":264993963,
				"timestamp":1779180000,
				"data":{"order_sn":"250520ABC","status":"READY_TO_SHIP","update_time":1779170000}
			}`, code)))
			if err != nil {
				t.Fatalf("parseShopeePushPayload() error = %v", err)
			}
			if event.PushName != name || event.OrderSN != "250520ABC" {
				t.Fatalf("event = %+v", event)
			}
		})
	}
}

func TestVerifyShopeeWebhookAcceptsAuthorizationHMAC(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"code":3,"shop_id":264993963,"data":{"order_sn":"250520ABC"}}`)
	publicURL := "https://animal-galvanize-tameness.ngrok-free.dev/webhook/shopee"
	req := httptest.NewRequest("POST", publicURL, nil)
	req.Header.Set("Authorization", hmacSHA256Hex("push-secret", []byte(publicURL+"|"+string(body))))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	h := &ShopeeRealtimeHandler{cfg: &config.Config{
		PublicBaseURL:               "https://animal-galvanize-tameness.ngrok-free.dev",
		ShopeeRealtimeWebhookSecret: "push-secret",
	}}
	if !h.verifyWebhook(c, body) {
		t.Fatalf("expected valid Authorization HMAC, status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestVerifyShopeeWebhookAcceptsQueryTokenFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"code":3,"shop_id":264993963,"data":{"order_sn":"250520ABC"}}`)
	req := httptest.NewRequest("POST", "https://animal-galvanize-tameness.ngrok-free.dev/webhook/shopee?token=push-secret", nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	h := &ShopeeRealtimeHandler{cfg: &config.Config{ShopeeRealtimeWebhookSecret: "push-secret"}}
	if !h.verifyWebhook(c, body) {
		t.Fatalf("expected valid query token, status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestShopeePushRawPayloadForStorageWrapsNonJSON(t *testing.T) {
	payload := shopeePushRawPayloadForStorage([]byte("not-json"))
	var decoded map[string]interface{}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("payload should be valid JSON: %v", err)
	}
	if decoded["_raw_sha256"] == "" || decoded["_raw_size"].(float64) != 8 {
		t.Fatalf("decoded = %+v", decoded)
	}
}

func TestParseShopeePushPayloadRequiresOrderSNForOrderEvent(t *testing.T) {
	_, err := parseShopeePushPayload([]byte(`{"code":3,"shop_id":264993963,"data":{"status":"READY_TO_SHIP"}}`))
	if err == nil {
		t.Fatal("expected missing order_sn error")
	}
	if !strings.Contains(err.Error(), "order_sn") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestParseShopeePushPayloadBuildsOrderStatusEvent(t *testing.T) {
	event, err := parseShopeePushPayload([]byte(`{
		"code":3,
		"shop_id":264993963,
		"timestamp":1779180000,
		"data":{"ordersn":"250520ABC","status":"READY_TO_SHIP","update_time":1779170000}
	}`))
	if err != nil {
		t.Fatalf("parseShopeePushPayload() error = %v", err)
	}
	if event.OrderSN != "250520ABC" || event.Status != "READY_TO_SHIP" || event.UpdateTime.Unix() != 1779170000 {
		t.Fatalf("event = %+v", event)
	}
	if event.DedupeKey == "" {
		t.Fatal("expected dedupe key")
	}
}

func TestShouldNotifyShopeeNewOrderCoversCODUnpaidToReady(t *testing.T) {
	tests := []struct {
		name     string
		before   *models.ShopeeOrderSnapshot
		after    *models.ShopeeOrderSnapshot
		suppress bool
		want     bool
	}{
		{
			name:  "new ready order not suppressed",
			after: &models.ShopeeOrderSnapshot{ERPStatus: "pending"},
			want:  true,
		},
		{
			name:     "baseline ready order suppressed",
			after:    &models.ShopeeOrderSnapshot{ERPStatus: "pending"},
			suppress: true,
			want:     false,
		},
		{
			name:   "cod unpaid later ready should notify",
			before: &models.ShopeeOrderSnapshot{OrderStatus: "UNPAID", ERPStatus: "blocked"},
			after:  &models.ShopeeOrderSnapshot{OrderStatus: "READY_TO_SHIP", ERPStatus: "pending"},
			want:   true,
		},
		{
			name:   "already ready does not notify again",
			before: &models.ShopeeOrderSnapshot{ERPStatus: "pending"},
			after:  &models.ShopeeOrderSnapshot{ERPStatus: "pending_erp"},
			want:   false,
		},
		{
			name:   "blocked stays blocked",
			before: &models.ShopeeOrderSnapshot{OrderStatus: "UNPAID", ERPStatus: "blocked"},
			after:  &models.ShopeeOrderSnapshot{OrderStatus: "UNPAID", ERPStatus: "blocked"},
			want:   false,
		},
		{
			name:  "new blocked order waits",
			after: &models.ShopeeOrderSnapshot{OrderStatus: "UNPAID", ERPStatus: "blocked"},
			want:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldNotifyShopeeNewOrder(tt.before, tt.after, tt.suppress); got != tt.want {
				t.Fatalf("shouldNotifyShopeeNewOrder() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShopeePaymentBreakdownEligibility(t *testing.T) {
	if shopeeDetailEligibleForPaymentBreakdown(shopeeapi.OrderDetail{PayTime: 1782037297}, &models.ShopeeOrderSnapshot{OrderStatus: "UNPAID"}) != true {
		t.Fatal("paid order detail should be eligible even if status still unpaid")
	}
	if shopeeDetailEligibleForPaymentBreakdown(shopeeapi.OrderDetail{}, &models.ShopeeOrderSnapshot{OrderStatus: "UNPAID"}) {
		t.Fatal("unpaid order without pay_time should not be eligible")
	}
	if !shopeeSnapshotEligibleForPaymentBreakdown(&models.ShopeeOrderSnapshot{
		OrderStatus: "UNPAID",
		RawDetail:   []byte(`{"pay_time":1782037297}`),
	}) {
		t.Fatal("snapshot with pay_time should be eligible")
	}
}

func TestPaymentBreakdownCacheAndBackoff(t *testing.T) {
	now := time.Now()
	if !paymentBreakdownCacheFresh(&models.ShopeeOrderPaymentSnapshot{Status: "ready", LastSyncedAt: &now}) {
		t.Fatal("fresh ready snapshot should be cache hit")
	}
	old := now.Add(-10 * time.Minute)
	if paymentBreakdownCacheFresh(&models.ShopeeOrderPaymentSnapshot{Status: "ready", LastSyncedAt: &old}) {
		t.Fatal("old snapshot should not be cache hit")
	}
	if got := paymentBreakdownRetryBackoff(1); got != time.Minute {
		t.Fatalf("first backoff = %s", got)
	}
	if got := paymentBreakdownRetryBackoff(2); got != 5*time.Minute {
		t.Fatalf("second backoff = %s", got)
	}
	if got := paymentBreakdownRetryBackoff(3); got != 30*time.Minute {
		t.Fatalf("third backoff = %s", got)
	}
}

func TestNormalizeShopeeRealtimeOrderRefsDedupeAndLimit(t *testing.T) {
	refs, err := normalizeShopeeRealtimeOrderRefs([]shopeeRealtimeOrderRef{
		{ShopID: 264993963, OrderSN: "ORDER1"},
		{ShopID: 264993963, OrderSN: " ORDER1 "},
		{ShopID: 264993963, OrderSN: "ORDER2"},
	})
	if err != nil {
		t.Fatalf("normalizeShopeeRealtimeOrderRefs() error = %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("len(refs) = %d, want 2", len(refs))
	}

	tooMany := make([]shopeeRealtimeOrderRef, shopeeRealtimeBulkCreateLimit+1)
	for i := range tooMany {
		tooMany[i] = shopeeRealtimeOrderRef{ShopID: 264993963, OrderSN: fmt.Sprintf("ORDER-%d", i)}
	}
	if _, err := normalizeShopeeRealtimeOrderRefs(tooMany); err == nil {
		t.Fatal("expected limit error")
	}
}

func TestBulkCreateDisabledReason(t *testing.T) {
	billID := "5c776587-36af-43bb-9643-fd56ffe1f77c"
	tests := []struct {
		name       string
		snap       *models.ShopeeOrderSnapshot
		routeReady bool
		want       string
	}{
		{
			name:       "ready pending",
			routeReady: true,
			snap:       &models.ShopeeOrderSnapshot{OrderStatus: "READY_TO_SHIP", ERPStatus: "pending"},
			want:       "",
		},
		{
			name:       "failed without bill can retry create",
			routeReady: true,
			snap:       &models.ShopeeOrderSnapshot{OrderStatus: "READY_TO_SHIP", ERPStatus: "failed"},
			want:       "",
		},
		{
			name:       "route missing",
			routeReady: false,
			snap:       &models.ShopeeOrderSnapshot{OrderStatus: "READY_TO_SHIP", ERPStatus: "pending"},
			want:       "ตั้งค่า",
		},
		{
			name:       "existing bill skipped",
			routeReady: true,
			snap:       &models.ShopeeOrderSnapshot{OrderStatus: "READY_TO_SHIP", ERPStatus: "pending_erp", BillID: &billID},
			want:       "สร้างเอกสารแล้ว",
		},
		{
			name:       "unpaid skipped",
			routeReady: true,
			snap:       &models.ShopeeOrderSnapshot{OrderStatus: "UNPAID", ERPStatus: "blocked"},
			want:       "ยังไม่ชำระเงิน",
		},
		{
			name:       "cancelled skipped",
			routeReady: true,
			snap:       &models.ShopeeOrderSnapshot{OrderStatus: "CANCELLED", ERPStatus: "cancelled"},
			want:       "ยกเลิก",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bulkCreateDisabledReason(tt.snap, tt.routeReady, "กรุณาตั้งค่า route")
			if tt.want == "" && got != "" {
				t.Fatalf("got %q, want empty", got)
			}
			if tt.want != "" && !strings.Contains(got, tt.want) {
				t.Fatalf("got %q, want contains %q", got, tt.want)
			}
		})
	}
}

func TestValidateShippingSelectionAcceptsNumericPickupIDs(t *testing.T) {
	params := shippingParameterFixture(t)
	err := validateShippingSelection(params, shippingOrderRequest{
		Pickup: map[string]interface{}{
			"address_id":     float64(12345),
			"pickup_time_id": float64(67890),
		},
	})
	if err != nil {
		t.Fatalf("validateShippingSelection() error = %v", err)
	}
}

func TestValidateShippingSelectionBlocksMultipleMethods(t *testing.T) {
	params := shippingParameterFixture(t)
	err := validateShippingSelection(params, shippingOrderRequest{
		Pickup:  map[string]interface{}{"address_id": float64(12345), "pickup_time_id": float64(67890)},
		Dropoff: map[string]interface{}{"branch_id": "BR-1"},
	})
	if err == nil || !strings.Contains(err.Error(), "1 วิธี") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateShippingSelectionBlocksUnknownDropoffBranch(t *testing.T) {
	params := shippingParameterFixture(t)
	err := validateShippingSelection(params, shippingOrderRequest{
		Dropoff: map[string]interface{}{"branch_id": "BR-MISSING"},
	})
	if err == nil || !strings.Contains(err.Error(), "dropoff branch") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateShippingSelectionBlocksMissingRequiredFields(t *testing.T) {
	params := shippingParameterFixture(t)
	err := validateShippingSelection(params, shippingOrderRequest{
		Pickup: map[string]interface{}{"address_id": float64(12345)},
	})
	if err == nil || !strings.Contains(err.Error(), "pickup_time_id") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateDropoffShippingGuardBlocksWhenAdvancedFlagOff(t *testing.T) {
	params := shippingParameterFixture(t)
	reason, msg := validateDropoffShippingGuard(params, shippingOrderRequest{
		Dropoff: map[string]interface{}{"branch_id": "BR-1"},
	}, false)
	if reason != "advanced_dropoff_disabled" {
		t.Fatalf("reason = %q, msg = %q", reason, msg)
	}
	if !strings.Contains(msg, "Seller Center") {
		t.Fatalf("message should point user to Seller Center: %q", msg)
	}
}

func TestValidateDropoffShippingGuardBlocksInsufficientBranchDetail(t *testing.T) {
	params := shippingParameterFixture(t)
	params.Response.Dropoff.BranchList[0].Name = ""
	params.Response.Dropoff.BranchList[0].Address = "73 หมู่ 9"
	reason, msg := validateDropoffShippingGuard(params, shippingOrderRequest{
		Dropoff: map[string]interface{}{"branch_id": "BR-1"},
	}, true)
	if reason != "insufficient_dropoff_branch_detail" {
		t.Fatalf("reason = %q, msg = %q", reason, msg)
	}
}

func TestValidateDropoffShippingGuardBlocksMissingLocationSignal(t *testing.T) {
	params := shippingParameterFixture(t)
	params.Response.Dropoff.BranchList[0].Latitude = shopeeapi.LogisticsID{}
	params.Response.Dropoff.BranchList[0].Longitude = shopeeapi.LogisticsID{}
	params.Response.Dropoff.BranchList[0].Distance = shopeeapi.LogisticsID{}
	reason, msg := validateDropoffShippingGuard(params, shippingOrderRequest{
		Dropoff: map[string]interface{}{"branch_id": "BR-1"},
	}, true)
	if reason != "insufficient_dropoff_branch_detail" {
		t.Fatalf("reason = %q, msg = %q", reason, msg)
	}
}

func TestValidateDropoffShippingGuardAllowsUsableDropoffWhenAdvancedFlagOn(t *testing.T) {
	params := shippingParameterFixture(t)
	reason, msg := validateDropoffShippingGuard(params, shippingOrderRequest{
		Dropoff: map[string]interface{}{"branch_id": "BR-1"},
	}, true)
	if reason != "" || msg != "" {
		t.Fatalf("reason = %q, msg = %q", reason, msg)
	}
}

func TestValidateDropoffShippingGuardDoesNotBlockPickup(t *testing.T) {
	params := shippingParameterFixture(t)
	reason, msg := validateDropoffShippingGuard(params, shippingOrderRequest{
		Pickup: map[string]interface{}{"address_id": float64(12345), "pickup_time_id": float64(67890)},
	}, false)
	if reason != "" || msg != "" {
		t.Fatalf("reason = %q, msg = %q", reason, msg)
	}
}

func TestApplyShippingReconcileToDetailAddsTrackingAndLogistics(t *testing.T) {
	detail := shopeeapi.OrderDetail{
		OrderSN:     "2606023B20RECS",
		OrderStatus: "PROCESSED",
		PackageList: []shopeeapi.OrderPackage{
			{PackageNumber: "OFG234114953270153"},
		},
	}
	tracking := &shopeeapi.TrackingNumberResponse{}
	tracking.Response.TrackingNumber = "WB306659324TH"
	info := &shopeeapi.TrackingInfoResponse{}
	info.Response.LogisticsStatus = "LOGISTICS_REQUEST_CREATED"
	info.Response.PackageNumber = "OFG234114953270153"

	applyShippingReconcileToDetail(&detail, "OFG234114953270153", tracking, info)

	if detail.TrackingNumber != "WB306659324TH" {
		t.Fatalf("detail tracking_number = %q", detail.TrackingNumber)
	}
	if detail.PackageList[0].TrackingNumber != "WB306659324TH" {
		t.Fatalf("package tracking_number = %q", detail.PackageList[0].TrackingNumber)
	}
	if detail.PackageList[0].LogisticsStatus != "LOGISTICS_REQUEST_CREATED" {
		t.Fatalf("package logistics_status = %q", detail.PackageList[0].LogisticsStatus)
	}
}

func TestShippingTrackingViewMarksSellerCenterShipmentAsExternal(t *testing.T) {
	view := shippingTrackingView(&models.ShopeeOrderSnapshot{
		OrderSN:         "2606023B20RECS",
		OrderStatus:     "PROCESSED",
		LogisticsStatus: "LOGISTICS_REQUEST_CREATED",
		TrackingNumber:  "WB306659324TH",
	})
	if got, _ := view["external_shipment"].(bool); !got {
		t.Fatalf("external_shipment = %v, want true", view["external_shipment"])
	}

	view = shippingTrackingView(&models.ShopeeOrderSnapshot{
		OrderSN:          "2606023B20RECS",
		OrderStatus:      "PROCESSED",
		LogisticsStatus:  "LOGISTICS_REQUEST_CREATED",
		TrackingNumber:   "WB306659324TH",
		ShipActionStatus: "done",
	})
	if got, _ := view["external_shipment"].(bool); got {
		t.Fatalf("external_shipment = %v, want false after Nexflow ship action", view["external_shipment"])
	}
}

func TestParseBoolQuery(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "one", value: "1", want: true},
		{name: "true", value: "true", want: true},
		{name: "yes uppercase", value: " YES ", want: true},
		{name: "on", value: "on", want: true},
		{name: "zero", value: "0", want: false},
		{name: "empty", value: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseBoolQuery(tt.value); got != tt.want {
				t.Fatalf("parseBoolQuery(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestIsCriticalShopeeAccessError(t *testing.T) {
	if !isCriticalShopeeAccessError(fmt.Errorf("Shopee API 403 permission denied")) {
		t.Fatal("expected permission error to be critical")
	}
	if isCriticalShopeeAccessError(fmt.Errorf("tracking number not ready yet")) {
		t.Fatal("expected ordinary tracking error to stay non-critical")
	}
	if isCriticalShopeeAccessError(nil) {
		t.Fatal("nil error must not be critical")
	}
}

func shippingParameterFixture(t *testing.T) *shopeeapi.ShippingParameterResponse {
	t.Helper()
	var params shopeeapi.ShippingParameterResponse
	err := json.Unmarshal([]byte(`{
		"response": {
			"info_needed": {
				"pickup": ["address_id", "pickup_time_id"],
				"dropoff": ["branch_id"],
				"non_integrated": ["tracking_number"]
			},
			"pickup": {
				"address_list": [
					{
						"address_id": 12345,
						"address": "warehouse",
						"time_slot_list": [
							{"pickup_time_id": 67890, "date": 1779180000}
						]
					}
				]
				},
				"dropoff": {
					"branch_list": [
						{"branch_id": "BR-1", "name": "Main branch", "address": "Bangkok 10110", "latitude": 13.7563, "longitude": 100.5018, "distance": 3090}
					]
				}
			}
	}`), &params)
	if err != nil {
		t.Fatalf("unmarshal shipping params: %v", err)
	}
	return &params
}
