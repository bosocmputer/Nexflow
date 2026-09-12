package tiktokshop

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"
)

const maxTikTokOrderSnapshotBatch = 20

var (
	ErrInvalidSnapshotInput       = errors.New("invalid TikTok Shop order snapshot input")
	ErrSnapshotAmountMismatch     = errors.New("TikTok Shop order and price detail amounts do not match")
	ErrSnapshotSourceInvalid      = errors.New("TikTok Shop order snapshot source is invalid")
	ErrSnapshotSourceUnavailable  = errors.New("TikTok Shop order snapshot source is unavailable")
	ErrSnapshotPersistenceFailed  = errors.New("TikTok Shop order snapshot persistence failed")
	ErrSnapshotStoreNotConfigured = errors.New("TikTok Shop order snapshot store is not configured")
	ErrInvalidSnapshotListFilter  = errors.New("invalid TikTok Shop order snapshot list filter")
	tikTokNumericIDPattern        = regexp.MustCompile(`^[0-9]{1,32}$`)
	tikTokMoneyPattern            = regexp.MustCompile(`^(0|[1-9][0-9]{0,17})(\.[0-9]{1,6})?$`)
)

type TikTokOrderSnapshotRequest struct {
	ShopID   string   `json:"shop_id"`
	OrderIDs []string `json:"order_ids"`
}

type TikTokOrderSnapshotSummary struct {
	OrderID              string      `json:"order_id"`
	OrderStatus          OrderStatus `json:"order_status"`
	ItemCount            int         `json:"item_count"`
	SKUCount             int         `json:"sku_count"`
	SourceHash           string      `json:"source_hash"`
	DetailRequestID      string      `json:"detail_request_id"`
	PriceDetailRequestID string      `json:"price_detail_request_id"`
}

type TikTokOrderSnapshotResult struct {
	ShopID      string                       `json:"shop_id"`
	SyncedCount int                          `json:"synced_count"`
	Snapshots   []TikTokOrderSnapshotSummary `json:"snapshots"`
}

type NormalizedTikTokOrderItem struct {
	ProductID   string   `json:"product_id"`
	SKUID       string   `json:"sku_id"`
	SellerSKU   string   `json:"seller_sku,omitempty"`
	ProductName string   `json:"product_name"`
	SKUName     string   `json:"sku_name"`
	Quantity    int      `json:"quantity"`
	LineIDs     []string `json:"line_ids"`
}

type TikTokOrderSnapshotRecord struct {
	OrderID              string
	OrderStatus          OrderStatus
	Currency             string
	PaymentTotal         string
	ProductSubtotal      string
	ShippingFee          string
	ItemInsuranceFee     string
	ItemCount            int
	SKUCount             int
	SafeOrderJSON        json.RawMessage
	SafePriceDetailJSON  json.RawMessage
	NormalizedItems      []NormalizedTikTokOrderItem
	NormalizedItemsJSON  json.RawMessage
	DetailRequestID      string
	PriceDetailRequestID string
	SourceHash           string
	LastOrderUpdateAt    *time.Time
}

type TikTokOrderSnapshotListFilter struct {
	ShopID        string
	Status        OrderStatus
	OrderIDPrefix string
	Page          int
	PageSize      int
}

type TikTokOrderSnapshotListItem struct {
	ShopID                 string      `json:"shop_id"`
	ShopName               string      `json:"shop_name"`
	OrderID                string      `json:"order_id"`
	OrderStatus            OrderStatus `json:"order_status"`
	Currency               string      `json:"currency"`
	PaymentTotalAmount     string      `json:"payment_total_amount"`
	ProductSubtotalAmount  string      `json:"product_subtotal_amount"`
	ShippingFeeAmount      string      `json:"shipping_fee_amount"`
	ItemInsuranceFeeAmount string      `json:"item_insurance_fee_amount"`
	ItemCount              int         `json:"item_count"`
	SKUCount               int         `json:"sku_count"`
	LastOrderUpdateAt      *time.Time  `json:"last_order_update_at,omitempty"`
	LastSyncedAt           time.Time   `json:"last_synced_at"`
}

