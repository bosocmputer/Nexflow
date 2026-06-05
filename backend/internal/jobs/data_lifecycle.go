package jobs

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

type DataLifecycle struct {
	db                   *sql.DB
	hotLogDays           int
	autoArchiveDays      int
	summaryRetentionDays int
	batchSize            int
	log                  *zap.Logger
}

func NewDataLifecycle(db *sql.DB, hotLogDays, autoArchiveDays, summaryRetentionDays, batchSize int, log *zap.Logger) *DataLifecycle {
	if hotLogDays <= 0 {
		hotLogDays = 90
	}
	if autoArchiveDays <= 0 {
		autoArchiveDays = 180
	}
	if summaryRetentionDays <= 0 {
		summaryRetentionDays = 730
	}
	if batchSize <= 0 || batchSize > 5000 {
		batchSize = 1000
	}
	return &DataLifecycle{
		db:                   db,
		hotLogDays:           hotLogDays,
		autoArchiveDays:      autoArchiveDays,
		summaryRetentionDays: summaryRetentionDays,
		batchSize:            batchSize,
		log:                  log,
	}
}

func (j *DataLifecycle) Register(c *cron.Cron, hour int) {
	if hour < 0 || hour > 23 {
		hour = 2
	}
	_, _ = c.AddFunc(fmt.Sprintf("0 %d * * *", hour), j.Run)
}

func (j *DataLifecycle) Run() {
	start := time.Now()
	j.log.Info("data lifecycle: starting",
		zap.Int("hot_log_days", j.hotLogDays),
		zap.Int("auto_archive_days", j.autoArchiveDays))

	if err := j.rollupAuditLogs(); err != nil {
		j.log.Error("data lifecycle: audit rollup", zap.Error(err))
		return
	}
	if err := j.rollupAIUsage(); err != nil {
		j.log.Error("data lifecycle: ai rollup", zap.Error(err))
		return
	}
	archived, err := j.autoArchiveBills()
	if err != nil {
		j.log.Error("data lifecycle: auto archive bills", zap.Error(err))
		return
	}
	purgedAudit, err := j.purgeAuditLogs()
	if err != nil {
		j.log.Error("data lifecycle: purge audit logs", zap.Error(err))
		return
	}
	purgedAI, err := j.purgeAIUsageLogs()
	if err != nil {
		j.log.Error("data lifecycle: purge ai usage logs", zap.Error(err))
		return
	}
	if err := j.pruneSummaries(); err != nil {
		j.log.Warn("data lifecycle: prune summaries", zap.Error(err))
	}

	j.log.Info("data lifecycle: done",
		zap.Int64("archived_bills", archived),
		zap.Int64("purged_audit_logs", purgedAudit),
		zap.Int64("purged_ai_usage_logs", purgedAI),
		zap.Int64("duration_ms", time.Since(start).Milliseconds()))
}

func (j *DataLifecycle) rollupAuditLogs() error {
	_, err := j.db.Exec(`
		INSERT INTO audit_log_daily_summaries (day, source, action, level, count, error_count, warn_count, first_seen_at, last_seen_at, updated_at)
		SELECT created_at::date,
		       COALESCE(source, ''),
		       COALESCE(action, ''),
		       COALESCE(level, ''),
		       COUNT(*),
		       COUNT(*) FILTER (WHERE level='error'),
		       COUNT(*) FILTER (WHERE level='warn'),
		       MIN(created_at),
		       MAX(created_at),
		       NOW()
		  FROM audit_logs
		 WHERE created_at < NOW() - ($1 || ' days')::INTERVAL
		 GROUP BY created_at::date, COALESCE(source, ''), COALESCE(action, ''), COALESCE(level, '')
		ON CONFLICT (day, source, action, level) DO UPDATE SET
		  count = EXCLUDED.count,
		  error_count = EXCLUDED.error_count,
		  warn_count = EXCLUDED.warn_count,
		  first_seen_at = EXCLUDED.first_seen_at,
		  last_seen_at = EXCLUDED.last_seen_at,
		  updated_at = NOW()`, fmt.Sprintf("%d", j.hotLogDays))
	return err
}

