package tiktokshop

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"nexflow/internal/models"
)

const TikTokReviewedBillFlow = "tiktok_shop_api_reviewed"

var (
	ErrTikTokReviewedBillInvalidInput  = errors.New("invalid TikTok Shop reviewed bill input")
	ErrTikTokReviewedBillNotReady      = errors.New("TikTok Shop reviewed bill is not ready")
	ErrTikTokReviewedBillReviewChanged = errors.New("TikTok Shop reviewed bill evidence changed")
	ErrTikTokReviewedBillConflict      = errors.New("TikTok Shop order belongs to another bill flow or scope")
)

type TikTokReviewedBillInput struct {
	ShopID       string
	OrderID      string
	ReviewDigest string
	ActorID      string
	TraceID      string
}

type TikTokReviewedBillResult struct {
	BillID        string `json:"bill_id"`
	Status        string `json:"status"`
	Reused        bool   `json:"reused"`
	DocumentRoute string `json:"document_route"`
	ReviewPath    string `json:"review_path"`
	Message       string `json:"message"`
}

type TikTokReviewedBillWriter interface {
	CreateWithItemsAndAudit(*models.Bill, []models.BillItem, models.AuditEntry) error
}

type TikTokReviewedBillService struct {
	loader TikTokBillShadowSourceLoader
	writer TikTokReviewedBillWriter
}

func NewTikTokReviewedBillService(loader TikTokBillShadowSourceLoader, writer TikTokReviewedBillWriter) *TikTokReviewedBillService {
	return &TikTokReviewedBillService{loader: loader, writer: writer}
}

func (s *TikTokReviewedBillService) Create(ctx context.Context, input TikTokReviewedBillInput) (*TikTokReviewedBillResult, error) {
	input.ShopID = strings.TrimSpace(input.ShopID)
	input.OrderID = strings.TrimSpace(input.OrderID)
	input.ReviewDigest = strings.TrimSpace(input.ReviewDigest)
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.TraceID = strings.TrimSpace(input.TraceID)
	if s == nil || s.loader == nil || s.writer == nil || !ValidTikTokShopID(input.ShopID) ||
		!ValidTikTokShopID(input.OrderID) || !validTikTokBillShadowImpactDigest(input.ReviewDigest) || input.ActorID == "" {
		return nil, ErrTikTokReviewedBillInvalidInput
	}

	preview, err := NewTikTokBillShadowService(s.loader).Preview(ctx, input.ShopID, input.OrderID)
	if err != nil {
		return nil, err
	}
	if preview.ExistingBill != nil {
		return reviewedTikTokExistingBill(preview.ExistingBill, input.ShopID)
	}
	if !preview.ReadyForReviewedBill || len(preview.Blockers) != 0 || preview.ReviewDigest == "" {
		return nil, ErrTikTokReviewedBillNotReady
	}
	if subtle.ConstantTimeCompare([]byte(preview.ReviewDigest), []byte(input.ReviewDigest)) != 1 {
		return nil, ErrTikTokReviewedBillReviewChanged
	}

	bill, items, audit, err := buildReviewedTikTokBill(preview, input)
	if err != nil {
		return nil, err
	}
	if err := s.writer.CreateWithItemsAndAudit(bill, items, audit); err != nil {
		// The database uniqueness guard may have won a concurrent retry. Reload
		// and only return success when the winner is the same reviewed shop flow.
		reloaded, reloadErr := NewTikTokBillShadowService(s.loader).Preview(ctx, input.ShopID, input.OrderID)
		if reloadErr == nil && reloaded != nil && reloaded.ExistingBill != nil {
			if existing, existingErr := reviewedTikTokExistingBill(reloaded.ExistingBill, input.ShopID); existingErr == nil {
				return existing, nil
			}
		}
		return nil, fmt.Errorf("create reviewed TikTok Shop bill: %w", err)
	}
	return &TikTokReviewedBillResult{
		BillID: bill.ID, Status: bill.Status, DocumentRoute: bill.DocumentRoute,
		ReviewPath: tikTokReviewedBillPath(bill.DocumentRoute),
		Message:    "สร้าง Bill ใน Nexflow แล้ว ยังไม่ได้ส่งเข้า SML",
	}, nil
}

