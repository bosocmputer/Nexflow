package repository

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"nexflow/internal/models"
)

func TestMarketplaceAliasAuditQueryTypesJSONParameters(t *testing.T) {
	if !strings.Contains(marketplaceAliasAuditInsertSQL, "'impact_digest',$10::text") {
		t.Fatal("impact digest must be explicitly typed so PostgreSQL can resolve jsonb_build_object parameters")
	}
	if !strings.Contains(marketplaceAliasAuditInsertSQL, "'affected_shop_ids',$11::jsonb") {
		t.Fatal("affected shop ids must remain explicitly typed as jsonb")
	}
}

func TestMarketplaceMappingCompletionReadyQueryHasContiguousParameters(t *testing.T) {
	matches := regexp.MustCompile(`\$(\d+)`).FindAllStringSubmatch(marketplaceMappingCompletionReadySQL, -1)
	seen := map[int]bool{}
	maxParameter := 0
	for _, match := range matches {
		parameter, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatal(err)
		}
		seen[parameter] = true
		if parameter > maxParameter {
			maxParameter = parameter
		}
	}
	for parameter := 1; parameter <= maxParameter; parameter++ {
		if !seen[parameter] {
			t.Fatalf("query skips $%d, so PostgreSQL cannot infer its parameter type", parameter)
		}
	}
}

func TestMarketplaceReservationReconcileUsesOnePostgresTypeForMultiplier(t *testing.T) {
	if !strings.Contains(marketplaceReservationReconcileUpdateSQL, "quantity_multiplier=$4::bigint") {
		t.Fatal("reservation multiplier assignment must explicitly use bigint")
	}
	if !strings.Contains(marketplaceReservationReconcileUpdateSQL, "source_qty*$4::bigint") {
		t.Fatal("reservation base quantity must reuse the bigint multiplier type")
	}
	if strings.Contains(marketplaceReservationReconcileUpdateSQL, "$4::numeric") {
		t.Fatal("casting the same parameter as numeric and bigint makes PostgreSQL reject the statement")
	}
	if !strings.Contains(marketplaceReservationReconcileUpdateSQL, "bi.id=ANY($12::uuid[])") ||
		!strings.Contains(marketplaceReservationReconcileUpdateSQL, "r.source_line_id=COALESCE(NULLIF(bi.source_line_id,''),bi.id::text)") {
		t.Fatal("reservation reconciliation must be scoped to the exact selected bill items")
	}
}

