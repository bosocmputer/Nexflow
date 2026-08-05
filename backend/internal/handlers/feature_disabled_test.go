package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"nexflow/internal/config"
)

func TestFeatureGoneReturnsStableContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	FeatureGone("ปิดใช้งานแล้ว")(c)

	if recorder.Code != http.StatusGone {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusGone)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["error"] != "feature_disabled" || body["message"] != "ปิดใช้งานแล้ว" {
		t.Fatalf("unexpected response: %#v", body)
	}
}

func TestBlockPurchaseFlowRejectsPurchaseOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &BillHandler{cfg: &config.Config{PurchaseFlowEnabled: false}}

	for _, billType := range []string{"purchase", " Purchase "} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		if !h.blockPurchaseFlow(c, billType) {
			t.Fatalf("bill type %q was not blocked", billType)
		}
		if recorder.Code != http.StatusGone {
			t.Fatalf("bill type %q status = %d, want %d", billType, recorder.Code, http.StatusGone)
		}
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	if h.blockPurchaseFlow(c, "sale") {
		t.Fatal("sale bill was blocked")
	}
}
