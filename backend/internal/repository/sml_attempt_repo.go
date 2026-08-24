package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"nexflow/internal/models"
)

var (
	ErrSMLAttemptBusy          = errors.New("SML attempt is currently leased")
	ErrSMLAttemptExists        = errors.New("SML attempt already exists")
	ErrSMLAttemptLeaseLost     = errors.New("SML attempt lease was lost")
	ErrSMLAttemptNotReplayable = errors.New("SML attempt requires reconciliation")
	ErrBillNotSendable         = errors.New("bill is not sendable")
	ErrBillMutationConflict    = errors.New("bill changed while preparing SML payload")
	ErrBillDependencyStale     = errors.New("bill conversion dependencies changed before SML attempt")
)

type SMLAttemptCreate struct {
	TenantKey             string
	BillID                string
	DocNo                 string
	Route                 string
	PayloadBytes          []byte
	PayloadJSON           json.RawMessage
	PayloadHash           string
	RouteSettings         json.RawMessage
	MappingRevisions      json.RawMessage
	UnitCatalogGeneration *string
	SetDefinitionHashes   json.RawMessage
	LeaseOwner            string
	LeaseDuration         time.Duration
	CreatedBy             *string
	ExpectedBillRevision  int64
}

const smlAttemptSelectColumns = `id::text, tenant_key, bill_id::text, doc_no, state, route,
	       payload_bytes, payload_json, payload_hash, route_settings, mapping_revisions,
	       unit_catalog_generation::text, set_definition_hashes, lease_owner, lease_until,
	       external_request_started_at, external_request_finished_at, response_bytes,
	       response_hash, error_message, created_by::text, created_at, updated_at`

func (r *BillRepo) CreateSMLAttempt(ctx context.Context, in SMLAttemptCreate) (*models.BillSMLAttempt, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("bill repository is not configured")
	}
	if in.BillID == "" || in.DocNo == "" || in.Route == "" || len(in.PayloadBytes) == 0 || in.LeaseOwner == "" {
		return nil, errors.New("incomplete SML attempt")
	}
	if !json.Valid(in.PayloadBytes) || (len(in.PayloadJSON) > 0 && !json.Valid(in.PayloadJSON)) {
		return nil, errors.New("invalid SML attempt JSON")
	}
	if len(in.PayloadJSON) == 0 {
		in.PayloadJSON = json.RawMessage(in.PayloadBytes)
	}
	if in.PayloadHash == "" {
		sum := sha256.Sum256(in.PayloadBytes)
		in.PayloadHash = hex.EncodeToString(sum[:])
	}
	if in.LeaseDuration <= 0 {
		in.LeaseDuration = 5 * time.Minute
	}
	in.RouteSettings = jsonObjectOrEmpty(in.RouteSettings)
	in.MappingRevisions = jsonObjectOrEmpty(in.MappingRevisions)
	in.SetDefinitionHashes = jsonObjectOrEmpty(in.SetDefinitionHashes)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	status, archived, currentID, mutationRevision, err := lockBillAttemptState(ctx, tx, in.BillID)
	if err != nil {
		return nil, err
	}
	if archived.Valid || (status != "pending" && status != "failed" && status != "needs_review") {
		return nil, ErrBillNotSendable
	}
	if currentID.Valid && currentID.String != "" {
		return nil, ErrSMLAttemptExists
	}
	if mutationRevision != in.ExpectedBillRevision {
		return nil, ErrBillMutationConflict
	}
	if err := validateSMLAttemptDependencies(ctx, tx, in.BillID, in.UnitCatalogGeneration); err != nil {
		return nil, err
	}

	row := tx.QueryRowContext(ctx, `INSERT INTO bill_sml_attempts
		(tenant_key,bill_id,doc_no,state,route,payload_bytes,payload_json,payload_hash,
		 route_settings,mapping_revisions,unit_catalog_generation,set_definition_hashes,
		 lease_owner,lease_until,external_request_started_at,created_by)
		VALUES ($1,$2,$3,'sending',$4,$5,$6,$7,$8,$9,$10,$11,$12,
		        NOW()+($13 * INTERVAL '1 second'),NOW(),$14)
		RETURNING `+smlAttemptSelectColumns,
		in.TenantKey, in.BillID, in.DocNo, in.Route, in.PayloadBytes, in.PayloadJSON, in.PayloadHash,
		in.RouteSettings, in.MappingRevisions, in.UnitCatalogGeneration, in.SetDefinitionHashes,
		in.LeaseOwner, int64(in.LeaseDuration/time.Second), in.CreatedBy)
	attempt, err := scanSMLAttempt(row)
	if err != nil {
		return nil, fmt.Errorf("create SML attempt: %w", err)
	}
	result, err := tx.ExecContext(ctx, `UPDATE bills
		   SET current_sml_attempt_id=$1, sml_attempt_state='sending', sml_doc_no=$2, sml_payload=$4
		 WHERE id=$3 AND current_sml_attempt_id IS NULL`, attempt.ID, in.DocNo, in.BillID, in.PayloadJSON)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return nil, ErrSMLAttemptExists
	}
	if _, err := tx.ExecContext(ctx, `UPDATE marketplace_stock_reservations
		SET state='sending_sml',updated_at=NOW()
		WHERE bill_id=$1 AND state='active'`, in.BillID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return attempt, nil
}

