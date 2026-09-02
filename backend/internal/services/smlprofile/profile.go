package smlprofile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"nexflow/internal/models"
)

const (
	Version      = "sml-document-v1"
	ModeOff      = "off"
	ModeShadow   = "shadow"
	ModeActive   = "active"
	MaxTextRunes = 255
)

var templateTokens = []struct {
	token string
	value func(TemplateContext) string
}{
	{"{{channel}}", func(v TemplateContext) string { return v.Channel }},
	{"{{order_ref}}", func(v TemplateContext) string { return v.OrderRef }},
	{"{{bill_no}}", func(v TemplateContext) string { return v.BillNo }},
}

type TemplateContext struct {
	Channel  string
	OrderRef string
	BillNo   string
}

func ParseMode(raw string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(raw))
	switch mode {
	case ModeOff, ModeShadow, ModeActive:
		return mode, nil
	default:
		return "", fmt.Errorf("SML_DOCUMENT_PROFILE_MODE must be off, shadow, or active; got %q", raw)
	}
}

func ValidateTextTemplate(field, value string) error {
	if err := validateBoundedText(field, value); err != nil {
		return err
	}
	remainder := value
	for _, item := range templateTokens {
		remainder = strings.ReplaceAll(remainder, item.token, "")
	}
	if strings.Contains(remainder, "{{") || strings.Contains(remainder, "}}") {
		return fmt.Errorf("%s contains an unknown or malformed template token", field)
	}
	return nil
}

func ResolveTemplate(template string, context TemplateContext) (string, error) {
	if err := ValidateTextTemplate("remark", template); err != nil {
		return "", err
	}
	resolved := template
	for _, item := range templateTokens {
		resolved = strings.ReplaceAll(resolved, item.token, item.value(context))
	}
	if err := validateBoundedText("resolved remark", resolved); err != nil {
		return "", err
	}
	return resolved, nil
}

func ValidateFreeText(field, value string) error {
	return validateBoundedText(field, value)
}

func validateBoundedText(field, value string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s must be valid UTF-8", field)
	}
	if utf8.RuneCountInString(value) > MaxTextRunes {
		return fmt.Errorf("%s must not exceed %d Unicode characters", field, MaxTextRunes)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("%s must not contain control characters", field)
		}
	}
	return nil
}

// RouteSignature hashes only bounded routing/configuration authority. It never
// includes buyer data, order numbers, document numbers, or secrets.
func RouteSignature(d models.ChannelDefault, mode string) string {
	type authority struct {
		Version              string  `json:"profile_version"`
		Mode                 string  `json:"profile_mode"`
		ConfigVersion        int64   `json:"config_version"`
		Channel              string  `json:"channel"`
		BillType             string  `json:"bill_type"`
		Endpoint             string  `json:"endpoint"`
		DocFormatCode        string  `json:"doc_format_code"`
		DocPrefix            string  `json:"doc_prefix"`
		DocRunningFormat     string  `json:"doc_running_format"`
		PartyCode            string  `json:"party_code"`
		BranchCode           string  `json:"branch_code"`
		SaleCode             string  `json:"sale_code"`
		WHCode               string  `json:"wh_code"`
		ShelfCode            string  `json:"shelf_code"`
		VATType              int     `json:"vat_type"`
		VATRate              float64 `json:"vat_rate"`
		InquiryType          int     `json:"inquiry_type"`
		ShippingItemEnabled  bool    `json:"shipping_item_enabled"`
		ShippingItemCode     string  `json:"shipping_item_code"`
		ShippingItemUnitCode string  `json:"shipping_item_unit_code"`
		Remark               string  `json:"remark"`
		Remark2              string  `json:"remark_2"`
	}
	body, _ := json.Marshal(authority{
		Version: Version, Mode: mode, ConfigVersion: d.ConfigVersion,
		Channel: d.Channel, BillType: d.BillType, Endpoint: d.Endpoint,
		DocFormatCode: d.DocFormatCode, DocPrefix: d.DocPrefix, DocRunningFormat: d.DocRunningFormat,
		PartyCode: d.PartyCode, BranchCode: d.BranchCode, SaleCode: d.SaleCode,
		WHCode: d.WHCode, ShelfCode: d.ShelfCode, VATType: d.VATType, VATRate: d.VATRate,
		InquiryType: d.InquiryType, ShippingItemEnabled: d.ShippingItemEnabled,
		ShippingItemCode: d.ShippingItemCode, ShippingItemUnitCode: d.ShippingItemUnitCode,
		Remark: d.Remark, Remark2: d.Remark2,
	})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
