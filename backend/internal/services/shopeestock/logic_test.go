package shopeestock

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"nexflow/internal/services/shopeeapi"
	"nexflow/internal/services/sml"
)

func TestSingleWarehouseFallbackOnlyAcceptsShopeeWhitelistCapability(t *testing.T) {
	if !isShopeeSingleWarehouseFallback(&shopeeapi.GatewayError{Code: "warehouse.error_not_in_whitelist"}) {
		t.Fatal("gateway whitelist error should use default seller stock")
	}
	if !isShopeeSingleWarehouseFallback(&shopeeapi.BusinessError{Code: "warehouse.error_not_in_whitelist"}) {
		t.Fatal("direct whitelist error should use default seller stock")
	}
	if isShopeeSingleWarehouseFallback(&shopeeapi.GatewayError{Code: "permission_denied"}) {
		t.Fatal("unrelated permission error must not be ignored")
	}
	if isShopeeSingleWarehouseFallback(errors.New("warehouse.error_not_in_whitelist")) {
		t.Fatal("untyped error must not be silently ignored")
	}
}

func TestTodayBangkokReturnsCalendarDate(t *testing.T) {
	if _, err := time.Parse("2006-01-02", TodayBangkok()); err != nil {
		t.Fatalf("TodayBangkok returned invalid date: %v", err)
	}
}

func TestCalculateTarget(t *testing.T) {
	if got := CalculateTarget(100, 80, 6); got != 13 {
		t.Fatalf("target = %d, want 13", got)
	}
	if got := CalculateTarget(-5, 80, 1); got != 0 {
		t.Fatalf("negative balance must clamp to zero, got %d", got)
	}
}

func TestWriteErrorClassification(t *testing.T) {
	rateLimit := &shopeeapi.GatewayError{Code: "rate_limited", Retryable: true}
	if !isRetryableWrite(rateLimit) || isUnknownWrite(rateLimit) {
		t.Fatal("rate limit must retry after read-back but is not an unknown timeout")
	}
	timeout := &shopeeapi.GatewayError{Code: "shopee_timeout", Retryable: true}
	if !isRetryableWrite(timeout) || !isUnknownWrite(timeout) {
		t.Fatal("gateway timeout must be retryable and retain unknown-result semantics")
	}
	if !isUnknownWrite(context.DeadlineExceeded) {
		t.Fatal("transport deadline must retain unknown-result semantics")
	}
}

func TestCalculateTargetConvertsSmallestUnitToShopeeSellingUnit(t *testing.T) {
	if got := CalculateTarget(375, 80, 6); got != 50 {
		t.Fatalf("target = %d, want floor(375*0.8/6)=50", got)
	}
}

func TestUnitWarnings(t *testing.T) {
	valid := sml.StockCatalogUnit{StandValue: 6, DivideValue: 1, Ratio: 6}
	if got := UnitWarnings(valid); len(got) != 0 {
		t.Fatalf("valid unit warnings = %v", got)
	}
	broken := sml.StockCatalogUnit{StandValue: 1, DivideValue: 2, Ratio: 1}
	if got := UnitWarnings(broken); !reflect.DeepEqual(got, []string{"unit_factor_below_one", "unit_ratio_mismatch"}) {
		t.Fatalf("warnings = %v", got)
	}
}

func TestCatalogResolveUsesModelSKUExactBeforeBarcode(t *testing.T) {
	index := NewCatalogIndex([]sml.StockCatalogItem{
		{ItemCode: "SKU-1", StandardUnit: "PCS", Units: []sml.StockCatalogUnit{{Code: "PCS", StandValue: 1, DivideValue: 1, Ratio: 1}}},
		{ItemCode: "OTHER", StandardUnit: "BOX", Units: []sml.StockCatalogUnit{{Code: "BOX", StandValue: 6, DivideValue: 1, Ratio: 6}}, Barcodes: []sml.StockCatalogBarcode{{Barcode: "BAR-2", UnitCode: "BOX"}}},
	})
	match, ok := index.Resolve(PreferredSKU("SKU-1", "BAR-2"))
	if !ok || match.ItemCode != "SKU-1" || match.Source != "sku" {
		t.Fatalf("unexpected match: %#v, %v", match, ok)
	}
}

func TestCatalogResolveUsesSmallestUnitPriorityForItemCode(t *testing.T) {
	index := NewCatalogIndex([]sml.StockCatalogItem{{
		ItemCode: "SKU-1", StandardUnit: "BOX",
		Units: []sml.StockCatalogUnit{
			{Code: "BOX", StandValue: 6, DivideValue: 1, Ratio: 6, RowOrder: 2, LineNumber: 2},
			{Code: "PCS", StandValue: 1, DivideValue: 1, Ratio: 1, RowOrder: 1, LineNumber: 1},
		},
	}})
	match, ok := index.Resolve("SKU-1")
	if !ok || match.UnitCode != "PCS" || match.Factor != 1 {
		t.Fatalf("must use smallest unit by row_order: %#v", match)
	}
}

func TestCatalogResolveDoesNotGuessAmbiguousBarcode(t *testing.T) {
	items := []sml.StockCatalogItem{
		{ItemCode: "A", Units: []sml.StockCatalogUnit{{Code: "PCS", StandValue: 1, DivideValue: 1, Ratio: 1}}, Barcodes: []sml.StockCatalogBarcode{{Barcode: "SAME", UnitCode: "PCS"}}},
		{ItemCode: "B", Units: []sml.StockCatalogUnit{{Code: "PCS", StandValue: 1, DivideValue: 1, Ratio: 1}}, Barcodes: []sml.StockCatalogBarcode{{Barcode: "SAME", UnitCode: "PCS"}}},
	}
	if _, ok := NewCatalogIndex(items).Resolve("SAME"); ok {
		t.Fatal("ambiguous barcode must not auto-map")
	}
}

func TestZeroDropCircuit(t *testing.T) {
	previous := []int64{1, 1, 1, 1, 1, 1, 1, 1, 1, 1}
	next := []int64{0, 0, 0, 0, 0, 0, 1, 1, 1, 1}
	if got := ZeroDropCircuit(previous, next); got != "mass_zero_drop" {
		t.Fatalf("circuit = %q", got)
	}
}
