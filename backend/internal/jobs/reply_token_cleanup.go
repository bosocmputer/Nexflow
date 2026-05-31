package jobs

import (
	"database/sql"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

// ReplyTokenCleanup clears replyTokens older than 1 hour from
// chat_conversations. LINE doesn't publish exact validity (subject to
// change without notice) but tokens are definitely dead by 1h — keeping
// them just wastes a Reply API round-trip on the next admin reply
// (which then fails and falls back to Push).
//
// Why an explicit cleanup vs lazy: with lazy expiry the FIRST admin
// reply after any long pause always pays a wasted LINE round-trip.
// One UPDATE/hour costs nothing and keeps the hot path fast.
type ReplyTokenCleanup struct {
	db     *sql.DB
	logger *zap.Logger
}

func NewReplyTokenCleanup(db *sql.DB, logger *zap.Logger) *ReplyTokenCleanup {
	return &ReplyTokenCleanup{db: db, logger: logger}
}

// Register schedules the job hourly at minute 7 (off-the-hour to avoid
// piling on top of insight/backup crons). Single SQL UPDATE — finishes in
// milliseconds even on 100k conversations because last_reply_token_at is
// indexable (we don't index it; the table is small enough).
func (j *ReplyTokenCleanup) Register(c *cron.Cron) {
	_, err := c.AddFunc("7 * * * *", j.runOnce)
	if err != nil {
		j.logger.Error("register reply_token_cleanup", zap.Error(err))
	}
}

func (j *ReplyTokenCleanup) runOnce() {
	res, err := j.db.Exec(
		`UPDATE chat_conversations
		   SET last_reply_token = '', last_reply_token_at = NULL
		 WHERE last_reply_token <> ''
		   AND last_reply_token_at < NOW() - INTERVAL '1 hour'`,
	)
	if err != nil {
		j.logger.Warn("reply_token_cleanup", zap.Error(err))
		return
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		j.logger.Info("reply_token_cleanup", zap.Int64("cleared", n))
	}
}
