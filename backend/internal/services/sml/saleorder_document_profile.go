package sml

import (
	"fmt"
	"math"
	"strings"
)

// ApplySaleOrderDocumentProfile applies the same immutable Profile V1 envelope
// as Sale Invoice while preserving the legacy Sale Order JSON when mode is off.
func ApplySaleOrderDocumentProfile(payload *SaleOrderPayload, opts InvoiceDocumentProfileOptions) error {
	if payload == nil {
		return fmt.Errorf("sale order payload is required")
	}
	mode := strings.ToLower(strings.TrimSpace(opts.Mode))
	if mode == "off" || mode == "" {
		return nil
	}
	if mode != "shadow" && mode != "active" {
		return fmt.Errorf("document profile mode must be off, shadow, or active")
	}
	if len(payload.Items) == 0 || len(payload.Items) > MaxInvoiceDocumentItems {
		return fmt.Errorf("document profile requires 1-%d items", MaxInvoiceDocumentItems)
	}
	for _, field := range []struct{ name, value string }{
		{"remark", payload.Remark}, {"remark_2", payload.Remark2}, {"remark_5", opts.Remark5},
	} {
		if err := validateInvoiceProfileText(field.name, field.value, 255); err != nil {
			return err
		}
	}
	if !strings.HasPrefix(opts.Remark5, "NEXFLOW|") {
		return fmt.Errorf("remark_5 must use NEXFLOW|<channel>|<order-or-bill>")
	}

	applicability := strings.TrimSpace(opts.ShipmentApplicability)
	payload.ProfileMode = mode
	payload.ProfileConfigVersion = opts.ConfigVersion
	payload.ProfileRouteSignature = strings.TrimSpace(opts.RouteSignature)
	payload.MarketplacePhysicalGoods = opts.MarketplacePhysicalGoods
	payload.ShipmentApplicability = applicability
	payload.Shipment = opts.Shipment
	payload.CreatorCode = "BILLFLOW"
	payload.CashierCode = "BILLFLOW"
	payload.UserRequest = "NEXFLOW"
	payload.CurrencyCode = "THB"
	payload.ExchangeRateDecimal = "1"
	payload.Remark5 = opts.Remark5
	if mode == "active" {
		payload.DocumentProfileVersion = InvoiceDocumentProfileVersion
	}

	payload.VATRateDecimal = exactProfileDecimal(payload.VATRate, 6)
	payload.TotalValueDecimal = exactProfileDecimal(payload.TotalValue, 2)
	payload.TotalDiscountDecimal = exactProfileDecimal(payload.TotalDiscount, 2)
	payload.TotalBeforeVATDecimal = exactProfileDecimal(payload.TotalBeforeVAT, 2)
	payload.TotalVATValueDecimal = exactProfileDecimal(payload.TotalVATValue, 2)
	payload.TotalExceptVATDecimal = exactProfileDecimal(payload.TotalExceptVAT, 2)
	payload.TotalAfterVATDecimal = exactProfileDecimal(payload.TotalAfterVAT, 2)
	payload.TotalAmountDecimal = exactProfileDecimal(payload.TotalAmount, 2)
	var detailTotal int64
	for i := range payload.Items {
		item := &payload.Items[i]
		for _, value := range []float64{item.Qty, item.Price, item.PriceExcludeVAT, item.DiscountAmount, item.SumAmount, item.VATAmount, item.SumAmountExclVAT} {
			if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
				return fmt.Errorf("item %d contains an invalid decimal value", i)
			}
		}
		if item.Qty <= 0 {
			return fmt.Errorf("item %d qty must be greater than zero", i)
		}
		item.QtyDecimal = exactProfileDecimal(item.Qty, 6)
		item.PriceDecimal = exactProfileDecimal(item.Price, 6)
		item.PriceExcludeVATDecimal = exactProfileDecimal(item.PriceExcludeVAT, 6)
		item.DiscountAmountDecimal = exactProfileDecimal(item.DiscountAmount, 2)
		item.SumAmountDecimal = exactProfileDecimal(item.SumAmount, 2)
		item.VATAmountDecimal = exactProfileDecimal(item.VATAmount, 2)
		item.SumAmountExclVATDecimal = exactProfileDecimal(item.SumAmountExclVAT, 2)
		detailTotal += int64(math.Round(item.SumAmount * 100))
	}
	if delta := int64(math.Round(payload.TotalValue*100)) - detailTotal; delta < -1 || delta > 1 {
		return fmt.Errorf("document total does not match item sum within 0.01")
	}
	if opts.MarketplacePhysicalGoods && applicability != "required" {
		return fmt.Errorf("marketplace physical-goods document requires shipment")
	}
	switch applicability {
	case "required":
		if opts.Shipment == nil {
			return fmt.Errorf("shipment is required before the core document can be created")
		}
		for _, field := range []struct {
			name, value string
			max         int
		}{
			{"shipment.transport_name", opts.Shipment.TransportName, 255},
			{"shipment.transport_address", opts.Shipment.TransportAddress, 1000},
			{"shipment.transport_telephone", opts.Shipment.TransportTelephone, 100},
		} {
			if strings.TrimSpace(field.value) == "" {
				return fmt.Errorf("%s is required", field.name)
			}
			if err := validateInvoiceProfileText(field.name, field.value, field.max); err != nil {
				return err
			}
		}
	case "not_applicable":
		if opts.MarketplacePhysicalGoods || opts.Shipment != nil {
			return fmt.Errorf("shipment must be required for marketplace goods and omitted when not applicable")
		}
	default:
		return fmt.Errorf("shipment_applicability must be required or not_applicable")
	}
	return nil
}
