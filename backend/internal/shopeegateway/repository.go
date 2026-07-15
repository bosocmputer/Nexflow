package shopeegateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"nexflow/internal/services/shopeeapi"
)

var ErrNonceAlreadyUsed = errors.New("gateway nonce already used")
var ErrShopAlreadyOwned = errors.New("Shopee shop is already connected to another tenant")

type Tenant struct {
	ID            string
	Slug          string
	PublicBaseURL string
	BackendURL    string
	Enabled       bool
}

type OAuthStateRecord struct {
	StateHash   string
	TenantID    string
	UserID      string
	ReturnURL   string
	Nonce       string
	Environment string
	ExpiresAt   time.Time
}

type EncryptedConnection struct {
	ID                   string
	TenantID             string
	TenantSlug           string
	ShopID               int64
	MerchantID           sql.NullInt64
	ShopName             string
	Environment          string
	AccessTokenCipher    []byte
	AccessTokenNonce     []byte
	RefreshTokenCipher   []byte
	RefreshTokenNonce    []byte
	EncryptionKeyVersion int
	AccessExpiresAt      time.Time
	RefreshExpiresAt     time.Time
	DisabledAt           sql.NullTime
}

type PushEventInput struct {
	ShopID      int64
	OrderSN     string
	PushCode    int
	EventStatus string
	DedupeKey   string
	RawPayload  json.RawMessage
}

type PushEventResult struct {
	Inserted bool
	Tenant   *Tenant
}

type DeliveryJob struct {
	ID         string
	TenantID   string
	TenantSlug string
	BackendURL string
	EventType  string
	DedupeKey  string
	Payload    json.RawMessage
	Attempts   int
}

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Ping(ctx context.Context) error {
	if r == nil || r.db == nil {
		return errors.New("gateway repository is not configured")
	}
	return r.db.PingContext(ctx)
}

