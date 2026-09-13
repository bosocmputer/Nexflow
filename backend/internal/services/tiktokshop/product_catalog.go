package tiktokshop

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

const (
	maxTikTokCatalogPages    = 100
	maxTikTokCatalogProducts = 10_000
	maxInventorySearchSKUs   = 600
)

var (
	ErrProductCatalogNotConfigured  = errors.New("TikTok Shop product catalog is not configured")
	ErrProductCatalogInvalidInput   = errors.New("invalid TikTok Shop product catalog input")
	ErrProductCatalogSourceInvalid  = errors.New("invalid TikTok Shop product catalog source")
	ErrProductCatalogSyncInProgress = errors.New("TikTok Shop product catalog sync is already running")
)

type TikTokCatalogGateway interface {
	SearchProducts(context.Context, GatewayProductSearchRequest) (*GatewayProductSearchResponse, error)
	SearchInventory(context.Context, GatewayInventorySearchRequest) (*GatewayInventorySearchResponse, error)
}

type TikTokCatalogStore interface {
	CreateRun(context.Context, string, string) (string, error)
	ReplaceCatalog(context.Context, TikTokCatalogRunResult, []TikTokCatalogProduct) error
	FinishRun(context.Context, TikTokCatalogRunResult) error
}

type TikTokCatalogProduct struct {
	Product   Product
	Inventory map[string]InventorySearchSKU
}

type TikTokCatalogRunResult struct {
	ID                 string    `json:"id"`
	ShopID             string    `json:"shop_id"`
	Status             string    `json:"status"`
	TriggerSource      string    `json:"trigger_source"`
	PageCount          int       `json:"page_count"`
	ProductCount       int       `json:"product_count"`
	SKUCount           int       `json:"sku_count"`
	WarehouseCount     int       `json:"warehouse_count"`
	UpstreamRequestIDs []string  `json:"upstream_request_ids"`
	ErrorCode          string    `json:"error_code,omitempty"`
	StartedAt          time.Time `json:"started_at,omitempty"`
	FinishedAt         time.Time `json:"finished_at,omitempty"`
}

type TikTokCatalogListFilter struct {
	ShopID   string
	Query    string
	Status   ProductStatus
	Page     int
	PageSize int
}

type TikTokCatalogInventoryView struct {
	WarehouseID       string `json:"warehouse_id"`
	AvailableQuantity int64  `json:"available_quantity"`
	CommittedQuantity int64  `json:"committed_quantity"`
}

type TikTokCatalogListItem struct {
	ShopID                 string                       `json:"shop_id"`
	ShopName               string                       `json:"shop_name"`
	ProductID              string                       `json:"product_id"`
	ProductTitle           string                       `json:"product_title"`
	ProductStatus          ProductStatus                `json:"product_status"`
	SKUID                  string                       `json:"sku_id"`
	SellerSKU              string                       `json:"seller_sku"`
	Price                  ProductPrice                 `json:"price"`
	TotalAvailableQuantity int64                        `json:"total_available_quantity"`
	TotalCommittedQuantity int64                        `json:"total_committed_quantity"`
	Inventory              []TikTokCatalogInventoryView `json:"inventory"`
	LastSeenAt             time.Time                    `json:"last_seen_at"`
}

type TikTokCatalogListResult struct {
	Data       []TikTokCatalogListItem `json:"data"`
	Page       int                     `json:"page"`
	PageSize   int                     `json:"page_size"`
	TotalItems int64                   `json:"total_items"`
	TotalPages int                     `json:"total_pages"`
	LatestRun  *TikTokCatalogRunResult `json:"latest_run,omitempty"`
}

type ProductCatalogService struct {
	gateway TikTokCatalogGateway
	store   TikTokCatalogStore
}

func NewProductCatalogService(gateway TikTokCatalogGateway, store TikTokCatalogStore) *ProductCatalogService {
	return &ProductCatalogService{gateway: gateway, store: store}
}

