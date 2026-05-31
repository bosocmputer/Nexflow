package repository

import (
	"database/sql"
	"fmt"
	"time"

	"nexflow/internal/models"
)

// ChatMessageRepo manages chat_messages (one row per chat event).
//
// ListByUser optionally hydrates the .Media field for image/file/audio rows
// via a single LEFT JOIN — saves a per-row roundtrip in the inbox view.
type ChatMessageRepo struct {
	db *sql.DB
}

func NewChatMessageRepo(db *sql.DB) *ChatMessageRepo {
	return &ChatMessageRepo{db: db}
}

const chatMsgCols = `
  id, line_user_id, direction, kind, text_content,
  line_message_id, line_event_ts, sender_admin_id,
  delivery_status, delivery_method, delivery_error, created_at
`

func scanChatMessage(s interface{ Scan(...any) error }) (*models.ChatMessage, error) {
	m := &models.ChatMessage{}
	var lineEventTS sql.NullInt64
	var senderAdmin sql.NullString
	if err := s.Scan(
		&m.ID, &m.LineUserID, &m.Direction, &m.Kind, &m.TextContent,
		&m.LineMessageID, &lineEventTS, &senderAdmin,
		&m.DeliveryStatus, &m.DeliveryMethod, &m.DeliveryError, &m.CreatedAt,
	); err != nil {
		return nil, err
	}
	if lineEventTS.Valid {
		v := lineEventTS.Int64
		m.LineEventTS = &v
	}
	if senderAdmin.Valid {
		v := senderAdmin.String
		m.SenderAdminID = &v
	}
	return m, nil
}

// Insert writes a new chat_message and returns the row populated with the
// server-generated ID + created_at.
func (r *ChatMessageRepo) Insert(m *models.ChatMessage) error {
	if m.DeliveryStatus == "" {
		m.DeliveryStatus = models.ChatDeliverySent
	}
	if m.DeliveryMethod == "" {
		m.DeliveryMethod = models.ChatDeliveryMethodPush
	}
	row := r.db.QueryRow(
		`INSERT INTO chat_messages (
		   line_user_id, direction, kind, text_content,
		   line_message_id, line_event_ts, sender_admin_id,
		   delivery_status, delivery_method, delivery_error
		 ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		 RETURNING id, created_at`,
		m.LineUserID, m.Direction, m.Kind, m.TextContent,
		m.LineMessageID, m.LineEventTS, m.SenderAdminID,
		m.DeliveryStatus, m.DeliveryMethod, m.DeliveryError,
	)
	if err := row.Scan(&m.ID, &m.CreatedAt); err != nil {
		return fmt.Errorf("insert chat_message: %w", err)
	}
	return nil
}

