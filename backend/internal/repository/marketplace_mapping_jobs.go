package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
	"go.uber.org/zap"

	"nexflow/internal/models"
)

var (
	ErrMarketplaceImpactChanged = errors.New("marketplace mapping impact changed")
	ErrMarketplaceUnitNotReady  = errors.New("marketplace unit is not available in the active catalog generation")
)

type MarketplaceAliasProposal struct {
	AliasID              string
	Identity             models.MarketplaceAliasIdentity
	BillType             string
	ItemCode             string
	UnitCode             string
	QuantityMultiplier   int64
	SalesEnabled         *bool
	StockPolicy          string
	ScopeConfirmed       *bool
	MatchMethod          string
	ConfirmedBy          string
	ExpectedRevision     int64
	ExpectedImpactDigest string
	Deactivate           bool
}

type MarketplaceAliasCommitResult struct {
	Alias     *models.MarketplaceItemAlias      `json:"alias,omitempty"`
	Job       models.MarketplaceMappingJob      `json:"job"`
	PolicyJob *models.MarketplaceStockPolicyJob `json:"policy_job,omitempty"`
	Impact    models.MarketplaceAliasImpact     `json:"impact"`
}

type marketplaceMutationTarget struct {
	Identity           models.MarketplaceAliasIdentity `json:"identity"`
	BillType           string                          `json:"bill_type"`
	ItemCode           string                          `json:"item_code"`
	UnitCode           string                          `json:"unit_code"`
	QuantityMultiplier int64                           `json:"quantity_multiplier"`
	StandValue         string                          `json:"stand_value"`
	DivideValue        string                          `json:"divide_value"`
	CatalogGeneration  string                          `json:"catalog_generation"`
	ConversionStatus   string                          `json:"conversion_status"`
	SalesEnabled       bool                            `json:"sales_enabled"`
	StockPolicy        string                          `json:"stock_policy"`
	ScopeConfirmed     bool                            `json:"scope_confirmed"`
	Deactivate         bool                            `json:"deactivate"`
	OldItemCode        string                          `json:"old_item_code"`
	OldUnitCode        string                          `json:"old_unit_code"`
	OldRevision        int64                           `json:"old_revision"`
	SetDefinitionHash  string                          `json:"set_definition_hash"`
	AffectedShopIDs    []int64                         `json:"affected_shop_ids"`
	RequestedAt        time.Time                       `json:"requested_at"`
}

type marketplaceImpactQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type marketplaceAliasCurrent struct {
	ID                 string
	Identity           models.MarketplaceAliasIdentity
	ItemCode           string
	UnitCode           string
	QuantityMultiplier int64
	StandValue         string
	DivideValue        string
	CatalogGeneration  string
	ConversionStatus   string
	SalesEnabled       bool
	StockPolicy        string
	ScopeConfirmed     bool
	MappingRevision    int64
	IsActive           bool
}

func (r *MarketplaceAliasRepo) PreviewMutation(ctx context.Context, proposal MarketplaceAliasProposal) (models.MarketplaceAliasImpact, error) {
	return previewMarketplaceMutation(ctx, r.db, proposal)
}

