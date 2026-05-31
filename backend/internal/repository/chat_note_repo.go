package repository

import (
	"database/sql"
	"fmt"

	"nexflow/internal/models"
)

// ChatNoteRepo manages admin-only annotations on a conversation (Phase 4.8).
type ChatNoteRepo struct {
	db *sql.DB
}

func NewChatNoteRepo(db *sql.DB) *ChatNoteRepo {
	return &ChatNoteRepo{db: db}
}

const chatNoteCols = `id, line_user_id, body, created_by, created_at, updated_at`

func scanChatNote(s interface{ Scan(...any) error }) (*models.ChatNote, error) {
	n := &models.ChatNote{}
	var createdBy sql.NullString
	if err := s.Scan(&n.ID, &n.LineUserID, &n.Body, &createdBy, &n.CreatedAt, &n.UpdatedAt); err != nil {
		return nil, err
	}
	if createdBy.Valid {
		v := createdBy.String
		n.CreatedBy = &v
	}
	return n, nil
}

func (r *ChatNoteRepo) ListByUser(lineUserID string) ([]*models.ChatNote, error) {
	rows, err := r.db.Query(
		`SELECT `+chatNoteCols+` FROM chat_notes
		 WHERE line_user_id = $1 ORDER BY created_at DESC`,
		lineUserID,
	)
	if err != nil {
		return nil, fmt.Errorf("list chat_notes: %w", err)
	}
	defer rows.Close()
	var out []*models.ChatNote
	for rows.Next() {
		n, err := scanChatNote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (r *ChatNoteRepo) Create(n *models.ChatNote) error {
	row := r.db.QueryRow(
		`INSERT INTO chat_notes (line_user_id, body, created_by)
		 VALUES ($1, $2, $3) RETURNING id, created_at, updated_at`,
		n.LineUserID, n.Body, n.CreatedBy,
	)
	return row.Scan(&n.ID, &n.CreatedAt, &n.UpdatedAt)
}

func (r *ChatNoteRepo) Update(id, body string) (*models.ChatNote, error) {
	_, err := r.db.Exec(
		`UPDATE chat_notes SET body = $1, updated_at = NOW() WHERE id = $2`,
		body, id,
	)
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRow(`SELECT `+chatNoteCols+` FROM chat_notes WHERE id = $1`, id)
	return scanChatNote(row)
}

func (r *ChatNoteRepo) Delete(id string) error {
	_, err := r.db.Exec(`DELETE FROM chat_notes WHERE id = $1`, id)
	return err
}
