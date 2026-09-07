package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"nexflow/internal/models"
	"nexflow/internal/services/diagnostics"
)

var (
	ErrInvalidSMLExchange    = errors.New("invalid SML exchange evidence")
	ErrSMLAttemptNotFound    = errors.New("SML attempt not found")
	ErrSMLExchangeNotStarted = errors.New("SML exchange is already finalized or missing")
)

type SMLExchangeStart struct {
	AttemptID     string
	TraceID       string
	Route         string
	Method        string
	CanonicalPath string
	ContentType   string
	CorrelationID string
}

type SMLExchangeFinish struct {
	ExchangeID        string
	Status            string
	HTTPStatus        *int
	ResponseHeaders   map[string][]string
	ResponseBody      []byte
	ResponseHash      string
	ResponseSize      int64
	ResponseTruncated bool
	ErrorCode         string
	ErrorClass        string
	SafeErrorSummary  string
}

const smlExchangeSelectColumns = `id::text,sml_attempt_id::text,exchange_sequence,trace_id,route,status,
	request_method,request_path,request_content_type,correlation_id,started_at,finished_at,
	duration_ms,http_status,response_headers,response_json,response_hash,response_size,
	response_truncated,error_code,error_class,safe_error_summary,created_at`

func hasControlCharacters(value string) bool {
	return strings.IndexFunc(value, unicode.IsControl) >= 0
}

func boundedEvidenceText(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if len(value) > maximum {
		value = value[:maximum]
	}
	return value
}

func validExchangeStart(in SMLExchangeStart) bool {
	method := strings.ToUpper(strings.TrimSpace(in.Method))
	path := strings.TrimSpace(in.CanonicalPath)
	return strings.TrimSpace(in.AttemptID) != "" &&
		strings.TrimSpace(in.Route) != "" && len(in.Route) <= 64 &&
		method != "" && len(method) <= 16 && !hasControlCharacters(method) &&
		strings.HasPrefix(path, "/") && len(path) <= 512 &&
		!strings.Contains(path, "://") && !strings.ContainsAny(path, "?#") &&
		!hasControlCharacters(path) && len(in.TraceID) <= 128 &&
		len(in.CorrelationID) <= 128 && len(in.ContentType) <= 128 &&
		!hasControlCharacters(in.TraceID) && !hasControlCharacters(in.CorrelationID) &&
		!hasControlCharacters(in.ContentType)
}

