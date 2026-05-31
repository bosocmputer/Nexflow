package models

import (
	"time"
)

// CatalogItem represents one row in sml_catalog
type CatalogItem struct {
	ItemCode             string     `json:"item_code"`
	ItemName             string     `json:"item_name"`
	ItemName2            string     `json:"item_name2"`
	UnitCode             string     `json:"unit_code"`
	WHCode               string     `json:"wh_code"`
	ShelfCode            string     `json:"shelf_code"`
	Price                *float64   `json:"price"`
	GroupCode            string     `json:"group_code"`
	BalanceQty           *float64   `json:"balance_qty"`
	EmbeddingStatus      string     `json:"embedding_status"` // pending | done | error
	EmbeddedAt           *time.Time `json:"embedded_at"`
	ImageCount           int        `json:"image_count"`
	PrimaryImageRoworder *int       `json:"primary_image_roworder,omitempty"`
	PrimaryImageGuid     string     `json:"primary_image_guid,omitempty"`
	PrimaryImageBytes    *int64     `json:"primary_image_bytes,omitempty"`
	ImageSyncedAt        *time.Time `json:"image_synced_at,omitempty"`
	ImageURL             string     `json:"image_url,omitempty"`
	HasHiddenChars       bool       `json:"has_hidden_chars"`
	CleanItemCode        string     `json:"clean_item_code,omitempty"`
	HiddenCharKinds      []string   `json:"hidden_char_kinds,omitempty"`
	ImageMetadataSynced  bool       `json:"-"`
	SyncedAt             time.Time  `json:"synced_at"`
	CreatedAt            time.Time  `json:"created_at"`
}

// CatalogMatch is one similarity search result
type CatalogMatch struct {
	ItemCode             string   `json:"item_code"`
	ItemName             string   `json:"item_name"`
	ItemName2            string   `json:"item_name2"`
	UnitCode             string   `json:"unit_code"`
	WHCode               string   `json:"wh_code"`
	ShelfCode            string   `json:"shelf_code"`
	Price                float64  `json:"price"`
	ImageCount           int      `json:"image_count"`
	PrimaryImageRoworder *int     `json:"primary_image_roworder,omitempty"`
	PrimaryImageGuid     string   `json:"primary_image_guid,omitempty"`
	PrimaryImageBytes    *int64   `json:"primary_image_bytes,omitempty"`
	ImageURL             string   `json:"image_url,omitempty"`
	HasHiddenChars       bool     `json:"has_hidden_chars"`
	CleanItemCode        string   `json:"clean_item_code,omitempty"`
	HiddenCharKinds      []string `json:"hidden_char_kinds,omitempty"`
	Score                float64  `json:"score"` // cosine similarity 0–1
}
