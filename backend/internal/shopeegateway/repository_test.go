package shopeegateway

import (
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestRepositorySyncTenantsUpsertsRegistryWithoutDeletingExistingRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
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
	err = repo.SyncTenants(t.Context(), []TenantDefinition{{
		Slug:          "aoy",
		PublicBaseURL: "https://nexflow-aoy.nextstep-soft.com",
		BackendURL:    "http://172.17.0.1:8111",
	}})
	if err != nil {
		t.Fatalf("SyncTenants() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestRepositoryUpsertConnectionRejectsCrossTenantOwnership(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO shop_connections").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()
	repo := NewRepository(db)
	err = repo.UpsertConnection(t.Context(), EncryptedConnection{
		TenantID: "11111111-1111-1111-1111-111111111111", ShopID: 99,
		MerchantID: sql.NullInt64{}, Environment: "live",
		AccessTokenCipher: []byte("a"), AccessTokenNonce: []byte("b"),
		RefreshTokenCipher: []byte("c"), RefreshTokenNonce: []byte("d"),
		EncryptionKeyVersion: 1, AccessExpiresAt: time.Now(), RefreshExpiresAt: time.Now().Add(time.Hour),
	})
	if !errors.Is(err, ErrShopAlreadyOwned) {
		t.Fatalf("error=%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositorySyncTenantRoutesRejectsCrossTenantOwnership(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tenantID := "11111111-1111-1111-1111-111111111111"
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE shop_routes").WithArgs(tenantID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO shop_routes").WithArgs(int64(99), tenantID).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()
	repo := NewRepository(db)
	err = repo.SyncTenantRoutes(t.Context(), tenantID, []int64{99})
	if !errors.Is(err, ErrShopAlreadyOwned) {
		t.Fatalf("error=%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryAcceptPushUnknownShopDoesNotEnqueueDelivery(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT t.id::text").WithArgs(int64(99)).WillReturnError(sql.ErrNoRows)
	mock.ExpectExec("INSERT INTO push_events").
		WithArgs("", int64(99), "ORDER-1", 3, "READY_TO_SHIP", "dedupe", []byte(`{"shop_id":99}`), "unknown_shop").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	repo := NewRepository(db)
	result, err := repo.AcceptPushEvent(t.Context(), PushEventInput{
		ShopID: 99, OrderSN: "ORDER-1", PushCode: 3, EventStatus: "READY_TO_SHIP",
		DedupeKey: "dedupe", RawPayload: json.RawMessage(`{"shop_id":99}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Inserted || result.Tenant != nil {
		t.Fatalf("result=%+v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryConsumeRejectsReplayedNonce(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()
	mock.ExpectExec("INSERT INTO api_request_logs").
		WithArgs("demo", "nonce-1").
		WillReturnResult(sqlmock.NewResult(0, 0))

	repo := NewRepository(db)
	err = repo.Consume(t.Context(), "demo", "nonce-1", time.Now().Add(time.Minute))
	if !errors.Is(err, ErrNonceAlreadyUsed) {
		t.Fatalf("Consume() error = %v, want nonce already used", err)
	}
}
