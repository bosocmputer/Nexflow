package handlers

import "testing"

func TestCatalogImageURL(t *testing.T) {
	roworder := 3
	got := catalogImageURL(" BF0004 ", 2, &roworder)
	if got != "/api/catalog/BF0004/image" {
		t.Fatalf("url = %q, want /api/catalog/BF0004/image", got)
	}
}

func TestCatalogImageURLEmptyWithoutMetadata(t *testing.T) {
	roworder := 1
	tests := []struct {
		name  string
		code  string
		count int
		row   *int
	}{
		{name: "no code", code: "", count: 1, row: &roworder},
		{name: "no count", code: "BF00002", count: 0, row: &roworder},
		{name: "no roworder", code: "BF00002", count: 1, row: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := catalogImageURL(tt.code, tt.count, tt.row); got != "" {
				t.Fatalf("url = %q, want empty", got)
			}
		})
	}
}

func TestCatalogImageRowURL(t *testing.T) {
	got := catalogImageRowURL(" BF0004 ", 3)
	if got != "/api/catalog/BF0004/images/3" {
		t.Fatalf("url = %q, want /api/catalog/BF0004/images/3", got)
	}
}

func TestCatalogImageRowURLEmptyWithoutValidInput(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		roworder int
	}{
		{name: "no code", code: "", roworder: 1},
		{name: "bad roworder", code: "BF00002", roworder: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := catalogImageRowURL(tt.code, tt.roworder); got != "" {
				t.Fatalf("url = %q, want empty", got)
			}
		})
	}
}
