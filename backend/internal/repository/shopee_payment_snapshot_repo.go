package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"nexflow/internal/models"
	"nexflow/internal/services/shopeeapi"
)

const shopeePaymentSnapshotCols = `
  id::text, shop_id, order_sn, status,
  buyer_total_amount::float8, escrow_amount::float8, original_price::float8,
  seller_discount::float8, shopee_discount::float8, commission_fee::float8,
  service_fee::float8, seller_transaction_fee::float8, final_shipping_fee::float8,
  actual_shipping_fee::float8, escrow_tax::float8, withholding_tax::float8,
  voucher_from_seller::float8, voucher_from_shopee::float8, reverse_shipping_fee::float8,
  buyer_paid_shipping_fee::float8, shopee_shipping_rebate::float8,
  seller_shipping_discount::float8, coin::float8, raw_escrow,
  attempts, next_run_at, last_error, last_request_id, last_synced_at,
  created_at, updated_at
`

const shopeePaymentSnapshotColsS = `
  s.id::text, s.shop_id, s.order_sn, s.status,
  s.buyer_total_amount::float8, s.escrow_amount::float8, s.original_price::float8,
  s.seller_discount::float8, s.shopee_discount::float8, s.commission_fee::float8,
  s.service_fee::float8, s.seller_transaction_fee::float8, s.final_shipping_fee::float8,
  s.actual_shipping_fee::float8, s.escrow_tax::float8, s.withholding_tax::float8,
  s.voucher_from_seller::float8, s.voucher_from_shopee::float8, s.reverse_shipping_fee::float8,
  s.buyer_paid_shipping_fee::float8, s.shopee_shipping_rebate::float8,
  s.seller_shipping_discount::float8, s.coin::float8, s.raw_escrow,
  s.attempts, s.next_run_at, s.last_error, s.last_request_id, s.last_synced_at,
  s.created_at, s.updated_at
`

