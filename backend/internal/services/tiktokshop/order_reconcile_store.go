package tiktokshop

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
)

type TikTokOrderReconcileStore struct {
	database *sql.DB
}

func NewTikTokOrderReconcileStore(database *sql.DB) *TikTokOrderReconcileStore {
	return &TikTokOrderReconcileStore{database: database}
}

func (s *TikTokOrderReconcileStore) StartManualRun(ctx context.Context, input TikTokOrderReconcileRequest) (TikTokOrderReconcileRun, error) {
	if s == nil || s.database == nil || !validTikTokOrderReconcileRequest(input) {
		return TikTokOrderReconcileRun{}, ErrInvalidOrderReconcileInput
	}
	shopID := strings.TrimSpace(input.ShopID)
	windowStart := time.Unix(input.UpdateTimeGE, 0).UTC()
	windowEnd := time.Unix(input.UpdateTimeLT, 0).UTC()
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return TikTokOrderReconcileRun{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var connectionID string
	if err := tx.QueryRowContext(ctx,
		`SELECT gateway_connection_id::text
		   FROM tiktok_shop_connections
		  WHERE shop_id = $1 AND disabled_at IS NULL
		  FOR SHARE`, shopID,
	).Scan(&connectionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TikTokOrderReconcileRun{}, ErrInvalidOrderReconcileInput
		}
		return TikTokOrderReconcileRun{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO tiktok_shop_order_sync_settings (shop_id)
		 VALUES ($1)
		 ON CONFLICT (shop_id) DO NOTHING`, shopID,
	); err != nil {
		return TikTokOrderReconcileRun{}, err
	}
	var runID string
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO tiktok_shop_order_sync_runs
		  (gateway_connection_id, shop_id, trigger_source, status, window_start, window_end, page_size, max_pages)
		 VALUES ($1::uuid, $2, 'manual', 'running', $3, $4, $5, $6)
		 RETURNING id::text`,
		connectionID, shopID, windowStart, windowEnd, tikTokOrderReconcilePageSize, tikTokOrderReconcileMaxPages,
	).Scan(&runID); err != nil {
		return TikTokOrderReconcileRun{}, fmt.Errorf("start TikTok Shop order reconciliation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return TikTokOrderReconcileRun{}, err
	}
	return TikTokOrderReconcileRun{ID: runID, ShopID: shopID, WindowStart: windowStart, WindowEnd: windowEnd}, nil
}

func (s *TikTokOrderReconcileStore) RecordPage(ctx context.Context, runID string, progress TikTokOrderReconcileProgress) error {
	if s == nil || s.database == nil || strings.TrimSpace(runID) == "" {
		return ErrInvalidOrderReconcileInput
	}
	tokenHash := reconcilePageTokenHash(progress.LastPageToken)
	requestIDs, err := reconcileRequestIDsJSON(progress.SearchRequestIDs)
	if err != nil {
		return err
	}
	result, err := s.database.ExecContext(ctx,
		`UPDATE tiktok_shop_order_sync_runs
		    SET page_count = $2, discovered_count = $3, snapshotted_count = $4,
		        last_page_token_hash = $5, search_request_ids = $6::jsonb, updated_at = NOW()
		  WHERE id = $1::uuid AND status = 'running'`,
		runID, progress.PageCount, progress.DiscoveredCount, progress.SnapshottedCount, tokenHash, requestIDs,
	)
	if err != nil {
		return err
	}
	return requireOneReconcileRow(result)
}

func (s *TikTokOrderReconcileStore) CompleteRun(ctx context.Context, runID string, progress TikTokOrderReconcileProgress) error {
	if s == nil || s.database == nil || strings.TrimSpace(runID) == "" {
		return ErrInvalidOrderReconcileInput
	}
	tokenHash := reconcilePageTokenHash(progress.LastPageToken)
	requestIDs, err := reconcileRequestIDsJSON(progress.SearchRequestIDs)
	if err != nil {
		return err
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var shopID string
	var windowEnd time.Time
	if err := tx.QueryRowContext(ctx,
		`UPDATE tiktok_shop_order_sync_runs
		    SET status = 'succeeded', page_count = $2, discovered_count = $3, snapshotted_count = $4,
		        last_page_token_hash = $5, search_request_ids = $6::jsonb,
		        error_code = '', error_message = '', finished_at = NOW(), updated_at = NOW()
		  WHERE id = $1::uuid AND status = 'running'
		  RETURNING shop_id, window_end`,
		runID, progress.PageCount, progress.DiscoveredCount, progress.SnapshottedCount, tokenHash, requestIDs,
	).Scan(&shopID, &windowEnd); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE tiktok_shop_order_sync_settings
		    SET watermark_update_at = CASE
		          WHEN watermark_update_at IS NULL OR watermark_update_at < $2 THEN $2
		          ELSE watermark_update_at
		        END,
		        last_success_at = NOW(), last_error_code = '', last_error_message = '', updated_at = NOW()
		  WHERE shop_id = $1`, shopID, windowEnd,
	)
	if err != nil {
		return err
	}
	if err := requireOneReconcileRow(result); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *TikTokOrderReconcileStore) FailRun(ctx context.Context, runID, code, message string) error {
	if s == nil || s.database == nil || strings.TrimSpace(runID) == "" {
		return ErrInvalidOrderReconcileInput
	}
	code = strings.TrimSpace(code)
	message = strings.TrimSpace(message)
	if code == "" || len(message) > 512 {
		return ErrInvalidOrderReconcileInput
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var shopID string
	if err := tx.QueryRowContext(ctx,
		`UPDATE tiktok_shop_order_sync_runs
		    SET status = 'failed', error_code = $2, error_message = $3,
		        finished_at = NOW(), updated_at = NOW()
		  WHERE id = $1::uuid AND status = 'running'
		  RETURNING shop_id`, runID, code, message,
	).Scan(&shopID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE tiktok_shop_order_sync_settings
		    SET last_error_code = $2, last_error_message = $3,
		        next_run_at = CASE WHEN enabled THEN NOW() + INTERVAL '60 seconds' ELSE next_run_at END,
		        updated_at = NOW()
		  WHERE shop_id = $1`, shopID, code, message,
	)
	if err != nil {
		return err
	}
	if err := requireOneReconcileRow(result); err != nil {
		return err
	}
	return tx.Commit()
}

func reconcilePageTokenHash(pageToken string) any {
	pageToken = strings.TrimSpace(pageToken)
	if pageToken == "" {
		return nil
	}
	digest := sha256.Sum256([]byte(pageToken))
	return hex.EncodeToString(digest[:])
}

func reconcileRequestIDsJSON(values []string) ([]byte, error) {
	if len(values) > tikTokOrderReconcileMaxPages {
		return nil, ErrInvalidOrderReconcileInput
	}
	clean := make([]string, len(values))
	for index, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 256 {
			return nil, ErrInvalidOrderReconcileInput
		}
		clean[index] = value
	}
	return json.Marshal(clean)
}

func requireOneReconcileRow(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrOrderReconcileFailed
	}
	return nil
}
