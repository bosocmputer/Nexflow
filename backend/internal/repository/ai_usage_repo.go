package repository

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"nexflow/internal/models"
)

type AIUsageRepo struct {
	db *sql.DB
}

func NewAIUsageRepo(db *sql.DB) *AIUsageRepo {
	return &AIUsageRepo{db: db}
}

func (r *AIUsageRepo) Log(e models.AIUsageEntry) error {
	if e.Provider == "" {
		e.Provider = "openrouter"
	}
	if e.Status == "" {
		e.Status = "success"
	}
	if e.TotalTokens == 0 {
		e.TotalTokens = e.InputTokens + e.OutputTokens
	}
	meta := e.Metadata
	if meta == nil {
		meta = map[string]interface{}{}
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("ai usage metadata: %w", err)
	}
	_, err = r.db.Exec(
		`INSERT INTO ai_usage_logs
		   (provider, model, feature, operation, bill_id, input_tokens, output_tokens,
		    total_tokens, estimated_cost_usd, duration_ms, status, error, metadata)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		e.Provider, e.Model, e.Feature, e.Operation, e.BillID, e.InputTokens,
		e.OutputTokens, e.TotalTokens, e.EstimatedCostUSD, e.DurationMs, e.Status, e.Error, metaJSON,
	)
	return err
}

func (r *AIUsageRepo) Summary(f models.AIUsageFilter) (models.AIUsageSummary, error) {
	where, args := usageWhere(f)
	var out models.AIUsageSummary
	out.EstimatedTHBRate = 36

	total, err := r.bucket("total", "ตามตัวกรอง", where, args)
	if err != nil {
		return out, err
	}
	out.Total = total

	if out.Today, err = r.bucket("today", "วันนี้", where+" AND created_at >= CURRENT_DATE", args); err != nil {
		return out, err
	}
	if out.SevenDays, err = r.bucket("seven_days", "7 วัน", where+" AND created_at >= NOW() - INTERVAL '7 days'", args); err != nil {
		return out, err
	}
	if out.Month, err = r.bucket("month", "เดือนนี้", where+" AND created_at >= DATE_TRUNC('month', NOW())", args); err != nil {
		return out, err
	}
	if out.ByModel, err = r.grouped("model", where, args); err != nil {
		return out, err
	}
	if out.ByFeature, err = r.grouped("feature", where, args); err != nil {
		return out, err
	}
	if out.Daily, err = r.daily(where, args); err != nil {
		return out, err
	}
	if out.TopExpensive, _, err = r.List(models.AIUsageFilter{
		DateFrom: f.DateFrom, DateTo: f.DateTo, Model: f.Model, Feature: f.Feature,
		Status: f.Status, Page: 1, PageSize: 8,
	}, "estimated_cost_usd DESC, created_at DESC"); err != nil {
		return out, err
	}
	return out, nil
}

func usageWhere(f models.AIUsageFilter) (string, []interface{}) {
	where := "WHERE 1=1"
	args := []interface{}{}
	n := 1
	if f.DateFrom != "" {
		where += fmt.Sprintf(" AND created_at >= $%d::date", n)
		args = append(args, f.DateFrom)
		n++
	}
	if f.DateTo != "" {
		where += fmt.Sprintf(" AND created_at < ($%d::date + INTERVAL '1 day')", n)
		args = append(args, f.DateTo)
		n++
	}
	if f.Model != "" {
		where += fmt.Sprintf(" AND model = $%d", n)
		args = append(args, f.Model)
		n++
	}
	if f.Feature != "" {
		where += fmt.Sprintf(" AND feature = $%d", n)
		args = append(args, f.Feature)
		n++
	}
	if f.Status != "" {
		where += fmt.Sprintf(" AND status = $%d", n)
		args = append(args, f.Status)
	}
	return where, args
}

func (r *AIUsageRepo) bucket(key, label, where string, args []interface{}) (models.AIUsageBucket, error) {
	var b models.AIUsageBucket
	b.Key = key
	b.Label = label
	err := r.db.QueryRow(
		`SELECT COUNT(*),
		        COUNT(*) FILTER (WHERE status = 'success'),
		        COUNT(*) FILTER (WHERE status = 'error'),
		        COALESCE(SUM(input_tokens),0),
		        COALESCE(SUM(output_tokens),0),
		        COALESCE(SUM(total_tokens),0),
		        COALESCE(SUM(estimated_cost_usd),0)
		   FROM ai_usage_logs `+where,
		args...,
	).Scan(&b.Requests, &b.Success, &b.Errors, &b.InputTokens, &b.OutputTokens, &b.TotalTokens, &b.EstimatedCostUSD)
	return b, err
}

func (r *AIUsageRepo) grouped(col, where string, args []interface{}) ([]models.AIUsageBucket, error) {
	if col != "model" && col != "feature" {
		return nil, fmt.Errorf("invalid group column")
	}
	rows, err := r.db.Query(
		`SELECT `+col+`,
		        COUNT(*),
		        COUNT(*) FILTER (WHERE status = 'success'),
		        COUNT(*) FILTER (WHERE status = 'error'),
		        COALESCE(SUM(input_tokens),0),
		        COALESCE(SUM(output_tokens),0),
		        COALESCE(SUM(total_tokens),0),
		        COALESCE(SUM(estimated_cost_usd),0)
		   FROM ai_usage_logs `+where+`
		  GROUP BY `+col+`
		  ORDER BY COALESCE(SUM(estimated_cost_usd),0) DESC, COUNT(*) DESC
		  LIMIT 50`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.AIUsageBucket
	for rows.Next() {
		var b models.AIUsageBucket
		if err := rows.Scan(&b.Key, &b.Requests, &b.Success, &b.Errors, &b.InputTokens, &b.OutputTokens, &b.TotalTokens, &b.EstimatedCostUSD); err != nil {
			return nil, err
		}
		b.Label = b.Key
		out = append(out, b)
	}
	return out, rows.Err()
}

func (r *AIUsageRepo) daily(where string, args []interface{}) ([]models.AIUsageBucket, error) {
	rows, err := r.db.Query(
		`SELECT TO_CHAR(created_at::date, 'YYYY-MM-DD'),
		        COUNT(*),
		        COUNT(*) FILTER (WHERE status = 'success'),
		        COUNT(*) FILTER (WHERE status = 'error'),
		        COALESCE(SUM(input_tokens),0),
		        COALESCE(SUM(output_tokens),0),
		        COALESCE(SUM(total_tokens),0),
		        COALESCE(SUM(estimated_cost_usd),0)
		   FROM ai_usage_logs `+where+`
		  GROUP BY created_at::date
		  ORDER BY created_at::date DESC
		  LIMIT 31`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.AIUsageBucket
	for rows.Next() {
		var b models.AIUsageBucket
		if err := rows.Scan(&b.Key, &b.Requests, &b.Success, &b.Errors, &b.InputTokens, &b.OutputTokens, &b.TotalTokens, &b.EstimatedCostUSD); err != nil {
			return nil, err
		}
		b.Label = b.Key
		out = append(out, b)
	}
	return out, rows.Err()
}

func (r *AIUsageRepo) List(f models.AIUsageFilter, orderBy ...string) ([]models.AIUsageLog, int, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 || f.PageSize > 200 {
		f.PageSize = 50
	}
	where, args := usageWhere(f)
	var total int
	if err := r.db.QueryRow("SELECT COUNT(*) FROM ai_usage_logs "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := "created_at DESC"
	if len(orderBy) > 0 && strings.TrimSpace(orderBy[0]) != "" {
		order = orderBy[0]
	}
	n := len(args) + 1
	query := `SELECT id, provider, model, feature, operation, bill_id, input_tokens,
	                 output_tokens, total_tokens, estimated_cost_usd, duration_ms,
	                 status, error, metadata, created_at
	            FROM ai_usage_logs ` + where + fmt.Sprintf(" ORDER BY %s LIMIT $%d OFFSET $%d", order, n, n+1)
	args = append(args, f.PageSize, (f.Page-1)*f.PageSize)
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []models.AIUsageLog
	for rows.Next() {
		var l models.AIUsageLog
		var billID sql.NullString
		var duration sql.NullInt64
		var metaRaw []byte
		if err := rows.Scan(&l.ID, &l.Provider, &l.Model, &l.Feature, &l.Operation, &billID,
			&l.InputTokens, &l.OutputTokens, &l.TotalTokens, &l.EstimatedCostUSD, &duration,
			&l.Status, &l.Error, &metaRaw, &l.CreatedAt); err != nil {
			return nil, 0, err
		}
		if billID.Valid {
			v := billID.String
			l.BillID = &v
		}
		if duration.Valid {
			v := int(duration.Int64)
			l.DurationMs = &v
		}
		if len(metaRaw) > 0 {
			_ = json.Unmarshal(metaRaw, &l.Metadata)
		}
		out = append(out, l)
	}
	return out, total, rows.Err()
}
