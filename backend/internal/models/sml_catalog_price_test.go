package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCatalogItemJSONDoesNotExposeLegacyPrice(t *testing.T) {
	price := 123.45
	body, err := json.Marshal(CatalogItem{ItemCode: "SKU-1", ItemName: "สินค้า", Price: &price})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if strings.Contains(string(body), "price") {
		t.Fatalf("catalog JSON leaked legacy price: %s", body)
	}
}

func TestCatalogMatchJSONDoesNotExposeLegacyPrice(t *testing.T) {
	body, err := json.Marshal(CatalogMatch{ItemCode: "SKU-1", Price: 123.45})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if strings.Contains(string(body), "price") {
		t.Fatalf("catalog match JSON leaked legacy price: %s", body)
	}
}
