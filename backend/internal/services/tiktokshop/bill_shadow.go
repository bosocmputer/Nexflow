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
	"reflect"
	"sort"
	"strings"
	"time"

	"nexflow/internal/marketplace"
)

var (
	ErrTikTokBillShadowInvalidInput       = errors.New("invalid TikTok Shop bill shadow input")
	ErrTikTokBillShadowNotFound           = errors.New("TikTok Shop order snapshot not found")
	ErrTikTokBillShadowSourceInvalid      = errors.New("TikTok Shop bill shadow source is invalid")
	ErrTikTokBillShadowStoreNotConfigured = errors.New("TikTok Shop bill shadow store is not configured")
)

const (
	TikTokBillShadowMappingReady          = "ready"
	TikTokBillShadowMappingMissing        = "missing"
	TikTokBillShadowMappingLegacyUnscoped = "legacy_unscoped"
	TikTokBillShadowMappingNotReady       = "not_ready"

	TikTokBillShadowBlockerStatusNotReady    = "order_status_not_ready"
	TikTokBillShadowBlockerAmountMismatch    = "amount_mismatch"
	TikTokBillShadowBlockerMappingMissing    = "mapping_missing"
	TikTokBillShadowBlockerMappingUnscoped   = "mapping_unscoped"
	TikTokBillShadowBlockerMappingNotReady   = "mapping_not_ready"
	TikTokBillShadowBlockerConversionInvalid = "quantity_conversion_invalid"
	TikTokBillShadowBlockerRouteNotReady     = "sale_route_not_ready"
	TikTokBillShadowBlockerShippingNotReady  = "shipping_item_not_ready"
	TikTokBillShadowBlockerExistingBill      = "existing_bill"
	TikTokBillShadowBlockerSourceInvalid     = "snapshot_evidence_invalid"
)

type TikTokBillShadowBlocker struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	ProductID string `json:"product_id,omitempty"`
	SKUID     string `json:"sku_id,omitempty"`
}

type TikTokBillShadowAmounts struct {
	ProductSubtotal              string `json:"product_subtotal"`
	Shipping                     string `json:"shipping"`
	ProposedDocumentTotal        string `json:"proposed_document_total"`
	BuyerPayment                 string `json:"buyer_payment"`
	ExcludedBuyerPlatformCharges string `json:"excluded_buyer_platform_charges"`
	ItemInsurance                string `json:"item_insurance"`
	GroupedLineTotal             string `json:"grouped_line_total"`
}

type TikTokBillShadowRoute struct {
	Ready                bool   `json:"ready"`
	SemanticRoute        string `json:"semantic_route,omitempty"`
	DocFormatCode        string `json:"doc_format_code,omitempty"`
	ShippingReady        bool   `json:"shipping_ready"`
	ShippingItemCode     string `json:"-"`
	ShippingItemUnitCode string `json:"-"`
}

type TikTokBillShadowItemMapping struct {
	Status                string `json:"status"`
	ItemCode              string `json:"item_code,omitempty"`
	UnitCode              string `json:"unit_code,omitempty"`
	MarketplaceQuantity   string `json:"marketplace_quantity"`
	SMLQuantity           string `json:"sml_quantity,omitempty"`
	BaseQuantity          string `json:"base_quantity,omitempty"`
	AliasID               string `json:"-"`
	MappingRevision       int64  `json:"-"`
	QuantityMultiplier    int64  `json:"-"`
	UnitStandValue        string `json:"-"`
	UnitDivideValue       string `json:"-"`
	UnitCatalogGeneration string `json:"-"`
	SetDefinitionHash     string `json:"-"`
}

type TikTokBillShadowItem struct {
	ProductID     string                      `json:"product_id"`
	SKUID         string                      `json:"sku_id"`
	SellerSKU     string                      `json:"seller_sku,omitempty"`
	ProductName   string                      `json:"product_name"`
	VariantName   string                      `json:"variant_name"`
	Quantity      int                         `json:"quantity"`
	UnitSalePrice string                      `json:"unit_sale_price"`
	LineTotal     string                      `json:"line_total"`
	Mapping       TikTokBillShadowItemMapping `json:"mapping"`
}

type TikTokBillShadowExistingBill struct {
	ID               string `json:"id"`
	Status           string `json:"status"`
	SMLDocNo         string `json:"sml_doc_no,omitempty"`
	SourceAccountKey string `json:"source_account_key"`
	SourceFlow       string `json:"-"`
	DocumentRoute    string `json:"-"`
}

