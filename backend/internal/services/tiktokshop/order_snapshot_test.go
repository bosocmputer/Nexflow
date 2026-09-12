package tiktokshop

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

type snapshotGatewayFake struct {
	details     *GatewayOrderDetailsResponse
	prices      map[string]*GatewayOrderPriceDetailResponse
	detailErr   error
	priceErr    error
	detailCalls int
	priceCalls  int
}

func (f *snapshotGatewayFake) GetOrderDetails(_ context.Context, _ GatewayOrderDetailsRequest) (*GatewayOrderDetailsResponse, error) {
	f.detailCalls++
	return f.details, f.detailErr
}

func (f *snapshotGatewayFake) GetPriceDetail(_ context.Context, input GatewayOrderPriceDetailRequest) (*GatewayOrderPriceDetailResponse, error) {
	f.priceCalls++
	if f.priceErr != nil {
		return nil, f.priceErr
	}
	return f.prices[input.OrderID], nil
}

type snapshotStoreFake struct {
	records []TikTokOrderSnapshotRecord
	err     error
	calls   int
}

func (f *snapshotStoreFake) UpsertBatch(_ context.Context, shopID string, records []TikTokOrderSnapshotRecord) error {
	f.calls++
	f.records = append([]TikTokOrderSnapshotRecord(nil), records...)
	return f.err
}

func TestBuildTikTokOrderSnapshotGroupsRepeatedSKUAndKeepsOnlySafeFields(t *testing.T) {
	var order Order
	if err := json.Unmarshal([]byte(`{
		"id":"585684843131602849","status":"COMPLETED","update_time":1789123000,
		"buyer_username":"must-not-persist","recipient_address":{"name":"Secret","phone_number":"0900000000"},
		"payment":{"currency":"THB","sub_total":"300","shipping_fee":"0","item_insurance_fee":"7.49","total_amount":"307.49"},
		"line_items":[
			{"id":"line-2","product_id":"prod-1","sku_id":"sku-1","seller_sku":"","product_name":"สินค้า A","sku_name":"แดง","currency":"THB","original_price":"150","sale_price":"150","seller_discount":"0","platform_discount":"0"},
			{"id":"line-1","product_id":"prod-1","sku_id":"sku-1","seller_sku":"","product_name":"สินค้า A","sku_name":"แดง","currency":"THB","original_price":"150","sale_price":"150","seller_discount":"0","platform_discount":"0"}
		]
	}`), &order); err != nil {
		t.Fatal(err)
	}
	price := PriceDetail{Currency: "THB", Payment: "307.49", Subtotal: "300", ShippingSalePrice: "0"}

	record, err := BuildTikTokOrderSnapshot(order, price, "detail-request", "price-request")
	if err != nil {
		t.Fatalf("BuildTikTokOrderSnapshot() error = %v", err)
	}
	if record.ItemCount != 2 || record.SKUCount != 1 || len(record.NormalizedItems) != 1 {
		t.Fatalf("counts = item:%d sku:%d normalized:%+v", record.ItemCount, record.SKUCount, record.NormalizedItems)
	}
	item := record.NormalizedItems[0]
	if item.Quantity != 2 || strings.Join(item.LineIDs, ",") != "line-1,line-2" || item.ProductID != "prod-1" || item.SKUID != "sku-1" {
		t.Fatalf("normalized item = %+v", item)
	}
	combined := strings.ToLower(string(record.SafeOrderJSON) + string(record.SafePriceDetailJSON))
	for _, forbidden := range []string{"buyer_username", "recipient_address", "phone_number", "secret", "0900000000"} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("safe payload contains %q: %s", forbidden, combined)
		}
	}
	if record.PaymentTotal != "307.49" || record.ProductSubtotal != "300" || record.ItemInsuranceFee != "7.49" || len(record.SourceHash) != 64 {
		t.Fatalf("amount/hash evidence = %+v", record)
	}
}

func TestBuildTikTokOrderSnapshotRejectsDuplicateLineIdentity(t *testing.T) {
	order := validSnapshotOrder("1001")
	order.LineItems = append(order.LineItems, order.LineItems[0])
	_, err := BuildTikTokOrderSnapshot(order, validSnapshotPrice("307.49"), "detail", "price")
	if !errors.Is(err, ErrInvalidSnapshotInput) {
		t.Fatalf("error = %v, want ErrInvalidSnapshotInput", err)
	}
}

