package tiktokshop

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
)

func TestProductCatalogStoreReplacesSnapshotAndFinishesRunAtomically(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	store := NewProductCatalogStore(database)
	result := TikTokCatalogRunResult{
		ID: "0d0615a1-789c-44ca-95eb-56343232dd3d", ShopID: "7494619203789490654", Status: "succeeded",
		PageCount: 1, ProductCount: 1, SKUCount: 1, WarehouseCount: 1,
		UpstreamRequestIDs: []string{"products-1", "inventory-1"},
	}
	products := []TikTokCatalogProduct{{
		Product: Product{ID: "1001", Title: "AOY Product", Status: ProductStatusActivate, SKUs: []ProductSKU{{ID: "2001", SellerSKU: "AOY-001"}}},
		Inventory: map[string]InventorySearchSKU{"2001": {
			ID: "2001", TotalAvailableQuantity: 7, TotalCommittedQuantity: 1,
			WarehouseInventory: []WarehouseInventory{{WarehouseID: "3001", AvailableQuantity: 7, CommittedQuantity: 1}},
		}},
	}}

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE tiktok_shop_product_inventory SET is_active=false`).WithArgs(result.ShopID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE tiktok_shop_product_skus SET is_active=false`).WithArgs(result.ShopID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE tiktok_shop_products SET is_active=false`).WithArgs(result.ShopID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO tiktok_shop_products`).
		WithArgs(result.ShopID, "1001", "AOY Product", ProductStatusActivate, int64(0), int64(0), result.ID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO tiktok_shop_product_skus`).
		WithArgs(result.ShopID, "1001", "2001", "AOY-001", sqlmock.AnyArg(), int64(7), int64(1), result.ID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO tiktok_shop_product_inventory`).
		WithArgs(result.ShopID, "1001", "2001", "3001", int64(7), int64(1), result.ID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE tiktok_shop_product_catalog_runs`).
		WithArgs(result.ID, 1, 1, 1, 1, sqlmock.AnyArg(), result.ShopID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := store.ReplaceCatalog(context.Background(), result, products); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestProductCatalogStoreRecognizesConcurrentRunConstraint(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	store := NewProductCatalogStore(database)
	mock.ExpectQuery(`(?s)WITH expired AS.*INSERT INTO tiktok_shop_product_catalog_runs`).
		WithArgs("7494619203789490654", "manual").
		WillReturnError(&pq.Error{Code: "23505", Constraint: "tiktok_shop_product_catalog_runs_active_idx"})
	if _, err := store.CreateRun(context.Background(), "7494619203789490654", "manual"); err != ErrProductCatalogSyncInProgress {
		t.Fatalf("error=%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
