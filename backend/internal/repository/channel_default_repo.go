package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"nexflow/internal/models"
)

type ChannelDefaultRepo struct {
	db *sql.DB
}

var ErrConfigVersionConflict = errors.New("channel default config version conflict")

type ShopeeSMLRouteBundleUpdate struct {
	Main, Cancellation                         *models.ChannelDefault
	ExpectedMainVersion, ExpectedCancelVersion int64
	UpdatedBy                                  string
	TraceID                                    string
	AuditDetail                                map[string]interface{}
}

type ShopeeSMLRouteBundleUpdateResult struct {
	Main, Cancellation *models.ChannelDefault
	PausedShops        int64
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
  wh_code, shelf_code, vat_type, vat_rate, inquiry_type, remark, remark_2,
  config_version,
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
		&d.WHCode, &d.ShelfCode, &d.VATType, &d.VATRate, &d.InquiryType, &d.Remark, &d.Remark2,
		&d.ConfigVersion,
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

// UpdateShopeeSMLRouteBundle persists the Shopee main and cancellation routes,
// their redacted audit evidence, and the automation pause as one transaction.
// A caller can therefore never observe a mixed route pair after a partial
// settings write.
func (r *ChannelDefaultRepo) UpdateShopeeSMLRouteBundle(ctx context.Context, in ShopeeSMLRouteBundleUpdate) (*ShopeeSMLRouteBundleUpdateResult, error) {
	if r == nil || r.db == nil || in.Main == nil || in.Cancellation == nil ||
		in.ExpectedMainVersion < 0 || in.ExpectedCancelVersion < 0 {
		return nil, ErrConfigVersionConflict
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin Shopee SML route bundle: %w", err)
	}
	defer tx.Rollback()

	main, err := upsertChannelDefaultExpectedTx(ctx, tx, in.Main, in.UpdatedBy, in.ExpectedMainVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConfigVersionConflict
	}
	if err != nil {
		return nil, fmt.Errorf("write Shopee SML main route: %w", err)
	}
	cancelRoute, err := upsertChannelDefaultExpectedTx(ctx, tx, in.Cancellation, in.UpdatedBy, in.ExpectedCancelVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConfigVersionConflict
	}
	if err != nil {
		return nil, fmt.Errorf("write Shopee SML cancellation route: %w", err)
	}

	pauseResult, err := tx.ExecContext(ctx, `UPDATE shopee_auto_sml_settings
		SET paused_reason='route_changed', paused_at=NOW(),
		    config_version=config_version+1, updated_at=NOW()
		WHERE enabled=true`)
	if err != nil {
		return nil, fmt.Errorf("pause Auto SML after route bundle change: %w", err)
	}
	paused, _ := pauseResult.RowsAffected()

	auditJSON, err := json.Marshal(in.AuditDetail)
	if err != nil {
		return nil, fmt.Errorf("marshal Shopee SML route bundle audit: %w", err)
	}
	var userID, traceID sql.NullString
	if in.UpdatedBy != "" {
		userID = sql.NullString{String: in.UpdatedBy, Valid: true}
	}
	if in.TraceID != "" {
		traceID = sql.NullString{String: in.TraceID, Valid: true}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_logs
		(action,user_id,source,level,trace_id,detail)
		VALUES ('shopee_sml_route_bundle_updated',$1,'channel_defaults','info',$2,$3)`,
		userID, traceID, auditJSON); err != nil {
		return nil, fmt.Errorf("write Shopee SML route bundle audit: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit Shopee SML route bundle: %w", err)
	}
	return &ShopeeSMLRouteBundleUpdateResult{Main: main, Cancellation: cancelRoute, PausedShops: paused}, nil
}

func upsertChannelDefaultExpectedTx(ctx context.Context, tx *sql.Tx, d *models.ChannelDefault, updatedBy string, expectedVersion int64) (*models.ChannelDefault, error) {
	var ub sql.NullString
	if updatedBy != "" {
		ub = sql.NullString{String: updatedBy, Valid: true}
	}
	row := tx.QueryRowContext(ctx, `INSERT INTO channel_defaults (
		channel,bill_type,party_code,party_name,party_phone,party_address,party_tax_id,
		doc_format_code,endpoint,doc_prefix,doc_running_format,branch_code,sale_code,unit_code,doc_time,
		shipping_item_enabled,shipping_item_code,shipping_item_unit_code,
		passbook_code,passbook_name,bank_code,bank_branch,expense_code,expense_name,
		wh_code,shelf_code,vat_type,vat_rate,inquiry_type,remark,remark_2,config_version,updated_by,updated_at)
	SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,
	       $25,$26,$27,$28,$29,$30,$31,1,$32,NOW()
	WHERE $33::bigint=0 OR EXISTS (
		SELECT 1 FROM channel_defaults WHERE channel=$1 AND bill_type=$2 AND config_version=$33
	)
	ON CONFLICT (channel,bill_type) DO UPDATE SET
		party_code=EXCLUDED.party_code,party_name=EXCLUDED.party_name,party_phone=EXCLUDED.party_phone,
		party_address=EXCLUDED.party_address,party_tax_id=EXCLUDED.party_tax_id,
		doc_format_code=EXCLUDED.doc_format_code,endpoint=EXCLUDED.endpoint,
		doc_prefix=EXCLUDED.doc_prefix,doc_running_format=EXCLUDED.doc_running_format,
		branch_code=EXCLUDED.branch_code,sale_code=EXCLUDED.sale_code,unit_code=EXCLUDED.unit_code,doc_time=EXCLUDED.doc_time,
		shipping_item_enabled=EXCLUDED.shipping_item_enabled,shipping_item_code=EXCLUDED.shipping_item_code,
		shipping_item_unit_code=EXCLUDED.shipping_item_unit_code,passbook_code=EXCLUDED.passbook_code,
		passbook_name=EXCLUDED.passbook_name,bank_code=EXCLUDED.bank_code,bank_branch=EXCLUDED.bank_branch,
		expense_code=EXCLUDED.expense_code,expense_name=EXCLUDED.expense_name,
		wh_code=EXCLUDED.wh_code,shelf_code=EXCLUDED.shelf_code,vat_type=EXCLUDED.vat_type,
		vat_rate=EXCLUDED.vat_rate,inquiry_type=EXCLUDED.inquiry_type,remark=EXCLUDED.remark,remark_2=EXCLUDED.remark_2,
		config_version=channel_defaults.config_version+1,updated_by=EXCLUDED.updated_by,updated_at=NOW()
	WHERE channel_defaults.config_version=$33
	RETURNING `+channelDefaultCols,
		d.Channel, d.BillType, d.PartyCode, d.PartyName, d.PartyPhone, d.PartyAddress, d.PartyTaxID,
		d.DocFormatCode, d.Endpoint, d.DocPrefix, d.DocRunningFormat, d.BranchCode, d.SaleCode, d.UnitCode, d.DocTime,
		d.ShippingItemEnabled, d.ShippingItemCode, d.ShippingItemUnitCode,
		d.PassbookCode, d.PassbookName, d.BankCode, d.BankBranch, d.ExpenseCode, d.ExpenseName,
		d.WHCode, d.ShelfCode, d.VATType, d.VATRate, d.InquiryType, d.Remark, d.Remark2, ub, expectedVersion,
	)
	return scanChannelDefault(row)
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
		   wh_code, shelf_code, vat_type, vat_rate, inquiry_type, remark, remark_2,
		   updated_by, updated_at
		 ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32, NOW())
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
		   remark = EXCLUDED.remark,
		   remark_2 = EXCLUDED.remark_2,
		   config_version = channel_defaults.config_version + 1,
		   updated_by = EXCLUDED.updated_by,
		   updated_at = NOW()`,
		d.Channel, d.BillType, d.PartyCode, d.PartyName, d.PartyPhone,
		d.PartyAddress, d.PartyTaxID, d.DocFormatCode, d.Endpoint,
		d.DocPrefix, d.DocRunningFormat,
		d.BranchCode, d.SaleCode, d.UnitCode, d.DocTime,
		d.ShippingItemEnabled, d.ShippingItemCode, d.ShippingItemUnitCode,
		d.PassbookCode, d.PassbookName, d.BankCode, d.BankBranch, d.ExpenseCode, d.ExpenseName,
		d.WHCode, d.ShelfCode, d.VATType, d.VATRate, d.InquiryType, d.Remark, d.Remark2,
		ub,
	)
	if err != nil {
		return fmt.Errorf("Upsert channel_default: %w", err)
	}
	return nil
}

// UpdateExpected updates an existing row only when the caller's version is
// current. This is the public settings write path; system seed callers keep
// using Upsert before the first operator edit.
func (r *ChannelDefaultRepo) UpdateExpected(d *models.ChannelDefault, updatedBy string, expectedVersion int64, pauseAutoSML bool) (*models.ChannelDefault, error) {
	if expectedVersion <= 0 {
		return nil, ErrConfigVersionConflict
	}
	var ub sql.NullString
	if updatedBy != "" {
		ub = sql.NullString{String: updatedBy, Valid: true}
	}
	row := r.db.QueryRow(
		`WITH updated AS (
		 UPDATE channel_defaults SET
		   party_code=$1, party_name=$2, party_phone=$3, party_address=$4, party_tax_id=$5,
		   doc_format_code=$6, endpoint=$7, doc_prefix=$8, doc_running_format=$9,
		   branch_code=$10, sale_code=$11, unit_code=$12, doc_time=$13,
		   shipping_item_enabled=$14, shipping_item_code=$15, shipping_item_unit_code=$16,
		   passbook_code=$17, passbook_name=$18, bank_code=$19, bank_branch=$20,
		   expense_code=$21, expense_name=$22, wh_code=$23, shelf_code=$24,
		   vat_type=$25, vat_rate=$26, inquiry_type=$27, remark=$28, remark_2=$29,
		   updated_by=$30, updated_at=NOW(), config_version=config_version+1
		 WHERE channel=$31 AND bill_type=$32 AND config_version=$33
		 RETURNING `+channelDefaultCols+`
		), paused AS (
		 UPDATE shopee_auto_sml_settings
		    SET paused_reason='route_changed', paused_at=NOW(),
		        config_version=config_version+1, updated_at=NOW()
		  WHERE $34 AND enabled=true AND EXISTS (SELECT 1 FROM updated)
		 RETURNING shop_id
		)
		SELECT `+channelDefaultCols+` FROM updated`,
		d.PartyCode, d.PartyName, d.PartyPhone, d.PartyAddress, d.PartyTaxID,
		d.DocFormatCode, d.Endpoint, d.DocPrefix, d.DocRunningFormat,
		d.BranchCode, d.SaleCode, d.UnitCode, d.DocTime,
		d.ShippingItemEnabled, d.ShippingItemCode, d.ShippingItemUnitCode,
		d.PassbookCode, d.PassbookName, d.BankCode, d.BankBranch,
		d.ExpenseCode, d.ExpenseName, d.WHCode, d.ShelfCode,
		d.VATType, d.VATRate, d.InquiryType, d.Remark, d.Remark2,
		ub, d.Channel, d.BillType, expectedVersion, pauseAutoSML,
	)
	updated, err := scanChannelDefault(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConfigVersionConflict
	}
	if err != nil {
		return nil, fmt.Errorf("UpdateExpected channel_default: %w", err)
	}
	return updated, nil
}

// CreateExpected creates a previously absent channel route at version 1.
// ON CONFLICT converts a concurrent creator into the same optimistic conflict
// used by updates.
func (r *ChannelDefaultRepo) CreateExpected(d *models.ChannelDefault, updatedBy string, pauseAutoSML bool) (*models.ChannelDefault, error) {
	var ub sql.NullString
	if updatedBy != "" {
		ub = sql.NullString{String: updatedBy, Valid: true}
	}
	row := r.db.QueryRow(
		`WITH created AS (
		 INSERT INTO channel_defaults (
		   channel, bill_type, party_code, party_name, party_phone,
		   party_address, party_tax_id, doc_format_code, endpoint,
		   doc_prefix, doc_running_format, branch_code, sale_code, unit_code, doc_time,
		   shipping_item_enabled, shipping_item_code, shipping_item_unit_code,
		   passbook_code, passbook_name, bank_code, bank_branch, expense_code, expense_name,
		   wh_code, shelf_code, vat_type, vat_rate, inquiry_type, remark, remark_2,
		   config_version, updated_by, updated_at
		 ) VALUES (
		   $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,
		   $23,$24,$25,$26,$27,$28,$29,$30,$31,1,$32,NOW()
		 ) ON CONFLICT (channel, bill_type) DO NOTHING
		 RETURNING `+channelDefaultCols+`
		), paused AS (
		 UPDATE shopee_auto_sml_settings
		    SET paused_reason='route_changed', paused_at=NOW(),
		        config_version=config_version+1, updated_at=NOW()
		  WHERE $33 AND enabled=true AND EXISTS (SELECT 1 FROM created)
		 RETURNING shop_id
		)
		SELECT `+channelDefaultCols+` FROM created`,
		d.Channel, d.BillType, d.PartyCode, d.PartyName, d.PartyPhone,
		d.PartyAddress, d.PartyTaxID, d.DocFormatCode, d.Endpoint,
		d.DocPrefix, d.DocRunningFormat, d.BranchCode, d.SaleCode, d.UnitCode, d.DocTime,
		d.ShippingItemEnabled, d.ShippingItemCode, d.ShippingItemUnitCode,
		d.PassbookCode, d.PassbookName, d.BankCode, d.BankBranch, d.ExpenseCode, d.ExpenseName,
		d.WHCode, d.ShelfCode, d.VATType, d.VATRate, d.InquiryType, d.Remark, d.Remark2, ub, pauseAutoSML,
	)
	created, err := scanChannelDefault(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConfigVersionConflict
	}
	if err != nil {
		return nil, fmt.Errorf("CreateExpected channel_default: %w", err)
	}
	return created, nil
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
