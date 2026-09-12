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

const tikTokOrderReconcileSafetyLag = 30 * time.Second

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
	if _, err := tx.ExecContext(ctx,
		`UPDATE tiktok_shop_order_sync_runs
		    SET status = 'failed', error_code = 'lease_expired', error_message = 'Previous reconciliation lease expired',
		        finished_at = NOW(), updated_at = NOW()
		  WHERE status = 'running' AND lease_until < NOW()`,
	); err != nil {
		return TikTokOrderReconcileRun{}, err
	}
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
		        last_page_token_hash = $5, search_request_ids = $6::jsonb,
		        lease_until = NOW() + INTERVAL '30 minutes', updated_at = NOW()
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
		        last_page_token_hash = COALESCE($5, last_page_token_hash), search_request_ids = $6::jsonb,
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

func (s *TikTokOrderReconcileStore) ClaimDueRun(ctx context.Context, now time.Time) (TikTokOrderReconcileRun, error) {
	if s == nil || s.database == nil || now.IsZero() {
		return TikTokOrderReconcileRun{}, ErrInvalidOrderReconcileInput
	}
	now = now.UTC()
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return TikTokOrderReconcileRun{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`UPDATE tiktok_shop_order_sync_runs
		    SET status = 'failed', error_code = 'lease_expired', error_message = 'Previous reconciliation lease expired',
		        finished_at = $1, updated_at = NOW()
		  WHERE status = 'running' AND lease_until < $1`, now,
	); err != nil {
		return TikTokOrderReconcileRun{}, err
	}
	var shopID, connectionID string
	var intervalSeconds, overlapSeconds int
	var watermark sql.NullTime
	if err := tx.QueryRowContext(ctx,
		`SELECT s.shop_id, c.gateway_connection_id::text, s.interval_seconds, s.overlap_seconds, s.watermark_update_at
		   FROM tiktok_shop_order_sync_settings s
		   JOIN tiktok_shop_connections c ON c.shop_id = s.shop_id AND c.disabled_at IS NULL
		  WHERE s.enabled = TRUE AND s.next_run_at <= $1
		    AND NOT EXISTS (
		      SELECT 1 FROM tiktok_shop_order_sync_runs r WHERE r.shop_id = s.shop_id AND r.status = 'running'
		    )
		  ORDER BY s.next_run_at, s.shop_id
		  FOR UPDATE OF s SKIP LOCKED
		  LIMIT 1`, now,
	).Scan(&shopID, &connectionID, &intervalSeconds, &overlapSeconds, &watermark); err != nil {
		return TikTokOrderReconcileRun{}, err
	}
	windowEnd := now.Add(-tikTokOrderReconcileSafetyLag)
	windowStart := windowEnd.Add(-time.Duration(overlapSeconds) * time.Second)
	if watermark.Valid && watermark.Time.Before(windowEnd) {
		windowStart = watermark.Time.UTC().Add(-time.Duration(overlapSeconds) * time.Second)
		if windowEnd.Sub(windowStart) > tikTokOrderReconcileMaxSpan {
			windowEnd = windowStart.Add(tikTokOrderReconcileMaxSpan)
		}
	}
	var runID string
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO tiktok_shop_order_sync_runs
		  (gateway_connection_id, shop_id, trigger_source, status, window_start, window_end, page_size, max_pages)
		 VALUES ($1::uuid, $2, 'schedule', 'running', $3, $4, $5, $6)
		 RETURNING id::text`,
		connectionID, shopID, windowStart, windowEnd, tikTokOrderReconcilePageSize, tikTokOrderReconcileMaxPages,
	).Scan(&runID); err != nil {
		return TikTokOrderReconcileRun{}, err
	}
	nextRun := now.Add(time.Duration(intervalSeconds) * time.Second)
	result, err := tx.ExecContext(ctx,
		`UPDATE tiktok_shop_order_sync_settings
		    SET next_run_at = $2, updated_at = NOW()
		  WHERE shop_id = $1`, shopID, nextRun,
	)
	if err != nil {
		return TikTokOrderReconcileRun{}, err
	}
	if err := requireOneReconcileRow(result); err != nil {
		return TikTokOrderReconcileRun{}, err
	}
	if err := tx.Commit(); err != nil {
		return TikTokOrderReconcileRun{}, err
	}
	return TikTokOrderReconcileRun{ID: runID, ShopID: shopID, WindowStart: windowStart, WindowEnd: windowEnd}, nil
}

func (s *TikTokOrderReconcileStore) UpdateSetting(ctx context.Context, shopID string, input TikTokOrderSyncSettingUpdate) (*TikTokOrderSyncSetting, error) {
	shopID = strings.TrimSpace(shopID)
	if s == nil || s.database == nil || !tikTokNumericIDPattern.MatchString(shopID) || input.Validate() != nil {
		return nil, ErrInvalidOrderReconcileInput
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var exists int
	if err := tx.QueryRowContext(ctx,
		`SELECT 1 FROM tiktok_shop_connections WHERE shop_id = $1 AND disabled_at IS NULL FOR SHARE`, shopID,
	).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidOrderReconcileInput
		}
		return nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO tiktok_shop_order_sync_settings (shop_id)
		 VALUES ($1)
		 ON CONFLICT (shop_id) DO NOTHING`, shopID,
	); err != nil {
		return nil, err
	}
	setting := &TikTokOrderSyncSetting{}
	if err := scanTikTokOrderSyncSetting(tx.QueryRowContext(ctx,
		`UPDATE tiktok_shop_order_sync_settings
		    SET enabled = $2, interval_seconds = $3, overlap_seconds = $4,
		        config_version = config_version + 1,
		        next_run_at = CASE WHEN $2 THEN NOW() ELSE next_run_at END,
		        last_error_code = '', last_error_message = '', updated_at = NOW()
		  WHERE shop_id = $1 AND config_version = $5
		  RETURNING shop_id, enabled, interval_seconds, overlap_seconds, watermark_update_at, next_run_at,
		            last_success_at, last_error_code, last_error_message, config_version`,
		shopID, input.Enabled, input.IntervalSeconds, input.OverlapSeconds, input.ConfigVersion,
	), setting); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrOrderSyncVersionConflict
		}
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return setting, nil
}

