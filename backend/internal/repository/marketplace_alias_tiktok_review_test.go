package repository

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexflow/internal/models"
)

func TestTikTokOrderSnapshotReviewGroupsExposeUnmappedIdentityWithSafeOrderReference(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	filter := models.MarketplaceAliasReviewFilter{BillType: "sale", Source: "tiktok", Query: "คิ้ว"}
	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\).*FROM tiktok_shop_order_snapshots s.*jsonb_array_elements\(s\.normalized_items\).*marketplace_item_aliases`).
		WithArgs("%คิ้ว%").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	total, err := NewMarketplaceAliasRepo(db).countTikTokOrderSnapshotReviewGroups(filter)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("total=%d, want 1", total)
	}

	mock.ExpectQuery(`(?s)SELECT .*shop_id.*order_count.*source_order_id.*FROM tiktok_shop_order_snapshots s.*jsonb_array_elements\(s\.normalized_items\).*ORDER BY order_count DESC.*LIMIT \$2 OFFSET \$3`).
		WithArgs("%คิ้ว%", 30, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"shop_id", "account_name", "external_item_id", "external_variant_id", "product_name", "sku_name",
			"source_sku", "order_count", "source_order_id",
		}).AddRow(
			"7494619203789490654", "henna_milkford", "1729429119195974110", "1729429118580984286",
			"สีเพ้นท์คิ้ว", "No.5 สีฟ้า", "SKU-TT-5", 3, "586030483469993439",
		))

	groups, err := NewMarketplaceAliasRepo(db).listTikTokOrderSnapshotReviewGroups(filter, 30, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Fatalf("groups=%+v, want one", groups)
	}
	group := groups[0]
	if group.Source != "tiktok" || group.AccountKey != "shop:7494619203789490654" {
		t.Fatalf("scope=%s/%s", group.Source, group.AccountKey)
	}
	if group.ExternalItemID != "1729429119195974110" || group.ExternalVariantID != "1729429118580984286" {
		t.Fatalf("identity=%s/%s", group.ExternalItemID, group.ExternalVariantID)
	}
	if group.DiscoverySource != "tiktok_order_snapshot" || group.SourceReferenceID != "586030483469993439" || group.OrderCount != 3 {
		t.Fatalf("review evidence=%+v", group)
	}
	if group.BillCount != 0 || group.ItemCount != 0 {
		t.Fatalf("snapshot group must not claim local bill rows: %+v", group)
	}
	if len(group.InputChannels) != 1 || group.InputChannels[0] != "tiktok_shop" {
		t.Fatalf("input channels=%v", group.InputChannels)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTikTokOrderSnapshotReviewGroupsDisabledForLazadaFilter(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	repo := NewMarketplaceAliasRepo(db)
	filter := models.MarketplaceAliasReviewFilter{BillType: "sale", Source: "lazada"}
	total, err := repo.countTikTokOrderSnapshotReviewGroups(filter)
	if err != nil || total != 0 {
		t.Fatalf("total=%d err=%v", total, err)
	}
	groups, err := repo.listTikTokOrderSnapshotReviewGroups(filter, 30, 0)
	if err != nil || len(groups) != 0 {
		t.Fatalf("groups=%+v err=%v", groups, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTikTokOrderSnapshotReviewGroupsUseRequestedSortBeforePagination(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(`(?s)SELECT .*FROM tiktok_shop_order_snapshots s.*ORDER BY product_name,sku_name,s\.shop_id,external_item_id,external_variant_id.*LIMIT \$1 OFFSET \$2`).
		WithArgs(10, 20).
		WillReturnRows(sqlmock.NewRows([]string{
			"shop_id", "account_name", "external_item_id", "external_variant_id", "product_name", "sku_name",
			"source_sku", "order_count", "source_order_id",
		}))

	groups, err := NewMarketplaceAliasRepo(db).listTikTokOrderSnapshotReviewGroups(models.MarketplaceAliasReviewFilter{
		BillType: "sale", Source: "tiktok", Sort: "name",
	}, 10, 20)
	if err != nil || len(groups) != 0 {
		t.Fatalf("groups=%+v err=%v", groups, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMarketplaceAliasReviewGroupsIncludesTikTokOrderSnapshots(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(`(?s)FROM bill_items bi.*JOIN bills b.*WHERE.*b\.bill_type = \$1.*b\.source = \$2`).
		WithArgs("sale", "tiktok").
		WillReturnRows(sqlmock.NewRows([]string{
			"bill_id", "source", "account_key", "account_name", "bill_type", "item_id", "raw_name",
			"source_sku", "external_item_id", "external_variant_id",
		}))
	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\).*FROM tiktok_shop_order_snapshots s.*jsonb_array_elements\(s\.normalized_items\)`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`(?s)SELECT .*shop_id.*order_count.*source_order_id.*FROM tiktok_shop_order_snapshots s.*LIMIT \$1 OFFSET \$2`).
		WithArgs(30, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"shop_id", "account_name", "external_item_id", "external_variant_id", "product_name", "sku_name",
			"source_sku", "order_count", "source_order_id",
		}).AddRow(
			"7494619203789490654", "henna_milkford", "1729429119195974110", "1729429118580984286",
			"สีเพ้นท์คิ้ว", "No.5", "SKU-TT-5", 2, "586030483469993439",
		))

	result, err := NewMarketplaceAliasRepo(db).ReviewGroupsPaged(models.MarketplaceAliasReviewFilter{
		BillType: "sale", Source: "tiktok", Sort: "impact", Page: 1, PerPage: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || len(result.Groups) != 1 {
		t.Fatalf("result=%+v", result)
	}
	if result.Groups[0].DiscoverySource != "tiktok_order_snapshot" || result.Groups[0].SourceReferenceID != "586030483469993439" {
		t.Fatalf("group=%+v", result.Groups[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