type TikTokOrderSnapshotListResult struct {
	Data       []TikTokOrderSnapshotListItem `json:"data"`
	Page       int                           `json:"page"`
	PageSize   int                           `json:"page_size"`
	TotalItems int64                         `json:"total_items"`
	TotalPages int                           `json:"total_pages"`
}

type orderSnapshotGateway interface {
	GetOrderDetails(context.Context, GatewayOrderDetailsRequest) (*GatewayOrderDetailsResponse, error)
	GetPriceDetail(context.Context, GatewayOrderPriceDetailRequest) (*GatewayOrderPriceDetailResponse, error)
}

type orderSnapshotBatchStore interface {
	UpsertBatch(context.Context, string, []TikTokOrderSnapshotRecord) error
}

type OrderSnapshotService struct {
	gateway orderSnapshotGateway
	store   orderSnapshotBatchStore
}

func NewOrderSnapshotService(gateway orderSnapshotGateway, store orderSnapshotBatchStore) *OrderSnapshotService {
	return &OrderSnapshotService{gateway: gateway, store: store}
}

func (s *OrderSnapshotService) Sync(ctx context.Context, input TikTokOrderSnapshotRequest) (*TikTokOrderSnapshotResult, error) {
	shopID, orderIDs, err := normalizeSnapshotRequest(input)
	if err != nil || s == nil || s.gateway == nil || s.store == nil {
		return nil, ErrInvalidSnapshotInput
	}
	detail, err := s.gateway.GetOrderDetails(ctx, GatewayOrderDetailsRequest{ShopID: shopID, OrderIDs: orderIDs})
	if err != nil {
		return nil, fmt.Errorf("%w: load order detail: %v", ErrSnapshotSourceUnavailable, err)
	}
	if detail == nil || strings.TrimSpace(detail.UpstreamRequestID) == "" || len(detail.Orders) != len(orderIDs) {
		return nil, ErrSnapshotSourceInvalid
	}
	orders := make(map[string]Order, len(detail.Orders))
	for _, order := range detail.Orders {
		order.ID = strings.TrimSpace(order.ID)
		if _, expected := findString(orderIDs, order.ID); !expected || order.Status == "" || !validOrderStatus(order.Status) {
			return nil, ErrSnapshotSourceInvalid
		}
		if _, duplicate := orders[order.ID]; duplicate {
			return nil, ErrSnapshotSourceInvalid
		}
		orders[order.ID] = order
	}

	records := make([]TikTokOrderSnapshotRecord, 0, len(orderIDs))
	for _, orderID := range orderIDs {
		priceResponse, err := s.gateway.GetPriceDetail(ctx, GatewayOrderPriceDetailRequest{ShopID: shopID, OrderID: orderID})
		if err != nil {
			return nil, fmt.Errorf("%w: load price detail: %v", ErrSnapshotSourceUnavailable, err)
		}
		if priceResponse == nil || priceResponse.PriceDetail == nil {
			return nil, ErrSnapshotSourceInvalid
		}
		record, err := BuildTikTokOrderSnapshot(orders[orderID], *priceResponse.PriceDetail, detail.UpstreamRequestID, priceResponse.UpstreamRequestID)
		if err != nil {
			if errors.Is(err, ErrSnapshotAmountMismatch) {
				return nil, err
			}
			return nil, fmt.Errorf("%w: %v", ErrSnapshotSourceInvalid, err)
		}
		records = append(records, record)
	}
	if err := s.store.UpsertBatch(ctx, shopID, records); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSnapshotPersistenceFailed, err)
	}
	result := &TikTokOrderSnapshotResult{ShopID: shopID, SyncedCount: len(records), Snapshots: make([]TikTokOrderSnapshotSummary, 0, len(records))}
	for _, record := range records {
		result.Snapshots = append(result.Snapshots, TikTokOrderSnapshotSummary{
			OrderID: record.OrderID, OrderStatus: record.OrderStatus, ItemCount: record.ItemCount,
			SKUCount: record.SKUCount, SourceHash: record.SourceHash,
			DetailRequestID: record.DetailRequestID, PriceDetailRequestID: record.PriceDetailRequestID,
		})
	}
	return result, nil
}

