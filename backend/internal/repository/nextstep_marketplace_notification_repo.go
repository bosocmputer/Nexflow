package repository

import (
	"context"
	"database/sql"
	"strings"
)

type NextStepMarketplaceNotificationRepo struct {
	db *sql.DB
}

type NextStepMarketplaceSeenInput struct {
	DocNo   string
	DocDate string
	Status  string
}

func NewNextStepMarketplaceNotificationRepo(db *sql.DB) *NextStepMarketplaceNotificationRepo {
	return &NextStepMarketplaceNotificationRepo{db: db}
}

func (r *NextStepMarketplaceNotificationRepo) BaselineCompleted(ctx context.Context) (bool, error) {
	if r == nil || r.db == nil {
		return false, nil
	}
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS (
		   SELECT 1
		     FROM nextstep_marketplace_notification_state
		    WHERE key = 'baseline_completed_at' AND value <> ''
		)`,
	).Scan(&exists)
	return exists, err
}

func (r *NextStepMarketplaceNotificationRepo) MarkBaselineCompleted(ctx context.Context) error {
	if r == nil || r.db == nil {
		return nil
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO nextstep_marketplace_notification_state (key, value, updated_at)
		 VALUES ('baseline_completed_at', NOW()::text, NOW())
		 ON CONFLICT (key) DO UPDATE
		    SET value = CASE
		          WHEN nextstep_marketplace_notification_state.value = '' THEN EXCLUDED.value
		          ELSE nextstep_marketplace_notification_state.value
		        END,
		        updated_at = NOW()`,
	)
	return err
}

// UpsertSeen records that Nexflow has observed a NextStep document. It returns
// inserted=true only for a brand-new doc_no so callers can notify exactly once.
func (r *NextStepMarketplaceNotificationRepo) UpsertSeen(ctx context.Context, in NextStepMarketplaceSeenInput) (bool, error) {
	if r == nil || r.db == nil {
		return false, nil
	}
	in.DocNo = strings.TrimSpace(in.DocNo)
	in.DocDate = strings.TrimSpace(in.DocDate)
	in.Status = strings.TrimSpace(in.Status)
	if in.DocNo == "" {
		return false, nil
	}
	var docNo string
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO nextstep_marketplace_notification_seen (doc_no, doc_date, status)
		 VALUES ($1, NULLIF($2, '')::date, $3)
		 ON CONFLICT (doc_no) DO NOTHING
		 RETURNING doc_no`,
		in.DocNo, in.DocDate, in.Status,
	).Scan(&docNo)
	if err == nil {
		return true, nil
	}
	if err != sql.ErrNoRows {
		return false, err
	}
	_, err = r.db.ExecContext(ctx,
		`UPDATE nextstep_marketplace_notification_seen
		    SET doc_date = COALESCE(NULLIF($2, '')::date, doc_date),
		        status = $3,
		        last_seen_at = NOW()
		  WHERE doc_no = $1`,
		in.DocNo, in.DocDate, in.Status,
	)
	return false, err
}

func (r *NextStepMarketplaceNotificationRepo) MarkNotified(ctx context.Context, docNo string) error {
	if r == nil || r.db == nil {
		return nil
	}
	docNo = strings.TrimSpace(docNo)
	if docNo == "" {
		return nil
	}
	_, err := r.db.ExecContext(ctx,
		`UPDATE nextstep_marketplace_notification_seen
		    SET notified_at = COALESCE(notified_at, NOW()),
		        last_seen_at = NOW()
		  WHERE doc_no = $1`,
		docNo,
	)
	return err
}

func (r *NextStepMarketplaceNotificationRepo) DeleteIfUnnotified(ctx context.Context, docNo string) error {
	if r == nil || r.db == nil {
		return nil
	}
	docNo = strings.TrimSpace(docNo)
	if docNo == "" {
		return nil
	}
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM nextstep_marketplace_notification_seen
		  WHERE doc_no = $1 AND notified_at IS NULL`,
		docNo,
	)
	return err
}
