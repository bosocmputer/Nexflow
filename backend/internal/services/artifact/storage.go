// Package artifact stores the original source file behind each bill
// (PDF, email HTML, envelope JSON, etc.) on a mounted volume, with DB
// metadata kept in bill_artifacts.
package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/repository"
)

type Service struct {
	rootDir   string
	maxBytes  int64
	repo      *repository.BillArtifactRepo
	logger    *zap.Logger
}

func New(rootDir string, maxBytes int64, repo *repository.BillArtifactRepo, logger *zap.Logger) *Service {
	return &Service{
		rootDir:   rootDir,
		maxBytes:  maxBytes,
		repo:      repo,
		logger:    logger,
	}
}

// SafeFilename pattern keeps only ASCII-safe chars; everything else collapsed to '_'.
// We DO NOT preserve the original Thai/utf-8 filename on disk because some
// filesystems on the storage path have unpredictable handling. The original
// name is preserved in DB column `filename` for display.
var safeRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func sanitize(name string) string {
	if name == "" {
		return "file"
	}
	cleaned := safeRe.ReplaceAllString(name, "_")
	cleaned = strings.Trim(cleaned, "._-")
	if cleaned == "" {
		cleaned = "file"
	}
	if len(cleaned) > 80 {
		cleaned = cleaned[:80]
	}
	return cleaned
}

// Save writes data to disk under {root}/YYYY/MM/{bill_id}/{seq}-{filename}
// and inserts the metadata row. seq is derived from the existing artifact
// count for this bill so multiple artifacts per bill don't collide.
func (s *Service) Save(
	billID, kind, filename, contentType string,
	data []byte,
	meta map[string]interface{},
) (*models.BillArtifact, error) {
	if s == nil {
		return nil, fmt.Errorf("artifact service not configured")
	}
	if billID == "" {
		return nil, fmt.Errorf("bill_id required")
	}
	if int64(len(data)) > s.maxBytes {
		return nil, fmt.Errorf("artifact too large (%d bytes > limit %d)", len(data), s.maxBytes)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("artifact empty")
	}

	// SHA256 for integrity
	hash := sha256.Sum256(data)
	hexsum := hex.EncodeToString(hash[:])

	// Path: YYYY/MM/<bill_id>/<unix_ms>-<safe_filename>
	now := time.Now()
	relDir := filepath.Join(now.Format("2006"), now.Format("01"), billID)
	absDir := filepath.Join(s.rootDir, relDir)
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir artifact dir: %w", err)
	}

	safe := sanitize(filename)
	storageName := fmt.Sprintf("%d-%s", now.UnixMilli(), safe)
	relPath := filepath.Join(relDir, storageName)
	absPath := filepath.Join(s.rootDir, relPath)

	if err := os.WriteFile(absPath, data, 0o644); err != nil {
		return nil, fmt.Errorf("write artifact: %w", err)
	}

	var metaJSON json.RawMessage
	if meta != nil {
		b, _ := json.Marshal(meta)
		metaJSON = b
	}

	a := &models.BillArtifact{
		BillID:      billID,
		Kind:        kind,
		Filename:    filename,
		ContentType: contentType,
		SizeBytes:   int64(len(data)),
		SHA256:      hexsum,
		StoragePath: relPath,
		SourceMeta:  metaJSON,
	}
	if err := s.repo.Insert(a); err != nil {
		// Best-effort cleanup if DB insert failed
		_ = os.Remove(absPath)
		return nil, fmt.Errorf("insert artifact row: %w", err)
	}

	if s.logger != nil {
		s.logger.Info("artifact saved",
			zap.String("bill_id", billID),
			zap.String("kind", kind),
			zap.String("path", relPath),
			zap.Int("size", len(data)),
		)
	}
	return a, nil
}

// Read returns (data, artifact, error). Verifies SHA256 if recorded.
func (s *Service) Read(artifactID string) ([]byte, *models.BillArtifact, error) {
	a, err := s.repo.GetOne(artifactID)
	if err != nil {
		return nil, nil, err
	}
	if a == nil {
		return nil, nil, nil
	}
	abs := filepath.Join(s.rootDir, a.StoragePath)
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, a, fmt.Errorf("read artifact: %w", err)
	}
	// Optional integrity check
	if a.SHA256 != "" {
		hash := sha256.Sum256(data)
		got := hex.EncodeToString(hash[:])
		if got != a.SHA256 {
			return nil, a, fmt.Errorf("sha256 mismatch — file may have been tampered with")
		}
	}
	return data, a, nil
}

// ListByBill is a thin pass-through for handlers.
func (s *Service) ListByBill(billID string) ([]models.BillArtifact, error) {
	return s.repo.ListByBill(billID)
}