func (j *DataLifecycle) rollupAIUsage() error {
	_, err := j.db.Exec(`
		INSERT INTO ai_usage_daily_summaries (
		  day, provider, model, feature, operation, status, requests,
		  input_tokens, output_tokens, total_tokens, estimated_cost_usd,
		  avg_duration_ms, updated_at
		)
		SELECT created_at::date,
		       COALESCE(provider, ''),
		       COALESCE(model, ''),
		       COALESCE(feature, ''),
		       COALESCE(operation, ''),
		       COALESCE(status, ''),
		       COUNT(*),
		       COALESCE(SUM(input_tokens), 0),
		       COALESCE(SUM(output_tokens), 0),
		       COALESCE(SUM(total_tokens), 0),
		       COALESCE(SUM(estimated_cost_usd), 0),
		       AVG(duration_ms),
		       NOW()
		  FROM ai_usage_logs
		 WHERE created_at < NOW() - ($1 || ' days')::INTERVAL
		 GROUP BY created_at::date, provider, model, feature, operation, status
		ON CONFLICT (day, provider, model, feature, operation, status) DO UPDATE SET
		  requests = EXCLUDED.requests,
		  input_tokens = EXCLUDED.input_tokens,
		  output_tokens = EXCLUDED.output_tokens,
		  total_tokens = EXCLUDED.total_tokens,
		  estimated_cost_usd = EXCLUDED.estimated_cost_usd,
		  avg_duration_ms = EXCLUDED.avg_duration_ms,
		  updated_at = NOW()`, fmt.Sprintf("%d", j.hotLogDays))
	return err
}

func (j *DataLifecycle) autoArchiveBills() (int64, error) {
	res, err := j.db.Exec(`
		UPDATE bills
		   SET archived_at = NOW(),
		       archived_by = NULL,
		       archive_reason = 'auto-archive: sent/skipped older than ' || $1 || ' days'
		 WHERE status IN ('sent','skipped')
		   AND archived_at IS NULL
		   AND created_at < NOW() - ($1 || ' days')::INTERVAL`, fmt.Sprintf("%d", j.autoArchiveDays))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (j *DataLifecycle) purgeAuditLogs() (int64, error) {
	return j.batchDelete("audit_logs", j.hotLogDays)
}

func (j *DataLifecycle) purgeAIUsageLogs() (int64, error) {
	return j.batchDelete("ai_usage_logs", j.hotLogDays)
}

func (j *DataLifecycle) batchDelete(table string, olderThanDays int) (int64, error) {
	var total int64
	for {
		query := fmt.Sprintf(`
			WITH doomed AS (
			  SELECT id FROM %s
			   WHERE created_at < NOW() - ($1 || ' days')::INTERVAL
			   ORDER BY created_at ASC
			   LIMIT $2
			)
			DELETE FROM %s
			 WHERE id IN (SELECT id FROM doomed)`, table, table)
		res, err := j.db.Exec(query, fmt.Sprintf("%d", olderThanDays), j.batchSize)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
		if n == 0 || n < int64(j.batchSize) {
			return total, nil
		}
	}
}

func (j *DataLifecycle) pruneSummaries() error {
	retentionDays := fmt.Sprintf("%d", j.summaryRetentionDays)
	if _, err := j.db.Exec(
		`DELETE FROM audit_log_daily_summaries WHERE day < CURRENT_DATE - ($1 || ' days')::INTERVAL`,
		retentionDays,
	); err != nil {
		return err
	}
	_, err := j.db.Exec(
		`DELETE FROM ai_usage_daily_summaries WHERE day < CURRENT_DATE - ($1 || ' days')::INTERVAL`,
		retentionDays,
	)
	return err
}
