package handlers

import (
	"errors"
	"testing"

	"nexflow/internal/models"
)

func strptr(value string) *string { return &value }

func TestDropUnchangedMarketplaceItemFields(t *testing.T) {
	existing := &models.BillItem{ItemCode: strptr("AH-0001"), UnitCode: strptr("กล่อง")}
	req := updateItemRequest{ItemCode: strptr(" AH-0001 "), UnitCode: strptr("กล่อง")}

	dropUnchangedMarketplaceItemFields(existing, &req)

	if req.ItemCode != nil || req.UnitCode != nil {
		t.Fatalf("unchanged item/unit must be omitted, got item=%v unit=%v", req.ItemCode, req.UnitCode)
	}
}

func TestValidateMarketplaceMasterReapplyAcceptsCurrentReadyMaster(t *testing.T) {
	aliasID := "00000000-0000-0000-0000-000000000010"
	stand, divide, generation := "1", "1", "00000000-0000-0000-0000-000000000099"
	item := &models.BillItem{MarketplaceAliasID: &aliasID}
	alias := &models.MarketplaceItemAlias{
		ID: aliasID, MappingRevision: 2, ScopeConfirmed: true, ConversionStatus: "ready", SalesEnabled: true,
		UnitStandValue: &stand, UnitDivideValue: &divide, UnitCatalogGeneration: &generation,
	}

	if err := validateMarketplaceMasterReapply(item, alias, 2); err != nil {
		t.Fatalf("ready Product Master rejected: %v", err)
	}
}

func TestValidateMarketplaceMasterReapplyRejectsStaleRevision(t *testing.T) {
	aliasID := "00000000-0000-0000-0000-000000000010"
	stand, divide, generation := "1", "1", "00000000-0000-0000-0000-000000000099"
	item := &models.BillItem{MarketplaceAliasID: &aliasID}
	alias := &models.MarketplaceItemAlias{
		ID: aliasID, MappingRevision: 3, ScopeConfirmed: true, ConversionStatus: "ready", SalesEnabled: true,
		UnitStandValue: &stand, UnitDivideValue: &divide, UnitCatalogGeneration: &generation,
	}

	if err := validateMarketplaceMasterReapply(item, alias, 2); !errors.Is(err, errMarketplaceMasterRevisionChanged) {
		t.Fatalf("error = %v, want revision conflict", err)
	}
}

func TestValidateMarketplaceMasterReapplyRejectsIncompleteConversion(t *testing.T) {
	aliasID := "00000000-0000-0000-0000-000000000010"
	item := &models.BillItem{MarketplaceAliasID: &aliasID}
	alias := &models.MarketplaceItemAlias{
		ID: aliasID, MappingRevision: 2, ScopeConfirmed: true, ConversionStatus: "ready", SalesEnabled: true,
	}

	if err := validateMarketplaceMasterReapply(item, alias, 2); !errors.Is(err, errMarketplaceMasterNotReady) {
		t.Fatalf("error = %v, want not-ready error", err)
	}
}

func TestDropUnchangedMarketplaceItemFieldsKeepsRealChanges(t *testing.T) {
	existing := &models.BillItem{ItemCode: strptr("AH-0001"), UnitCode: strptr("กล่อง")}
	req := updateItemRequest{ItemCode: strptr("AH-0003"), UnitCode: strptr("แพ็ค")}

	dropUnchangedMarketplaceItemFields(existing, &req)

	if req.ItemCode == nil || *req.ItemCode != "AH-0003" {
		t.Fatalf("changed item code was dropped: %#v", req.ItemCode)
	}
	if req.UnitCode == nil || *req.UnitCode != "แพ็ค" {
		t.Fatalf("changed unit code was dropped: %#v", req.UnitCode)
	}
}

func TestDropUnchangedMarketplaceItemFieldsTreatsWhitespaceAsSame(t *testing.T) {
	existing := &models.BillItem{ItemCode: strptr("AH-0001"), UnitCode: strptr(" กล่อง ")}
	req := updateItemRequest{ItemCode: strptr("AH-0001"), UnitCode: strptr("กล่อง")}

	dropUnchangedMarketplaceItemFields(existing, &req)

	if req.ItemCode != nil || req.UnitCode != nil {
		t.Fatalf("whitespace-only differences must not create manual overrides")
	}
}
