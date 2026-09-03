package config

import "testing"

func TestParseMarketplaceConversionMode(t *testing.T) {
	for _, mode := range []string{"off", "shadow", "active"} {
		got, err := parseMarketplaceConversionMode(mode)
		if err != nil {
			t.Fatalf("parseMarketplaceConversionMode(%q): %v", mode, err)
		}
		if got != mode {
			t.Fatalf("parseMarketplaceConversionMode(%q) = %q", mode, got)
		}
	}
}

func TestParseMarketplaceConversionModeRejectsUnknownValue(t *testing.T) {
	if _, err := parseMarketplaceConversionMode("enabled"); err == nil {
		t.Fatal("expected invalid mode to be rejected")
	}
}

func TestParseSMLStockAvailabilityMode(t *testing.T) {
	for _, mode := range []string{"physical_v1", "shadow", "net_sale_order_v1"} {
		got, err := parseSMLStockAvailabilityMode(mode)
		if err != nil {
			t.Fatalf("parseSMLStockAvailabilityMode(%q): %v", mode, err)
		}
		if got != mode {
			t.Fatalf("parseSMLStockAvailabilityMode(%q) = %q", mode, got)
		}
	}
}

func TestParseSMLStockAvailabilityModeRejectsUnsafeFallback(t *testing.T) {
	if _, err := parseSMLStockAvailabilityMode("active"); err == nil {
		t.Fatal("unknown stock availability mode must fail closed")
	}
}

func TestParseSMLDocumentProfileMode(t *testing.T) {
	for _, mode := range []string{"off", "shadow", "active"} {
		got, err := parseSMLDocumentProfileMode(mode)
		if err != nil || got != mode {
			t.Fatalf("parseSMLDocumentProfileMode(%q) = %q, %v", mode, got, err)
		}
	}
	if _, err := parseSMLDocumentProfileMode("enabled"); err == nil {
		t.Fatal("unknown document profile mode must fail closed")
	}
}

func TestParseSMLDocumentProfileRouteModesUsesSafeDefaults(t *testing.T) {
	modes, err := parseSMLDocumentProfileRouteModes("", "active")
	if err != nil {
		t.Fatal(err)
	}
	if modes["saleinvoice"] != "active" {
		t.Fatalf("saleinvoice=%q want active", modes["saleinvoice"])
	}
	for _, route := range []string{"saleorder", "saleordercancel", "saleinvoicecancel", "creditnote"} {
		if modes[route] != "off" {
			t.Fatalf("%s=%q want off", route, modes[route])
		}
	}
}

func TestParseSMLDocumentProfileRouteModesAcceptsCompleteOverride(t *testing.T) {
	modes, err := parseSMLDocumentProfileRouteModes(
		"saleinvoice:active,saleorder:shadow,saleordercancel:shadow,saleinvoicecancel:off,creditnote:active",
		"off",
	)
	if err != nil {
		t.Fatal(err)
	}
	if modes["saleinvoice"] != "active" || modes["saleorder"] != "shadow" || modes["creditnote"] != "active" {
		t.Fatalf("unexpected modes: %+v", modes)
	}
}

func TestParseSMLDocumentProfileRouteModesRejectsUnsafeInput(t *testing.T) {
	for _, raw := range []string{
		"unknown:active",
		"saleinvoice:enabled",
		"saleinvoice:active,saleinvoice:shadow",
		"saleinvoice",
		"saleinvoice:active,",
	} {
		if _, err := parseSMLDocumentProfileRouteModes(raw, "active"); err == nil {
			t.Fatalf("%q must fail closed", raw)
		}
	}
}
