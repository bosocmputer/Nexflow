package marketplacestock

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestInsertAuditCastsTargetIDToUUID(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer database.Close()

	mock.ExpectBegin()
	tx, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	mock.ExpectExec(`INSERT INTO audit_logs\(action,target_id,user_id,source,level,detail\)\s+VALUES\(\$1,NULLIF\(\$2,''\)::uuid,NULLIF\(\$3,''\)::uuid,'marketplace_stock','info',\$4::jsonb\)`).
		WithArgs("marketplace_stock_pool_created", "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	if err := insertAudit(context.Background(), tx, "marketplace_stock_pool_created", "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", map[string]any{"member_count": 1}); err != nil {
		t.Fatalf("insertAudit: %v", err)
	}
	mock.ExpectCommit()
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}
