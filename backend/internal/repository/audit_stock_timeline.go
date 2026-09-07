package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"time"

	"nexflow/internal/models"
)

// AppendStockTimeline projects persisted job evidence without writing audit rows
// or implying that historical HTTP attempts were recorded. One indexed lookup.
func (r *AuditLogRepo) AppendStockTimeline(ctx context.Context, billID string, logs []models.AuditLog) ([]models.AuditLog, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var id, status, docNo string
	var processed, verified, updated sql.NullTime
	err := r.db.QueryRowContext(ctx, `SELECT j.id::text,j.status,a.doc_no,j.processstock_succeeded_at,j.balance_verified_at,j.updated_at
 FROM bills b JOIN bill_sml_attempts a ON a.id=b.current_sml_attempt_id
 JOIN marketplace_stock_recalc_jobs j ON j.sml_attempt_id=a.id AND j.bill_id=b.id
 WHERE b.id=$1`, billID).Scan(&id, &status, &docNo, &processed, &verified, &updated)
	if err == sql.ErrNoRows {
		return logs, nil
	}
	if err != nil {
		return logs, err
	}
	event := models.AuditLog{ID: "stock-job:" + id, TargetID: &billID, Source: "stock_job", Level: "info"}
	switch {
	case status == "completed" && processed.Valid && verified.Valid:
		// Preserve an existing success milestone for this document instead of
		// presenting the same outcome twice on legacy bills.
		for _, old := range logs {
			var detail struct {
				DocNo string `json:"doc_no"`
			}
			if old.Action == "sml_stock_recalc_ok" && json.Unmarshal(old.Detail, &detail) == nil && detail.DocNo == docNo {
				return logs, nil
			}
		}
		event.Action = "sml_stock_job_completed"
		event.CreatedAt = verified.Time
	case (status == "failed" || status == "manual_reconciliation") && updated.Valid:
		event.Action = "sml_stock_job_incomplete"
		event.Level = "warn"
		event.CreatedAt = updated.Time
	default:
		return logs, nil
	}
	detail := map[string]any{"doc_no": docNo, "job_id": id, "evidence_source": "marketplace_stock_recalc_jobs", "status": status}
	if processed.Valid {
		detail["processstock_succeeded_at"] = processed.Time
	}
	event.Detail, _ = json.Marshal(detail)
	logs = append(logs, event)
	sort.SliceStable(logs, func(i, j int) bool { return logs[i].CreatedAt.Before(logs[j].CreatedAt) })
	if len(logs) > 200 {
		logs = logs[len(logs)-200:]
	}
	return logs, nil
}