// ListByUser returns messages in a conversation, oldest-first (so UI scrolls
// down to the newest at the bottom). When `since` is non-nil, only messages
// strictly after that timestamp are returned (used for delta polling).
// When q is non-empty, filters to messages whose text_content matches ILIKE.
//
// Always hydrates .Media for image/file/audio rows.
func (r *ChatMessageRepo) ListByUser(lineUserID string, since *time.Time, limit int, q string) ([]*models.ChatMessage, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	var rows *sql.Rows
	var err error

	// Phase D thread-search: when q is non-empty, filter by text_content
	// ILIKE. Skip the since-delta path (search returns full match list).
	if q != "" {
		query := `SELECT
		            m.id, m.line_user_id, m.direction, m.kind, m.text_content,
		            m.line_message_id, m.line_event_ts, m.sender_admin_id,
		            m.delivery_status, m.delivery_method, m.delivery_error, m.created_at,
		            media.id, media.filename, media.content_type, media.size_bytes,
		            media.sha256, media.storage_path, media.created_at
		          FROM chat_messages m
		          LEFT JOIN chat_media media ON media.message_id = m.id
		          WHERE m.line_user_id = $1 AND m.text_content ILIKE $2
		          ORDER BY m.created_at ASC LIMIT $3`
		rows, err = r.db.Query(query, lineUserID, "%"+q+"%", limit)
	} else if since != nil {
		query := `SELECT
		            m.id, m.line_user_id, m.direction, m.kind, m.text_content,
		            m.line_message_id, m.line_event_ts, m.sender_admin_id,
		            m.delivery_status, m.delivery_method, m.delivery_error, m.created_at,
		            media.id, media.filename, media.content_type, media.size_bytes,
		            media.sha256, media.storage_path, media.created_at
		          FROM chat_messages m
		          LEFT JOIN chat_media media ON media.message_id = m.id
		          WHERE m.line_user_id = $1
		            AND m.created_at > $2
		          ORDER BY m.created_at ASC LIMIT $3`
		rows, err = r.db.Query(query, lineUserID, *since, limit)
	} else {
		// No since → show the most recent N messages, oldest-first.
		// Subquery to grab the latest N then re-order ascending.
		query := `SELECT * FROM (
		            SELECT
		              m.id, m.line_user_id, m.direction, m.kind, m.text_content,
		              m.line_message_id, m.line_event_ts, m.sender_admin_id,
		              m.delivery_status, m.delivery_method, m.delivery_error, m.created_at,
		              media.id AS media_id, media.filename, media.content_type, media.size_bytes,
		              media.sha256, media.storage_path, media.created_at AS media_created_at
		            FROM chat_messages m
		            LEFT JOIN chat_media media ON media.message_id = m.id
		            WHERE m.line_user_id = $1
		            ORDER BY m.created_at DESC
		            LIMIT $2
		          ) sub ORDER BY created_at ASC`
		rows, err = r.db.Query(query, lineUserID, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list chat_messages: %w", err)
	}
	defer rows.Close()

	var out []*models.ChatMessage
	for rows.Next() {
		m := &models.ChatMessage{}
		var lineEventTS sql.NullInt64
		var senderAdmin sql.NullString
		var mediaID, mediaFilename, mediaCT, mediaSHA, mediaPath sql.NullString
		var mediaSize sql.NullInt64
		var mediaCreated sql.NullTime
		if err := rows.Scan(
			&m.ID, &m.LineUserID, &m.Direction, &m.Kind, &m.TextContent,
			&m.LineMessageID, &lineEventTS, &senderAdmin,
			&m.DeliveryStatus, &m.DeliveryMethod, &m.DeliveryError, &m.CreatedAt,
			&mediaID, &mediaFilename, &mediaCT, &mediaSize,
			&mediaSHA, &mediaPath, &mediaCreated,
		); err != nil {
			return nil, err
		}
		if lineEventTS.Valid {
			v := lineEventTS.Int64
			m.LineEventTS = &v
		}
		if senderAdmin.Valid {
			v := senderAdmin.String
			m.SenderAdminID = &v
		}
		if mediaID.Valid {
			m.Media = &models.ChatMedia{
				ID:          mediaID.String,
				MessageID:   m.ID,
				Filename:    mediaFilename.String,
				ContentType: mediaCT.String,
				SizeBytes:   mediaSize.Int64,
				SHA256:      mediaSHA.String,
				StoragePath: mediaPath.String,
				CreatedAt:   mediaCreated.Time,
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Get returns a single message by ID (without media — caller can fetch media
// separately via ChatMediaRepo.GetByMessageID).
func (r *ChatMessageRepo) Get(messageID string) (*models.ChatMessage, error) {
	row := r.db.QueryRow(
		`SELECT `+chatMsgCols+` FROM chat_messages WHERE id = $1`,
		messageID,
	)
	m, err := scanChatMessage(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get chat_message: %w", err)
	}
	return m, nil
}

// UpdateDeliveryStatus is used after we attempt the LINE send to record
// success or failure. errMsg is "" on success. method is "reply" or "push"
// — whichever transport actually delivered (or attempted on failure).
func (r *ChatMessageRepo) UpdateDeliveryStatus(messageID, status, method, errMsg string) error {
	_, err := r.db.Exec(
		`UPDATE chat_messages
		   SET delivery_status = $1, delivery_method = $2, delivery_error = $3
		 WHERE id = $4`,
		status, method, errMsg, messageID,
	)
	if err != nil {
		return fmt.Errorf("update delivery status: %w", err)
	}
	return nil
}
