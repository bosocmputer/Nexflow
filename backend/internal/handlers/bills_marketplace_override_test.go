package handlers

import (
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
