package handlers

import (
	"context"
	"encoding/json"
	"fmt"
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
	mode := smlprofile.ModeOff
	if h != nil && h.cfg != nil {
		mode = h.cfg.SMLDocumentProfileMode
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
	profile.Options.ShipmentApplicability = "required"
	shipment, err := h.resolveMarketplaceInvoiceShipment(ctx, bill)
	profile.Options.Shipment = shipment
	if err != nil {
		return profile, err
	}
	return profile, nil
}

func (h *BillHandler) resolveMarketplaceInvoiceShipment(ctx context.Context, bill *models.Bill) (*sml.InvoiceShipment, error) {
	if bill == nil {
		return nil, fmt.Errorf("shipment source is missing")
	}
	if shipment := invoiceShipmentFromJSON(bill.RawData); shipment != nil {
		return shipment, nil
	}
	if shopID, orderSN, ok := shopeeRealtimeBillIdentity(bill); ok && h != nil && h.shopeeRealtimeRepo != nil {
		snapshot, err := h.shopeeRealtimeRepo.FindSnapshot(ctx, shopID, orderSN)
		if err != nil {
			return nil, fmt.Errorf("load shipment snapshot failed")
		}
		if shipment := invoiceShipmentFromJSON(snapshot.RawDetail); shipment != nil {
			return shipment, nil
		}
	}
	return nil, fmt.Errorf("shipment recipient name, address, and telephone are required")
}

func invoiceShipmentFromJSON(raw json.RawMessage) *sml.InvoiceShipment {
	if len(raw) == 0 {
		return nil
	}
	var source struct {
		RecipientAddress struct {
			Name        string `json:"name"`
			Phone       string `json:"phone"`
			FullAddress string `json:"full_address"`
		} `json:"recipient_address"`
		ShippingAddress struct {
			RecipientName string `json:"recipientName"`
			PhoneNumber   string `json:"phoneNumber"`
			Address       string `json:"address"`
		} `json:"shippingAddress"`
		TransportName      string `json:"transport_name"`
		TransportAddress   string `json:"transport_address"`
		TransportTelephone string `json:"transport_telephone"`
	}
	if err := json.Unmarshal(raw, &source); err != nil {
		return nil
	}
	shipment := &sml.InvoiceShipment{
		TransportName:      firstNonEmpty(source.TransportName, source.RecipientAddress.Name, source.ShippingAddress.RecipientName),
		TransportAddress:   firstNonEmpty(source.TransportAddress, source.RecipientAddress.FullAddress, source.ShippingAddress.Address),
		TransportTelephone: firstNonEmpty(source.TransportTelephone, source.RecipientAddress.Phone, source.ShippingAddress.PhoneNumber),
	}
	if strings.TrimSpace(shipment.TransportName) == "" || strings.TrimSpace(shipment.TransportAddress) == "" || strings.TrimSpace(shipment.TransportTelephone) == "" {
		return nil
	}
	return shipment
}