func (r *Repository) SyncTenants(ctx context.Context, definitions []TenantDefinition) error {
	if r == nil || r.db == nil {
		return errors.New("gateway repository is not configured")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, tenant := range definitions {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO tenants (slug, public_base_url, backend_url, enabled)
			 VALUES ($1, $2, $3, TRUE)
			 ON CONFLICT (slug) DO UPDATE
			    SET public_base_url = EXCLUDED.public_base_url,
			        backend_url = EXCLUDED.backend_url,
			        updated_at = NOW()`,
			tenant.Slug, tenant.PublicBaseURL, tenant.BackendURL,
		); err != nil {
			return fmt.Errorf("sync tenant %s: %w", tenant.Slug, err)
		}
	}
	return tx.Commit()
}

func (r *Repository) TenantBySlug(ctx context.Context, slug string) (*Tenant, error) {
	var out Tenant
	err := r.db.QueryRowContext(ctx,
		`SELECT id::text, slug, public_base_url, backend_url, enabled
		   FROM tenants
		  WHERE slug = $1`,
		slug,
	).Scan(&out.ID, &out.Slug, &out.PublicBaseURL, &out.BackendURL, &out.Enabled)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *Repository) TenantByID(ctx context.Context, id string) (*Tenant, error) {
	var out Tenant
	err := r.db.QueryRowContext(ctx,
		`SELECT id::text, slug, public_base_url, backend_url, enabled
		   FROM tenants
		  WHERE id = $1::uuid`,
		id,
	).Scan(&out.ID, &out.Slug, &out.PublicBaseURL, &out.BackendURL, &out.Enabled)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *Repository) TenantByShopID(ctx context.Context, shopID int64) (*Tenant, error) {
	var out Tenant
	err := r.db.QueryRowContext(ctx,
		`SELECT t.id::text, t.slug, t.public_base_url, t.backend_url, t.enabled
		   FROM shop_routes r
		   JOIN tenants t ON t.id = r.tenant_id
		  WHERE r.shop_id = $1
		    AND r.active = TRUE`,
		shopID,
	).Scan(&out.ID, &out.Slug, &out.PublicBaseURL, &out.BackendURL, &out.Enabled)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *Repository) ActiveTenants(ctx context.Context) ([]Tenant, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id::text, slug, public_base_url, backend_url, enabled
		   FROM tenants
		  WHERE enabled = TRUE
		  ORDER BY slug`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	output := make([]Tenant, 0)
	for rows.Next() {
		var tenant Tenant
		if err := rows.Scan(&tenant.ID, &tenant.Slug, &tenant.PublicBaseURL, &tenant.BackendURL, &tenant.Enabled); err != nil {
			return nil, err
		}
		output = append(output, tenant)
	}
	return output, rows.Err()
}

// SyncTenantRoutes replaces only legacy-discovered routes for one tenant.
// Gateway OAuth routes are preserved, and a shop already owned by another
// tenant is never reassigned.
func (r *Repository) SyncTenantRoutes(ctx context.Context, tenantID string, shopIDs []int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`UPDATE shop_routes
		    SET active = FALSE, updated_at = NOW()
		  WHERE tenant_id = $1::uuid
		    AND route_source = 'legacy_sync'`, tenantID,
	); err != nil {
		return err
	}
	seen := make(map[int64]struct{}, len(shopIDs))
	for _, shopID := range shopIDs {
		if shopID <= 0 {
			return errors.New("invalid Shopee shop route")
		}
		if _, exists := seen[shopID]; exists {
			continue
		}
		seen[shopID] = struct{}{}
		result, err := tx.ExecContext(ctx,
			`INSERT INTO shop_routes (shop_id, tenant_id, route_source)
			 VALUES ($1, $2::uuid, 'legacy_sync')
			 ON CONFLICT (shop_id) DO UPDATE
			    SET active = TRUE,
			        last_seen_at = NOW(),
			        updated_at = NOW(),
			        route_source = CASE
			          WHEN shop_routes.route_source = 'gateway_oauth' THEN 'gateway_oauth'
			          ELSE 'legacy_sync'
			        END
			  WHERE shop_routes.tenant_id = EXCLUDED.tenant_id`,
			shopID, tenantID,
		)
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrShopAlreadyOwned
		}
	}
	return tx.Commit()
}

func (r *Repository) CreateOAuthState(ctx context.Context, record OAuthStateRecord) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO oauth_states
		   (state_hash, tenant_id, user_id, return_url, nonce, environment, expires_at)
		 VALUES ($1, $2::uuid, $3, $4, $5, $6, $7)`,
		record.StateHash, record.TenantID, record.UserID, record.ReturnURL, record.Nonce, record.Environment, record.ExpiresAt,
	)
	return err
}

func (r *Repository) ConsumeOAuthState(ctx context.Context, stateHash string) (*OAuthStateRecord, error) {
	var out OAuthStateRecord
	err := r.db.QueryRowContext(ctx,
		`UPDATE oauth_states
		    SET consumed_at = NOW()
		  WHERE state_hash = $1
		    AND consumed_at IS NULL
		    AND expires_at > NOW()
		  RETURNING state_hash, tenant_id::text, user_id, return_url, nonce, environment, expires_at`,
		stateHash,
	).Scan(&out.StateHash, &out.TenantID, &out.UserID, &out.ReturnURL, &out.Nonce, &out.Environment, &out.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *Repository) Consume(ctx context.Context, tenant, nonce string, _ time.Time) error {
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO api_request_logs
		   (tenant_id, nonce, direction, operation)
		 SELECT id, $2, 'tenant_to_gateway', 'pending'
		   FROM tenants
		  WHERE slug = $1 AND enabled = TRUE
		 ON CONFLICT (tenant_id, nonce, direction) DO NOTHING`,
		tenant, nonce,
	)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrNonceAlreadyUsed
	}
	return nil
}