type TikTokBillShadowPreview struct {
	ShadowMode           bool                          `json:"shadow_mode"`
	CanCreateBill        bool                          `json:"can_create_bill"`
	ReadyForReviewedBill bool                          `json:"ready_for_reviewed_bill"`
	ShopID               string                        `json:"shop_id"`
	ShopName             string                        `json:"shop_name"`
	OrderID              string                        `json:"order_id"`
	OrderStatus          OrderStatus                   `json:"order_status"`
	Currency             string                        `json:"currency"`
	LastSyncedAt         time.Time                     `json:"last_synced_at"`
	Amounts              TikTokBillShadowAmounts       `json:"amounts"`
	Route                TikTokBillShadowRoute         `json:"route"`
	Items                []TikTokBillShadowItem        `json:"items"`
	Blockers             []TikTokBillShadowBlocker     `json:"blockers"`
	ExistingBill         *TikTokBillShadowExistingBill `json:"existing_bill,omitempty"`
	ReviewDigest         string                        `json:"review_digest"`
	SourceHash           string                        `json:"-"`
}

// TikTokBillShadowMappingSource is internal read-only evidence. The public
// preview deliberately omits alias IDs and catalog internals.
type TikTokBillShadowMappingSource struct {
	AliasID               string
	ProductID             string
	SKUID                 string
	AccountKey            string
	ItemCode              string
	UnitCode              string
	IsActive              bool
	ScopeConfirmed        bool
	SalesEnabled          bool
	ConversionStatus      string
	QuantityMultiplier    int64
	UnitStandValue        string
	UnitDivideValue       string
	CatalogReady          bool
	MappingRevision       int64
	UnitCatalogGeneration string
	SetDefinitionHash     string
}

type TikTokBillShadowRouteSource struct {
	Configured           bool
	Endpoint             string
	DocFormatCode        string
	ShippingItemEnabled  bool
	ShippingItemCode     string
	ShippingItemUnitCode string
}

type TikTokBillShadowSource struct {
	ShopID                 string
	ShopName               string
	StoredOrderStatus      OrderStatus
	StoredCurrency         string
	StoredPaymentTotal     string
	StoredProductSubtotal  string
	StoredShippingFee      string
	StoredItemInsuranceFee string
	LastSyncedAt           time.Time
	SourceHash             string
	Order                  Order
	Price                  PriceDetail
	Items                  []NormalizedTikTokOrderItem
	Mappings               []TikTokBillShadowMappingSource
	Route                  TikTokBillShadowRouteSource
	ExistingBill           *TikTokBillShadowExistingBill
}

type TikTokBillShadowSourceLoader interface {
	Load(context.Context, string, string) (*TikTokBillShadowSource, error)
}

type TikTokBillShadowService struct {
	store TikTokBillShadowSourceLoader
}

func NewTikTokBillShadowService(store TikTokBillShadowSourceLoader) *TikTokBillShadowService {
	return &TikTokBillShadowService{store: store}
}

func (s *TikTokBillShadowService) Preview(ctx context.Context, shopID, orderID string) (*TikTokBillShadowPreview, error) {
	shopID = strings.TrimSpace(shopID)
	orderID = strings.TrimSpace(orderID)
	if s == nil || s.store == nil || !ValidTikTokShopID(shopID) || !ValidTikTokShopID(orderID) {
		return nil, ErrTikTokBillShadowInvalidInput
	}
	source, err := s.store.Load(ctx, shopID, orderID)
	if err != nil {
		return nil, err
	}
	if source == nil || source.ShopID != shopID || strings.TrimSpace(source.Order.ID) != orderID {
		return nil, ErrTikTokBillShadowSourceInvalid
	}
	return buildTikTokBillShadowPreview(source)
}

