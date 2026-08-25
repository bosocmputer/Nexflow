package repository

import (
	"context"
	"errors"
	"sort"
	"strings"
)

var ErrMarketplaceSharedPoolPolicyLocked = errors.New("marketplace shared pool member has an explicit stock policy")

type MarketplaceSharedPoolMember struct {
	ItemID  int64
	ModelID int64
}

type marketplaceSharedPoolAlias struct {
	itemID, modelID    int64
	aliasID            string
	itemCode           string
	unitCode           string
	quantityMultiplier int64
	salesEnabled       bool
	stockPolicy        string
	conversionStatus   string
	scopeConfirmed     bool
}

func marketplaceSharedPoolPolicyAction(policy, conversionStatus string) (bool, error) {
	if strings.TrimSpace(conversionStatus) != "ready" {
		return false, ErrMarketplaceUnitNotReady
	}
	switch strings.TrimSpace(policy) {
	case "managed":
		return false, nil
	case "blocked":
		return true, nil
	default:
		return false, ErrMarketplaceSharedPoolPolicyLocked
	}
}

func (r *MarketplaceAliasRepo) AutoManageShopeeSharedPool(
	ctx context.Context,
	shopID int64,
	smlItemCode string,
	members []MarketplaceSharedPoolMember,
	userID string,
) (int, error) {
	if r == nil || shopID <= 0 || strings.TrimSpace(smlItemCode) == "" || len(members) < 2 {
		return 0, ErrMarketplaceImpactChanged
	}
	wanted := make(map[[2]int64]struct{}, len(members))
	for _, member := range members {
		key := [2]int64{member.ItemID, member.ModelID}
		if member.ItemID <= 0 || member.ModelID < 0 {
			return 0, ErrMarketplaceImpactChanged
		}
		if _, duplicate := wanted[key]; duplicate {
			return 0, ErrMarketplaceImpactChanged
		}
		wanted[key] = struct{}{}
	}

	rows, err := r.db.QueryContext(ctx, `SELECT m.item_id,m.model_id,COALESCE(a.id::text,''),
		a.item_code,a.unit_code,a.quantity_multiplier,a.sales_enabled,a.stock_policy,a.conversion_status,a.scope_confirmed
		FROM shopee_stock_mappings m
		JOIN shopee_stock_products p USING(shop_id,item_id,model_id)
		JOIN marketplace_item_aliases a ON a.id=m.marketplace_alias_id AND a.is_active=true
		WHERE m.shop_id=$1 AND m.sml_item_code=$2 AND m.excluded=false AND p.is_active=true
		ORDER BY m.item_id,m.model_id`, shopID, strings.TrimSpace(smlItemCode))
	if err != nil {
		return 0, err
	}
	aliases := []marketplaceSharedPoolAlias{}
	for rows.Next() {
		var alias marketplaceSharedPoolAlias
		if err := rows.Scan(&alias.itemID, &alias.modelID, &alias.aliasID, &alias.itemCode, &alias.unitCode,
			&alias.quantityMultiplier, &alias.salesEnabled, &alias.stockPolicy, &alias.conversionStatus,
			&alias.scopeConfirmed); err != nil {
			rows.Close()
			return 0, err
		}
		aliases = append(aliases, alias)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if len(aliases) != len(wanted) {
		return 0, ErrMarketplaceImpactChanged
	}
	for _, alias := range aliases {
		key := [2]int64{alias.itemID, alias.modelID}
		if _, ok := wanted[key]; !ok || alias.aliasID == "" || alias.itemCode != strings.TrimSpace(smlItemCode) {
			return 0, ErrMarketplaceImpactChanged
		}
		delete(wanted, key)
	}
	if len(wanted) != 0 {
		return 0, ErrMarketplaceImpactChanged
	}

	// Keep mutation and lease ordering stable even if a future caller sends a
	// different member order.
	sort.Slice(aliases, func(i, j int) bool {
		if aliases[i].itemID == aliases[j].itemID {
			return aliases[i].modelID < aliases[j].modelID
		}
		return aliases[i].itemID < aliases[j].itemID
	})
	promoted := 0
	for _, alias := range aliases {
		shouldPromote, err := marketplaceSharedPoolPolicyAction(alias.stockPolicy, alias.conversionStatus)
		if err != nil {
			return promoted, err
		}
		if !shouldPromote {
			continue
		}
		proposal := MarketplaceAliasProposal{
			AliasID: alias.aliasID, BillType: "sale", ItemCode: alias.itemCode, UnitCode: alias.unitCode,
			QuantityMultiplier: alias.quantityMultiplier, SalesEnabled: &alias.salesEnabled, StockPolicy: "managed",
			ScopeConfirmed: &alias.scopeConfirmed, MatchMethod: "manual_identity", ConfirmedBy: userID,
		}
		impact, err := r.PreviewMutation(ctx, proposal)
		if err != nil {
			return promoted, err
		}
		proposal.ExpectedRevision = impact.CurrentMappingRevision
		proposal.ExpectedImpactDigest = impact.ImpactDigest
		if _, err := r.CommitMutation(ctx, proposal); err != nil {
			return promoted, err
		}
		promoted++
	}
	return promoted, nil
}
