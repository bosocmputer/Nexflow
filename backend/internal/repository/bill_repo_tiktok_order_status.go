package repository

import (
	"context"
	"database/sql"
	"strings"
)

// TikTokShopOrderStatus reads the latest reconciled, shop-scoped order state.
// It never calls TikTok while a Bill is being rendered or sent.
func (r *BillRepo) TikTokShopOrderStatus(ctx context.Context, shopID, orderID string) (string, error) {
	if r == nil || r.db == nil || strings.TrimSpace(shopID) == "" || strings.TrimSpace(orderID) == "" {
		return "", sql.ErrNoRows
	}
	var status string
	err := r.db.QueryRowContext(ctx, `SELECT order_status FROM tiktok_shop_order_snapshots WHERE shop_id=$1 AND order_id=$2`,
		shopID, orderID).Scan(&status)
	return strings.TrimSpace(status), err
}
