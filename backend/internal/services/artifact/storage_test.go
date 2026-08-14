package artifact

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"go.uber.org/zap"

	"nexflow/internal/repository"
)

func TestSaveForImportRunKeepsOneSourceFile(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	runID := "8e0f04a0-8117-4ddc-b553-467851a9ea01"
	artifactID := "0cdf6054-0826-43f1-b295-eb2854ee4e7f"
	createdAt := time.Now()
	repo := repository.NewBillArtifactRepo(db)
	service := New(t.TempDir(), 1024, repo, zap.NewNop())

	mock.ExpectQuery("INSERT INTO import_run_artifacts").
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(artifactID, createdAt))
	first, err := service.SaveForImportRun(runID, "tiktok_csv", "orders.csv", "text/csv", []byte("source"), nil)
	if err != nil {
		t.Fatalf("first SaveForImportRun: %v", err)
	}

	mock.ExpectQuery("INSERT INTO import_run_artifacts").
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}))
	mock.ExpectQuery("SELECT id::text FROM import_run_artifacts").
		WithArgs(runID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(artifactID))
	mock.ExpectQuery("SELECT id, import_run_id::text").
		WithArgs(artifactID, runID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "import_run_id", "kind", "filename", "content_type", "size_bytes", "sha256", "storage_path", "source_meta", "created_at",
		}).AddRow(artifactID, runID, "tiktok_csv", "orders.csv", "text/csv", 6, first.SHA256, first.StoragePath, nil, createdAt))
	second, err := service.SaveForImportRun(runID, "tiktok_csv", "orders.csv", "text/csv", []byte("source"), nil)
	if err != nil {
		t.Fatalf("second SaveForImportRun: %v", err)
	}
	if second.ID != first.ID || second.StoragePath != first.StoragePath {
		t.Fatalf("duplicate save returned a different artifact: first=%#v second=%#v", first, second)
	}

	files := 0
	err = filepath.WalkDir(service.rootDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr == nil && !entry.IsDir() {
			files++
		}
		return walkErr
	})
	if err != nil {
		t.Fatal(err)
	}
	if files != 1 {
		t.Fatalf("source files = %d, want 1", files)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}
