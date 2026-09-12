package tiktokshop

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

const maxTikTokWebhookAttempts = 8

var (
	ErrWebhookStoreNotConfigured = errors.New("TikTok Shop webhook store is not configured")
	ErrInvalidWebhookDelivery    = errors.New("invalid TikTok Shop webhook delivery")
	ErrNoWebhookJobDue           = errors.New("no TikTok Shop webhook job due")
)

type TikTokWebhookJob struct {
	ID             string
	GatewayEventID string
	NotificationID string
	ShopID         string
	OrderID        string
	OrderStatus    string
	Timestamp      time.Time
	OrderUpdateAt  time.Time
	Attempts       int
}

type TikTokWebhookStore struct {
	database *sql.DB
}

func NewTikTokWebhookStore(database *sql.DB) *TikTokWebhookStore {
	return &TikTokWebhookStore{database: database}
}

func (s *TikTokWebhookStore) Ingest(ctx context.Context, delivery GatewayWebhookDelivery) (bool, error) {
	prepared, err := prepareWebhookDelivery(delivery)
	if err != nil {
		return false, err
	}
	if s == nil || s.database == nil {
		return false, ErrWebhookStoreNotConfigured
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var connectionID string
	if err := tx.QueryRowContext(ctx,
		`SELECT gateway_connection_id::text FROM tiktok_shop_connections
		  WHERE shop_id = $1 AND disabled_at IS NULL FOR SHARE`, prepared.ShopID,
	).Scan(&connectionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrInvalidWebhookDelivery
		}
		return false, err
	}
	result, err := tx.ExecContext(ctx,
		`INSERT INTO tiktok_shop_webhook_events
		   (gateway_event_id, notification_id, gateway_connection_id, shop_id, order_id,
		    event_order_status, event_timestamp, order_update_at)
		 VALUES ($1::uuid, $2, $3::uuid, $4, $5, $6, $7, $8)
		 ON CONFLICT DO NOTHING`,
		prepared.GatewayEventID, prepared.NotificationID, connectionID, prepared.ShopID,
		prepared.OrderID, prepared.OrderStatus, prepared.Timestamp, prepared.OrderUpdateAt,
	)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return rows == 1, nil
}

func (s *TikTokWebhookStore) ClaimDue(ctx context.Context, now time.Time) (TikTokWebhookJob, error) {
	if s == nil || s.database == nil || now.IsZero() {
		return TikTokWebhookJob{}, ErrWebhookStoreNotConfigured
	}
	var job TikTokWebhookJob
	err := s.database.QueryRowContext(ctx,
		`WITH recovered AS (
		   UPDATE tiktok_shop_webhook_events
		      SET status = CASE WHEN attempts >= $2 THEN 'needs_review' ELSE 'retry' END,
		          next_run_at = $1, lease_until = NULL, updated_at = NOW(), last_error_code = 'lease_expired'
		    WHERE status = 'running' AND lease_until < $1
		 ), picked AS (
		   SELECT id FROM tiktok_shop_webhook_events
		    WHERE status IN ('pending','retry') AND next_run_at <= $1 AND attempts < $2
		    ORDER BY next_run_at, received_at FOR UPDATE SKIP LOCKED LIMIT 1
		 ), leased AS (
		   UPDATE tiktok_shop_webhook_events e
		      SET status = 'running', attempts = attempts + 1, started_at = COALESCE(started_at, $1),
		          lease_until = $1 + INTERVAL '2 minutes', updated_at = NOW()
		     FROM picked WHERE e.id = picked.id
		   RETURNING e.id::text, e.gateway_event_id::text, e.notification_id, e.shop_id, e.order_id,
		             e.event_order_status, e.event_timestamp, e.order_update_at, e.attempts
		 )
		 SELECT * FROM leased`, now.UTC(), maxTikTokWebhookAttempts,
	).Scan(&job.ID, &job.GatewayEventID, &job.NotificationID, &job.ShopID, &job.OrderID,
		&job.OrderStatus, &job.Timestamp, &job.OrderUpdateAt, &job.Attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return TikTokWebhookJob{}, ErrNoWebhookJobDue
	}
	return job, err
}

func (s *TikTokWebhookStore) MarkSucceeded(ctx context.Context, jobID string) error {
	if s == nil || s.database == nil {
		return ErrWebhookStoreNotConfigured
	}
	result, err := s.database.ExecContext(ctx,
		`UPDATE tiktok_shop_webhook_events
		    SET status = 'succeeded', lease_until = NULL, completed_at = NOW(), updated_at = NOW(),
		        last_error_code = '', last_error_message = ''
		  WHERE id = $1::uuid AND status = 'running'`, strings.TrimSpace(jobID))
	if err != nil {
		return err
	}
	return requireOneWebhookRow(result)
}

func (s *TikTokWebhookStore) MarkFailed(ctx context.Context, job TikTokWebhookJob, code, message string, nextRunAt time.Time) error {
	code = strings.TrimSpace(code)
	message = strings.TrimSpace(message)
	if s == nil || s.database == nil || strings.TrimSpace(job.ID) == "" || code == "" || len(code) > 100 || len(message) > 512 || nextRunAt.IsZero() {
		return ErrInvalidWebhookDelivery
	}
	status := "retry"
	var completedAt *time.Time
	if job.Attempts >= maxTikTokWebhookAttempts {
		status = "needs_review"
		value := time.Now().UTC()
		completedAt = &value
	}
	result, err := s.database.ExecContext(ctx,
		`UPDATE tiktok_shop_webhook_events
		    SET status = $2, next_run_at = $3, lease_until = NULL, completed_at = $4,
		        last_error_code = $5, last_error_message = $6, updated_at = NOW()
		  WHERE id = $1::uuid AND status = 'running'`,
		job.ID, status, nextRunAt.UTC(), completedAt, code, message)
	if err != nil {
		return err
	}
	return requireOneWebhookRow(result)
}

func prepareWebhookDelivery(input GatewayWebhookDelivery) (TikTokWebhookJob, error) {
	input.GatewayEventID = strings.TrimSpace(input.GatewayEventID)
	input.NotificationID = strings.TrimSpace(input.NotificationID)
	input.ShopID = strings.TrimSpace(input.ShopID)
	input.OrderID = strings.TrimSpace(input.OrderID)
	input.OrderStatus = strings.ToUpper(strings.TrimSpace(input.OrderStatus))
	timestamp, timestampErr := time.Parse(time.RFC3339, input.Timestamp)
	updateAt, updateErr := time.Parse(time.RFC3339, input.OrderUpdateAt)
	if !connectionIDPattern.MatchString(input.GatewayEventID) || !tikTokNumericIDPattern.MatchString(input.NotificationID) ||
		!tikTokNumericIDPattern.MatchString(input.ShopID) || !tikTokNumericIDPattern.MatchString(input.OrderID) ||
		input.OrderStatus == "" || len(input.OrderStatus) > 64 || timestampErr != nil || updateErr != nil {
		return TikTokWebhookJob{}, ErrInvalidWebhookDelivery
	}
	return TikTokWebhookJob{
		GatewayEventID: input.GatewayEventID, NotificationID: input.NotificationID, ShopID: input.ShopID,
		OrderID: input.OrderID, OrderStatus: input.OrderStatus, Timestamp: timestamp.UTC(), OrderUpdateAt: updateAt.UTC(),
	}, nil
}

func requireOneWebhookRow(result sql.Result) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrInvalidWebhookDelivery
	}
	return nil
}
