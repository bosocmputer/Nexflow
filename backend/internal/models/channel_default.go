package models

import "time"

// ChannelDefault is the per-(channel, bill_type) party setting that decides
// which SML customer (sale) or supplier (purchase) bills route to.
//
// PartyCode is sent as cust_code in saleorder/saleinvoice/purchaseorder.
// PartyName overrides the AI-extracted contact_name so SML doesn't
// create a fresh AR row for every session.
type ChannelDefault struct {
	Channel              string `json:"channel"`
	BillType             string `json:"bill_type"`
	PartyCode            string `json:"party_code"`
	PartyName            string `json:"party_name"`
	PartyPhone           string `json:"party_phone"`
	PartyAddress         string `json:"party_address"`
	PartyTaxID           string `json:"party_tax_id"`
	DocFormatCode        string `json:"doc_format_code"`
	Endpoint             string `json:"endpoint"`
	DocPrefix            string `json:"doc_prefix"`
	DocRunningFormat     string `json:"doc_running_format"`
	BranchCode           string `json:"branch_code"`
	SaleCode             string `json:"sale_code"`
	UnitCode             string `json:"unit_code"`
	DocTime              string `json:"doc_time"`
	ShippingItemEnabled  bool   `json:"shipping_item_enabled"`
	ShippingItemCode     string `json:"shipping_item_code"`
	ShippingItemUnitCode string `json:"shipping_item_unit_code"`
	PassbookCode         string `json:"passbook_code"`
	PassbookName         string `json:"passbook_name"`
	BankCode             string `json:"bank_code"`
	BankBranch           string `json:"bank_branch"`
	ExpenseCode          string `json:"expense_code"`
	ExpenseName          string `json:"expense_name"`
	// Inventory + VAT overrides (sentinel: empty / -1 = "use server default")
	WHCode      string    `json:"wh_code"`
	ShelfCode   string    `json:"shelf_code"`
	VATType     int       `json:"vat_type"`
	VATRate     float64   `json:"vat_rate"`
	InquiryType int       `json:"inquiry_type"` // -1 = ยังไม่ได้ตั้ง (กรอกตอนส่ง)
	Remark2     string    `json:"remark_2"`     // sentinel: '' = ไม่ระบุ
	UpdatedBy   *string   `json:"updated_by,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ChannelDefaultUpsert is the admin-supplied payload for PUT.
// PartyName/Phone/Address/TaxID come from the SML party master (snapshot at
// save time) so the table can render code+name without a second SML lookup.
// Endpoint blank = auto-resolve by (channel, bill_type) in bills.go.
type ChannelDefaultUpsert struct {
	Channel              string `json:"channel" binding:"required,oneof=line email shopee shopee_realtime shopee_email shopee_shipped lazada tiktok manual shopee_settlement"`
	BillType             string `json:"bill_type" binding:"required,oneof=sale purchase ar_receipt"`
	PartyCode            string `json:"party_code"`
	PartyName            string `json:"party_name"`
	PartyPhone           string `json:"party_phone"`
	PartyAddress         string `json:"party_address"`
	PartyTaxID           string `json:"party_tax_id"`
	DocFormatCode        string `json:"doc_format_code"`
	Endpoint             string `json:"endpoint"` // free-form URL or path; bills.go detects client by keyword
	DocPrefix            string `json:"doc_prefix"`
	DocRunningFormat     string `json:"doc_running_format"`
	BranchCode           string `json:"branch_code"`
	SaleCode             string `json:"sale_code"`
	UnitCode             string `json:"unit_code"`
	DocTime              string `json:"doc_time"`
	ShippingItemEnabled  bool   `json:"shipping_item_enabled"`
	ShippingItemCode     string `json:"shipping_item_code"`
	ShippingItemUnitCode string `json:"shipping_item_unit_code"`
	PassbookCode         string `json:"passbook_code"`
	PassbookName         string `json:"passbook_name"`
	BankCode             string `json:"bank_code"`
	BankBranch           string `json:"bank_branch"`
	ExpenseCode          string `json:"expense_code"`
	ExpenseName          string `json:"expense_name"`
	// Inventory + VAT overrides; empty / -1 = "use server default"
	WHCode      string  `json:"wh_code"`
	ShelfCode   string  `json:"shelf_code"`
	VATType     int     `json:"vat_type"`
	VATRate     float64 `json:"vat_rate"`
	InquiryType int     `json:"inquiry_type"` // -1 = ยังไม่ได้ตั้ง
	Remark2     string  `json:"remark_2"`     // sentinel: '' = ไม่ระบุ
}
