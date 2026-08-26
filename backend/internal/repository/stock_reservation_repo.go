package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"nexflow/internal/models"
)

type marketplaceReservationSnapshot struct {
	Source             string
	AccountKey         string
	OrderID            string
	SourceLineID       string
	ExternalItemID     string
	ExternalVariantID  string
	MarketplaceAliasID *string
	MappingRevision    *int64
	SourceQty          string
	QuantityMultiplier int64
	UnitCode           string
	UnitStandValue     *string
	UnitDivideValue    *string
	BaseQty            string
	SMLItemCode        string
	SetDefinitionHash  string
	State              string
	StateReason        string
}

func buildMarketplaceReservation(source, accountKey, orderID string, item models.BillItem) (marketplaceReservationSnapshot, bool) {
	if item.SourceSKU == models.ShopeeShippingSourceSKU || item.SourceSKU == models.LazadaShippingSourceSKU || item.SourceSKU == models.TikTokShippingSourceSKU {
		return marketplaceReservationSnapshot{}, false
	}
	sourceQty := item.Qty
	if item.SourceQty != nil {
		sourceQty = *item.SourceQty
	}
	lineID := strings.TrimSpace(item.SourceLineID)
	if lineID == "" {
		lineID = item.ID
	}
	snapshot := marketplaceReservationSnapshot{
		Source: source, AccountKey: firstNonEmptyRepository(accountKey, "default"), OrderID: strings.TrimSpace(orderID),
		SourceLineID: lineID, ExternalItemID: strings.TrimSpace(item.SourceItemID), ExternalVariantID: strings.TrimSpace(item.SourceVariantID),
		MarketplaceAliasID: item.MarketplaceAliasID, MappingRevision: item.MappingRevisionSnapshot,
		SourceQty: strconv.FormatFloat(sourceQty, 'f', -1, 64), QuantityMultiplier: 1,
		UnitStandValue: item.UnitStandValueSnapshot, UnitDivideValue: item.UnitDivideValueSnapshot,
		SetDefinitionHash: strings.TrimSpace(item.SetDefinitionHashSnapshot), State: "blocked_mapping",
	}
	if item.QuantityMultiplierSnapshot != nil {
		snapshot.QuantityMultiplier = *item.QuantityMultiplierSnapshot
	}
	if item.UnitCode != nil {
		snapshot.UnitCode = strings.TrimSpace(*item.UnitCode)
	}
	if item.ItemCode != nil {
		snapshot.SMLItemCode = strings.TrimSpace(*item.ItemCode)
	}
	if item.BaseQtySnapshot != nil {
		snapshot.BaseQty = strings.TrimSpace(*item.BaseQtySnapshot)
	}
	snapshot.StateReason = strings.TrimSpace(item.ConversionIssueCode)
	if snapshot.StateReason == "" {
		snapshot.StateReason = "conversion_snapshot_missing"
	}
	if item.MarketplaceAliasID == nil || item.MappingRevisionSnapshot == nil || item.QuantityMultiplierSnapshot == nil ||
		item.UnitStandValueSnapshot == nil || item.UnitDivideValueSnapshot == nil || snapshot.SMLItemCode == "" || snapshot.BaseQty == "" {
		return snapshot, true
	}
	if snapshot.QuantityMultiplier < 1 || !positiveRationalText(*item.UnitStandValueSnapshot) ||
		!positiveRationalText(*item.UnitDivideValueSnapshot) || !positiveRationalText(snapshot.BaseQty) {
		snapshot.StateReason = "conversion_snapshot_invalid"
		return snapshot, true
	}
	if item.ConversionIssueCode != "" {
		return snapshot, true
	}
	snapshot.State = "active"
	snapshot.StateReason = ""
	return snapshot, true
}

func positiveRationalText(value string) bool {
	r, ok := new(big.Rat).SetString(strings.TrimSpace(value))
	return ok && r.Sign() > 0
}

