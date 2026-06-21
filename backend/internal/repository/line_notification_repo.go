package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"nexflow/internal/models"
)

type LineNotificationRepo struct {
	db *sql.DB
}

func NewLineNotificationRepo(db *sql.DB) *LineNotificationRepo {
	return &LineNotificationRepo{db: db}
}

const lineNotificationRecipientCols = `
  r.id::text, r.line_oa_id::text, COALESCE(oa.name, '') AS line_oa_name,
  r.name, r.destination_type, r.destination_id, r.enabled,
  r.last_test_at, r.last_test_status, r.last_test_error,
  r.last_sent_at, r.last_error, r.created_at, r.updated_at
`

func scanLineNotificationRecipient(s interface{ Scan(...any) error }) (models.LineNotificationRecipient, error) {
	var out models.LineNotificationRecipient
	var lastTestAt, lastSentAt sql.NullTime
	if err := s.Scan(
		&out.ID, &out.LineOAID, &out.LineOAName, &out.Name, &out.DestinationType,
		&out.DestinationID, &out.Enabled, &lastTestAt, &out.LastTestStatus,
		&out.LastTestError, &lastSentAt, &out.LastError, &out.CreatedAt, &out.UpdatedAt,
	); err != nil {
		return out, err
	}
	if lastTestAt.Valid {
		out.LastTestAt = &lastTestAt.Time
	}
	if lastSentAt.Valid {
		out.LastSentAt = &lastSentAt.Time
	}
	return out, nil
}

func normalizeLineDestinationType(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "group", "room":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return "user"
	}
}

func (r *LineNotificationRepo) ListRecipients(ctx context.Context) ([]models.LineNotificationRecipient, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+lineNotificationRecipientCols+`
		  FROM line_notification_recipients r
		  JOIN line_oa_accounts oa ON oa.id = r.line_oa_id
		 ORDER BY r.enabled DESC, r.name ASC`)
	if err != nil {
		return nil, fmt.Errorf("list line notification recipients: %w", err)
	}
	defer rows.Close()
	out := []models.LineNotificationRecipient{}
	for rows.Next() {
		row, err := scanLineNotificationRecipient(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *LineNotificationRepo) GetRecipient(ctx context.Context, id string) (*models.LineNotificationRecipient, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT `+lineNotificationRecipientCols+`
		  FROM line_notification_recipients r
		  JOIN line_oa_accounts oa ON oa.id = r.line_oa_id
		 WHERE r.id = $1::uuid`, strings.TrimSpace(id))
	out, err := scanLineNotificationRecipient(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get line notification recipient: %w", err)
	}
	return &out, nil
}

func (r *LineNotificationRepo) CreateRecipient(ctx context.Context, in models.LineNotificationRecipientUpsert) (*models.LineNotificationRecipient, error) {
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	var id string
	if err := r.db.QueryRowContext(ctx, `
		INSERT INTO line_notification_recipients
		  (line_oa_id, name, destination_type, destination_id, enabled)
		VALUES ($1::uuid, $2, $3, $4, $5)
		RETURNING id::text`,
		strings.TrimSpace(in.LineOAID), strings.TrimSpace(in.Name),
		normalizeLineDestinationType(in.DestinationType), strings.TrimSpace(in.DestinationID), enabled,
	).Scan(&id); err != nil {
		return nil, fmt.Errorf("create line notification recipient: %w", err)
	}
	return r.GetRecipient(ctx, id)
}

func (r *LineNotificationRepo) UpdateRecipient(ctx context.Context, id string, in models.LineNotificationRecipientUpsert) (*models.LineNotificationRecipient, error) {
	current, err := r.GetRecipient(ctx, id)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, sql.ErrNoRows
	}
	enabled := current.Enabled
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	_, err = r.db.ExecContext(ctx, `
		UPDATE line_notification_recipients
		   SET line_oa_id = $1::uuid,
		       name = $2,
		       destination_type = $3,
		       destination_id = $4,
		       enabled = $5,
		       updated_at = NOW()
		 WHERE id = $6::uuid`,
		strings.TrimSpace(in.LineOAID), strings.TrimSpace(in.Name),
		normalizeLineDestinationType(in.DestinationType), strings.TrimSpace(in.DestinationID),
		enabled, strings.TrimSpace(id),
	)
	if err != nil {
		return nil, fmt.Errorf("update line notification recipient: %w", err)
	}
	return r.GetRecipient(ctx, id)
}

func (r *LineNotificationRepo) DeleteRecipient(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM line_notification_recipients WHERE id = $1::uuid`, strings.TrimSpace(id))
	if err != nil {
		return fmt.Errorf("delete line notification recipient: %w", err)
	}
	return nil
}