func buildTikTokBillShadowPreview(source *TikTokBillShadowSource) (*TikTokBillShadowPreview, error) {
	if source == nil || len(source.Items) == 0 || len(source.Order.LineItems) == 0 {
		return nil, ErrTikTokBillShadowSourceInvalid
	}
	preview := &TikTokBillShadowPreview{
		ShadowMode: true, CanCreateBill: false, ShopID: source.ShopID, ShopName: source.ShopName,
		OrderID: source.Order.ID, OrderStatus: source.StoredOrderStatus,
		Currency: strings.ToUpper(strings.TrimSpace(source.StoredCurrency)), LastSyncedAt: source.LastSyncedAt.UTC(),
		Items: []TikTokBillShadowItem{}, Blockers: []TikTokBillShadowBlocker{}, ExistingBill: source.ExistingBill,
	}
	if !validTikTokBillShadowImpactDigest(source.SourceHash) {
		appendTikTokBillShadowBlocker(&preview.Blockers, TikTokBillShadowBlocker{
			Code:    TikTokBillShadowBlockerSourceInvalid,
			Message: "หลักฐาน Snapshot ไม่สมบูรณ์ กรุณาซิงก์ออเดอร์ใหม่ก่อนสร้าง Bill",
		})
	}
	if !tikTokBillLifecycleReady(source.StoredOrderStatus) {
		appendTikTokBillShadowBlocker(&preview.Blockers, TikTokBillShadowBlocker{
			Code:    TikTokBillShadowBlockerStatusNotReady,
			Message: "สถานะ TikTok Shop ปัจจุบันยังไม่พร้อมสำหรับการตรวจสร้างเอกสารขาย",
		})
	}

	storedPayment, err := shadowMoney(source.StoredPaymentTotal)
	if err != nil {
		return nil, ErrTikTokBillShadowSourceInvalid
	}
	storedProduct, err := shadowMoney(source.StoredProductSubtotal)
	if err != nil {
		return nil, ErrTikTokBillShadowSourceInvalid
	}
	storedShipping, err := shadowMoney(source.StoredShippingFee)
	if err != nil {
		return nil, ErrTikTokBillShadowSourceInvalid
	}
	storedInsurance, err := shadowMoney(source.StoredItemInsuranceFee)
	if err != nil {
		return nil, ErrTikTokBillShadowSourceInvalid
	}

	groupedLineTotals := make(map[string]*big.Rat, len(source.Items))
	for _, line := range source.Order.LineItems {
		amount, parseErr := shadowMoney(line.SalePrice)
		if parseErr != nil || strings.ToUpper(strings.TrimSpace(line.Currency)) != preview.Currency {
			return nil, ErrTikTokBillShadowSourceInvalid
		}
		key := tikTokBillShadowItemKey(line.ProductID, line.SKUID)
		if groupedLineTotals[key] == nil {
			groupedLineTotals[key] = new(big.Rat)
		}
		groupedLineTotals[key].Add(groupedLineTotals[key], amount)
	}
	lineTotal := new(big.Rat)
	for _, amount := range groupedLineTotals {
		lineTotal.Add(lineTotal, amount)
	}

	orderPayment, orderPaymentErr := shadowMoney(source.Order.Payment.TotalAmount)
	orderProduct, orderProductErr := shadowMoney(source.Order.Payment.SubTotal)
	orderShipping, orderShippingErr := shadowMoney(source.Order.Payment.ShippingFee)
	orderInsurance, orderInsuranceErr := shadowMoney(source.Order.Payment.ItemInsuranceFee)
	pricePayment, pricePaymentErr := shadowMoney(source.Price.Payment)
	amountsMatch := orderPaymentErr == nil && orderProductErr == nil && orderShippingErr == nil && orderInsuranceErr == nil && pricePaymentErr == nil &&
		strings.TrimSpace(source.Order.ID) != "" && source.Order.Status == source.StoredOrderStatus &&
		strings.ToUpper(strings.TrimSpace(source.Order.Payment.Currency)) == preview.Currency &&
		strings.ToUpper(strings.TrimSpace(source.Price.Currency)) == preview.Currency &&
		storedPayment.Cmp(orderPayment) == 0 && storedPayment.Cmp(pricePayment) == 0 &&
		storedProduct.Cmp(orderProduct) == 0 && storedShipping.Cmp(orderShipping) == 0 &&
		storedInsurance.Cmp(orderInsurance) == 0 && storedProduct.Cmp(lineTotal) == 0
	if source.Price.Subtotal != "" {
		priceProduct, priceProductErr := shadowMoney(source.Price.Subtotal)
		amountsMatch = amountsMatch && priceProductErr == nil && storedProduct.Cmp(priceProduct) == 0
	}
	explainedPayment := new(big.Rat).Add(new(big.Rat).Add(new(big.Rat).Set(storedProduct), storedShipping), storedInsurance)
	amountsMatch = amountsMatch && storedPayment.Cmp(explainedPayment) == 0
	canonicalItems, canonicalErr := normalizeTikTokOrderItems(source.Order.LineItems)
	amountsMatch = amountsMatch && canonicalErr == nil && reflect.DeepEqual(canonicalItems, source.Items)
	if !amountsMatch {
		appendTikTokBillShadowBlocker(&preview.Blockers, TikTokBillShadowBlocker{
			Code:    TikTokBillShadowBlockerAmountMismatch,
			Message: "ยอดหรือรายการสินค้าใน Snapshot ไม่ตรงกัน ระบบจึงหยุดไว้เพื่อตรวจสอบ",
		})
	}
	documentTotal := new(big.Rat).Add(new(big.Rat).Set(storedProduct), storedShipping)
	preview.Amounts = TikTokBillShadowAmounts{
		ProductSubtotal: shadowMoneyText(storedProduct), Shipping: shadowMoneyText(storedShipping),
		ProposedDocumentTotal: shadowMoneyText(documentTotal), BuyerPayment: shadowMoneyText(storedPayment),
		ExcludedBuyerPlatformCharges: shadowMoneyText(storedInsurance), ItemInsurance: shadowMoneyText(storedInsurance),
		GroupedLineTotal: shadowMoneyText(lineTotal),
	}

	scopedKey := "shop:" + source.ShopID
	mappingByItem := make(map[string][]TikTokBillShadowMappingSource, len(source.Mappings))
	for _, mapping := range source.Mappings {
		key := tikTokBillShadowItemKey(mapping.ProductID, mapping.SKUID)
		mappingByItem[key] = append(mappingByItem[key], mapping)
	}
	for _, item := range source.Items {
		key := tikTokBillShadowItemKey(item.ProductID, item.SKUID)
		groupTotal := groupedLineTotals[key]
		if groupTotal == nil || item.Quantity < 1 {
			return nil, ErrTikTokBillShadowSourceInvalid
		}
		itemPreview := TikTokBillShadowItem{
			ProductID: item.ProductID, SKUID: item.SKUID, SellerSKU: item.SellerSKU,
			ProductName: item.ProductName, VariantName: item.SKUName, Quantity: item.Quantity,
			UnitSalePrice: shadowMoneyText(new(big.Rat).Quo(new(big.Rat).Set(groupTotal), big.NewRat(int64(item.Quantity), 1))),
			LineTotal:     shadowMoneyText(groupTotal),
			Mapping:       TikTokBillShadowItemMapping{Status: TikTokBillShadowMappingMissing, MarketplaceQuantity: fmt.Sprintf("%d", item.Quantity)},
		}
		var scoped, legacy *TikTokBillShadowMappingSource
		for index := range mappingByItem[key] {
			candidate := &mappingByItem[key][index]
			if candidate.AccountKey == scopedKey {
				scoped = candidate
			} else if candidate.AccountKey == "default" {
				legacy = candidate
			}
		}
		switch {
		case scoped == nil && legacy != nil:
			itemPreview.Mapping.Status = TikTokBillShadowMappingLegacyUnscoped
			appendTikTokBillShadowBlocker(&preview.Blockers, TikTokBillShadowBlocker{
				Code:      TikTokBillShadowBlockerMappingUnscoped,
				Message:   "พบ Product Master เดิมที่ยังไม่ผูกกับร้าน TikTok Shop นี้ กรุณาตรวจสอบขอบเขตร้าน",
				ProductID: item.ProductID, SKUID: item.SKUID,
			})
		case scoped == nil:
			appendTikTokBillShadowBlocker(&preview.Blockers, TikTokBillShadowBlocker{
				Code:      TikTokBillShadowBlockerMappingMissing,
				Message:   "ยังไม่ได้จับคู่สินค้า TikTok Shop กับ Product Master ของร้านนี้",
				ProductID: item.ProductID, SKUID: item.SKUID,
			})
		default:
			itemPreview.Mapping.ItemCode = scoped.ItemCode
			itemPreview.Mapping.UnitCode = scoped.UnitCode
			itemPreview.Mapping.AliasID = scoped.AliasID
			itemPreview.Mapping.MappingRevision = scoped.MappingRevision
			itemPreview.Mapping.QuantityMultiplier = scoped.QuantityMultiplier
			itemPreview.Mapping.UnitStandValue = scoped.UnitStandValue
			itemPreview.Mapping.UnitDivideValue = scoped.UnitDivideValue
			itemPreview.Mapping.UnitCatalogGeneration = scoped.UnitCatalogGeneration
			itemPreview.Mapping.SetDefinitionHash = scoped.SetDefinitionHash
			if !scoped.IsActive || !scoped.ScopeConfirmed || !scoped.SalesEnabled || scoped.ConversionStatus != "ready" || !scoped.CatalogReady {
				itemPreview.Mapping.Status = TikTokBillShadowMappingNotReady
				appendTikTokBillShadowBlocker(&preview.Blockers, TikTokBillShadowBlocker{
					Code:      TikTokBillShadowBlockerMappingNotReady,
					Message:   "Product Master ของสินค้านี้ยังไม่พร้อมใช้กับ Catalog และงานขาย",
					ProductID: item.ProductID, SKUID: item.SKUID,
				})
			} else {
				conversion, conversionErr := marketplace.CalculateQuantityConversion(marketplace.QuantityConversionInput{
					MarketplaceQty: fmt.Sprintf("%d", item.Quantity), Multiplier: scoped.QuantityMultiplier,
					StandValue: scoped.UnitStandValue, DivideValue: scoped.UnitDivideValue,
				})
				smlQuantity, smlErr := marketplace.RatFiniteDecimal(conversion.SMLQty)
				baseQuantity, baseErr := marketplace.RatFiniteDecimal(conversion.BaseQty)
				if conversionErr != nil || smlErr != nil || baseErr != nil {
					itemPreview.Mapping.Status = TikTokBillShadowMappingNotReady
					appendTikTokBillShadowBlocker(&preview.Blockers, TikTokBillShadowBlocker{
						Code:      TikTokBillShadowBlockerConversionInvalid,
						Message:   "ตัวคูณหรือหน่วยสินค้าไม่สามารถคำนวณจำนวน SML ได้อย่างแน่นอน",
						ProductID: item.ProductID, SKUID: item.SKUID,
					})
				} else {
					itemPreview.Mapping.Status = TikTokBillShadowMappingReady
					itemPreview.Mapping.SMLQuantity = smlQuantity
					itemPreview.Mapping.BaseQuantity = baseQuantity
				}
			}
		}
		preview.Items = append(preview.Items, itemPreview)
	}

	semanticRoute := tikTokBillShadowSemanticRoute(source.Route.Endpoint)
	preview.Route = TikTokBillShadowRoute{
		Ready:         source.Route.Configured && strings.TrimSpace(source.Route.DocFormatCode) != "" && semanticRoute != "",
		SemanticRoute: semanticRoute, DocFormatCode: strings.TrimSpace(source.Route.DocFormatCode),
		ShippingReady:        storedShipping.Sign() == 0 || (source.Route.ShippingItemEnabled && strings.TrimSpace(source.Route.ShippingItemCode) != "" && strings.TrimSpace(source.Route.ShippingItemUnitCode) != ""),
		ShippingItemCode:     strings.TrimSpace(source.Route.ShippingItemCode),
		ShippingItemUnitCode: strings.TrimSpace(source.Route.ShippingItemUnitCode),
	}
	if !preview.Route.Ready {
		appendTikTokBillShadowBlocker(&preview.Blockers, TikTokBillShadowBlocker{
			Code:    TikTokBillShadowBlockerRouteNotReady,
			Message: "การตั้งค่าเอกสารขาย TikTok Shop ยังไม่พร้อม",
		})
	}
	if !preview.Route.ShippingReady {
		appendTikTokBillShadowBlocker(&preview.Blockers, TikTokBillShadowBlocker{
			Code:    TikTokBillShadowBlockerShippingNotReady,
			Message: "ออเดอร์มีค่าจัดส่ง แต่ยังไม่ได้ตั้งรหัสสินค้าและหน่วยค่าจัดส่งสำหรับ SML",
		})
	}
	if source.ExistingBill != nil {
		appendTikTokBillShadowBlocker(&preview.Blockers, TikTokBillShadowBlocker{
			Code:    TikTokBillShadowBlockerExistingBill,
			Message: "Order ID นี้มี Bill TikTok ที่ยังใช้งานอยู่แล้ว จึงห้ามสร้างซ้ำ",
		})
	}
	sort.SliceStable(preview.Blockers, func(i, j int) bool {
		if preview.Blockers[i].Code == preview.Blockers[j].Code {
			return tikTokBillShadowItemKey(preview.Blockers[i].ProductID, preview.Blockers[i].SKUID) < tikTokBillShadowItemKey(preview.Blockers[j].ProductID, preview.Blockers[j].SKUID)
		}
		return preview.Blockers[i].Code < preview.Blockers[j].Code
	})
	preview.ReadyForReviewedBill = len(preview.Blockers) == 0
	preview.SourceHash = strings.TrimSpace(source.SourceHash)
	preview.ReviewDigest = tikTokBillShadowReviewDigest(preview)
	return preview, nil
}