func validateSMLAttemptDependencies(ctx context.Context, tx *sql.Tx, billID string, expectedGeneration *string) error {
	if expectedGeneration == nil || strings.TrimSpace(*expectedGeneration) == "" {
		return nil
	}
	expected := strings.TrimSpace(*expectedGeneration)
	var activeGeneration string
	err := tx.QueryRowContext(ctx, `SELECT id::text FROM sml_catalog_sync_runs WHERE status='active'
		ORDER BY activated_at DESC NULLS LAST,created_at DESC LIMIT 1 FOR SHARE`).Scan(&activeGeneration)
	if err != nil || activeGeneration != expected {
		return ErrBillDependencyStale
	}
	var catalogReady, mappingReady, reservationReady bool
	if err := tx.QueryRowContext(ctx, `SELECT catalog_generation_ready,mapping_backfill_ready,reservation_ledger_ready
		FROM marketplace_conversion_readiness WHERE singleton=true FOR SHARE`).Scan(
		&catalogReady, &mappingReady, &reservationReady); err != nil {
		return ErrBillDependencyStale
	}
	if !catalogReady || !mappingReady || !reservationReady {
		return ErrBillDependencyStale
	}
	var expectedLines, lockedLines int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM bill_items
		WHERE bill_id=$1 AND COALESCE(source_sku,'') NOT IN ($2,$3,$4)`, billID,
		models.ShopeeShippingSourceSKU, models.LazadaShippingSourceSKU, models.TikTokShippingSourceSKU).Scan(&expectedLines); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT bi.id::text FROM bill_items bi
		JOIN marketplace_item_aliases a ON a.id=bi.marketplace_alias_id
		JOIN sml_catalog c ON c.item_code=a.item_code
		WHERE bi.bill_id=$1 AND COALESCE(bi.source_sku,'') NOT IN ($2,$3,$4)
		  AND bi.mapping_revision_snapshot=a.mapping_revision
		  AND bi.unit_catalog_generation_snapshot=$5::uuid
		  AND a.unit_catalog_generation=$5::uuid AND a.conversion_status='ready' AND a.sales_enabled=true
		  AND COALESCE(bi.set_definition_hash_snapshot,'')=CASE WHEN COALESCE(c.item_type,0)=3 THEN COALESCE(c.set_definition_hash,'') ELSE '' END
		ORDER BY a.id,bi.id FOR SHARE OF a,c`, billID,
		models.ShopeeShippingSourceSKU, models.LazadaShippingSourceSKU, models.TikTokShippingSourceSKU, expected)
	if err != nil {
		return err
	}
	for rows.Next() {
		lockedLines++
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if lockedLines != expectedLines {
		return ErrBillDependencyStale
	}
	return nil
}