func BuildTikTokOrderSnapshot(order Order, price PriceDetail, detailRequestID, priceRequestID string) (TikTokOrderSnapshotRecord, error) {
	order.ID = strings.TrimSpace(order.ID)
	detailRequestID = strings.TrimSpace(detailRequestID)
	priceRequestID = strings.TrimSpace(priceRequestID)
	currency := strings.ToUpper(strings.TrimSpace(order.Payment.Currency))
	priceCurrency := strings.ToUpper(strings.TrimSpace(price.Currency))
	if order.ID == "" || order.Status == "" || !validOrderStatus(order.Status) || currency == "" || priceCurrency == "" ||
		currency != priceCurrency || detailRequestID == "" || priceRequestID == "" {
		return TikTokOrderSnapshotRecord{}, ErrInvalidSnapshotInput
	}
	paymentTotal, paymentValue, err := snapshotMoney(order.Payment.TotalAmount, true)
	if err != nil {
		return TikTokOrderSnapshotRecord{}, err
	}
	_, priceValue, err := snapshotMoney(price.Payment, true)
	if err != nil {
		return TikTokOrderSnapshotRecord{}, err
	}
	if paymentValue.Cmp(priceValue) != 0 {
		return TikTokOrderSnapshotRecord{}, ErrSnapshotAmountMismatch
	}
	productSubtotal, _, err := snapshotMoney(order.Payment.SubTotal, false)
	if err != nil {
		return TikTokOrderSnapshotRecord{}, err
	}
	shippingFee, _, err := snapshotMoney(order.Payment.ShippingFee, false)
	if err != nil {
		return TikTokOrderSnapshotRecord{}, err
	}
	itemInsuranceFee, _, err := snapshotMoney(order.Payment.ItemInsuranceFee, false)
	if err != nil {
		return TikTokOrderSnapshotRecord{}, err
	}
	normalized, err := normalizeTikTokOrderItems(order.LineItems)
	if err != nil {
		return TikTokOrderSnapshotRecord{}, err
	}
	// TikTok models quantity as repeated line entries. Canonicalize their order
	// so harmless upstream array reordering does not change the content hash.
	safeOrderValue := order
	safeOrderValue.LineItems = append([]OrderLineItem(nil), order.LineItems...)
	sort.Slice(safeOrderValue.LineItems, func(i, j int) bool {
		return strings.TrimSpace(safeOrderValue.LineItems[i].ID) < strings.TrimSpace(safeOrderValue.LineItems[j].ID)
	})
	safeOrderValue.Packages = append([]OrderPackage(nil), order.Packages...)
	sort.Slice(safeOrderValue.Packages, func(i, j int) bool {
		return strings.TrimSpace(safeOrderValue.Packages[i].ID) < strings.TrimSpace(safeOrderValue.Packages[j].ID)
	})
	safeOrder, err := json.Marshal(safeOrderValue)
	if err != nil {
		return TikTokOrderSnapshotRecord{}, err
	}
	safePrice, err := json.Marshal(price)
	if err != nil {
		return TikTokOrderSnapshotRecord{}, err
	}
	normalizedJSON, err := json.Marshal(normalized)
	if err != nil {
		return TikTokOrderSnapshotRecord{}, err
	}
	hashInput, err := json.Marshal(struct {
		Order json.RawMessage `json:"order"`
		Price json.RawMessage `json:"price_detail"`
		Items json.RawMessage `json:"normalized_items"`
	}{safeOrder, safePrice, normalizedJSON})
	if err != nil {
		return TikTokOrderSnapshotRecord{}, err
	}
	digest := sha256.Sum256(hashInput)
	var lastUpdate *time.Time
	if order.UpdateTime > 0 {
		value := time.Unix(order.UpdateTime, 0).UTC()
		lastUpdate = &value
	}
	return TikTokOrderSnapshotRecord{
		OrderID: order.ID, OrderStatus: order.Status, Currency: currency,
		PaymentTotal: paymentTotal, ProductSubtotal: productSubtotal, ShippingFee: shippingFee,
		ItemInsuranceFee: itemInsuranceFee, ItemCount: len(order.LineItems), SKUCount: len(normalized),
		SafeOrderJSON: safeOrder, SafePriceDetailJSON: safePrice, NormalizedItems: normalized,
		NormalizedItemsJSON: normalizedJSON, DetailRequestID: detailRequestID,
		PriceDetailRequestID: priceRequestID, SourceHash: hex.EncodeToString(digest[:]), LastOrderUpdateAt: lastUpdate,
	}, nil
}