func (r *ShopeeRealtimeRepo) QueuePaymentBreakdown(ctx context.Context, shopID int64, orderSN string) error {
	orderSN = strings.TrimSpace(orderSN)
	if r == nil || r.db == nil || shopID <= 0 || orderSN == "" {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO shopee_order_payment_snapshots (shop_id, order_sn, status, next_run_at)
		VALUES ($1, $2, 'queued', NOW())
		ON CONFLICT (shop_id, order_sn) DO UPDATE
		   SET status = CASE
		                  WHEN shopee_order_payment_snapshots.status = 'ready' THEN shopee_order_payment_snapshots.status
		                  ELSE 'queued'
		                END,
		       next_run_at = CASE
		                       WHEN shopee_order_payment_snapshots.status = 'ready' THEN shopee_order_payment_snapshots.next_run_at
		                       ELSE NOW()
		                     END,
		       locked_at = NULL,
		       attempts = CASE
		                    WHEN shopee_order_payment_snapshots.status = 'ready' THEN shopee_order_payment_snapshots.attempts
		                    ELSE 0
		                  END,
		       last_error = CASE
		                      WHEN shopee_order_payment_snapshots.status = 'ready' THEN shopee_order_payment_snapshots.last_error
		                      ELSE ''
		                    END,
		       updated_at = NOW()`,
		shopID, orderSN,
	)
	if err != nil {
		return fmt.Errorf("queue shopee payment breakdown: %w", err)
	}
	return nil
}

func (r *ShopeeRealtimeRepo) LeasePaymentBreakdownJobs(ctx context.Context, limit, maxAttempts int) ([]models.ShopeeOrderPaymentSnapshot, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	rows, err := r.db.QueryContext(ctx, `
		WITH picked AS (
			SELECT id
			  FROM shopee_order_payment_snapshots
			 WHERE status IN ('queued','failed')
			   AND attempts < $1
			   AND next_run_at <= NOW()
			 ORDER BY next_run_at ASC, created_at ASC
			 LIMIT $2
			 FOR UPDATE SKIP LOCKED
		)
		UPDATE shopee_order_payment_snapshots s
		   SET status = 'running',
		       attempts = s.attempts + 1,
		       locked_at = NOW(),
		       updated_at = NOW()
		  FROM picked p
		 WHERE s.id = p.id
		 RETURNING `+shopeePaymentSnapshotColsS,
		maxAttempts, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("lease shopee payment breakdown jobs: %w", err)
	}
	defer rows.Close()
	out := []models.ShopeeOrderPaymentSnapshot{}
	for rows.Next() {
		row, err := scanShopeePaymentSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *ShopeeRealtimeRepo) FindPaymentBreakdown(ctx context.Context, shopID int64, orderSN string) (*models.ShopeeOrderPaymentSnapshot, error) {
	orderSN = strings.TrimSpace(orderSN)
	if r == nil || r.db == nil || shopID <= 0 || orderSN == "" {
		return nil, nil
	}
	row := r.db.QueryRowContext(ctx,
		`SELECT `+shopeePaymentSnapshotCols+`
		   FROM shopee_order_payment_snapshots
		  WHERE shop_id = $1 AND order_sn = $2
		  LIMIT 1`,
		shopID, orderSN,
	)
	out, err := scanShopeePaymentSnapshot(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find shopee payment breakdown: %w", err)
	}
	return &out, nil
}

func (r *ShopeeRealtimeRepo) MarkPaymentBreakdownReady(ctx context.Context, shopID int64, orderSN string, income shopeeapi.EscrowOrderIncome, raw json.RawMessage, requestID string) (*models.ShopeeOrderPaymentSnapshot, error) {
	orderSN = strings.TrimSpace(orderSN)
	if r == nil || r.db == nil || shopID <= 0 || orderSN == "" {
		return nil, fmt.Errorf("shop_id/order_sn are required")
	}
	rawText := "{}"
	if len(raw) > 0 && strings.TrimSpace(string(raw)) != "" {
		rawText = string(raw)
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO shopee_order_payment_snapshots
		  (shop_id, order_sn, status, buyer_total_amount, escrow_amount, original_price,
		   seller_discount, shopee_discount, commission_fee, service_fee, seller_transaction_fee,
		   final_shipping_fee, actual_shipping_fee, escrow_tax, withholding_tax,
		   voucher_from_seller, voucher_from_shopee, reverse_shipping_fee,
		   buyer_paid_shipping_fee, shopee_shipping_rebate, seller_shipping_discount, coin,
		   raw_escrow, locked_at, last_error, last_request_id, last_synced_at, updated_at)
		VALUES
		  ($1, $2, 'ready', $3, $4, $5, $6, $7, $8, $9, $10,
		   $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21,
		   COALESCE(NULLIF($22, '')::jsonb, '{}'::jsonb), NULL, '', $23, NOW(), NOW())
		ON CONFLICT (shop_id, order_sn) DO UPDATE
		   SET status = 'ready',
		       buyer_total_amount = EXCLUDED.buyer_total_amount,
		       escrow_amount = EXCLUDED.escrow_amount,
		       original_price = EXCLUDED.original_price,
		       seller_discount = EXCLUDED.seller_discount,
		       shopee_discount = EXCLUDED.shopee_discount,
		       commission_fee = EXCLUDED.commission_fee,
		       service_fee = EXCLUDED.service_fee,
		       seller_transaction_fee = EXCLUDED.seller_transaction_fee,
		       final_shipping_fee = EXCLUDED.final_shipping_fee,
		       actual_shipping_fee = EXCLUDED.actual_shipping_fee,
		       escrow_tax = EXCLUDED.escrow_tax,
		       withholding_tax = EXCLUDED.withholding_tax,
		       voucher_from_seller = EXCLUDED.voucher_from_seller,
		       voucher_from_shopee = EXCLUDED.voucher_from_shopee,
		       reverse_shipping_fee = EXCLUDED.reverse_shipping_fee,
		       buyer_paid_shipping_fee = EXCLUDED.buyer_paid_shipping_fee,
		       shopee_shipping_rebate = EXCLUDED.shopee_shipping_rebate,
		       seller_shipping_discount = EXCLUDED.seller_shipping_discount,
		       coin = EXCLUDED.coin,
		       raw_escrow = EXCLUDED.raw_escrow,
		       locked_at = NULL,
		       last_error = '',
		       last_request_id = EXCLUDED.last_request_id,
		       last_synced_at = NOW(),
		       updated_at = NOW()
		RETURNING `+shopeePaymentSnapshotCols,
		shopID, orderSN, income.BuyerTotalAmount, income.EscrowAmount, income.OriginalPrice,
		income.SellerDiscount, income.ShopeeDiscount, income.CommissionFee, income.ServiceFee,
		income.SellerTransactionFee, income.FinalShippingFee, income.ActualShippingFee,
		income.EscrowTax, income.WithholdingTax, income.VoucherFromSeller, income.VoucherFromShopee,
		income.ReverseShippingFee, income.BuyerPaidShippingFee, income.ShopeeShippingRebate,
		income.SellerShippingDiscount, income.Coin, rawText, strings.TrimSpace(requestID),
	)
	out, err := scanShopeePaymentSnapshot(row)
	if err != nil {
		return nil, fmt.Errorf("mark shopee payment breakdown ready: %w", err)
	}
	return &out, nil
}

func (r *ShopeeRealtimeRepo) MarkPaymentBreakdownUnavailable(ctx context.Context, shopID int64, orderSN, errMsg, requestID string) (*models.ShopeeOrderPaymentSnapshot, error) {
	return r.markPaymentBreakdownTerminal(ctx, shopID, orderSN, "unavailable", errMsg, requestID)
}

