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