func (s *ProductCatalogService) Sync(ctx context.Context, shopID, triggerSource string) (*TikTokCatalogRunResult, error) {
	shopID = strings.TrimSpace(shopID)
	triggerSource = strings.TrimSpace(triggerSource)
	if s == nil || s.gateway == nil || s.store == nil {
		return nil, ErrProductCatalogNotConfigured
	}
	if !ValidTikTokShopID(shopID) || (triggerSource != "manual" && triggerSource != "schedule") {
		return nil, ErrProductCatalogInvalidInput
	}
	runID, err := s.store.CreateRun(ctx, shopID, triggerSource)
	if err != nil {
		return nil, err
	}
	result := &TikTokCatalogRunResult{ID: runID, ShopID: shopID, Status: "running", TriggerSource: triggerSource}
	fail := func(cause error, code string) (*TikTokCatalogRunResult, error) {
		result.Status, result.ErrorCode = "failed", code
		_ = s.store.FinishRun(context.WithoutCancel(ctx), *result)
		return nil, cause
	}

	products, requestIDs, pageCount, err := s.loadAllProducts(ctx, shopID)
	if err != nil {
		return fail(err, catalogErrorCode(err))
	}
	result.PageCount, result.UpstreamRequestIDs = pageCount, requestIDs
	if err := s.loadInventory(ctx, shopID, products, &result.UpstreamRequestIDs); err != nil {
		return fail(err, catalogErrorCode(err))
	}
	for _, product := range products {
		result.ProductCount++
		result.SKUCount += len(product.Product.SKUs)
		for _, inventory := range product.Inventory {
			result.WarehouseCount += len(inventory.WarehouseInventory)
		}
	}
	result.Status = "succeeded"
	if err := s.store.ReplaceCatalog(ctx, *result, products); err != nil {
		result.Status = "running"
		return fail(fmt.Errorf("replace TikTok Shop product catalog: %w", err), "persistence_failed")
	}
	return result, nil
}

func (s *ProductCatalogService) loadAllProducts(ctx context.Context, shopID string) ([]TikTokCatalogProduct, []string, int, error) {
	products := make([]TikTokCatalogProduct, 0)
	seenProducts := make(map[string]struct{})
	seenTokens := make(map[string]struct{})
	requestIDs := make([]string, 0)
	pageToken := ""
	expectedTotal := int64(-1)
	for page := 1; page <= maxTikTokCatalogPages; page++ {
		response, err := s.gateway.SearchProducts(ctx, GatewayProductSearchRequest{
			ShopID: shopID,
			Search: SearchProductsRequest{PageSize: 100, PageToken: pageToken, Status: ProductStatusAll},
		})
		if err != nil {
			return nil, requestIDs, page - 1, fmt.Errorf("search TikTok Shop product page: %w", err)
		}
		if response == nil || !validCatalogRequestID(response.UpstreamRequestID) || response.TotalCount < 0 {
			return nil, requestIDs, page - 1, ErrProductCatalogSourceInvalid
		}
		requestIDs = append(requestIDs, strings.TrimSpace(response.UpstreamRequestID))
		if expectedTotal == -1 {
			expectedTotal = response.TotalCount
		} else if response.TotalCount != expectedTotal {
			return nil, requestIDs, page, ErrProductCatalogSourceInvalid
		}
		for _, product := range response.Products {
			if _, duplicate := seenProducts[product.ID]; duplicate {
				return nil, requestIDs, page, ErrProductCatalogSourceInvalid
			}
			seenProducts[product.ID] = struct{}{}
			products = append(products, TikTokCatalogProduct{Product: product, Inventory: make(map[string]InventorySearchSKU)})
			if len(products) > maxTikTokCatalogProducts {
				return nil, requestIDs, page, ErrProductCatalogSourceInvalid
			}
		}
		next := strings.TrimSpace(response.NextPageToken)
		if next == "" {
			if int64(len(products)) != expectedTotal {
				return nil, requestIDs, page, ErrProductCatalogSourceInvalid
			}
			return products, requestIDs, page, nil
		}
		if next == pageToken {
			return nil, requestIDs, page, ErrProductCatalogSourceInvalid
		}
		if _, duplicate := seenTokens[next]; duplicate {
			return nil, requestIDs, page, ErrProductCatalogSourceInvalid
		}
		seenTokens[next] = struct{}{}
		pageToken = next
	}
	return nil, requestIDs, maxTikTokCatalogPages, ErrProductCatalogSourceInvalid
}

