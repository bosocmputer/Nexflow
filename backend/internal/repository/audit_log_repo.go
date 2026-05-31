package repository

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"nexflow/internal/models"
)

type AuditLogRepo struct {
	db *sql.DB
}

type AuditLogListResult struct {
	Logs       []models.AuditLog
	Total      *int
	HasMore    bool
	NextCursor string
	Page       int
	PageSize   int
}

func NewAuditLogRepo(db *sql.DB) *AuditLogRepo {
	return &AuditLogRepo{db: db}
}

// Log writes one audit event. All fields in AuditEntry are optional except Action.
func (r *AuditLogRepo) Log(e models.AuditEntry) error {
	var detailJSON []byte
	if e.Detail != nil {
		var err error
		detailJSON, err = json.Marshal(e.Detail)
		if err != nil {
			return fmt.Errorf("audit log marshal: %w", err)
		}
	}
	level := e.Level
	if level == "" {
		level = "info"
	}
	var traceID *string
	if e.TraceID != "" {
		traceID = &e.TraceID
	}
	_, err := r.db.Exec(
		`INSERT INTO audit_logs (action, target_id, user_id, source, level, duration_ms, trace_id, detail)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		e.Action, e.TargetID, e.UserID, e.Source, level, e.DurationMs, traceID, detailJSON,
	)
	return err
}

// List returns audit logs with optional filters, newest first. It supports both
// legacy page/offset pagination and production keyset pagination via cursor.
func (r *AuditLogRepo) List(f models.AuditLogFilter) (*AuditLogListResult, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 || f.PageSize > 200 {
		f.PageSize = 50
	}
	if f.Limit < 1 || f.Limit > 200 {
		f.Limit = f.PageSize
	}

	where := "WHERE 1=1"
	args := []interface{}{}
	n := 1

	if f.Action != "" {
		where += fmt.Sprintf(" AND a.action = $%d", n)
		args = append(args, f.Action)
		n++
	}
	if f.Source != "" {
		where += fmt.Sprintf(" AND a.source = $%d", n)
		args = append(args, f.Source)
		n++
	}
	if f.Level != "" {
		where += fmt.Sprintf(" AND a.level = $%d", n)
		args = append(args, f.Level)
		n++
	}
	if f.UserID != "" {
		where += fmt.Sprintf(" AND a.user_id = $%d", n)
		args = append(args, f.UserID)
		n++
	}
	if f.DateFrom != "" {
		where += fmt.Sprintf(" AND a.created_at >= $%d::date", n)
		args = append(args, f.DateFrom)
		n++
	}
	if f.DateTo != "" {
		where += fmt.Sprintf(" AND a.created_at < ($%d::date + INTERVAL '1 day')", n)
		args = append(args, f.DateTo)
		n++
	}

	var total *int
	legacyOffset := !f.CursorMode && !f.IncludeTotal
	if f.IncludeTotal || legacyOffset {
		var t int
		if err := r.db.QueryRow("SELECT COUNT(*) FROM audit_logs a "+where, args...).Scan(&t); err != nil {
			return nil, fmt.Errorf("audit count: %w", err)
		}
		total = &t
	}

	useCursor := f.CursorMode
	if f.Cursor != "" {
		cursorTime, cursorID, err := decodeTimeIDCursor(f.Cursor)
		if err != nil {
			return nil, err
		}
		where += fmt.Sprintf(" AND (a.created_at, a.id) < ($%d::timestamptz, $%d::uuid)", n, n+1)
		args = append(args, cursorTime, cursorID)
		n += 2
	}

	limit := f.PageSize
	if useCursor {
		limit = f.Limit
	}
	queryLimit := limit
	if useCursor {
		queryLimit = limit + 1
	}
	query := `SELECT a.id, a.user_id, COALESCE(u.name, ''), COALESCE(u.email, ''), COALESCE(u.role, ''),
	                 a.action, a.target_id, a.source, a.level, a.duration_ms, a.trace_id, a.detail, a.created_at
	          FROM audit_logs a
	          LEFT JOIN users u ON u.id = a.user_id ` +
		where + fmt.Sprintf(" ORDER BY a.created_at DESC, a.id DESC LIMIT $%d", n)
	args = append(args, queryLimit)
	if !useCursor {
		query += fmt.Sprintf(" OFFSET $%d", n+1)
		args = append(args, (f.Page-1)*f.PageSize)
	}

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("audit list: %w", err)
	}
	defer rows.Close()

	var logs []models.AuditLog
	for rows.Next() {
		l, err := scanAuditLog(rows)
		if err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	hasMore := len(logs) > limit
	if hasMore {
		logs = logs[:limit]
	}
	nextCursor := ""
	if hasMore && len(logs) > 0 {
		last := logs[len(logs)-1]
		nextCursor = encodeTimeIDCursor(last.CreatedAt, last.ID)
	}
	return &AuditLogListResult{
		Logs:       logs,
		Total:      total,
		HasMore:    hasMore,
		NextCursor: nextCursor,
		Page:       f.Page,
		PageSize:   limit,
	}, nil
}

// ListByTarget returns audit_log rows whose target_id matches, oldest-first.
// Used by the BillDetail timeline view to show every event tied to one bill
// (created → confirmed → SML send → retried → ...). Caps at 200 rows so a
// pathological bill with many retries doesn't blow up the response.
func (r *AuditLogRepo) ListByTarget(targetID string) ([]models.AuditLog, error) {
	rows, err := r.db.Query(
		`SELECT a.id, a.user_id, COALESCE(u.name, ''), COALESCE(u.email, ''), COALESCE(u.role, ''),
		        a.action, a.target_id, a.source, a.level, a.duration_ms,
		        a.trace_id, a.detail, a.created_at
		 FROM audit_logs a
		 LEFT JOIN users u ON u.id = a.user_id
		 WHERE a.target_id = $1
		 ORDER BY created_at ASC
		 LIMIT 200`,
		targetID,
	)
	if err != nil {
		return nil, fmt.Errorf("audit list by target: %w", err)
	}
	defer rows.Close()

	var out []models.AuditLog
	for rows.Next() {
		l, err := scanAuditLog(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

type auditScanner interface {
	Scan(dest ...interface{}) error
}

func scanAuditLog(s auditScanner) (models.AuditLog, error) {
	var l models.AuditLog
	var source, traceID sql.NullString
	var detailRaw []byte
	var actorName, actorEmail, actorRole string
	if err := s.Scan(&l.ID, &l.UserID, &actorName, &actorEmail, &actorRole,
		&l.Action, &l.TargetID,
		&source, &l.Level, &l.DurationMs, &traceID,
		&detailRaw, &l.CreatedAt); err != nil {
		return l, err
	}
	l.Source = source.String
	l.TraceID = traceID.String
	l.Actor = buildAuditActor(l.UserID, actorName, actorEmail, actorRole, l.Source)
	if detailRaw != nil {
		l.Detail = json.RawMessage(detailRaw)
	}
	return l, nil
}

func auditCursorForTest(t time.Time, id string) string {
	return encodeTimeIDCursor(t, id)
}

func buildAuditActor(userID *string, name, email, role, source string) *models.AuditActor {
	if userID != nil && *userID != "" {
		display := name
		if display == "" {
			display = email
		}
		if display == "" {
			display = "Unknown user"
		}
		return &models.AuditActor{
			ID:    *userID,
			Name:  display,
			Email: email,
			Role:  role,
			Type:  "user",
		}
	}
	switch source {
	case "email", "shopee_email", "shopee_shipped":
		return &models.AuditActor{Name: "Email worker", Type: "worker"}
	case "system", "setup":
		return &models.AuditActor{Name: "System", Type: "system"}
	default:
		return &models.AuditActor{Name: "System", Type: "system"}
	}
}
