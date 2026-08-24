package catalog

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"nexflow/internal/repository"
	"nexflow/internal/services/sml"
)

func TestNormalizeUnitCatalogPagePreservesMultipleUnits(t *testing.T) {
	page, err := normalizeUnitCatalogPage([]sml.StockCatalogItem{{
		ItemCode: "SKU-1", ItemName: "สินค้า", StandardUnit: "ชิ้น",
		Units: []sml.StockCatalogUnit{
			{Code: "ชิ้น", Name: "ชิ้น", StandValue: 1, DivideValue: 1, RowOrder: 0},
			{Code: "แพ็ค", Name: "แพ็ค 12", StandValue: 12, DivideValue: 1, RowOrder: 1},
		},
	}})
	if err != nil {
		t.Fatalf("normalizeUnitCatalogPage: %v", err)
	}
	if len(page.Units) != 2 {
		t.Fatalf("units = %#v", page.Units)
	}
	if page.Units[1].StandValue != "12" || page.Units[1].DivideValue != "1" {
		t.Fatalf("exact ratio = %s/%s", page.Units[1].StandValue, page.Units[1].DivideValue)
	}
	if !page.Units[0].IsDefault || page.Units[1].IsDefault {
		t.Fatalf("default flags = %#v", page.Units)
	}
}

func TestNormalizeUnitCatalogPageFailsClosedForInvalidFactors(t *testing.T) {
	for _, factor := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		_, err := normalizeUnitCatalogPage([]sml.StockCatalogItem{{
			ItemCode: "SKU-1", StandardUnit: "แพ็ค",
			Units: []sml.StockCatalogUnit{{Code: "แพ็ค", StandValue: factor, DivideValue: 1}},
		}})
		if err == nil {
			t.Fatalf("factor %v should be rejected", factor)
		}
	}
}

func TestNormalizeUnitCatalogPageRejectsMissingDefaultUnit(t *testing.T) {
	_, err := normalizeUnitCatalogPage([]sml.StockCatalogItem{{
		ItemCode: "SKU-1", StandardUnit: "ชิ้น",
		Units: []sml.StockCatalogUnit{{Code: "แพ็ค", StandValue: 12, DivideValue: 1}},
	}})
	if err == nil {
		t.Fatal("expected standard unit missing from ic_unit_use to fail")
	}
}

func TestRunUnitCatalogGenerationActivatesOnlyAfterEveryPage(t *testing.T) {
	store := &fakeGenerationStore{}
	client := &fakeCatalogPager{pages: map[int]*sml.StockCatalogPage{
		1: {Items: []sml.StockCatalogItem{stockCatalogItem("SKU-1")}, Total: 2, Page: 1, Size: 1},
		2: {Items: []sml.StockCatalogItem{stockCatalogItem("SKU-2")}, Total: 2, Page: 2, Size: 1},
	}}

	count, err := runUnitCatalogGeneration(context.Background(), store, client, "worker-1", time.Now())
	if err != nil {
		t.Fatalf("runUnitCatalogGeneration: %v", err)
	}
	if count != 2 || len(store.pages) != 2 || !store.activated || store.failed {
		t.Fatalf("count=%d pages=%d activated=%v failed=%v", count, len(store.pages), store.activated, store.failed)
	}
	if store.pages[1].ProductCount != 2 || store.pages[1].UnitCount != 2 {
		t.Fatalf("final progress = %#v", store.pages[1])
	}
}

func TestRunUnitCatalogGenerationLeavesPreviousGenerationActiveOnPartialFailure(t *testing.T) {
	store := &fakeGenerationStore{}
	want := errors.New("page failed")
	client := &fakeCatalogPager{
		pages: map[int]*sml.StockCatalogPage{
			1: {Items: []sml.StockCatalogItem{stockCatalogItem("SKU-1")}, Total: 2, Page: 1, Size: 1},
		},
		errors: map[int]error{2: want},
	}

	_, err := runUnitCatalogGeneration(context.Background(), store, client, "worker-1", time.Now())
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
	if store.activated || !store.failed {
		t.Fatalf("activated=%v failed=%v", store.activated, store.failed)
	}
}

func stockCatalogItem(code string) sml.StockCatalogItem {
	return sml.StockCatalogItem{
		ItemCode: code, ItemName: code, StandardUnit: "ชิ้น",
		Units: []sml.StockCatalogUnit{{Code: "ชิ้น", Name: "ชิ้น", StandValue: 1, DivideValue: 1}},
	}
}

type fakeCatalogPager struct {
	pages  map[int]*sml.StockCatalogPage
	errors map[int]error
}

func (f *fakeCatalogPager) CatalogRange(_ context.Context, page, _ int, _, _ *time.Time) (*sml.StockCatalogPage, error) {
	if err := f.errors[page]; err != nil {
		return nil, err
	}
	return f.pages[page], nil
}

type fakeGenerationStore struct {
	pages     []repository.CatalogGenerationPage
	activated bool
	failed    bool
}

func (f *fakeGenerationStore) AcquireCatalogGenerationLease(context.Context, string, time.Duration) (int64, error) {
	return 11, nil
}
func (f *fakeGenerationStore) RenewCatalogGenerationLease(context.Context, string, int64, time.Duration) error {
	return nil
}
func (f *fakeGenerationStore) ReleaseCatalogGenerationLease(context.Context, string, int64) error {
	return nil
}
func (f *fakeGenerationStore) BeginCatalogGeneration(context.Context, string, int64, time.Time) (string, error) {
	return "gen-1", nil
}
func (f *fakeGenerationStore) StageCatalogGenerationPage(_ context.Context, _, _ string, _ int64, page repository.CatalogGenerationPage) error {
	f.pages = append(f.pages, page)
	return nil
}
func (f *fakeGenerationStore) ActivateCatalogGeneration(context.Context, string, string, int64, time.Time) error {
	f.activated = true
	return nil
}
func (f *fakeGenerationStore) FailCatalogGeneration(context.Context, string, string) error {
	f.failed = true
	return nil
}
