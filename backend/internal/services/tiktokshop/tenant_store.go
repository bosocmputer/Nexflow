package tiktokshop

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	ErrTenantStoreNotConfigured = errors.New("TikTok Shop tenant connection store is not configured")
	ErrInvalidGatewayConnection = errors.New("invalid TikTok Shop gateway connection metadata")
	connectionIDPattern         = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89aAbB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
)

type TenantConnectionStore struct {
	database *sql.DB
}

func NewTenantConnectionStore(database *sql.DB) *TenantConnectionStore {
	return &TenantConnectionStore{database: database}
}

func (s *TenantConnectionStore) Sync(ctx context.Context, connections []GatewayConnection) error {
	if s == nil || s.database == nil {
		return ErrTenantStoreNotConfigured
	}
	prepared := make([]preparedGatewayConnection, len(connections))
	seenShops := make(map[string]struct{}, len(connections))
	for index, connection := range connections {
		item, err := prepareGatewayConnection(connection)
		if err != nil {
			return err
		}
		if _, exists := seenShops[item.ShopID]; exists {
			return ErrInvalidGatewayConnection
		}
		seenShops[item.ShopID] = struct{}{}
		prepared[index] = item
	}
	if len(prepared) == 0 {
		return nil
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback() }()
	for _, connection := range prepared {
		if _, err := transaction.ExecContext(ctx,
			`INSERT INTO tiktok_shop_connections
			   (gateway_connection_id, shop_id, shop_name, label, shop_region, seller_type, shop_code,
			    granted_scopes, access_expires_at, refresh_expires_at, disabled_at, connected_at, gateway_updated_at)
			 VALUES ($1::uuid, $2, $3, $3, $4, $5, $6, $7::jsonb, $8, $9, $10, $11, $12)
			 ON CONFLICT (shop_id) DO UPDATE
			    SET gateway_connection_id = EXCLUDED.gateway_connection_id,
			        shop_name = EXCLUDED.shop_name,
			        label = CASE
			          WHEN tiktok_shop_connections.label = '' OR tiktok_shop_connections.label = tiktok_shop_connections.shop_name
			          THEN EXCLUDED.shop_name ELSE tiktok_shop_connections.label END,
			        shop_region = EXCLUDED.shop_region,
			        seller_type = EXCLUDED.seller_type,
			        shop_code = EXCLUDED.shop_code,
			        granted_scopes = EXCLUDED.granted_scopes,
			        access_expires_at = EXCLUDED.access_expires_at,
			        refresh_expires_at = EXCLUDED.refresh_expires_at,
			        disabled_at = EXCLUDED.disabled_at,
			        connected_at = EXCLUDED.connected_at,
			        gateway_updated_at = EXCLUDED.gateway_updated_at,
			        last_gateway_sync_at = NOW(),
			        updated_at = NOW()`,
			connection.GatewayConnectionID, connection.ShopID, connection.ShopName, connection.ShopRegion,
			connection.SellerType, connection.ShopCode, connection.GrantedScopes,
			connection.AccessExpiresAt, connection.RefreshExpiresAt, connection.DisabledAt,
			connection.ConnectedAt, connection.UpdatedAt,
		); err != nil {
			return fmt.Errorf("sync TikTok Shop connection %s: %w", connection.ShopID, err)
		}
	}
	return transaction.Commit()
}

type preparedGatewayConnection struct {
	GatewayConnectionID string
	ShopID              string
	ShopName            string
	ShopRegion          string
	SellerType          string
	ShopCode            string
	GrantedScopes       []byte
	AccessExpiresAt     time.Time
	RefreshExpiresAt    time.Time
	DisabledAt          *time.Time
	ConnectedAt         time.Time
	UpdatedAt           time.Time
}

func prepareGatewayConnection(connection GatewayConnection) (preparedGatewayConnection, error) {
	connection.GatewayConnectionID = strings.TrimSpace(connection.GatewayConnectionID)
	connection.ShopID = strings.TrimSpace(connection.ShopID)
	if !connectionIDPattern.MatchString(connection.GatewayConnectionID) || connection.ShopID == "" || len(connection.GrantedScopes) == 0 {
		return preparedGatewayConnection{}, ErrInvalidGatewayConnection
	}
	accessExpiresAt, err := time.Parse(time.RFC3339, connection.AccessExpiresAt)
	if err != nil {
		return preparedGatewayConnection{}, ErrInvalidGatewayConnection
	}
	refreshExpiresAt, err := time.Parse(time.RFC3339, connection.RefreshExpiresAt)
	if err != nil || !refreshExpiresAt.After(accessExpiresAt) {
		return preparedGatewayConnection{}, ErrInvalidGatewayConnection
	}
	connectedAt, err := time.Parse(time.RFC3339, connection.ConnectedAt)
	if err != nil {
		return preparedGatewayConnection{}, ErrInvalidGatewayConnection
	}
	updatedAt, err := time.Parse(time.RFC3339, connection.UpdatedAt)
	if err != nil {
		return preparedGatewayConnection{}, ErrInvalidGatewayConnection
	}
	scopes, err := json.Marshal(connection.GrantedScopes)
	if err != nil {
		return preparedGatewayConnection{}, ErrInvalidGatewayConnection
	}
	var disabledAt *time.Time
	if connection.Disabled {
		value := updatedAt
		disabledAt = &value
	}
	return preparedGatewayConnection{
		GatewayConnectionID: connection.GatewayConnectionID, ShopID: connection.ShopID,
		ShopName: strings.TrimSpace(connection.ShopName), ShopRegion: strings.TrimSpace(connection.ShopRegion),
		SellerType: strings.TrimSpace(connection.SellerType), ShopCode: strings.TrimSpace(connection.ShopCode),
		GrantedScopes: scopes, AccessExpiresAt: accessExpiresAt, RefreshExpiresAt: refreshExpiresAt,
		DisabledAt: disabledAt, ConnectedAt: connectedAt, UpdatedAt: updatedAt,
	}, nil
}
