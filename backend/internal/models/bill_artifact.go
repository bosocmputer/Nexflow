package models

import (
	"encoding/json"
	"time"
)

// BillArtifact is the metadata record for an original source file attached
// to a bill (PDF, email HTML, envelope JSON, etc.). The actual binary lives
// on disk under ArtifactsDir/{StoragePath}.
type BillArtifact struct {
	ID          string          `json:"id"`
	BillID      string          `json:"bill_id"`
	Kind        string          `json:"kind"`
	Filename    string          `json:"filename"`
	ContentType string          `json:"content_type,omitempty"`
	SizeBytes   int64           `json:"size_bytes"`
	SHA256      string          `json:"sha256,omitempty"`
	StoragePath string          `json:"-"` // never expose path to clients
	SourceMeta  json.RawMessage `json:"source_meta,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}