func previewMarketplaceMutation(ctx context.Context, q marketplaceImpactQueryer, proposal MarketplaceAliasProposal) (models.MarketplaceAliasImpact, error) {
	proposal, current, target, err := resolveMarketplaceMutation(ctx, q, proposal)
	if err != nil {
		return models.MarketplaceAliasImpact{}, err
	}
	identity := target.Identity
	where, args := billItemIdentityWhere(identity, 1)
	var impact models.MarketplaceAliasImpact
	impact.CurrentMappingRevision = current.MappingRevision
	if err := q.QueryRowContext(ctx, `SELECT
		COUNT(*) FILTER (WHERE b.archived_at IS NULL AND b.current_sml_attempt_id IS NULL
		  AND b.status IN ('pending','needs_review','failed')),
		COUNT(DISTINCT b.id) FILTER (WHERE b.archived_at IS NULL AND b.current_sml_attempt_id IS NULL
		  AND b.status IN ('pending','needs_review','failed')),
		COUNT(*) FILTER (WHERE b.current_sml_attempt_id IS NOT NULL OR b.status='sent'),
		COUNT(*) FILTER (WHERE b.archived_at IS NOT NULL),
		COUNT(*) FILTER (WHERE b.archived_at IS NULL AND b.current_sml_attempt_id IS NULL
		  AND b.status IN ('pending','needs_review','failed') AND bi.conversion_override_fields<>'{}'::jsonb)
		FROM bill_items bi JOIN bills b ON b.id=bi.bill_id WHERE (`+where+`)`, args...).Scan(
		&impact.OpenItems, &impact.OpenBills, &impact.AttemptedItems, &impact.ArchivedItems, &impact.ManualOverrideItems); err != nil {
		return impact, err
	}

	reservationWhere, reservationArgs := reservationIdentityWhere(identity, proposal.AliasID, 1)
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*),COUNT(*) FILTER (
		WHERE state IN ('active','blocked_mapping') AND
		 (sml_item_code IS DISTINCT FROM $`+strconv.Itoa(len(reservationArgs)+1)+` OR mapping_revision IS DISTINCT FROM $`+strconv.Itoa(len(reservationArgs)+2)+`))
		FROM marketplace_stock_reservations WHERE (`+reservationWhere+`)
		AND state IN ('active','blocked_mapping','sending_sml','awaiting_stock_recalc')`,
		append(reservationArgs, target.ItemCode, current.MappingRevision+1)...).Scan(&impact.Reservations, &impact.ReservationMoves); err != nil {
		return impact, err
	}

	codes := nonEmptyUnique(current.ItemCode, target.ItemCode)
	if proposal.AliasID != "" {
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM shopee_stock_mappings WHERE marketplace_alias_id=$1::uuid`, proposal.AliasID).Scan(&impact.StockMappings); err != nil {
			return impact, err
		}
	}
	if len(codes) > 0 {
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM shopee_stock_mappings m
			WHERE m.excluded=false AND m.sml_item_code=$1
			  AND (NULLIF($2,'')::uuid IS NULL OR m.marketplace_alias_id IS DISTINCT FROM NULLIF($2,'')::uuid)`, target.ItemCode, proposal.AliasID).Scan(&impact.StockConflicts); err != nil {
			return impact, err
		}
	}
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT m.shop_id,COALESCE(st.config_version,0)
		FROM shopee_stock_mappings m
		LEFT JOIN shopee_stock_settings st ON st.shop_id=m.shop_id
		WHERE (NULLIF($1,'')::uuid IS NULL OR m.marketplace_alias_id=NULLIF($1,'')::uuid)
		   OR m.sml_item_code=ANY($2::text[])
		   OR EXISTS (SELECT 1 FROM sml_catalog_set_components c
		       WHERE c.parent_item_code=m.sml_item_code AND c.component_item_code=ANY($2::text[]))
		ORDER BY m.shop_id`, proposal.AliasID, pq.Array(codes))
	if err != nil {
		return impact, err
	}
	impact.StockConfigVersions = map[string]int64{}
	for rows.Next() {
		var shopID, version int64
		if err := rows.Scan(&shopID, &version); err != nil {
			rows.Close()
			return impact, err
		}
		impact.AffectedShopIDs = append(impact.AffectedShopIDs, shopID)
		impact.StockConfigVersions[strconv.FormatInt(shopID, 10)] = version
	}
	if err := rows.Close(); err != nil {
		return impact, err
	}
	if shopID, itemID, modelID, ok := shopeeMutationIDs(identity); ok {
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM shopee_stock_mappings
			WHERE (NULLIF($1,'')::uuid IS NOT NULL AND marketplace_alias_id=NULLIF($1,'')::uuid)
			   OR (shop_id=$2 AND item_id=$3 AND model_id=$4)`, proposal.AliasID, shopID, itemID, modelID).Scan(&impact.StockMappings); err != nil {
			return impact, err
		}
		var configVersion int64
		if err := q.QueryRowContext(ctx, `SELECT COALESCE(config_version,0) FROM shopee_stock_settings WHERE shop_id=$1`, shopID).Scan(&configVersion); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return impact, err
		}
		if impact.StockConfigVersions == nil {
			impact.StockConfigVersions = map[string]int64{}
		}
		impact.StockConfigVersions[strconv.FormatInt(shopID, 10)] = configVersion
		impact.AffectedShopIDs = appendUniqueInt64(impact.AffectedShopIDs, shopID)
		sort.Slice(impact.AffectedShopIDs, func(i, j int) bool { return impact.AffectedShopIDs[i] < impact.AffectedShopIDs[j] })
	}
	if len(impact.AffectedShopIDs) > 0 {
		if err := q.QueryRowContext(ctx, `SELECT
			COUNT(*) FILTER (WHERE manual_unit_factor IS NOT NULL),
			COUNT(*) FILTER (WHERE excluded=true)
			FROM shopee_stock_mappings WHERE shop_id=ANY($1::bigint[])
			  AND ((NULLIF($2,'')::uuid IS NULL OR marketplace_alias_id=NULLIF($2,'')::uuid) OR sml_item_code=ANY($3::text[]))`,
			pq.Array(impact.AffectedShopIDs), proposal.AliasID, pq.Array(codes)).Scan(&impact.LegacyManualFactors, &impact.LegacyExclusions); err != nil {
			return impact, err
		}
	}
	impact.BeforeFormula = marketplaceFormula(current.QuantityMultiplier, current.UnitCode, current.StandValue, current.DivideValue)
	impact.AfterFormula = marketplaceFormula(target.QuantityMultiplier, target.UnitCode, target.StandValue, target.DivideValue)
	impact.ConversionStatus = target.ConversionStatus
	impact.DryRunRequired = len(impact.AffectedShopIDs) > 0
	impact.ImpactDigest = marketplaceImpactDigest(impact, target)
	return impact, nil
}

func resolveMarketplaceMutation(ctx context.Context, q marketplaceImpactQueryer, proposal MarketplaceAliasProposal) (MarketplaceAliasProposal, marketplaceAliasCurrent, marketplaceMutationTarget, error) {
	proposal.Identity = normalizeAliasIdentity(proposal.Identity)
	proposal.AliasID = strings.TrimSpace(proposal.AliasID)
	proposal.ItemCode = strings.TrimSpace(proposal.ItemCode)
	proposal.UnitCode = strings.TrimSpace(proposal.UnitCode)
	proposal.BillType = strings.TrimSpace(proposal.BillType)
	if proposal.BillType == "" {
		proposal.BillType = "sale"
	}
	if proposal.StockPolicy != "" && !validMarketplaceStockPolicy(proposal.StockPolicy) {
		return proposal, marketplaceAliasCurrent{}, marketplaceMutationTarget{}, fmt.Errorf("invalid marketplace stock policy")
	}
	if proposal.Identity.Source != "" && proposal.Identity.Source != "shopee" && proposal.StockPolicy != "" && proposal.StockPolicy != "blocked" {
		return proposal, marketplaceAliasCurrent{}, marketplaceMutationTarget{}, fmt.Errorf("stock policy is only available for Shopee")
	}
	if proposal.ItemCode == "" && !proposal.Deactivate {
		return proposal, marketplaceAliasCurrent{}, marketplaceMutationTarget{}, fmt.Errorf("item code is required")
	}
	current := marketplaceAliasCurrent{QuantityMultiplier: 1, SalesEnabled: true, StockPolicy: "blocked", IsActive: true}
	if proposal.AliasID != "" {
		var stand, divide, generation sql.NullString
		err := q.QueryRowContext(ctx, `SELECT id::text,source,account_key,external_item_id,external_variant_id,source_sku,raw_name,normalized_key,
			item_code,unit_code,quantity_multiplier,unit_stand_value::text,unit_divide_value::text,unit_catalog_generation::text,
			conversion_status,sales_enabled,stock_policy,scope_confirmed,mapping_revision,is_active
			FROM marketplace_item_aliases WHERE id=$1::uuid`, proposal.AliasID).Scan(
			&current.ID, &current.Identity.Source, &current.Identity.AccountKey, &current.Identity.ExternalItemID,
			&current.Identity.ExternalVariantID, &current.Identity.SourceSKU, &current.Identity.RawName, &current.Identity.NormalizedKey,
			&current.ItemCode, &current.UnitCode, &current.QuantityMultiplier, &stand, &divide, &generation,
			&current.ConversionStatus, &current.SalesEnabled, &current.StockPolicy, &current.ScopeConfirmed, &current.MappingRevision, &current.IsActive)
		if errors.Is(err, sql.ErrNoRows) {
			return proposal, current, marketplaceMutationTarget{}, ErrMarketplaceAliasConflict
		}
		if err != nil {
			return proposal, current, marketplaceMutationTarget{}, err
		}
		current.StandValue, current.DivideValue, current.CatalogGeneration = stand.String, divide.String, generation.String
		proposal.Identity = current.Identity
		if proposal.ItemCode == "" {
			proposal.ItemCode = current.ItemCode
		}
		if proposal.UnitCode == "" {
			proposal.UnitCode = current.UnitCode
		}
	}
	if proposal.QuantityMultiplier == 0 {
		proposal.QuantityMultiplier = current.QuantityMultiplier
		if proposal.QuantityMultiplier == 0 {
			proposal.QuantityMultiplier = 1
		}
	}
	if proposal.QuantityMultiplier < 1 || proposal.QuantityMultiplier > 1_000_000 {
		return proposal, marketplaceAliasCurrent{}, marketplaceMutationTarget{}, fmt.Errorf("quantity multiplier must be between 1 and 1000000")
	}
	if proposal.StockPolicy == "" {
		proposal.StockPolicy = current.StockPolicy
		if proposal.StockPolicy == "" {
			proposal.StockPolicy = "blocked"
		}
	}
	if targetSource := firstNonEmptyRepository(proposal.Identity.Source, current.Identity.Source); targetSource != "shopee" && proposal.StockPolicy != "blocked" {
		return proposal, marketplaceAliasCurrent{}, marketplaceMutationTarget{}, fmt.Errorf("stock policy is only available for Shopee")
	}
	salesEnabled := current.SalesEnabled
	if proposal.SalesEnabled != nil {
		salesEnabled = *proposal.SalesEnabled
	}
	scopeConfirmed := current.ScopeConfirmed
	if proposal.ScopeConfirmed != nil {
		scopeConfirmed = *proposal.ScopeConfirmed
	}
	if proposal.Deactivate {
		salesEnabled = false
		proposal.StockPolicy = "blocked"
		proposal.ItemCode = current.ItemCode
		proposal.UnitCode = current.UnitCode
	}
	target := marketplaceMutationTarget{
		Identity: proposal.Identity, BillType: proposal.BillType, ItemCode: proposal.ItemCode, UnitCode: proposal.UnitCode,
		QuantityMultiplier: proposal.QuantityMultiplier, SalesEnabled: salesEnabled, StockPolicy: proposal.StockPolicy,
		ScopeConfirmed: scopeConfirmed, Deactivate: proposal.Deactivate, OldItemCode: current.ItemCode,
		OldUnitCode: current.UnitCode, OldRevision: current.MappingRevision,
	}
	var productActive, setDocumentValid bool
	var defaultUnit, setHash string
	var itemType int
	err := q.QueryRowContext(ctx, `SELECT is_active,unit_code,item_type,set_document_valid,set_definition_hash
		FROM sml_catalog WHERE item_code=$1`, target.ItemCode).Scan(&productActive, &defaultUnit, &itemType, &setDocumentValid, &setHash)
	if errors.Is(err, sql.ErrNoRows) && proposal.Deactivate {
		err = nil
	}
	if err != nil {
		return proposal, current, target, err
	}
	if !proposal.Deactivate && (!productActive || (itemType == 3 && !setDocumentValid)) {
		return proposal, current, target, fmt.Errorf("SML product is inactive or set definition is not document-ready")
	}
	if target.UnitCode == "" {
		target.UnitCode = defaultUnit
		proposal.UnitCode = defaultUnit
	}
	target.SetDefinitionHash = setHash
	var generation, stand, divide sql.NullString
	err = q.QueryRowContext(ctx, `SELECT r.id::text,u.stand_value::text,u.divide_value::text
		FROM sml_catalog_sync_runs r JOIN sml_catalog_units u ON u.generation_id=r.id
		WHERE r.status='active' AND u.item_code=$1 AND u.unit_code=$2 AND u.is_active=true
		ORDER BY r.activated_at DESC NULLS LAST LIMIT 1`, target.ItemCode, target.UnitCode).Scan(&generation, &stand, &divide)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return proposal, current, target, err
	}
	target.ConversionStatus = "needs_review"
	if err == nil && generation.Valid && stand.Valid && divide.Valid && target.ScopeConfirmed && !target.Deactivate {
		target.CatalogGeneration, target.StandValue, target.DivideValue = generation.String, stand.String, divide.String
		target.ConversionStatus = "ready"
	}
	return proposal, current, target, nil
}

func (r *MarketplaceAliasRepo) CommitMutation(ctx context.Context, proposal MarketplaceAliasProposal) (*MarketplaceAliasCommitResult, error) {
	preliminary, err := r.PreviewMutation(ctx, proposal)
	if err != nil {
		return nil, err
	}
	if proposal.ExpectedImpactDigest != "" && proposal.ExpectedImpactDigest != preliminary.ImpactDigest {
		return nil, ErrMarketplaceImpactChanged
	}
	owner := fmt.Sprintf("mapping:%d", time.Now().UnixNano())
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Keep the interactive configuration transaction bounded. A conflicting
	// stock run or editor must surface as a retryable conflict instead of
	// holding the API connection indefinitely.
	if _, err := tx.ExecContext(ctx, `SET LOCAL lock_timeout='1s'`); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `SET LOCAL statement_timeout='5s'`); err != nil {
		return nil, err
	}
	for _, shopID := range preliminary.AffectedShopIDs {
		var token int64
		err := tx.QueryRowContext(ctx, `INSERT INTO shopee_stock_leases(shop_id,owner_id,lease_until,fencing_token,heartbeat_at)
			VALUES($1,$2,NOW()+INTERVAL '2 minutes',1,NOW())
			ON CONFLICT(shop_id) DO UPDATE SET owner_id=EXCLUDED.owner_id,lease_until=EXCLUDED.lease_until,
			  fencing_token=shopee_stock_leases.fencing_token+1,heartbeat_at=NOW(),updated_at=NOW()
			WHERE shopee_stock_leases.lease_until<NOW() RETURNING fencing_token`, shopID, owner).Scan(&token)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrMarketplaceImpactChanged
		}
		if err != nil {
			return nil, err
		}
	}
	if proposal.AliasID != "" {
		var locked string
		if err := tx.QueryRowContext(ctx, `SELECT id::text FROM marketplace_item_aliases WHERE id=$1::uuid FOR UPDATE`, proposal.AliasID).Scan(&locked); err != nil {
			return nil, ErrMarketplaceAliasConflict
		}
	} else {
		identity := normalizeAliasIdentity(proposal.Identity)
		identityKey := strings.Join([]string{identity.Source, identity.AccountKey, identity.ExternalItemID, identity.ExternalVariantID, identity.SourceSKU, identity.NormalizedKey}, "\x1f")
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, identityKey); err != nil {
			return nil, err
		}
	}
	if len(preliminary.AffectedShopIDs) > 0 {
		rows, err := tx.QueryContext(ctx, `SELECT shop_id,item_id,model_id FROM shopee_stock_mappings WHERE shop_id=ANY($1::bigint[])
			ORDER BY shop_id,item_id,model_id FOR UPDATE`, pq.Array(preliminary.AffectedShopIDs))
		if err != nil {
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		rows, err = tx.QueryContext(ctx, `SELECT shop_id FROM shopee_stock_settings WHERE shop_id=ANY($1::bigint[])
			ORDER BY shop_id FOR UPDATE`, pq.Array(preliminary.AffectedShopIDs))
		if err != nil {
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	proposal, current, target, err := resolveMarketplaceMutation(ctx, tx, proposal)
	if err != nil {
		return nil, err
	}
	currentImpact, err := previewMarketplaceMutation(ctx, tx, proposal)
	if err != nil {
		return nil, err
	}
	if proposal.ExpectedRevision != current.MappingRevision ||
		(proposal.ExpectedImpactDigest != "" && proposal.ExpectedImpactDigest != currentImpact.ImpactDigest) ||
		currentImpact.ImpactDigest != preliminary.ImpactDigest {
		return nil, ErrMarketplaceImpactChanged
	}
	var aliasID string
	created := proposal.AliasID == ""
	if created {
		where, args := aliasIdentityWhere(target.Identity)
		var existing string
		err := tx.QueryRowContext(ctx, `SELECT id::text FROM marketplace_item_aliases a WHERE `+where+` AND is_active=true`, args...).Scan(&existing)
		if err == nil {
			return nil, ErrMarketplaceAliasConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		alias, err := upsertAliasTx(tx, target.Identity, target.ItemCode, target.UnitCode, proposal.MatchMethod, target.ScopeConfirmed, proposal.ConfirmedBy)
		if err != nil || alias == nil {
			return nil, err
		}
		aliasID = alias.ID
	} else {
		aliasID = proposal.AliasID
	}
	targetRevision := current.MappingRevision + 1
	expectedStoredRevision := current.MappingRevision
	if created {
		targetRevision = 1
		expectedStoredRevision = 1
	}
	result, err := tx.ExecContext(ctx, `UPDATE marketplace_item_aliases SET
		item_code=$2,unit_code=$3,quantity_multiplier=$4,
		unit_stand_value=NULLIF($5,'')::numeric,unit_divide_value=NULLIF($6,'')::numeric,
		unit_catalog_generation=NULLIF($7,'')::uuid,conversion_status=$8,sales_enabled=$9,stock_policy=$10,
		scope_confirmed=$11,is_active=$12,mapping_revision=$13,confirmed_by=COALESCE(NULLIF($14,'')::uuid,confirmed_by),updated_at=NOW()
		WHERE id=$1::uuid AND mapping_revision=$15`, aliasID, target.ItemCode, target.UnitCode, target.QuantityMultiplier,
		target.StandValue, target.DivideValue, target.CatalogGeneration, target.ConversionStatus, target.SalesEnabled,
		target.StockPolicy, target.ScopeConfirmed, !target.Deactivate, targetRevision, proposal.ConfirmedBy, expectedStoredRevision)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return nil, ErrMarketplaceAliasConflict
	}
	if shopID, itemID, modelID, ok := shopeeMutationIDs(target.Identity); ok {
		result, err := tx.ExecContext(ctx, `UPDATE shopee_stock_mappings SET marketplace_alias_id=$4::uuid,
			warning_codes=CASE WHEN warning_codes ? 'master_revision_pending' THEN warning_codes ELSE warning_codes||'["master_revision_pending"]'::jsonb END,
			shared_pool_enabled=false,updated_at=NOW()
			WHERE shop_id=$1 AND item_id=$2 AND model_id=$3`, shopID, itemID, modelID, aliasID)
		if err != nil {
			return nil, err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return nil, ErrMarketplaceAliasConflict
		}
	}
	target.OldRevision = current.MappingRevision
	target.RequestedAt = time.Now().UTC()
	target.AffectedShopIDs = append([]int64(nil), currentImpact.AffectedShopIDs...)
	snapshotJSON, err := json.Marshal(target)
	if err != nil {
		return nil, err
	}
	jobID := ""
	idempotencyKey := fmt.Sprintf("%s:%d", aliasID, targetRevision)
	err = tx.QueryRowContext(ctx, `INSERT INTO marketplace_mapping_jobs
		(alias_id,target_revision,job_type,status,impact_digest,dependency_snapshot,requested_by,idempotency_key)
		VALUES($1::uuid,$2,'mapping_reconcile','queued',$3,$4,NULLIF($5,'')::uuid,$6)
		ON CONFLICT (idempotency_key) WHERE idempotency_key IS NOT NULL DO UPDATE SET updated_at=NOW()
		RETURNING id::text`, aliasID, targetRevision, currentImpact.ImpactDigest, snapshotJSON, proposal.ConfirmedBy, idempotencyKey).Scan(&jobID)
	if err != nil {
		return nil, err
	}
	var policyJob *models.MarketplaceStockPolicyJob
	if target.StockPolicy == "zeroing" {
		shopID, itemID, modelID, ok := shopeeMutationIDs(target.Identity)
		if !ok {
			return nil, fmt.Errorf("zeroing policy requires an exact Shopee variant identity")
		}
		policyJobID := ""
		policyKey := fmt.Sprintf("%s:%d:zero_then_disable", aliasID, targetRevision)
		err := tx.QueryRowContext(ctx, `INSERT INTO shopee_stock_policy_jobs
			(shop_id,marketplace_alias_id,target_revision,item_id,model_id,policy_action,status,idempotency_key,request_hash,requested_by)
			VALUES($1,$2::uuid,$3,$4,$5,'zero_then_disable','queued',$6,$7,NULLIF($8,'')::uuid)
			ON CONFLICT(idempotency_key) DO UPDATE SET updated_at=NOW()
			RETURNING id::text`, shopID, aliasID, targetRevision, itemID, modelID, policyKey, currentImpact.ImpactDigest, proposal.ConfirmedBy).Scan(&policyJobID)
		if err != nil {
			return nil, err
		}
		policyJob = &models.MarketplaceStockPolicyJob{ID: policyJobID, ShopID: shopID, MarketplaceAlias: aliasID,
			TargetRevision: targetRevision, ItemID: itemID, ModelID: modelID, PolicyAction: "zero_then_disable", Status: "queued", CreatedAt: time.Now()}
	}
	if target.StockPolicy == "manual_unmanaged" {
		if _, err := tx.ExecContext(ctx, `UPDATE marketplace_item_aliases SET stock_policy_acknowledged_at=NOW(),
			stock_policy_acknowledged_by=NULLIF($2,'')::uuid WHERE id=$1::uuid`, aliasID, proposal.ConfirmedBy); err != nil {
			return nil, err
		}
	}
	if len(currentImpact.AffectedShopIDs) > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE shopee_stock_settings SET enabled=false,dry_run_required=true,
			paused_reason='marketplace_mapping_reconcile',config_version=config_version+1,updated_at=NOW()
			WHERE shop_id=ANY($1::bigint[])`, pq.Array(currentImpact.AffectedShopIDs)); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE shopee_stock_mappings SET
			warning_codes=CASE WHEN warning_codes ? 'master_revision_pending' THEN warning_codes ELSE warning_codes||'["master_revision_pending"]'::jsonb END,
			updated_at=NOW() WHERE marketplace_alias_id=$1::uuid`, aliasID); err != nil {
			return nil, err
		}
	}
	beforeJSON, _ := json.Marshal(current)
	afterJSON, _ := json.Marshal(target)
	action := "marketplace_alias_updated"
	if created {
		action = "marketplace_alias_confirmed"
	} else if target.Deactivate {
		action = "marketplace_alias_deactivated"
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_logs
		(action,user_id,source,level,target_id,revision,job_id,before_state,after_state,detail)
		VALUES($1,NULLIF($2,'')::uuid,$3,'info',$4::uuid,$5,$6::uuid,$7,$8,
		  jsonb_build_object('impact_digest',$9,'affected_shop_ids',$10::jsonb))`, action, proposal.ConfirmedBy,
		target.Identity.Source, aliasID, targetRevision, jobID, beforeJSON, afterJSON, currentImpact.ImpactDigest, mustJSON(currentImpact.AffectedShopIDs)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if len(currentImpact.AffectedShopIDs) > 0 {
		_, _ = r.db.ExecContext(context.Background(), `DELETE FROM shopee_stock_leases WHERE owner_id=$1`, owner)
	}
	alias, _ := r.getAliasByIDAny(ctx, aliasID)
	return &MarketplaceAliasCommitResult{
		Alias:     alias,
		Job:       models.MarketplaceMappingJob{ID: jobID, AliasID: aliasID, TargetRevision: targetRevision, JobType: "mapping_reconcile", Status: "queued", CreatedAt: time.Now()},
		PolicyJob: policyJob,
		Impact:    currentImpact,
	}, nil
}

