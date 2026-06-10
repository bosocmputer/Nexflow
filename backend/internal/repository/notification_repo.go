package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/lib/pq"

	"nexflow/internal/models"
)

type NotificationRepo struct {
	db *sql.DB
}

func NewNotificationRepo(db *sql.DB) *NotificationRepo {
	return &NotificationRepo{db: db}
}

func (r *NotificationRepo) CreateForRoles(ctx context.Context, roles []string, in models.NotificationInput) ([]models.Notification, error) {
	roles = normalizeNotificationRoles(roles)
	if len(roles) == 0 {
		return nil, nil
	}
	in = normalizeNotificationInput(in)
	if in.Title == "" || in.DedupeKey == "" {
		return nil, fmt.Errorf("notification title and dedupe_key are required")
	}
	rows, err := r.db.QueryContext(ctx,
		`INSERT INTO notifications
		  (recipient_id, source, severity, title, body, action_url, entity_type, entity_id, dedupe_key)
		 SELECT id, $1, $2, $3, $4, $5, $6, $7, $8
		   FROM users
		  WHERE role = ANY($9)
		 ON CONFLICT (recipient_id, dedupe_key) DO NOTHING
		 RETURNING id::text, recipient_id::text, source, severity, title, body, action_url,
		           entity_type, entity_id, dedupe_key, read_at, resolved_at, resolved_reason,
		           created_at, updated_at`,
		in.Source, in.Severity, in.Title, in.Body, in.ActionURL, in.EntityType, in.EntityID,
		in.DedupeKey, pq.Array(roles),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Notification{}
	for rows.Next() {
		row, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *NotificationRepo) ListForUser(ctx context.Context, userID string, f models.NotificationFilter) ([]models.Notification, int, error) {
	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	where := "recipient_id = $1"
	args := []interface{}{userID}
	if !f.IncludeResolved {
		where += " AND resolved_at IS NULL"
	}
	if f.UnreadOnly {
		where += " AND read_at IS NULL"
	}
	var unread int
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*)::int FROM notifications WHERE recipient_id = $1 AND read_at IS NULL AND resolved_at IS NULL`,
		userID,
	).Scan(&unread); err != nil {
		return nil, 0, err
	}
	args = append(args, limit)
	rows, err := r.db.QueryContext(ctx,
		`SELECT id::text, recipient_id::text, source, severity, title, body, action_url,
		        entity_type, entity_id, dedupe_key, read_at, resolved_at, resolved_reason,
		        created_at, updated_at
		   FROM notifications
		  WHERE `+where+`
		  ORDER BY created_at DESC
		  LIMIT $`+fmt.Sprint(len(args)),
		args...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []models.Notification{}
	for rows.Next() {
		row, err := scanNotification(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, row)
	}
	return out, unread, rows.Err()
}

func (r *NotificationRepo) UnreadCount(ctx context.Context, userID string) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*)::int FROM notifications WHERE recipient_id = $1 AND read_at IS NULL AND resolved_at IS NULL`,
		userID,
	).Scan(&n)
	return n, err
}

func (r *NotificationRepo) MarkRead(ctx context.Context, userID, id string) (int, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE notifications
		    SET read_at = COALESCE(read_at, NOW()),
		        updated_at = NOW()
		  WHERE recipient_id = $1 AND id = $2::uuid`,
		userID, id,
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (r *NotificationRepo) MarkAllRead(ctx context.Context, userID string) (int, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE notifications
		    SET read_at = COALESCE(read_at, NOW()),
		        updated_at = NOW()
		  WHERE recipient_id = $1 AND read_at IS NULL AND resolved_at IS NULL`,
		userID,
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (r *NotificationRepo) ResolveShopeeShopIssues(ctx context.Context, shopID int64, reason string) (int, error) {
	if shopID <= 0 {
		return 0, nil
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "shop sync recovered"
	}
	shop := fmt.Sprint(shopID)
	res, err := r.db.ExecContext(ctx,
		`UPDATE notifications
		    SET resolved_at = COALESCE(resolved_at, NOW()),
		        resolved_reason = CASE
		          WHEN COALESCE(resolved_reason, '') = '' THEN $2
		          ELSE resolved_reason
		        END,
		        updated_at = NOW()
		  WHERE resolved_at IS NULL
		    AND entity_type = 'shopee_shop'
		    AND entity_id = $1
		    AND (
		      dedupe_key LIKE $3
		      OR dedupe_key LIKE $4
		    )`,
		shop,
		reason,
		fmt.Sprintf("shopee:sync_error:%d:%%", shopID),
		fmt.Sprintf("shopee:token_error:%d:%%", shopID),
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

type notificationScanner interface {
	Scan(dest ...interface{}) error
}

func scanNotification(rows notificationScanner) (models.Notification, error) {
	var out models.Notification
	var readAt, resolvedAt sql.NullTime
	if err := rows.Scan(
		&out.ID, &out.RecipientID, &out.Source, &out.Severity, &out.Title, &out.Body,
		&out.ActionURL, &out.EntityType, &out.EntityID, &out.DedupeKey, &readAt, &resolvedAt,
		&out.ResolvedReason,
		&out.CreatedAt, &out.UpdatedAt,
	); err != nil {
		return out, err
	}
	if readAt.Valid {
		out.ReadAt = &readAt.Time
	}
	if resolvedAt.Valid {
		out.ResolvedAt = &resolvedAt.Time
	}
	return out, nil
}

func normalizeNotificationInput(in models.NotificationInput) models.NotificationInput {
	in.Source = strings.TrimSpace(in.Source)
	if in.Source == "" {
		in.Source = "system"
	}
	in.Severity = strings.ToLower(strings.TrimSpace(in.Severity))
	switch in.Severity {
	case "warning", "error":
	default:
		in.Severity = "info"
	}
	in.Title = strings.TrimSpace(in.Title)
	in.Body = strings.TrimSpace(in.Body)
	in.ActionURL = strings.TrimSpace(in.ActionURL)
	in.EntityType = strings.TrimSpace(in.EntityType)
	in.EntityID = strings.TrimSpace(in.EntityID)
	in.DedupeKey = strings.TrimSpace(in.DedupeKey)
	return in
}

func normalizeNotificationRoles(roles []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, role := range roles {
		role = strings.TrimSpace(role)
		if role == "" || seen[role] {
			continue
		}
		seen[role] = true
		out = append(out, role)
	}
	return out
}
