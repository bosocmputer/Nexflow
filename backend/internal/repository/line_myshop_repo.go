package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"nexflow/internal/models"
)

type LineMyShopRepo struct {
	db *sql.DB
}

type LineMyShopSnapshotUpsert struct {
	ConnectionID   string
	OrderNo        string
	OrderStatus    string
	PaymentStatus  string
	ShipmentStatus string
	PaymentMethod  string
	TotalAmount    float64
	SubtotalPrice  float64
	ShipmentPrice  float64
	DiscountAmount float64
	ItemCount      int
	RawDetail      json.RawMessage
	RawWebhook     json.RawMessage
	LastEventName  string
	LastEventAt    *time.Time
}

type LineMyShopWebhookEventInput struct {
	ConnectionID   string
	OrderNo        string
	RequestID      string
	EventName      string
	EventAt        *time.Time
	DedupeKey      string
	SignatureValid bool
	RawPayload     json.RawMessage
}

func NewLineMyShopRepo(db *sql.DB) *LineMyShopRepo {
	return &LineMyShopRepo{db: db}
}

func (r *LineMyShopRepo) DB() *sql.DB { return r.db }

const lineMyShopConnectionPublicCols = `
  id::text, name, channel_id, premium_id, random_id, enabled,
  COALESCE(api_key, '') <> '' AS has_api_key,
  COALESCE(webhook_secret, '') <> '' AS has_webhook_secret,
  last_sync_at, last_error, created_at, updated_at
`

func scanLineMyShopConnection(s interface{ Scan(...any) error }) (models.LineMyShopConnection, error) {
	var out models.LineMyShopConnection
	var channelID sql.NullInt64
	var lastSync sql.NullTime
	if err := s.Scan(
		&out.ID, &out.Name, &channelID, &out.PremiumID, &out.RandomID, &out.Enabled,
		&out.HasAPIKey, &out.HasWebhookSecret, &lastSync, &out.LastError, &out.CreatedAt, &out.UpdatedAt,
	); err != nil {
		return out, err
	}
	if channelID.Valid {
		v := channelID.Int64
		out.ChannelID = &v
	}
	if lastSync.Valid {
		out.LastSyncAt = &lastSync.Time
	}
	return out, nil
}

func (r *LineMyShopRepo) ListConnections(ctx context.Context) ([]models.LineMyShopConnection, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+lineMyShopConnectionPublicCols+`
		  FROM line_myshop_connections
		 ORDER BY enabled DESC, name ASC, created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list line myshop connections: %w", err)
	}
	defer rows.Close()
	out := []models.LineMyShopConnection{}
	for rows.Next() {
		conn, err := scanLineMyShopConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, conn)
	}
	return out, rows.Err()
}

func (r *LineMyShopRepo) GetConnection(ctx context.Context, id string) (*models.LineMyShopConnection, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT `+lineMyShopConnectionPublicCols+`
		  FROM line_myshop_connections
		 WHERE id = $1::uuid`, strings.TrimSpace(id))
	out, err := scanLineMyShopConnection(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get line myshop connection: %w", err)
	}
	return &out, nil
}

func (r *LineMyShopRepo) GetConnectionSecret(ctx context.Context, id string) (*models.LineMyShopConnectionSecret, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT `+lineMyShopConnectionPublicCols+`, api_key, webhook_secret
		  FROM line_myshop_connections
		 WHERE id = $1::uuid`, strings.TrimSpace(id))
	conn, apiKey, webhookSecret, err := scanLineMyShopConnectionSecret(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get line myshop connection secret: %w", err)
	}
	conn.APIKey = apiKey
	conn.WebhookSecret = webhookSecret
	return &conn, nil
}

func (r *LineMyShopRepo) FindEnabledConnectionByShop(ctx context.Context, channelID *int64, premiumID, randomID string) (*models.LineMyShopConnectionSecret, error) {
	args := []any{strings.TrimSpace(premiumID), strings.TrimSpace(randomID)}
	where := `enabled = TRUE`
	if channelID != nil && *channelID > 0 {
		args = append(args, *channelID)
		where += ` AND channel_id = $3`
	} else {
		where += ` AND (($1 <> '' AND premium_id = $1) OR ($2 <> '' AND random_id = $2))`
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT `+lineMyShopConnectionPublicCols+`, api_key, webhook_secret
		  FROM line_myshop_connections
		 WHERE `+where+`
		 ORDER BY updated_at DESC
		 LIMIT 1`, args...)
	conn, apiKey, webhookSecret, err := scanLineMyShopConnectionSecret(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find line myshop connection by shop: %w", err)
	}
	conn.APIKey = apiKey
	conn.WebhookSecret = webhookSecret
	return &conn, nil
}