func (r *Repository) RecordAPIResult(ctx context.Context, tenant, nonce, operation string, statusCode, durationMS int, errorCode, requestID string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE api_request_logs AS l
		    SET operation = $3,
		        status_code = $4,
		        duration_ms = $5,
		        error_code = $6,
		        request_id = $7
		   FROM tenants AS t
		  WHERE l.tenant_id = t.id
		    AND t.slug = $1
		    AND l.nonce = $2
		    AND l.direction = 'tenant_to_gateway'`,
		tenant, nonce, operation, statusCode, durationMS, errorCode, requestID,
	)
	return err
}

func (r *Repository) Connection(ctx context.Context, tenantSlug string, shopID int64) (*EncryptedConnection, error) {
	var out EncryptedConnection
	err := r.db.QueryRowContext(ctx,
		`SELECT c.id::text, c.tenant_id::text, t.slug, c.shop_id, c.merchant_id,
		        c.shop_name, c.environment, c.access_token_cipher, c.access_token_nonce,
		        c.refresh_token_cipher, c.refresh_token_nonce, c.encryption_key_version,
		        c.access_expires_at, c.refresh_expires_at, c.disabled_at
		   FROM shop_connections c
		   JOIN tenants t ON t.id = c.tenant_id
		  WHERE t.slug = $1
		    AND c.shop_id = $2`,
		tenantSlug, shopID,
	).Scan(
		&out.ID, &out.TenantID, &out.TenantSlug, &out.ShopID, &out.MerchantID,
		&out.ShopName, &out.Environment, &out.AccessTokenCipher, &out.AccessTokenNonce,
		&out.RefreshTokenCipher, &out.RefreshTokenNonce, &out.EncryptionKeyVersion,
		&out.AccessExpiresAt, &out.RefreshExpiresAt, &out.DisabledAt,
	)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *Repository) UpsertConnection(ctx context.Context, conn EncryptedConnection) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx,
		`INSERT INTO shop_connections
		   (tenant_id, shop_id, merchant_id, shop_name, environment,
		    access_token_cipher, access_token_nonce, refresh_token_cipher, refresh_token_nonce,
		    encryption_key_version, access_expires_at, refresh_expires_at)
		 VALUES ($1::uuid, $2, NULLIF($3, 0), $4, $5, $6, $7, $8, $9, $10, $11, $12)
		 ON CONFLICT (shop_id) DO UPDATE
		    SET tenant_id = EXCLUDED.tenant_id,
		        merchant_id = COALESCE(EXCLUDED.merchant_id, shop_connections.merchant_id),
		        shop_name = COALESCE(NULLIF(EXCLUDED.shop_name, ''), shop_connections.shop_name),
		        environment = EXCLUDED.environment,
		        access_token_cipher = EXCLUDED.access_token_cipher,
		        access_token_nonce = EXCLUDED.access_token_nonce,
		        refresh_token_cipher = EXCLUDED.refresh_token_cipher,
		        refresh_token_nonce = EXCLUDED.refresh_token_nonce,
		        encryption_key_version = EXCLUDED.encryption_key_version,
		        access_expires_at = EXCLUDED.access_expires_at,
		        refresh_expires_at = EXCLUDED.refresh_expires_at,
		        disabled_at = NULL,
		        connected_at = NOW(),
		        updated_at = NOW(),
		        last_error_code = ''
		  WHERE shop_connections.tenant_id = EXCLUDED.tenant_id`,
		conn.TenantID, conn.ShopID, conn.MerchantID.Int64, conn.ShopName, conn.Environment,
		conn.AccessTokenCipher, conn.AccessTokenNonce, conn.RefreshTokenCipher, conn.RefreshTokenNonce,
		conn.EncryptionKeyVersion, conn.AccessExpiresAt, conn.RefreshExpiresAt,
	)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrShopAlreadyOwned
	}
	result, err = tx.ExecContext(ctx,
		`INSERT INTO shop_routes (shop_id, tenant_id, route_source)
		 VALUES ($1, $2::uuid, 'gateway_oauth')
		 ON CONFLICT (shop_id) DO UPDATE
		    SET route_source = 'gateway_oauth', active = TRUE,
		        last_seen_at = NOW(), updated_at = NOW()
		  WHERE shop_routes.tenant_id = EXCLUDED.tenant_id`,
		conn.ShopID, conn.TenantID,
	)
	if err != nil {
		return err
	}
	rows, err = result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrShopAlreadyOwned
	}
	return tx.Commit()
}

func (r *Repository) UpdateConnectionTokens(ctx context.Context, conn EncryptedConnection) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE shop_connections
		    SET access_token_cipher = $2,
		        access_token_nonce = $3,
		        refresh_token_cipher = $4,
		        refresh_token_nonce = $5,
		        encryption_key_version = $6,
		        access_expires_at = $7,
		        refresh_expires_at = $8,
		        last_refreshed_at = NOW(),
		        updated_at = NOW(),
		        last_error_code = ''
		  WHERE id = $1::uuid`,
		conn.ID, conn.AccessTokenCipher, conn.AccessTokenNonce, conn.RefreshTokenCipher,
		conn.RefreshTokenNonce, conn.EncryptionKeyVersion, conn.AccessExpiresAt, conn.RefreshExpiresAt,
	)
	return err
}