func normalizeTikTokOrderItems(lineItems []OrderLineItem) ([]NormalizedTikTokOrderItem, error) {
	if len(lineItems) == 0 || len(lineItems) > 1000 {
		return nil, ErrInvalidSnapshotInput
	}
	grouped := make(map[string]*NormalizedTikTokOrderItem, len(lineItems))
	lineIDs := make(map[string]struct{}, len(lineItems))
	for _, line := range lineItems {
		line.ID = strings.TrimSpace(line.ID)
		line.ProductID = strings.TrimSpace(line.ProductID)
		line.SKUID = strings.TrimSpace(line.SKUID)
		if line.ID == "" || line.ProductID == "" || line.SKUID == "" {
			return nil, ErrInvalidSnapshotInput
		}
		if _, duplicate := lineIDs[line.ID]; duplicate {
			return nil, ErrInvalidSnapshotInput
		}
		lineIDs[line.ID] = struct{}{}
		key := line.ProductID + "\x00" + line.SKUID
		item := grouped[key]
		if item == nil {
			item = &NormalizedTikTokOrderItem{
				ProductID: line.ProductID, SKUID: line.SKUID, SellerSKU: strings.TrimSpace(line.SellerSKU),
				ProductName: strings.TrimSpace(line.ProductName), SKUName: strings.TrimSpace(line.SKUName),
			}
			grouped[key] = item
		} else if item.SellerSKU == "" {
			item.SellerSKU = strings.TrimSpace(line.SellerSKU)
		}
		item.Quantity++
		item.LineIDs = append(item.LineIDs, line.ID)
	}
	items := make([]NormalizedTikTokOrderItem, 0, len(grouped))
	for _, item := range grouped {
		sort.Strings(item.LineIDs)
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].ProductID == items[j].ProductID {
			return items[i].SKUID < items[j].SKUID
		}
		return items[i].ProductID < items[j].ProductID
	})
	return items, nil
}

func snapshotMoney(raw string, required bool) (string, *big.Rat, error) {
	value := strings.TrimSpace(raw)
	if value == "" && !required {
		value = "0"
	}
	if !tikTokMoneyPattern.MatchString(value) {
		return "", nil, ErrInvalidSnapshotInput
	}
	number, ok := new(big.Rat).SetString(value)
	if !ok || number.Sign() < 0 {
		return "", nil, ErrInvalidSnapshotInput
	}
	return value, number, nil
}

func normalizeSnapshotRequest(input TikTokOrderSnapshotRequest) (string, []string, error) {
	shopID := strings.TrimSpace(input.ShopID)
	if !tikTokNumericIDPattern.MatchString(shopID) || len(input.OrderIDs) == 0 || len(input.OrderIDs) > maxTikTokOrderSnapshotBatch {
		return "", nil, ErrInvalidSnapshotInput
	}
	orderIDs := make([]string, len(input.OrderIDs))
	seen := make(map[string]struct{}, len(input.OrderIDs))
	for index, orderID := range input.OrderIDs {
		orderID = strings.TrimSpace(orderID)
		if !tikTokNumericIDPattern.MatchString(orderID) {
			return "", nil, ErrInvalidSnapshotInput
		}
		if _, duplicate := seen[orderID]; duplicate {
			return "", nil, ErrInvalidSnapshotInput
		}
		seen[orderID] = struct{}{}
		orderIDs[index] = orderID
	}
	return shopID, orderIDs, nil
}