// CommitCurrentMutation is for deterministic system-created masters (for
// example an exact SKU learned during import). Interactive callers must use
// PreviewMutation + CommitMutation with the digest shown to the operator.
func (r *MarketplaceAliasRepo) CommitCurrentMutation(ctx context.Context, proposal MarketplaceAliasProposal) (*MarketplaceAliasCommitResult, error) {
	impact, err := r.PreviewMutation(ctx, proposal)
	if err != nil {
		return nil, err
	}
	proposal.ExpectedRevision = impact.CurrentMappingRevision
	proposal.ExpectedImpactDigest = impact.ImpactDigest
	return r.CommitMutation(ctx, proposal)
}

func (r *MarketplaceAliasRepo) getAliasByIDAny(ctx context.Context, id string) (*models.MarketplaceItemAlias, error) {
	return scanAlias(r.db.QueryRowContext(ctx, `SELECT `+aliasSelectColumns+` FROM marketplace_item_aliases a WHERE a.id=$1::uuid`, id))
}

func reservationIdentityWhere(identity models.MarketplaceAliasIdentity, aliasID string, start int) (string, []any) {
	args := []any{identity.Source, identity.AccountKey}
	clauses := []string{fmt.Sprintf("source=$%d AND account_key=$%d", start, start+1)}
	if identity.ExternalItemID != "" {
		args = append(args, identity.ExternalItemID, identity.ExternalVariantID)
		clauses = append(clauses, fmt.Sprintf("external_item_id=$%d AND external_variant_id=$%d", start+2, start+3))
	} else if identity.ExternalVariantID != "" {
		args = append(args, identity.ExternalVariantID)
		clauses = append(clauses, fmt.Sprintf("external_variant_id=$%d", start+2))
	}
	identityClause := strings.Join(clauses, " AND ")
	if aliasID != "" {
		args = append(args, aliasID)
		return fmt.Sprintf("(%s) OR marketplace_alias_id=$%d::uuid", identityClause, start+len(args)-1), args
	}
	return identityClause, args
}

