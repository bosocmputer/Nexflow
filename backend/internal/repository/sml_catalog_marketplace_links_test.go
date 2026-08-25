package repository

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestCatalogListAttachesMarketplaceSummariesInOneBatch(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM sml_catalog WHERE is_active = TRUE`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`(?s)SELECT item_code, item_name.*FROM sml_catalog.*ORDER BY item_code`).
		WithArgs(50, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"item_code", "item_name", "item_name2", "unit_code", "wh_code", "shelf_code",
			"group_code", "balance_qty", "embedding_status", "embedded_at", "is_active",
			"image_count", "primary_image_roworder", "primary_image_guid", "primary_image_bytes",
			"image_synced_at", "synced_at", "created_at", "item_type", "set_component_count",
			"set_definition_hash", "set_document_valid", "set_stock_valid", "set_warning_codes",
		}).AddRow(
			"SKU-1", "สินค้า 1", "", "ชิ้น", "", "", "", nil, "disabled", nil, true,
			0, nil, "", nil, nil, now, now, 0, 0, "", true, true, []byte(`[]`),
		))
	mock.ExpectQuery(`(?s)SELECT item_code, source, COUNT\(\*\).*FROM marketplace_item_aliases`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"item_code", "source", "mapping_count", "product_count", "account_count"}).
			AddRow("SKU-1", "shopee", 3, 2, 1).
			AddRow("SKU-1", "tiktok", 1, 1, 1))

	items, total, err := NewSMLCatalogRepo(db).List(1, 50, "", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("total/items = %d/%d, want 1/1", total, len(items))
	}
	if len(items[0].MarketplaceSummaries) != 2 {
		t.Fatalf("marketplace summaries = %#v", items[0].MarketplaceSummaries)
	}
	if got := items[0].MarketplaceSummaries[0]; got.Source != "shopee" || got.MappingCount != 3 || got.ProductCount != 2 || got.AccountCount != 1 {
		t.Fatalf("Shopee summary = %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestCatalogMarketplaceLinksUsesBoundedKeysetPage(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)FROM marketplace_item_aliases a.*a.item_code=\$1.*ORDER BY a.source,a.account_key,a.id`).
		WithArgs("SKU-1", "shopee", "shop:1", "alias-1", 3).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source", "account_key", "account_name", "product_name", "variant_name",
			"source_sku", "external_item_id", "external_variant_id", "unit_code",
			"quantity_multiplier", "conversion_status", "scope_confirmed", "updated_at",
		}).
			AddRow("alias-2", "shopee", "shop:1", "AOY", "สินค้า A", "สีแดง", "SKU-A-R", "100", "200", "ชิ้น", 1, "ready", true, now).
			AddRow("alias-3", "shopee", "shop:1", "AOY", "สินค้า A", "สีน้ำเงิน", "SKU-A-B", "100", "201", "ชิ้น", 2, "ready", true, now).
			AddRow("alias-4", "tiktok", "default", "", "สินค้า B", "แบบปกติ", "SKU-B", "300", "400", "ชิ้น", 1, "needs_review", true, now))

	links, hasMore, err := NewSMLCatalogRepo(db).MarketplaceLinks(t.Context(), CatalogMarketplaceLinkFilter{
		ItemCode: "SKU-1", Limit: 2, AfterSource: "shopee", AfterAccountKey: "shop:1", AfterID: "alias-1",
	})
	if err != nil {
		t.Fatalf("MarketplaceLinks: %v", err)
	}
	if !hasMore || len(links) != 2 {
		t.Fatalf("hasMore/len = %v/%d, want true/2", hasMore, len(links))
	}
	if links[1].QuantityMultiplier != 2 || links[1].VariantName != "สีน้ำเงิน" {
		t.Fatalf("second link = %#v", links[1])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}
