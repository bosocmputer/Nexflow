package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"

	"nexflow/internal/models"
	"nexflow/internal/repository"
)

type trackingCatalogReadThrough struct {
	mu            sync.Mutex
	calls         map[string]int
	inFlight      int
	maxConcurrent int
}

func (f *trackingCatalogReadThrough) RefreshOneContext(ctx context.Context, itemCode string) (*models.CatalogItem, bool, error) {
	f.mu.Lock()
	f.calls[itemCode]++
	f.inFlight++
	if f.inFlight > f.maxConcurrent {
		f.maxConcurrent = f.inFlight
	}
	f.mu.Unlock()
	select {
	case <-time.After(3 * time.Millisecond):
	case <-ctx.Done():
		return nil, false, ctx.Err()
	}
	f.mu.Lock()
	f.inFlight--
	f.mu.Unlock()
	return &models.CatalogItem{ItemCode: itemCode, UnitCode: "PCS", IsActive: true}, false, nil
}

func TestPrepareMarketplaceResolutionCapsAndDeduplicatesReadThrough(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	mock.ExpectQuery(`(?s)SELECT item_code.*FROM sml_catalog.*is_active = TRUE.*item_code = ANY`).
		WillReturnRows(sqlmock.NewRows([]string{"item_code", "item_name", "item_name2", "unit_code", "wh_code", "shelf_code", "price"}))

	items := make([]ShopeeExcelItem, 0, marketplaceCatalogReadThroughLimit+7)
	for i := 0; i < marketplaceCatalogReadThroughLimit+6; i++ {
		items = append(items, ShopeeExcelItem{SKU: fmt.Sprintf("SKU-%03d", i), ProductName: "สินค้า", Qty: 1})
	}
	items = append(items, ShopeeExcelItem{SKU: "SKU-000", ProductName: "สินค้าซ้ำ", Qty: 1})
	reader := &trackingCatalogReadThrough{calls: map[string]int{}}
	batch, err := prepareMarketplaceResolution(
		context.Background(), "shopee", []ShopeeOrder{{OrderID: "ORDER-1", Items: items}}, nil,
		repository.NewSMLCatalogRepo(db), reader, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("prepareMarketplaceResolution: %v", err)
	}
	if batch == nil {
		t.Fatal("batch is nil")
	}
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if len(reader.calls) != marketplaceCatalogReadThroughLimit {
		t.Fatalf("unique read-through calls = %d, want %d", len(reader.calls), marketplaceCatalogReadThroughLimit)
	}
	for i := 0; i < marketplaceCatalogReadThroughLimit; i++ {
		code := fmt.Sprintf("SKU-%03d", i)
		if reader.calls[code] != 1 {
			t.Fatalf("first batch SKU %s was not read through exactly once", code)
		}
	}
	if reader.calls["SKU-050"] != 0 {
		t.Fatal("read-through exceeded the first 50 unique SKUs")
	}
	for code, count := range reader.calls {
		if count != 1 {
			t.Fatalf("read-through %s called %d times, want once", code, count)
		}
	}
	if reader.maxConcurrent > 3 {
		t.Fatalf("max concurrency = %d, want <= 3", reader.maxConcurrent)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestBlockIfCatalogNotReadyReturnsActionableConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM sml_catalog WHERE is_active = TRUE`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	if !blockIfCatalogNotReady(c, repository.NewSMLCatalogRepo(db)) {
		t.Fatal("empty catalog was not blocked")
	}
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMarketplaceBillItemUsesCatalogSKUBeforeNameMapping(t *testing.T) {
	price := 120.0
	learned := &models.Mapping{
		ID:       "map-1",
		ItemCode: "NAME-CODE",
		UnitCode: "ชิ้น",
	}
	alias := &models.MarketplaceItemAlias{
		ID:       "alias-old",
		ItemCode: "ALIAS-OLD",
		UnitCode: "ชิ้น",
		IsActive: true,
	}
	matches := []models.CatalogMatch{{
		ItemCode: "NAME-MATCH",
		UnitCode: "กล่อง",
		Score:    0.99,
	}}

	item, high := marketplaceBillItemFromMatch(
		"Marketplace Name",
		" SKU-001\t",
		2,
		&price,
		"ถุง",
		alias,
		learned,
		matches,
		func(code string) *models.CatalogItem {
			if code != "SKU-001" {
				t.Fatalf("lookup code = %q, want normalized SKU-001", code)
			}
			return &models.CatalogItem{ItemCode: "SKU-001", UnitCode: "แพ็ค"}
		},
		0.85,
	)

	if !high || !item.Mapped {
		t.Fatalf("high/mapped = %v/%v, want true/true", high, item.Mapped)
	}
	if item.SourceSKU != "SKU-001" {
		t.Fatalf("SourceSKU = %q, want SKU-001", item.SourceSKU)
	}
	if item.ItemCode == nil || *item.ItemCode != "SKU-001" {
		t.Fatalf("ItemCode = %v, want SKU-001", item.ItemCode)
	}
	if item.UnitCode == nil || *item.UnitCode != "แพ็ค" {
		t.Fatalf("UnitCode = %v, want แพ็ค", item.UnitCode)
	}
	if item.MappingID != nil {
		t.Fatalf("MappingID = %v, want nil because SKU match wins", item.MappingID)
	}
}

func TestMarketplaceBillItemKeepsLegacyNameMappingAsReviewHint(t *testing.T) {
	learned := &models.Mapping{
		ID:       "map-verified",
		ItemCode: "NAME-CODE",
		UnitCode: "ชิ้น",
		Source:   "verified",
	}

	item, high := marketplaceBillItemFromMatch(
		"ชื่อที่ผู้ใช้ยืนยัน",
		"",
		1,
		nil,
		"หน่วย",
		nil,
		learned,
		nil,
		nil,
		0,
	)

	if high || item.Mapped {
		t.Fatalf("high/mapped = %v/%v, want false/false", high, item.Mapped)
	}
	if item.ItemCode != nil {
		t.Fatalf("ItemCode = %v, want nil until scoped Master is confirmed", item.ItemCode)
	}
	if item.MappingID != nil {
		t.Fatalf("MappingID = %v, want nil", item.MappingID)
	}
}

func TestMarketplaceResolutionIsolatesShopsAndPrefersStableIdentity(t *testing.T) {
	shopOneIdentity := &models.MarketplaceItemAlias{ID: "identity-1", ItemCode: "SML-A"}
	shopOneSKU := &models.MarketplaceItemAlias{ID: "sku-1", ItemCode: "SML-B"}
	shopTwoSKU := &models.MarketplaceItemAlias{ID: "sku-2", ItemCode: "SML-C"}
	batch := &marketplaceResolutionBatch{resolutions: map[string]matchResolution{
		"identity\x00shop:1\x00100\x00200": {alias: shopOneIdentity},
		"sku\x00shop:1\x00SKU-X":           {alias: shopOneSKU},
		"sku\x00shop:2\x00SKU-X":           {alias: shopTwoSKU},
	}}

	if got := batch.resolutionScoped("shop:1", "100", "200", "SKU-X", "สินค้า").alias; got != shopOneIdentity {
		t.Fatalf("stable identity did not win: %#v", got)
	}
	if got := batch.resolutionScoped("shop:2", "", "", "SKU-X", "สินค้า").alias; got != shopTwoSKU {
		t.Fatalf("shop 2 resolved wrong alias: %#v", got)
	}
	if got := batch.resolutionScoped("shop:3", "", "", "SKU-X", "สินค้า").alias; got != nil {
		t.Fatalf("shop isolation failed: %#v", got)
	}
}

func TestMarketplaceBillItemDoesNotGuessByNameWhenSKUIsMissingFromCatalog(t *testing.T) {
	price := 88.0
	matches := []models.CatalogMatch{{
		ItemCode: "NAME-MATCH",
		UnitCode: "กล่อง",
		Score:    0.91,
	}}

	item, high := marketplaceBillItemFromMatch(
		"Marketplace Name",
		"UNKNOWN-SKU",
		1,
		&price,
		"ถุง",
		nil,
		nil,
		matches,
		func(code string) *models.CatalogItem {
			if code != "UNKNOWN-SKU" {
				t.Fatalf("lookup code = %q, want UNKNOWN-SKU", code)
			}
			return nil
		},
		0.85,
	)

	if high || item.Mapped {
		t.Fatalf("high/mapped = %v/%v, want false/false without exact SKU or confirmed alias", high, item.Mapped)
	}
	if item.SourceSKU != "UNKNOWN-SKU" {
		t.Fatalf("SourceSKU = %q, want UNKNOWN-SKU", item.SourceSKU)
	}
	if item.ItemCode != nil {
		t.Fatalf("ItemCode = %v, want nil", item.ItemCode)
	}
}

func TestMarketplaceBillItemUsesAliasBeforeNameMapping(t *testing.T) {
	price := 88.0
	alias := &models.MarketplaceItemAlias{
		ID:       "alias-1",
		ItemCode: "ALIAS-CODE",
		UnitCode: "กล่อง",
		IsActive: true,
	}
	learned := &models.Mapping{
		ID:       "map-1",
		ItemCode: "NAME-CODE",
		UnitCode: "ชิ้น",
	}

	item, high := marketplaceBillItemFromMatch(
		"Marketplace Name",
		"UNKNOWN-SKU",
		1,
		&price,
		"ถุง",
		alias,
		learned,
		nil,
		func(code string) *models.CatalogItem { return nil },
		0.85,
	)

	if !high || !item.Mapped {
		t.Fatalf("high/mapped = %v/%v, want true/true from alias", high, item.Mapped)
	}
	if item.ItemCode == nil || *item.ItemCode != "ALIAS-CODE" {
		t.Fatalf("ItemCode = %v, want ALIAS-CODE", item.ItemCode)
	}
}

func TestMarketplaceBillItemDoesNotUseUnconfirmedNameSuggestion(t *testing.T) {
	item, high := marketplaceBillItemFromMatch(
		"สติ๊กเกอร์บล็อคคิ้ว / No.4 สีชมพู",
		"",
		1,
		nil,
		"แผ่น",
		nil,
		nil,
		[]models.CatalogMatch{{
			ItemCode: "AH-0030",
			ItemName: "สติ้กเกอร์บล็อคคิ้ว สีฟ้า 5 คู่",
			UnitCode: "แผ่น",
			Score:    0.92,
		}},
		nil,
		0.85,
	)

	if high || item.Mapped {
		t.Fatalf("high/mapped = %v/%v, want false/false for color conflict", high, item.Mapped)
	}
}

func TestMarketplaceBillItemFallsBackToNeedsReviewWithoutSKUOrHighNameMatch(t *testing.T) {
	item, high := marketplaceBillItemFromMatch(
		"Marketplace Name",
		"nan",
		1,
		nil,
		"ถุง",
		nil,
		nil,
		[]models.CatalogMatch{{
			ItemCode: "LOW-MATCH",
			UnitCode: "ชิ้น",
			Score:    0.4,
		}},
		func(code string) *models.CatalogItem {
			t.Fatalf("lookup should not run for nan SKU, got %q", code)
			return nil
		},
		0.85,
	)

	if high || item.Mapped {
		t.Fatalf("high/mapped = %v/%v, want false/false", high, item.Mapped)
	}
	if item.SourceSKU != "" {
		t.Fatalf("SourceSKU = %q, want empty", item.SourceSKU)
	}
	if item.ItemCode != nil {
		t.Fatalf("ItemCode = %v, want nil because suggestions are disabled", item.ItemCode)
	}
}