func marketplaceFormula(multiplier int64, unitCode, stand, divide string) string {
	if multiplier < 1 {
		multiplier = 1
	}
	if stand == "" || divide == "" {
		return fmt.Sprintf("1 Marketplace = %d %s", multiplier, firstNonEmptyRepository(unitCode, "หน่วย SML"))
	}
	return fmt.Sprintf("1 Marketplace = %d %s = %d × %s/%s หน่วยฐาน", multiplier, firstNonEmptyRepository(unitCode, "หน่วย SML"), multiplier, stand, divide)
}

func marketplaceImpactDigest(impact models.MarketplaceAliasImpact, target marketplaceMutationTarget) string {
	impact.ImpactDigest = ""
	target.RequestedAt = time.Time{}
	raw, _ := json.Marshal(struct {
		Impact models.MarketplaceAliasImpact `json:"impact"`
		Target marketplaceMutationTarget     `json:"target"`
	}{impact, target})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func validMarketplaceStockPolicy(value string) bool {
	switch value {
	case "managed", "zeroing", "disabled_zero", "manual_unmanaged", "blocked":
		return true
	default:
		return false
	}
}

func nonEmptyUnique(values ...string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func appendUniqueInt64(values []int64, value int64) []int64 {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func shopeeMutationIDs(identity models.MarketplaceAliasIdentity) (shopID, itemID, modelID int64, ok bool) {
	if strings.ToLower(strings.TrimSpace(identity.Source)) != "shopee" || !strings.HasPrefix(identity.AccountKey, "shop:") {
		return 0, 0, 0, false
	}
	shopID, err := strconv.ParseInt(strings.TrimPrefix(identity.AccountKey, "shop:"), 10, 64)
	if err != nil || shopID <= 0 {
		return 0, 0, 0, false
	}
	itemID, err = strconv.ParseInt(strings.TrimSpace(identity.ExternalItemID), 10, 64)
	if err != nil || itemID <= 0 {
		return 0, 0, 0, false
	}
	modelRaw := strings.TrimSpace(identity.ExternalVariantID)
	if modelRaw != "" {
		modelID, err = strconv.ParseInt(modelRaw, 10, 64)
		if err != nil || modelID < 0 {
			return 0, 0, 0, false
		}
	}
	return shopID, itemID, modelID, true
}

func mustJSON(value any) []byte {
	raw, _ := json.Marshal(value)
	return raw
}

type claimedMarketplaceMappingJob struct {
	ID             string
	AliasID        string
	TargetRevision int64
	Snapshot       marketplaceMutationTarget
	LeaseOwner     string
	AttemptCount   int
}

func (r *MarketplaceAliasRepo) ClaimMappingJob(ctx context.Context, leaseOwner string, leaseDuration time.Duration) (*claimedMarketplaceMappingJob, error) {
	if leaseDuration <= 0 {
		leaseDuration = 5 * time.Minute
	}
	var job claimedMarketplaceMappingJob
	var snapshot json.RawMessage
	err := r.db.QueryRowContext(ctx, `WITH candidate AS (
		SELECT id FROM marketplace_mapping_jobs
		WHERE status='queued' OR (status='running' AND lease_until<NOW())
		ORDER BY created_at,id FOR UPDATE SKIP LOCKED LIMIT 1
	)
	UPDATE marketplace_mapping_jobs j SET status='running',lease_owner=$1,
		lease_until=NOW()+($2*INTERVAL '1 second'),heartbeat_at=NOW(),attempt_count=attempt_count+1,
		started_at=COALESCE(started_at,NOW()),updated_at=NOW(),error_message=''
	FROM candidate WHERE j.id=candidate.id
	RETURNING j.id::text,j.alias_id::text,j.target_revision,j.dependency_snapshot,j.lease_owner,j.attempt_count`,
		leaseOwner, int64(leaseDuration/time.Second)).Scan(&job.ID, &job.AliasID, &job.TargetRevision, &snapshot, &job.LeaseOwner, &job.AttemptCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(snapshot, &job.Snapshot); err != nil {
		return nil, fmt.Errorf("decode marketplace mapping job snapshot: %w", err)
	}
	return &job, nil
}

func (r *MarketplaceAliasRepo) heartbeatMappingJob(ctx context.Context, jobID, leaseOwner string, duration time.Duration) error {
	res, err := r.db.ExecContext(ctx, `UPDATE marketplace_mapping_jobs SET
		lease_until=NOW()+($3*INTERVAL '1 second'),heartbeat_at=NOW(),updated_at=NOW()
		WHERE id=$1::uuid AND lease_owner=$2 AND status='running' AND lease_until>NOW()`,
		jobID, leaseOwner, int64(duration/time.Second))
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected != 1 {
		return ErrMarketplaceImpactChanged
	}
	return nil
}

func (r *MarketplaceAliasRepo) processMappingJobBatch(ctx context.Context, job *claimedMarketplaceMappingJob, enforceConversion, reconcileReservations bool) (int, error) {
	if job == nil {
		return 0, errors.New("mapping job is required")
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT mapping_revision FROM marketplace_item_aliases
		WHERE id=$1::uuid FOR SHARE`, job.AliasID).Scan(&revision); err != nil {
		return 0, err
	}
	if revision != job.TargetRevision {
		return 0, ErrMarketplaceImpactChanged
	}
	identityWhere, identityArgs := billItemIdentityWhere(job.Snapshot.Identity, 3)
	args := []any{job.AliasID, job.TargetRevision}
	args = append(args, identityArgs...)
	rows, err := tx.QueryContext(ctx, `SELECT bi.id::text,bi.bill_id::text
		FROM bill_items bi JOIN bills b ON b.id=bi.bill_id
		WHERE (bi.marketplace_alias_id=$1::uuid OR (`+identityWhere+`))
		  AND b.archived_at IS NULL AND b.current_sml_attempt_id IS NULL
		  AND b.status IN ('pending','needs_review','failed')
		  AND COALESCE(bi.mapping_revision_snapshot,0)<>$2
		ORDER BY bi.id FOR UPDATE OF bi SKIP LOCKED LIMIT 200`, args...)
	if err != nil {
		return 0, err
	}
	itemIDs := []string{}
	billSet := map[string]struct{}{}
	for rows.Next() {
		var itemID, billID string
		if err := rows.Scan(&itemID, &billID); err != nil {
			rows.Close()
			return 0, err
		}
		itemIDs = append(itemIDs, itemID)
		billSet[billID] = struct{}{}
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if len(itemIDs) == 0 {
		return 0, tx.Commit()
	}
	billIDs := make([]string, 0, len(billSet))
	for billID := range billSet {
		billIDs = append(billIDs, billID)
	}
	sort.Strings(billIDs)
	issueCode := ""
	if job.Snapshot.Deactivate {
		issueCode = "master_inactive"
	} else if job.Snapshot.ConversionStatus != "ready" {
		issueCode = "conversion_" + job.Snapshot.ConversionStatus
	} else if !job.Snapshot.SalesEnabled {
		issueCode = "sales_disabled"
	}
	mappedWhenCanonical := !job.Snapshot.Deactivate && (!enforceConversion || issueCode == "")
	_, err = tx.ExecContext(ctx, `UPDATE bill_items SET
		marketplace_alias_id=$2::uuid,
		item_code=CASE WHEN conversion_override_fields ? 'item_code' THEN item_code ELSE $3 END,
		unit_code=CASE WHEN conversion_override_fields ? 'unit_code' THEN unit_code ELSE $4 END,
		source_qty=COALESCE(source_qty,qty),gross_amount=COALESCE(gross_amount,ROUND(qty*COALESCE(price,0),2)),
		sml_qty=CASE WHEN $5<>'' AND $6<>'' AND NOT $7 THEN qty*$8::numeric ELSE NULL END,
		quantity_multiplier_snapshot=$8,
		unit_stand_value_snapshot=NULLIF($5,'')::numeric,unit_divide_value_snapshot=NULLIF($6,'')::numeric,
		base_qty_snapshot=CASE WHEN $5<>'' AND $6<>'' AND NOT $7 THEN qty*$8::numeric*$5::numeric/$6::numeric ELSE NULL END,
		mapping_revision_snapshot=$9,unit_catalog_generation_snapshot=NULLIF($10,'')::uuid,
		set_definition_hash_snapshot=$11,
		conversion_issue_code=CASE WHEN conversion_override_fields ?| ARRAY['item_code','unit_code']
		  THEN 'manual_conversion_review_required' ELSE $12 END,
		mapped=CASE WHEN conversion_override_fields ?| ARRAY['item_code','unit_code'] THEN false ELSE $13 END
		WHERE id=ANY($1::uuid[])`, pq.Array(itemIDs), job.AliasID, job.Snapshot.ItemCode, job.Snapshot.UnitCode,
		job.Snapshot.StandValue, job.Snapshot.DivideValue, job.Snapshot.Deactivate, job.Snapshot.QuantityMultiplier,
		job.TargetRevision, job.Snapshot.CatalogGeneration, job.Snapshot.SetDefinitionHash, issueCode, mappedWhenCanonical)
	if err != nil {
		return 0, err
	}
	if reconcileReservations {
		if err := reconcileMappingReservationsTx(ctx, tx, job, billIDs); err != nil {
			return 0, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bills b SET mutation_revision=mutation_revision+1,
		status=CASE
		  WHEN EXISTS (SELECT 1 FROM bill_items bi WHERE bi.bill_id=b.id AND (bi.mapped IS DISTINCT FROM true OR COALESCE(bi.item_code,'')='')) THEN 'needs_review'
		  WHEN b.status='needs_review' THEN 'pending'
		  ELSE b.status END,
		error_msg=CASE WHEN EXISTS (SELECT 1 FROM bill_items bi WHERE bi.bill_id=b.id AND bi.mapped IS DISTINCT FROM true)
		  THEN 'marketplace_conversion_requires_review' ELSE NULL END
		WHERE b.id=ANY($1::uuid[]) AND b.current_sml_attempt_id IS NULL AND b.archived_at IS NULL`, pq.Array(billIDs)); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE marketplace_mapping_jobs SET processed_count=processed_count+$3,
		heartbeat_at=NOW(),lease_until=NOW()+INTERVAL '5 minutes',updated_at=NOW()
		WHERE id=$1::uuid AND lease_owner=$2 AND status='running'`, job.ID, job.LeaseOwner, len(itemIDs)); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(itemIDs), nil
}

func reconcileMappingReservationsTx(ctx context.Context, tx *sql.Tx, job *claimedMarketplaceMappingJob, billIDs []string) error {
	if len(billIDs) == 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO marketplace_stock_demand_versions(warehouse_code,location_code,item_code,revision)
		SELECT DISTINCT r.warehouse_code,r.location_code,r.sml_item_code,1
		FROM marketplace_stock_reservations r WHERE r.bill_id=ANY($1::uuid[]) AND r.state IN ('active','blocked_mapping')
		  AND r.sml_item_code<>'' AND NOT EXISTS (SELECT 1 FROM marketplace_stock_reservation_components c WHERE c.reservation_id=r.id)
		ON CONFLICT (warehouse_code,location_code,item_code) DO UPDATE
		SET revision=marketplace_stock_demand_versions.revision+1,updated_at=NOW()`, pq.Array(billIDs)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO marketplace_stock_demand_versions(warehouse_code,location_code,item_code,revision)
		SELECT DISTINCT c.warehouse_code,c.location_code,c.component_item_code,1
		FROM marketplace_stock_reservations r JOIN marketplace_stock_reservation_components c ON c.reservation_id=r.id
		WHERE r.bill_id=ANY($1::uuid[]) AND r.state IN ('active','blocked_mapping')
		ON CONFLICT (warehouse_code,location_code,item_code) DO UPDATE
		SET revision=marketplace_stock_demand_versions.revision+1,updated_at=NOW()`, pq.Array(billIDs)); err != nil {
		return err
	}
	ready := job.Snapshot.ConversionStatus == "ready" && !job.Snapshot.Deactivate
	state, reason := "blocked_mapping", "conversion_"+job.Snapshot.ConversionStatus
	if job.Snapshot.Deactivate {
		reason = "master_inactive"
	} else if ready {
		state, reason = "active", ""
	}
	if _, err := tx.ExecContext(ctx, `UPDATE marketplace_stock_reservations SET
		marketplace_alias_id=$2::uuid,mapping_revision=$3,quantity_multiplier=$4,unit_code=$5,
		unit_stand_value=NULLIF($6,'')::numeric,unit_divide_value=NULLIF($7,'')::numeric,
		base_qty=CASE WHEN $6<>'' AND $7<>'' THEN source_qty*$4::numeric*$6::numeric/$7::numeric ELSE NULL END,
		sml_item_code=$8,set_definition_hash=$9,state=$10,state_reason=$11,demand_revision=demand_revision+1,updated_at=NOW()
		WHERE bill_id=ANY($1::uuid[]) AND state IN ('active','blocked_mapping')`, pq.Array(billIDs), job.AliasID,
		job.TargetRevision, job.Snapshot.QuantityMultiplier, job.Snapshot.UnitCode, job.Snapshot.StandValue,
		job.Snapshot.DivideValue, job.Snapshot.ItemCode, job.Snapshot.SetDefinitionHash, state, reason); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM marketplace_stock_reservation_components c
		USING marketplace_stock_reservations r WHERE c.reservation_id=r.id AND r.bill_id=ANY($1::uuid[])
		  AND r.state IN ('active','blocked_mapping')`, pq.Array(billIDs)); err != nil {
		return err
	}
	if ready && job.Snapshot.SetDefinitionHash != "" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO marketplace_stock_reservation_components
			(reservation_id,component_item_code,warehouse_code,location_code,component_base_qty,set_definition_hash)
			SELECT r.id,c.component_item_code,r.warehouse_code,r.location_code,SUM(r.base_qty*c.qty*c.unit_factor),$2
			FROM marketplace_stock_reservations r JOIN sml_catalog_set_components c
			  ON c.parent_item_code=r.sml_item_code AND c.definition_hash=$2 AND c.is_active=true AND c.unit_valid=true
			WHERE r.bill_id=ANY($1::uuid[]) AND r.state='active'
			GROUP BY r.id,c.component_item_code,r.warehouse_code,r.location_code
			ON CONFLICT (reservation_id,component_item_code,warehouse_code,location_code) DO UPDATE
			SET component_base_qty=EXCLUDED.component_base_qty,set_definition_hash=EXCLUDED.set_definition_hash`,
			pq.Array(billIDs), job.Snapshot.SetDefinitionHash); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO marketplace_stock_demand_versions(warehouse_code,location_code,item_code,revision)
		SELECT DISTINCT demand.warehouse_code,demand.location_code,demand.item_code,1 FROM (
		  SELECT r.warehouse_code,r.location_code,r.sml_item_code AS item_code
		  FROM marketplace_stock_reservations r WHERE r.bill_id=ANY($1::uuid[]) AND r.state='active'
		    AND NOT EXISTS (SELECT 1 FROM marketplace_stock_reservation_components c WHERE c.reservation_id=r.id)
		  UNION SELECT c.warehouse_code,c.location_code,c.component_item_code
		  FROM marketplace_stock_reservations r JOIN marketplace_stock_reservation_components c ON c.reservation_id=r.id
		  WHERE r.bill_id=ANY($1::uuid[]) AND r.state='active'
		) demand WHERE demand.item_code<>''
		ON CONFLICT (warehouse_code,location_code,item_code) DO UPDATE
		SET revision=marketplace_stock_demand_versions.revision+1,updated_at=NOW()`, pq.Array(billIDs)); err != nil {
		return err
	}
	return nil
}

func (r *MarketplaceAliasRepo) completeMappingJob(ctx context.Context, job *claimedMarketplaceMappingJob) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	warnings := marketplaceMappingCompletionWarnings(job.Snapshot)
	warningsJSON, err := json.Marshal(warnings)
	if err != nil {
		return err
	}
	if job.Snapshot.StandValue != "" && job.Snapshot.DivideValue != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE shopee_stock_mappings SET
			sml_item_code=$2,sml_unit_code=$3,unit_factor=$6::numeric*$7::numeric/$8::numeric,
			manual_unit_factor=CASE WHEN manual_unit_factor IS NULL THEN NULL ELSE $6::numeric*$7::numeric/$8::numeric END,
			shared_pool_enabled=CASE WHEN $9='managed' THEN shared_pool_enabled ELSE false END,
			set_definition_hash=$4,
			warning_codes=(warning_codes-'master_revision_pending'-'conversion_needs_review'-'conversion_stale'-'conversion_blocked'-
			  'stock_policy_blocked'-'stock_policy_manual_unmanaged'-'stock_policy_zeroing'-'stock_policy_disabled_zero') || $10::jsonb,
			updated_at=NOW() WHERE marketplace_alias_id=$1::uuid`, job.AliasID, job.Snapshot.ItemCode,
			job.Snapshot.UnitCode, job.Snapshot.SetDefinitionHash, job.Snapshot.ConversionStatus,
			job.Snapshot.QuantityMultiplier, job.Snapshot.StandValue, job.Snapshot.DivideValue,
			job.Snapshot.StockPolicy, warningsJSON); err != nil {
			return err
		}
	} else if _, err := tx.ExecContext(ctx, `UPDATE shopee_stock_mappings SET
		shared_pool_enabled=false,
		warning_codes=(warning_codes-'master_revision_pending'-'conversion_needs_review'-'conversion_stale'-'conversion_blocked'-
		  'stock_policy_blocked'-'stock_policy_manual_unmanaged'-'stock_policy_zeroing'-'stock_policy_disabled_zero')||$2::jsonb,
		updated_at=NOW() WHERE marketplace_alias_id=$1::uuid`, job.AliasID, warningsJSON); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE marketplace_mapping_jobs SET status='completed',lease_owner='',lease_until=NULL,
		heartbeat_at=NOW(),finished_at=NOW(),updated_at=NOW(),result_summary=jsonb_build_object(
		 'processed',processed_count,'target_revision',target_revision,'affected_shops',$3::jsonb)
		WHERE id=$1::uuid AND lease_owner=$2 AND status='running'`, job.ID, job.LeaseOwner, mustJSON(job.Snapshot.AffectedShopIDs))
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrMarketplaceImpactChanged
	}
	return tx.Commit()
}

