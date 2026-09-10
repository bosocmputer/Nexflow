package tiktokgateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
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
	ShopName             string
	ShopRegion           string
	SellerType           string
	ShopCode             string
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

type ConnectionMetadata struct {
	ID               string
	ShopID           string
	ShopName         string
	ShopRegion       string
	SellerType       string
	ShopCode         string
	GrantedScopes    []string
	AccessExpiresAt  time.Time
	RefreshExpiresAt time.Time
	DisabledAt       sql.NullTime
	ConnectedAt      time.Time
	UpdatedAt        time.Time
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

func (r *Repository) ListConnectionsByTenantID(ctx context.Context, tenantID string) ([]ConnectionMetadata, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("TikTok gateway repository is not configured")
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT id::text, shop_id, shop_name, shop_region, seller_type, shop_code, granted_scopes,
		        access_expires_at, refresh_expires_at, disabled_at, connected_at, updated_at
		   FROM shop_connections
		  WHERE tenant_id = $1::uuid
		  ORDER BY connected_at, shop_id
		  LIMIT 1001`, strings.TrimSpace(tenantID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	connections := make([]ConnectionMetadata, 0)
	for rows.Next() {
		var connection ConnectionMetadata
		var scopes []byte
		if err := rows.Scan(
			&connection.ID, &connection.ShopID, &connection.ShopName, &connection.ShopRegion,
			&connection.SellerType, &connection.ShopCode, &scopes, &connection.AccessExpiresAt,
			&connection.RefreshExpiresAt, &connection.DisabledAt, &connection.ConnectedAt, &connection.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(scopes, &connection.GrantedScopes); err != nil {
			return nil, fmt.Errorf("decode TikTok Shop connection scopes: %w", err)
		}
		connections = append(connections, connection)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(connections) > 1000 {
		return nil, errors.New("TikTok Shop connection limit exceeded")
	}
	return connections, nil
}

func (r *Repository) AuthorizationTokenGroupByShop(ctx context.Context, tenantSlug, shopID string) (*AuthorizationTokenGroup, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("TikTok gateway repository is not configured")
	}
	rows, err := r.db.QueryContext(ctx,
		`WITH target_authorization AS (
		   SELECT sc.tenant_id, sc.open_id
		     FROM shop_connections AS sc
		     JOIN tenants AS target_tenant ON target_tenant.id = sc.tenant_id
		    WHERE target_tenant.slug = $1
		      AND target_tenant.enabled = TRUE
		      AND sc.shop_id = $2
		      AND sc.disabled_at IS NULL
		)
		 SELECT t.id::text, t.slug, sc.open_id, sc.granted_scopes,
		        sc.access_expires_at, sc.refresh_expires_at,
		        sc.id::text, sc.shop_id, sc.shop_cipher,
		        sc.access_token_cipher, sc.access_token_nonce,
		        sc.refresh_token_cipher, sc.refresh_token_nonce,
		        sc.encryption_key_version
		   FROM target_authorization AS target
		   JOIN tenants AS t ON t.id = target.tenant_id
		   JOIN shop_connections AS sc
		     ON sc.tenant_id = target.tenant_id AND sc.open_id = target.open_id
		  WHERE sc.disabled_at IS NULL
		  ORDER BY sc.shop_id
		  LIMIT 1001`,
		strings.ToLower(strings.TrimSpace(tenantSlug)), strings.TrimSpace(shopID),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var group *AuthorizationTokenGroup
	for rows.Next() {
		var tenantID, storedTenantSlug, openID string
		var scopesJSON []byte
		var accessExpiresAt, refreshExpiresAt time.Time
		var connection AuthorizationTokenConnection
		if err := rows.Scan(
			&tenantID, &storedTenantSlug, &openID, &scopesJSON,
			&accessExpiresAt, &refreshExpiresAt,
			&connection.ID, &connection.ShopID, &connection.ShopCipher,
			&connection.AccessTokenCipher, &connection.AccessTokenNonce,
			&connection.RefreshTokenCipher, &connection.RefreshTokenNonce,
			&connection.EncryptionKeyVersion,
		); err != nil {
			return nil, err
		}
		var scopes []string
		if err := json.Unmarshal(scopesJSON, &scopes); err != nil {
			return nil, fmt.Errorf("decode TikTok Shop authorization scopes: %w", err)
		}
		if group == nil {
			group = &AuthorizationTokenGroup{
				TenantID: tenantID, TenantSlug: storedTenantSlug, OpenID: openID,
				GrantedScopes: scopes, AccessExpiresAt: accessExpiresAt, RefreshExpiresAt: refreshExpiresAt,
			}
		} else if group.TenantID != tenantID || group.TenantSlug != storedTenantSlug || group.OpenID != openID ||
			!group.AccessExpiresAt.Equal(accessExpiresAt) || !group.RefreshExpiresAt.Equal(refreshExpiresAt) || !sameStrings(group.GrantedScopes, scopes) {
			return nil, ErrInvalidTokenCredential
		}
		group.Connections = append(group.Connections, connection)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if group == nil {
		return nil, sql.ErrNoRows
	}
	if len(group.Connections) > 1000 {
		return nil, ErrInvalidTokenCredential
	}
	return group, nil
}

// LockAuthorizationRefresh uses a session-scoped PostgreSQL advisory lock so
// multiple gateway processes cannot rotate the same seller refresh token at
// the same time. The caller must always invoke the returned unlock function.
func (r *Repository) LockAuthorizationRefresh(ctx context.Context, tenantID, openID string) (func(), error) {
	if r == nil || r.db == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(openID) == "" {
		return nil, ErrInvalidTokenCredential
	}
	connection, err := r.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	lockKey := strings.TrimSpace(tenantID) + "\x00" + strings.TrimSpace(openID)
	var locked bool
	if err := connection.QueryRowContext(ctx, `SELECT TRUE FROM pg_advisory_lock(hashtextextended($1, 0))`, lockKey).Scan(&locked); err != nil || !locked {
		_ = connection.Close()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("TikTok Shop authorization lock was not acquired")
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			unlockContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var unlocked bool
			_ = connection.QueryRowContext(unlockContext, `SELECT pg_advisory_unlock(hashtextextended($1, 0))`, lockKey).Scan(&unlocked)
			_ = connection.Close()
		})
	}, nil
}

// RotateAuthorizationTokens replaces the duplicated encrypted token material
// for every active shop belonging to one TikTok seller grant in one database
// transaction. If the sibling set changed after it was read, nothing is saved.
func (r *Repository) RotateAuthorizationTokens(ctx context.Context, tenantID, openID string, rotations []RotatedConnectionTokens, metadata RefreshedAuthorizationMetadata) error {
	if r == nil || r.db == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(openID) == "" ||
		len(rotations) == 0 || len(rotations) > 1000 || !containsEveryScope(metadata.GrantedScopes, requiredOAuthScopes) ||
		metadata.RefreshedAt.IsZero() || !metadata.AccessExpiresAt.After(metadata.RefreshedAt) || !metadata.RefreshExpiresAt.After(metadata.AccessExpiresAt) {
		return ErrInvalidTokenCredential
	}
	expected := make(map[string]string, len(rotations))
	for _, rotation := range rotations {
		if strings.TrimSpace(rotation.ConnectionID) == "" || strings.TrimSpace(rotation.ShopID) == "" ||
			len(rotation.AccessTokenCipher) == 0 || len(rotation.AccessTokenNonce) == 0 ||
			len(rotation.RefreshTokenCipher) == 0 || len(rotation.RefreshTokenNonce) == 0 || rotation.EncryptionKeyVersion <= 0 {
			return ErrInvalidTokenCredential
		}
		if _, exists := expected[rotation.ConnectionID]; exists {
			return ErrInvalidTokenCredential
		}
		expected[rotation.ConnectionID] = rotation.ShopID
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx,
		`SELECT id::text, shop_id
		   FROM shop_connections
		  WHERE tenant_id = $1::uuid AND open_id = $2 AND disabled_at IS NULL
		  ORDER BY id
		  FOR UPDATE`, strings.TrimSpace(tenantID), strings.TrimSpace(openID))
	if err != nil {
		return err
	}
	actual := make(map[string]string, len(rotations))
	for rows.Next() {
		var connectionID, shopID string
		if err := rows.Scan(&connectionID, &shopID); err != nil {
			_ = rows.Close()
			return err
		}
		actual[connectionID] = shopID
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(actual) != len(expected) {
		return ErrInvalidTokenCredential
	}
	for connectionID, shopID := range expected {
		if actual[connectionID] != shopID {
			return ErrInvalidTokenCredential
		}
	}
	scopesJSON, err := json.Marshal(metadata.GrantedScopes)
	if err != nil {
		return err
	}
	for _, rotation := range rotations {
		result, err := tx.ExecContext(ctx,
			`UPDATE shop_connections
			    SET access_token_cipher = $5,
			        access_token_nonce = $6,
			        refresh_token_cipher = $7,
			        refresh_token_nonce = $8,
			        encryption_key_version = $9,
			        granted_scopes = $10::jsonb,
			        seller_name = COALESCE(NULLIF($11, ''), seller_name),
			        seller_base_region = COALESCE(NULLIF($12, ''), seller_base_region),
			        access_expires_at = $13,
			        refresh_expires_at = $14,
			        last_refreshed_at = $15,
			        last_error_code = '',
			        updated_at = $15
			  WHERE tenant_id = $1::uuid AND open_id = $2 AND id = $3::uuid AND shop_id = $4 AND disabled_at IS NULL`,
			strings.TrimSpace(tenantID), strings.TrimSpace(openID), rotation.ConnectionID, rotation.ShopID,
			rotation.AccessTokenCipher, rotation.AccessTokenNonce, rotation.RefreshTokenCipher, rotation.RefreshTokenNonce,
			rotation.EncryptionKeyVersion, scopesJSON, strings.TrimSpace(metadata.SellerName), strings.TrimSpace(metadata.SellerBaseRegion),
			metadata.AccessExpiresAt, metadata.RefreshExpiresAt, metadata.RefreshedAt,
		)
		if err != nil {
			return err
		}
		updated, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if updated != 1 {
			return ErrInvalidTokenCredential
		}
	}
	return tx.Commit()
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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
	tenantID := strings.TrimSpace(connections[0].TenantID)
	openID := strings.TrimSpace(connections[0].OpenID)
	for _, connection := range connections {
		if err := validateEncryptedConnection(connection); err != nil {
			return err
		}
		if strings.TrimSpace(connection.TenantID) != tenantID || strings.TrimSpace(connection.OpenID) != openID {
			return ErrInvalidConnection
		}
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	lockKey := tenantID + "\x00" + openID
	var locked bool
	if err := tx.QueryRowContext(ctx, `SELECT TRUE FROM pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey).Scan(&locked); err != nil || !locked {
		if err != nil {
			return err
		}
		return errors.New("TikTok Shop authorization lock was not acquired")
	}
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
		   (tenant_id, shop_id, shop_cipher, shop_name, shop_region, seller_type, shop_code,
		    open_id, seller_name, seller_base_region, granted_scopes,
		    access_token_cipher, access_token_nonce, refresh_token_cipher, refresh_token_nonce,
		    encryption_key_version, access_expires_at, refresh_expires_at)
		 VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12, $13, $14, $15, $16, $17, $18)
		 ON CONFLICT (shop_id) DO UPDATE
		    SET shop_cipher = EXCLUDED.shop_cipher,
		        shop_name = EXCLUDED.shop_name,
		        shop_region = EXCLUDED.shop_region,
		        seller_type = EXCLUDED.seller_type,
		        shop_code = EXCLUDED.shop_code,
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
		connection.TenantID, connection.ShopID, connection.ShopCipher,
		connection.ShopName, connection.ShopRegion, connection.SellerType, connection.ShopCode,
		connection.OpenID, connection.SellerName, connection.SellerBaseRegion, scopes,
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
