package repository

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"nexflow/internal/models"
	"github.com/lib/pq"
)

type SMLBulkJobRepo struct {
	db *sql.DB
}

type CreateSMLBulkJobInput struct {
	ClientRequestID string
	BillIDs         []string
	Source          string
	BillType        string
	DocumentRoute   string
	Title           string
	RequestPayload  json.RawMessage
	FilterSnapshot  json.RawMessage
	CreatedBy       string
	CreatedByEmail  string
}

type SMLBulkJobListFilter struct {
	Status        string
	Source        string
	BillType      string
	DocumentRoute string
	Page          int
	PerPage       int
}

type SMLBulkJobListResult struct {
	Jobs    []models.SMLBulkJob
	Total   int
	Page    int
	PerPage int
}

type ActiveBulkJobConflictError struct {
	BillIDs []string
}

func (e ActiveBulkJobConflictError) Error() string {
	if len(e.BillIDs) == 0 {
		return "some bills are already in an active bulk job"
	}
	return "bills already in an active bulk job: " + strings.Join(e.BillIDs, ", ")
}

func NewSMLBulkJobRepo(db *sql.DB) *SMLBulkJobRepo {
	return &SMLBulkJobRepo{db: db}
}

func (r *SMLBulkJobRepo) List(filter SMLBulkJobListFilter) (SMLBulkJobListResult, error) {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PerPage <= 0 {
		filter.PerPage = 20
	}
	if filter.PerPage > 100 {
		filter.PerPage = 100
	}

	where := []string{"1=1"}
	args := []interface{}{}
	argN := 1
	if strings.TrimSpace(filter.Status) != "" {
		where = append(where, fmt.Sprintf("j.status = $%d", argN))
		args = append(args, strings.TrimSpace(filter.Status))
		argN++
	}
	if strings.TrimSpace(filter.Source) != "" {
		where = append(where, fmt.Sprintf("j.source = $%d", argN))
		args = append(args, strings.TrimSpace(filter.Source))
		argN++
	}
	if strings.TrimSpace(filter.BillType) != "" {
		where = append(where, fmt.Sprintf("j.bill_type = $%d", argN))
		args = append(args, strings.TrimSpace(filter.BillType))
		argN++
	}
	if strings.TrimSpace(filter.DocumentRoute) != "" {
		where = append(where, fmt.Sprintf("j.document_route = $%d", argN))
		args = append(args, strings.TrimSpace(filter.DocumentRoute))
		argN++
	}

	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := r.db.QueryRow(`SELECT COUNT(*)::int FROM sml_bulk_jobs j WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return SMLBulkJobListResult{}, fmt.Errorf("count bulk jobs: %w", err)
	}

	offset := (filter.Page - 1) * filter.PerPage
	queryArgs := append(append([]interface{}{}, args...), filter.PerPage, offset)
	rows, err := r.db.Query(
		smlBulkJobSelect+` WHERE `+whereSQL+fmt.Sprintf(` ORDER BY j.created_at DESC, j.id DESC LIMIT $%d OFFSET $%d`, argN, argN+1),
		queryArgs...,
	)
	if err != nil {
		return SMLBulkJobListResult{}, fmt.Errorf("list bulk jobs: %w", err)
	}
	defer rows.Close()

	jobs := []models.SMLBulkJob{}
	for rows.Next() {
		job, err := r.scanJob(rows)
		if err != nil {
			return SMLBulkJobListResult{}, err
		}
		jobs = append(jobs, *job)
	}
	if err := rows.Err(); err != nil {
		return SMLBulkJobListResult{}, err
	}

	return SMLBulkJobListResult{
		Jobs:    jobs,
		Total:   total,
		Page:    filter.Page,
		PerPage: filter.PerPage,
	}, nil
}

func (r *SMLBulkJobRepo) FindByClientRequestID(clientRequestID string) (*models.SMLBulkJob, error) {
	clientRequestID = strings.TrimSpace(clientRequestID)
	if clientRequestID == "" {
		return nil, nil
	}
	job, err := r.scanJob(r.db.QueryRow(smlBulkJobSelect+` WHERE j.client_request_id = $1`, clientRequestID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find bulk job by client_request_id: %w", err)
	}
	return r.Get(job.ID)
}

func (r *SMLBulkJobRepo) FindActive(source, billType, documentRoute, userID string) (*models.SMLBulkJob, error) {
	where := []string{"j.status IN ('queued','running')"}
	args := []interface{}{}
	argN := 1
	if strings.TrimSpace(source) != "" {
		where = append(where, fmt.Sprintf("j.source = $%d", argN))
		args = append(args, strings.TrimSpace(source))
		argN++
	}
	if strings.TrimSpace(billType) != "" {
		where = append(where, fmt.Sprintf("j.bill_type = $%d", argN))
		args = append(args, strings.TrimSpace(billType))
		argN++
	}
	if strings.TrimSpace(documentRoute) != "" {
		where = append(where, fmt.Sprintf("j.document_route = $%d", argN))
		args = append(args, strings.TrimSpace(documentRoute))
		argN++
	}
	if strings.TrimSpace(userID) != "" {
		where = append(where, fmt.Sprintf("j.created_by = $%d::uuid", argN))
		args = append(args, strings.TrimSpace(userID))
		argN++
	}

	query := smlBulkJobSelect + ` WHERE ` + strings.Join(where, " AND ") + ` ORDER BY j.created_at DESC LIMIT 1`
	job, err := r.scanJob(r.db.QueryRow(query, args...))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find active bulk job: %w", err)
	}
	return r.Get(job.ID)
}

func (r *SMLBulkJobRepo) Create(input CreateSMLBulkJobInput) (*models.SMLBulkJob, error) {
	if len(input.BillIDs) == 0 {
		return nil, fmt.Errorf("bill_ids is required")
	}
	requestPayload := input.RequestPayload
	if len(requestPayload) == 0 {
		requestPayload = json.RawMessage(`{}`)
	}
	filterSnapshot := input.FilterSnapshot
	if len(filterSnapshot) == 0 {
		filterSnapshot = json.RawMessage(`{}`)
	}

	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`LOCK TABLE sml_bulk_job_items IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return nil, fmt.Errorf("lock bulk job items: %w", err)
	}

	conflicts, err := activeBulkJobConflicts(tx, input.BillIDs)
	if err != nil {
		return nil, err
	}
	if len(conflicts) > 0 {
		return nil, ActiveBulkJobConflictError{BillIDs: conflicts}
	}

	var createdBy *string
	if strings.TrimSpace(input.CreatedBy) != "" {
		v := strings.TrimSpace(input.CreatedBy)
		createdBy = &v
	}
	job, err := r.scanJob(tx.QueryRow(
		`INSERT INTO sml_bulk_jobs
		   (client_request_id, status, source, bill_type, document_route, title,
		    request_payload, filter_snapshot, total_count, created_by, created_by_email)
		 VALUES ($1, 'queued', $2, $3, $4, $5, $6, $7, $8, NULLIF($9, '')::uuid, $10)
		 RETURNING `+smlBulkJobReturningColumns,
		strings.TrimSpace(input.ClientRequestID),
		strings.TrimSpace(input.Source),
		strings.TrimSpace(input.BillType),
		strings.TrimSpace(input.DocumentRoute),
		strings.TrimSpace(input.Title),
		requestPayload,
		filterSnapshot,
		len(input.BillIDs),
		stringOrEmptyPtr(createdBy),
		strings.TrimSpace(input.CreatedByEmail),
	))
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			if existing, findErr := r.FindByClientRequestID(input.ClientRequestID); findErr == nil && existing != nil {
				return existing, nil
			}
		}
		return nil, fmt.Errorf("insert bulk job: %w", err)
	}

	for i, billID := range input.BillIDs {
		var itemID string
		err := tx.QueryRow(
			`INSERT INTO sml_bulk_job_items (job_id, bill_id, sequence, order_no)
			 SELECT $1::uuid,
			        b.id,
			        $2,
			        COALESCE(
			          NULLIF(b.sml_order_id, ''),
			          NULLIF(b.raw_data->>'order_id', ''),
			          NULLIF(b.raw_data->>'shopee_order_id', ''),
			          NULLIF(b.raw_data->>'lazada_order_id', ''),
			          NULLIF(b.raw_data->>'tiktok_order_id', ''),
			          LEFT(b.id::text, 8)
			        )
			   FROM bills b
			  WHERE b.id = $3::uuid
			 RETURNING id::text`,
			job.ID, i+1, strings.TrimSpace(billID),
		).Scan(&itemID)
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("bill not found: %s", billID)
		}
		if err != nil {
			return nil, fmt.Errorf("insert bulk job item: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.Get(job.ID)
}

func activeBulkJobConflicts(tx *sql.Tx, billIDs []string) ([]string, error) {
	rows, err := tx.Query(`
		SELECT DISTINCT i.bill_id::text
		  FROM sml_bulk_job_items i
		  JOIN sml_bulk_jobs j ON j.id = i.job_id
		 WHERE i.bill_id = ANY($1::uuid[])
		   AND j.status IN ('queued','running')
		   AND i.status IN ('queued','running')
		 ORDER BY i.bill_id::text
		 LIMIT 20`,
		pq.Array(billIDs),
	)
	if err != nil {
		return nil, fmt.Errorf("check active bulk job conflicts: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *SMLBulkJobRepo) Get(id string) (*models.SMLBulkJob, error) {
	job, err := r.scanJob(r.db.QueryRow(smlBulkJobSelect+` WHERE j.id = $1::uuid`, strings.TrimSpace(id)))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get bulk job: %w", err)
	}
	items, err := r.ListItems(id)
	if err != nil {
		return nil, err
	}
	job.Items = items
	return job, nil
}

func (r *SMLBulkJobRepo) ListItems(jobID string) ([]models.SMLBulkJobItem, error) {
	rows, err := r.db.Query(
		`SELECT id::text, job_id::text, bill_id::text, sequence, status, order_no,
		        doc_no_attempted, doc_no, error, attempts, started_at, finished_at,
		        created_at, updated_at
		   FROM sml_bulk_job_items
		  WHERE job_id = $1::uuid
		  ORDER BY sequence ASC`,
		strings.TrimSpace(jobID),
	)
	if err != nil {
		return nil, fmt.Errorf("list bulk job items: %w", err)
	}
	defer rows.Close()
	items := []models.SMLBulkJobItem{}
	for rows.Next() {
		item, err := scanBulkJobItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *SMLBulkJobRepo) StartJob(jobID string) error {
	_, err := r.db.Exec(`
		UPDATE sml_bulk_jobs
		   SET status = 'running',
		       started_at = COALESCE(started_at, NOW()),
		       updated_at = NOW()
		 WHERE id = $1::uuid
		   AND status IN ('queued','running')`,
		strings.TrimSpace(jobID),
	)
	return err
}

func (r *SMLBulkJobRepo) StartItem(itemID string) error {
	_, err := r.db.Exec(`
		UPDATE sml_bulk_job_items
		   SET status = 'running',
		       attempts = attempts + 1,
		       started_at = COALESCE(started_at, NOW()),
		       updated_at = NOW()
		 WHERE id = $1::uuid`,
		strings.TrimSpace(itemID),
	)
	return err
}

func (r *SMLBulkJobRepo) FinishItemSent(itemID, docNo, docNoAttempted string) error {
	return r.finishItem(itemID, models.SMLBulkJobItemSent, docNo, docNoAttempted, "")
}

func (r *SMLBulkJobRepo) FinishItemFailed(itemID, errMsg, docNoAttempted string) error {
	return r.finishItem(itemID, models.SMLBulkJobItemFailed, "", docNoAttempted, errMsg)
}

func (r *SMLBulkJobRepo) FinishItemSkipped(itemID, reason string) error {
	return r.finishItem(itemID, models.SMLBulkJobItemSkipped, "", "", reason)
}

func (r *SMLBulkJobRepo) finishItem(itemID string, status models.SMLBulkJobItemStatus, docNo, docNoAttempted, errMsg string) error {
	_, err := r.db.Exec(`
		UPDATE sml_bulk_job_items
		   SET status = $2,
		       doc_no = $3,
		       doc_no_attempted = $4,
		       error = $5,
		       finished_at = NOW(),
		       updated_at = NOW()
		 WHERE id = $1::uuid`,
		strings.TrimSpace(itemID), string(status), docNo, docNoAttempted, errMsg,
	)
	return err
}

func (r *SMLBulkJobRepo) RefreshCounts(jobID string) error {
	_, err := r.db.Exec(`
		UPDATE sml_bulk_jobs j
		   SET total_count = COALESCE(c.total_count, 0),
		       sent_count = COALESCE(c.sent_count, 0),
		       failed_count = COALESCE(c.failed_count, 0),
		       skipped_count = COALESCE(c.skipped_count, 0),
		       updated_at = NOW()
		  FROM (
		    SELECT job_id,
		           COUNT(*)::int AS total_count,
		           COUNT(*) FILTER (WHERE status = 'sent')::int AS sent_count,
		           COUNT(*) FILTER (WHERE status = 'failed')::int AS failed_count,
		           COUNT(*) FILTER (WHERE status = 'skipped')::int AS skipped_count
		      FROM sml_bulk_job_items
		     WHERE job_id = $1::uuid
		     GROUP BY job_id
		  ) c
		 WHERE j.id = c.job_id`,
		strings.TrimSpace(jobID),
	)
	return err
}

func (r *SMLBulkJobRepo) FinalizeJob(jobID string) error {
	if err := r.RefreshCounts(jobID); err != nil {
		return err
	}
	_, err := r.db.Exec(`
		UPDATE sml_bulk_jobs
		   SET status = CASE
		         WHEN failed_count > 0 THEN 'completed_with_errors'
		         ELSE 'completed'
		       END,
		       finished_at = COALESCE(finished_at, NOW()),
		       updated_at = NOW()
		 WHERE id = $1::uuid`,
		strings.TrimSpace(jobID),
	)
	return err
}

func (r *SMLBulkJobRepo) MarkJobFailed(jobID, errMsg string) error {
	_, err := r.db.Exec(`
		UPDATE sml_bulk_jobs
		   SET status = 'failed',
		       last_error = $2,
		       finished_at = COALESCE(finished_at, NOW()),
		       updated_at = NOW()
		 WHERE id = $1::uuid`,
		strings.TrimSpace(jobID), errMsg,
	)
	return err
}

func (r *SMLBulkJobRepo) FailedBillIDs(jobID string) ([]string, error) {
	rows, err := r.db.Query(`
		SELECT bill_id::text
		  FROM sml_bulk_job_items
		 WHERE job_id = $1::uuid
		   AND status = 'failed'
		 ORDER BY sequence ASC`,
		strings.TrimSpace(jobID),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *SMLBulkJobRepo) RecoverInterruptedActiveJobs(message string) (int, error) {
	rows, err := r.db.Query(`SELECT id::text FROM sml_bulk_jobs WHERE status IN ('queued','running')`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, id := range ids {
		if _, err := r.db.Exec(`
			UPDATE sml_bulk_job_items
			   SET status = 'failed',
			       error = CASE WHEN error = '' THEN $2 ELSE error END,
			       finished_at = COALESCE(finished_at, NOW()),
			       updated_at = NOW()
			 WHERE job_id = $1::uuid
			   AND status IN ('queued','running')`,
			id, message,
		); err != nil {
			return len(ids), err
		}
		if err := r.RefreshCounts(id); err != nil {
			return len(ids), err
		}
		if err := r.MarkJobFailed(id, message); err != nil {
			return len(ids), err
		}
	}
	return len(ids), nil
}

const smlBulkJobColumns = `j.id::text, j.client_request_id, j.status, j.source, j.bill_type, j.document_route, j.title,
j.request_payload, j.filter_snapshot, j.total_count, j.sent_count, j.failed_count, j.skipped_count,
j.created_by::text, j.created_by_email, j.last_error, j.created_at, j.started_at, j.finished_at, j.updated_at`

const smlBulkJobSelect = `SELECT ` + smlBulkJobColumns + ` FROM sml_bulk_jobs j`

const smlBulkJobReturningColumns = `id::text, client_request_id, status, source, bill_type, document_route, title,
request_payload, filter_snapshot, total_count, sent_count, failed_count, skipped_count,
created_by::text, created_by_email, last_error, created_at, started_at, finished_at, updated_at`

type bulkJobScanner interface {
	Scan(dest ...interface{}) error
}

func (r *SMLBulkJobRepo) scanJob(row bulkJobScanner) (*models.SMLBulkJob, error) {
	var job models.SMLBulkJob
	var payload, filters []byte
	var createdBy sql.NullString
	if err := row.Scan(
		&job.ID, &job.ClientRequestID, &job.Status, &job.Source, &job.BillType,
		&job.DocumentRoute, &job.Title, &payload, &filters, &job.TotalCount,
		&job.SentCount, &job.FailedCount, &job.SkippedCount, &createdBy,
		&job.CreatedByEmail, &job.LastError, &job.CreatedAt, &job.StartedAt,
		&job.FinishedAt, &job.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if len(payload) > 0 {
		job.RequestPayload = json.RawMessage(payload)
	}
	if len(filters) > 0 {
		job.FilterSnapshot = json.RawMessage(filters)
	}
	if createdBy.Valid && createdBy.String != "" {
		job.CreatedBy = &createdBy.String
	}
	return &job, nil
}

type bulkJobItemScanner interface {
	Scan(dest ...interface{}) error
}

func scanBulkJobItem(row bulkJobItemScanner) (models.SMLBulkJobItem, error) {
	var item models.SMLBulkJobItem
	err := row.Scan(
		&item.ID, &item.JobID, &item.BillID, &item.Sequence, &item.Status,
		&item.OrderNo, &item.DocNoAttempted, &item.DocNo, &item.Error,
		&item.Attempts, &item.StartedAt, &item.FinishedAt, &item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func stringOrEmptyPtr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