func marketplaceMappingCompletionWarnings(snapshot marketplaceMutationTarget) []string {
	warnings := []string{}
	if snapshot.ConversionStatus != "ready" {
		warnings = append(warnings, "conversion_"+firstNonEmptyRepository(snapshot.ConversionStatus, "needs_review"))
	}
	if snapshot.StockPolicy != "managed" {
		warnings = append(warnings, "stock_policy_"+firstNonEmptyRepository(snapshot.StockPolicy, "blocked"))
	}
	return warnings
}

func (r *MarketplaceAliasRepo) failMappingJob(ctx context.Context, job *claimedMarketplaceMappingJob, cause error) error {
	if job == nil {
		return nil
	}
	message := "mapping reconciliation failed"
	if cause != nil {
		message = cause.Error()
	}
	_, err := r.db.ExecContext(ctx, `UPDATE marketplace_mapping_jobs SET status='failed',failed_count=failed_count+1,
		error_message=$3,lease_owner='',lease_until=NULL,finished_at=NOW(),updated_at=NOW()
		WHERE id=$1::uuid AND lease_owner=$2 AND status='running'`, job.ID, job.LeaseOwner, message)
	return err
}

func (r *MarketplaceAliasRepo) GetMappingJob(ctx context.Context, id string) (*models.MarketplaceMappingJob, error) {
	var job models.MarketplaceMappingJob
	var aliasID sql.NullString
	var summary json.RawMessage
	err := r.db.QueryRowContext(ctx, `SELECT id::text,alias_id::text,target_revision,job_type,status,processed_count,skipped_count,
		failed_count,result_summary,error_message,created_at,started_at,finished_at FROM marketplace_mapping_jobs WHERE id=$1::uuid`, id).Scan(
		&job.ID, &aliasID, &job.TargetRevision, &job.JobType, &job.Status, &job.ProcessedCount, &job.SkippedCount,
		&job.FailedCount, &summary, &job.ErrorMessage, &job.CreatedAt, &job.StartedAt, &job.FinishedAt)
	if err != nil {
		return nil, err
	}
	job.AliasID = aliasID.String
	_ = json.Unmarshal(summary, &job.ResultSummary)
	if job.ResultSummary == nil {
		job.ResultSummary = map[string]any{}
	}
	return &job, nil
}