func (r *BillRepo) BeginSMLAttemptExchange(ctx context.Context, in SMLExchangeStart) (*models.BillSMLAttemptExchange, error) {
	if r == nil || r.db == nil || !validExchangeStart(in) {
		return nil, ErrInvalidSMLExchange
	}
	in.Method = strings.ToUpper(strings.TrimSpace(in.Method))
	in.CanonicalPath = strings.TrimSpace(in.CanonicalPath)
	in.Route = strings.TrimSpace(in.Route)
	in.TraceID = strings.TrimSpace(in.TraceID)
	in.CorrelationID = strings.TrimSpace(in.CorrelationID)
	in.ContentType = strings.TrimSpace(in.ContentType)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var attemptID string
	if err := tx.QueryRowContext(ctx,
		"SELECT id::text FROM bill_sml_attempts WHERE id=$1 FOR UPDATE", in.AttemptID,
	).Scan(&attemptID); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSMLAttemptNotFound
	} else if err != nil {
		return nil, err
	}

	exchange, err := scanSMLAttemptExchange(tx.QueryRowContext(ctx, `INSERT INTO bill_sml_attempt_exchanges
		(sml_attempt_id,exchange_sequence,trace_id,route,status,request_method,request_path,
		 request_content_type,correlation_id)
		SELECT $1,COALESCE(MAX(exchange_sequence),0)+1,$2,$3,'started',$4,$5,$6,$7
		FROM bill_sml_attempt_exchanges WHERE sml_attempt_id=$1
		RETURNING `+smlExchangeSelectColumns,
		in.AttemptID, in.TraceID, in.Route, in.Method, in.CanonicalPath, in.ContentType, in.CorrelationID,
	))
	if err != nil {
		return nil, fmt.Errorf("begin SML exchange: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return exchange, nil
}

func (r *BillRepo) FinishSMLAttemptExchange(ctx context.Context, in SMLExchangeFinish) (*models.BillSMLAttemptExchange, error) {
	if r == nil || r.db == nil || strings.TrimSpace(in.ExchangeID) == "" ||
		(in.Status != "succeeded" && in.Status != "failed" && in.Status != "unknown") {
		return nil, ErrInvalidSMLExchange
	}
	if in.HTTPStatus != nil && (*in.HTTPStatus < 100 || *in.HTTPStatus > 599) {
		return nil, ErrInvalidSMLExchange
	}

	raw := in.ResponseBody
	if len(raw) > diagnostics.MaxResponseBytes {
		raw = raw[:diagnostics.MaxResponseBytes]
		in.ResponseTruncated = true
	}
	responseSize := in.ResponseSize
	if responseSize <= 0 {
		responseSize = int64(len(in.ResponseBody))
	}
	responseHash := strings.TrimSpace(in.ResponseHash)
	if responseHash == "" && len(in.ResponseBody) > 0 {
		sum := sha256.Sum256(in.ResponseBody)
		responseHash = hex.EncodeToString(sum[:])
	}
	var responseJSON json.RawMessage
	if len(raw) > 0 {
		sanitized := diagnostics.SanitizeJSON(raw)
		if sanitized.ValidJSON {
			responseJSON = json.RawMessage(sanitized.Body)
		}
		in.ResponseTruncated = in.ResponseTruncated || sanitized.Truncated
		if in.SafeErrorSummary == "" {
			in.SafeErrorSummary = sanitized.SafeSummary
		}
	}
	responseHeaders, err := json.Marshal(diagnostics.SanitizeHeaders(in.ResponseHeaders))
	if err != nil {
		return nil, err
	}
	in.ErrorCode = boundedEvidenceText(in.ErrorCode, 128)
	in.ErrorClass = boundedEvidenceText(in.ErrorClass, 128)
	in.SafeErrorSummary = boundedEvidenceText(in.SafeErrorSummary, 1000)

	exchange, err := scanSMLAttemptExchange(r.db.QueryRowContext(ctx, `UPDATE bill_sml_attempt_exchanges SET
		status=$2,finished_at=NOW(),
		duration_ms=GREATEST(0,ROUND(EXTRACT(EPOCH FROM (NOW()-started_at))*1000))::BIGINT,
		http_status=$3,response_headers=$4,response_json=$5,response_hash=$6,response_size=$7,
		response_truncated=$8,error_code=$9,error_class=$10,safe_error_summary=$11
		WHERE id=$1 AND status='started'
		RETURNING `+smlExchangeSelectColumns,
		in.ExchangeID, in.Status, in.HTTPStatus, responseHeaders, responseJSON, responseHash,
		responseSize, in.ResponseTruncated, in.ErrorCode, in.ErrorClass, in.SafeErrorSummary,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSMLExchangeNotStarted
	}
	if err != nil {
		return nil, err
	}
	return exchange, nil
}

func (r *BillRepo) ListSMLAttemptExchanges(ctx context.Context, attemptID string, limit int) ([]models.BillSMLAttemptExchange, error) {
	if r == nil || r.db == nil || strings.TrimSpace(attemptID) == "" {
		return nil, ErrInvalidSMLExchange
	}
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+smlExchangeSelectColumns+`
		FROM bill_sml_attempt_exchanges WHERE sml_attempt_id=$1
		ORDER BY exchange_sequence ASC LIMIT $2`, attemptID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]models.BillSMLAttemptExchange, 0)
	for rows.Next() {
		exchange, err := scanSMLAttemptExchange(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *exchange)
	}
	return result, rows.Err()
}

func scanSMLAttemptExchange(row smlAttemptScanner) (*models.BillSMLAttemptExchange, error) {
	var exchange models.BillSMLAttemptExchange
	var finishedAt sql.NullTime
	var durationMS, httpStatus sql.NullInt64
	var responseHeaders, responseJSON []byte
	if err := row.Scan(
		&exchange.ID, &exchange.SMLAttemptID, &exchange.Sequence, &exchange.TraceID,
		&exchange.Route, &exchange.Status, &exchange.RequestMethod, &exchange.RequestPath,
		&exchange.RequestContentType, &exchange.CorrelationID, &exchange.StartedAt, &finishedAt,
		&durationMS, &httpStatus, &responseHeaders, &responseJSON, &exchange.ResponseHash,
		&exchange.ResponseSize, &exchange.ResponseTruncated, &exchange.ErrorCode,
		&exchange.ErrorClass, &exchange.SafeErrorSummary, &exchange.CreatedAt,
	); err != nil {
		return nil, err
	}
	if finishedAt.Valid {
		exchange.FinishedAt = &finishedAt.Time
	}
	if durationMS.Valid {
		value := durationMS.Int64
		exchange.DurationMS = &value
	}
	if httpStatus.Valid {
		value := int(httpStatus.Int64)
		exchange.HTTPStatus = &value
	}
	if len(responseHeaders) > 0 {
		if err := json.Unmarshal(responseHeaders, &exchange.ResponseHeaders); err != nil {
			return nil, err
		}
	}
	if len(responseJSON) > 0 {
		exchange.ResponseJSON = json.RawMessage(responseJSON)
	}
	return &exchange, nil
}
