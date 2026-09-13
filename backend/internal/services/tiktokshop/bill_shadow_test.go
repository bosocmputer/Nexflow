package tiktokshop

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

type billShadowSourceFake struct {
	source  *TikTokBillShadowSource
	err     error
	shopID  string
	orderID string
	calls   int
}

func (f *billShadowSourceFake) Load(_ context.Context, shopID, orderID string) (*TikTokBillShadowSource, error) {
	f.calls++
	f.shopID = shopID
	f.orderID = orderID
	return f.source, f.err
}

func TestTikTokBillShadowPreviewExplainsControlledOrderWithoutWriting(t *testing.T) {
	store := &billShadowSourceFake{source: controlledTikTokBillShadowSource()}
	preview, err := NewTikTokBillShadowService(store).Preview(context.Background(), "7494619203789490654", "586030483469993439")
	if err != nil {
		t.Fatal(err)
	}
	if store.calls != 1 || store.shopID != "7494619203789490654" || store.orderID != "586030483469993439" {
		t.Fatalf("unexpected source call: %+v", store)
	}
	if !preview.ShadowMode || preview.CanCreateBill || preview.ReadyForReviewedBill {
		t.Fatalf("unsafe preview flags: %+v", preview)
	}
	if preview.Amounts.ProductSubtotal != "300.00" || preview.Amounts.ProposedDocumentTotal != "300.00" ||
		preview.Amounts.BuyerPayment != "307.49" || preview.Amounts.ExcludedBuyerPlatformCharges != "7.49" {
		t.Fatalf("amount semantics changed: %+v", preview.Amounts)
	}
	if len(preview.Items) != 1 || preview.Items[0].Mapping.Status != TikTokBillShadowMappingMissing {
		t.Fatalf("expected one missing scoped mapping: %+v", preview.Items)
	}
	if !hasTikTokBillShadowBlocker(preview.Blockers, TikTokBillShadowBlockerMappingMissing) {
		t.Fatalf("missing mapping blocker: %+v", preview.Blockers)
	}
}

