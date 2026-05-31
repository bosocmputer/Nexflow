package repository

import (
	"database/sql"
	"fmt"

	"nexflow/internal/models"
)

type ChannelDefaultRepo struct {
	db *sql.DB
}

func NewChannelDefaultRepo(db *sql.DB) *ChannelDefaultRepo {
	return &ChannelDefaultRepo{db: db}
}

const channelDefaultCols = `
  channel, bill_type, party_code, party_name, party_phone,
  party_address, party_tax_id, doc_format_code, endpoint,
  doc_prefix, doc_running_format,
  branch_code, sale_code, unit_code, doc_time,
  shipping_item_enabled, shipping_item_code, shipping_item_unit_code,
  passbook_code, passbook_name, bank_code, bank_branch, expense_code, expense_name,
  wh_code, shelf_code, vat_type, vat_rate, inquiry_type, remark_2,
  updated_by, updated_at
`

func scanChannelDefault(s interface{ Scan(...any) error }) (*models.ChannelDefault, error) {
	d := &models.ChannelDefault{}
	var updatedBy sql.NullString
	err := s.Scan(
		&d.Channel, &d.BillType, &d.PartyCode, &d.PartyName, &d.PartyPhone,
		&d.PartyAddress, &d.PartyTaxID, &d.DocFormatCode, &d.Endpoint,
		&d.DocPrefix, &d.DocRunningFormat,
		&d.BranchCode, &d.SaleCode, &d.UnitCode, &d.DocTime,
		&d.ShippingItemEnabled, &d.ShippingItemCode, &d.ShippingItemUnitCode,
		&d.PassbookCode, &d.PassbookName, &d.BankCode, &d.BankBranch, &d.ExpenseCode, &d.ExpenseName,
		&d.WHCode, &d.ShelfCode, &d.VATType, &d.VATRate, &d.InquiryType, &d.Remark2,
		&updatedBy, &d.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if updatedBy.Valid {
		s := updatedBy.String
		d.UpdatedBy = &s
	}
	return d, nil
}

func (r *ChannelDefaultRepo) ListAll() ([]*models.ChannelDefault, error) {
	rows, err := r.db.Query(
		`SELECT ` + channelDefaultCols + ` FROM channel_defaults
		 ORDER BY channel, bill_type`)
	if err != nil {
		return nil, fmt.Errorf("ListAll channel_defaults: %w", err)
	}
	defer rows.Close()

	var out []*models.ChannelDefault
	for rows.Next() {
		d, err := scanChannelDefault(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *ChannelDefaultRepo) Get(channel, billType string) (*models.ChannelDefault, error) {
	row := r.db.QueryRow(
		`SELECT `+channelDefaultCols+` FROM channel_defaults
		 WHERE channel=$1 AND bill_type=$2`,
		channel, billType,
	)
	d, err := scanChannelDefault(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("Get channel_default: %w", err)
	}
	return d, nil
}

// Upsert inserts or updates by (channel, bill_type).
// updatedBy may be empty when the call comes from a system seed.
func (r *ChannelDefaultRepo) Upsert(d *models.ChannelDefault, updatedBy string) error {
	var ub sql.NullString
	if updatedBy != "" {
		ub = sql.NullString{String: updatedBy, Valid: true}
	}
	_, err := r.db.Exec(
		`INSERT INTO channel_defaults (
		   channel, bill_type, party_code, party_name, party_phone,
		   party_address, party_tax_id, doc_format_code, endpoint,
		   doc_prefix, doc_running_format,
		   branch_code, sale_code, unit_code, doc_time,
		   shipping_item_enabled, shipping_item_code, shipping_item_unit_code,
		   passbook_code, passbook_name, bank_code, bank_branch, expense_code, expense_name,
		   wh_code, shelf_code, vat_type, vat_rate, inquiry_type, remark_2,
		   updated_by, updated_at
		 ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31, NOW())
		 ON CONFLICT (channel, bill_type) DO UPDATE SET
		   party_code = EXCLUDED.party_code,
		   party_name = EXCLUDED.party_name,
		   party_phone = EXCLUDED.party_phone,
		   party_address = EXCLUDED.party_address,
		   party_tax_id = EXCLUDED.party_tax_id,
		   doc_format_code = EXCLUDED.doc_format_code,
		   endpoint = EXCLUDED.endpoint,
		   doc_prefix = EXCLUDED.doc_prefix,
		   doc_running_format = EXCLUDED.doc_running_format,
		   branch_code = EXCLUDED.branch_code,
		   sale_code = EXCLUDED.sale_code,
		   unit_code = EXCLUDED.unit_code,
		   doc_time = EXCLUDED.doc_time,
		   shipping_item_enabled = EXCLUDED.shipping_item_enabled,
		   shipping_item_code = EXCLUDED.shipping_item_code,
		   shipping_item_unit_code = EXCLUDED.shipping_item_unit_code,
		   passbook_code = EXCLUDED.passbook_code,
		   passbook_name = EXCLUDED.passbook_name,
		   bank_code = EXCLUDED.bank_code,
		   bank_branch = EXCLUDED.bank_branch,
		   expense_code = EXCLUDED.expense_code,
		   expense_name = EXCLUDED.expense_name,
		   wh_code = EXCLUDED.wh_code,
		   shelf_code = EXCLUDED.shelf_code,
		   vat_type = EXCLUDED.vat_type,
		   vat_rate = EXCLUDED.vat_rate,
		   inquiry_type = EXCLUDED.inquiry_type,
		   remark_2 = EXCLUDED.remark_2,
		   updated_by = EXCLUDED.updated_by,
		   updated_at = NOW()`,
		d.Channel, d.BillType, d.PartyCode, d.PartyName, d.PartyPhone,
		d.PartyAddress, d.PartyTaxID, d.DocFormatCode, d.Endpoint,
		d.DocPrefix, d.DocRunningFormat,
		d.BranchCode, d.SaleCode, d.UnitCode, d.DocTime,
		d.ShippingItemEnabled, d.ShippingItemCode, d.ShippingItemUnitCode,
		d.PassbookCode, d.PassbookName, d.BankCode, d.BankBranch, d.ExpenseCode, d.ExpenseName,
		d.WHCode, d.ShelfCode, d.VATType, d.VATRate, d.InquiryType, d.Remark2,
		ub,
	)
	if err != nil {
		return fmt.Errorf("Upsert channel_default: %w", err)
	}
	return nil
}

func (r *ChannelDefaultRepo) Delete(channel, billType string) error {
	_, err := r.db.Exec(
		`DELETE FROM channel_defaults WHERE channel=$1 AND bill_type=$2`,
		channel, billType,
	)
	return err
}

// IsEmpty reports whether the table has zero rows. Used by main.go to decide
// whether to run seedChannelDefaultsFromEnv on first boot.
func (r *ChannelDefaultRepo) IsEmpty() (bool, error) {
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM channel_defaults`).Scan(&n); err != nil {
		return false, err
	}
	return n == 0, nil
}
