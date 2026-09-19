package handlers

import (
	"context"
	"strings"

	"nexflow/internal/models"
	"nexflow/internal/services/sml"
	"nexflow/internal/services/smlprofile"
)

type resolvedInvoiceDocumentProfile struct {
	Mode    string
	Remark  string
	Remark2 string
	Options sml.InvoiceDocumentProfileOptions
}

func (h *BillHandler) resolveInvoiceDocumentProfile(ctx context.Context, bill *models.Bill, def *models.ChannelDefault, req RetryRequest, docNo string) (resolvedInvoiceDocumentProfile, error) {
	return h.resolveSalesDocumentProfile(ctx, bill, def, req, docNo, "saleinvoice")
}

func (h *BillHandler) resolveSaleOrderDocumentProfile(ctx context.Context, bill *models.Bill, def *models.ChannelDefault, req RetryRequest, docNo string) (resolvedInvoiceDocumentProfile, error) {
	return h.resolveSalesDocumentProfile(ctx, bill, def, req, docNo, "saleorder")
}

func (h *BillHandler) resolveSalesDocumentProfile(ctx context.Context, bill *models.Bill, def *models.ChannelDefault, req RetryRequest, docNo, route string) (resolvedInvoiceDocumentProfile, error) {
	mode := smlprofile.ModeOff
	if h != nil && h.cfg != nil {
		if configured, ok := h.cfg.SMLDocumentProfileRouteModes[route]; ok {
			mode = configured
		} else if route == "saleinvoice" {
			mode = h.cfg.SMLDocumentProfileMode
		}
	}
	channel := channelDefaultKeyForBill(bill)
	orderRef := docRefFromBill(bill)

	remark := strings.TrimSpace(req.Remark)
	if remark != "" {
		if err := smlprofile.ValidateFreeText("remark", remark); err != nil {
			return resolvedInvoiceDocumentProfile{}, err
		}
	} else if def != nil && strings.TrimSpace(def.Remark) != "" {
		remark = strings.TrimSpace(def.Remark)
		if err := smlprofile.ValidateFreeText("remark", remark); err != nil {
			return resolvedInvoiceDocumentProfile{}, err
		}
	} else if bill != nil {
		remark = strings.TrimSpace(bill.Remark)
		if err := smlprofile.ValidateFreeText("remark", remark); err != nil {
			return resolvedInvoiceDocumentProfile{}, err
		}
	}

	remark2 := strings.TrimSpace(req.Remark2)
	if remark2 == "" && def != nil {
		remark2 = strings.TrimSpace(def.Remark2)
	}
	if err := smlprofile.ValidateFreeText("remark_2", remark2); err != nil {
		return resolvedInvoiceDocumentProfile{}, err
	}

	identity := firstNonEmpty(orderRef, docNo)
	profile := resolvedInvoiceDocumentProfile{
		Mode: mode, Remark: remark, Remark2: remark2,
		Options: sml.InvoiceDocumentProfileOptions{
			Mode: mode, Channel: channel, Remark5: "NEXFLOW|" + channel + "|" + identity,
		},
	}
	if def != nil {
		profile.Options.ConfigVersion = def.ConfigVersion
		profile.Options.RouteSignature = smlprofile.RouteSignature(*def, mode)
	}
	if mode == smlprofile.ModeOff {
		return profile, nil
	}
	profile.Options.MarketplacePhysicalGoods = bill != nil && bill.BillType == "sale" && isMarketplaceSource(bill.Source)
	if !profile.Options.MarketplacePhysicalGoods {
		profile.Options.ShipmentApplicability = "not_applicable"
		return profile, nil
	}
	// Nexflow is stock-focused. Marketplace recipient PII is not required for
	// the SML stock document and must not block or widen the outbound payload.
	profile.Options.ShipmentApplicability = "not_applicable"
	return profile, nil
}
