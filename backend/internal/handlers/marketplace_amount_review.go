package handlers

import (
	"encoding/json"

	"nexflow/internal/models"
)

type marketplaceAmountReviewSource struct {
	OrderAmount        *float64 `json:"order_total_amount"`
	PaidAmount         *float64 `json:"paid_total_amount"`
	ItemGrossAmount    *float64 `json:"item_gross_amount"`
	PlatformDiscount   *float64 `json:"platform_discount_amount"`
	SellerDiscount     *float64 `json:"seller_discount_amount"`
	DiscountAmount     *float64 `json:"discount_amount"`
	NetProductAmount   *float64 `json:"net_product_amount"`
	ShippingAmount     *float64 `json:"shipping_amount"`
	TaxesAmount        *float64 `json:"taxes_amount"`
	PaymentDiscount    *float64 `json:"payment_discount_amount"`
	AmountDifference   *float64 `json:"amount_difference"`
	AmountReviewReason string   `json:"amount_review_reason"`
}

func marketplaceAmountReviewAuditDetail(bill *models.Bill, fingerprint string) map[string]any {
	source := marketplaceAmountReviewSource{}
	if bill != nil && len(bill.RawData) > 0 {
		_ = json.Unmarshal(bill.RawData, &source)
	}

	orderAmount := amountReviewValue(source.OrderAmount)
	if source.OrderAmount == nil {
		orderAmount = amountReviewValue(source.PaidAmount)
	}
	itemGross := amountReviewValue(source.ItemGrossAmount)
	platformDiscount := amountReviewValue(source.PlatformDiscount)
	sellerDiscount := amountReviewValue(source.SellerDiscount)
	discount := amountReviewValue(source.DiscountAmount)
	if source.DiscountAmount == nil {
		discount = platformDiscount + sellerDiscount
	}
	netProduct := amountReviewValue(source.NetProductAmount)
	if source.NetProductAmount == nil {
		netProduct = itemGross - discount
	}
	shipping := amountReviewValue(source.ShippingAmount)
	taxes := amountReviewValue(source.TaxesAmount)
	paymentDiscount := amountReviewValue(source.PaymentDiscount)
	itemizedAmount := moneyFromCents(centsFromMoney(netProduct) + centsFromMoney(shipping) + centsFromMoney(taxes) - centsFromMoney(paymentDiscount))
	unallocated := moneyFromCents(centsFromMoney(orderAmount) - centsFromMoney(itemizedAmount))

	var smlCents int64
	if bill != nil {
		for _, item := range bill.Items {
			gross := item.Qty * amountReviewValue(item.Price)
			if item.GrossAmount != nil {
				gross = *item.GrossAmount
			}
			lineCents := centsFromMoney(gross) - centsFromMoney(item.DiscountAmount)
			if lineCents > 0 {
				smlCents += lineCents
			}
		}
	}

	sourceName := ""
	unallocatedKind := "marketplace_charge_not_itemized"
	if bill != nil {
		sourceName = bill.Source
		if bill.Source == "tiktok" {
			// TikTok's Excel export can include buyer-paid protection/insurance in
			// Order Amount without exposing a dedicated column for that charge. It
			// is a buyer/platform charge, not a bill line or proven seller revenue.
			unallocatedKind = "tiktok_buyer_protection_or_unitemized_charge"
		}
	}
	return map[string]any{
		"fingerprint":                    fingerprint,
		"marketplace_source":             sourceName,
		"marketplace_order_amount":       moneyFromCents(centsFromMoney(orderAmount)),
		"item_gross_amount":              moneyFromCents(centsFromMoney(itemGross)),
		"platform_discount_amount":       moneyFromCents(centsFromMoney(platformDiscount)),
		"seller_discount_amount":         moneyFromCents(centsFromMoney(sellerDiscount)),
		"net_product_amount":             moneyFromCents(centsFromMoney(netProduct)),
		"shipping_amount":                moneyFromCents(centsFromMoney(shipping)),
		"taxes_amount":                   moneyFromCents(centsFromMoney(taxes)),
		"payment_discount_amount":        moneyFromCents(centsFromMoney(paymentDiscount)),
		"marketplace_itemized_amount":    itemizedAmount,
		"unallocated_marketplace_amount": unallocated,
		"unallocated_amount_kind":        unallocatedKind,
		"sml_document_amount":            moneyFromCents(smlCents),
		"sml_amount_authority":           "bill_items",
		"review_reason":                  source.AmountReviewReason,
	}
}

func amountReviewValue(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}
