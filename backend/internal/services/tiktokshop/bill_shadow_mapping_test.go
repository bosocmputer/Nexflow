package tiktokshop

import (
	"context"
	"errors"
	"testing"

	"nexflow/internal/models"
	"nexflow/internal/repository"
)

type billShadowMappingRepoFake struct {
	previewProposal repository.MarketplaceAliasProposal
	commitProposal  repository.MarketplaceAliasProposal
	previewResult   models.MarketplaceAliasImpact
	commitResult    *repository.MarketplaceAliasCommitResult
	err             error
	previewCalls    int
	commitCalls     int
}

func (f *billShadowMappingRepoFake) PreviewMutation(_ context.Context, proposal repository.MarketplaceAliasProposal) (models.MarketplaceAliasImpact, error) {
	f.previewCalls++
	f.previewProposal = proposal
	return f.previewResult, f.err
}

func (f *billShadowMappingRepoFake) CommitMutation(_ context.Context, proposal repository.MarketplaceAliasProposal) (*repository.MarketplaceAliasCommitResult, error) {
	f.commitCalls++
	f.commitProposal = proposal
	return f.commitResult, f.err
}

func TestTikTokBillShadowMappingPreviewBuildsExactShopScopedIdentity(t *testing.T) {
	loader := &billShadowSourceFake{source: controlledTikTokBillShadowSource()}
	repo := &billShadowMappingRepoFake{previewResult: models.MarketplaceAliasImpact{
		CurrentMappingRevision: 0, AfterFormula: "1 แท่ง", ConversionStatus: "ready", ImpactDigest: "preview-digest",
	}}
	service := NewTikTokBillShadowMappingService(loader, repo)

	impact, err := service.Preview(context.Background(), TikTokBillShadowMappingSelection{
		ShopID: "7494619203789490654", OrderID: "586030483469993439",
		ProductID: "1729429119195974110", SKUID: "1729429118580984286",
		ItemCode: "AH-0006", UnitCode: "แท่ง", QuantityMultiplier: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if impact.ImpactDigest != "preview-digest" || loader.calls != 1 || repo.previewCalls != 1 || repo.commitCalls != 0 {
		t.Fatalf("unexpected calls/result: impact=%+v loader=%d repo=%+v", impact, loader.calls, repo)
	}
	proposal := repo.previewProposal
	if proposal.Identity.Source != "tiktok" || proposal.Identity.AccountKey != "shop:7494619203789490654" ||
		proposal.Identity.ExternalItemID != "1729429119195974110" || proposal.Identity.ExternalVariantID != "1729429118580984286" ||
		proposal.Identity.RawName != "สีเพ้นคิ้วเฮนน่า / 02 - น้ำตาลเข้ม" || proposal.BillType != "sale" ||
		proposal.ItemCode != "AH-0006" || proposal.UnitCode != "แท่ง" || proposal.QuantityMultiplier != 2 ||
		proposal.ScopeConfirmed == nil || !*proposal.ScopeConfirmed || proposal.SalesEnabled == nil || !*proposal.SalesEnabled ||
		proposal.StockPolicy != "blocked" {
		t.Fatalf("unsafe mapping proposal: %+v", proposal)
	}
}

func TestTikTokBillShadowMappingConfirmReusesReviewedImpact(t *testing.T) {
	loader := &billShadowSourceFake{source: controlledTikTokBillShadowSource()}
	repo := &billShadowMappingRepoFake{commitResult: &repository.MarketplaceAliasCommitResult{
		Alias:  &models.MarketplaceItemAlias{ID: "alias-1", AccountKey: "shop:7494619203789490654"},
		Impact: models.MarketplaceAliasImpact{CurrentMappingRevision: 0, ImpactDigest: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
	}}
	service := NewTikTokBillShadowMappingService(loader, repo)

	result, err := service.Confirm(context.Background(), TikTokBillShadowMappingConfirmation{
		Selection: TikTokBillShadowMappingSelection{
			ShopID: "7494619203789490654", OrderID: "586030483469993439",
			ProductID: "1729429119195974110", SKUID: "1729429118580984286",
			ItemCode: "AH-0006", UnitCode: "แท่ง", QuantityMultiplier: 1,
		},
		ExpectedMappingRevision: 0,
		ImpactDigest:            "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}, "91e80d9f-aba7-4d9e-89db-e7e4e6d262ef")
	if err != nil {
		t.Fatal(err)
	}
	if result.Alias == nil || repo.previewCalls != 0 || repo.commitCalls != 1 {
		t.Fatalf("unexpected result/calls: result=%+v repo=%+v", result, repo)
	}
	proposal := repo.commitProposal
	if proposal.ConfirmedBy != "91e80d9f-aba7-4d9e-89db-e7e4e6d262ef" || proposal.ExpectedRevision != 0 ||
		proposal.ExpectedImpactDigest != "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" ||
		proposal.Identity.AccountKey != "shop:7494619203789490654" {
		t.Fatalf("confirmation lost reviewed scope: %+v", proposal)
	}
}

func TestTikTokBillShadowMappingRejectsItemOutsideSnapshot(t *testing.T) {
	loader := &billShadowSourceFake{source: controlledTikTokBillShadowSource()}
	repo := &billShadowMappingRepoFake{}
	service := NewTikTokBillShadowMappingService(loader, repo)

	_, err := service.Preview(context.Background(), TikTokBillShadowMappingSelection{
		ShopID: "7494619203789490654", OrderID: "586030483469993439",
		ProductID: "1729429119195974110", SKUID: "9999999999999999999",
		ItemCode: "AH-0006", UnitCode: "แท่ง", QuantityMultiplier: 1,
	})
	if !errors.Is(err, ErrTikTokBillShadowMappingItemNotFound) || repo.previewCalls != 0 {
		t.Fatalf("err=%v preview_calls=%d", err, repo.previewCalls)
	}
}

func TestTikTokBillShadowMappingRejectsInvalidSelectionAndDigest(t *testing.T) {
	loader := &billShadowSourceFake{source: controlledTikTokBillShadowSource()}
	repo := &billShadowMappingRepoFake{}
	service := NewTikTokBillShadowMappingService(loader, repo)

	_, err := service.Preview(context.Background(), TikTokBillShadowMappingSelection{
		ShopID: "shop-a", OrderID: "586030483469993439", ProductID: "1", SKUID: "2",
		ItemCode: "AH-0006", UnitCode: "แท่ง", QuantityMultiplier: 1,
	})
	if !errors.Is(err, ErrTikTokBillShadowMappingInvalidInput) || loader.calls != 0 {
		t.Fatalf("invalid selection err=%v loader_calls=%d", err, loader.calls)
	}

	_, err = service.Confirm(context.Background(), TikTokBillShadowMappingConfirmation{
		Selection: TikTokBillShadowMappingSelection{
			ShopID: "7494619203789490654", OrderID: "586030483469993439",
			ProductID: "1729429119195974110", SKUID: "1729429118580984286",
			ItemCode: "AH-0006", UnitCode: "แท่ง", QuantityMultiplier: 1,
		},
		ImpactDigest: "not-reviewed",
	}, "91e80d9f-aba7-4d9e-89db-e7e4e6d262ef")
	if !errors.Is(err, ErrTikTokBillShadowMappingInvalidInput) || repo.commitCalls != 0 {
		t.Fatalf("invalid digest err=%v commit_calls=%d", err, repo.commitCalls)
	}
}
