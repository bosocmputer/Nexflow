package repository

import (
	"database/sql"
	"fmt"

	"nexflow/internal/models"
)

// LineOAAccountRepo manages line_oa_accounts (one row per LINE OA).
type LineOAAccountRepo struct {
	db *sql.DB
}

func NewLineOAAccountRepo(db *sql.DB) *LineOAAccountRepo {
	return &LineOAAccountRepo{db: db}
}

const lineOACols = `
  id, name, channel_secret, channel_access_token, bot_user_id,
  admin_user_id, greeting, enabled, mark_as_read_enabled,
  created_at, updated_at
`

func scanLineOA(s interface{ Scan(...any) error }) (*models.LineOAAccount, error) {
	a := &models.LineOAAccount{}
	if err := s.Scan(
		&a.ID, &a.Name, &a.ChannelSecret, &a.ChannelAccessToken, &a.BotUserID,
		&a.AdminUserID, &a.Greeting, &a.Enabled, &a.MarkAsReadEnabled,
		&a.CreatedAt, &a.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return a, nil
}

// ListAll returns all OAs (enabled and disabled), ordered by name.
// Used by both the admin UI and the LineRegistry on boot.
func (r *LineOAAccountRepo) ListAll() ([]*models.LineOAAccount, error) {
	rows, err := r.db.Query(`SELECT ` + lineOACols + ` FROM line_oa_accounts ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list line_oa_accounts: %w", err)
	}
	defer rows.Close()
	var out []*models.LineOAAccount
	for rows.Next() {
		a, err := scanLineOA(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListEnabled returns enabled OAs only — used by the LineRegistry to avoid
// loading dead/test rows.
func (r *LineOAAccountRepo) ListEnabled() ([]*models.LineOAAccount, error) {
	rows, err := r.db.Query(
		`SELECT ` + lineOACols + ` FROM line_oa_accounts WHERE enabled = TRUE ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("list enabled line_oa_accounts: %w", err)
	}
	defer rows.Close()
	var out []*models.LineOAAccount
	for rows.Next() {
		a, err := scanLineOA(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Get returns a single OA by ID, or nil (no error) when not found.
func (r *LineOAAccountRepo) Get(id string) (*models.LineOAAccount, error) {
	row := r.db.QueryRow(
		`SELECT `+lineOACols+` FROM line_oa_accounts WHERE id = $1`, id,
	)
	a, err := scanLineOA(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get line_oa_account: %w", err)
	}
	return a, nil
}

// Create inserts a new OA and returns the persisted row.
func (r *LineOAAccountRepo) Create(a *models.LineOAAccount) error {
	row := r.db.QueryRow(
		`INSERT INTO line_oa_accounts
		   (name, channel_secret, channel_access_token, bot_user_id,
		    admin_user_id, greeting, enabled, mark_as_read_enabled)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, created_at, updated_at`,
		a.Name, a.ChannelSecret, a.ChannelAccessToken, a.BotUserID,
		a.AdminUserID, a.Greeting, a.Enabled, a.MarkAsReadEnabled,
	)
	if err := row.Scan(&a.ID, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return fmt.Errorf("create line_oa_account: %w", err)
	}
	return nil
}

// Update mutates an existing OA. Empty channel_secret / channel_access_token
// in the upsert payload mean "don't touch" so admins don't have to re-enter
// long tokens to change just the name or greeting.
func (r *LineOAAccountRepo) Update(id string, in models.LineOAAccountUpsert) (*models.LineOAAccount, error) {
	current, err := r.Get(id)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, fmt.Errorf("line_oa_account %s not found", id)
	}
	secret := in.ChannelSecret
	if secret == "" {
		secret = current.ChannelSecret
	}
	token := in.ChannelAccessToken
	if token == "" {
		token = current.ChannelAccessToken
	}
	enabled := current.Enabled
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	markAsRead := current.MarkAsReadEnabled
	if in.MarkAsReadEnabled != nil {
		markAsRead = *in.MarkAsReadEnabled
	}
	_, err = r.db.Exec(
		`UPDATE line_oa_accounts
		 SET name = $1, channel_secret = $2, channel_access_token = $3,
		     admin_user_id = $4, greeting = $5, enabled = $6,
		     mark_as_read_enabled = $7,
		     updated_at = NOW()
		 WHERE id = $8`,
		in.Name, secret, token, in.AdminUserID, in.Greeting, enabled, markAsRead, id,
	)
	if err != nil {
		return nil, fmt.Errorf("update line_oa_account: %w", err)
	}
	return r.Get(id)
}

// SetBotUserID updates the cached bot_user_id (called by the test-connect
// endpoint after a successful /v2/bot/info call).
func (r *LineOAAccountRepo) SetBotUserID(id, botUserID string) error {
	_, err := r.db.Exec(
		`UPDATE line_oa_accounts SET bot_user_id = $1, updated_at = NOW() WHERE id = $2`,
		botUserID, id,
	)
	return err
}

// Delete removes an OA. chat_conversations.line_oa_id will become NULL
// (ON DELETE SET NULL is NOT in the migration — we use the FK default which
// is NO ACTION, so deletion will fail if any conversation references this OA;
// admin must reassign or delete those conversations first).
func (r *LineOAAccountRepo) Delete(id string) error {
	_, err := r.db.Exec(`DELETE FROM line_oa_accounts WHERE id = $1`, id)
	return err
}

// IsEmpty reports whether the table has zero rows. Used by main.go to decide
// whether to seed the default OA from env vars on first boot.
func (r *LineOAAccountRepo) IsEmpty() (bool, error) {
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM line_oa_accounts`).Scan(&n); err != nil {
		return false, err
	}
	return n == 0, nil
}