func (s *ProductCatalogService) loadInventory(ctx context.Context, shopID string, products []TikTokCatalogProduct, requestIDs *[]string) error {
	skuProduct := make(map[string]int)
	skuIDs := make([]string, 0)
	for productIndex := range products {
		for _, sku := range products[productIndex].Product.SKUs {
			if _, duplicate := skuProduct[sku.ID]; duplicate {
				return ErrProductCatalogSourceInvalid
			}
			skuProduct[sku.ID] = productIndex
			skuIDs = append(skuIDs, sku.ID)
		}
	}
	seen := make(map[string]struct{}, len(skuIDs))
	for start := 0; start < len(skuIDs); start += maxInventorySearchSKUs {
		end := min(start+maxInventorySearchSKUs, len(skuIDs))
		response, err := s.gateway.SearchInventory(ctx, GatewayInventorySearchRequest{
			ShopID: shopID, Search: InventorySearchRequest{SKUIDs: append([]string(nil), skuIDs[start:end]...)},
		})
		if err != nil {
			return fmt.Errorf("search TikTok Shop inventory: %w", err)
		}
		if response == nil || !validCatalogRequestID(response.UpstreamRequestID) {
			return ErrProductCatalogSourceInvalid
		}
		*requestIDs = append(*requestIDs, strings.TrimSpace(response.UpstreamRequestID))
		for _, record := range response.Inventory {
			for _, sku := range record.SKUs {
				productIndex, expected := skuProduct[sku.ID]
				if !expected || products[productIndex].Product.ID != record.ProductID {
					return ErrProductCatalogSourceInvalid
				}
				if _, duplicate := seen[sku.ID]; duplicate {
					return ErrProductCatalogSourceInvalid
				}
				seen[sku.ID] = struct{}{}
				products[productIndex].Inventory[sku.ID] = sku
			}
		}
	}
	if len(seen) != len(skuIDs) {
		return ErrProductCatalogSourceInvalid
	}
	return nil
}

func catalogErrorCode(err error) string {
	var gatewayError *GatewayError
	switch {
	case errors.Is(err, ErrProductCatalogSourceInvalid), errors.Is(err, ErrInvalidProductResponse):
		return "source_invalid"
	case errors.As(err, &gatewayError):
		switch gatewayError.Code {
		case "product_scope_required":
			return "scope_required"
		case "reconnect_required":
			return "reconnect_required"
		case "gateway_not_ready":
			return "gateway_unavailable"
		case "tiktok_api_error":
			return "upstream_rejected"
		default:
			return "gateway_error"
		}
	default:
		return "source_unavailable"
	}
}

func validCatalogRequestID(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= 128
}

type ProductCatalogStore struct {
	database *sql.DB
}

func NewProductCatalogStore(database *sql.DB) *ProductCatalogStore {
	return &ProductCatalogStore{database: database}
}

func (s *ProductCatalogStore) CreateRun(ctx context.Context, shopID, triggerSource string) (string, error) {
	if s == nil || s.database == nil {
		return "", ErrProductCatalogNotConfigured
	}
	var runID string
	err := s.database.QueryRowContext(ctx, `
		WITH expired AS (
		  UPDATE tiktok_shop_product_catalog_runs
		     SET status='failed',error_code='lease_expired',finished_at=NOW()
		   WHERE shop_id=$1 AND status='running' AND lease_until<NOW()
		)
		INSERT INTO tiktok_shop_product_catalog_runs (shop_id,status,trigger_source,lease_until)
		SELECT shop_id,'running',$2,NOW()+INTERVAL '2 hours' FROM tiktok_shop_connections
		 WHERE shop_id=$1 AND disabled_at IS NULL
		RETURNING id::text`, shopID, triggerSource).Scan(&runID)
	if err != nil {
		var databaseError *pq.Error
		if errors.As(err, &databaseError) && databaseError.Code == "23505" && databaseError.Constraint == "tiktok_shop_product_catalog_runs_active_idx" {
			return "", ErrProductCatalogSyncInProgress
		}
	}
	return runID, err
}

func (s *ProductCatalogStore) FinishRun(ctx context.Context, result TikTokCatalogRunResult) error {
	if s == nil || s.database == nil {
		return ErrProductCatalogNotConfigured
	}
	requestIDs, err := json.Marshal(result.UpstreamRequestIDs)
	if err != nil {
		return err
	}
	finished, err := s.database.ExecContext(ctx, `
		UPDATE tiktok_shop_product_catalog_runs
		   SET status=$2,page_count=$3,product_count=$4,sku_count=$5,warehouse_count=$6,
		       upstream_request_ids=$7::jsonb,error_code=$8,finished_at=NOW()
		 WHERE id=$1::uuid AND status='running'`, result.ID, result.Status, result.PageCount,
		result.ProductCount, result.SKUCount, result.WarehouseCount, requestIDs, result.ErrorCode)
	if err != nil {
		return err
	}
	rows, err := finished.RowsAffected()
	if err != nil || rows != 1 {
		return ErrProductCatalogSourceInvalid
	}
	return nil
}

