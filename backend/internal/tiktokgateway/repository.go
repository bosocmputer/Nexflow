package tiktokgateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrNonceAlreadyUsed  = errors.New("gateway nonce already used")
	ErrShopAlreadyOwned  = errors.New("TikTok Shop is already connected to another tenant")
	ErrInvalidConnection = errors.New("invalid TikTok Shop connection")
)

type Tenant struct {
	ID            string
	Slug          string
	PublicBaseURL string
	BackendURL    string
	Enabled       bool
}

type OAuthStateRecord struct {
	StateHash string
	TenantID  string
	UserID    string
	ReturnURL string
	Nonce     string
	ExpiresAt time.Time
}

type EncryptedConnection struct {
	ID                   string
	TenantID             string
	TenantSlug           string
	ShopID               string
	ShopCipher           string
	OpenID               string
	SellerName           string
	SellerBaseRegion     string
	GrantedScopes        []string
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

func (r *Repository) Ping(ctx context.Context) error {
	if r == nil || r.db == nil {
		return errors.New("TikTok gateway repository is not configured")
	}
	return r.db.PingContext(ctx)
}

func (r *Repository) SyncTenants(ctx context.Context, definitions []TenantDefinition) error {
	if r == nil || r.db == nil {
		return errors.New("TikTok gateway repository is not configured")
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
	if r == nil || r.db == nil {
		return nil, errors.New("TikTok gateway repository is not configured")
	}
	var tenant Tenant
	err := r.db.QueryRowContext(ctx,
		`SELECT id::text, slug, public_base_url, backend_url, enabled
		   FROM tenants
		  WHERE slug = $1`, strings.ToLower(strings.TrimSpace(slug)),
	).Scan(&tenant.ID, &tenant.Slug, &tenant.PublicBaseURL, &tenant.BackendURL, &tenant.Enabled)
	if err != nil {
		return nil, err
	}
	return &tenant, nil
}

func (r *Repository) CreateOAuthState(ctx context.Context, record OAuthStateRecord) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO oauth_states (state_hash, tenant_id, user_id, return_url, nonce, expires_at)
		 VALUES ($1, $2::uuid, $3, $4, $5, $6)`,
		record.StateHash, record.TenantID, record.UserID, record.ReturnURL, record.Nonce, record.ExpiresAt,
	)
	return err
}

func (r *Repository) ConsumeOAuthState(ctx context.Context, stateHash string) (*OAuthStateRecord, error) {
	var record OAuthStateRecord
	err := r.db.QueryRowContext(ctx,
		`UPDATE oauth_states
		    SET consumed_at = NOW()
		  WHERE state_hash = $1
		    AND consumed_at IS NULL
		    AND expires_at > NOW()
		  RETURNING state_hash, tenant_id::text, user_id, return_url, nonce, expires_at`,
		strings.TrimSpace(stateHash),
	).Scan(&record.StateHash, &record.TenantID, &record.UserID, &record.ReturnURL, &record.Nonce, &record.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (r *Repository) UpsertConnection(ctx context.Context, connection EncryptedConnection) error {
	if r == nil || r.db == nil {
		return errors.New("TikTok gateway repository is not configured")
	}
	if err := validateEncryptedConnection(connection); err != nil {
		return err
	}
	return upsertConnection(ctx, r.db, connection)
}

// UpsertConnections writes every shop returned by one seller authorization as
// a unit. This prevents a multi-shop authorization from being only partially
// bound to a Nexflow tenant when one shop conflicts or a database write fails.
func (r *Repository) UpsertConnections(ctx context.Context, connections []EncryptedConnection) error {
	if r == nil || r.db == nil {
		return errors.New("TikTok gateway repository is not configured")
	}
	if len(connections) == 0 {
		return ErrInvalidConnection
	}
	for _, connection := range connections {
		if err := validateEncryptedConnection(connection); err != nil {
			return err
		}
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, connection := range connections {
		if err := upsertConnection(ctx, tx, connection); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type connectionExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func upsertConnection(ctx context.Context, execer connectionExecer, connection EncryptedConnection) error {
	scopes, err := json.Marshal(connection.GrantedScopes)
	if err != nil {
		return err
	}
	result, err := execer.ExecContext(ctx,
		`INSERT INTO shop_connections
		   (tenant_id, shop_id, shop_cipher, open_id, seller_name, seller_base_region, granted_scopes,
		    access_token_cipher, access_token_nonce, refresh_token_cipher, refresh_token_nonce,
		    encryption_key_version, access_expires_at, refresh_expires_at)
		 VALUES ($1::uuid, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10, $11, $12, $13, $14)
		 ON CONFLICT (shop_id) DO UPDATE
		    SET shop_cipher = EXCLUDED.shop_cipher,
		        open_id = EXCLUDED.open_id,
		        seller_name = EXCLUDED.seller_name,
		        seller_base_region = EXCLUDED.seller_base_region,
		        granted_scopes = EXCLUDED.granted_scopes,
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
		connection.TenantID, connection.ShopID, connection.ShopCipher, connection.OpenID,
		connection.SellerName, connection.SellerBaseRegion, scopes,
		connection.AccessTokenCipher, connection.AccessTokenNonce,
		connection.RefreshTokenCipher, connection.RefreshTokenNonce,
		connection.EncryptionKeyVersion, connection.AccessExpiresAt, connection.RefreshExpiresAt,
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
	return nil
}

func validateEncryptedConnection(connection EncryptedConnection) error {
	if strings.TrimSpace(connection.TenantID) == "" ||
		strings.TrimSpace(connection.ShopID) == "" ||
		strings.TrimSpace(connection.ShopCipher) == "" ||
		strings.TrimSpace(connection.OpenID) == "" ||
		len(connection.GrantedScopes) == 0 ||
		len(connection.AccessTokenCipher) == 0 ||
		len(connection.AccessTokenNonce) == 0 ||
		len(connection.RefreshTokenCipher) == 0 ||
		len(connection.RefreshTokenNonce) == 0 ||
		connection.EncryptionKeyVersion <= 0 ||
		connection.AccessExpiresAt.IsZero() ||
		connection.RefreshExpiresAt.IsZero() {
		return ErrInvalidConnection
	}
	return nil
}

func (r *Repository) Consume(ctx context.Context, tenant, nonce string, _ time.Time) error {
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO api_request_logs (tenant_id, nonce, direction, operation)
		 SELECT id, $2, 'tenant_to_gateway', 'pending'
		   FROM tenants
		  WHERE slug = $1 AND enabled = TRUE
		 ON CONFLICT (tenant_id, nonce, direction) DO NOTHING`,
		strings.ToLower(strings.TrimSpace(tenant)), strings.TrimSpace(nonce),
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
		    SET operation = $3, status_code = $4, duration_ms = $5, error_code = $6, request_id = $7
		   FROM tenants AS t
		  WHERE l.tenant_id = t.id AND t.slug = $1 AND l.nonce = $2 AND l.direction = 'tenant_to_gateway'`,
		strings.ToLower(strings.TrimSpace(tenant)), strings.TrimSpace(nonce), strings.TrimSpace(operation),
		statusCode, durationMS, strings.TrimSpace(errorCode), strings.TrimSpace(requestID),
	)
	return err
}
