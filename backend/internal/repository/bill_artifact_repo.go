package repository

import (
	"database/sql"
	"encoding/json"

	"nexflow/internal/models"
)

type BillArtifactRepo struct {
	db *sql.DB
}

func NewBillArtifactRepo(db *sql.DB) *BillArtifactRepo {
	return &BillArtifactRepo{db: db}
}

func (r *BillArtifactRepo) Insert(a *models.BillArtifact) error {
	// pq sends []byte(nil) as the empty bytea string "", which a JSONB
	// column rejects with "invalid input syntax for type json". Pass an
	// untyped nil interface so the driver emits SQL NULL instead.
	var metaArg interface{}
	if len(a.SourceMeta) > 0 {
		metaArg = []byte(a.SourceMeta)
	}
	return r.db.QueryRow(
		`INSERT INTO bill_artifacts
		   (bill_id, kind, filename, content_type, size_bytes, sha256, storage_path, source_meta)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		 RETURNING id, created_at`,
		a.BillID, a.Kind, a.Filename, a.ContentType,
		a.SizeBytes, a.SHA256, a.StoragePath, metaArg,
	).Scan(&a.ID, &a.CreatedAt)
}

func (r *BillArtifactRepo) ListByBill(billID string) ([]models.BillArtifact, error) {
	rows, err := r.db.Query(
		`SELECT id, bill_id, kind, filename, content_type, size_bytes, sha256, storage_path, source_meta, created_at
		 FROM bill_artifacts WHERE bill_id = $1 ORDER BY created_at`,
		billID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.BillArtifact
	for rows.Next() {
		var a models.BillArtifact
		var meta sql.NullString
		var ct, sha sql.NullString
		if err := rows.Scan(
			&a.ID, &a.BillID, &a.Kind, &a.Filename, &ct, &a.SizeBytes,
			&sha, &a.StoragePath, &meta, &a.CreatedAt,
		); err != nil {
			return nil, err
		}
		if ct.Valid {
			a.ContentType = ct.String
		}
		if sha.Valid {
			a.SHA256 = sha.String
		}
		if meta.Valid && meta.String != "" {
			a.SourceMeta = json.RawMessage(meta.String)
		}
		out = append(out, a)
	}
	return out, nil
}

func (r *BillArtifactRepo) GetOne(id string) (*models.BillArtifact, error) {
	var a models.BillArtifact
	var meta sql.NullString
	var ct, sha sql.NullString
	err := r.db.QueryRow(
		`SELECT id, bill_id, kind, filename, content_type, size_bytes, sha256, storage_path, source_meta, created_at
		 FROM bill_artifacts WHERE id = $1`,
		id,
	).Scan(
		&a.ID, &a.BillID, &a.Kind, &a.Filename, &ct, &a.SizeBytes,
		&sha, &a.StoragePath, &meta, &a.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if ct.Valid {
		a.ContentType = ct.String
	}
	if sha.Valid {
		a.SHA256 = sha.String
	}
	if meta.Valid && meta.String != "" {
		a.SourceMeta = json.RawMessage(meta.String)
	}
	return &a, nil
}

func (r *BillArtifactRepo) Delete(id string) error {
	_, err := r.db.Exec(`DELETE FROM bill_artifacts WHERE id = $1`, id)
	return err
}