func (r *Repository) EnqueueDelivery(ctx context.Context, tenantID, eventType, dedupeKey string, payload interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO delivery_outbox (tenant_id, event_type, dedupe_key, payload)
		 VALUES ($1::uuid, $2, $3, $4::jsonb)
		 ON CONFLICT (dedupe_key) DO NOTHING`,
		tenantID, eventType, dedupeKey, body,
	)
	return err
}

// AcceptPushEvent stores the authenticated Shopee payload and its tenant
// delivery atomically. Unknown shops are retained for diagnostics but are not
// sent to an arbitrary tenant.
func (r *Repository) AcceptPushEvent(ctx context.Context, input PushEventInput) (*PushEventResult, error) {
	if input.ShopID <= 0 || input.DedupeKey == "" || !json.Valid(input.RawPayload) {
		return nil, errors.New("invalid Shopee push event")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var tenant Tenant
	tenantErr := tx.QueryRowContext(ctx,
		`SELECT t.id::text, t.slug, t.public_base_url, t.backend_url, t.enabled
		   FROM shop_routes r
		   JOIN tenants t ON t.id = r.tenant_id
		  WHERE r.shop_id = $1
		    AND r.active = TRUE
		    AND t.enabled = TRUE`,
		input.ShopID,
	).Scan(&tenant.ID, &tenant.Slug, &tenant.PublicBaseURL, &tenant.BackendURL, &tenant.Enabled)
	if tenantErr != nil && !errors.Is(tenantErr, sql.ErrNoRows) {
		return nil, tenantErr
	}
	knownTenant := tenantErr == nil
	processingStatus := "unknown_shop"
	var tenantID interface{}
	if knownTenant {
		processingStatus = "queued"
		tenantID = tenant.ID
	}
	result, err := tx.ExecContext(ctx,
		`INSERT INTO push_events
		   (tenant_id, shop_id, order_sn, push_code, event_status, dedupe_key, raw_payload, processing_status)
		 VALUES (NULLIF($1, '')::uuid, $2, $3, $4, $5, $6, $7::jsonb, $8)
		 ON CONFLICT (dedupe_key) DO NOTHING`,
		stringValue(tenantID), input.ShopID, input.OrderSN, input.PushCode, input.EventStatus,
		input.DedupeKey, []byte(input.RawPayload), processingStatus,
	)
	if err != nil {
		return nil, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	inserted := rows == 1
	if inserted && knownTenant {
		payload, err := json.Marshal(shopeeapi.GatewayPushDelivery{ShopID: input.ShopID, RawPayload: json.RawMessage(input.RawPayload)})
		if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO delivery_outbox (tenant_id, event_type, dedupe_key, payload)
			 VALUES ($1::uuid, 'push_event', $2, $3::jsonb)
			 ON CONFLICT (dedupe_key) DO NOTHING`,
			tenant.ID, "push:"+input.DedupeKey, payload,
		); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	output := &PushEventResult{Inserted: inserted}
	if knownTenant {
		output.Tenant = &tenant
	}
	return output, nil
}

