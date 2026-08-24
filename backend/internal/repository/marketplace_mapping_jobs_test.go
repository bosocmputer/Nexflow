package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"nexflow/internal/models"
)

func TestMarketplaceImpactDigestIsStableButIncludesPolicy(t *testing.T) {
	impact := models.MarketplaceAliasImpact{
		CurrentMappingRevision: 7,
		OpenItems:              12,
		AffectedShopIDs:        []int64{2, 9},
		StockConfigVersions:    map[string]int64{"9": 4, "2": 8},
	}
	target := marketplaceMutationTarget{
		ItemCode: "SKU-1", UnitCode: "แพ็ค", QuantityMultiplier: 2,
		StandValue: "12", DivideValue: "1", StockPolicy: "managed",
		RequestedAt: time.Now(),
	}
	first := marketplaceImpactDigest(impact, target)
	target.RequestedAt = target.RequestedAt.Add(time.Hour)
	second := marketplaceImpactDigest(impact, target)
	if first == "" || first != second {
		t.Fatalf("digest should ignore request time: first=%q second=%q", first, second)
	}
	target.StockPolicy = "manual_unmanaged"
	if third := marketplaceImpactDigest(impact, target); third == first {
		t.Fatal("digest must change when stock policy changes")
	}
}

func TestMarketplaceMappingCompletionWarningsFailClosed(t *testing.T) {
	tests := []struct {
		name     string
		target   marketplaceMutationTarget
		expected []string
	}{
		{name: "managed ready", target: marketplaceMutationTarget{ConversionStatus: "ready", StockPolicy: "managed"}},
		{name: "manual unmanaged", target: marketplaceMutationTarget{ConversionStatus: "ready", StockPolicy: "manual_unmanaged"}, expected: []string{"stock_policy_manual_unmanaged"}},
		{name: "ambiguous blocked", target: marketplaceMutationTarget{ConversionStatus: "needs_review", StockPolicy: "blocked"}, expected: []string{"conversion_needs_review", "stock_policy_blocked"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := marketplaceMappingCompletionWarnings(test.target)
			if len(actual) != len(test.expected) {
				t.Fatalf("warnings=%v expected=%v", actual, test.expected)
			}
			for index := range actual {
				if actual[index] != test.expected[index] {
					t.Fatalf("warnings=%v expected=%v", actual, test.expected)
				}
			}
		})
	}
}

func TestMarketplaceStockPolicyTransitionsRequireDurableConfirmation(t *testing.T) {
	if err := validateMarketplaceStockPolicyTransition("managed", "disabled_zero"); err == nil {
		t.Fatal("public mutation must not assert disabled_zero without Shopee read-back")
	}
	if err := validateMarketplaceStockPolicyTransition("zeroing", "manual_unmanaged"); err == nil {
		t.Fatal("zeroing must not be bypassed while the durable write is in flight")
	}
	if err := validateMarketplaceStockPolicyTransition("disabled_zero", "managed"); err != nil {
		t.Fatalf("a confirmed zero listing may be enabled through the normal guarded flow: %v", err)
	}
}

func TestResolveMarketplaceMutationPreservesOmittedExistingConversionFields(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`(?s)SELECT id::text,source,account_key.*FROM marketplace_item_aliases WHERE id=\$1::uuid`).
		WithArgs("00000000-0000-0000-0000-000000000001").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source", "account_key", "external_item_id", "external_variant_id", "source_sku", "raw_name", "normalized_key",
			"item_code", "unit_code", "quantity_multiplier", "stand", "divide", "generation", "conversion_status",
			"sales_enabled", "stock_policy", "scope_confirmed", "mapping_revision", "is_active",
		}).AddRow("00000000-0000-0000-0000-000000000001", "shopee", "shop:42", "10", "20", "SELLER", "สินค้า", "สินค้า",
			"OLD", "แพ็ค", 3, "12", "1", "00000000-0000-0000-0000-000000000099", "ready", true, "managed", true, 7, true))
	mock.ExpectQuery(`SELECT is_active,unit_code,item_type,set_document_valid,set_definition_hash`).
		WithArgs("NEW").
		WillReturnRows(sqlmock.NewRows([]string{"is_active", "unit_code", "item_type", "set_document_valid", "set_definition_hash"}).
			AddRow(true, "แพ็ค", 0, true, ""))
	mock.ExpectQuery(`(?s)SELECT r.id::text,u.stand_value::text,u.divide_value::text.*r.status='active'.*u.item_code=\$1.*u.unit_code=\$2`).
		WithArgs("NEW", "แพ็ค").
		WillReturnRows(sqlmock.NewRows([]string{"generation", "stand", "divide"}).
			AddRow("00000000-0000-0000-0000-000000000100", "24", "1"))

	_, _, target, err := resolveMarketplaceMutation(context.Background(), db, MarketplaceAliasProposal{
		AliasID: "00000000-0000-0000-0000-000000000001", ItemCode: "NEW",
	})
	if err != nil {
		t.Fatal(err)
	}
	if target.QuantityMultiplier != 3 || !target.SalesEnabled || target.StockPolicy != "managed" || !target.ScopeConfirmed {
		t.Fatalf("omitted fields were not preserved: %#v", target)
	}
	if target.UnitCode != "แพ็ค" || target.StandValue != "24" || target.ConversionStatus != "ready" {
		t.Fatalf("active unit generation not snapshotted: %#v", target)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