func (s *ProductCatalogStore) ReplaceCatalog(ctx context.Context, result TikTokCatalogRunResult, products []TikTokCatalogProduct) error {
	if s == nil || s.database == nil {
		return ErrProductCatalogNotConfigured
	}
	shopID, runID := result.ShopID, result.ID
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, table := range []string{"tiktok_shop_product_inventory", "tiktok_shop_product_skus", "tiktok_shop_products"} {
		if _, err := tx.ExecContext(ctx, "UPDATE "+table+" SET is_active=false,updated_at=NOW() WHERE shop_id=$1", shopID); err != nil {
			return err
		}
	}
	for _, item := range products {
		product := item.Product
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO tiktok_shop_products
			 (shop_id,product_id,title,status,source_create_time,source_update_time,catalog_run_id,last_seen_at,is_active)
			VALUES ($1,$2,$3,$4,$5,$6,$7::uuid,NOW(),true)
			ON CONFLICT (shop_id,product_id) DO UPDATE SET
			 title=EXCLUDED.title,status=EXCLUDED.status,source_create_time=EXCLUDED.source_create_time,
			 source_update_time=EXCLUDED.source_update_time,catalog_run_id=EXCLUDED.catalog_run_id,
			 last_seen_at=NOW(),is_active=true,updated_at=NOW()`, shopID, product.ID, product.Title,
			product.Status, product.CreateTime, product.UpdateTime, runID); err != nil {
			return err
		}
		for _, sku := range product.SKUs {
			inventory := item.Inventory[sku.ID]
			price, err := json.Marshal(sku.Price)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO tiktok_shop_product_skus
				 (shop_id,product_id,sku_id,seller_sku,price,total_available_quantity,total_committed_quantity,catalog_run_id,last_seen_at,is_active)
				VALUES ($1,$2,$3,$4,$5::jsonb,$6,$7,$8::uuid,NOW(),true)
				ON CONFLICT (shop_id,product_id,sku_id) DO UPDATE SET
				 seller_sku=EXCLUDED.seller_sku,price=EXCLUDED.price,
				 total_available_quantity=EXCLUDED.total_available_quantity,total_committed_quantity=EXCLUDED.total_committed_quantity,
				 catalog_run_id=EXCLUDED.catalog_run_id,last_seen_at=NOW(),is_active=true,updated_at=NOW()`,
				shopID, product.ID, sku.ID, sku.SellerSKU, price, inventory.TotalAvailableQuantity,
				inventory.TotalCommittedQuantity, runID); err != nil {
				return err
			}
			for _, warehouse := range inventory.WarehouseInventory {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO tiktok_shop_product_inventory
					 (shop_id,product_id,sku_id,warehouse_id,available_quantity,committed_quantity,catalog_run_id,last_seen_at,is_active)
					VALUES ($1,$2,$3,$4,$5,$6,$7::uuid,NOW(),true)
					ON CONFLICT (shop_id,product_id,sku_id,warehouse_id) DO UPDATE SET
					 available_quantity=EXCLUDED.available_quantity,committed_quantity=EXCLUDED.committed_quantity,
					 catalog_run_id=EXCLUDED.catalog_run_id,last_seen_at=NOW(),is_active=true,updated_at=NOW()`,
					shopID, product.ID, sku.ID, warehouse.WarehouseID, warehouse.AvailableQuantity,
					warehouse.CommittedQuantity, runID); err != nil {
					return err
				}
			}
		}
	}
	requestIDs, err := json.Marshal(result.UpstreamRequestIDs)
	if err != nil {
		return err
	}
	finished, err := tx.ExecContext(ctx, `
		UPDATE tiktok_shop_product_catalog_runs
		   SET status='succeeded',page_count=$2,product_count=$3,sku_count=$4,warehouse_count=$5,
		       upstream_request_ids=$6::jsonb,error_code='',finished_at=NOW()
		 WHERE id=$1::uuid AND shop_id=$7 AND status='running'`, runID, result.PageCount,
		result.ProductCount, result.SKUCount, result.WarehouseCount, requestIDs, shopID)
	if err != nil {
		return err
	}
	rows, err := finished.RowsAffected()
	if err != nil || rows != 1 {
		return ErrProductCatalogSourceInvalid
	}
	return tx.Commit()
}

func (s *ProductCatalogStore) List(ctx context.Context, filter TikTokCatalogListFilter) (*TikTokCatalogListResult, error) {
	if s == nil || s.database == nil {
		return nil, ErrProductCatalogNotConfigured
	}
	filter, err := normalizeTikTokCatalogListFilter(filter)
	if err != nil {
		return nil, err
	}
	where := `p.is_active=true AND s.is_active=true AND ($1='' OR p.shop_id=$1)
		AND ($2='' OR p.status=$2)
		AND ($3='' OR p.title ILIKE '%'||$3||'%' OR s.seller_sku ILIKE '%'||$3||'%' OR p.product_id=$3 OR s.sku_id=$3)`
	var total int64
	if err := s.database.QueryRowContext(ctx, `SELECT COUNT(*) FROM tiktok_shop_products p
		JOIN tiktok_shop_product_skus s USING(shop_id,product_id) WHERE `+where,
		filter.ShopID, filter.Status, filter.Query).Scan(&total); err != nil {
		return nil, err
	}
	rows, err := s.database.QueryContext(ctx, `
		SELECT p.shop_id,c.shop_name,p.product_id,p.title,p.status,s.sku_id,s.seller_sku,s.price,
		       s.total_available_quantity,s.total_committed_quantity,s.last_seen_at,
		       COALESCE(jsonb_agg(jsonb_build_object(
		         'warehouse_id',i.warehouse_id,'available_quantity',i.available_quantity,
		         'committed_quantity',i.committed_quantity
		       ) ORDER BY i.warehouse_id) FILTER (WHERE i.warehouse_id IS NOT NULL),'[]'::jsonb)
		  FROM tiktok_shop_products p
		  JOIN tiktok_shop_product_skus s USING(shop_id,product_id)
		  JOIN tiktok_shop_connections c ON c.shop_id=p.shop_id
		  LEFT JOIN tiktok_shop_product_inventory i ON i.shop_id=s.shop_id AND i.product_id=s.product_id
		       AND i.sku_id=s.sku_id AND i.is_active=true
		 WHERE `+where+`
		 GROUP BY p.shop_id,c.shop_name,p.product_id,p.title,p.status,s.sku_id,s.seller_sku,s.price,
		          s.total_available_quantity,s.total_committed_quantity,s.last_seen_at
		 ORDER BY p.product_id,s.sku_id
		 LIMIT $4 OFFSET $5`, filter.ShopID, filter.Status, filter.Query, filter.PageSize, (filter.Page-1)*filter.PageSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]TikTokCatalogListItem, 0, filter.PageSize)
	for rows.Next() {
		var item TikTokCatalogListItem
		var priceJSON, inventoryJSON []byte
		if err := rows.Scan(&item.ShopID, &item.ShopName, &item.ProductID, &item.ProductTitle, &item.ProductStatus,
			&item.SKUID, &item.SellerSKU, &priceJSON, &item.TotalAvailableQuantity, &item.TotalCommittedQuantity,
			&item.LastSeenAt, &inventoryJSON); err != nil {
			return nil, err
		}
		if json.Unmarshal(priceJSON, &item.Price) != nil || json.Unmarshal(inventoryJSON, &item.Inventory) != nil {
			return nil, ErrProductCatalogSourceInvalid
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := &TikTokCatalogListResult{Data: items, Page: filter.Page, PageSize: filter.PageSize, TotalItems: total}
	if total > 0 {
		result.TotalPages = int((total + int64(filter.PageSize) - 1) / int64(filter.PageSize))
	}
	var latest TikTokCatalogRunResult
	var requestIDs []byte
	err = s.database.QueryRowContext(ctx, `SELECT id::text,shop_id,status,trigger_source,page_count,product_count,
		sku_count,warehouse_count,upstream_request_ids,error_code,started_at,COALESCE(finished_at,started_at)
		FROM tiktok_shop_product_catalog_runs WHERE ($1='' OR shop_id=$1) ORDER BY started_at DESC LIMIT 1`, filter.ShopID).Scan(
		&latest.ID, &latest.ShopID, &latest.Status, &latest.TriggerSource, &latest.PageCount, &latest.ProductCount,
		&latest.SKUCount, &latest.WarehouseCount, &requestIDs, &latest.ErrorCode, &latest.StartedAt, &latest.FinishedAt)
	if err == nil {
		if json.Unmarshal(requestIDs, &latest.UpstreamRequestIDs) != nil {
			return nil, ErrProductCatalogSourceInvalid
		}
		result.LatestRun = &latest
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return result, nil
}

func normalizeTikTokCatalogListFilter(filter TikTokCatalogListFilter) (TikTokCatalogListFilter, error) {
	filter.ShopID, filter.Query = strings.TrimSpace(filter.ShopID), strings.TrimSpace(filter.Query)
	if filter.ShopID != "" && !ValidTikTokShopID(filter.ShopID) || len([]rune(filter.Query)) > 100 {
		return TikTokCatalogListFilter{}, ErrProductCatalogInvalidInput
	}
	if filter.Status != "" {
		request := SearchProductsRequest{PageSize: 1, Status: filter.Status}
		if request.Validate() != nil || request.Status == ProductStatusAll {
			return TikTokCatalogListFilter{}, ErrProductCatalogInvalidInput
		}
	}
	if filter.Page < 1 || filter.Page > 1_000_000 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 50
	}
	return filter, nil
}
