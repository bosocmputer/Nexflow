package catalog

import (
	"testing"

	"nexflow/internal/models"
)

func TestCatalogTextScorePrioritizesExactItemCode(t *testing.T) {
	item := models.CatalogItem{
		ItemCode: "BF00002",
		ItemName: "PUMPKIN หัวแร้งบัดกรี",
	}

	if got := catalogTextScore("bf00002", item); got != 1 {
		t.Fatalf("score = %v, want exact code score 1", got)
	}
}

func TestCatalogTextScoreBoostsItemCodePrefix(t *testing.T) {
	item := models.CatalogItem{
		ItemCode: "BF00002",
		ItemName: "PUMPKIN หัวแร้งบัดกรี",
	}

	if got := catalogTextScore("bf000", item); got < 0.98 {
		t.Fatalf("score = %v, want prefix code score >= 0.98", got)
	}
}
