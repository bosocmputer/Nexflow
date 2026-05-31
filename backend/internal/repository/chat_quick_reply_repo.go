package repository

import (
	"database/sql"
	"fmt"

	"nexflow/internal/models"
)

type ChatQuickReplyRepo struct {
	db *sql.DB
}

func NewChatQuickReplyRepo(db *sql.DB) *ChatQuickReplyRepo {
	return &ChatQuickReplyRepo{db: db}
}

const quickReplyCols = `id, label, body, sort_order, created_by, created_at, updated_at`

func scanQuickReply(s interface{ Scan(...any) error }) (*models.ChatQuickReply, error) {
	q := &models.ChatQuickReply{}
	var createdBy sql.NullString
	if err := s.Scan(&q.ID, &q.Label, &q.Body, &q.SortOrder, &createdBy, &q.CreatedAt, &q.UpdatedAt); err != nil {
		return nil, err
	}
	if createdBy.Valid {
		v := createdBy.String
		q.CreatedBy = &v
	}
	return q, nil
}

// List returns all quick replies ordered by sort_order, label.
func (r *ChatQuickReplyRepo) List() ([]*models.ChatQuickReply, error) {
	rows, err := r.db.Query(
		`SELECT ` + quickReplyCols + ` FROM chat_quick_replies ORDER BY sort_order, label`,
	)
	if err != nil {
		return nil, fmt.Errorf("list chat_quick_replies: %w", err)
	}
	defer rows.Close()
	var out []*models.ChatQuickReply
	for rows.Next() {
		q, err := scanQuickReply(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (r *ChatQuickReplyRepo) Create(q *models.ChatQuickReply) error {
	row := r.db.QueryRow(
		`INSERT INTO chat_quick_replies (label, body, sort_order, created_by)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, created_at, updated_at`,
		q.Label, q.Body, q.SortOrder, q.CreatedBy,
	)
	return row.Scan(&q.ID, &q.CreatedAt, &q.UpdatedAt)
}

func (r *ChatQuickReplyRepo) Update(id string, in models.ChatQuickReplyUpsert) (*models.ChatQuickReply, error) {
	_, err := r.db.Exec(
		`UPDATE chat_quick_replies
		 SET label = $1, body = $2, sort_order = $3, updated_at = NOW()
		 WHERE id = $4`,
		in.Label, in.Body, in.SortOrder, id,
	)
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRow(`SELECT `+quickReplyCols+` FROM chat_quick_replies WHERE id = $1`, id)
	return scanQuickReply(row)
}

func (r *ChatQuickReplyRepo) Delete(id string) error {
	_, err := r.db.Exec(`DELETE FROM chat_quick_replies WHERE id = $1`, id)
	return err
}
