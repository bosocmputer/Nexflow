package smlprofile

import (
	"strings"
	"testing"

	"nexflow/internal/models"
)

func TestResolveTemplateUsesOnlyDocumentTokens(t *testing.T) {
	got, err := ResolveTemplate(
		"{{channel}} | {{order_ref}} | {{bill_no}}",
		TemplateContext{Channel: "Shopee API", OrderRef: "ORDER-1", BillNo: "BF-1"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Shopee API | ORDER-1 | BF-1" {
		t.Fatalf("resolved template = %q", got)
	}
}

func TestValidateTemplateRejectsUnknownControlAndOversizedValues(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "unknown token", value: "{{buyer_name}}"},
		{name: "control character", value: "safe\nunsafe"},
		{name: "more than 255 unicode characters", value: strings.Repeat("ก", 256)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateTextTemplate("remark", tt.value); err == nil {
				t.Fatalf("ValidateTextTemplate(%q) unexpectedly succeeded", tt.value)
			}
		})
	}

	if err := ValidateTextTemplate("remark", strings.Repeat("ก", 255)); err != nil {
		t.Fatalf("255 unicode characters must be accepted: %v", err)
	}
}

func TestRouteSignatureCoversProfileRoutingAuthority(t *testing.T) {
	base := models.ChannelDefault{
		Channel: "shopee_realtime", BillType: "sale", Endpoint: "/api/v1/ic/sale-invoices",
		DocFormatCode: "SI", WHCode: "AB-1", ShelfCode: "001", VATType: 1, VATRate: 7,
		ShippingItemEnabled: true, ShippingItemCode: "AH-0061", ShippingItemUnitCode: "ชิ้น",
		Remark: "{{channel}} {{order_ref}}", Remark2: "ข้อความ", ConfigVersion: 4,
	}
	want := RouteSignature(base, ModeActive)

	changed := base
	changed.Remark = "{{bill_no}}"
	if got := RouteSignature(changed, ModeActive); got == want {
		t.Fatal("remark must change route signature")
	}

	changed = base
	changed.ConfigVersion++
	if got := RouteSignature(changed, ModeActive); got == want {
		t.Fatal("config version must change route signature")
	}

	if got := RouteSignature(base, ModeShadow); got == want {
		t.Fatal("profile mode must change route signature")
	}
}

func TestParseModeFailsClosed(t *testing.T) {
	for _, mode := range []string{"off", "shadow", "active", " ACTIVE "} {
		if _, err := ParseMode(mode); err != nil {
			t.Fatalf("ParseMode(%q): %v", mode, err)
		}
	}
	if _, err := ParseMode("enabled"); err == nil {
		t.Fatal("unknown profile mode must be rejected")
	}
}
