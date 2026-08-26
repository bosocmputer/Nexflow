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
