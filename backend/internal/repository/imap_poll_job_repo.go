package repository

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"nexflow/internal/models"
)

type IMAPPollJobRepo struct {
	db *sql.DB
}

type CreateIMAPPollJobInput struct {
	AccountID      string
	CreatedBy      string
	CreatedByEmail string
}

type UpdateIMAPPollJobProgressInput struct {
	TotalCount    int
	ScannedCount  int
	CreatedCount  int
	SkippedCount  int
	FailedCount   int
	BacklogCount  int
	ReasonCounts  map[string]int
	LatestDetails []models.IMAPPollDetail
	LastError     string
}

func NewIMAPPollJobRepo(db *sql.DB) *IMAPPollJobRepo {
	return &IMAPPollJobRepo{db: db}
}

func (r *IMAPPollJobRepo) CreateOrGetActive(input CreateIMAPPollJobInput) (*models.IMAPPollJob, bool, error) {
	if strings.TrimSpace(input.AccountID) == "" {
		return nil, false, fmt.Errorf("account_id is required")
	}
	tx, err := r.db.Begin()
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()

	existing, err := r.getActiveForAccountTx(tx, input.AccountID)
	if err != nil {
		return nil, false, err
	}
	if existing != nil {
		return existing, true, tx.Commit()
	}

	job, err := scanIMAPPollJob(tx.QueryRow(
		`INSERT INTO imap_poll_jobs (account_id, created_by, created_by_email)
		 VALUES ($1::uuid, NULLIF($2, '')::uuid, $3)
		 RETURNING `+imapPollJobReturningColumns,
		strings.TrimSpace(input.AccountID),
		strings.TrimSpace(input.CreatedBy),
		strings.TrimSpace(input.CreatedByEmail),
	))
	if err != nil {
		return nil, false, fmt.Errorf("insert imap poll job: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return job, false, nil
}

func (r *IMAPPollJobRepo) Get(id string) (*models.IMAPPollJob, error) {
	job, err := scanIMAPPollJob(r.db.QueryRow(imapPollJobSelect+` WHERE j.id = $1::uuid`, strings.TrimSpace(id)))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get imap poll job: %w", err)
	}
	return job, nil
}

func (r *IMAPPollJobRepo) ActiveForAccount(accountID string) (*models.IMAPPollJob, error) {
	return r.getActiveForAccountTx(r.db, accountID)
}

func (r *IMAPPollJobRepo) ListActive() ([]models.IMAPPollJob, error) {
	rows, err := r.db.Query(imapPollJobSelect + ` WHERE j.status IN ('queued','running') ORDER BY j.created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list active imap poll jobs: %w", err)
	}
	defer rows.Close()
	out := []models.IMAPPollJob{}
	for rows.Next() {
		job, err := scanIMAPPollJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *IMAPPollJobRepo) Start(id string) error {
	_, err := r.db.Exec(
		`UPDATE imap_poll_jobs
		    SET status='running', started_at=COALESCE(started_at, NOW()), updated_at=NOW()
		  WHERE id=$1::uuid AND status IN ('queued','running')`,
		strings.TrimSpace(id),
	)
	return err
}

func (r *IMAPPollJobRepo) UpdateProgress(id string, in UpdateIMAPPollJobProgressInput) error {
	reasonJSON, _ := json.Marshal(in.ReasonCounts)
	if len(reasonJSON) == 0 || string(reasonJSON) == "null" {
		reasonJSON = []byte(`{}`)
	}
	detailJSON, _ := json.Marshal(in.LatestDetails)
	if len(detailJSON) == 0 || string(detailJSON) == "null" {
		detailJSON = []byte(`[]`)
	}
	_, err := r.db.Exec(
		`UPDATE imap_poll_jobs
		    SET total_count=$2,
		        scanned_count=$3,
		        created_count=$4,
		        skipped_count=$5,
		        failed_count=$6,
		        backlog_count=$7,
		        reason_counts=$8,
		        latest_details=$9,
		        last_error=$10,
		        updated_at=NOW()
		  WHERE id=$1::uuid AND status IN ('queued','running')`,
		strings.TrimSpace(id),
		max0(in.TotalCount),
		max0(in.ScannedCount),
		max0(in.CreatedCount),
		max0(in.SkippedCount),
		max0(in.FailedCount),
		max0(in.BacklogCount),
		reasonJSON,
		detailJSON,
		strings.TrimSpace(in.LastError),
	)
	return err
}

func (r *IMAPPollJobRepo) Finish(id string, status models.IMAPPollJobStatus, lastError string) error {
	if status != models.IMAPPollJobCompleted && status != models.IMAPPollJobCompletedWithErrors && status != models.IMAPPollJobFailed {
		status = models.IMAPPollJobFailed
	}
	_, err := r.db.Exec(
		`UPDATE imap_poll_jobs
		    SET status=$2, last_error=$3, finished_at=NOW(), updated_at=NOW()
		  WHERE id=$1::uuid`,
		strings.TrimSpace(id), string(status), strings.TrimSpace(lastError),
	)
	return err
}

func (r *IMAPPollJobRepo) RecoverInterrupted() (int64, error) {
	res, err := r.db.Exec(
		`UPDATE imap_poll_jobs
		    SET status='failed',
		        last_error=COALESCE(NULLIF(last_error, ''), 'server restarted before job completed'),
		        finished_at=NOW(),
		        updated_at=NOW()
		  WHERE status IN ('queued','running')`,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

type imapPollJobQueryer interface {
	QueryRow(query string, args ...any) *sql.Row
}

func (r *IMAPPollJobRepo) getActiveForAccountTx(q imapPollJobQueryer, accountID string) (*models.IMAPPollJob, error) {
	job, err := scanIMAPPollJob(q.QueryRow(
		imapPollJobSelect+` WHERE j.account_id = $1::uuid AND j.status IN ('queued','running') ORDER BY j.created_at DESC LIMIT 1`,
		strings.TrimSpace(accountID),
	))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find active imap poll job: %w", err)
	}
	return job, nil
}

func scanIMAPPollJob(scanner interface{ Scan(dest ...any) error }) (*models.IMAPPollJob, error) {
	var job models.IMAPPollJob
	var reasonBytes []byte
	var detailBytes []byte
	var createdBy sql.NullString
	err := scanner.Scan(
		&job.ID, &job.AccountID, &job.AccountName, &job.AccountEmail, &job.Status,
		&job.TotalCount, &job.ScannedCount, &job.CreatedCount, &job.SkippedCount,
		&job.FailedCount, &job.BacklogCount, &reasonBytes, &detailBytes, &job.LastError,
		&createdBy, &job.CreatedByEmail, &job.StartedAt, &job.FinishedAt, &job.CreatedAt, &job.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if len(reasonBytes) == 0 {
		reasonBytes = []byte(`{}`)
	}
	job.ReasonCounts = json.RawMessage(reasonBytes)
	if len(detailBytes) > 0 {
		_ = json.Unmarshal(detailBytes, &job.LatestDetails)
	}
	if createdBy.Valid {
		job.CreatedBy = &createdBy.String
	}
	return &job, nil
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

const imapPollJobColumns = `
  j.id::text, j.account_id::text, a.name, a.username, j.status,
  j.total_count, j.scanned_count, j.created_count, j.skipped_count,
  j.failed_count, j.backlog_count, j.reason_counts, j.latest_details,
  j.last_error, j.created_by::text, j.created_by_email,
  j.started_at, j.finished_at, j.created_at, j.updated_at`

const imapPollJobSelect = `SELECT ` + imapPollJobColumns + `
  FROM imap_poll_jobs j
  JOIN imap_accounts a ON a.id = j.account_id`

const imapPollJobReturningColumns = `
  id::text, account_id::text, ''::text, ''::text, status,
  total_count, scanned_count, created_count, skipped_count,
  failed_count, backlog_count, reason_counts, latest_details,
  last_error, created_by::text, created_by_email,
  started_at, finished_at, created_at, updated_at`