func scanLineMyShopConnectionSecret(s interface{ Scan(...any) error }) (models.LineMyShopConnectionSecret, string, string, error) {
	var base models.LineMyShopConnection
	var channelID sql.NullInt64
	var lastSync sql.NullTime
	var apiKey, webhookSecret string
	if err := s.Scan(
		&base.ID, &base.Name, &channelID, &base.PremiumID, &base.RandomID, &base.Enabled,
		&base.HasAPIKey, &base.HasWebhookSecret, &lastSync, &base.LastError, &base.CreatedAt, &base.UpdatedAt,
		&apiKey, &webhookSecret,
	); err != nil {
		return models.LineMyShopConnectionSecret{}, "", "", err
	}
	if channelID.Valid {
		v := channelID.Int64
		base.ChannelID = &v
	}
	if lastSync.Valid {
		base.LastSyncAt = &lastSync.Time
	}
	return models.LineMyShopConnectionSecret{LineMyShopConnection: base}, apiKey, webhookSecret, nil
}

func (r *LineMyShopRepo) CreateConnection(ctx context.Context, in models.LineMyShopConnectionUpsert, userID string) (*models.LineMyShopConnection, error) {
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	var id string
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO line_myshop_connections
		  (name, api_key, webhook_secret, channel_id, premium_id, random_id, enabled, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, '')::uuid, NULLIF($8, '')::uuid)
		RETURNING id::text`,
		strings.TrimSpace(in.Name), strings.TrimSpace(in.APIKey), strings.TrimSpace(in.WebhookSecret),
		in.ChannelID, strings.TrimSpace(in.PremiumID), strings.TrimSpace(in.RandomID), enabled, strings.TrimSpace(userID),
	).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("create line myshop connection: %w", err)
	}
	return r.GetConnection(ctx, id)
}

func (r *LineMyShopRepo) UpdateConnection(ctx context.Context, id string, in models.LineMyShopConnectionUpsert, userID string) (*models.LineMyShopConnection, error) {
	current, err := r.GetConnection(ctx, id)
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
		UPDATE line_myshop_connections
		   SET name = $2,
		       api_key = COALESCE(NULLIF($3, ''), api_key),
		       webhook_secret = CASE WHEN $4 THEN '' ELSE COALESCE(NULLIF($5, ''), webhook_secret) END,
		       channel_id = $6,
		       premium_id = $7,
		       random_id = $8,
		       enabled = $9,
		       updated_by = NULLIF($10, '')::uuid,
		       updated_at = NOW()
		 WHERE id = $1::uuid`,
		strings.TrimSpace(id), strings.TrimSpace(in.Name), strings.TrimSpace(in.APIKey),
		in.ClearWebhookSecret, strings.TrimSpace(in.WebhookSecret), in.ChannelID, strings.TrimSpace(in.PremiumID),
		strings.TrimSpace(in.RandomID), enabled, strings.TrimSpace(userID),
	)
	if err != nil {
		return nil, fmt.Errorf("update line myshop connection: %w", err)
	}
	return r.GetConnection(ctx, id)
}

func (r *LineMyShopRepo) DeleteConnection(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM line_myshop_connections WHERE id = $1::uuid`, strings.TrimSpace(id))
	return err
}

func (r *LineMyShopRepo) MarkConnectionSync(ctx context.Context, id, errMsg string) error {
	if len(errMsg) > 800 {
		errMsg = errMsg[:800]
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE line_myshop_connections
		   SET last_sync_at = NOW(),
		       last_error = $2,
		       updated_at = NOW()
		 WHERE id = $1::uuid`,
		strings.TrimSpace(id), strings.TrimSpace(errMsg),
	)
	return err
}

