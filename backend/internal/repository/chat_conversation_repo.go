package repository

import (
	"database/sql"
	"fmt"
	"strings"

	"nexflow/internal/models"
)

// ChatConversationRepo manages chat_conversations (one row per LINE user).
type ChatConversationRepo struct {
	db *sql.DB
}

func NewChatConversationRepo(db *sql.DB) *ChatConversationRepo {
	return &ChatConversationRepo{db: db}
}

const chatConvCols = `
  line_user_id, line_oa_id, display_name, picture_url, phone, status,
  last_message_at, last_inbound_at, last_admin_reply_at,
  unread_admin_count, created_at
`

func scanChatConversation(s interface{ Scan(...any) error }) (*models.ChatConversation, error) {
	c := &models.ChatConversation{}
	var lineOAID sql.NullString
	var lastInbound, lastAdminReply sql.NullTime
	err := s.Scan(
		&c.LineUserID, &lineOAID, &c.DisplayName, &c.PictureURL, &c.Phone, &c.Status,
		&c.LastMessageAt, &lastInbound, &lastAdminReply,
		&c.UnreadAdminCount, &c.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	if lineOAID.Valid {
		v := lineOAID.String
		c.LineOAID = &v
	}
	if lastInbound.Valid {
		t := lastInbound.Time
		c.LastInboundAt = &t
	}
	if lastAdminReply.Valid {
		t := lastAdminReply.Time
		c.LastAdminReplyAt = &t
	}
	return c, nil
}

// Upsert is the legacy single-OA entry point — kept for backward compat.
// New callers should use UpsertWithOA so the row gets a line_oa_id.
func (r *ChatConversationRepo) Upsert(lineUserID, displayName, pictureURL string) (*models.ChatConversation, bool, error) {
	return r.UpsertWithOA(lineUserID, displayName, pictureURL, nil)
}

// UpsertWithOA creates or updates a conversation row, optionally setting the
// line_oa_id. The OA id is set ONLY on insert; subsequent calls don't move a
// conversation between OAs (LINE userIDs are scoped per OA anyway, so a row
// can only belong to one OA for its lifetime).
//
// Returns (conversation, isNew). isNew=true on first insert — caller fetches
// the profile from LINE and sends the greeting in that case.
func (r *ChatConversationRepo) UpsertWithOA(
	lineUserID, displayName, pictureURL string, oaID *string,
) (*models.ChatConversation, bool, error) {
	row := r.db.QueryRow(
		`INSERT INTO chat_conversations (line_user_id, line_oa_id, display_name, picture_url)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (line_user_id) DO UPDATE SET
		   display_name = CASE WHEN EXCLUDED.display_name <> '' THEN EXCLUDED.display_name ELSE chat_conversations.display_name END,
		   picture_url  = CASE WHEN EXCLUDED.picture_url  <> '' THEN EXCLUDED.picture_url  ELSE chat_conversations.picture_url  END,
		   -- backfill line_oa_id only if it was previously NULL (don't move OAs)
		   line_oa_id   = COALESCE(chat_conversations.line_oa_id, EXCLUDED.line_oa_id)
		 RETURNING `+chatConvCols+`, (xmax = 0) AS is_new`,
		lineUserID, oaID, displayName, pictureURL,
	)
	c := &models.ChatConversation{}
	var lineOAID sql.NullString
	var lastInbound, lastAdminReply sql.NullTime
	var isNew bool
	if err := row.Scan(
		&c.LineUserID, &lineOAID, &c.DisplayName, &c.PictureURL, &c.Phone, &c.Status,
		&c.LastMessageAt, &lastInbound, &lastAdminReply,
		&c.UnreadAdminCount, &c.CreatedAt, &isNew,
	); err != nil {
		return nil, false, fmt.Errorf("upsert chat_conversation: %w", err)
	}
	if lineOAID.Valid {
		v := lineOAID.String
		c.LineOAID = &v
	}
	if lastInbound.Valid {
		t := lastInbound.Time
		c.LastInboundAt = &t
	}
	if lastAdminReply.Valid {
		t := lastAdminReply.Time
		c.LastAdminReplyAt = &t
	}
	return c, isNew, nil
}

// Get returns a single conversation, or nil (no error) when not found.
func (r *ChatConversationRepo) Get(lineUserID string) (*models.ChatConversation, error) {
	row := r.db.QueryRow(
		`SELECT `+chatConvCols+` FROM chat_conversations WHERE line_user_id = $1`,
		lineUserID,
	)
	c, err := scanChatConversation(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get chat_conversation: %w", err)
	}
	return c, nil
}

// ConversationListRow is a list-view row that joins line_oa_accounts.name so
// the inbox can render an OA badge per conversation without an N+1 query.
type ConversationListRow struct {
	models.ChatConversation
	LineOAName string `json:"line_oa_name,omitempty"`
}

// ConversationListFilter holds the query options for List.
type ConversationListFilter struct {
	Limit      int
	Offset     int
	UnreadOnly bool
	Status     string   // "" = no filter; "open" / "resolved" / "archived"
	Q          string   // case-insensitive substring match on display_name + last text
	TagIDs     []string // ANY-match — conversation must have at least one of these tags
}

// List returns conversations ordered by last_message_at DESC.
// Each row includes the OA's display name (line_oa_name) for badge rendering.
// Phase D adds Q (search) — ILIKE on display_name + the most recent text from
// chat_messages (LATERAL subquery).
func (r *ChatConversationRepo) List(f ConversationListFilter) ([]*ConversationListRow, error) {
	limit, offset := f.Limit, f.Offset
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	args := []any{limit, offset}
	where := ""
	addWhere := func(cond string) {
		if where == "" {
			where = " WHERE " + cond
		} else {
			where += " AND " + cond
		}
	}
	if f.UnreadOnly {
		addWhere("cc.unread_admin_count > 0")
	}
	if f.Status != "" {
		args = append(args, f.Status)
		addWhere(fmt.Sprintf("cc.status = $%d", len(args)))
	}
	if f.Q != "" {
		// ILIKE on display_name OR latest message text. Subquery is LATERAL
		// so it sees the outer cc.line_user_id.
		args = append(args, "%"+f.Q+"%")
		addWhere(fmt.Sprintf(
			`(cc.display_name ILIKE $%d OR EXISTS (
			   SELECT 1 FROM chat_messages m
			   WHERE m.line_user_id = cc.line_user_id
			     AND m.text_content ILIKE $%d
			 ))`, len(args), len(args)))
	}
	if len(f.TagIDs) > 0 {
		// ANY-match: conversation has at least one of the requested tag IDs.
		// pq.Array would let us pass a single $N — but we already use lib/pq
		// strings only, so build a placeholder list inline.
		ph := make([]string, 0, len(f.TagIDs))
		for _, id := range f.TagIDs {
			args = append(args, id)
			ph = append(ph, fmt.Sprintf("$%d", len(args)))
		}
		addWhere(fmt.Sprintf(
			`EXISTS (
			   SELECT 1 FROM chat_conversation_tags ct
			   WHERE ct.line_user_id = cc.line_user_id
			     AND ct.tag_id IN (%s)
			 )`, strings.Join(ph, ",")))
	}

	q := `SELECT
	        cc.line_user_id, cc.line_oa_id, cc.display_name, cc.picture_url,
	        cc.phone, cc.status,
	        cc.last_message_at, cc.last_inbound_at, cc.last_admin_reply_at,
	        cc.unread_admin_count, cc.created_at,
	        COALESCE(oa.name, '') AS line_oa_name
	      FROM chat_conversations cc
	      LEFT JOIN line_oa_accounts oa ON oa.id = cc.line_oa_id` +
		where +
		` ORDER BY cc.last_message_at DESC LIMIT $1 OFFSET $2`
	rows, err := r.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list chat_conversations: %w", err)
	}
	defer rows.Close()
	var out []*ConversationListRow
	for rows.Next() {
		row := &ConversationListRow{}
		var lineOAID sql.NullString
		var lastInbound, lastAdminReply sql.NullTime
		if err := rows.Scan(
			&row.LineUserID, &lineOAID, &row.DisplayName, &row.PictureURL,
			&row.Phone, &row.Status,
			&row.LastMessageAt, &lastInbound, &lastAdminReply,
			&row.UnreadAdminCount, &row.CreatedAt,
			&row.LineOAName,
		); err != nil {
			return nil, err
		}
		if lineOAID.Valid {
			v := lineOAID.String
			row.LineOAID = &v
		}
		if lastInbound.Valid {
			t := lastInbound.Time
			row.LastInboundAt = &t
		}
		if lastAdminReply.Valid {
			t := lastAdminReply.Time
			row.LastAdminReplyAt = &t
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// SetPhone stores a phone number on a conversation (Phase 4.7).
func (r *ChatConversationRepo) SetPhone(lineUserID, phone string) error {
	_, err := r.db.Exec(
		`UPDATE chat_conversations SET phone = $1 WHERE line_user_id = $2`,
		phone, lineUserID,
	)
	if err != nil {
		return fmt.Errorf("set phone: %w", err)
	}
	return nil
}

// SetStatus changes a conversation's lifecycle state. Caller validates that
// the value is one of open/resolved/archived; the DB CHECK enforces it too.
func (r *ChatConversationRepo) SetStatus(lineUserID, status string) error {
	_, err := r.db.Exec(
		`UPDATE chat_conversations SET status = $1 WHERE line_user_id = $2`,
		status, lineUserID,
	)
	if err != nil {
		return fmt.Errorf("set status: %w", err)
	}
	return nil
}

// SetReplyToken stores the latest replyToken from an inbound webhook event.
// Per-conversation (not per-message) because admin always replies to the most
// recent inbound, and newer tokens always supersede older ones. Caller skips
// this for redelivered events (deliveryContext.isRedelivery) since redelivered
// tokens may already be invalid and would clobber the live one.
func (r *ChatConversationRepo) SetReplyToken(lineUserID, token string) error {
	_, err := r.db.Exec(
		`UPDATE chat_conversations
		   SET last_reply_token = $1, last_reply_token_at = NOW()
		 WHERE line_user_id = $2`,
		token, lineUserID,
	)
	if err != nil {
		return fmt.Errorf("set reply token: %w", err)
	}
	return nil
}

// ConsumeReplyToken atomically claims the stored replyToken (clearing it so
// no other admin can reuse it) and returns the OLD value. Returns "" when
// the column was already empty — caller falls back to Push API.
//
// Single-use semantics matter: LINE replyTokens are valid exactly once. If
// two admins hit SendReply simultaneously, the SELECT FOR UPDATE in the CTE
// serializes them — the second tx sees an already-empty column and gets "".
//
// Why CTE: Postgres RETURNING gives the POST-update row (always ''), so we
// SELECT the old value first, then UPDATE in the same statement.
func (r *ChatConversationRepo) ConsumeReplyToken(lineUserID string) (string, error) {
	var token string
	err := r.db.QueryRow(
		`WITH cur AS (
		   SELECT line_user_id, last_reply_token
		     FROM chat_conversations
		    WHERE line_user_id = $1 AND last_reply_token <> ''
		    FOR UPDATE
		 ), upd AS (
		   UPDATE chat_conversations
		      SET last_reply_token = '', last_reply_token_at = NULL
		    WHERE line_user_id = (SELECT line_user_id FROM cur)
		   RETURNING 1
		 )
		 SELECT last_reply_token FROM cur`,
		lineUserID,
	).Scan(&token)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("consume reply token: %w", err)
	}
	return token, nil
}

func (r *ChatConversationRepo) AutoReviveOnInbound(lineUserID string) (bool, error) {
	res, err := r.db.Exec(
		`UPDATE chat_conversations SET status = 'open'
		 WHERE line_user_id = $1 AND status = 'resolved'`,
		lineUserID,
	)
	if err != nil {
		return false, fmt.Errorf("auto-revive: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// TouchLastMessage updates last_message_at = NOW(), and (when isInbound) also
// last_inbound_at; (when !isInbound) last_admin_reply_at.
func (r *ChatConversationRepo) TouchLastMessage(lineUserID string, isInbound bool) error {
	var q string
	if isInbound {
		q = `UPDATE chat_conversations
		     SET last_message_at = NOW(), last_inbound_at = NOW()
		     WHERE line_user_id = $1`
	} else {
		q = `UPDATE chat_conversations
		     SET last_message_at = NOW(), last_admin_reply_at = NOW()
		     WHERE line_user_id = $1`
	}
	if _, err := r.db.Exec(q, lineUserID); err != nil {
		return fmt.Errorf("touch chat_conversation: %w", err)
	}
	return nil
}

// IncrementUnread bumps unread_admin_count by 1 (called on every incoming message).
func (r *ChatConversationRepo) IncrementUnread(lineUserID string) error {
	_, err := r.db.Exec(
		`UPDATE chat_conversations SET unread_admin_count = unread_admin_count + 1 WHERE line_user_id = $1`,
		lineUserID,
	)
	if err != nil {
		return fmt.Errorf("increment unread: %w", err)
	}
	return nil
}

// MarkRead zeroes unread_admin_count for one conversation.
func (r *ChatConversationRepo) MarkRead(lineUserID string) error {
	_, err := r.db.Exec(
		`UPDATE chat_conversations SET unread_admin_count = 0 WHERE line_user_id = $1`,
		lineUserID,
	)
	if err != nil {
		return fmt.Errorf("mark read: %w", err)
	}
	return nil
}

// UnreadCount returns the total unread (sum across all conversations) — used
// by the sidebar badge poll.
func (r *ChatConversationRepo) UnreadCount() (int, error) {
	var n int
	err := r.db.QueryRow(`SELECT COALESCE(SUM(unread_admin_count), 0) FROM chat_conversations`).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// CountAll returns total number of conversations matching the filter.
func (r *ChatConversationRepo) CountAll(f ConversationListFilter) (int, error) {
	args := []any{}
	where := ""
	add := func(cond string) {
		if where == "" {
			where = " WHERE " + cond
		} else {
			where += " AND " + cond
		}
	}
	if f.UnreadOnly {
		add("unread_admin_count > 0")
	}
	if f.Status != "" {
		args = append(args, f.Status)
		add(fmt.Sprintf("status = $%d", len(args)))
	}
	if f.Q != "" {
		args = append(args, "%"+f.Q+"%")
		add(fmt.Sprintf(`(display_name ILIKE $%d OR EXISTS (
		   SELECT 1 FROM chat_messages m
		   WHERE m.line_user_id = chat_conversations.line_user_id
		     AND m.text_content ILIKE $%d
		 ))`, len(args), len(args)))
	}
	if len(f.TagIDs) > 0 {
		ph := make([]string, 0, len(f.TagIDs))
		for _, id := range f.TagIDs {
			args = append(args, id)
			ph = append(ph, fmt.Sprintf("$%d", len(args)))
		}
		add(fmt.Sprintf(`EXISTS (
		   SELECT 1 FROM chat_conversation_tags ct
		   WHERE ct.line_user_id = chat_conversations.line_user_id
		     AND ct.tag_id IN (%s)
		 )`, strings.Join(ph, ",")))
	}
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM chat_conversations`+where, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}
