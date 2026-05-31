package repository

import (
	"database/sql"
	"fmt"

	"nexflow/internal/models"
)

// ChatTagRepo manages global tags + many-to-many to conversations (Phase 4.9).
type ChatTagRepo struct {
	db *sql.DB
}

func NewChatTagRepo(db *sql.DB) *ChatTagRepo {
	return &ChatTagRepo{db: db}
}

const chatTagCols = `id, label, color, created_at`

func scanChatTag(s interface{ Scan(...any) error }) (*models.ChatTag, error) {
	t := &models.ChatTag{}
	if err := s.Scan(&t.ID, &t.Label, &t.Color, &t.CreatedAt); err != nil {
		return nil, err
	}
	return t, nil
}

// ── Global tag list ──────────────────────────────────────────────────────────

func (r *ChatTagRepo) ListAll() ([]*models.ChatTag, error) {
	rows, err := r.db.Query(`SELECT ` + chatTagCols + ` FROM chat_tags ORDER BY label`)
	if err != nil {
		return nil, fmt.Errorf("list chat_tags: %w", err)
	}
	defer rows.Close()
	var out []*models.ChatTag
	for rows.Next() {
		t, err := scanChatTag(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *ChatTagRepo) Create(t *models.ChatTag) error {
	if t.Color == "" {
		t.Color = "gray"
	}
	row := r.db.QueryRow(
		`INSERT INTO chat_tags (label, color) VALUES ($1, $2) RETURNING id, created_at`,
		t.Label, t.Color,
	)
	return row.Scan(&t.ID, &t.CreatedAt)
}

func (r *ChatTagRepo) Update(id string, in models.ChatTagUpsert) (*models.ChatTag, error) {
	color := in.Color
	if color == "" {
		color = "gray"
	}
	_, err := r.db.Exec(
		`UPDATE chat_tags SET label = $1, color = $2 WHERE id = $3`,
		in.Label, color, id,
	)
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRow(`SELECT `+chatTagCols+` FROM chat_tags WHERE id = $1`, id)
	return scanChatTag(row)
}

func (r *ChatTagRepo) Delete(id string) error {
	_, err := r.db.Exec(`DELETE FROM chat_tags WHERE id = $1`, id)
	return err
}

// ── Conversation ↔ tag m2m ───────────────────────────────────────────────────

// TagsForConversation returns all tags currently attached to a conversation.
func (r *ChatTagRepo) TagsForConversation(lineUserID string) ([]*models.ChatTag, error) {
	rows, err := r.db.Query(
		`SELECT t.id, t.label, t.color, t.created_at
		 FROM chat_tags t
		 JOIN chat_conversation_tags m ON m.tag_id = t.id
		 WHERE m.line_user_id = $1
		 ORDER BY t.label`,
		lineUserID,
	)
	if err != nil {
		return nil, fmt.Errorf("tags for conv: %w", err)
	}
	defer rows.Close()
	var out []*models.ChatTag
	for rows.Next() {
		t, err := scanChatTag(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SetTagsForConversation replaces the tag set for a conversation atomically.
func (r *ChatTagRepo) SetTagsForConversation(lineUserID string, tagIDs []string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM chat_conversation_tags WHERE line_user_id = $1`, lineUserID); err != nil {
		return err
	}
	for _, id := range tagIDs {
		if _, err := tx.Exec(
			`INSERT INTO chat_conversation_tags (line_user_id, tag_id) VALUES ($1, $2)
			 ON CONFLICT DO NOTHING`,
			lineUserID, id,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}