func (r *LineMyShopRepo) RecordWebhookEvent(ctx context.Context, in LineMyShopWebhookEventInput) (*models.LineMyShopWebhookEvent, bool, error) {
	dedupeKey := strings.TrimSpace(in.DedupeKey)
	if dedupeKey == "" {
		return nil, false, fmt.Errorf("dedupe_key is required")
	}
	raw := string(in.RawPayload)
	if strings.TrimSpace(raw) == "" {
		raw = "{}"
	}
	var out models.LineMyShopWebhookEvent
	var eventAt, processedAt sql.NullTime
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO line_myshop_webhook_events
		  (connection_id, order_no, request_id, event_name, event_at, dedupe_key, signature_valid, raw_payload)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, COALESCE(NULLIF($8, '')::jsonb, '{}'::jsonb))
		ON CONFLICT (dedupe_key) DO NOTHING
		RETURNING id::text, connection_id::text, order_no, request_id, event_name, event_at,
		          dedupe_key, signature_valid, raw_payload, processing_status, error, created_at, processed_at`,
		strings.TrimSpace(in.ConnectionID), strings.TrimSpace(in.OrderNo), strings.TrimSpace(in.RequestID),
		strings.TrimSpace(in.EventName), in.EventAt, dedupeKey, in.SignatureValid, raw,
	).Scan(
		&out.ID, &out.ConnectionID, &out.OrderNo, &out.RequestID, &out.EventName, &eventAt,
		&out.DedupeKey, &out.SignatureValid, &out.RawPayload, &out.ProcessingStatus, &out.Error, &out.CreatedAt, &processedAt,
	)
	if err == nil {
		if eventAt.Valid {
			out.EventAt = &eventAt.Time
		}
		if processedAt.Valid {
			out.ProcessedAt = &processedAt.Time
		}
		return &out, true, nil
	}
	if err != sql.ErrNoRows {
		return nil, false, fmt.Errorf("record line myshop webhook event: %w", err)
	}
	existing, err := r.getWebhookEventByDedupeKey(ctx, dedupeKey)
	if err != nil {
		return nil, false, err
	}
	return existing, false, nil
}

func (r *LineMyShopRepo) getWebhookEventByDedupeKey(ctx context.Context, dedupeKey string) (*models.LineMyShopWebhookEvent, error) {
	var out models.LineMyShopWebhookEvent
	var eventAt, processedAt sql.NullTime
	err := r.db.QueryRowContext(ctx, `
		SELECT id::text, connection_id::text, order_no, request_id, event_name, event_at,
		       dedupe_key, signature_valid, raw_payload, processing_status, error, created_at, processed_at
		  FROM line_myshop_webhook_events
		 WHERE dedupe_key = $1`, strings.TrimSpace(dedupeKey),
	).Scan(
		&out.ID, &out.ConnectionID, &out.OrderNo, &out.RequestID, &out.EventName, &eventAt,
		&out.DedupeKey, &out.SignatureValid, &out.RawPayload, &out.ProcessingStatus, &out.Error, &out.CreatedAt, &processedAt,
	)
	if err != nil {
		return nil, err
	}
	if eventAt.Valid {
		out.EventAt = &eventAt.Time
	}
	if processedAt.Valid {
		out.ProcessedAt = &processedAt.Time
	}
	return &out, nil
}

func (r *LineMyShopRepo) MarkWebhookEvent(ctx context.Context, dedupeKey, status, errMsg string) error {
	if strings.TrimSpace(dedupeKey) == "" {
		return nil
	}
	if strings.TrimSpace(status) == "" {
		status = "processed"
	}
	if len(errMsg) > 800 {
		errMsg = errMsg[:800]
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE line_myshop_webhook_events
		   SET processing_status = $2,
		       error = $3,
		       processed_at = NOW()
		 WHERE dedupe_key = $1`,
		strings.TrimSpace(dedupeKey), strings.TrimSpace(status), strings.TrimSpace(errMsg),
	)
	return err
}