func TestBuildTikTokOrderSnapshotRejectsEmptyOrderStatus(t *testing.T) {
	order := validSnapshotOrder("1001")
	order.Status = ""
	_, err := BuildTikTokOrderSnapshot(order, validSnapshotPrice("307.49"), "detail", "price")
	if !errors.Is(err, ErrInvalidSnapshotInput) {
		t.Fatalf("error = %v, want ErrInvalidSnapshotInput", err)
	}
}

func TestBuildTikTokOrderSnapshotHashIsStableWhenLineOrderChanges(t *testing.T) {
	order := validSnapshotOrder("1001")
	order.LineItems = append(order.LineItems, OrderLineItem{
		ID: "line-second", ProductID: "prod-2", SKUID: "sku-2", ProductName: "B", SKUName: "Blue",
		Currency: "THB", OriginalPrice: "0", SalePrice: "0",
	})
	first, err := BuildTikTokOrderSnapshot(order, validSnapshotPrice("307.49"), "detail", "price")
	if err != nil {
		t.Fatal(err)
	}
	order.LineItems[0], order.LineItems[1] = order.LineItems[1], order.LineItems[0]
	second, err := BuildTikTokOrderSnapshot(order, validSnapshotPrice("307.49"), "detail", "price")
	if err != nil {
		t.Fatal(err)
	}
	if first.SourceHash != second.SourceHash {
		t.Fatalf("source hash changed with line order: %s != %s", first.SourceHash, second.SourceHash)
	}
}

func TestBuildTikTokOrderSnapshotRejectsAmountBeyondDatabasePrecision(t *testing.T) {
	order := validSnapshotOrder("1001")
	order.Payment.TotalAmount = "1234567890123456789.00"
	_, err := BuildTikTokOrderSnapshot(order, validSnapshotPrice("1234567890123456789.00"), "detail", "price")
	if !errors.Is(err, ErrInvalidSnapshotInput) {
		t.Fatalf("error = %v, want ErrInvalidSnapshotInput", err)
	}
}

func TestTikTokOrderSnapshotServiceClassifiesMalformedUpstreamAsSourceFailure(t *testing.T) {
	malformed := validSnapshotOrder("1001")
	malformed.Status = ""
	gateway := &snapshotGatewayFake{
		details: &GatewayOrderDetailsResponse{UpstreamRequestID: "detail", Orders: []Order{malformed}},
		prices:  map[string]*GatewayOrderPriceDetailResponse{"1001": {UpstreamRequestID: "price", PriceDetail: ptrPrice(validSnapshotPrice("307.49"))}},
	}
	store := &snapshotStoreFake{}
	service := NewOrderSnapshotService(gateway, store)

	_, err := service.Sync(t.Context(), TikTokOrderSnapshotRequest{ShopID: "7000714532876273420", OrderIDs: []string{"1001"}})
	if !errors.Is(err, ErrSnapshotSourceInvalid) || store.calls != 0 {
		t.Fatalf("error=%v store_calls=%d", err, store.calls)
	}
}

func TestTikTokOrderSnapshotServiceFailsBeforeStoreWhenAmountsDisagree(t *testing.T) {
	gateway := &snapshotGatewayFake{
		details: &GatewayOrderDetailsResponse{UpstreamRequestID: "detail", Orders: []Order{validSnapshotOrder("1001")}},
		prices:  map[string]*GatewayOrderPriceDetailResponse{"1001": {UpstreamRequestID: "price", PriceDetail: ptrPrice(validSnapshotPrice("999.00"))}},
	}
	store := &snapshotStoreFake{}
	service := NewOrderSnapshotService(gateway, store)

	_, err := service.Sync(t.Context(), TikTokOrderSnapshotRequest{ShopID: "7000714532876273420", OrderIDs: []string{"1001"}})
	if !errors.Is(err, ErrSnapshotAmountMismatch) || store.calls != 0 {
		t.Fatalf("error=%v store_calls=%d", err, store.calls)
	}
}