func TestTikTokBillShadowPreviewUsesOnlyExactShopScopedReadyMapping(t *testing.T) {
	source := controlledTikTokBillShadowSource()
	source.Items[0].Quantity = 2
	source.Items[0].LineIDs = append(source.Items[0].LineIDs, "line-2")
	source.Order.LineItems = append(source.Order.LineItems, OrderLineItem{
		ID: "line-2", ProductID: source.Items[0].ProductID, SKUID: source.Items[0].SKUID,
		ProductName: source.Items[0].ProductName, SKUName: source.Items[0].SKUName,
		Currency: "THB", OriginalPrice: "300", SalePrice: "300", SellerDiscount: "0", PlatformDiscount: "0",
	})
	source.Order.Payment.SubTotal = "600"
	source.Order.Payment.TotalAmount = "607.49"
	source.Price.Payment = "607.49"
	source.Price.Subtotal = "600"
	source.StoredProductSubtotal = "600"
	source.StoredPaymentTotal = "607.49"
	source.Mappings = []TikTokBillShadowMappingSource{
		{
			ProductID: source.Items[0].ProductID, SKUID: source.Items[0].SKUID,
			AccountKey: "default", ItemCode: "WRONG-SHOP", UnitCode: "กล่อง",
			IsActive: true, ScopeConfirmed: true, SalesEnabled: true, ConversionStatus: "ready",
			QuantityMultiplier: 1, UnitStandValue: "1", UnitDivideValue: "1", CatalogReady: true,
		},
		{
			ProductID: source.Items[0].ProductID, SKUID: source.Items[0].SKUID,
			AccountKey: "shop:7494619203789490654", ItemCode: "AH-0006", UnitCode: "แท่ง",
			IsActive: true, ScopeConfirmed: true, SalesEnabled: true, ConversionStatus: "ready",
			QuantityMultiplier: 3, UnitStandValue: "12", UnitDivideValue: "1", CatalogReady: true,
		},
	}

	preview, err := NewTikTokBillShadowService(&billShadowSourceFake{source: source}).Preview(
		context.Background(), "7494619203789490654", "586030483469993439",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.ReadyForReviewedBill || preview.CanCreateBill || len(preview.Blockers) != 0 {
		t.Fatalf("expected reviewed readiness without write permission: %+v", preview)
	}
	mapping := preview.Items[0].Mapping
	if mapping.ItemCode != "AH-0006" || mapping.UnitCode != "แท่ง" || mapping.MarketplaceQuantity != "2" ||
		mapping.SMLQuantity != "6" || mapping.BaseQuantity != "72" || mapping.Status != TikTokBillShadowMappingReady {
		t.Fatalf("wrong scoped conversion: %+v", mapping)
	}
}

func TestTikTokBillShadowPreviewFailsClosedOnAmountMismatchAndExistingBill(t *testing.T) {
	source := controlledTikTokBillShadowSource()
	source.StoredProductSubtotal = "299.99"
	source.ExistingBill = &TikTokBillShadowExistingBill{ID: "bill-1", Status: "pending", SourceAccountKey: "default"}

	preview, err := NewTikTokBillShadowService(&billShadowSourceFake{source: source}).Preview(
		context.Background(), source.ShopID, source.Order.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !hasTikTokBillShadowBlocker(preview.Blockers, TikTokBillShadowBlockerAmountMismatch) ||
		!hasTikTokBillShadowBlocker(preview.Blockers, TikTokBillShadowBlockerExistingBill) {
		t.Fatalf("expected amount and duplicate blockers: %+v", preview.Blockers)
	}
}

func TestTikTokBillShadowPreviewRejectsInvalidInputAndPropagatesNotFound(t *testing.T) {
	store := &billShadowSourceFake{err: ErrTikTokBillShadowNotFound}
	service := NewTikTokBillShadowService(store)
	if _, err := service.Preview(context.Background(), "shop-a", "order-a"); !errors.Is(err, ErrTikTokBillShadowInvalidInput) || store.calls != 0 {
		t.Fatalf("invalid input err=%v calls=%d", err, store.calls)
	}
	if _, err := service.Preview(context.Background(), "7494619203789490654", "586030483469993439"); !errors.Is(err, ErrTikTokBillShadowNotFound) {
		t.Fatalf("not found err=%v", err)
	}
}

func TestTikTokBillShadowStoreLoadsOnlyShopScopedAndLegacyIdentityCandidates(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	source := controlledTikTokBillShadowSource()
	safeOrder, _ := json.Marshal(source.Order)
	safePrice, _ := json.Marshal(source.Price)
	normalized, _ := json.Marshal(source.Items)
	mock.ExpectQuery("FROM tiktok_shop_order_snapshots s").
		WithArgs(source.ShopID, source.Order.ID).
		WillReturnRows(sqlmock.NewRows([]string{
			"shop_id", "shop_name", "order_status", "currency", "payment_total", "product_subtotal",
			"shipping_fee", "insurance_fee", "safe_order", "safe_price", "normalized_items", "last_synced_at", "source_hash",
		}).AddRow(
			source.ShopID, source.ShopName, string(source.StoredOrderStatus), source.StoredCurrency,
			source.StoredPaymentTotal, source.StoredProductSubtotal, source.StoredShippingFee, source.StoredItemInsuranceFee,
			safeOrder, safePrice, normalized, source.LastSyncedAt, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		))
	mock.ExpectQuery("FROM jsonb_to_recordset").
		WithArgs(normalized, "shop:"+source.ShopID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "external_item_id", "external_variant_id", "account_key", "item_code", "unit_code", "is_active",
			"scope_confirmed", "sales_enabled", "conversion_status", "quantity_multiplier", "stand", "divide",
			"mapping_revision", "generation", "set_hash", "catalog_ready",
		}).AddRow(
			"22222222-2222-4222-8222-222222222222", source.Items[0].ProductID, source.Items[0].SKUID,
			"shop:"+source.ShopID, "AH-0006", "แท่ง", true,
			true, true, "ready", int64(1), "1", "1", int64(1),
			"33333333-3333-4333-8333-333333333333", "", true,
		))
	mock.ExpectQuery("FROM channel_defaults").WillReturnRows(sqlmock.NewRows([]string{
		"endpoint", "doc_format_code", "shipping_item_enabled", "shipping_item_code", "shipping_item_unit_code",
	}).AddRow("/api/v1/ic/sale-invoices", "SI", false, "", ""))
	mock.ExpectQuery("FROM bills").WithArgs(source.Order.ID).WillReturnError(sql.ErrNoRows)

	loaded, err := NewTikTokBillShadowStore(database).Load(t.Context(), source.ShopID, source.Order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Mappings) != 1 || loaded.Mappings[0].AccountKey != "shop:"+source.ShopID || !loaded.Route.Configured || loaded.ExistingBill != nil {
		t.Fatalf("loaded source = %+v", loaded)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func controlledTikTokBillShadowSource() *TikTokBillShadowSource {
	syncedAt := time.Date(2026, 9, 13, 1, 55, 52, 0, time.UTC)
	return &TikTokBillShadowSource{
		ShopID: "7494619203789490654", ShopName: "henna_milkford",
		SourceHash:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		StoredOrderStatus: OrderStatusAwaitingCollection, StoredCurrency: "THB",
		StoredPaymentTotal: "307.49", StoredProductSubtotal: "300",
		StoredShippingFee: "0", StoredItemInsuranceFee: "7.49", LastSyncedAt: syncedAt,
		Order: Order{
			ID: "586030483469993439", Status: OrderStatusAwaitingCollection,
			Payment: OrderPayment{Currency: "THB", SubTotal: "300", ShippingFee: "0", ItemInsuranceFee: "7.49", TotalAmount: "307.49"},
			LineItems: []OrderLineItem{{
				ID: "line-1", ProductID: "1729429119195974110", SKUID: "1729429118580984286",
				ProductName: "สีเพ้นคิ้วเฮนน่า", SKUName: "02 - น้ำตาลเข้ม", Currency: "THB",
				OriginalPrice: "300", SalePrice: "300", SellerDiscount: "0", PlatformDiscount: "0",
			}},
		},
		Price: PriceDetail{Currency: "THB", Payment: "307.49", Subtotal: "300"},
		Items: []NormalizedTikTokOrderItem{{
			ProductID: "1729429119195974110", SKUID: "1729429118580984286",
			ProductName: "สีเพ้นคิ้วเฮนน่า", SKUName: "02 - น้ำตาลเข้ม", Quantity: 1, LineIDs: []string{"line-1"},
		}},
		Route: TikTokBillShadowRouteSource{
			Configured: true, Endpoint: "/api/v1/ic/sale-invoices", DocFormatCode: "SI",
		},
	}
}

func hasTikTokBillShadowBlocker(blockers []TikTokBillShadowBlocker, code string) bool {
	for _, blocker := range blockers {
		if blocker.Code == code {
			return true
		}
	}
	return false
}
