package sml

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	InvoiceDocumentProfileVersion = "sml-document-v1"
	MaxInvoiceDocumentItems       = 500
	MaxInvoiceDocumentBytes       = 2 << 20
)

type InvoiceDocumentProfileOptions struct {
	Mode                     string
	Channel                  string
	ConfigVersion            int64
	RouteSignature           string
	Remark5                  string
	MarketplacePhysicalGoods bool
	ShipmentApplicability    string
	Shipment                 *InvoiceShipment
}

type InvoiceDocumentProfileResult struct {
	Version                string
	PayloadHash            string
	CoreStatus             string
	ProfileStatus          string
	RequiredChecks         []string
	CompletedChecks        []string
	ReconciliationRequired bool
}

// ApplyInvoiceDocumentProfile makes decimal strings authoritative for both
// shadow validation and active writes. Shadow intentionally omits the opt-in
// version so the Gateway executes its legacy core path without supplements.
func ApplyInvoiceDocumentProfile(payload *InvoicePayload, opts InvoiceDocumentProfileOptions) error {
	if payload == nil {
		return fmt.Errorf("invoice payload is required")
	}
	mode := strings.ToLower(strings.TrimSpace(opts.Mode))
	if mode == "off" || mode == "" {
		return nil
	}
	if mode != "shadow" && mode != "active" {
		return fmt.Errorf("document profile mode must be off, shadow, or active")
	}
	if len(payload.Details) == 0 || len(payload.Details) > MaxInvoiceDocumentItems {
		return fmt.Errorf("document profile requires 1-%d details", MaxInvoiceDocumentItems)
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
	for i := range payload.Details {
		detail := &payload.Details[i]
		for _, value := range []float64{detail.Qty, detail.Price, detail.PriceExcludeVAT, detail.DiscountAmount, detail.SumAmount, detail.VATAmount, detail.SumAmountExclVAT} {
			if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
				return fmt.Errorf("detail %d contains an invalid decimal value", i)
			}
		}
		if detail.Qty <= 0 {
			return fmt.Errorf("detail %d qty must be greater than zero", i)
		}
		detail.QtyDecimal = exactProfileDecimal(detail.Qty, 6)
		detail.PriceDecimal = exactProfileDecimal(detail.Price, 6)
		detail.PriceExcludeVATDecimal = exactProfileDecimal(detail.PriceExcludeVAT, 6)
		detail.DiscountAmountDecimal = exactProfileDecimal(detail.DiscountAmount, 2)
		detail.SumAmountDecimal = exactProfileDecimal(detail.SumAmount, 2)
		detail.VATAmountDecimal = exactProfileDecimal(detail.VATAmount, 2)
		detail.SumAmountExclVATDecimal = exactProfileDecimal(detail.SumAmountExclVAT, 2)
		detailTotal += int64(math.Round(detail.SumAmount * 100))
	}
	headerTotal := int64(math.Round(payload.TotalValue * 100))
	if delta := headerTotal - detailTotal; delta < -1 || delta > 1 {
		return fmt.Errorf("document total does not match detail sum within 0.01")
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
			return fmt.Errorf("shipment not_applicable is invalid for this document")
		}
	default:
		return fmt.Errorf("shipment_applicability must be required or not_applicable")
	}
	return nil
}

func exactProfileDecimal(value float64, scale int) string {
	return strconv.FormatFloat(value, 'f', scale, 64)
}

func validateInvoiceProfileText(field, value string, max int) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s must be valid UTF-8", field)
	}
	if utf8.RuneCountInString(value) > max {
		return fmt.Errorf("%s must not exceed %d Unicode characters", field, max)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("%s must not contain control characters", field)
		}
	}
	return nil
}
