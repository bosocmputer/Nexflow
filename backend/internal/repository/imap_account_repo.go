package repository

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"nexflow/internal/models"
)

type ImapAccountRepo struct {
	db *sql.DB
}

func NewImapAccountRepo(db *sql.DB) *ImapAccountRepo {
	return &ImapAccountRepo{db: db}
}

const imapSelectCols = `
  id, name, host, port, username, password, mailbox,
  filter_from, filter_subjects, channel, shopee_domains,
  lookback_days, poll_interval_seconds, enabled,
  last_polled_at, last_poll_status, last_poll_error, last_poll_messages,
  last_poll_found, last_poll_processed, last_poll_skipped, last_poll_details,
  last_poll_summary, last_seen_uid, last_poll_limited, last_poll_backlog,
  consecutive_failures, last_admin_alert_at, created_at, updated_at
`

func scanImapAccount(s interface{ Scan(...any) error }) (*models.IMAPAccount, error) {
	a := &models.IMAPAccount{}
	var status, errMsg sql.NullString
	var msgCount sql.NullInt32
	var foundCount, processedCount, skippedCount sql.NullInt32
	var backlogCount sql.NullInt32
	var detailBytes []byte
	var summaryBytes []byte
	err := s.Scan(
		&a.ID, &a.Name, &a.Host, &a.Port, &a.Username, &a.Password, &a.Mailbox,
		&a.FilterFrom, &a.FilterSubjects, &a.Channel, &a.ShopeeDomains,
		&a.LookbackDays, &a.PollIntervalSeconds, &a.Enabled,
		&a.LastPolledAt, &status, &errMsg, &msgCount,
		&foundCount, &processedCount, &skippedCount, &detailBytes, &summaryBytes,
		&a.LastSeenUID, &a.LastPollLimited, &backlogCount,
		&a.ConsecutiveFailures, &a.LastAdminAlertAt, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if status.Valid {
		s := status.String
		a.LastPollStatus = &s
	}
	if errMsg.Valid {
		s := errMsg.String
		a.LastPollError = &s
	}
	if msgCount.Valid {
		n := int(msgCount.Int32)
		a.LastPollMessages = &n
	}
	if foundCount.Valid {
		n := int(foundCount.Int32)
		a.LastPollFound = &n
	}
	if processedCount.Valid {
		n := int(processedCount.Int32)
		a.LastPollProcessed = &n
	}
	if skippedCount.Valid {
		n := int(skippedCount.Int32)
		a.LastPollSkipped = &n
	}
	if len(detailBytes) > 0 {
		_ = json.Unmarshal(detailBytes, &a.LastPollDetails)
	}
	if len(summaryBytes) > 0 {
		_ = json.Unmarshal(summaryBytes, &a.LastPollSummary)
	}
	if backlogCount.Valid {
		n := int(backlogCount.Int32)
		a.LastPollBacklog = &n
	}
	return a, nil
}

// CountFailing returns how many enabled inboxes have an active error/warning.
// Processing warnings (e.g. OpenRouter 402 during AI extract) do not increment
// consecutive_failures, but they should still surface on the dashboard.
func (r *ImapAccountRepo) CountFailing() (int, error) {
	var n int
	err := r.db.QueryRow(
		`SELECT COUNT(*) FROM imap_accounts
		 WHERE enabled = TRUE
		   AND (
		     consecutive_failures > 0
		     OR last_poll_status IN ('warning','connect_failed','auth_failed','select_failed','search_failed','fetch_failed','error')
		   )`,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count failing imap_accounts: %w", err)
	}
	return n, nil
}

func (r *ImapAccountRepo) ListAll() ([]*models.IMAPAccount, error) {
	rows, err := r.db.Query(`SELECT ` + imapSelectCols + ` FROM imap_accounts ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("ListAll imap_accounts: %w", err)
	}
	defer rows.Close()

	var out []*models.IMAPAccount
	for rows.Next() {
		a, err := scanImapAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *ImapAccountRepo) ListEnabled() ([]*models.IMAPAccount, error) {
	rows, err := r.db.Query(
		`SELECT ` + imapSelectCols + ` FROM imap_accounts WHERE enabled = TRUE ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("ListEnabled imap_accounts: %w", err)
	}
	defer rows.Close()

	var out []*models.IMAPAccount
	for rows.Next() {
		a, err := scanImapAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *ImapAccountRepo) GetByID(id string) (*models.IMAPAccount, error) {
	row := r.db.QueryRow(`SELECT `+imapSelectCols+` FROM imap_accounts WHERE id = $1`, id)
	a, err := scanImapAccount(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetByID imap_account: %w", err)
	}
	return a, nil
}

// Create inserts a new account. The runtime status fields stay at defaults.
func (r *ImapAccountRepo) Create(a *models.IMAPAccount) error {
	return r.db.QueryRow(
		`INSERT INTO imap_accounts (
		   name, host, port, username, password, mailbox,
		   filter_from, filter_subjects, channel, shopee_domains,
		   lookback_days, poll_interval_seconds, enabled
		 ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		 RETURNING id, created_at, updated_at`,
		a.Name, a.Host, a.Port, a.Username, a.Password, a.Mailbox,
		a.FilterFrom, a.FilterSubjects, a.Channel, a.ShopeeDomains,
		a.LookbackDays, a.PollIntervalSeconds, a.Enabled,
	).Scan(&a.ID, &a.CreatedAt, &a.UpdatedAt)
}

// Update replaces the user-editable fields. If the supplied password is
// empty the existing one is preserved (so the UI can omit it on edit).
func (r *ImapAccountRepo) Update(id string, a *models.IMAPAccount) error {
	if a.Password == "" {
		_, err := r.db.Exec(
			`UPDATE imap_accounts SET
			   name=$2, host=$3, port=$4, username=$5, mailbox=$6,
			   filter_from=$7, filter_subjects=$8, channel=$9, shopee_domains=$10,
			   lookback_days=$11, poll_interval_seconds=$12, enabled=$13,
			   updated_at=NOW()
			 WHERE id=$1`,
			id, a.Name, a.Host, a.Port, a.Username, a.Mailbox,
			a.FilterFrom, a.FilterSubjects, a.Channel, a.ShopeeDomains,
			a.LookbackDays, a.PollIntervalSeconds, a.Enabled,
		)
		return err
	}
	_, err := r.db.Exec(
		`UPDATE imap_accounts SET
		   name=$2, host=$3, port=$4, username=$5, password=$6, mailbox=$7,
		   filter_from=$8, filter_subjects=$9, channel=$10, shopee_domains=$11,
		   lookback_days=$12, poll_interval_seconds=$13, enabled=$14,
		   updated_at=NOW()
		 WHERE id=$1`,
		id, a.Name, a.Host, a.Port, a.Username, a.Password, a.Mailbox,
		a.FilterFrom, a.FilterSubjects, a.Channel, a.ShopeeDomains,
		a.LookbackDays, a.PollIntervalSeconds, a.Enabled,
	)
	return err
}

func (r *ImapAccountRepo) Delete(id string) error {
	_, err := r.db.Exec(`DELETE FROM imap_accounts WHERE id = $1`, id)
	return err
}

// ResetPollProgress clears the resumable IMAP cursor without deleting dedup
// history or bills. Optionally updates lookback_days before the next poll.
func (r *ImapAccountRepo) ResetPollProgress(id string, lookbackDays *int) error {
	if lookbackDays != nil {
		_, err := r.db.Exec(
			`UPDATE imap_accounts SET
			   lookback_days=$2,
			   last_seen_uid=0,
			   last_poll_limited=FALSE,
			   last_poll_backlog=NULL,
			   last_poll_status=NULL,
			   last_poll_error=NULL,
			   last_poll_found=NULL,
			   last_poll_processed=NULL,
			   last_poll_skipped=NULL,
			   last_poll_messages=NULL,
			   last_poll_details='[]'::jsonb,
			   last_poll_summary='{}'::jsonb,
			   consecutive_failures=0,
			   updated_at=NOW()
			 WHERE id=$1`,
			id, *lookbackDays,
		)
		return err
	}
	_, err := r.db.Exec(
		`UPDATE imap_accounts SET
		   last_seen_uid=0,
		   last_poll_limited=FALSE,
		   last_poll_backlog=NULL,
		   last_poll_status=NULL,
		   last_poll_error=NULL,
		   last_poll_found=NULL,
		   last_poll_processed=NULL,
		   last_poll_skipped=NULL,
		   last_poll_messages=NULL,
		   last_poll_details='[]'::jsonb,
		   last_poll_summary='{}'::jsonb,
		   consecutive_failures=0,
		   updated_at=NOW()
		 WHERE id=$1`,
		id,
	)
	return err
}

// UpdatePollStatus is called by the coordinator after each poll cycle.
// status="ok" resets consecutive_failures to 0; status="warning" stores a
// process-level issue without counting it as an IMAP connection failure.
func (r *ImapAccountRepo) UpdatePollStatus(
	id, status, errMsg string,
	foundCount, processedCount, skippedCount int,
	summary models.IMAPPollSummary,
	details []models.IMAPPollDetail,
	lastSeenUID int64,
	limited bool,
	backlogCount int,
) error {
	var em sql.NullString
	if errMsg != "" {
		em = sql.NullString{String: errMsg, Valid: true}
	}
	var backlog sql.NullInt32
	if backlogCount > 0 {
		backlog = sql.NullInt32{Int32: int32(backlogCount), Valid: true}
	}
	detailJSON, _ := json.Marshal(details)
	summaryJSON, _ := json.Marshal(summary)
	if status == "ok" || status == "no_new_mail" || status == "backlog" || status == "partial" || status == "interrupted" {
		_, err := r.db.Exec(
			`UPDATE imap_accounts SET
			   last_polled_at=NOW(), last_poll_status=$2, last_poll_error=NULL,
			   last_poll_messages=$4,
			   last_poll_found=$3, last_poll_processed=$4, last_poll_skipped=$5,
			   last_poll_details=$6, last_poll_summary=$7,
			   last_seen_uid=GREATEST(last_seen_uid, $8),
			   last_poll_limited=$9,
			   last_poll_backlog=$10,
			   consecutive_failures=0
			 WHERE id=$1`,
			id, status, foundCount, processedCount, skippedCount, detailJSON, summaryJSON,
			lastSeenUID, limited, backlog,
		)
		return err
	}
	if status == "warning" {
		_, err := r.db.Exec(
			`UPDATE imap_accounts SET
			   last_polled_at=NOW(), last_poll_status=$2, last_poll_error=$3,
			   last_poll_messages=$5,
			   last_poll_found=$4, last_poll_processed=$5, last_poll_skipped=$6,
			   last_poll_details=$7, last_poll_summary=$8,
			   last_seen_uid=GREATEST(last_seen_uid, $9),
			   last_poll_limited=$10,
			   last_poll_backlog=$11,
			   consecutive_failures=0
			 WHERE id=$1`,
			id, status, em, foundCount, processedCount, skippedCount, detailJSON, summaryJSON,
			lastSeenUID, limited, backlog,
		)
		return err
	}
	_, err := r.db.Exec(
		`UPDATE imap_accounts SET
		   last_polled_at=NOW(), last_poll_status=$2, last_poll_error=$3,
	   last_poll_messages=$5,
	   last_poll_found=$4, last_poll_processed=$5, last_poll_skipped=$6,
	   last_poll_details=$7, last_poll_summary=$8,
	   last_seen_uid=GREATEST(last_seen_uid, $9),
	   last_poll_limited=$10,
	   last_poll_backlog=$11,
	   consecutive_failures = consecutive_failures + 1
	 WHERE id=$1`,
		id, status, em, foundCount, processedCount, skippedCount, detailJSON, summaryJSON,
		lastSeenUID, limited, backlog,
	)
	return err
}

// MarkAlertSent stamps last_admin_alert_at so the LINE notify throttler
// can skip resending until > 1 h has elapsed.
func (r *ImapAccountRepo) MarkAlertSent(id string) error {
	_, err := r.db.Exec(
		`UPDATE imap_accounts SET last_admin_alert_at=NOW() WHERE id=$1`, id)
	return err
}
