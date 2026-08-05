package repository

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/lib/pq"

	"nexflow/internal/marketplace"
	"nexflow/internal/models"
)

type MappingRepo struct {
	db *sql.DB
}

// FindByRawNames preloads verified exact-name mappings for an import batch.
func (r *MappingRepo) FindByRawNames(rawNames []string) (map[string]*models.Mapping, error) {
	normalizedNames := make([]string, 0, len(rawNames))
	seen := make(map[string]struct{}, len(rawNames))
	for _, rawName := range rawNames {
		key := marketplace.NormalizeKey(rawName, "")
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalizedNames = append(normalizedNames, key)
	}
	result := make(map[string]*models.Mapping, len(normalizedNames))
	if len(normalizedNames) == 0 {
		return result, nil
	}
	rows, err := r.db.Query(`
		SELECT DISTINCT ON (btrim(regexp_replace(replace(m.raw_name, chr(65279), ''), '\s+', ' ', 'g')))
		       m.id, m.raw_name, m.item_code, m.unit_code, m.confidence, m.source,
		       m.usage_count, m.last_used_at, m.learned_from_bill_id, m.created_by, m.created_at, m.updated_at
		FROM mappings m
		JOIN sml_catalog c ON c.item_code = m.item_code AND c.is_active = TRUE
		WHERE btrim(regexp_replace(replace(m.raw_name, chr(65279), ''), '\s+', ' ', 'g')) = ANY($1)
		  AND m.source IN ('manual', 'verified')
		ORDER BY btrim(regexp_replace(replace(m.raw_name, chr(65279), ''), '\s+', ' ', 'g')),
		         m.updated_at DESC, m.created_at DESC
	`, pq.Array(normalizedNames))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m models.Mapping
		if err := rows.Scan(
			&m.ID, &m.RawName, &m.ItemCode, &m.UnitCode, &m.Confidence,
			&m.Source, &m.UsageCount, &m.LastUsedAt, &m.LearnedFromBillID,
			&m.CreatedBy, &m.CreatedAt, &m.UpdatedAt,
		); err != nil {
			return nil, err
		}
		copy := m
		result[marketplace.NormalizeKey(m.RawName, "")] = &copy
	}
	return result, rows.Err()
}

func NewMappingRepo(db *sql.DB) *MappingRepo {
	return &MappingRepo{db: db}
}

func (r *MappingRepo) FindByRawName(rawName string) (*models.Mapping, error) {
	normalizedName := marketplace.NormalizeKey(rawName, "")
	if normalizedName == "" {
		return nil, nil
	}
	m := &models.Mapping{}
	err := r.db.QueryRow(
		`SELECT m.id, m.raw_name, m.item_code, m.unit_code, m.confidence, m.source,
		        m.usage_count, m.last_used_at, m.learned_from_bill_id, m.created_by, m.created_at, m.updated_at
		 FROM mappings m
		 JOIN sml_catalog c ON c.item_code = m.item_code AND c.is_active = TRUE
		 WHERE btrim(regexp_replace(replace(m.raw_name, chr(65279), ''), '\s+', ' ', 'g')) = $1
		   AND m.source IN ('manual', 'verified')
		 ORDER BY m.updated_at DESC, m.created_at DESC
		 LIMIT 1`, normalizedName,
	).Scan(
		&m.ID, &m.RawName, &m.ItemCode, &m.UnitCode, &m.Confidence,
		&m.Source, &m.UsageCount, &m.LastUsedAt, &m.LearnedFromBillID,
		&m.CreatedBy, &m.CreatedAt, &m.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("FindByRawName: %w", err)
	}
	return m, nil
}

