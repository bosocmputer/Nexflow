package repository

import "time"

type CatalogGenerationProduct struct {
	ItemCode        string
	ItemName        string
	StandardUnit    string
	ItemType        int
	SourceUpdatedAt time.Time
}

type CatalogGenerationUnit struct {
	ItemCode    string
	UnitCode    string
	UnitName    string
	StandValue  string
	DivideValue string
	IsDefault   bool
	UnitOrder   int
}

type CatalogGenerationBarcode struct {
	ItemCode string
	UnitCode string
	Barcode  string
}

type CatalogGenerationSetComponent struct {
	ParentItemCode string
	LineNumber     int
	RowOrder       int
	ItemCode       string
	ItemName       string
	UnitCode       string
	Qty            string
	UnitFactor     string
	DefinitionHash string
}
