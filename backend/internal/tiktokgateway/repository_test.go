package tiktokgateway

import (
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestRepositorySyncTenantsUpsertsWithoutDeletingExistingRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO tenants (slug, public_base_url, backend_url, enabled)
			 VALUES ($1, $2, $3, TRUE)
			 ON CONFLICT (slug) DO UPDATE
			    SET public_base_url = EXCLUDED.public_base_url,
			        backend_url = EXCLUDED.backend_url,
			        updated_at = NOW()`)).
		WithArgs("aoy", "https://nexflow-aoy.nextstep-soft.com", "http://172.17.0.1:8111").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	repo := NewRepository(db)
	err = repo.SyncTenants(t.Context(), []TenantDefinition{{Slug: "aoy", PublicBaseURL: "https://nexflow-aoy.nextstep-soft.com", BackendURL: "http://172.17.0.1:8111"}})
	if err != nil {
		t.Fatalf("SyncTenants() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryConsumesOAuthStateOnlyOnce(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	expiresAt := time.Now().Add(time.Minute)
	mock.ExpectQuery("UPDATE oauth_states").WithArgs("state-hash").
		WillReturnRows(sqlmock.NewRows([]string{"state_hash", "tenant_id", "user_id", "return_url", "nonce", "expires_at"}).
			AddRow("state-hash", "11111111-1111-1111-1111-111111111111", "user-1", "https://nexflow-aoy.nextstep-soft.com/settings/tiktok-shop", "nonce-1", expiresAt))

	record, err := NewRepository(db).ConsumeOAuthState(t.Context(), "state-hash")
	if err != nil || record.Nonce != "nonce-1" {
		t.Fatalf("ConsumeOAuthState() = %+v, %v", record, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryRejectsCrossTenantShopOwnership(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectExec("INSERT INTO shop_connections").WillReturnResult(sqlmock.NewResult(0, 0))

	err = NewRepository(db).UpsertConnection(t.Context(), EncryptedConnection{
		TenantID: "11111111-1111-1111-1111-111111111111",
		ShopID:   "749000000000000001", ShopCipher: "cipher", OpenID: "open-id", SellerName: "AOY", SellerBaseRegion: "TH",
		AccessTokenCipher: []byte("a"), AccessTokenNonce: []byte("b"), RefreshTokenCipher: []byte("c"), RefreshTokenNonce: []byte("d"),
		EncryptionKeyVersion: 1, AccessExpiresAt: time.Now(), RefreshExpiresAt: time.Now().Add(time.Hour), GrantedScopes: []string{"seller.authorization.info"},
	})
	if !errors.Is(err, ErrShopAlreadyOwned) {
		t.Fatalf("UpsertConnection() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryRejectsInvalidConnectionBeforeDatabase(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	err = NewRepository(db).UpsertConnection(t.Context(), EncryptedConnection{
		TenantID: "11111111-1111-1111-1111-111111111111",
		ShopID:   "",
	})
	if !errors.Is(err, ErrInvalidConnection) {
		t.Fatalf("UpsertConnection() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryUpsertsMultipleConnectionsAtomically(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT TRUE FROM pg_advisory_xact_lock").
		WithArgs("11111111-1111-1111-1111-111111111111\x00open-id").
		WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(true))
	mock.ExpectExec("INSERT INTO shop_connections").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO shop_connections").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	first := validEncryptedConnection("749000000000000001")
	second := validEncryptedConnection("749000000000000002")
	if err := NewRepository(db).UpsertConnections(t.Context(), []EncryptedConnection{first, second}); err != nil {
		t.Fatalf("UpsertConnections() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryRollsBackAllConnectionsWhenOneShopBelongsToAnotherTenant(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT TRUE FROM pg_advisory_xact_lock").
		WithArgs("11111111-1111-1111-1111-111111111111\x00open-id").
		WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(true))
	mock.ExpectExec("INSERT INTO shop_connections").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO shop_connections").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	first := validEncryptedConnection("749000000000000001")
	second := validEncryptedConnection("749000000000000002")
	err = NewRepository(db).UpsertConnections(t.Context(), []EncryptedConnection{first, second})
	if !errors.Is(err, ErrShopAlreadyOwned) {
		t.Fatalf("UpsertConnections() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryRejectsReplayedInternalNonce(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectExec("INSERT INTO api_request_logs").WithArgs("aoy", "nonce-1").WillReturnResult(sqlmock.NewResult(0, 0))

	err = NewRepository(db).Consume(t.Context(), "aoy", "nonce-1", time.Now().Add(time.Minute))
	if !errors.Is(err, ErrNonceAlreadyUsed) {
		t.Fatalf("Consume() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryTenantBySlugReturnsNoRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT id::text, slug").WithArgs("missing").WillReturnError(sql.ErrNoRows)
	_, err = NewRepository(db).TenantBySlug(t.Context(), "missing")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("TenantBySlug() error = %v", err)
	}
}

func TestRepositoryListsOnlyTenantConnectionMetadata(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	accessExpiresAt := time.Now().Add(time.Hour)
	refreshExpiresAt := time.Now().Add(30 * 24 * time.Hour)
	connectedAt := time.Now().Add(-time.Hour)
	updatedAt := time.Now()
	mock.ExpectQuery("SELECT id::text, shop_id, shop_name").
		WithArgs("11111111-1111-1111-1111-111111111111").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "shop_id", "shop_name", "shop_region", "seller_type", "shop_code", "granted_scopes",
			"access_expires_at", "refresh_expires_at", "disabled_at", "connected_at", "updated_at",
		}).AddRow(
			"22222222-2222-2222-2222-222222222222", "7000714532876273420", "AOY Main", "TH", "LOCAL", "THAOY1", []byte(`["seller.authorization.info","seller.order.info"]`),
			accessExpiresAt, refreshExpiresAt, nil, connectedAt, updatedAt,
		))

	connections, err := NewRepository(db).ListConnectionsByTenantID(t.Context(), "11111111-1111-1111-1111-111111111111")
	if err != nil {
		t.Fatalf("ListConnectionsByTenantID() error = %v", err)
	}
	if len(connections) != 1 || connections[0].ShopID != "7000714532876273420" || connections[0].ShopName != "AOY Main" || connections[0].DisabledAt.Valid {
		t.Fatalf("connections = %+v", connections)
	}
	if len(connections[0].GrantedScopes) != 2 || connections[0].GrantedScopes[1] != "seller.order.info" {
		t.Fatalf("scopes = %#v", connections[0].GrantedScopes)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryLoadsAllSiblingTokensForOneSellerAuthorization(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	accessExpiresAt := time.Now().Add(time.Hour)
	refreshExpiresAt := time.Now().Add(30 * 24 * time.Hour)
	mock.ExpectQuery("WITH target_authorization").
		WithArgs("aoy", "shop-2").
		WillReturnRows(sqlmock.NewRows([]string{
			"tenant_id", "tenant_slug", "open_id", "granted_scopes", "access_expires_at", "refresh_expires_at",
			"id", "shop_id", "shop_cipher", "access_token_cipher", "access_token_nonce", "refresh_token_cipher", "refresh_token_nonce", "encryption_key_version",
		}).
			AddRow("11111111-1111-1111-1111-111111111111", "aoy", "seller-open-id", []byte(`["seller.authorization.info","seller.order.info"]`), accessExpiresAt, refreshExpiresAt,
				"connection-1", "shop-1", "cipher-1", []byte("a1"), []byte("n1"), []byte("r1"), []byte("rn1"), 1).
			AddRow("11111111-1111-1111-1111-111111111111", "aoy", "seller-open-id", []byte(`["seller.authorization.info","seller.order.info"]`), accessExpiresAt, refreshExpiresAt,
				"connection-2", "shop-2", "cipher-2", []byte("a2"), []byte("n2"), []byte("r2"), []byte("rn2"), 1))

	group, err := NewRepository(db).AuthorizationTokenGroupByShop(t.Context(), "AOY", "shop-2")
	if err != nil {
		t.Fatalf("AuthorizationTokenGroupByShop() error = %v", err)
	}
	if group.TenantSlug != "aoy" || group.OpenID != "seller-open-id" || len(group.Connections) != 2 || group.Connections[1].ShopCipher != "cipher-2" {
		t.Fatalf("group = %+v", group)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryAuthorizationRefreshLockUsesDatabaseAdvisoryLock(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	lockKey := "11111111-1111-1111-1111-111111111111\x00seller-open-id"
	mock.ExpectQuery("SELECT TRUE FROM pg_advisory_lock").WithArgs(lockKey).
		WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(true))
	mock.ExpectQuery("SELECT pg_advisory_unlock").WithArgs(lockKey).
		WillReturnRows(sqlmock.NewRows([]string{"unlocked"}).AddRow(true))

	unlock, err := NewRepository(db).LockAuthorizationRefresh(t.Context(), "11111111-1111-1111-1111-111111111111", "seller-open-id")
	if err != nil {
		t.Fatalf("LockAuthorizationRefresh() error = %v", err)
	}
	unlock()
	unlock()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryRotatesEverySiblingTokenInOneTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tenantID := "11111111-1111-1111-1111-111111111111"
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id::text, shop_id").WithArgs(tenantID, "seller-open-id").
		WillReturnRows(sqlmock.NewRows([]string{"id", "shop_id"}).AddRow("connection-1", "shop-1").AddRow("connection-2", "shop-2"))
	mock.ExpectExec("UPDATE shop_connections").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE shop_connections").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	rotations := []RotatedConnectionTokens{
		{ConnectionID: "connection-1", ShopID: "shop-1", AccessTokenCipher: []byte("a1"), AccessTokenNonce: []byte("n1"), RefreshTokenCipher: []byte("r1"), RefreshTokenNonce: []byte("rn1"), EncryptionKeyVersion: 1},
		{ConnectionID: "connection-2", ShopID: "shop-2", AccessTokenCipher: []byte("a2"), AccessTokenNonce: []byte("n2"), RefreshTokenCipher: []byte("r2"), RefreshTokenNonce: []byte("rn2"), EncryptionKeyVersion: 1},
	}
	metadata := RefreshedAuthorizationMetadata{
		SellerName: "AOY", SellerBaseRegion: "TH", GrantedScopes: []string{"seller.authorization.info", "seller.order.info"},
		AccessExpiresAt: time.Now().Add(time.Hour), RefreshExpiresAt: time.Now().Add(30 * 24 * time.Hour), RefreshedAt: time.Now(),
	}
	if err := NewRepository(db).RotateAuthorizationTokens(t.Context(), tenantID, "seller-open-id", rotations, metadata); err != nil {
		t.Fatalf("RotateAuthorizationTokens() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryRollsBackRotationWhenSiblingSetChanged(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tenantID := "11111111-1111-1111-1111-111111111111"
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id::text, shop_id").WithArgs(tenantID, "seller-open-id").
		WillReturnRows(sqlmock.NewRows([]string{"id", "shop_id"}).AddRow("connection-1", "shop-1").AddRow("connection-2", "shop-2"))
	mock.ExpectRollback()

	err = NewRepository(db).RotateAuthorizationTokens(t.Context(), tenantID, "seller-open-id", []RotatedConnectionTokens{
		{ConnectionID: "connection-1", ShopID: "shop-1", AccessTokenCipher: []byte("a1"), AccessTokenNonce: []byte("n1"), RefreshTokenCipher: []byte("r1"), RefreshTokenNonce: []byte("rn1"), EncryptionKeyVersion: 1},
	}, RefreshedAuthorizationMetadata{
		GrantedScopes: []string{"seller.authorization.info", "seller.order.info"}, AccessExpiresAt: time.Now().Add(time.Hour),
		RefreshExpiresAt: time.Now().Add(30 * 24 * time.Hour), RefreshedAt: time.Now(),
	})
	if !errors.Is(err, ErrInvalidTokenCredential) {
		t.Fatalf("RotateAuthorizationTokens() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func validEncryptedConnection(shopID string) EncryptedConnection {
	return EncryptedConnection{
		TenantID: "11111111-1111-1111-1111-111111111111",
		ShopID:   shopID, ShopCipher: "cipher-" + shopID, OpenID: "open-id", SellerName: "AOY", SellerBaseRegion: "TH",
		AccessTokenCipher: []byte("a"), AccessTokenNonce: []byte("b"), RefreshTokenCipher: []byte("c"), RefreshTokenNonce: []byte("d"),
		EncryptionKeyVersion: 1, AccessExpiresAt: time.Now(), RefreshExpiresAt: time.Now().Add(time.Hour), GrantedScopes: []string{"seller.authorization.info"},
	}
}