func (r *MappingRepo) ListAll() ([]models.Mapping, error) {
	rows, err := r.db.Query(
		`SELECT m.id, m.raw_name, m.item_code, m.unit_code, m.confidence, m.source,
		        m.usage_count, m.last_used_at, m.created_by, m.created_at, m.updated_at,
		        COALESCE(c.item_name, ''), COALESCE(u.email, ''),
		        COALESCE(c.is_active, FALSE),
		        (SELECT COUNT(*)
		           FROM bill_items bi
		           JOIN bills b ON b.id = bi.bill_id
		          WHERE b.source IN ('shopee','lazada','tiktok')
		            AND b.bill_type = 'sale'
		            AND b.status IN ('pending','needs_review')
		            AND b.archived_at IS NULL
		            AND COALESCE(bi.source_sku, '') = ''
		            AND btrim(regexp_replace(replace(bi.raw_name, chr(65279), ''), '\s+', ' ', 'g')) =
		                btrim(regexp_replace(replace(m.raw_name, chr(65279), ''), '\s+', ' ', 'g')))
		 FROM mappings m
		 LEFT JOIN sml_catalog c ON c.item_code = m.item_code
		 LEFT JOIN users u ON u.id = m.created_by
		 ORDER BY m.usage_count DESC, m.raw_name`,
	)
	if err != nil {
		return nil, fmt.Errorf("ListAll: %w", err)
	}
	defer rows.Close()

	var mappings []models.Mapping
	for rows.Next() {
		var m models.Mapping
		if err := rows.Scan(
			&m.ID, &m.RawName, &m.ItemCode, &m.UnitCode, &m.Confidence,
			&m.Source, &m.UsageCount, &m.LastUsedAt, &m.CreatedBy, &m.CreatedAt,
			&m.UpdatedAt, &m.ItemName, &m.ConfirmedName, &m.ProductActive, &m.OpenItemCount,
		); err != nil {
			return nil, err
		}
		mappings = append(mappings, m)
	}
	return mappings, rows.Err()
}

func (r *MappingRepo) Create(rawName, itemCode, unitCode, createdBy string) (*models.Mapping, error) {
	rawName = marketplace.NormalizeKey(rawName, "")
	m := &models.Mapping{}
	err := r.db.QueryRow(
		`INSERT INTO mappings (raw_name, item_code, unit_code, source, created_by)
		 VALUES ($1, $2, $3, 'manual', $4)
		 RETURNING id, raw_name, item_code, unit_code, confidence, source, usage_count, created_at, updated_at`,
		rawName, itemCode, unitCode, createdBy,
	).Scan(&m.ID, &m.RawName, &m.ItemCode, &m.UnitCode, &m.Confidence, &m.Source, &m.UsageCount, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("Create mapping: %w", err)
	}
	return m, nil
}

func (r *MappingRepo) Upsert(rawName, itemCode, unitCode, source string, billID *string) error {
	rawName = marketplace.NormalizeKey(rawName, "")
	_, err := r.db.Exec(
		`INSERT INTO mappings (raw_name, item_code, unit_code, source, confidence, learned_from_bill_id)
		 VALUES ($1, $2, $3, $4, 1.0, $5)
		 ON CONFLICT (raw_name) DO UPDATE
		   SET item_code = EXCLUDED.item_code,
		       unit_code = EXCLUDED.unit_code,
		       source = EXCLUDED.source,
		       confidence = 1.0,
		       learned_from_bill_id = EXCLUDED.learned_from_bill_id,
		       usage_count = mappings.usage_count + 1,
		       last_used_at = NOW(),
		       updated_at = NOW()`,
		rawName, itemCode, unitCode, source, billID,
	)
	return err
}

func (r *MappingRepo) IncrementUsage(id string) error {
	_, err := r.db.Exec(
		`UPDATE mappings SET usage_count = usage_count + 1, last_used_at = NOW() WHERE id = $1`,
		id,
	)
	return err
}

var ErrMappingConflict = fmt.Errorf("mapping changed")

