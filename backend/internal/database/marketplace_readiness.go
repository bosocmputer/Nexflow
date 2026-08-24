package database

import (
	"context"
	"database/sql"
	"fmt"
)

// CheckMarketplaceActivation prevents an active conversion deployment from
// starting before the tenant's generation, bounded backfill, and reservation
// reconciliation have all completed. Off and shadow modes remain available for
// safe rollout and observation.
func CheckMarketplaceActivation(ctx context.Context, db *sql.DB, mode string, unitCatalogEnabled, reservationLedgerEnabled bool) error {
	if mode != "active" {
		return nil
	}
	if !unitCatalogEnabled {
		return fmt.Errorf("active marketplace conversion requires MARKETPLACE_UNIT_CATALOG_ENABLED=true")
	}
	if !reservationLedgerEnabled {
		return fmt.Errorf("active marketplace conversion requires MARKETPLACE_RESERVATION_LEDGER_ENABLED=true")
	}

	var catalogReady, mappingReady, reservationReady bool
	err := db.QueryRowContext(ctx, `
		SELECT catalog_generation_ready, mapping_backfill_ready, reservation_ledger_ready
		FROM marketplace_conversion_readiness
		WHERE singleton = true
	`).Scan(&catalogReady, &mappingReady, &reservationReady)
	if err == sql.ErrNoRows {
		return fmt.Errorf("active marketplace conversion requires a completed tenant readiness record")
	}
	if err != nil {
		return fmt.Errorf("read marketplace conversion readiness: %w", err)
	}
	if !catalogReady {
		return fmt.Errorf("active marketplace conversion requires an active catalog generation")
	}
	if !mappingReady {
		return fmt.Errorf("active marketplace conversion requires the mapping backfill to complete")
	}
	if !reservationReady {
		return fmt.Errorf("active marketplace conversion requires the reservation ledger reconciliation to complete")
	}
	return nil
}
