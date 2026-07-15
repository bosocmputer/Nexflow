package shopeegateway

import (
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
