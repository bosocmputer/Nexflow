package handlers

import (
	"testing"

	"nexflow/internal/models"
)

func TestMarketplaceBillItemUsesCatalogSKUBeforeNameMapping(t *testing.T) {
	price := 120.0
	learned := &models.Mapping{
		ID:       "map-1",
		ItemCode: "NAME-CODE",
		UnitCode: "ชิ้น",
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
		nil,
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

func TestMarketplaceBillItemFallsBackToNameWhenSKUIsMissingFromCatalog(t *testing.T) {
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

	if !high || !item.Mapped {
		t.Fatalf("high/mapped = %v/%v, want true/true from name fallback", high, item.Mapped)
	}
	if item.SourceSKU != "UNKNOWN-SKU" {
		t.Fatalf("SourceSKU = %q, want UNKNOWN-SKU", item.SourceSKU)
	}
	if item.ItemCode == nil || *item.ItemCode != "NAME-MATCH" {
		t.Fatalf("ItemCode = %v, want NAME-MATCH", item.ItemCode)
	}
}

func TestMarketplaceBillItemUsesAliasBeforeNameMapping(t *testing.T) {
	price := 88.0
	alias := &models.MarketplaceItemAlias{
		ID:       "alias-1",
		ItemCode: "ALIAS-CODE",
		UnitCode: "กล่อง",
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

func TestMarketplaceBillItemBlocksVariantConflict(t *testing.T) {
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
	if item.ItemCode == nil || *item.ItemCode != "LOW-MATCH" {
		t.Fatalf("ItemCode = %v, want LOW-MATCH as candidate", item.ItemCode)
	}
}