func (r *LineNotificationRepo) MarkRecipientTest(ctx context.Context, id, status, errMsg string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE line_notification_recipients
		   SET last_test_at = NOW(),
		       last_test_status = $2,
		       last_test_error = $3,
		       updated_at = NOW()
		 WHERE id = $1::uuid`,
		strings.TrimSpace(id), strings.TrimSpace(status), strings.TrimSpace(errMsg),
	)
	return err
}

func (r *LineNotificationRepo) Enqueue(ctx context.Context, in models.LineNotificationMessageInput) (int, error) {
	in = normalizeLineNotificationMessageInput(in)
	if in.Title == "" || in.DedupeKey == "" || in.MessageText == "" {
		return 0, fmt.Errorf("line notification title, dedupe_key, and message_text are required")
	}
	rows, err := r.db.QueryContext(ctx, `
		INSERT INTO line_notification_deliveries
		  (recipient_id, line_oa_id, source, severity, title, body, action_url,
		   entity_type, entity_id, dedupe_key, message_text, alt_text, flex_payload, payload_version)
		SELECT r.id, r.line_oa_id, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12
		  FROM line_notification_recipients r
		  JOIN line_oa_accounts oa ON oa.id = r.line_oa_id
		 WHERE r.enabled = TRUE
		   AND oa.enabled = TRUE
		 ON CONFLICT (recipient_id, dedupe_key) DO NOTHING
		 RETURNING id`,
		in.Source, in.Severity, in.Title, in.Body, in.ActionURL,
		in.EntityType, in.EntityID, in.DedupeKey, in.MessageText,
		in.AltText, string(in.FlexPayload), in.PayloadVersion,
	)
	if err != nil {
		return 0, fmt.Errorf("enqueue line notification: %w", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		count++
	}
	return count, rows.Err()
}

func (r *LineNotificationRepo) LeaseDeliveries(ctx context.Context, limit, maxAttempts int) ([]models.LineNotificationDeliveryJob, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	rows, err := r.db.QueryContext(ctx, `
		WITH picked AS (
			SELECT d.id
			  FROM line_notification_deliveries d
			  JOIN line_notification_recipients r ON r.id = d.recipient_id
			  JOIN line_oa_accounts oa ON oa.id = d.line_oa_id
			 WHERE d.status IN ('queued','failed')
			   AND d.attempts < $1
			   AND d.next_run_at <= NOW()
			   AND r.enabled = TRUE
			   AND oa.enabled = TRUE
			 ORDER BY d.created_at ASC
			 LIMIT $2
			 FOR UPDATE SKIP LOCKED
		)
		UPDATE line_notification_deliveries d
		   SET status = 'sending',
		       attempts = d.attempts + 1,
		       updated_at = NOW()
		  FROM picked p, line_notification_recipients r, line_oa_accounts oa
		 WHERE d.id = p.id
		   AND r.id = d.recipient_id
		   AND oa.id = d.line_oa_id
		 RETURNING d.id::text, d.recipient_id::text, r.name, d.line_oa_id::text, oa.name,
		           d.source, d.severity, d.title, d.body, d.action_url, d.entity_type,
		           d.entity_id, d.dedupe_key, d.message_text, d.alt_text, d.flex_payload, d.payload_version,
		           d.status, d.attempts,
		           d.last_error, d.next_run_at, d.sent_at, d.created_at, d.updated_at,
		           r.destination_type, r.destination_id, oa.channel_secret, oa.channel_access_token`,
		maxAttempts, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("lease line notification deliveries: %w", err)
	}
	defer rows.Close()
	out := []models.LineNotificationDeliveryJob{}
	for rows.Next() {
		var job models.LineNotificationDeliveryJob
		var sentAt sql.NullTime
		if err := rows.Scan(
			&job.ID, &job.RecipientID, &job.Recipient, &job.LineOAID, &job.LineOAName,
			&job.Source, &job.Severity, &job.Title, &job.Body, &job.ActionURL,
			&job.EntityType, &job.EntityID, &job.DedupeKey, &job.MessageText,
			&job.AltText, &job.FlexPayload, &job.PayloadVersion,
			&job.Status, &job.Attempts, &job.LastError, &job.NextRunAt, &sentAt,
			&job.CreatedAt, &job.UpdatedAt, &job.DestinationType, &job.DestinationID,
			&job.ChannelSecret, &job.ChannelAccessToken,
		); err != nil {
			return nil, err
		}
		if sentAt.Valid {
			job.SentAt = &sentAt.Time
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (r *LineNotificationRepo) MarkDeliverySent(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE line_notification_deliveries d
		   SET status = 'sent',
		       last_error = '',
		       sent_at = NOW(),
		       updated_at = NOW()
		 WHERE d.id = $1::uuid`, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	_, _ = r.db.ExecContext(ctx, `
		UPDATE line_notification_recipients r
		   SET last_sent_at = NOW(),
		       last_error = '',
		       updated_at = NOW()
		  FROM line_notification_deliveries d
		 WHERE d.id = $1::uuid
		   AND r.id = d.recipient_id`, strings.TrimSpace(id))
	return nil
}