func (r *MarketplaceAliasRepo) RetryMappingJob(ctx context.Context, id, userID string) (*models.MarketplaceMappingJob, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE marketplace_mapping_jobs SET status='queued',
		lease_owner='',lease_until=NULL,error_message='',finished_at=NULL,updated_at=NOW()
		WHERE id=$1::uuid AND status='failed'`, id)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return nil, ErrMarketplaceImpactChanged
	}
	_, _ = r.db.ExecContext(ctx, `INSERT INTO audit_logs(action,user_id,source,level,job_id,detail)
		VALUES('marketplace_mapping_job_retried',NULLIF($1,'')::uuid,'marketplace','info',$2::uuid,
		jsonb_build_object('job_id',$2,'requested_at',NOW()))`, userID, id)
	return r.GetMappingJob(ctx, id)
}

func (r *MarketplaceAliasRepo) GetStockPolicyJob(ctx context.Context, id string) (*models.MarketplaceStockPolicyJob, error) {
	var job models.MarketplaceStockPolicyJob
	var aliasID sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT id::text,shop_id,marketplace_alias_id::text,target_revision,item_id,model_id,
		policy_action,status,attempt_count,error_message,created_at,finished_at
		FROM shopee_stock_policy_jobs WHERE id=$1::uuid`, id).Scan(&job.ID, &job.ShopID, &aliasID,
		&job.TargetRevision, &job.ItemID, &job.ModelID, &job.PolicyAction, &job.Status, &job.AttemptCount,
		&job.ErrorMessage, &job.CreatedAt, &job.FinishedAt)
	if err != nil {
		return nil, err
	}
	job.MarketplaceAlias = aliasID.String
	return &job, nil
}