func TestTikTokOrderSnapshotServicePersistsOneAtomicBatch(t *testing.T) {
	gateway := &snapshotGatewayFake{
		details: &GatewayOrderDetailsResponse{UpstreamRequestID: "detail", Orders: []Order{validSnapshotOrder("1002"), validSnapshotOrder("1001")}},
		prices: map[string]*GatewayOrderPriceDetailResponse{
			"1001": {UpstreamRequestID: "price-1", PriceDetail: ptrPrice(validSnapshotPrice("307.49"))},
			"1002": {UpstreamRequestID: "price-2", PriceDetail: ptrPrice(validSnapshotPrice("307.49"))},
		},
	}
	store := &snapshotStoreFake{}
	service := NewOrderSnapshotService(gateway, store)

	result, err := service.Sync(t.Context(), TikTokOrderSnapshotRequest{ShopID: "7000714532876273420", OrderIDs: []string{"1001", "1002"}})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if store.calls != 1 || len(store.records) != 2 || store.records[0].OrderID != "1001" || store.records[1].OrderID != "1002" {
		t.Fatalf("store calls=%d records=%+v", store.calls, store.records)
	}
	if result.SyncedCount != 2 || gateway.detailCalls != 1 || gateway.priceCalls != 2 {
		t.Fatalf("result=%+v gateway detail=%d price=%d", result, gateway.detailCalls, gateway.priceCalls)
	}
}