func reviewedTikTokExistingBill(existing *TikTokBillShadowExistingBill, shopID string) (*TikTokReviewedBillResult, error) {
	if existing == nil || strings.TrimSpace(existing.ID) == "" {
		return nil, ErrTikTokReviewedBillConflict
	}
	if existing.SourceAccountKey != "shop:"+strings.TrimSpace(shopID) || existing.SourceFlow != TikTokReviewedBillFlow {
		return nil, ErrTikTokReviewedBillConflict
	}
	route := strings.TrimSpace(existing.DocumentRoute)
	if route != "saleinvoice" && route != "saleorder" {
		return nil, ErrTikTokReviewedBillConflict
	}
	return &TikTokReviewedBillResult{
		BillID: existing.ID, Status: existing.Status, Reused: true, DocumentRoute: route,
		ReviewPath: tikTokReviewedBillPath(route), Message: "Order นี้สร้าง Bill ที่ตรวจทานแล้วไว้ก่อนหน้านี้",
	}, nil
}

func buildReviewedTikTokBill(preview *TikTokBillShadowPreview, input TikTokReviewedBillInput) (*models.Bill, []models.BillItem, models.AuditEntry, error) {
	if preview == nil || len(preview.Items) == 0 {
		return nil, nil, models.AuditEntry{}, ErrTikTokReviewedBillNotReady
	}
	documentRoute := tikTokReviewedBillRoute(preview.Route.SemanticRoute)
	if documentRoute == "" {
		return nil, nil, models.AuditEntry{}, ErrTikTokReviewedBillNotReady
	}
	items := make([]models.BillItem, 0, len(preview.Items)+1)
	for index, source := range preview.Items {
		mapping := source.Mapping
		if mapping.Status != TikTokBillShadowMappingReady || mapping.AliasID == "" || mapping.MappingRevision < 1 ||
			mapping.ItemCode == "" || mapping.UnitCode == "" || mapping.UnitCatalogGeneration == "" {
			return nil, nil, models.AuditEntry{}, ErrTikTokReviewedBillNotReady
		}
		price, err := strconv.ParseFloat(source.UnitSalePrice, 64)
		if err != nil {
			return nil, nil, models.AuditEntry{}, ErrTikTokBillShadowSourceInvalid
		}
		gross, err := strconv.ParseFloat(source.LineTotal, 64)
		if err != nil {
			return nil, nil, models.AuditEntry{}, ErrTikTokBillShadowSourceInvalid
		}
		smlQty, err := strconv.ParseFloat(mapping.SMLQuantity, 64)
		if err != nil {
			return nil, nil, models.AuditEntry{}, ErrTikTokBillShadowSourceInvalid
		}
		sourceQty := float64(source.Quantity)
		multiplier := mapping.QuantityMultiplier
		revision := mapping.MappingRevision
		aliasID, itemCode, unitCode := mapping.AliasID, mapping.ItemCode, mapping.UnitCode
		stand, divide, baseQty, generation := mapping.UnitStandValue, mapping.UnitDivideValue, mapping.BaseQuantity, mapping.UnitCatalogGeneration
		items = append(items, models.BillItem{
			RawName: reviewedTikTokRawName(source.ProductName, source.VariantName), SourceSKU: source.SellerSKU,
			SourceItemID: source.ProductID, SourceVariantID: source.SKUID,
			SourceLineID:       fmt.Sprintf("%s:%06d:%s:%s", preview.OrderID, index+1, source.ProductID, source.SKUID),
			MarketplaceAliasID: &aliasID, ItemCode: &itemCode, UnitCode: &unitCode,
			Qty: sourceQty, SourceQty: &sourceQty, SMLQty: &smlQty, Price: &price, GrossAmount: &gross,
			Mapped: true, QuantityMultiplierSnapshot: &multiplier,
			UnitStandValueSnapshot: &stand, UnitDivideValueSnapshot: &divide, BaseQtySnapshot: &baseQty,
			MappingRevisionSnapshot: &revision, UnitCatalogGenerationSnapshot: &generation,
			SetDefinitionHashSnapshot: mapping.SetDefinitionHash,
		})
	}
	if preview.Amounts.Shipping != "0.00" {
		shipping, err := strconv.ParseFloat(preview.Amounts.Shipping, 64)
		if err != nil || shipping <= 0 || preview.Route.ShippingItemCode == "" || preview.Route.ShippingItemUnitCode == "" {
			return nil, nil, models.AuditEntry{}, ErrTikTokReviewedBillNotReady
		}
		code, unit := preview.Route.ShippingItemCode, preview.Route.ShippingItemUnitCode
		items = append(items, models.BillItem{
			RawName: "ค่าจัดส่ง TikTok Shop", SourceSKU: models.TikTokShippingSourceSKU,
			SourceLineID: preview.OrderID + ":shipping", ItemCode: &code, UnitCode: &unit,
			Qty: 1, Price: &shipping, GrossAmount: &shipping, Mapped: true,
		})
	}
	rawData, err := json.Marshal(map[string]interface{}{
		"flow": TikTokReviewedBillFlow, "order_id": preview.OrderID, "tiktok_order_id": preview.OrderID,
		"tiktok_shop_id": preview.ShopID, "tiktok_shop_name": preview.ShopName, "source_hash": preview.SourceHash,
		"order_status": preview.OrderStatus, "currency": preview.Currency,
		"last_synced_at": preview.LastSyncedAt.UTC(), "review_digest": input.ReviewDigest,
		"product_subtotal": preview.Amounts.ProductSubtotal, "shipping_amount": preview.Amounts.Shipping,
		"proposed_document_total": preview.Amounts.ProposedDocumentTotal,
		"buyer_payment":           preview.Amounts.BuyerPayment, "item_insurance_fee": preview.Amounts.ItemInsurance,
		"excluded_buyer_platform_charges": preview.Amounts.ExcludedBuyerPlatformCharges,
		"document_route":                  documentRoute, "doc_format_code": preview.Route.DocFormatCode,
	})
	if err != nil {
		return nil, nil, models.AuditEntry{}, err
	}
	actorID := input.ActorID
	bill := &models.Bill{
		BillType: "sale", Source: "tiktok", SourceAccountKey: "shop:" + preview.ShopID,
		Status: "pending", DocumentRoute: documentRoute, RawData: rawData,
		SMLOrderID: preview.OrderID, CreatedBy: &actorID,
	}
	audit := models.AuditEntry{
		Action: "bill_created", UserID: &actorID, Source: "tiktok_shop", Level: "info", TraceID: input.TraceID,
		Detail: map[string]interface{}{
			"flow": TikTokReviewedBillFlow, "shop_id": preview.ShopID, "order_id": preview.OrderID,
			"shop_name": preview.ShopName, "items_count": len(items), "total_amount": preview.Amounts.ProposedDocumentTotal,
			"document_route": documentRoute, "doc_format_code": preview.Route.DocFormatCode,
			"status": bill.Status, "review_digest": input.ReviewDigest,
			"sml_write": false, "notification_write": false,
		},
	}
	return bill, items, audit, nil
}

func tikTokReviewedBillRoute(semantic string) string {
	switch strings.TrimSpace(semantic) {
	case "sale_invoice":
		return "saleinvoice"
	case "sale_order":
		return "saleorder"
	default:
		return ""
	}
}

func tikTokReviewedBillPath(route string) string {
	if route == "saleinvoice" {
		return "/sale-invoices"
	}
	return "/sales-orders"
}

func reviewedTikTokRawName(productName, variantName string) string {
	productName = strings.TrimSpace(productName)
	variantName = strings.TrimSpace(variantName)
	if productName == "" {
		return variantName
	}
	if variantName == "" || variantName == "-" {
		return productName
	}
	return productName + " / " + variantName
}
