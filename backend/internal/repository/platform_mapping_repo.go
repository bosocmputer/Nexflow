package repository

import (
	"database/sql"
	"fmt"

	"nexflow/internal/models"
)

// Default column mappings for each platform — used when no DB config exists yet
var defaultColumnMappings = map[string]map[string]string{
	"lazada": {
		"order_id":    "Order ID",
		"buyer_name":  "Buyer Name",
		"buyer_phone": "Buyer Phone",
		"item_name":   "Product Name",
		"sku":         "Seller SKU",
		"qty":         "Quantity",
		"price":       "Unit Price",
	},
	"shopee": {
		"order_id":    "Order ID",
		"buyer_name":  "Buyer Username",
		"buyer_phone": "Contact Phone",
		"item_name":   "Product Name",
		"sku":         "Variation SKU",
		"qty":         "Quantity",
		"price":       "Selling Price",
	},
}

type PlatformMappingRepo struct {
	db *sql.DB
}

func NewPlatformMappingRepo(db *sql.DB) *PlatformMappingRepo {
	return &PlatformMappingRepo{db: db}
}

// Get returns all column mappings for a platform, falling back to defaults for missing fields.
func (r *PlatformMappingRepo) Get(platform string) ([]models.PlatformColumnMapping, error) {
	rows, err := r.db.Query(
		`SELECT id, platform, field_name, column_name, updated_at
		 FROM platform_column_mappings WHERE platform = $1`, platform,
	)
	if err != nil {
		return nil, fmt.Errorf("PlatformMappingRepo.Get: %w", err)
	}
	defer rows.Close()

	stored := map[string]models.PlatformColumnMapping{}
	for rows.Next() {
		var m models.PlatformColumnMapping
		if err := rows.Scan(&m.ID, &m.Platform, &m.FieldName, &m.ColumnName, &m.UpdatedAt); err != nil {
			return nil, err
		}
		stored[m.FieldName] = m
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Merge with defaults: DB values override defaults
	defaults := defaultColumnMappings[platform]
	result := make([]models.PlatformColumnMapping, 0, len(defaults))
	for fieldName, defaultCol := range defaults {
		if m, ok := stored[fieldName]; ok {
			result = append(result, m)
		} else {
			result = append(result, models.PlatformColumnMapping{
				Platform:   platform,
				FieldName:  fieldName,
				ColumnName: defaultCol,
			})
		}
	}
	return result, nil
}

// Upsert inserts or updates a single column mapping for a platform.
func (r *PlatformMappingRepo) Upsert(platform, fieldName, columnName string, userID *string) error {
	_, err := r.db.Exec(
		`INSERT INTO platform_column_mappings (platform, field_name, column_name, updated_by, updated_at)
		 VALUES ($1, $2, $3, $4, NOW())
		 ON CONFLICT (platform, field_name) DO UPDATE
		 SET column_name = EXCLUDED.column_name,
		     updated_by  = EXCLUDED.updated_by,
		     updated_at  = NOW()`,
		platform, fieldName, columnName, userID,
	)
	return err
}

// ToMap converts a slice of PlatformColumnMapping to field_name→column_name map.
func ToColumnMap(mappings []models.PlatformColumnMapping) map[string]string {
	m := make(map[string]string, len(mappings))
	for _, pcm := range mappings {
		m[pcm.FieldName] = pcm.ColumnName
	}
	return m
}
