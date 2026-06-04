package handlers

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"nexflow/internal/config"
	"nexflow/internal/models"
	"nexflow/internal/services/shopeeapi"
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