func (r *Repository) LeaseDeliveries(ctx context.Context, limit int) ([]DeliveryJob, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx,
		`WITH recovered AS (
		   UPDATE delivery_outbox
		      SET status = 'failed', next_run_at = NOW(), updated_at = NOW(), last_error_code = 'lease_expired'
		    WHERE status = 'running' AND updated_at < NOW() - INTERVAL '5 minutes'
		), picked AS (
		   SELECT id
		     FROM delivery_outbox
		    WHERE status IN ('pending', 'failed') AND next_run_at <= NOW()
		    ORDER BY next_run_at, created_at
		    FOR UPDATE SKIP LOCKED
		    LIMIT $1
		), leased AS (
		   UPDATE delivery_outbox d
		      SET status = 'running', attempts = attempts + 1, updated_at = NOW()
		     FROM picked
		    WHERE d.id = picked.id
		    RETURNING d.id, d.tenant_id, d.event_type, d.dedupe_key, d.payload, d.attempts
		)
		 SELECT l.id::text, l.tenant_id::text, t.slug, t.backend_url,
		        l.event_type, l.dedupe_key, l.payload, l.attempts
		   FROM leased l
		   JOIN tenants t ON t.id = l.tenant_id`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := make([]DeliveryJob, 0, limit)
	for rows.Next() {
		var job DeliveryJob
		if err := rows.Scan(&job.ID, &job.TenantID, &job.TenantSlug, &job.BackendURL, &job.EventType, &job.DedupeKey, &job.Payload, &job.Attempts); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (r *Repository) MarkDeliveryDone(ctx context.Context, job DeliveryJob) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`UPDATE delivery_outbox
		    SET status = 'delivered', delivered_at = NOW(), updated_at = NOW(), last_error_code = ''
		  WHERE id = $1::uuid AND status = 'running'`, job.ID,
	); err != nil {
		return err
	}
	if job.EventType == "push_event" {
		if _, err := tx.ExecContext(ctx,
			`UPDATE push_events
			    SET processing_status = 'delivered', delivered_at = NOW(), updated_at = NOW(), last_error_code = ''
			  WHERE dedupe_key = $1`, strings.TrimPrefix(job.DedupeKey, "push:"),
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Repository) MarkDeliveryFailed(ctx context.Context, job DeliveryJob, errorCode string, nextRunAt time.Time) error {
	if len(errorCode) > 100 {
		errorCode = errorCode[:100]
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`UPDATE delivery_outbox
		    SET status = 'failed', next_run_at = $2, updated_at = NOW(), last_error_code = $3
		  WHERE id = $1::uuid AND status = 'running'`, job.ID, nextRunAt, errorCode,
	); err != nil {
		return err
	}
	if job.EventType == "push_event" {
		if _, err := tx.ExecContext(ctx,
			`UPDATE push_events
			    SET processing_status = 'failed', attempts = $2, updated_at = NOW(), last_error_code = $3
			  WHERE dedupe_key = $1`, strings.TrimPrefix(job.DedupeKey, "push:"), job.Attempts, errorCode,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Repository) RecordOutboundAPIResult(ctx context.Context, job DeliveryJob, nonce, operation string, statusCode, durationMS int, errorCode, requestID string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO api_request_logs
		   (tenant_id, nonce, direction, operation, status_code, duration_ms, error_code, request_id)
		 VALUES ($1::uuid, $2, 'gateway_to_tenant', $3, $4, $5, $6, $7)
		 ON CONFLICT (tenant_id, nonce, direction) DO NOTHING`,
		job.TenantID, nonce, operation, statusCode, durationMS, errorCode, requestID,
	)
	return err
}

func stringValue(value interface{}) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