func (r *LineNotificationRepo) MarkDeliveryFailed(ctx context.Context, id, errMsg string, maxAttempts int) error {
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	errMsg = strings.TrimSpace(errMsg)
	if len(errMsg) > 1000 {
		errMsg = errMsg[:1000]
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE line_notification_deliveries
		   SET status = 'failed',
		       last_error = $2,
		       next_run_at = CASE
		         WHEN attempts >= $3 THEN NOW() + INTERVAL '100 years'
		         ELSE NOW() + (attempts * INTERVAL '5 minutes')
		       END,
		       updated_at = NOW()
		 WHERE id = $1::uuid`,
		strings.TrimSpace(id), errMsg, maxAttempts,
	)
	if err != nil {
		return err
	}
	_, _ = r.db.ExecContext(ctx, `
		UPDATE line_notification_recipients r
		   SET last_error = $2,
		       updated_at = NOW()
		  FROM line_notification_deliveries d
		 WHERE d.id = $1::uuid
		   AND r.id = d.recipient_id`, strings.TrimSpace(id), errMsg)
	return nil
}

func (r *LineNotificationRepo) RecentDeliveries(ctx context.Context, limit int) ([]models.LineNotificationDelivery, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT d.id::text, d.recipient_id::text, COALESCE(r.name, ''), d.line_oa_id::text,
		       COALESCE(oa.name, ''), d.source, d.severity, d.title, d.body, d.action_url,
		       d.entity_type, d.entity_id, d.dedupe_key, d.message_text,
		       d.alt_text, d.flex_payload, d.payload_version, d.status,
		       d.attempts, d.last_error, d.next_run_at, d.sent_at, d.created_at, d.updated_at
		  FROM line_notification_deliveries d
		  LEFT JOIN line_notification_recipients r ON r.id = d.recipient_id
		  LEFT JOIN line_oa_accounts oa ON oa.id = d.line_oa_id
		 ORDER BY d.created_at DESC
		 LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("recent line notification deliveries: %w", err)
	}
	defer rows.Close()
	out := []models.LineNotificationDelivery{}
	for rows.Next() {
		var row models.LineNotificationDelivery
		var sentAt sql.NullTime
		if err := rows.Scan(
			&row.ID, &row.RecipientID, &row.Recipient, &row.LineOAID, &row.LineOAName,
			&row.Source, &row.Severity, &row.Title, &row.Body, &row.ActionURL,
			&row.EntityType, &row.EntityID, &row.DedupeKey, &row.MessageText,
			&row.AltText, &row.FlexPayload, &row.PayloadVersion,
			&row.Status, &row.Attempts, &row.LastError, &row.NextRunAt, &sentAt,
			&row.CreatedAt, &row.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if sentAt.Valid {
			row.SentAt = &sentAt.Time
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func normalizeLineNotificationMessageInput(in models.LineNotificationMessageInput) models.LineNotificationMessageInput {
	in.Source = strings.TrimSpace(in.Source)
	if in.Source == "" {
		in.Source = "shopee_realtime"
	}
	in.Severity = strings.ToLower(strings.TrimSpace(in.Severity))
	switch in.Severity {
	case "warning", "error":
	default:
		in.Severity = "info"
	}
	in.Title = strings.TrimSpace(in.Title)
	in.Body = strings.TrimSpace(in.Body)
	in.ActionURL = strings.TrimSpace(in.ActionURL)
	in.EntityType = strings.TrimSpace(in.EntityType)
	in.EntityID = strings.TrimSpace(in.EntityID)
	in.DedupeKey = strings.TrimSpace(in.DedupeKey)
	in.MessageText = strings.TrimSpace(in.MessageText)
	in.AltText = strings.TrimSpace(in.AltText)
	if len(in.FlexPayload) == 0 || !json.Valid(in.FlexPayload) {
		in.FlexPayload = json.RawMessage(`{}`)
		in.PayloadVersion = 0
	}
	if string(in.FlexPayload) == "{}" {
		in.PayloadVersion = 0
	} else if in.PayloadVersion <= 0 {
		in.PayloadVersion = 1
	}
	return in
}