func TestResolveMarketplaceMutationReusesExistingTikTokVariant(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	aliasID := "00000000-0000-0000-0000-000000000001"
	mock.ExpectQuery(`(?s)SELECT id::text FROM marketplace_item_aliases a.*a.source=\$1.*a.account_key=\$2.*a.external_item_id=''.*a.external_variant_id=\$3.*a.is_active=true`).
		WithArgs("tiktok", "default", "1729429119889017310").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(aliasID))
	mock.ExpectQuery(`(?s)SELECT id::text,source,account_key.*FROM marketplace_item_aliases WHERE id=\$1::uuid`).
		WithArgs(aliasID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source", "account_key", "external_item_id", "external_variant_id", "source_sku", "raw_name", "normalized_key",
			"item_code", "unit_code", "quantity_multiplier", "stand", "divide", "generation", "conversion_status",
			"sales_enabled", "stock_policy", "scope_confirmed", "mapping_revision", "is_active",
		}).AddRow(aliasID, "tiktok", "default", "", "1729429119889017310", "", "สินค้า / ตัวเลือก", "สินค้า / ตัวเลือก",
			"AH-0006", "แท่ง", 1, "1", "1", "00000000-0000-0000-0000-000000000099", "ready",
			true, "blocked", true, 1, true))
	mock.ExpectQuery(`SELECT is_active,unit_code,item_type,set_document_valid,set_definition_hash`).
		WithArgs("AH-0006").
		WillReturnRows(sqlmock.NewRows([]string{"is_active", "unit_code", "item_type", "set_document_valid", "set_definition_hash"}).
			AddRow(true, "แท่ง", 0, true, ""))
	mock.ExpectQuery(`(?s)SELECT r.id::text,u.stand_value::text,u.divide_value::text.*u.item_code=\$1.*u.unit_code=\$2`).
		WithArgs("AH-0006", "แท่ง").
		WillReturnRows(sqlmock.NewRows([]string{"generation", "stand", "divide"}).
			AddRow("00000000-0000-0000-0000-000000000099", "1", "1"))

	proposal, current, _, err := resolveMarketplaceMutation(context.Background(), db, MarketplaceAliasProposal{
		Identity: models.MarketplaceAliasIdentity{
			Source: "tiktok", AccountKey: "default", ExternalVariantID: "1729429119889017310", RawName: "สินค้า / ตัวเลือก",
		},
		BillType: "sale", ItemCode: "AH-0006", UnitCode: "แท่ง",
	})
	if err != nil {
		t.Fatal(err)
	}
	if proposal.AliasID != aliasID || current.ID != aliasID || current.MappingRevision != 1 {
		t.Fatalf("existing alias was not reused: proposal=%+v current=%+v", proposal, current)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

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
	if err := validateMarketplaceStockPolicyTransition("managed", "blocked", false); !errors.Is(err, ErrMarketplaceUnsafeStockDisable) {
		t.Fatalf("managed listing must not be silently blocked: %v", err)
	}
	if err := validateMarketplaceStockPolicyTransition("managed", "zeroing", false); err != nil {
		t.Fatalf("managed listing may enter durable zeroing: %v", err)
	}
	if err := validateMarketplaceStockPolicyTransition("managed", "manual_unmanaged", false); !errors.Is(err, ErrMarketplaceManualUnmanagedAcknowledgementRequired) {
		t.Fatalf("manual unmanaged transition must require an explicit acknowledgement: %v", err)
	}
	if err := validateMarketplaceStockPolicyTransition("managed", "manual_unmanaged", true); err != nil {
		t.Fatalf("acknowledged manual unmanaged transition should be allowed: %v", err)
	}
	if err := validateMarketplaceStockPolicyTransition("managed", "disabled_zero", false); err == nil {
		t.Fatal("public mutation must not assert disabled_zero without Shopee read-back")
	}
	if err := validateMarketplaceStockPolicyTransition("zeroing", "manual_unmanaged", true); !errors.Is(err, ErrMarketplaceStockZeroingInProgress) {
		t.Fatalf("zeroing must not be bypassed while the durable write is in flight: %v", err)
	}
	if err := validateMarketplaceStockPolicyTransition("zeroing", "zeroing", false); !errors.Is(err, ErrMarketplaceStockZeroingInProgress) {
		t.Fatalf("the public mutation flow must be locked while the durable zeroing job owns the listing: %v", err)
	}
	if err := validateMarketplaceStockPolicyTransition("disabled_zero", "managed", false); err != nil {
		t.Fatalf("a confirmed zero listing may be enabled through the normal guarded flow: %v", err)
	}
}

func TestDeactivateManagedMarketplaceAliasRequiresSafeStockPolicyFirst(t *testing.T) {
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
			"OLD", "ชิ้น", 1, "1", "1", "00000000-0000-0000-0000-000000000099", "ready", true, "managed", true, 7, true))

	_, _, _, err = resolveMarketplaceMutation(context.Background(), db, MarketplaceAliasProposal{
		AliasID: "00000000-0000-0000-0000-000000000001", Deactivate: true,
	})
	if !errors.Is(err, ErrMarketplaceUnsafeStockDisable) {
		t.Fatalf("managed alias deactivation must fail closed before any catalog mutation: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLatestStockPolicyJobForAliasRecoversDurableJobAcrossSessions(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createdAt := time.Now().UTC()
	mock.ExpectQuery(`(?s)FROM shopee_stock_policy_jobs.*marketplace_alias_id=\$1::uuid AND tenant_key=\$2.*ORDER BY created_at DESC LIMIT 1`).
		WithArgs("00000000-0000-0000-0000-000000000001", "tenant-a").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "shop_id", "marketplace_alias_id", "target_revision", "item_id", "model_id", "policy_action",
			"status", "attempt_count", "error_message", "created_at", "finished_at",
		}).AddRow("00000000-0000-0000-0000-000000000010", int64(42), "00000000-0000-0000-0000-000000000001", int64(8), int64(10), int64(20),
			"zero_then_disable", "failed", 10, "read-back failed", createdAt, nil))

	repo := NewMarketplaceAliasRepo(db).WithTenantKey("tenant-a")
	job, err := repo.LatestStockPolicyJobForAlias(context.Background(), "00000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != "00000000-0000-0000-0000-000000000010" || job.Status != "failed" || job.AttemptCount != 10 {
		t.Fatalf("job=%+v", job)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
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
