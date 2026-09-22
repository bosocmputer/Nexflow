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
	ID                    string
	GatewayEventID        string
	NotificationID        string
	NotificationType      int
	ShopID                string
	OrderID               string
	OrderStatus           string
	CancellationStatus    string
	CancellationID        string
	CancellationRole      string
	CancellationCreatedAt time.Time
	Timestamp             time.Time
	OrderUpdateAt         time.Time
	Attempts              int
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
		    notification_type, event_order_status, cancellation_status, cancellation_id,
		    cancellation_role, cancellation_created_at, event_timestamp, order_update_at)
		 VALUES ($1::uuid, $2, $3::uuid, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		 ON CONFLICT DO NOTHING`,
		prepared.GatewayEventID, prepared.NotificationID, connectionID, prepared.ShopID,
		prepared.OrderID, prepared.NotificationType, prepared.OrderStatus, prepared.CancellationStatus,
		prepared.CancellationID, prepared.CancellationRole, nullableTikTokWebhookTime(prepared.CancellationCreatedAt),
		prepared.Timestamp, prepared.OrderUpdateAt,
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
	var cancellationCreatedAt sql.NullTime
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
		   RETURNING e.id::text, e.gateway_event_id::text, e.notification_id, e.notification_type, e.shop_id, e.order_id,
		             e.event_order_status, e.cancellation_status, e.cancellation_id, e.cancellation_role,
		             e.cancellation_created_at, e.event_timestamp, e.order_update_at, e.attempts
		 )
		 SELECT * FROM leased`, now.UTC(), maxTikTokWebhookAttempts,
	).Scan(&job.ID, &job.GatewayEventID, &job.NotificationID, &job.NotificationType, &job.ShopID, &job.OrderID,
		&job.OrderStatus, &job.CancellationStatus,
		&job.CancellationID, &job.CancellationRole, &cancellationCreatedAt, &job.Timestamp, &job.OrderUpdateAt, &job.Attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return TikTokWebhookJob{}, ErrNoWebhookJobDue
	}
	if err != nil {
		return TikTokWebhookJob{}, err
	}
	if cancellationCreatedAt.Valid {
		job.CancellationCreatedAt = cancellationCreatedAt.Time.UTC()
	}
	return job, nil
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
	input.CancellationStatus = strings.ToUpper(strings.TrimSpace(input.CancellationStatus))
	input.CancellationID = strings.TrimSpace(input.CancellationID)
	input.CancellationRole = strings.ToUpper(strings.TrimSpace(input.CancellationRole))
	timestamp, timestampErr := time.Parse(time.RFC3339, input.Timestamp)
	updateAt, updateErr := time.Parse(time.RFC3339, input.OrderUpdateAt)
	cancellationCreatedAt, cancellationCreatedAtErr := parseOptionalTikTokWebhookTime(input.CancellationCreatedAt)
	if !connectionIDPattern.MatchString(input.GatewayEventID) || !tikTokNumericIDPattern.MatchString(input.NotificationID) ||
		!tikTokNumericIDPattern.MatchString(input.ShopID) || !tikTokNumericIDPattern.MatchString(input.OrderID) ||
		timestampErr != nil || updateErr != nil || cancellationCreatedAtErr != nil {
		return TikTokWebhookJob{}, ErrInvalidWebhookDelivery
	}
	notificationType := input.NotificationType
	if notificationType == 0 {
		notificationType = 1
	}
	if (notificationType == 1 && (input.OrderStatus == "" || len(input.OrderStatus) > 64 || input.CancellationStatus != "" || input.CancellationID != "" || input.CancellationRole != "" || !cancellationCreatedAt.IsZero())) ||
		(notificationType == 11 && (input.OrderStatus != "" || !validTikTokCancellationWebhook(input.CancellationStatus, input.CancellationID, input.CancellationRole, cancellationCreatedAt))) ||
		(notificationType != 1 && notificationType != 11) {
		return TikTokWebhookJob{}, ErrInvalidWebhookDelivery
	}
	return TikTokWebhookJob{
		GatewayEventID: input.GatewayEventID, NotificationID: input.NotificationID, NotificationType: notificationType,
		ShopID: input.ShopID, OrderID: input.OrderID, OrderStatus: input.OrderStatus,
		CancellationStatus: input.CancellationStatus, CancellationID: input.CancellationID,
		CancellationRole: input.CancellationRole, CancellationCreatedAt: cancellationCreatedAt,
		Timestamp: timestamp.UTC(), OrderUpdateAt: updateAt.UTC(),
	}, nil
}

func validTikTokCancellationWebhook(status, cancellationID, role string, createdAt time.Time) bool {
	if status != "CANCELLATION_REQUEST_PENDING" && status != "CANCELLATION_REQUEST_SUCCESS" &&
		status != "CANCELLATION_REQUEST_CANCELLED" && status != "CANCELLATION_REQUEST_COMPLETE" {
		return false
	}
	return tikTokNumericIDPattern.MatchString(cancellationID) && (role == "BUYER" || role == "SELLER" || role == "SYSTEM") && !createdAt.IsZero()
}

func parseOptionalTikTokWebhookTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}

func nullableTikTokWebhookTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
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
