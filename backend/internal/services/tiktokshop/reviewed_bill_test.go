package tiktokshop

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"nexflow/internal/models"
)

type reviewedBillWriterFake struct {
	bill  *models.Bill
	items []models.BillItem
	audit models.AuditEntry
	err   error
	calls int
}

func (f *reviewedBillWriterFake) CreateWithItemsAndAudit(bill *models.Bill, items []models.BillItem, audit models.AuditEntry) error {
	f.calls++
	f.bill = bill
	f.items = items
	f.audit = audit
	if f.err == nil {
		bill.ID = "11111111-1111-4111-8111-111111111111"
	}
	return f.err
}

func TestTikTokReviewedBillCreatesOnePendingBillFromReviewedSnapshot(t *testing.T) {
	source := readyControlledTikTokBillSource()
	loader := &billShadowSourceFake{source: source}
	writer := &reviewedBillWriterFake{}
	preview, err := NewTikTokBillShadowService(loader).Preview(t.Context(), source.ShopID, source.Order.ID)
	if err != nil {
		t.Fatal(err)
	}

	result, err := NewTikTokReviewedBillService(loader, writer).Create(t.Context(), TikTokReviewedBillInput{
		ShopID: source.ShopID, OrderID: source.Order.ID, ReviewDigest: preview.ReviewDigest,
		ActorID: "91e80d9f-aba7-4d9e-89db-e7e4e6d262ef", TraceID: "trace-reviewed-bill",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.Reused || result.BillID == "" || writer.calls != 1 {
		t.Fatalf("result=%+v writer_calls=%d", result, writer.calls)
	}
	if writer.bill.Source != "tiktok" || writer.bill.SourceAccountKey != "shop:"+source.ShopID ||
		writer.bill.Status != "pending" || writer.bill.SMLOrderID != source.Order.ID || writer.bill.SMLDocNo != nil || len(writer.bill.SMLPayload) != 0 {
		t.Fatalf("unsafe bill=%+v", writer.bill)
	}
	if len(writer.items) != 1 {
		t.Fatalf("items=%+v", writer.items)
	}
	item := writer.items[0]
	if item.ItemCode == nil || *item.ItemCode != "AH-0002" || item.UnitCode == nil || *item.UnitCode != "กล่อง" ||
		item.MarketplaceAliasID == nil || *item.MarketplaceAliasID != "22222222-2222-4222-8222-222222222222" ||
		item.SourceQty == nil || *item.SourceQty != 1 || item.SMLQty == nil || *item.SMLQty != 1 ||
		item.MappingRevisionSnapshot == nil || *item.MappingRevisionSnapshot != 1 {
		t.Fatalf("item evidence=%+v", item)
	}
	var raw map[string]any
	if err := json.Unmarshal(writer.bill.RawData, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["flow"] != TikTokReviewedBillFlow || raw["review_digest"] != preview.ReviewDigest || raw["item_insurance_fee"] != "7.49" {
		t.Fatalf("raw=%+v", raw)
	}
	auditDetail, ok := writer.audit.Detail.(map[string]interface{})
	if writer.audit.Action != "bill_created" || !ok || auditDetail["flow"] != TikTokReviewedBillFlow {
		t.Fatalf("audit=%+v", writer.audit)
	}
}

func TestTikTokReviewedBillReplayReusesOnlySameShopReviewedBill(t *testing.T) {
	source := readyControlledTikTokBillSource()
	preview, err := NewTikTokBillShadowService(&billShadowSourceFake{source: source}).Preview(t.Context(), source.ShopID, source.Order.ID)
	if err != nil {
		t.Fatal(err)
	}
	source.ExistingBill = &TikTokBillShadowExistingBill{
		ID: "11111111-1111-4111-8111-111111111111", Status: "pending",
		SourceAccountKey: "shop:" + source.ShopID, SourceFlow: TikTokReviewedBillFlow, DocumentRoute: "saleinvoice",
	}
	writer := &reviewedBillWriterFake{}
	result, err := NewTikTokReviewedBillService(&billShadowSourceFake{source: source}, writer).Create(t.Context(), TikTokReviewedBillInput{
		ShopID: source.ShopID, OrderID: source.Order.ID, ReviewDigest: preview.ReviewDigest,
		ActorID: "91e80d9f-aba7-4d9e-89db-e7e4e6d262ef",
	})
	if err != nil || result == nil || !result.Reused || result.BillID != source.ExistingBill.ID || writer.calls != 0 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, writer.calls)
	}

	source.ExistingBill.SourceAccountKey = "default"
	if _, err := NewTikTokReviewedBillService(&billShadowSourceFake{source: source}, writer).Create(t.Context(), TikTokReviewedBillInput{
		ShopID: source.ShopID, OrderID: source.Order.ID, ReviewDigest: preview.ReviewDigest,
		ActorID: "91e80d9f-aba7-4d9e-89db-e7e4e6d262ef",
	}); !errors.Is(err, ErrTikTokReviewedBillConflict) {
		t.Fatalf("cross-scope replay err=%v", err)
	}
}

func TestTikTokReviewedBillFailsClosedWhenReviewChangedOrPreviewBlocked(t *testing.T) {
	source := readyControlledTikTokBillSource()
	writer := &reviewedBillWriterFake{}
	service := NewTikTokReviewedBillService(&billShadowSourceFake{source: source}, writer)
	if _, err := service.Create(context.Background(), TikTokReviewedBillInput{
		ShopID: source.ShopID, OrderID: source.Order.ID,
		ReviewDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ActorID:      "91e80d9f-aba7-4d9e-89db-e7e4e6d262ef",
	}); !errors.Is(err, ErrTikTokReviewedBillReviewChanged) || writer.calls != 0 {
		t.Fatalf("digest err=%v calls=%d", err, writer.calls)
	}

	blocked := controlledTikTokBillShadowSource()
	blockedPreview, err := NewTikTokBillShadowService(&billShadowSourceFake{source: blocked}).Preview(t.Context(), blocked.ShopID, blocked.Order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewTikTokReviewedBillService(&billShadowSourceFake{source: blocked}, writer).Create(t.Context(), TikTokReviewedBillInput{
		ShopID: blocked.ShopID, OrderID: blocked.Order.ID, ReviewDigest: blockedPreview.ReviewDigest,
		ActorID: "91e80d9f-aba7-4d9e-89db-e7e4e6d262ef",
	}); !errors.Is(err, ErrTikTokReviewedBillNotReady) || writer.calls != 0 {
		t.Fatalf("blocked err=%v calls=%d", err, writer.calls)
	}

	invalidEvidence := readyControlledTikTokBillSource()
	invalidEvidence.SourceHash = ""
	invalidPreview, err := NewTikTokBillShadowService(&billShadowSourceFake{source: invalidEvidence}).Preview(t.Context(), invalidEvidence.ShopID, invalidEvidence.Order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !hasTikTokBillShadowBlocker(invalidPreview.Blockers, TikTokBillShadowBlockerSourceInvalid) {
		t.Fatalf("missing source evidence blocker: %+v", invalidPreview.Blockers)
	}
	if _, err := NewTikTokReviewedBillService(&billShadowSourceFake{source: invalidEvidence}, writer).Create(t.Context(), TikTokReviewedBillInput{
		ShopID: invalidEvidence.ShopID, OrderID: invalidEvidence.Order.ID, ReviewDigest: invalidPreview.ReviewDigest,
		ActorID: "91e80d9f-aba7-4d9e-89db-e7e4e6d262ef",
	}); !errors.Is(err, ErrTikTokReviewedBillNotReady) || writer.calls != 0 {
		t.Fatalf("invalid evidence err=%v calls=%d", err, writer.calls)
	}
}

func readyControlledTikTokBillSource() *TikTokBillShadowSource {
	source := controlledTikTokBillShadowSource()
	source.SourceHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	source.Mappings = []TikTokBillShadowMappingSource{{
		AliasID:   "22222222-2222-4222-8222-222222222222",
		ProductID: source.Items[0].ProductID, SKUID: source.Items[0].SKUID,
		AccountKey: "shop:" + source.ShopID, ItemCode: "AH-0002", UnitCode: "กล่อง",
		IsActive: true, ScopeConfirmed: true, SalesEnabled: true, ConversionStatus: "ready",
		QuantityMultiplier: 1, UnitStandValue: "1", UnitDivideValue: "1", CatalogReady: true,
		MappingRevision: 1, UnitCatalogGeneration: "33333333-3333-4333-8333-333333333333",
	}}
	return source
}