func (r *LineMyShopRepo) UpsertSnapshot(ctx context.Context, in LineMyShopSnapshotUpsert) (*models.LineMyShopOrderSnapshot, error) {
	rawDetail := string(in.RawDetail)
	if strings.TrimSpace(rawDetail) == "" {
		rawDetail = "{}"
	}
	rawWebhook := string(in.RawWebhook)
	if strings.TrimSpace(rawWebhook) == "" {
		rawWebhook = "{}"
	}
	var out models.LineMyShopOrderSnapshot
	var billID sql.NullString
	var lastEventAt, lastSyncedAt sql.NullTime
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO line_myshop_order_snapshots
		  (connection_id, order_no, order_status, payment_status, shipment_status, payment_method,
		   total_amount, subtotal_price, shipment_price, discount_amount, item_count,
		   raw_detail, raw_webhook, last_event_name, last_event_at, last_synced_at, last_error)
		VALUES ($1::uuid, $2, $3, $4, $5, $6,
		        $7, $8, $9, $10, $11,
		        COALESCE(NULLIF($12, '')::jsonb, '{}'::jsonb),
		        COALESCE(NULLIF($13, '')::jsonb, '{}'::jsonb),
		        $14, $15, NOW(), '')
		ON CONFLICT (connection_id, order_no) DO UPDATE
		   SET order_status = EXCLUDED.order_status,
		       payment_status = EXCLUDED.payment_status,
		       shipment_status = EXCLUDED.shipment_status,
		       payment_method = EXCLUDED.payment_method,
		       total_amount = EXCLUDED.total_amount,
		       subtotal_price = EXCLUDED.subtotal_price,
		       shipment_price = EXCLUDED.shipment_price,
		       discount_amount = EXCLUDED.discount_amount,
		       item_count = EXCLUDED.item_count,
		       raw_detail = CASE WHEN EXCLUDED.raw_detail <> '{}'::jsonb THEN EXCLUDED.raw_detail ELSE line_myshop_order_snapshots.raw_detail END,
		       raw_webhook = CASE WHEN EXCLUDED.raw_webhook <> '{}'::jsonb THEN EXCLUDED.raw_webhook ELSE line_myshop_order_snapshots.raw_webhook END,
		       last_event_name = COALESCE(NULLIF(EXCLUDED.last_event_name, ''), line_myshop_order_snapshots.last_event_name),
		       last_event_at = COALESCE(EXCLUDED.last_event_at, line_myshop_order_snapshots.last_event_at),
		       last_synced_at = NOW(),
		       last_error = '',
		       updated_at = NOW()
		RETURNING id::text, connection_id::text, order_no, order_status, payment_status, shipment_status,
		          payment_method, total_amount::float8, subtotal_price::float8, shipment_price::float8,
		          discount_amount::float8, item_count, raw_detail, raw_webhook, bill_id::text, sml_doc_no,
		          last_event_name, last_event_at, last_synced_at, last_error, created_at, updated_at`,
		strings.TrimSpace(in.ConnectionID), strings.TrimSpace(in.OrderNo), strings.TrimSpace(in.OrderStatus),
		strings.TrimSpace(in.PaymentStatus), strings.TrimSpace(in.ShipmentStatus), strings.TrimSpace(in.PaymentMethod),
		in.TotalAmount, in.SubtotalPrice, in.ShipmentPrice, in.DiscountAmount, in.ItemCount,
		rawDetail, rawWebhook, strings.TrimSpace(in.LastEventName), in.LastEventAt,
	).Scan(
		&out.ID, &out.ConnectionID, &out.OrderNo, &out.OrderStatus, &out.PaymentStatus, &out.ShipmentStatus,
		&out.PaymentMethod, &out.TotalAmount, &out.SubtotalPrice, &out.ShipmentPrice,
		&out.DiscountAmount, &out.ItemCount, &out.RawDetail, &out.RawWebhook, &billID, &out.SMLDocNo,
		&out.LastEventName, &lastEventAt, &lastSyncedAt, &out.LastError, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert line myshop snapshot: %w", err)
	}
	if billID.Valid {
		out.BillID = &billID.String
	}
	if lastEventAt.Valid {
		out.LastEventAt = &lastEventAt.Time
	}
	if lastSyncedAt.Valid {
		out.LastSyncedAt = &lastSyncedAt.Time
	}
	return &out, nil
}

func (r *LineMyShopRepo) LinkBill(ctx context.Context, connectionID, orderNo, billID, smlDocNo string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE line_myshop_order_snapshots
		   SET bill_id = COALESCE(NULLIF($3, '')::uuid, bill_id),
		       sml_doc_no = COALESCE(NULLIF($4, ''), sml_doc_no),
		       updated_at = NOW()
		 WHERE connection_id = $1::uuid
		   AND order_no = $2`,
		strings.TrimSpace(connectionID), strings.TrimSpace(orderNo), strings.TrimSpace(billID), strings.TrimSpace(smlDocNo),
	)
	return err
}

func (r *LineMyShopRepo) FindBillIDForOrder(ctx context.Context, connectionID, orderNo string) (string, bool, error) {
	var billID string
	err := r.db.QueryRowContext(ctx, `
		SELECT id::text
		  FROM bills
		 WHERE source = 'line_myshop'
		   AND raw_data->>'line_myshop_connection_id' = $1
		   AND raw_data->>'line_myshop_order_no' = $2
		   AND archived_at IS NULL
		 ORDER BY created_at DESC
		 LIMIT 1`,
		strings.TrimSpace(connectionID), strings.TrimSpace(orderNo),
	).Scan(&billID)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("find line myshop bill: %w", err)
	}
	return billID, true, nil
}
