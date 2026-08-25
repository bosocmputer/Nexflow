package models

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestCatalogItemDoesNotContainLegacyProductPrice(t *testing.T) {
	if _, exists := reflect.TypeOf(CatalogItem{}).FieldByName("Price"); exists {
		t.Fatal("CatalogItem must not read or expose the legacy product price")
	}
	body, err := json.Marshal(CatalogItem{ItemCode: "SKU-1", ItemName: "สินค้า"})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if strings.Contains(string(body), "price") {
		t.Fatalf("catalog JSON leaked legacy price: %s", body)
	}
}

func TestCatalogMatchDoesNotContainLegacyProductPrice(t *testing.T) {
	if _, exists := reflect.TypeOf(CatalogMatch{}).FieldByName("Price"); exists {
		t.Fatal("CatalogMatch must not read or expose the legacy product price")
	}
	body, err := json.Marshal(CatalogMatch{ItemCode: "SKU-1"})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if strings.Contains(string(body), "price") {
		t.Fatalf("catalog match JSON leaked legacy price: %s", body)
	}
}
