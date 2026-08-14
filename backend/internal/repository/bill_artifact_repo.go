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

func (r *BillArtifactRepo) InsertForImportRun(a *models.BillArtifact) error {
	var metaArg interface{}
	if len(a.SourceMeta) > 0 {
		metaArg = []byte(a.SourceMeta)
	}
	return r.db.QueryRow(
		`INSERT INTO import_run_artifacts
		   (import_run_id, kind, filename, content_type, size_bytes, sha256, storage_path, source_meta)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		 ON CONFLICT (import_run_id) DO NOTHING
		 RETURNING id, created_at`,
		a.ImportRunID, a.Kind, a.Filename, a.ContentType, a.SizeBytes, a.SHA256, a.StoragePath, metaArg,
	).Scan(&a.ID, &a.CreatedAt)
}

func (r *BillArtifactRepo) GetForImportRun(importRunID string) (*models.BillArtifact, error) {
	var id string
	err := r.db.QueryRow(`SELECT id::text FROM import_run_artifacts WHERE import_run_id=$1`, importRunID).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r.GetOneForImportRun(id, importRunID)
}

func (r *BillArtifactRepo) ListByBill(billID string) ([]models.BillArtifact, error) {
	rows, err := r.db.Query(
		`SELECT id, bill_id, '' AS import_run_id, kind, filename, content_type, size_bytes, sha256, storage_path, source_meta, created_at
		 FROM bill_artifacts WHERE bill_id = $1
		 UNION ALL
		 SELECT ira.id, $1::uuid AS bill_id, ira.import_run_id::text, ira.kind, ira.filename,
		        ira.content_type, ira.size_bytes, ira.sha256, ira.storage_path, ira.source_meta, ira.created_at
		 FROM import_run_artifacts ira
		 JOIN bills b ON b.id=$1
		 WHERE ira.import_run_id::text=COALESCE(b.raw_data->>'import_run_id','')
		 ORDER BY created_at`,
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
			&a.ID, &a.BillID, &a.ImportRunID, &a.Kind, &a.Filename, &ct, &a.SizeBytes,
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

func (r *BillArtifactRepo) GetOneForBill(id, billID string) (*models.BillArtifact, error) {
	var a models.BillArtifact
	var meta sql.NullString
	var ct, sha sql.NullString
	err := r.db.QueryRow(
		`SELECT id, bill_id, import_run_id, kind, filename, content_type, size_bytes, sha256, storage_path, source_meta, created_at
		 FROM (
		   SELECT ba.id, ba.bill_id, ''::text AS import_run_id, ba.kind, ba.filename, ba.content_type,
		          ba.size_bytes, ba.sha256, ba.storage_path, ba.source_meta, ba.created_at
		   FROM bill_artifacts ba WHERE ba.id=$1 AND ba.bill_id=$2
		   UNION ALL
		   SELECT ira.id, b.id AS bill_id, ira.import_run_id::text, ira.kind, ira.filename, ira.content_type,
		          ira.size_bytes, ira.sha256, ira.storage_path, ira.source_meta, ira.created_at
		   FROM import_run_artifacts ira
		   JOIN bills b ON COALESCE(b.raw_data->>'import_run_id','')=ira.import_run_id::text
		   WHERE ira.id=$1 AND b.id=$2
		 ) owned LIMIT 1`, id, billID,
	).Scan(&a.ID, &a.BillID, &a.ImportRunID, &a.Kind, &a.Filename, &ct, &a.SizeBytes,
		&sha, &a.StoragePath, &meta, &a.CreatedAt)
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

func (r *BillArtifactRepo) GetOneForImportRun(id, importRunID string) (*models.BillArtifact, error) {
	var a models.BillArtifact
	var meta sql.NullString
	var ct, sha sql.NullString
	err := r.db.QueryRow(`SELECT id, import_run_id::text, kind, filename, content_type,
		size_bytes, sha256, storage_path, source_meta, created_at
		FROM import_run_artifacts WHERE id=$1 AND import_run_id=$2`, id, importRunID).
		Scan(&a.ID, &a.ImportRunID, &a.Kind, &a.Filename, &ct, &a.SizeBytes,
			&sha, &a.StoragePath, &meta, &a.CreatedAt)
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
