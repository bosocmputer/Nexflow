package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"nexflow/internal/models"
)

func TestMarketplaceAliasProductGroupsUsesBoundedKeysetPage(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(`(?s)WITH matched_keys AS.*FROM marketplace_item_aliases matched.*\(matched.source,matched.account_key,COALESCE.*\) > \(\$3,\$4,\$5\).*LIMIT \$6.*JOIN marketplace_item_aliases a.*GROUP BY.*ORDER BY.*`).
		WithArgs("shopee", "%pack%", "shopee", "shop:1", "100", 51).
		WillReturnRows(sqlmock.NewRows([]string{
			"source", "account_key", "account_name", "parent_key", "parent_key_kind", "product_name",
			"variant_count", "ready_count", "fix_count", "disabled_count", "updated_at",
		}))

	groups, more, err := NewMarketplaceAliasRepo(db).ProductGroups(context.Background(), MarketplaceProductGroupFilter{
		Source: "shopee", Query: "pack", Limit: 50,
		AfterSource: "shopee", AfterAccountKey: "shop:1", AfterParentKey: "100",
	})
	if err != nil || more || len(groups) != 0 {
		t.Fatalf("groups=%#v more=%v err=%v", groups, more, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMarketplaceAliasProductGroupsReportsObservedShopeeInputChannels(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	updatedAt := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)WITH matched_keys AS.*shopee_stock_mappings.*raw_data->>'flow'.*shopee_excel.*FROM matched_keys`).
		WithArgs("shopee", 31).
		WillReturnRows(sqlmock.NewRows([]string{
			"source", "account_key", "account_name", "parent_key", "parent_key_kind", "product_name",
			"variant_count", "ready_count", "fix_count", "disabled_count", "updated_at",
			"shopee_api_used", "shopee_excel_used",
		}).AddRow("shopee", "shop:66610219", "Ploy", "26058910935", "external", "สินค้า",
			1, 1, 0, 0, updatedAt, true, false))

	groups, more, err := NewMarketplaceAliasRepo(db).ProductGroups(context.Background(), MarketplaceProductGroupFilter{
		Source: "shopee", Limit: 30,
	})
	if err != nil || more || len(groups) != 1 {
		t.Fatalf("groups=%#v more=%v err=%v", groups, more, err)
	}
	if got := groups[0].InputChannels; len(got) != 1 || got[0] != "shopee" {
		t.Fatalf("input channels=%v, want Shopee API only", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMarketplaceAliasGroupVariantsUsesChildKeysetWithoutOffset(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(`(?s)FROM marketplace_item_aliases a.*alias_parent_key.*=\$3.*\(COALESCE\(a.external_variant_id,''\),a.id::text\) > \(\$4,\$5\).*LIMIT \$6`).
		WithArgs("shopee", "shop:1", "100", "200", "alias-1", 101).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	variants, more, err := NewMarketplaceAliasRepo(db).ProductGroupVariants(context.Background(), MarketplaceProductVariantFilter{
		Source: "shopee", AccountKey: "shop:1", ParentKey: "100", Limit: 100,
		AfterVariantID: "200", AfterID: "alias-1",
	})
	if err != nil || more || len(variants) != 0 {
		t.Fatalf("variants=%#v more=%v err=%v", variants, more, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMarketplaceAliasIncrementUsageDoesNotChangeVersion(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectExec(`(?s)UPDATE marketplace_item_aliases\s+SET usage_count = usage_count \+ 1,\s+last_used_at = NOW\(\)\s+WHERE id = \$1`).
		WithArgs("alias-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := NewMarketplaceAliasRepo(db).IncrementUsage("alias-1"); err != nil {
		t.Fatalf("IncrementUsage: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMarketplaceAliasIncrementUsageCountsUsesOneBatchUpdate(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectExec(`(?s)UPDATE marketplace_item_aliases a.*unnest\(\$1::uuid\[\],\$2::bigint\[\]\).*WHERE a\.id=batch\.id`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 2))

	if err := NewMarketplaceAliasRepo(db).IncrementUsageCounts(map[string]int{"00000000-0000-0000-0000-000000000001": 2}); err != nil {
		t.Fatalf("IncrementUsageCounts: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMarketplaceAliasListUsableOnlyExcludesUnconfirmedScope(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\).*a\.is_active = TRUE AND a\.scope_confirmed = TRUE`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`(?s)FROM marketplace_item_aliases a.*a\.is_active = TRUE AND a\.scope_confirmed = TRUE.*LIMIT \$1 OFFSET \$2`).
		WithArgs(30, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	rows, total, err := NewMarketplaceAliasRepo(db).List("", "", true, 1, 30)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 0 || len(rows) != 0 {
		t.Fatalf("List returned total=%d rows=%d, want 0/0", total, len(rows))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMarketplaceAliasReviewGroupsIncludesUnmappedShopeeCatalogProducts(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(`(?s)FROM bill_items bi.*JOIN bills b.*WHERE.*b\.bill_type = \$1.*b\.source = \$2`).
		WithArgs("sale", "shopee").
		WillReturnRows(sqlmock.NewRows([]string{
			"bill_id", "source", "account_key", "account_name", "bill_type", "item_id", "raw_name",
			"source_sku", "external_item_id", "external_variant_id",
		}))
	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\).*FROM shopee_stock_products p.*NOT EXISTS.*COALESCE\(NULLIF\(btrim\(p\.model_sku\),''\),p\.item_sku,''\).*marketplace_item_aliases`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`(?s)SELECT p\.shop_id.*FROM shopee_stock_products p.*NOT EXISTS.*marketplace_item_aliases.*ORDER BY.*LIMIT \$1 OFFSET \$2`).
		WithArgs(30, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"shop_id", "account_name", "item_id", "model_id", "item_name", "model_name", "item_sku", "model_sku",
		}).AddRow(
			int64(66610219), "Ploy", int64(26058910935), int64(187745336266),
			"อัยน่าเอส กล่องขาว วีเนสต้า กล่องเหลือ", "1ก+ดีท็อก 1 แผง", "", "",
		))

	result, err := NewMarketplaceAliasRepo(db).ReviewGroupsPaged(models.MarketplaceAliasReviewFilter{
		BillType: "sale", Source: "shopee", Sort: "impact", Page: 1, PerPage: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || len(result.Groups) != 1 {
		t.Fatalf("result=%+v, want one catalog-discovered review group", result)
	}
	group := result.Groups[0]
	if !group.CatalogProduct || group.Source != "shopee" || group.AccountKey != "shop:66610219" {
		t.Fatalf("group=%+v, want a Shopee catalog product scoped to the connected shop", group)
	}
	if len(group.InputChannels) != 1 || group.InputChannels[0] != "shopee" {
		t.Fatalf("input channels=%v, want Shopee API only", group.InputChannels)
	}
	if group.ExternalItemID != "26058910935" || group.ExternalVariantID != "187745336266" {
		t.Fatalf("group identity=%s/%s, want exact Shopee item/model identity", group.ExternalItemID, group.ExternalVariantID)
	}
	if group.RawName != "อัยน่าเอส กล่องขาว วีเนสต้า กล่องเหลือ · 1ก+ดีท็อก 1 แผง" {
		t.Fatalf("raw_name=%q", group.RawName)
	}
	if group.ItemCount != 0 || group.BillCount != 0 {
		t.Fatalf("catalog-only group must not claim pending orders: items=%d bills=%d", group.ItemCount, group.BillCount)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMarketplaceAliasReviewGroupsKeepsOrderIssuesAheadOfCatalogOnlyProducts(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(`(?s)FROM bill_items bi.*JOIN bills b.*WHERE.*b\.bill_type = \$1`).
		WithArgs("sale").
		WillReturnRows(sqlmock.NewRows([]string{
			"bill_id", "source", "account_key", "account_name", "bill_type", "item_id", "raw_name",
			"source_sku", "external_item_id", "external_variant_id",
		}).AddRow("bill-1", "lazada", "default", "", "sale", "item-1", "สินค้าในออเดอร์", "SKU-1", "", ""))
	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\).*FROM shopee_stock_products p.*NOT EXISTS.*COALESCE\(NULLIF\(btrim\(p\.model_sku\),''\),p\.item_sku,''\).*marketplace_item_aliases`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(4))

	result, err := NewMarketplaceAliasRepo(db).ReviewGroupsPaged(models.MarketplaceAliasReviewFilter{
		BillType: "sale", Sort: "impact", Page: 1, PerPage: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 5 || len(result.Groups) != 1 || result.Groups[0].CatalogProduct {
		t.Fatalf("result=%+v, want the pending order first and an exact combined total", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMarketplaceAliasReviewGroupsPaginatesShopeeCatalogProducts(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(`(?s)FROM bill_items bi.*JOIN bills b.*WHERE.*b\.bill_type = \$1.*b\.source = \$2`).
		WithArgs("sale", "shopee").
		WillReturnRows(sqlmock.NewRows([]string{
			"bill_id", "source", "account_key", "account_name", "bill_type", "item_id", "raw_name",
			"source_sku", "external_item_id", "external_variant_id",
		}))
	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\).*FROM shopee_stock_products p`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(34))
	mock.ExpectQuery(`(?s)SELECT p\.shop_id.*FROM shopee_stock_products p.*LIMIT \$1 OFFSET \$2`).
		WithArgs(30, 30).
		WillReturnRows(sqlmock.NewRows([]string{
			"shop_id", "account_name", "item_id", "model_id", "item_name", "model_name", "item_sku", "model_sku",
		}).AddRow(int64(66610219), "Ploy", int64(26058910935), int64(187745336266), "สินค้า", "ตัวเลือก", "", ""))

	result, err := NewMarketplaceAliasRepo(db).ReviewGroupsPaged(models.MarketplaceAliasReviewFilter{
		BillType: "sale", Source: "shopee", Sort: "impact", Page: 2, PerPage: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 34 || result.Page != 2 || len(result.Groups) != 1 {
		t.Fatalf("result=%+v, want catalog page two with the combined total", result)
	}
	if !result.Groups[0].CatalogProduct || result.Groups[0].ExternalVariantID != "187745336266" {
		t.Fatalf("group=%+v, want the exact catalog product from offset 30", result.Groups[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMarketplaceAliasReviewGroupsSkipsShopeeCatalogForAnotherSource(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(`(?s)FROM bill_items bi.*JOIN bills b.*WHERE.*b\.bill_type = \$1.*b\.source = \$2`).
		WithArgs("sale", "lazada").
		WillReturnRows(sqlmock.NewRows([]string{
			"bill_id", "source", "account_key", "account_name", "bill_type", "item_id", "raw_name",
			"source_sku", "external_item_id", "external_variant_id",
		}))

	result, err := NewMarketplaceAliasRepo(db).ReviewGroupsPaged(models.MarketplaceAliasReviewFilter{
		BillType: "sale", Source: "lazada", Sort: "impact", Page: 1, PerPage: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 0 || len(result.Groups) != 0 {
		t.Fatalf("result=%+v, want no Shopee catalog products in a Lazada-only filter", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMarketplaceAliasImpactExcludesItemsWithExactActiveSKU(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\), COUNT\(DISTINCT b\.id\).*NOT EXISTS.*FROM sml_catalog c.*c\.item_code=btrim.*source_sku.*\$3`).
		WithArgs("lazada", "default", "SELLER-SKU").
		WillReturnRows(sqlmock.NewRows([]string{"items", "bills"}).AddRow(3, 2))

	impact, err := NewMarketplaceAliasRepo(db).PreviewImpact(models.MarketplaceAliasIdentity{
		Source: "lazada", AccountKey: "default", SourceSKU: "SELLER-SKU",
	}, "", "SML-001")
	if err != nil {
		t.Fatalf("PreviewImpact: %v", err)
	}
	if impact.OpenItems != 3 || impact.OpenBills != 2 {
		t.Fatalf("impact = %#v, want 3 items in 2 bills", impact)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMarketplaceAliasSaveAndApplyRejectsStaleVersionAndRollsBack(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	version := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT id::text FROM marketplace_item_aliases.*FOR UPDATE`).
		WithArgs("alias-1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("alias-1"))
	mock.ExpectQuery(`(?s)UPDATE marketplace_item_aliases a.*external_item_id=\$3.*updated_at=\$14.*RETURNING`).
		WithArgs("shopee", "shop:1", "10", "20", "", "", "", "SML-NEW", "PCS", "user-1", "", false, "alias-1", version).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source", "account_key", "external_item_id", "external_variant_id",
			"source_sku", "raw_name", "normalized_key", "item_code", "unit_code",
			"confidence", "confirmed_by", "usage_count", "last_used_at", "created_at", "updated_at",
			"is_active", "match_method", "scope_confirmed",
			"external_parent_id", "parent_key", "parent_key_kind", "source_product_name", "source_variant_name",
			"mapping_revision", "metadata_updated_at", "quantity_multiplier",
			"unit_stand_value", "unit_divide_value", "unit_catalog_generation",
			"conversion_status", "sales_enabled", "stock_policy",
		}))
	mock.ExpectRollback()

	_, err = NewMarketplaceAliasRepo(db).SaveAndApply(MarketplaceAliasMutation{
		ID: "alias-1",
		Identity: models.MarketplaceAliasIdentity{
			Source: "shopee", AccountKey: "shop:1", ExternalItemID: "10", ExternalVariantID: "20",
		},
		BillType: "sale", ItemCode: "SML-NEW", UnitCode: "PCS", ConfirmedBy: "user-1", Version: &version,
	})
	if !errors.Is(err, ErrMarketplaceAliasConflict) {
		t.Fatalf("SaveAndApply error = %v, want ErrMarketplaceAliasConflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMarketplaceAliasApplyMatchesOnlyExactSourceSKU(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(`(?s)SELECT bi\.id.*NOT EXISTS.*c\.item_code = btrim.*\$5 <> ''.*source_sku.*= \$5`).
		WithArgs("shopee", "sale", "TARGET", "PCS", "SKU-A", "").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("item-a"))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE bill_items
		    SET item_code = $1, unit_code = $2, mapped = TRUE
		  WHERE id IN ($3)`)).
		WithArgs("TARGET", "PCS", "item-a").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)UPDATE bills b\s+SET status = 'pending'.*WHERE b\.source = \$1.*AND b\.bill_type = \$2`).
		WithArgs("shopee", "sale").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	applied, ready, err := NewMarketplaceAliasRepo(db).ApplyToOpenItems(
		"shopee", "sale", "SKU-A", "", "ชื่อเดียวกัน", "TARGET", "PCS",
	)
	if err != nil {
		t.Fatalf("ApplyToOpenItems: %v", err)
	}
	if applied != 1 || ready != 1 {
		t.Fatalf("applied=%d ready=%d, want 1/1", applied, ready)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMarketplaceAliasNoSKUUpsertUsesContiguousPostgresParameters(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectExec(`(?s)INSERT INTO marketplace_item_aliases.*ON CONFLICT DO NOTHING`).
		WithArgs("shopee", "default", "", "", "", "ชื่อ สินค้า", "ชื่อ สินค้า", "SKU-1", "PCS", "user-1", "manual_name", true).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`(?s)UPDATE marketplace_item_aliases a.*source_sku=\$4.*raw_name=\$5.*item_code=\$7.*WHERE a\.source=\$1 AND a\.account_key=\$2.*normalized_key=\$3`).
		WithArgs("shopee", "default", "ชื่อ สินค้า", "", "ชื่อ สินค้า", "ชื่อ สินค้า", "SKU-1", "PCS", "user-1", "manual_name", true).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source", "account_key", "external_item_id", "external_variant_id",
			"source_sku", "raw_name", "normalized_key", "item_code", "unit_code",
			"confidence", "confirmed_by", "usage_count", "last_used_at", "created_at", "updated_at",
			"is_active", "match_method", "scope_confirmed",
			"external_parent_id", "parent_key", "parent_key_kind", "source_product_name", "source_variant_name",
			"mapping_revision", "metadata_updated_at", "quantity_multiplier",
			"unit_stand_value", "unit_divide_value", "unit_catalog_generation",
			"conversion_status", "sales_enabled", "stock_policy",
		}).AddRow("alias-1", "shopee", "default", "", "", "", "ชื่อ สินค้า", "ชื่อ สินค้า", "SKU-1", "PCS", 1.0, "user-1", 0, now, now, now, true, "manual_name", true,
			"", "", "derived", "", "", 1, nil, 1, nil, nil, nil, "needs_review", true, "blocked"))
	mock.ExpectCommit()

	alias, err := NewMarketplaceAliasRepo(db).Upsert("shopee", "", "  ชื่อ   สินค้า  ", "SKU-1", "PCS", "user-1")
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if alias == nil || alias.ID != "alias-1" {
		t.Fatalf("alias = %#v", alias)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
