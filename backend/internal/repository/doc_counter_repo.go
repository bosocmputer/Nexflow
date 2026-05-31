package repository

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// DocCounterRepo serves atomic counters per (prefix, period) for SML doc_no
// generation. Each call to NextSeq increments and returns the value to use,
// so two retries racing the same channel don't collide on doc_no.
type DocCounterRepo struct {
	db *sql.DB
}

func NewDocCounterRepo(db *sql.DB) *DocCounterRepo {
	return &DocCounterRepo{db: db}
}

// NextSeq atomically increments the counter for (prefix, period) and returns
// the seq value to use right now. First call for a (prefix, period) returns 1.
func (r *DocCounterRepo) NextSeq(prefix, period string) (int, error) {
	var seq int
	err := r.db.QueryRow(
		`INSERT INTO doc_counters (prefix, period, last_used_seq, updated_at)
		 VALUES ($1, $2, 1, NOW())
		 ON CONFLICT (prefix, period) DO UPDATE SET
		   last_used_seq = doc_counters.last_used_seq + 1,
		   updated_at    = NOW()
		 RETURNING last_used_seq`,
		prefix, period,
	).Scan(&seq)
	if err != nil {
		return 0, fmt.Errorf("doc counter increment: %w", err)
	}
	return seq, nil
}

// NextSeqAtLeast atomically reserves a sequence that is at least minSeq.
// If another Nexflow worker already reserved minSeq, this returns the next
// local sequence instead. This lets us sync from SML's authoritative latest
// running while still preventing concurrent Nexflow sends from colliding.
func (r *DocCounterRepo) NextSeqAtLeast(prefix, period string, minSeq int) (int, error) {
	if minSeq < 1 {
		minSeq = 1
	}
	var seq int
	err := r.db.QueryRow(
		`INSERT INTO doc_counters (prefix, period, last_used_seq, updated_at)
		 VALUES ($1, $2, $3, NOW())
		 ON CONFLICT (prefix, period) DO UPDATE SET
		   last_used_seq = GREATEST(doc_counters.last_used_seq + 1, EXCLUDED.last_used_seq),
		   updated_at    = NOW()
		 RETURNING last_used_seq`,
		prefix, period, minSeq,
	).Scan(&seq)
	if err != nil {
		return 0, fmt.Errorf("doc counter sync increment: %w", err)
	}
	return seq, nil
}

// GenerateDocNo renders prefix + format with date tokens substituted and the
// sequence counter atomically incremented.
//
// Format tokens (case-sensitive):
//
//	YYYY  → 4-digit year       (e.g. 2026)
//	YY    → 2-digit year       (26)
//	MM    → 2-digit month      (04)
//	DD    → 2-digit day        (28)
//	#...  → zero-padded counter; the count of #s is the padding width
//	        (so "####" = 4 digits, "#####" = 5 digits)
//
// Period for counter reset is derived from which date tokens the format uses:
//
//	contains DD → resets daily        (period = YYYYMMDD)
//	contains MM → resets monthly      (period = YYYYMM, default for YYMM####)
//	contains YY → resets yearly       (period = YYYY)
//	none of the above → never resets  (period = "_")
//
// Both prefix and format default to safe values when blank: "BF" and "YYMM####".
func (r *DocCounterRepo) GenerateDocNo(prefix, format string, now time.Time) (string, error) {
	return r.renderDocNo(prefix, format, now, true, 0)
}

func (r *DocCounterRepo) GenerateDocNoAtLeast(prefix, format string, now time.Time, minSeq int) (string, error) {
	if prefix == "" {
		prefix = "BF"
	}
	if format == "" {
		format = "YYMM####"
	}
	period := docCounterPeriod(format, now)
	seq, err := r.NextSeqAtLeast(prefix, period, minSeq)
	if err != nil {
		return "", err
	}
	return renderDocNoFromSeq(prefix, format, now, seq), nil
}

// RenderDocNoFromSeq renders a doc_no for a known sequence without reading or
// mutating doc_counters. Use this for SML-authoritative preview only; sending
// still must reserve through GenerateDocNo/GenerateDocNoAtLeast.
func RenderDocNoFromSeq(prefix, format string, now time.Time, seq int) string {
	return renderDocNoFromSeq(prefix, format, now, seq)
}

// PeekDocNo renders the next doc_no without incrementing/reserving the
// sequence. It is for UI preview only; GenerateDocNo remains the source of
// truth when sending.
func (r *DocCounterRepo) PeekDocNo(prefix, format string, now time.Time) (string, error) {
	return r.PeekDocNoWithOffset(prefix, format, now, 0)
}

func (r *DocCounterRepo) PeekDocNoWithOffset(prefix, format string, now time.Time, offset int) (string, error) {
	if offset < 0 {
		offset = 0
	}
	return r.renderDocNo(prefix, format, now, false, offset)
}

func (r *DocCounterRepo) renderDocNo(prefix, format string, now time.Time, increment bool, offset int) (string, error) {
	if prefix == "" {
		prefix = "BF"
	}
	if format == "" {
		format = "YYMM####"
	}

	period := docCounterPeriod(format, now)

	seq := 1
	if increment {
		var err error
		seq, err = r.NextSeq(prefix, period)
		if err != nil {
			return "", err
		}
	} else {
		var lastUsed int
		err := r.db.QueryRow(
			`SELECT last_used_seq FROM doc_counters WHERE prefix = $1 AND period = $2`,
			prefix, period,
		).Scan(&lastUsed)
		if err != nil && err != sql.ErrNoRows {
			return "", fmt.Errorf("doc counter peek: %w", err)
		}
		seq = lastUsed + 1 + offset
	}

	return renderDocNoFromSeq(prefix, format, now, seq), nil
}

func docCounterPeriod(format string, now time.Time) string {
	yyyy := fmt.Sprintf("%04d", now.Year())
	mm := fmt.Sprintf("%02d", int(now.Month()))
	dd := fmt.Sprintf("%02d", now.Day())
	switch {
	case strings.Contains(format, "DD"):
		return yyyy + mm + dd
	case strings.Contains(format, "MM"):
		return yyyy + mm
	case strings.Contains(format, "YYYY") || strings.Contains(format, "YY"):
		return yyyy
	default:
		return "_"
	}
}

func renderDocNoFromSeq(prefix, format string, now time.Time, seq int) string {
	yyyy := fmt.Sprintf("%04d", now.Year())
	yy := fmt.Sprintf("%02d", now.Year()%100)
	mm := fmt.Sprintf("%02d", int(now.Month()))
	dd := fmt.Sprintf("%02d", now.Day())
	width := 4
	if hashRe.MatchString(format) {
		width = len(hashRe.FindString(format))
	}
	out := format
	out = strings.ReplaceAll(out, "YYYY", yyyy)
	out = strings.ReplaceAll(out, "YY", yy)
	out = strings.ReplaceAll(out, "MM", mm)
	out = strings.ReplaceAll(out, "DD", dd)
	out = hashRe.ReplaceAllString(out, fmt.Sprintf("%0*d", width, seq))
	return prefix + out
}

var hashRe = regexp.MustCompile(`#+`)
