package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/lib/pq"
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
		where, args, n = appendAuditSourceFilter(where, args, n, f.Source)
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

func appendAuditSourceFilter(where string, args []interface{}, n int, source string) (string, []interface{}, int) {
	switch source {
	case "tiktok_shop":
		where += fmt.Sprintf(" AND (a.source = $%d OR a.action LIKE $%d OR COALESCE(a.detail->>'flow', '') = $%d)", n, n+1, n+2)
		args = append(args, "tiktok_shop", "tiktok_shop_%", "tiktok_shop_api_reviewed")
		return where, args, n + 3
	case "tiktok":
		where += fmt.Sprintf(" AND a.source = $%d AND NOT (a.action LIKE $%d OR COALESCE(a.detail->>'flow', '') = $%d)", n, n+1, n+2)
		args = append(args, "tiktok", "tiktok_shop_%", "tiktok_shop_api_reviewed")
		return where, args, n + 3
	default:
		where += fmt.Sprintf(" AND a.source = $%d", n)
		args = append(args, source)
		return where, args, n + 1
	}
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

type smlAuditResolution struct {
	BillID     string
	DocNo      string
	Route      string
	State      string
	CoreStatus string
	BillStatus string
	Current    bool
}

func auditDetailIdentity(detail json.RawMessage) (attemptID, exchangeID, docNo, route string) {
	if len(detail) == 0 {
		return "", "", "", ""
	}
	var raw struct {
		AttemptID      string `json:"attempt_id"`
		ExchangeID     string `json:"exchange_id"`
		DocNo          string `json:"doc_no"`
		DocNoAttempted string `json:"doc_no_attempted"`
		Route          string `json:"route"`
	}
	if json.Unmarshal(detail, &raw) != nil {
		return "", "", "", ""
	}
	docNo = strings.TrimSpace(raw.DocNo)
	if docNo == "" {
		docNo = strings.TrimSpace(raw.DocNoAttempted)
	}
	return strings.TrimSpace(raw.AttemptID), strings.TrimSpace(raw.ExchangeID), docNo, strings.TrimSpace(raw.Route)
}

var auditUUIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

var smlAttemptAuditActions = map[string]struct{}{
	"profile_requested": {}, "core_committed": {}, "reconcile_queued": {},
	"profile_complete": {}, "profile_terminal_failure": {}, "profile_retry_requested": {},
	"sml_sent": {}, "sml_failed": {}, "sml_erp_log_warning": {},
	"sml_stock_recalc_ok": {}, "sml_stock_recalc_failed": {},
}

func legacySMLAttemptKey(billID, docNo, route string) string {
	if billID == "" || docNo == "" || route == "" {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(billID) + "\x00" + strings.TrimSpace(docNo) + "\x00" + strings.TrimSpace(route))
}

func coreAlreadyCreated(state, coreStatus, billStatus string) bool {
	if state == "sent" || billStatus == "sent" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(coreStatus)) {
	case "created", "already_exists", "complete":
		return true
	default:
		return false
	}
}

// EnrichSMLResolution adds backend-owned retry eligibility. Failure to enrich
// is safe: CanRetry defaults false, so callers never expose an unsafe resend.
func (r *AuditLogRepo) EnrichSMLResolution(ctx context.Context, logs []models.AuditLog) error {
	if r == nil || r.db == nil || len(logs) == 0 {
		return nil
	}
	attemptIDs := make([]string, 0)
	billIDs := make([]string, 0)
	seen := make(map[string]struct{})
	seenBills := make(map[string]struct{})
	traceAttempts := make(map[string]map[string]struct{})
	for index := range logs {
		attemptID, exchangeID, _, _ := auditDetailIdentity(logs[index].Detail)
		logs[index].AttemptID = attemptID
		logs[index].ExchangeID = exchangeID
		if attemptID != "" && strings.TrimSpace(logs[index].TraceID) != "" {
			ids := traceAttempts[logs[index].TraceID]
			if ids == nil {
				ids = make(map[string]struct{})
				traceAttempts[logs[index].TraceID] = ids
			}
			ids[attemptID] = struct{}{}
		}
		if attemptID == "" {
			if _, relevant := smlAttemptAuditActions[logs[index].Action]; relevant && logs[index].TargetID != nil {
				billID := strings.TrimSpace(*logs[index].TargetID)
				if auditUUIDPattern.MatchString(billID) {
					if _, ok := seenBills[billID]; !ok {
						seenBills[billID] = struct{}{}
						billIDs = append(billIDs, billID)
					}
				}
			}
			continue
		}
		if _, ok := seen[attemptID]; !ok {
			seen[attemptID] = struct{}{}
			attemptIDs = append(attemptIDs, attemptID)
		}
	}
	if len(attemptIDs) == 0 && len(billIDs) == 0 {
		return nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT a.id::text,a.bill_id::text,a.doc_no,a.route,a.state,COALESCE(a.core_status,''),b.status,
		(b.current_sml_attempt_id=a.id) AS is_current
		FROM bill_sml_attempts a JOIN bills b ON b.id=a.bill_id
		WHERE a.id=ANY($1::uuid[]) OR a.bill_id=ANY($2::uuid[])`, pq.Array(attemptIDs), pq.Array(billIDs))
	if err != nil {
		return err
	}
	defer rows.Close()
	resolutions := make(map[string]smlAuditResolution, len(attemptIDs))
	legacyMatches := make(map[string][]string)
	for rows.Next() {
		var id string
		var value smlAuditResolution
		if err := rows.Scan(&id, &value.BillID, &value.DocNo, &value.Route, &value.State, &value.CoreStatus, &value.BillStatus, &value.Current); err != nil {
			return err
		}
		resolutions[id] = value
		key := legacySMLAttemptKey(value.BillID, value.DocNo, value.Route)
		if key != "" {
			legacyMatches[key] = append(legacyMatches[key], id)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for index := range logs {
		if logs[index].AttemptID != "" {
			continue
		}
		if ids := traceAttempts[strings.TrimSpace(logs[index].TraceID)]; len(ids) == 1 {
			for id := range ids {
				logs[index].AttemptID = id
			}
			continue
		}
		if logs[index].TargetID == nil {
			continue
		}
		_, _, docNo, route := auditDetailIdentity(logs[index].Detail)
		key := legacySMLAttemptKey(*logs[index].TargetID, docNo, route)
		if ids := legacyMatches[key]; len(ids) == 1 {
			logs[index].AttemptID = ids[0]
		}
	}
	for index := range logs {
		value, ok := resolutions[logs[index].AttemptID]
		if !ok {
			continue
		}
		created := coreAlreadyCreated(value.State, value.CoreStatus, value.BillStatus)
		switch {
		case logs[index].Action == "sml_failed" && created:
			logs[index].ResolutionStatus = "resolved"
		case logs[index].Action == "sml_failed" && (value.State == "unknown" || value.State == "sending"):
			logs[index].ResolutionStatus = "unknown"
		case logs[index].Action == "sml_failed":
			logs[index].ResolutionStatus = "unresolved"
			logs[index].CanRetry = value.Current && value.State == "failed_exact_retry" && !created
		case created:
			logs[index].ResolutionStatus = "succeeded"
		case value.State == "unknown" || value.State == "sending":
			logs[index].ResolutionStatus = "unknown"
		default:
			logs[index].ResolutionStatus = "unresolved"
		}
	}
	return nil
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