func firstNonEmptyRepository(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func insertMarketplaceReservationsTx(tx *sql.Tx, tenantKey string, bill *models.Bill, items []models.BillItem) error {
	if bill == nil || (bill.Source != "shopee" && bill.Source != "lazada" && bill.Source != "tiktok") {
		return nil
	}
	if marketplaceOrderUnpaid(bill.RawData) {
		return nil
	}
	orderID := strings.TrimSpace(bill.SMLOrderID)
	if orderID == "" {
		var raw struct {
			OrderID string `json:"order_id"`
		}
		_ = json.Unmarshal(bill.RawData, &raw)
		orderID = strings.TrimSpace(raw.OrderID)
	}
	if orderID == "" {
		return nil
	}
	sourceVersion := marketplaceSourceVersion(bill.RawData)
	for _, item := range items {
		snapshot, include := buildMarketplaceReservation(bill.Source, bill.SourceAccountKey, orderID, item)
		if !include {
			continue
		}
		var reservationID, state string
		var baseQty any
		if snapshot.BaseQty != "" {
			baseQty = snapshot.BaseQty
		}
		err := tx.QueryRow(`INSERT INTO marketplace_stock_reservations
			(tenant_key,source,account_key,order_id,source_line_id,external_item_id,external_variant_id,bill_id,
			 marketplace_alias_id,mapping_revision,source_qty,quantity_multiplier,unit_code,
			 unit_stand_value,unit_divide_value,base_qty,sml_item_code,set_definition_hash,
			 source_event_version,state,state_reason)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
			ON CONFLICT (source,account_key,order_id,source_line_id,external_item_id,external_variant_id)
			DO UPDATE SET bill_id=COALESCE(marketplace_stock_reservations.bill_id,EXCLUDED.bill_id),
			              source_event_version=CASE WHEN EXCLUDED.source_event_version<>'' THEN EXCLUDED.source_event_version ELSE marketplace_stock_reservations.source_event_version END,
			              updated_at=NOW()
			RETURNING id::text,state`,
			strings.TrimSpace(tenantKey), snapshot.Source, snapshot.AccountKey, snapshot.OrderID, snapshot.SourceLineID,
			snapshot.ExternalItemID, snapshot.ExternalVariantID, bill.ID, snapshot.MarketplaceAliasID,
			snapshot.MappingRevision, snapshot.SourceQty, snapshot.QuantityMultiplier, snapshot.UnitCode,
			snapshot.UnitStandValue, snapshot.UnitDivideValue, baseQty, snapshot.SMLItemCode,
			snapshot.SetDefinitionHash, sourceVersion, snapshot.State, snapshot.StateReason,
		).Scan(&reservationID, &state)
		if err != nil {
			return err
		}
		if state != "active" || snapshot.SMLItemCode == "" {
			continue
		}
		if snapshot.SetDefinitionHash == "" {
			if err := bumpDemandVersionTx(tx, "", "", snapshot.SMLItemCode); err != nil {
				return err
			}
			continue
		}
		result, err := tx.Exec(`INSERT INTO marketplace_stock_reservation_components
			(reservation_id,component_item_code,warehouse_code,location_code,component_base_qty,set_definition_hash)
			SELECT $1,component_item_code,'','',SUM($2::numeric * qty * unit_factor),$3
			  FROM sml_catalog_set_components
			 WHERE parent_item_code=$4 AND definition_hash=$3 AND is_active=true AND unit_valid=true
			 GROUP BY component_item_code
			ON CONFLICT (reservation_id,component_item_code,warehouse_code,location_code) DO NOTHING`,
			reservationID, snapshot.BaseQty, snapshot.SetDefinitionHash, snapshot.SMLItemCode)
		if err != nil {
			return err
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			if _, err := tx.Exec(`UPDATE marketplace_stock_reservations
				SET state='blocked_mapping',state_reason='set_definition_unavailable',updated_at=NOW()
				WHERE id=$1`, reservationID); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.Exec(`INSERT INTO marketplace_stock_demand_versions(warehouse_code,location_code,item_code,revision)
			SELECT DISTINCT '','',component_item_code,1 FROM marketplace_stock_reservation_components WHERE reservation_id=$1
			ON CONFLICT (warehouse_code,location_code,item_code)
			DO UPDATE SET revision=marketplace_stock_demand_versions.revision+1,updated_at=NOW()`, reservationID); err != nil {
			return err
		}
	}
	return nil
}

func bumpDemandVersionTx(tx *sql.Tx, warehouse, location, itemCode string) error {
	_, err := tx.Exec(`INSERT INTO marketplace_stock_demand_versions(warehouse_code,location_code,item_code,revision)
		VALUES($1,$2,$3,1)
		ON CONFLICT (warehouse_code,location_code,item_code)
		DO UPDATE SET revision=marketplace_stock_demand_versions.revision+1,updated_at=NOW()`, warehouse, location, itemCode)
	return err
}

// ReconcileMarketplaceReservationCancelled releases only demand that has never
// reached SML. A reservation whose write is in flight or awaiting stock
// recalculation remains a circuit breaker until an operator reconciles it.
func (r *BillRepo) ReconcileMarketplaceReservationCancelled(ctx context.Context, source, accountKey, orderID string) error {
	source = strings.TrimSpace(strings.ToLower(source))
	accountKey = strings.TrimSpace(accountKey)
	orderID = strings.TrimSpace(orderID)
	if source == "" || accountKey == "" || orderID == "" {
		return errors.New("marketplace reservation identity is required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `INSERT INTO marketplace_stock_demand_versions(warehouse_code,location_code,item_code,revision)
		SELECT DISTINCT r.warehouse_code,r.location_code,r.sml_item_code,1
		FROM marketplace_stock_reservations r
		WHERE r.source=$1 AND r.account_key=$2 AND r.order_id=$3
		  AND r.state IN ('active','blocked_mapping') AND r.sml_item_code<>''
		  AND NOT EXISTS (SELECT 1 FROM marketplace_stock_reservation_components c WHERE c.reservation_id=r.id)
		ON CONFLICT (warehouse_code,location_code,item_code)
		DO UPDATE SET revision=marketplace_stock_demand_versions.revision+1,updated_at=NOW()`, source, accountKey, orderID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO marketplace_stock_demand_versions(warehouse_code,location_code,item_code,revision)
		SELECT DISTINCT c.warehouse_code,c.location_code,c.component_item_code,1
		FROM marketplace_stock_reservations r
		JOIN marketplace_stock_reservation_components c ON c.reservation_id=r.id
		WHERE r.source=$1 AND r.account_key=$2 AND r.order_id=$3
		  AND r.state IN ('active','blocked_mapping')
		ON CONFLICT (warehouse_code,location_code,item_code)
		DO UPDATE SET revision=marketplace_stock_demand_versions.revision+1,updated_at=NOW()`, source, accountKey, orderID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE marketplace_stock_reservations
		SET state=CASE
		      WHEN state IN ('active','blocked_mapping') THEN 'released_cancelled'
		      WHEN state IN ('sending_sml','awaiting_stock_recalc') THEN 'manual_reconciliation'
		      ELSE state
		    END,
		    state_reason=CASE
		      WHEN state IN ('active','blocked_mapping') THEN 'marketplace_cancelled'
		      WHEN state IN ('sending_sml','awaiting_stock_recalc') THEN 'cancelled_during_sml_reconciliation'
		      ELSE state_reason
		    END,
		    released_at=CASE WHEN state IN ('active','blocked_mapping') THEN NOW() ELSE released_at END,
		    updated_at=NOW()
		WHERE source=$1 AND account_key=$2 AND order_id=$3
		  AND state IN ('active','blocked_mapping','sending_sml','awaiting_stock_recalc')`, source, accountKey, orderID); err != nil {
		return err
	}
	return tx.Commit()
}

func marketplaceOrderUnpaid(rawData json.RawMessage) bool {
	var raw map[string]any
	if json.Unmarshal(rawData, &raw) != nil {
		return false
	}
	for _, key := range []string{"order_status", "status", "payment_status"} {
		if strings.EqualFold(strings.TrimSpace(toString(raw[key])), "UNPAID") {
			return true
		}
	}
	return false
}

func marketplaceSourceVersion(rawData json.RawMessage) string {
	var raw map[string]any
	if json.Unmarshal(rawData, &raw) != nil {
		return ""
	}
	return firstNonEmptyRepository(toString(raw["event_version"]), toString(raw["amount_source_fingerprint"]), toString(raw["import_run_id"]))
}

func toString(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

type StockRecalcJob struct {
	ID                      string
	BillID                  string
	SMLAttemptID            string
	AttemptCount            int
	LeaseOwner              string
	LeaseUntil              *time.Time
	ProcessStockSucceededAt *time.Time
}

type StockRecalcDemand struct {
	ItemCodes []string
	Warehouse string
	Location  string
	Lines     []StockRecalcDemandLine
}

type StockRecalcDemandLine struct {
	EvidenceID           string
	ReservationID        string
	SMLAttemptID         string
	DocNo                string
	Route                string
	ItemCode             string
	Warehouse            string
	Location             string
	ExpectedBaseQtyExact string
	EvidenceKind         string
}

type VerifiedStockEvidence struct {
	StockRecalcDemandLine
	EvidenceGroupID                   string
	DocumentScopeExpectedBaseQtyExact string
	ActualBaseQtyExact                string
	SourceFingerprint                 string
	EvidenceHash                      string
	VerifiedSourceSnapshotAt          time.Time
}

func (r *BillRepo) ClaimStockRecalcJob(ctx context.Context, leaseOwner string, leaseDuration time.Duration) (*StockRecalcJob, error) {
	if leaseDuration <= 0 {
		leaseDuration = 5 * time.Minute
	}
	var job StockRecalcJob
	var billID, attemptID sql.NullString
	var leaseUntil, processSucceeded sql.NullTime
	err := r.db.QueryRowContext(ctx, `WITH candidate AS (
		SELECT id FROM marketplace_stock_recalc_jobs
		 WHERE status IN ('queued','failed') AND next_attempt_at<=NOW()
		   AND (lease_until IS NULL OR lease_until<NOW())
		 ORDER BY next_attempt_at,created_at,id
		 FOR UPDATE SKIP LOCKED LIMIT 1
	)
	UPDATE marketplace_stock_recalc_jobs j
	   SET status='running',attempt_count=attempt_count+1,lease_owner=$1,
	       lease_until=NOW()+($2 * INTERVAL '1 second'),updated_at=NOW()
	  FROM candidate WHERE j.id=candidate.id
	RETURNING j.id::text,j.bill_id::text,j.sml_attempt_id::text,j.attempt_count,j.lease_owner,j.lease_until,j.processstock_succeeded_at`,
		leaseOwner, int64(leaseDuration/time.Second)).Scan(
		&job.ID, &billID, &attemptID, &job.AttemptCount, &job.LeaseOwner, &leaseUntil, &processSucceeded)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	job.BillID = billID.String
	job.SMLAttemptID = attemptID.String
	if leaseUntil.Valid {
		job.LeaseUntil = &leaseUntil.Time
	}
	if processSucceeded.Valid {
		job.ProcessStockSucceededAt = &processSucceeded.Time
	}
	return &job, nil
}

func (r *BillRepo) StockRecalcDemand(ctx context.Context, jobID string) (StockRecalcDemand, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT r.id::text,j.sml_attempt_id::text,a.doc_no,a.route,
		demand.item_code,demand.warehouse_code,demand.location_code,demand.base_qty::text
		FROM marketplace_stock_recalc_jobs j
		JOIN bill_sml_attempts a ON a.id=j.sml_attempt_id
		JOIN marketplace_stock_reservations r ON r.bill_id=j.bill_id
		JOIN LATERAL (
		  SELECT c.component_item_code AS item_code,c.warehouse_code,c.location_code,c.component_base_qty AS base_qty
		    FROM marketplace_stock_reservation_components c WHERE c.reservation_id=r.id
		  UNION ALL
		  SELECT r.sml_item_code,r.warehouse_code,r.location_code,r.base_qty
		   WHERE r.sml_item_code<>'' AND NOT EXISTS (
		     SELECT 1 FROM marketplace_stock_reservation_components c WHERE c.reservation_id=r.id
		   )
		) demand ON true
		WHERE j.id=$1 AND r.state='awaiting_stock_recalc'
		ORDER BY r.id,demand.item_code,demand.warehouse_code,demand.location_code`, jobID)
	if err != nil {
		return StockRecalcDemand{}, err
	}
	defer rows.Close()
	result := StockRecalcDemand{}
	seen := map[string]struct{}{}
	for rows.Next() {
		var line StockRecalcDemandLine
		if err := rows.Scan(&line.ReservationID, &line.SMLAttemptID, &line.DocNo, &line.Route,
			&line.ItemCode, &line.Warehouse, &line.Location, &line.ExpectedBaseQtyExact); err != nil {
			return StockRecalcDemand{}, err
		}
		line.Route = strings.ToLower(strings.TrimSpace(line.Route))
		switch line.Route {
		case "saleorder":
			line.EvidenceKind = "sale_order_demand"
		case "saleinvoice":
			line.EvidenceKind = "stock_movement"
		default:
			return StockRecalcDemand{}, fmt.Errorf("unsupported stock evidence route %q", line.Route)
		}
		line.EvidenceID = strings.Join([]string{line.ReservationID, line.ItemCode, line.Warehouse, line.Location}, ":")
		warehouse, location, code := line.Warehouse, line.Location, line.ItemCode
		if result.Warehouse == "" && result.Location == "" {
			result.Warehouse, result.Location = warehouse, location
		} else if result.Warehouse != warehouse || result.Location != location {
			return StockRecalcDemand{}, errors.New("reservation stock scope mismatch")
		}
		if _, ok := seen[code]; !ok {
			seen[code] = struct{}{}
			result.ItemCodes = append(result.ItemCodes, code)
		}
		result.Lines = append(result.Lines, line)
	}
	return result, rows.Err()
}

func (r *BillRepo) MarkStockRecalcProcessed(ctx context.Context, jobID, leaseOwner string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE marketplace_stock_recalc_jobs
		SET processstock_succeeded_at=NOW(),updated_at=NOW()
		WHERE id=$1 AND lease_owner=$2 AND status='running'`, jobID, leaseOwner)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrSMLAttemptLeaseLost
	}
	return nil
}

func (r *BillRepo) CompleteStockRecalcJob(ctx context.Context, jobID, leaseOwner string, evidence []VerifiedStockEvidence) error {
	if len(evidence) == 0 {
		return errors.New("verified stock representation evidence is required")
	}
	approvedFingerprint := strings.TrimSpace(evidence[0].SourceFingerprint)
	for _, item := range evidence {
		if strings.TrimSpace(item.SourceFingerprint) == "" || strings.TrimSpace(item.EvidenceHash) == "" || item.VerifiedSourceSnapshotAt.IsZero() ||
			strings.TrimSpace(item.ActualBaseQtyExact) == "" || strings.TrimSpace(item.ExpectedBaseQtyExact) == "" ||
			strings.TrimSpace(item.EvidenceGroupID) == "" || strings.TrimSpace(item.DocumentScopeExpectedBaseQtyExact) == "" {
			return errors.New("verified stock representation evidence is incomplete")
		}
		if strings.TrimSpace(item.SourceFingerprint) != approvedFingerprint {
			return errors.New("verified stock representation evidence uses mixed source fingerprints")
		}
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, item := range evidence {
		result, err := tx.ExecContext(ctx, `INSERT INTO marketplace_stock_representation_evidence
			(reservation_id,sml_attempt_id,doc_no,route,warehouse_code,location_code,item_code,
			 expected_base_qty,evidence_group_id,document_scope_expected_base_qty,actual_base_qty,
			 evidence_kind,status,source_semantics_fingerprint,evidence_hash,
			 verified_source_snapshot_at,verified_at,retry_count,last_reason,updated_at)
			SELECT $3::uuid,j.sml_attempt_id,$4,$5,$6,$7,$8,$9::numeric,$10,$11::numeric,$12::numeric,$13,'verified',$14,$15,$16,NOW(),1,'',NOW()
			FROM marketplace_stock_recalc_jobs j
			JOIN marketplace_stock_reservations r ON r.id=$3::uuid AND r.bill_id=j.bill_id AND r.state='awaiting_stock_recalc'
			WHERE j.id=$1 AND j.lease_owner=$2 AND j.status='running' AND j.processstock_succeeded_at IS NOT NULL
			  AND j.sml_attempt_id=$17::uuid
			ON CONFLICT (reservation_id,sml_attempt_id,warehouse_code,location_code,item_code,evidence_kind)
			DO UPDATE SET evidence_group_id=EXCLUDED.evidence_group_id,
			 document_scope_expected_base_qty=EXCLUDED.document_scope_expected_base_qty,
			 actual_base_qty=EXCLUDED.actual_base_qty,status='verified',
			 source_semantics_fingerprint=EXCLUDED.source_semantics_fingerprint,evidence_hash=EXCLUDED.evidence_hash,
			 verified_source_snapshot_at=EXCLUDED.verified_source_snapshot_at,verified_at=NOW(),
			 retry_count=marketplace_stock_representation_evidence.retry_count+1,last_reason='',updated_at=NOW()`,
			jobID, leaseOwner, item.ReservationID, item.DocNo, item.Route, item.Warehouse, item.Location, item.ItemCode,
			item.ExpectedBaseQtyExact, item.EvidenceGroupID, item.DocumentScopeExpectedBaseQtyExact, item.ActualBaseQtyExact,
			item.EvidenceKind, item.SourceFingerprint, item.EvidenceHash, item.VerifiedSourceSnapshotAt, item.SMLAttemptID)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return ErrSMLAttemptLeaseLost
		}
	}
	var missingEvidence int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM marketplace_stock_recalc_jobs j
		JOIN marketplace_stock_reservations r ON r.bill_id=j.bill_id AND r.state='awaiting_stock_recalc'
		JOIN bill_sml_attempts a ON a.id=j.sml_attempt_id
		JOIN LATERAL (
		  SELECT c.component_item_code AS item_code,c.warehouse_code,c.location_code,c.component_base_qty AS base_qty,
		         CASE WHEN lower(a.route)='saleorder' THEN 'sale_order_demand' ELSE 'stock_movement' END AS evidence_kind
		  FROM marketplace_stock_reservation_components c WHERE c.reservation_id=r.id
		  UNION ALL
		  SELECT r.sml_item_code,r.warehouse_code,r.location_code,r.base_qty,
		         CASE WHEN lower(a.route)='saleorder' THEN 'sale_order_demand' ELSE 'stock_movement' END
		  WHERE r.sml_item_code<>'' AND NOT EXISTS (
		    SELECT 1 FROM marketplace_stock_reservation_components c WHERE c.reservation_id=r.id
		  )
		) demand ON true
		WHERE j.id=$1 AND j.lease_owner=$2 AND j.status='running'
		  AND NOT EXISTS (
		    SELECT 1 FROM marketplace_stock_representation_evidence e
		    WHERE e.reservation_id=r.id AND e.sml_attempt_id=j.sml_attempt_id
		      AND e.item_code=demand.item_code AND e.warehouse_code=demand.warehouse_code AND e.location_code=demand.location_code
		      AND e.evidence_kind=demand.evidence_kind AND e.expected_base_qty=demand.base_qty AND e.status='verified'
		      AND e.source_semantics_fingerprint=$3 AND e.evidence_group_id<>''
		      AND e.document_scope_expected_base_qty=e.actual_base_qty
		  )`, jobID, leaseOwner, approvedFingerprint).Scan(&missingEvidence); err != nil {
		return err
	}
	if missingEvidence != 0 {
		return fmt.Errorf("stock representation evidence missing for %d demand lines", missingEvidence)
	}
	var billID string
	err = tx.QueryRowContext(ctx, `UPDATE marketplace_stock_recalc_jobs
		SET status='completed',balance_verified_at=NOW(),lease_owner='',lease_until=NULL,error_message='',updated_at=NOW()
		WHERE id=$1 AND lease_owner=$2 AND status='running' AND processstock_succeeded_at IS NOT NULL
		RETURNING bill_id::text`, jobID, leaseOwner).Scan(&billID)
	if err == sql.ErrNoRows {
		return ErrSMLAttemptLeaseLost
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO marketplace_stock_demand_versions(warehouse_code,location_code,item_code,revision)
		SELECT DISTINCT c.warehouse_code,c.location_code,c.component_item_code,1
		FROM marketplace_stock_reservations r
		JOIN marketplace_stock_reservation_components c ON c.reservation_id=r.id
		WHERE r.bill_id=$1 AND r.state='awaiting_stock_recalc'
		ON CONFLICT (warehouse_code,location_code,item_code)
		DO UPDATE SET revision=marketplace_stock_demand_versions.revision+1,updated_at=NOW()`, billID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO marketplace_stock_demand_versions(warehouse_code,location_code,item_code,revision)
		SELECT DISTINCT r.warehouse_code,r.location_code,r.sml_item_code,1
		FROM marketplace_stock_reservations r
		WHERE r.bill_id=$1 AND r.state='awaiting_stock_recalc' AND r.sml_item_code<>''
		  AND NOT EXISTS (SELECT 1 FROM marketplace_stock_reservation_components c WHERE c.reservation_id=r.id)
		ON CONFLICT (warehouse_code,location_code,item_code)
		DO UPDATE SET revision=marketplace_stock_demand_versions.revision+1,updated_at=NOW()`, billID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE marketplace_stock_reservations
		SET state='incorporated_in_sml',incorporated_at=NOW(),updated_at=NOW()
		WHERE bill_id=$1 AND state='awaiting_stock_recalc'`, billID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *BillRepo) FailStockRecalcJob(ctx context.Context, jobID, leaseOwner, message string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	var billID sql.NullString
	err = tx.QueryRowContext(ctx, `UPDATE marketplace_stock_recalc_jobs
		SET status=CASE WHEN attempt_count>=10 THEN 'manual_reconciliation' ELSE 'failed' END,
		    next_attempt_at=NOW()+(LEAST(3600,power(2,LEAST(attempt_count,8))::int*15) * INTERVAL '1 second'),
		    lease_owner='',lease_until=NULL,error_message=$3,updated_at=NOW()
		WHERE id=$1 AND lease_owner=$2 AND status='running'
		RETURNING status,bill_id::text`, jobID, leaseOwner, message).Scan(&status, &billID)
	if err == sql.ErrNoRows {
		return ErrSMLAttemptLeaseLost
	}
	if err != nil {
		return err
	}
	if status == "manual_reconciliation" && billID.Valid {
		_, err = tx.ExecContext(ctx, `UPDATE marketplace_stock_reservations
			SET state='manual_reconciliation',state_reason='stock_recalc_verification_failed',updated_at=NOW()
			WHERE bill_id=$1 AND state='awaiting_stock_recalc'`, billID.String)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}