func (r *BillRepo) ClaimExistingSMLAttempt(ctx context.Context, billID, leaseOwner string, leaseDuration time.Duration) (*models.BillSMLAttempt, error) {
	if r == nil || r.db == nil || billID == "" || leaseOwner == "" {
		return nil, errors.New("incomplete SML attempt claim")
	}
	if leaseDuration <= 0 {
		leaseDuration = 5 * time.Minute
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	_, _, currentID, _, err := lockBillAttemptState(ctx, tx, billID)
	if err != nil {
		return nil, err
	}
	if !currentID.Valid || currentID.String == "" {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	attempt, err := scanSMLAttempt(tx.QueryRowContext(ctx, `SELECT `+smlAttemptSelectColumns+`
		  FROM bill_sml_attempts WHERE id=$1 FOR UPDATE`, currentID.String))
	if err != nil {
		return nil, err
	}
	if attempt.State == "sent" {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return attempt, nil
	}
	if attempt.State == "stale_requires_reconciliation" {
		return nil, ErrSMLAttemptNotReplayable
	}
	if attempt.State == "sending" && attempt.LeaseUntil != nil && attempt.LeaseUntil.After(time.Now()) && attempt.LeaseOwner != leaseOwner {
		return nil, ErrSMLAttemptBusy
	}
	attempt, err = scanSMLAttempt(tx.QueryRowContext(ctx, `UPDATE bill_sml_attempts
		   SET state='sending', lease_owner=$2,
		       lease_until=NOW()+($3 * INTERVAL '1 second'), updated_at=NOW()
		 WHERE id=$1
		 RETURNING `+smlAttemptSelectColumns, attempt.ID, leaseOwner, int64(leaseDuration/time.Second)))
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bills SET sml_attempt_state='sending' WHERE id=$1`, billID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return attempt, nil
}

func (r *BillRepo) RenewSMLAttemptLease(ctx context.Context, attemptID, leaseOwner string, leaseDuration time.Duration) error {
	if leaseDuration <= 0 {
		leaseDuration = 5 * time.Minute
	}
	result, err := r.db.ExecContext(ctx, `UPDATE bill_sml_attempts
		   SET lease_until=NOW()+($3 * INTERVAL '1 second'), updated_at=NOW()
		 WHERE id=$1 AND lease_owner=$2 AND state='sending'`, attemptID, leaseOwner, int64(leaseDuration/time.Second))
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrSMLAttemptLeaseLost
	}
	return nil
}

func (r *BillRepo) FinishSMLAttempt(
	ctx context.Context,
	attemptID, leaseOwner, attemptState, billStatus string,
	responseBytes []byte,
	errorMessage string,
) error {
	validAttemptState := attemptState == "unknown" || attemptState == "sent" ||
		attemptState == "failed_exact_retry" || attemptState == "stale_requires_reconciliation"
	validBillStatus := billStatus == "failed" || billStatus == "sent" || billStatus == "needs_review"
	if attemptID == "" || leaseOwner == "" || !validAttemptState || !validBillStatus {
		return errors.New("invalid SML attempt completion")
	}
	responseHash := ""
	if len(responseBytes) > 0 {
		sum := sha256.Sum256(responseBytes)
		responseHash = hex.EncodeToString(sum[:])
	}
	var responseJSON json.RawMessage
	if json.Valid(responseBytes) {
		responseJSON = json.RawMessage(responseBytes)
	}
	var billError *string
	if errorMessage != "" {
		billError = &errorMessage
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var billID, docNo string
	err = tx.QueryRowContext(ctx, `UPDATE bill_sml_attempts
		   SET state=$2, response_bytes=$3, response_hash=$4, error_message=$5,
		       external_request_finished_at=NOW(), lease_owner='', lease_until=NULL, updated_at=NOW()
		 WHERE id=$1 AND lease_owner=$6 AND state='sending'
		 RETURNING bill_id::text, doc_no`,
		attemptID, attemptState, responseBytes, responseHash, errorMessage, leaseOwner,
	).Scan(&billID, &docNo)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSMLAttemptLeaseLost
	}
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE bills
		   SET status=$2, sml_attempt_state=$3, sml_doc_no=$4, sml_response=$5,
		       current_sml_attempt_id=$6, error_msg=$7,
		       sent_at=CASE WHEN $2='sent' THEN NOW() ELSE sent_at END
		 WHERE id=$1 AND current_sml_attempt_id=$6`,
		billID, billStatus, attemptState, docNo, responseJSON, attemptID, billError,
	)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrSMLAttemptLeaseLost
	}
	switch attemptState {
	case "sent":
		if _, err := tx.ExecContext(ctx, `INSERT INTO marketplace_stock_demand_versions(warehouse_code,location_code,item_code,revision)
			SELECT demand.warehouse_code,demand.location_code,demand.item_code,1 FROM (
			  SELECT r.warehouse_code,r.location_code,r.sml_item_code AS item_code
			  FROM marketplace_stock_reservations r WHERE r.bill_id=$1 AND r.sml_item_code<>''
			    AND NOT EXISTS(SELECT 1 FROM marketplace_stock_reservation_components c WHERE c.reservation_id=r.id)
			  UNION SELECT c.warehouse_code,c.location_code,c.component_item_code
			  FROM marketplace_stock_reservations r JOIN marketplace_stock_reservation_components c ON c.reservation_id=r.id
			  WHERE r.bill_id=$1
			) demand GROUP BY demand.warehouse_code,demand.location_code,demand.item_code
			ON CONFLICT (warehouse_code,location_code,item_code)
			DO UPDATE SET revision=marketplace_stock_demand_versions.revision+1,updated_at=NOW()`, billID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE marketplace_stock_reservations r
			SET state='awaiting_stock_recalc',
			    warehouse_code=COALESCE(NULLIF(r.warehouse_code,''),a.route_settings#>>'{config,WHCode}',''),
			    location_code=COALESCE(NULLIF(r.location_code,''),a.route_settings#>>'{config,ShelfCode}',''),
			    updated_at=NOW()
			FROM bill_sml_attempts a
			WHERE r.bill_id=$1 AND a.id=$2 AND r.state IN ('active','sending_sml')`, billID, attemptID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE marketplace_stock_reservation_components c
			SET warehouse_code=r.warehouse_code,
			    location_code=r.location_code
			FROM marketplace_stock_reservations r
			WHERE c.reservation_id=r.id AND r.bill_id=$1`, billID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO marketplace_stock_demand_versions(warehouse_code,location_code,item_code,revision)
			SELECT demand.warehouse_code,demand.location_code,demand.item_code,1 FROM (
			  SELECT r.warehouse_code,r.location_code,r.sml_item_code AS item_code
			  FROM marketplace_stock_reservations r WHERE r.bill_id=$1 AND r.sml_item_code<>''
			    AND NOT EXISTS(SELECT 1 FROM marketplace_stock_reservation_components c WHERE c.reservation_id=r.id)
			  UNION SELECT c.warehouse_code,c.location_code,c.component_item_code
			  FROM marketplace_stock_reservations r JOIN marketplace_stock_reservation_components c ON c.reservation_id=r.id
			  WHERE r.bill_id=$1
			) demand GROUP BY demand.warehouse_code,demand.location_code,demand.item_code
			ON CONFLICT (warehouse_code,location_code,item_code)
			DO UPDATE SET revision=marketplace_stock_demand_versions.revision+1,updated_at=NOW()`, billID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO marketplace_stock_recalc_jobs(tenant_key,bill_id,sml_attempt_id,status)
			SELECT tenant_key,$1,$2,'queued' FROM bill_sml_attempts WHERE id=$2
			ON CONFLICT (sml_attempt_id) DO NOTHING`, billID, attemptID); err != nil {
			return err
		}
	case "failed_exact_retry":
		if _, err := tx.ExecContext(ctx, `UPDATE marketplace_stock_reservations
			SET state='active',updated_at=NOW()
			WHERE bill_id=$1 AND state='sending_sml'`, billID); err != nil {
			return err
		}
	case "stale_requires_reconciliation":
		if _, err := tx.ExecContext(ctx, `UPDATE marketplace_stock_reservations
			SET state='manual_reconciliation',state_reason='sml_attempt_stale',updated_at=NOW()
			WHERE bill_id=$1 AND state NOT IN ('incorporated_in_sml','released_cancelled')`, billID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func lockBillAttemptState(ctx context.Context, tx *sql.Tx, billID string) (string, sql.NullTime, sql.NullString, int64, error) {
	var status string
	var archived sql.NullTime
	var currentID sql.NullString
	var mutationRevision int64
	err := tx.QueryRowContext(ctx, `SELECT status, archived_at, current_sml_attempt_id::text, COALESCE(mutation_revision,0)
		   FROM bills WHERE id=$1 FOR UPDATE`, billID).Scan(&status, &archived, &currentID, &mutationRevision)
	if errors.Is(err, sql.ErrNoRows) {
		return "", archived, currentID, 0, ErrBillNotSendable
	}
	return status, archived, currentID, mutationRevision, err
}

type smlAttemptScanner interface {
	Scan(dest ...any) error
}

func scanSMLAttempt(row smlAttemptScanner) (*models.BillSMLAttempt, error) {
	var attempt models.BillSMLAttempt
	var payloadJSON, routeSettings, mappingRevisions, setHashes []byte
	var unitGeneration, createdBy sql.NullString
	var leaseUntil, startedAt, endedAt sql.NullTime
	if err := row.Scan(
		&attempt.ID, &attempt.TenantKey, &attempt.BillID, &attempt.DocNo, &attempt.State, &attempt.Route,
		&attempt.PayloadBytes, &payloadJSON, &attempt.PayloadHash, &routeSettings, &mappingRevisions,
		&unitGeneration, &setHashes, &attempt.LeaseOwner, &leaseUntil, &startedAt, &endedAt,
		&attempt.ResponseBytes, &attempt.ResponseHash, &attempt.ErrorMessage, &createdBy,
		&attempt.CreatedAt, &attempt.UpdatedAt,
	); err != nil {
		return nil, err
	}
	attempt.PayloadJSON = json.RawMessage(payloadJSON)
	attempt.RouteSettings = json.RawMessage(routeSettings)
	attempt.MappingRevisions = json.RawMessage(mappingRevisions)
	attempt.SetDefinitionHashes = json.RawMessage(setHashes)
	if unitGeneration.Valid {
		attempt.UnitCatalogGeneration = &unitGeneration.String
	}
	if createdBy.Valid {
		attempt.CreatedBy = &createdBy.String
	}
	if leaseUntil.Valid {
		attempt.LeaseUntil = &leaseUntil.Time
	}
	if startedAt.Valid {
		attempt.ExternalRequestStartedAt = &startedAt.Time
	}
	if endedAt.Valid {
		attempt.ExternalRequestEndedAt = &endedAt.Time
	}
	return &attempt, nil
}
