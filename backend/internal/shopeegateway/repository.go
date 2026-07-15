package shopeegateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrNonceAlreadyUsed = errors.New("gateway nonce already used")

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

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
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

func (r *Repository) TenantByShopID(ctx context.Context, shopID int64) (*Tenant, error) {
	var out Tenant
	err := r.db.QueryRowContext(ctx,
		`SELECT t.id::text, t.slug, t.public_base_url, t.backend_url, t.enabled
		   FROM shop_connections c
		   JOIN tenants t ON t.id = c.tenant_id
		  WHERE c.shop_id = $1
		    AND c.disabled_at IS NULL`,
		shopID,
	).Scan(&out.ID, &out.Slug, &out.PublicBaseURL, &out.BackendURL, &out.Enabled)
	if err != nil {
		return nil, err
	}
	return &out, nil
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
	_, err := r.db.ExecContext(ctx,
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
		        last_error_code = ''`,
		conn.TenantID, conn.ShopID, conn.MerchantID.Int64, conn.ShopName, conn.Environment,
		conn.AccessTokenCipher, conn.AccessTokenNonce, conn.RefreshTokenCipher, conn.RefreshTokenNonce,
		conn.EncryptionKeyVersion, conn.AccessExpiresAt, conn.RefreshExpiresAt,
	)
	return err
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