func (r *ShopeeRealtimeRepo) MarkPaymentBreakdownFailed(ctx context.Context, shopID int64, orderSN, errMsg string, nextRunAt time.Time) error {
	orderSN = strings.TrimSpace(orderSN)
	if r == nil || r.db == nil || shopID <= 0 || orderSN == "" {
		return nil
	}
	if nextRunAt.IsZero() {
		nextRunAt = time.Now().Add(30 * time.Minute)
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO shopee_order_payment_snapshots
		  (shop_id, order_sn, status, attempts, locked_at, last_error, next_run_at, updated_at)
		VALUES ($1, $2, 'failed', 1, NULL, $3, $4, NOW())
		ON CONFLICT (shop_id, order_sn) DO UPDATE
		   SET status = 'failed',
		       locked_at = NULL,
		       last_error = EXCLUDED.last_error,
		       next_run_at = EXCLUDED.next_run_at,
		       updated_at = NOW()`,
		shopID, orderSN, truncateShopeePaymentError(errMsg), nextRunAt,
	)
	return err
}

func (r *ShopeeRealtimeRepo) RecoverStalePaymentBreakdownJobs(ctx context.Context, olderThan time.Duration) (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	if olderThan <= 0 {
		olderThan = 2 * time.Minute
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE shopee_order_payment_snapshots
		   SET status = 'failed',
		       locked_at = NULL,
		       last_error = 'payment breakdown worker recovered stale running job',
		       next_run_at = NOW(),
		       updated_at = NOW()
		 WHERE status = 'running'
		   AND locked_at < NOW() - ($1 * INTERVAL '1 second')`,
		int(olderThan.Seconds()),
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *ShopeeRealtimeRepo) markPaymentBreakdownTerminal(ctx context.Context, shopID int64, orderSN, status, errMsg, requestID string) (*models.ShopeeOrderPaymentSnapshot, error) {
	orderSN = strings.TrimSpace(orderSN)
	if r == nil || r.db == nil || shopID <= 0 || orderSN == "" {
		return nil, fmt.Errorf("shop_id/order_sn are required")
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO shopee_order_payment_snapshots
		  (shop_id, order_sn, status, locked_at, last_error, last_request_id, last_synced_at, updated_at)
		VALUES ($1, $2, $3, NULL, $4, $5, NOW(), NOW())
		ON CONFLICT (shop_id, order_sn) DO UPDATE
		   SET status = EXCLUDED.status,
		       locked_at = NULL,
		       last_error = EXCLUDED.last_error,
		       last_request_id = EXCLUDED.last_request_id,
		       last_synced_at = NOW(),
		       updated_at = NOW()
		RETURNING `+shopeePaymentSnapshotCols,
		shopID, orderSN, status, truncateShopeePaymentError(errMsg), strings.TrimSpace(requestID),
	)
	out, err := scanShopeePaymentSnapshot(row)
	if err != nil {
		return nil, fmt.Errorf("mark shopee payment breakdown %s: %w", status, err)
	}
	return &out, nil
}

type shopeePaymentScanner interface {
	Scan(...any) error
}

func scanShopeePaymentSnapshot(s shopeePaymentScanner) (models.ShopeeOrderPaymentSnapshot, error) {
	var out models.ShopeeOrderPaymentSnapshot
	var raw []byte
	var nextRunAt, lastSyncedAt sql.NullTime
	if err := s.Scan(
		&out.ID, &out.ShopID, &out.OrderSN, &out.Status,
		&out.BuyerTotalAmount, &out.EscrowAmount, &out.OriginalPrice,
		&out.SellerDiscount, &out.ShopeeDiscount, &out.CommissionFee,
		&out.ServiceFee, &out.SellerTransactionFee, &out.FinalShippingFee,
		&out.ActualShippingFee, &out.EscrowTax, &out.WithholdingTax,
		&out.VoucherFromSeller, &out.VoucherFromShopee, &out.ReverseShippingFee,
		&out.BuyerPaidShippingFee, &out.ShopeeShippingRebate,
		&out.SellerShippingDiscount, &out.Coin, &raw,
		&out.Attempts, &nextRunAt, &out.LastError, &out.LastRequestID, &lastSyncedAt,
		&out.CreatedAt, &out.UpdatedAt,
	); err != nil {
		return out, err
	}
	out.RawEscrow = json.RawMessage(raw)
	out.DeductionAmount = roundShopeePaymentMoney(out.BuyerTotalAmount - out.EscrowAmount)
	if nextRunAt.Valid {
		out.NextRunAt = &nextRunAt.Time
	}
	if lastSyncedAt.Valid {
		out.LastSyncedAt = &lastSyncedAt.Time
	}
	return out, nil
}

func truncateShopeePaymentError(v string) string {
	v = strings.TrimSpace(v)
	if len(v) > 800 {
		return v[:800]
	}
	return v
}

func roundShopeePaymentMoney(v float64) float64 {
	return math.Round(v*100) / 100
}