func (s *TikTokOrderReconcileStore) ListSettings(ctx context.Context) ([]TikTokOrderSyncSetting, error) {
	if s == nil || s.database == nil {
		return nil, ErrInvalidOrderReconcileInput
	}
	rows, err := s.database.QueryContext(ctx,
		`SELECT c.shop_id, c.shop_name,
		        COALESCE(s.enabled, FALSE), COALESCE(s.interval_seconds, 300), COALESCE(s.overlap_seconds, 900),
		        s.watermark_update_at, COALESCE(s.next_run_at, NOW()), s.last_success_at,
		        COALESCE(s.last_error_code, ''), COALESCE(s.last_error_message, ''), COALESCE(s.config_version, 1)
		   FROM tiktok_shop_connections c
		   LEFT JOIN tiktok_shop_order_sync_settings s ON s.shop_id = c.shop_id
		  WHERE c.disabled_at IS NULL
		  ORDER BY c.shop_name, c.shop_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	settings := make([]TikTokOrderSyncSetting, 0)
	for rows.Next() {
		var setting TikTokOrderSyncSetting
		var watermark, lastSuccess sql.NullTime
		if err := rows.Scan(
			&setting.ShopID, &setting.ShopName, &setting.Enabled, &setting.IntervalSeconds, &setting.OverlapSeconds,
			&watermark, &setting.NextRunAt, &lastSuccess, &setting.LastErrorCode, &setting.LastErrorMessage, &setting.ConfigVersion,
		); err != nil {
			return nil, err
		}
		if watermark.Valid {
			value := watermark.Time.UTC()
			setting.WatermarkUpdateAt = &value
		}
		if lastSuccess.Valid {
			value := lastSuccess.Time.UTC()
			setting.LastSuccessAt = &value
		}
		settings = append(settings, setting)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return settings, nil
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

type reconcileSettingScanner interface {
	Scan(...any) error
}

func scanTikTokOrderSyncSetting(scanner reconcileSettingScanner, output *TikTokOrderSyncSetting) error {
	if output == nil {
		return ErrInvalidOrderReconcileInput
	}
	var watermark, lastSuccess sql.NullTime
	if err := scanner.Scan(
		&output.ShopID, &output.Enabled, &output.IntervalSeconds, &output.OverlapSeconds,
		&watermark, &output.NextRunAt, &lastSuccess, &output.LastErrorCode, &output.LastErrorMessage, &output.ConfigVersion,
	); err != nil {
		return err
	}
	if watermark.Valid {
		value := watermark.Time.UTC()
		output.WatermarkUpdateAt = &value
	}
	if lastSuccess.Valid {
		value := lastSuccess.Time.UTC()
		output.LastSuccessAt = &value
	}
	return nil
}
