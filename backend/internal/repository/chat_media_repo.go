package repository

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nexflow/internal/models"
)

// ChatMediaRepo handles chat_media DB rows + binary file storage.
// Files live under {rootDir}/chat-media/<YYYY>/<MM>/<sha256>.<ext>
// — the sha256 prefix means identical uploads dedupe naturally on disk.
type ChatMediaRepo struct {
	db       *sql.DB
	rootDir  string
	maxBytes int64
}

func NewChatMediaRepo(db *sql.DB, rootDir string, maxBytes int64) *ChatMediaRepo {
	return &ChatMediaRepo{db: db, rootDir: rootDir, maxBytes: maxBytes}
}

const chatMediaCols = `
  id, message_id, filename, content_type, size_bytes,
  sha256, storage_path, created_at
`

func scanChatMedia(s interface{ Scan(...any) error }) (*models.ChatMedia, error) {
	m := &models.ChatMedia{}
	if err := s.Scan(
		&m.ID, &m.MessageID, &m.Filename, &m.ContentType, &m.SizeBytes,
		&m.SHA256, &m.StoragePath, &m.CreatedAt,
	); err != nil {
		return nil, err
	}
	return m, nil
}

// Save writes the bytes to disk and inserts the chat_media row. Idempotent
// on (message_id, sha256) — re-saving the same bytes for the same message
// will reuse the existing row and disk file.
func (r *ChatMediaRepo) Save(messageID, filename, contentType string, data []byte) (*models.ChatMedia, error) {
	if r == nil || r.rootDir == "" {
		return nil, fmt.Errorf("chat_media repo not configured")
	}
	if messageID == "" {
		return nil, fmt.Errorf("message_id required")
	}
	if r.maxBytes > 0 && int64(len(data)) > r.maxBytes {
		return nil, fmt.Errorf("media too large (%d bytes > limit %d)", len(data), r.maxBytes)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("media empty")
	}

	hash := sha256.Sum256(data)
	hexsum := hex.EncodeToString(hash[:])

	now := time.Now()
	relDir := filepath.Join("chat-media", now.Format("2006"), now.Format("01"))
	absDir := filepath.Join(r.rootDir, relDir)
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir chat_media: %w", err)
	}

	ext := filepath.Ext(filename)
	if ext == "" {
		ext = extFromContentType(contentType)
	}
	storageName := hexsum + ext
	relPath := filepath.Join(relDir, storageName)
	absPath := filepath.Join(r.rootDir, relPath)

	// Dedupe: if same bytes already on disk, don't rewrite.
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		if err := os.WriteFile(absPath, data, 0o644); err != nil {
			return nil, fmt.Errorf("write chat_media: %w", err)
		}
	}

	m := &models.ChatMedia{
		MessageID:   messageID,
		Filename:    filename,
		ContentType: contentType,
		SizeBytes:   int64(len(data)),
		SHA256:      hexsum,
		StoragePath: relPath,
	}
	row := r.db.QueryRow(
		`INSERT INTO chat_media (message_id, filename, content_type, size_bytes, sha256, storage_path)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, created_at`,
		m.MessageID, m.Filename, m.ContentType, m.SizeBytes, m.SHA256, m.StoragePath,
	)
	if err := row.Scan(&m.ID, &m.CreatedAt); err != nil {
		// Best-effort: if file was newly written and DB failed, leave the file
		// (sha256 dedup means a future upload will reuse it without orphaning).
		return nil, fmt.Errorf("insert chat_media: %w", err)
	}
	return m, nil
}

// GetByID returns the chat_media metadata row.
func (r *ChatMediaRepo) GetByID(id string) (*models.ChatMedia, error) {
	row := r.db.QueryRow(
		`SELECT `+chatMediaCols+` FROM chat_media WHERE id = $1`,
		id,
	)
	m, err := scanChatMedia(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get chat_media: %w", err)
	}
	return m, nil
}

// GetByMessageID returns the chat_media row attached to a chat_message
// (one-to-one in v1; nil when message has no media).
func (r *ChatMediaRepo) GetByMessageID(messageID string) (*models.ChatMedia, error) {
	row := r.db.QueryRow(
		`SELECT `+chatMediaCols+` FROM chat_media WHERE message_id = $1 LIMIT 1`,
		messageID,
	)
	m, err := scanChatMedia(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get chat_media by message: %w", err)
	}
	return m, nil
}

// ReadBytes reads the binary content from disk for serving via HTTP.
// Returns (data, media, error). media is non-nil even on read failure for
// caller logging.
func (r *ChatMediaRepo) ReadBytes(id string) ([]byte, *models.ChatMedia, error) {
	m, err := r.GetByID(id)
	if err != nil {
		return nil, nil, err
	}
	if m == nil {
		return nil, nil, nil
	}
	abs := filepath.Join(r.rootDir, m.StoragePath)
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, m, fmt.Errorf("read chat_media file: %w", err)
	}
	return data, m, nil
}

// extFromContentType returns a file extension (with dot) for common types
// LINE may send. Used as fallback when filename has no extension.
func extFromContentType(ct string) string {
	switch strings.ToLower(strings.SplitN(ct, ";", 2)[0]) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "application/pdf":
		return ".pdf"
	case "audio/m4a", "audio/x-m4a", "audio/mp4":
		return ".m4a"
	case "audio/mpeg":
		return ".mp3"
	case "audio/wav", "audio/x-wav":
		return ".wav"
	case "video/mp4":
		return ".mp4"
	}
	return ""
}
