package repository

import (
	"database/sql"
	"fmt"

	"nexflow/internal/models"
)

type InsightRepo struct {
	db *sql.DB
}

func NewInsightRepo(db *sql.DB) *InsightRepo {
	return &InsightRepo{db: db}
}

// Save inserts or updates the daily insight for today
func (r *InsightRepo) Save(statsJSON, insightText string) error {
	_, err := r.db.Exec(`
		INSERT INTO daily_insights (date, stats_json, insight)
		VALUES (CURRENT_DATE, $1, $2)
		ON CONFLICT (date) DO UPDATE SET
			stats_json = EXCLUDED.stats_json,
			insight    = EXCLUDED.insight
	`, statsJSON, insightText)
	return err
}

// List returns recent daily insights, newest first
func (r *InsightRepo) List(limit int) ([]models.DailyInsight, error) {
	if limit <= 0 {
		limit = 7
	}
	rows, err := r.db.Query(`
		SELECT id, date, stats_json, insight, created_at
		FROM daily_insights
		ORDER BY date DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("InsightRepo.List: %w", err)
	}
	defer rows.Close()

	var items []models.DailyInsight
	for rows.Next() {
		var d models.DailyInsight
		if err := rows.Scan(&d.ID, &d.Date, &d.StatsJSON, &d.Insight, &d.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	return items, rows.Err()
}
