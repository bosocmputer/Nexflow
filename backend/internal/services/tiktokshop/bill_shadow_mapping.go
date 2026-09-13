package tiktokshop

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"

	"nexflow/internal/models"
	"nexflow/internal/repository"
)

var (
	ErrTikTokBillShadowMappingInvalidInput = errors.New("invalid TikTok Shop bill shadow mapping input")
	ErrTikTokBillShadowMappingItemNotFound = errors.New("TikTok Shop bill shadow mapping item not found")
)

type TikTokBillShadowMappingSelection struct {
	ShopID             string `json:"-"`
	OrderID            string `json:"-"`
	ProductID          string `json:"product_id"`
	SKUID              string `json:"sku_id"`
	ItemCode           string `json:"item_code"`
	UnitCode           string `json:"unit_code"`
	QuantityMultiplier int64  `json:"quantity_multiplier"`
}

type TikTokBillShadowMappingConfirmation struct {
	Selection               TikTokBillShadowMappingSelection `json:"-"`
	ExpectedMappingRevision int64                            `json:"expected_mapping_revision"`
	ImpactDigest            string                           `json:"impact_digest"`
}

type TikTokBillShadowMappingRepository interface {
	PreviewMutation(context.Context, repository.MarketplaceAliasProposal) (models.MarketplaceAliasImpact, error)
	CommitMutation(context.Context, repository.MarketplaceAliasProposal) (*repository.MarketplaceAliasCommitResult, error)
}

type TikTokBillShadowMappingService struct {
	sourceLoader TikTokBillShadowSourceLoader
	aliases      TikTokBillShadowMappingRepository
}

func NewTikTokBillShadowMappingService(sourceLoader TikTokBillShadowSourceLoader, aliases TikTokBillShadowMappingRepository) *TikTokBillShadowMappingService {
	return &TikTokBillShadowMappingService{sourceLoader: sourceLoader, aliases: aliases}
}

func (s *TikTokBillShadowMappingService) Preview(ctx context.Context, selection TikTokBillShadowMappingSelection) (models.MarketplaceAliasImpact, error) {
	proposal, err := s.proposal(ctx, selection)
	if err != nil {
		return models.MarketplaceAliasImpact{}, err
	}
	return s.aliases.PreviewMutation(ctx, proposal)
}

func (s *TikTokBillShadowMappingService) Confirm(ctx context.Context, confirmation TikTokBillShadowMappingConfirmation, actorID string) (*repository.MarketplaceAliasCommitResult, error) {
	if !validTikTokBillShadowImpactDigest(confirmation.ImpactDigest) || strings.TrimSpace(actorID) == "" || confirmation.ExpectedMappingRevision < 0 {
		return nil, ErrTikTokBillShadowMappingInvalidInput
	}
	proposal, err := s.proposal(ctx, confirmation.Selection)
	if err != nil {
		return nil, err
	}
	proposal.ConfirmedBy = strings.TrimSpace(actorID)
	proposal.ExpectedRevision = confirmation.ExpectedMappingRevision
	proposal.ExpectedImpactDigest = strings.TrimSpace(confirmation.ImpactDigest)
	return s.aliases.CommitMutation(ctx, proposal)
}

func (s *TikTokBillShadowMappingService) proposal(ctx context.Context, selection TikTokBillShadowMappingSelection) (repository.MarketplaceAliasProposal, error) {
	selection.ShopID = strings.TrimSpace(selection.ShopID)
	selection.OrderID = strings.TrimSpace(selection.OrderID)
	selection.ProductID = strings.TrimSpace(selection.ProductID)
	selection.SKUID = strings.TrimSpace(selection.SKUID)
	selection.ItemCode = strings.TrimSpace(selection.ItemCode)
	selection.UnitCode = strings.TrimSpace(selection.UnitCode)
	if s == nil || s.sourceLoader == nil || s.aliases == nil || !ValidTikTokShopID(selection.ShopID) ||
		!ValidTikTokShopID(selection.OrderID) || !ValidTikTokShopID(selection.ProductID) || !ValidTikTokShopID(selection.SKUID) ||
		selection.ItemCode == "" || len(selection.ItemCode) > 128 || selection.UnitCode == "" || len(selection.UnitCode) > 128 ||
		selection.QuantityMultiplier < 1 || selection.QuantityMultiplier > 1_000_000 {
		return repository.MarketplaceAliasProposal{}, ErrTikTokBillShadowMappingInvalidInput
	}

	source, err := s.sourceLoader.Load(ctx, selection.ShopID, selection.OrderID)
	if err != nil {
		return repository.MarketplaceAliasProposal{}, err
	}
	if source == nil || source.ShopID != selection.ShopID || source.Order.ID != selection.OrderID {
		return repository.MarketplaceAliasProposal{}, ErrTikTokBillShadowSourceInvalid
	}
	var matched *NormalizedTikTokOrderItem
	for index := range source.Items {
		candidate := &source.Items[index]
		if candidate.ProductID == selection.ProductID && candidate.SKUID == selection.SKUID {
			matched = candidate
			break
		}
	}
	if matched == nil {
		return repository.MarketplaceAliasProposal{}, ErrTikTokBillShadowMappingItemNotFound
	}

	rawName := strings.TrimSpace(matched.ProductName)
	if variant := strings.TrimSpace(matched.SKUName); variant != "" {
		rawName = strings.TrimSpace(rawName + " / " + variant)
	}
	salesEnabled := true
	scopeConfirmed := true
	return repository.MarketplaceAliasProposal{
		Identity: models.MarketplaceAliasIdentity{
			Source: "tiktok", AccountKey: "shop:" + selection.ShopID,
			ExternalItemID: selection.ProductID, ExternalVariantID: selection.SKUID,
			SourceSKU: strings.TrimSpace(matched.SellerSKU), RawName: rawName,
		},
		BillType: "sale", ItemCode: selection.ItemCode, UnitCode: selection.UnitCode,
		QuantityMultiplier: selection.QuantityMultiplier, SalesEnabled: &salesEnabled,
		StockPolicy: "blocked", ScopeConfirmed: &scopeConfirmed, MatchMethod: "manual_identity",
	}, nil
}

func validTikTokBillShadowImpactDigest(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}
