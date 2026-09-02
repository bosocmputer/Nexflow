package handlers

import (
	"testing"
	"time"

	"nexflow/internal/models"
)

func TestAutoSMLPreviewTokenBindsShopTriggerConfigAndRoute(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	claims := autoSMLPreviewClaims{ShopID: 123, TriggerStatus: "READY_TO_SHIP", ConfigVersion: 4, RouteSignature: "route-1", ExpiresAt: now.Add(time.Minute).Unix()}
	token, err := signAutoSMLPreviewToken(claims, "01234567890123456789012345678901")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateAutoSMLPreviewToken(token, "01234567890123456789012345678901", claims, now); err != nil {
		t.Fatal(err)
	}
	changed := claims
	changed.RouteSignature = "route-2"
	if err := validateAutoSMLPreviewToken(token, "01234567890123456789012345678901", changed, now); err == nil {
		t.Fatal("route change must invalidate preview")
	}
	if err := validateAutoSMLPreviewToken(token, "01234567890123456789012345678901", claims, now.Add(2*time.Minute)); err == nil {
		t.Fatal("expired preview must be rejected")
	}
	changed = claims
	changed.ConfigVersion++
	if err := validateAutoSMLPreviewToken(token, "01234567890123456789012345678901", changed, now); err == nil {
		t.Fatal("config race must invalidate preview")
	}
}

func TestAutoSMLResumeRequiresSeparateConfirmation(t *testing.T) {
	before := models.ShopeeAutoSMLSetting{Enabled: true, TriggerStatus: "READY_TO_SHIP"}
	before.PausedReason = "profile_terminal_failure"
	if got := requiredAutoSMLSettingConfirmation(before, true, "READY_TO_SHIP"); got != "RESUME_AUTO_SML" {
		t.Fatalf("confirmation=%q", got)
	}
}