func (r *MarketplaceAliasRepo) RetryStockPolicyJob(ctx context.Context, id, userID string) (*models.MarketplaceStockPolicyJob, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE shopee_stock_policy_jobs SET status='queued',attempt_count=0,
		next_attempt_at=NOW(),lease_owner='',lease_until=NULL,error_message='',finished_at=NULL,updated_at=NOW()
		WHERE id=$1::uuid AND status IN ('failed','unknown')`, id)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return nil, ErrMarketplaceImpactChanged
	}
	_, _ = r.db.ExecContext(ctx, `INSERT INTO audit_logs(action,user_id,source,level,job_id,detail)
		VALUES('shopee_stock_policy_job_retried',NULLIF($1,'')::uuid,'shopee_stock','info',$2::uuid,
		jsonb_build_object('job_id',$2,'requested_at',NOW()))`, userID, id)
	return r.GetStockPolicyJob(ctx, id)
}

type MarketplaceMappingWorker struct {
	repo                  *MarketplaceAliasRepo
	conversionMode        string
	reconcileReservations bool
	log                   *zap.Logger
}

func NewMarketplaceMappingWorker(repo *MarketplaceAliasRepo, conversionMode string, reconcileReservations bool, log *zap.Logger) *MarketplaceMappingWorker {
	return &MarketplaceMappingWorker{repo: repo, conversionMode: strings.ToLower(strings.TrimSpace(conversionMode)), reconcileReservations: reconcileReservations, log: log}
}

func (w *MarketplaceMappingWorker) Start(ctx context.Context) {
	if w == nil || w.repo == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.tick(ctx)
			}
		}
	}()
}

func (w *MarketplaceMappingWorker) tick(ctx context.Context) {
	owner := fmt.Sprintf("mapping-worker:%d", time.Now().UnixNano())
	job, err := w.repo.ClaimMappingJob(ctx, owner, 5*time.Minute)
	if err != nil || job == nil {
		if err != nil && w.log != nil {
			w.log.Warn("claim marketplace mapping job", zap.Error(err))
		}
		return
	}
	for {
		processed, err := w.repo.processMappingJobBatch(ctx, job, w.conversionMode == "active", w.reconcileReservations)
		if err != nil {
			_ = w.repo.failMappingJob(context.Background(), job, err)
			if w.log != nil {
				w.log.Warn("process marketplace mapping job", zap.String("job_id", job.ID), zap.Error(err))
			}
			return
		}
		if processed == 0 {
			if err := w.repo.completeMappingJob(ctx, job); err != nil {
				_ = w.repo.failMappingJob(context.Background(), job, err)
			}
			return
		}
		if err := w.repo.heartbeatMappingJob(ctx, job.ID, job.LeaseOwner, 5*time.Minute); err != nil {
			_ = w.repo.failMappingJob(context.Background(), job, err)
			return
		}
	}
}