func tikTokBillShadowReviewDigest(preview *TikTokBillShadowPreview) string {
	if preview == nil {
		return ""
	}
	type digestMapping struct {
		Status                string `json:"status"`
		AliasID               string `json:"alias_id"`
		ItemCode              string `json:"item_code"`
		UnitCode              string `json:"unit_code"`
		MarketplaceQuantity   string `json:"marketplace_quantity"`
		SMLQuantity           string `json:"sml_quantity"`
		BaseQuantity          string `json:"base_quantity"`
		MappingRevision       int64  `json:"mapping_revision"`
		QuantityMultiplier    int64  `json:"quantity_multiplier"`
		UnitStandValue        string `json:"unit_stand_value"`
		UnitDivideValue       string `json:"unit_divide_value"`
		UnitCatalogGeneration string `json:"unit_catalog_generation"`
		SetDefinitionHash     string `json:"set_definition_hash"`
	}
	type digestItem struct {
		ProductID     string        `json:"product_id"`
		SKUID         string        `json:"sku_id"`
		SellerSKU     string        `json:"seller_sku"`
		ProductName   string        `json:"product_name"`
		VariantName   string        `json:"variant_name"`
		Quantity      int           `json:"quantity"`
		UnitSalePrice string        `json:"unit_sale_price"`
		LineTotal     string        `json:"line_total"`
		Mapping       digestMapping `json:"mapping"`
	}
	items := make([]digestItem, 0, len(preview.Items))
	for _, item := range preview.Items {
		mapping := item.Mapping
		items = append(items, digestItem{
			ProductID: item.ProductID, SKUID: item.SKUID, SellerSKU: item.SellerSKU,
			ProductName: item.ProductName, VariantName: item.VariantName, Quantity: item.Quantity,
			UnitSalePrice: item.UnitSalePrice, LineTotal: item.LineTotal,
			Mapping: digestMapping{
				Status: mapping.Status, AliasID: mapping.AliasID, ItemCode: mapping.ItemCode, UnitCode: mapping.UnitCode,
				MarketplaceQuantity: mapping.MarketplaceQuantity, SMLQuantity: mapping.SMLQuantity,
				BaseQuantity: mapping.BaseQuantity, MappingRevision: mapping.MappingRevision,
				QuantityMultiplier: mapping.QuantityMultiplier, UnitStandValue: mapping.UnitStandValue,
				UnitDivideValue: mapping.UnitDivideValue, UnitCatalogGeneration: mapping.UnitCatalogGeneration,
				SetDefinitionHash: mapping.SetDefinitionHash,
			},
		})
	}
	payload := struct {
		Version      string                  `json:"version"`
		ShopID       string                  `json:"shop_id"`
		OrderID      string                  `json:"order_id"`
		OrderStatus  OrderStatus             `json:"order_status"`
		Currency     string                  `json:"currency"`
		LastSyncedAt string                  `json:"last_synced_at"`
		SourceHash   string                  `json:"source_hash"`
		Amounts      TikTokBillShadowAmounts `json:"amounts"`
		Route        struct {
			Ready                bool   `json:"ready"`
			SemanticRoute        string `json:"semantic_route"`
			DocFormatCode        string `json:"doc_format_code"`
			ShippingReady        bool   `json:"shipping_ready"`
			ShippingItemCode     string `json:"shipping_item_code"`
			ShippingItemUnitCode string `json:"shipping_item_unit_code"`
		} `json:"route"`
		Items []digestItem `json:"items"`
	}{
		Version: "tiktok_reviewed_bill_v1", ShopID: preview.ShopID, OrderID: preview.OrderID,
		OrderStatus: preview.OrderStatus, Currency: preview.Currency,
		LastSyncedAt: preview.LastSyncedAt.UTC().Format(time.RFC3339Nano), SourceHash: preview.SourceHash,
		Amounts: preview.Amounts, Items: items,
	}
	payload.Route.Ready = preview.Route.Ready
	payload.Route.SemanticRoute = preview.Route.SemanticRoute
	payload.Route.DocFormatCode = preview.Route.DocFormatCode
	payload.Route.ShippingReady = preview.Route.ShippingReady
	payload.Route.ShippingItemCode = preview.Route.ShippingItemCode
	payload.Route.ShippingItemUnitCode = preview.Route.ShippingItemUnitCode
	encoded, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func tikTokBillLifecycleReady(status OrderStatus) bool {
	switch status {
	case OrderStatusAwaitingCollection, OrderStatusInTransit, OrderStatusDelivered, OrderStatusCompleted:
		return true
	default:
		return false
	}
}

func tikTokBillShadowSemanticRoute(endpoint string) string {
	value := strings.ToLower(strings.TrimSpace(endpoint))
	switch {
	case value == "saleinvoice", value == "sale-invoices", strings.Contains(value, "sale-invoices"), strings.Contains(value, "saleinvoice"):
		return "sale_invoice"
	case value == "saleorder", value == "sale-orders", strings.Contains(value, "sale-orders"), strings.Contains(value, "saleorder"):
		return "sale_order"
	default:
		return ""
	}
}

func shadowMoney(raw string) (*big.Rat, error) {
	_, value, err := snapshotMoney(raw, false)
	return value, err
}

func shadowMoneyText(value *big.Rat) string {
	if value == nil {
		return "0.00"
	}
	return value.FloatString(2)
}

func tikTokBillShadowItemKey(productID, skuID string) string {
	return strings.TrimSpace(productID) + "\x00" + strings.TrimSpace(skuID)
}

func appendTikTokBillShadowBlocker(blockers *[]TikTokBillShadowBlocker, candidate TikTokBillShadowBlocker) {
	for _, blocker := range *blockers {
		if blocker.Code == candidate.Code && blocker.ProductID == candidate.ProductID && blocker.SKUID == candidate.SKUID {
			return
		}
	}
	*blockers = append(*blockers, candidate)
}

type TikTokBillShadowStore struct {
	database *sql.DB
}

func NewTikTokBillShadowStore(database *sql.DB) *TikTokBillShadowStore {
	return &TikTokBillShadowStore{database: database}
}

func (s *TikTokBillShadowStore) Load(ctx context.Context, shopID, orderID string) (*TikTokBillShadowSource, error) {
	if s == nil || s.database == nil {
		return nil, ErrTikTokBillShadowStoreNotConfigured
	}
	shopID = strings.TrimSpace(shopID)
	orderID = strings.TrimSpace(orderID)
	if !ValidTikTokShopID(shopID) || !ValidTikTokShopID(orderID) {
		return nil, ErrTikTokBillShadowInvalidInput
	}
	var source TikTokBillShadowSource
	var safeOrder, safePrice, normalizedItems []byte
	err := s.database.QueryRowContext(ctx,
		`SELECT s.shop_id, c.shop_name, s.order_status, s.currency,
		        s.payment_total_amount::text, s.product_subtotal_amount::text,
		        s.shipping_fee_amount::text, s.item_insurance_fee_amount::text,
		        s.safe_order, s.safe_price_detail, s.normalized_items, s.last_synced_at, s.source_hash
		   FROM tiktok_shop_order_snapshots s
		   JOIN tiktok_shop_connections c ON c.shop_id=s.shop_id AND c.disabled_at IS NULL
		  WHERE s.shop_id=$1 AND s.order_id=$2`, shopID, orderID,
	).Scan(
		&source.ShopID, &source.ShopName, &source.StoredOrderStatus, &source.StoredCurrency,
		&source.StoredPaymentTotal, &source.StoredProductSubtotal, &source.StoredShippingFee, &source.StoredItemInsuranceFee,
		&safeOrder, &safePrice, &normalizedItems, &source.LastSyncedAt, &source.SourceHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTikTokBillShadowNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load TikTok Shop bill shadow snapshot: %w", err)
	}
	if json.Unmarshal(safeOrder, &source.Order) != nil || json.Unmarshal(safePrice, &source.Price) != nil ||
		json.Unmarshal(normalizedItems, &source.Items) != nil || len(source.Items) == 0 {
		return nil, ErrTikTokBillShadowSourceInvalid
	}

	rows, err := s.database.QueryContext(ctx,
		`SELECT a.id::text, a.external_item_id, a.external_variant_id, a.account_key,
		        a.item_code, a.unit_code, a.is_active, a.scope_confirmed,
		        a.sales_enabled, a.conversion_status, a.quantity_multiplier,
		        COALESCE(a.unit_stand_value::text,''), COALESCE(a.unit_divide_value::text,''),
		        a.mapping_revision, COALESCE(a.unit_catalog_generation::text,''),
		        COALESCE((SELECT catalog.set_definition_hash FROM sml_catalog catalog
		                   WHERE catalog.catalog_generation_id=a.unit_catalog_generation
		                     AND catalog.item_code=a.item_code AND catalog.is_active=true LIMIT 1),''),
		        COALESCE(g.status='active'
		          AND EXISTS (
		            SELECT 1 FROM sml_catalog_units u
		             WHERE u.generation_id=a.unit_catalog_generation
		               AND u.item_code=a.item_code AND u.unit_code=a.unit_code AND u.is_active=true
		          )
		          AND EXISTS (
		            SELECT 1 FROM sml_catalog catalog
		             WHERE catalog.catalog_generation_id=a.unit_catalog_generation
		               AND catalog.item_code=a.item_code AND catalog.is_active=true
		          ), false) AS catalog_ready
		   FROM jsonb_to_recordset($1::jsonb) AS item(product_id text, sku_id text)
		   JOIN marketplace_item_aliases a
		     ON a.source='tiktok'
		    AND a.account_key IN ($2, 'default')
		    AND a.external_item_id=item.product_id
		    AND a.external_variant_id=item.sku_id
		   LEFT JOIN sml_catalog_sync_runs g ON g.id=a.unit_catalog_generation
		  ORDER BY a.external_item_id, a.external_variant_id,
		           CASE WHEN a.account_key=$2 THEN 0 ELSE 1 END`, normalizedItems, "shop:"+shopID,
	)
	if err != nil {
		return nil, fmt.Errorf("load TikTok Shop bill shadow mappings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var mapping TikTokBillShadowMappingSource
		if err := rows.Scan(
			&mapping.AliasID, &mapping.ProductID, &mapping.SKUID, &mapping.AccountKey, &mapping.ItemCode, &mapping.UnitCode,
			&mapping.IsActive, &mapping.ScopeConfirmed, &mapping.SalesEnabled, &mapping.ConversionStatus,
			&mapping.QuantityMultiplier, &mapping.UnitStandValue, &mapping.UnitDivideValue,
			&mapping.MappingRevision, &mapping.UnitCatalogGeneration, &mapping.SetDefinitionHash, &mapping.CatalogReady,
		); err != nil {
			return nil, fmt.Errorf("scan TikTok Shop bill shadow mapping: %w", err)
		}
		source.Mappings = append(source.Mappings, mapping)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate TikTok Shop bill shadow mappings: %w", err)
	}

	err = s.database.QueryRowContext(ctx,
		`SELECT endpoint, doc_format_code, shipping_item_enabled,
		        shipping_item_code, shipping_item_unit_code
		   FROM channel_defaults
		  WHERE channel='tiktok' AND bill_type='sale'`,
	).Scan(
		&source.Route.Endpoint, &source.Route.DocFormatCode, &source.Route.ShippingItemEnabled,
		&source.Route.ShippingItemCode, &source.Route.ShippingItemUnitCode,
	)
	if err == nil {
		source.Route.Configured = true
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("load TikTok Shop bill shadow route: %w", err)
	}

	var existing TikTokBillShadowExistingBill
	err = s.database.QueryRowContext(ctx,
		`SELECT id::text, status, COALESCE(sml_doc_no,''), source_account_key,
		        COALESCE(raw_data->>'flow',''), document_route
		   FROM bills
		  WHERE source='tiktok' AND sml_order_id=$1 AND archived_at IS NULL
		  ORDER BY created_at DESC, id DESC
		  LIMIT 1`, orderID,
	).Scan(&existing.ID, &existing.Status, &existing.SMLDocNo, &existing.SourceAccountKey, &existing.SourceFlow, &existing.DocumentRoute)
	if err == nil {
		source.ExistingBill = &existing
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("load existing TikTok Shop bill: %w", err)
	}
	return &source, nil
}