func TestTikTokOrderSnapshotStoreUpsertsBatchInOneTransaction(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	record1, err := BuildTikTokOrderSnapshot(validSnapshotOrder("1001"), validSnapshotPrice("307.49"), "detail", "price-1")
	if err != nil {
		t.Fatal(err)
	}
	record2, err := BuildTikTokOrderSnapshot(validSnapshotOrder("1002"), validSnapshotPrice("307.49"), "detail", "price-2")
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT gateway_connection_id::text").WithArgs("7000714532876273420").
		WillReturnRows(sqlmock.NewRows([]string{"gateway_connection_id"}).AddRow("11111111-1111-4111-8111-111111111111"))
	mock.ExpectExec("INSERT INTO tiktok_shop_order_snapshots").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO tiktok_shop_order_snapshots").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err = NewTikTokOrderSnapshotStore(database).UpsertBatch(t.Context(), "7000714532876273420", []TikTokOrderSnapshotRecord{record1, record2})
	if err != nil {
		t.Fatalf("UpsertBatch() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTikTokOrderSnapshotStoreRollsBackWholeBatchOnOneFailure(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	record1, _ := BuildTikTokOrderSnapshot(validSnapshotOrder("1001"), validSnapshotPrice("307.49"), "detail", "price-1")
	record2, _ := BuildTikTokOrderSnapshot(validSnapshotOrder("1002"), validSnapshotPrice("307.49"), "detail", "price-2")
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT gateway_connection_id::text").WithArgs("7000714532876273420").
		WillReturnRows(sqlmock.NewRows([]string{"gateway_connection_id"}).AddRow("11111111-1111-4111-8111-111111111111"))
	mock.ExpectExec("INSERT INTO tiktok_shop_order_snapshots").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO tiktok_shop_order_snapshots").WillReturnError(errors.New("write failed"))
	mock.ExpectRollback()

	err = NewTikTokOrderSnapshotStore(database).UpsertBatch(t.Context(), "7000714532876273420", []TikTokOrderSnapshotRecord{record1, record2})
	if err == nil || !strings.Contains(err.Error(), "1002") {
		t.Fatalf("UpsertBatch() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTikTokOrderSnapshotStoreListsBoundedPIIMinimizedOperationsRows(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	updatedAt := time.Date(2026, 9, 12, 3, 30, 0, 0, time.UTC)
	syncedAt := updatedAt.Add(time.Minute)
	mock.ExpectQuery("SELECT COUNT\\(\\*\\)").
		WithArgs("7494619203789490654", "", "completed", "5856%").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(1)))
	mock.ExpectQuery("COUNT\\(\\*\\) FILTER").
		WithArgs("7494619203789490654", "5856%").
		WillReturnRows(sqlmock.NewRows([]string{"total", "unpaid", "to_ship", "shipping", "completed", "cancelled"}).
			AddRow(int64(4), int64(0), int64(1), int64(2), int64(1), int64(0)))
	mock.ExpectQuery("SELECT s.shop_id, c.shop_name").
		WithArgs("7494619203789490654", "", "completed", "5856%", 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"shop_id", "shop_name", "order_id", "order_status", "currency",
			"payment_total_amount", "product_subtotal_amount", "shipping_fee_amount", "item_insurance_fee_amount",
			"item_count", "sku_count", "last_order_update_at", "last_synced_at",
		}).AddRow(
			"7494619203789490654", "henna_milkford", "585684843131602849", "COMPLETED", "THB",
			"307.49", "300", "0", "7.49", 1, 1, updatedAt, syncedAt,
		))

	result, err := NewTikTokOrderSnapshotStore(database).List(t.Context(), TikTokOrderSnapshotListFilter{
		ShopID: "7494619203789490654", StatusGroup: TikTokOrderStatusGroupCompleted, OrderIDPrefix: "5856", Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if result.TotalItems != 1 || len(result.Data) != 1 {
		t.Fatalf("List() result = %+v", result)
	}
	if result.StatusCounts.Total != 4 || result.StatusCounts.ToShip != 1 || result.StatusCounts.Shipping != 2 || result.StatusCounts.Completed != 1 {
		t.Fatalf("List() status counts = %+v", result.StatusCounts)
	}
	row := result.Data[0]
	if row.ShopName != "henna_milkford" || row.PaymentTotalAmount != "307.49" || row.ItemInsuranceFeeAmount != "7.49" {
		t.Fatalf("List() row = %+v", row)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"safe_order", "safe_price_detail", "normalized_items", "request_id", "source_hash", "buyer", "recipient", "phone", "email", "address"} {
		if strings.Contains(strings.ToLower(string(encoded)), forbidden) {
			t.Fatalf("List() leaked %q in %s", forbidden, encoded)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTikTokOrderSnapshotStoreKeepsExactTotalOnEmptyPage(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	mock.ExpectQuery("SELECT COUNT\\(\\*\\)").WithArgs("", "", "", "").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(4)))
	mock.ExpectQuery("COUNT\\(\\*\\) FILTER").WithArgs("", "").
		WillReturnRows(sqlmock.NewRows([]string{"total", "unpaid", "to_ship", "shipping", "completed", "cancelled"}).AddRow(4, 0, 1, 2, 1, 0))
	result, err := NewTikTokOrderSnapshotStore(database).List(t.Context(), TikTokOrderSnapshotListFilter{Page: 2, PageSize: 20})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if result.TotalItems != 4 || result.TotalPages != 1 || len(result.Data) != 0 {
		t.Fatalf("List() result = %+v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTikTokOrderSnapshotStoreRejectsUnboundedOrMalformedListFilters(t *testing.T) {
	database, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	store := NewTikTokOrderSnapshotStore(database)
	for _, filter := range []TikTokOrderSnapshotListFilter{
		{Page: 0, PageSize: 20},
		{Page: 1, PageSize: 51},
		{ShopID: "not-a-shop", Page: 1, PageSize: 20},
		{OrderIDPrefix: "5856%' OR true --", Page: 1, PageSize: 20},
		{Status: "UNKNOWN", Page: 1, PageSize: 20},
		{StatusGroup: "unknown", Page: 1, PageSize: 20},
	} {
		if _, err := store.List(t.Context(), filter); !errors.Is(err, ErrInvalidSnapshotListFilter) {
			t.Fatalf("List(%+v) error = %v", filter, err)
		}
	}
}

func validSnapshotOrder(orderID string) Order {
	return Order{
		ID: orderID, Status: OrderStatusCompleted, UpdateTime: 1789123000,
		Payment:   OrderPayment{Currency: "THB", SubTotal: "300", ShippingFee: "0", ItemInsuranceFee: "7.49", TotalAmount: "307.49"},
		LineItems: []OrderLineItem{{ID: "line-" + orderID, ProductID: "prod-1", SKUID: "sku-1", ProductName: "A", SKUName: "Red", Currency: "THB", OriginalPrice: "300", SalePrice: "300"}},
	}
}

func validSnapshotPrice(payment string) PriceDetail {
	return PriceDetail{Currency: "THB", Payment: payment, Subtotal: "300", ShippingSalePrice: "0"}
}

func ptrPrice(value PriceDetail) *PriceDetail { return &value }