func findString(values []string, target string) (int, bool) {
	for index, value := range values {
		if value == target {
			return index, true
		}
	}
	return -1, false
}

type TikTokOrderSnapshotStore struct {
	database *sql.DB
}

func NewTikTokOrderSnapshotStore(database *sql.DB) *TikTokOrderSnapshotStore {
	return &TikTokOrderSnapshotStore{database: database}
}

func (s *TikTokOrderSnapshotStore) UpsertBatch(ctx context.Context, shopID string, records []TikTokOrderSnapshotRecord) error {
	if s == nil || s.database == nil {
		return ErrSnapshotStoreNotConfigured
	}
	shopID = strings.TrimSpace(shopID)
	if !tikTokNumericIDPattern.MatchString(shopID) || len(records) == 0 || len(records) > maxTikTokOrderSnapshotBatch {
		return ErrInvalidSnapshotInput
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var connectionID string
	if err := tx.QueryRowContext(ctx,
		`SELECT gateway_connection_id::text
		   FROM tiktok_shop_connections
		  WHERE shop_id = $1 AND disabled_at IS NULL
		  FOR SHARE`, shopID,
	).Scan(&connectionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalidSnapshotInput
		}
		return err
	}
	for _, record := range records {
		if record.OrderID == "" || record.ItemCount < 1 || record.SKUCount < 1 || len(record.SourceHash) != 64 ||
			len(record.SafeOrderJSON) == 0 || len(record.SafePriceDetailJSON) == 0 || len(record.NormalizedItemsJSON) == 0 {
			return ErrInvalidSnapshotInput
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO tiktok_shop_order_snapshots
			  (gateway_connection_id, shop_id, order_id, order_status, currency,
			   payment_total_amount, product_subtotal_amount, shipping_fee_amount, item_insurance_fee_amount,
			   item_count, sku_count, safe_order, safe_price_detail, normalized_items,
			   detail_request_id, price_detail_request_id, source_hash, last_order_update_at,
			   last_synced_at, updated_at)
			 VALUES ($1::uuid, $2, $3, $4, $5, $6::numeric, $7::numeric, $8::numeric, $9::numeric,
			         $10, $11, $12::jsonb, $13::jsonb, $14::jsonb, $15, $16, $17, $18, NOW(), NOW())
			 ON CONFLICT (shop_id, order_id) DO UPDATE
			    SET gateway_connection_id = EXCLUDED.gateway_connection_id,
			        order_status = EXCLUDED.order_status,
			        currency = EXCLUDED.currency,
			        payment_total_amount = EXCLUDED.payment_total_amount,
			        product_subtotal_amount = EXCLUDED.product_subtotal_amount,
			        shipping_fee_amount = EXCLUDED.shipping_fee_amount,
			        item_insurance_fee_amount = EXCLUDED.item_insurance_fee_amount,
			        item_count = EXCLUDED.item_count,
			        sku_count = EXCLUDED.sku_count,
			        safe_order = EXCLUDED.safe_order,
			        safe_price_detail = EXCLUDED.safe_price_detail,
			        normalized_items = EXCLUDED.normalized_items,
			        detail_request_id = EXCLUDED.detail_request_id,
			        price_detail_request_id = EXCLUDED.price_detail_request_id,
			        source_hash = EXCLUDED.source_hash,
			        last_order_update_at = COALESCE(EXCLUDED.last_order_update_at, tiktok_shop_order_snapshots.last_order_update_at),
			        last_synced_at = NOW(),
			        updated_at = NOW()`,
			connectionID, shopID, record.OrderID, string(record.OrderStatus), record.Currency,
			record.PaymentTotal, record.ProductSubtotal, record.ShippingFee, record.ItemInsuranceFee,
			record.ItemCount, record.SKUCount, []byte(record.SafeOrderJSON), []byte(record.SafePriceDetailJSON),
			[]byte(record.NormalizedItemsJSON), record.DetailRequestID, record.PriceDetailRequestID,
			record.SourceHash, record.LastOrderUpdateAt,
		)
		if err != nil {
			return fmt.Errorf("upsert TikTok Shop order snapshot %s: %w", record.OrderID, err)
		}
	}
	return tx.Commit()
}

func (s *TikTokOrderSnapshotStore) List(ctx context.Context, filter TikTokOrderSnapshotListFilter) (*TikTokOrderSnapshotListResult, error) {
	filter.ShopID = strings.TrimSpace(filter.ShopID)
	filter.OrderIDPrefix = strings.TrimSpace(filter.OrderIDPrefix)
	filter.Status = OrderStatus(strings.TrimSpace(string(filter.Status)))
	if s == nil || s.database == nil || filter.Page < 1 || filter.Page > 1000000 || filter.PageSize < 1 || filter.PageSize > 50 ||
		(filter.ShopID != "" && !tikTokNumericIDPattern.MatchString(filter.ShopID)) ||
		(filter.OrderIDPrefix != "" && !tikTokNumericIDPattern.MatchString(filter.OrderIDPrefix)) ||
		(filter.Status != "" && !validOrderStatus(filter.Status)) {
		return nil, ErrInvalidSnapshotListFilter
	}
	offset := (filter.Page - 1) * filter.PageSize
	result := &TikTokOrderSnapshotListResult{Data: []TikTokOrderSnapshotListItem{}, Page: filter.Page, PageSize: filter.PageSize}
	if err := s.database.QueryRowContext(ctx,
		`SELECT COUNT(*)
		   FROM tiktok_shop_order_snapshots s
		   JOIN tiktok_shop_connections c ON c.shop_id = s.shop_id AND c.disabled_at IS NULL
		  WHERE ($1 = '' OR s.shop_id = $1)
		    AND ($2 = '' OR s.order_status = $2)
		    AND ($3 = '' OR s.order_id LIKE $3)`,
		filter.ShopID, string(filter.Status), listOrderIDPrefix(filter.OrderIDPrefix),
	).Scan(&result.TotalItems); err != nil {
		return nil, err
	}
	if result.TotalItems > 0 {
		result.TotalPages = int((result.TotalItems + int64(filter.PageSize) - 1) / int64(filter.PageSize))
	}
	if int64(offset) >= result.TotalItems {
		return result, nil
	}
	rows, err := s.database.QueryContext(ctx,
		`SELECT s.shop_id, c.shop_name, s.order_id, s.order_status, s.currency,
		        s.payment_total_amount::text, s.product_subtotal_amount::text,
		        s.shipping_fee_amount::text, s.item_insurance_fee_amount::text,
		        s.item_count, s.sku_count, s.last_order_update_at, s.last_synced_at
		   FROM tiktok_shop_order_snapshots s
		   JOIN tiktok_shop_connections c ON c.shop_id = s.shop_id AND c.disabled_at IS NULL
		  WHERE ($1 = '' OR s.shop_id = $1)
		    AND ($2 = '' OR s.order_status = $2)
		    AND ($3 = '' OR s.order_id LIKE $3)
		  ORDER BY s.last_synced_at DESC, s.order_id DESC
		  LIMIT $4 OFFSET $5`,
		filter.ShopID, string(filter.Status), listOrderIDPrefix(filter.OrderIDPrefix), filter.PageSize, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item TikTokOrderSnapshotListItem
		var lastUpdate sql.NullTime
		if err := rows.Scan(
			&item.ShopID, &item.ShopName, &item.OrderID, &item.OrderStatus, &item.Currency,
			&item.PaymentTotalAmount, &item.ProductSubtotalAmount, &item.ShippingFeeAmount, &item.ItemInsuranceFeeAmount,
			&item.ItemCount, &item.SKUCount, &lastUpdate, &item.LastSyncedAt,
		); err != nil {
			return nil, err
		}
		if lastUpdate.Valid {
			value := lastUpdate.Time.UTC()
			item.LastOrderUpdateAt = &value
		}
		item.LastSyncedAt = item.LastSyncedAt.UTC()
		result.Data = append(result.Data, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func listOrderIDPrefix(value string) string {
	if value == "" {
		return ""
	}
	return value + "%"
}