func (r *MappingRepo) UpdateByID(id, itemCode, unitCode, updatedBy string, version time.Time) (*models.Mapping, error) {
	var updatedByArg interface{}
	if updatedBy != "" {
		updatedByArg = updatedBy
	}
	row := r.db.QueryRow(`
		UPDATE mappings
		SET item_code = $1, unit_code = $2, source = 'verified',
		    created_by = COALESCE($3, created_by), updated_at = NOW()
		WHERE id = $4 AND updated_at = $5
		RETURNING id, raw_name, item_code, unit_code, confidence, source,
		          usage_count, last_used_at, learned_from_bill_id, created_by, created_at, updated_at`,
		itemCode, unitCode, updatedByArg, id, version,
	)
	var m models.Mapping
	err := row.Scan(
		&m.ID, &m.RawName, &m.ItemCode, &m.UnitCode, &m.Confidence, &m.Source,
		&m.UsageCount, &m.LastUsedAt, &m.LearnedFromBillID, &m.CreatedBy, &m.CreatedAt, &m.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrMappingConflict
	}
	return &m, err
}

func (r *MappingRepo) DeleteVersion(id string, version time.Time) (bool, error) {
	res, err := r.db.Exec(`DELETE FROM mappings WHERE id = $1 AND updated_at = $2`, id, version)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (r *MappingRepo) ApplyToOpenNoSKUItems(rawName, itemCode, unitCode string) (int, int, error) {
	normalizedName := marketplace.NormalizeKey(rawName, "")
	tx, err := r.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.Exec(`
		UPDATE bill_items bi
		SET item_code = $1, unit_code = $2, mapped = TRUE
		FROM bills b
		WHERE b.id = bi.bill_id
		  AND b.source IN ('shopee','lazada','tiktok')
		  AND b.bill_type = 'sale'
		  AND b.status IN ('pending','needs_review')
		  AND b.archived_at IS NULL
		  AND btrim(replace(COALESCE(bi.source_sku, ''), chr(65279), '')) = ''
		  AND btrim(regexp_replace(replace(bi.raw_name, chr(65279), ''), '\s+', ' ', 'g')) = $3`, itemCode, unitCode, normalizedName)
	if err != nil {
		return 0, 0, err
	}
	applied, _ := res.RowsAffected()
	readyRes, err := tx.Exec(`
		UPDATE bills b
		SET status = 'pending', error_msg = NULL
		WHERE b.source IN ('shopee','lazada','tiktok')
		  AND b.bill_type = 'sale'
		  AND b.status = 'needs_review'
		  AND b.archived_at IS NULL
		  AND NOT EXISTS (
		    SELECT 1 FROM bill_items bi
		     WHERE bi.bill_id = b.id
		       AND (COALESCE(bi.item_code, '') = '' OR bi.mapped IS DISTINCT FROM TRUE)
		  )`)
	if err != nil {
		return 0, 0, err
	}
	ready, _ := readyRes.RowsAffected()
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return int(applied), int(ready), nil
}

func (r *MappingRepo) Stats() (map[string]interface{}, error) {
	stats := map[string]interface{}{}

	var total, verified, manual int
	_ = r.db.QueryRow(`SELECT COUNT(*) FROM mappings`).Scan(&total)
	_ = r.db.QueryRow(`SELECT COUNT(*) FROM mappings WHERE source='verified'`).Scan(&verified)
	_ = r.db.QueryRow(`SELECT COUNT(*) FROM mappings WHERE source='manual'`).Scan(&manual)

	stats["total"] = total
	stats["ai_learned"] = 0
	stats["verified"] = verified
	stats["manual"] = manual
	// auto_confirmed = ai_learned mappings (system learned from feedback)
	// needs_review = manual mappings (admin had to map manually)
	// Both share the same denominator (total mappings) for consistent %-bars
	stats["auto_confirmed"] = verified
	stats["needs_review"] = manual

	var feedbackCount int
	_ = r.db.QueryRow(`SELECT COUNT(*) FROM mapping_feedback`).Scan(&feedbackCount)
	stats["feedback_count"] = feedbackCount

	return stats, nil
}
