package emailservice

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"nexflow/internal/models"

	gomail "github.com/emersion/go-message/mail"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-sasl"
	"go.uber.org/zap"
)

// MailSource identifies which configured inbox produced a message.
type MailSource struct {
	AccountID   string `json:"imap_account_id,omitempty"`
	AccountName string `json:"imap_account_name,omitempty"`
	Username    string `json:"imap_username,omitempty"`
	Mailbox     string `json:"imap_mailbox,omitempty"`
	EmailDate   string `json:"email_date,omitempty"`
}

// AttachmentProcessor is called once per qualifying attachment found in email.
type AttachmentProcessor func(data []byte, mimeType, filename, messageID, subject, fromAddr string, source MailSource) error

// ShopeeBodyProcessor is called when an email from a Shopee domain is detected.
// bodyText is the plain-text part (sent to AI); bodyHTML is the original HTML
// (stored in raw_data for display in BillDetail).
type ShopeeBodyProcessor func(subject, from, bodyText, bodyHTML, messageID string, source MailSource) error

// PollConfig holds everything one IMAP poll cycle needs.
// Re-built per cycle from the account's current DB state, so admin edits
// take effect on the next poll without restarting the goroutine.
type PollConfig struct {
	AccountID      string
	AccountName    string
	Host           string
	Port           int
	Username       string
	Password       string
	Mailbox        string // default "INBOX" if empty
	FilterFrom     string
	FilterSubjects []string // lower-cased keywords; empty = match all
	LookbackDays   int      // ≥1
	LastSeenUID    int64
	Channel        string   // "general" | "shopee" | "lazada"
	ShopeeDomains  []string // accepted senders for channel="shopee" (legacy DB name)
	Progress       func(PollResult)
}

// PollResult summarises one poll cycle. Either Err is non-nil or the counts
// describe what happened.
type PollResult struct {
	TraceID         string
	MessagesFound   int
	Processed       int
	Skipped         int
	Summary         models.IMAPPollSummary
	Details         []models.IMAPPollDetail
	LastSeenUID     int64
	Limited         bool
	Backlog         int
	ProcessWarnings []string
	Duration        time.Duration
	Err             error
	FailureStage    string // "connect" | "authenticate" | "select" | "search" | "" if ok
}

type imapMessageSummary struct {
	UID      imap.UID
	Envelope *imap.Envelope
	FromAddr string
}

type imapUIDCandidates struct {
	Selected []imap.UID
	Total    int
	Backlog  int
	Limited  bool
}

const (
	imapFetchBatchSize       = 25
	defaultMaxMessagesPerRun = 150
	minMaxMessagesPerRun     = 25
	maxMaxMessagesPerRun     = 500
)

// Status returns a short tag suitable for `imap_accounts.last_poll_status`.
func (r *PollResult) Status() string {
	if r.Err != nil && errors.Is(r.Err, context.Canceled) {
		if r.LastSeenUID > 0 && (r.Processed > 0 || r.Skipped > 0) {
			return "partial"
		}
		return "interrupted"
	}
	if r.Err == nil {
		if (r.Limited || r.Backlog > 0) && r.Summary.Created > 0 && r.Summary.Failed == 0 {
			return "backlog"
		}
		if len(r.ProcessWarnings) > 0 {
			return "warning"
		}
		if r.Limited || r.Backlog > 0 {
			return "backlog"
		}
		if r.Processed == 0 {
			return "no_new_mail"
		}
		return "ok"
	}
	switch r.FailureStage {
	case "":
		if r.LastSeenUID > 0 && (r.Processed > 0 || r.Skipped > 0) {
			return "partial"
		}
		return "error"
	default:
		if r.LastSeenUID > 0 && (r.Processed > 0 || r.Skipped > 0) {
			return "partial"
		}
		return r.FailureStage + "_failed"
	}
}

// PollOnce runs one search-and-process cycle against the supplied account.
// It does not own any goroutine — the caller (AccountPoller) loops on a ticker.
func PollOnce(ctx context.Context, cfg PollConfig, p *Processors, logger *zap.Logger) PollResult {
	res := PollResult{TraceID: imapNewTraceID()}
	pollStart := time.Now()
	defer func() { res.Duration = time.Since(pollStart) }()

	mailbox := cfg.Mailbox
	if mailbox == "" {
		mailbox = "INBOX"
	}
	source := MailSource{
		AccountID:   cfg.AccountID,
		AccountName: cfg.AccountName,
		Username:    cfg.Username,
		Mailbox:     mailbox,
	}
	lookback := cfg.LookbackDays
	if lookback <= 0 {
		lookback = 30
	}

	logger.Info("imap_poll_start",
		zap.String("trace_id", res.TraceID),
		zap.String("account_id", cfg.AccountID),
		zap.String("account_name", cfg.AccountName),
		zap.String("host", cfg.Host),
		zap.String("user", cfg.Username),
		zap.String("mailbox", mailbox),
	)

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	c, err := imapclient.DialTLS(addr, nil)
	if err != nil {
		res.Err = fmt.Errorf("IMAP connect %s: %w", addr, err)
		res.FailureStage = "connect"
		logger.Error("imap_poll_failed", zap.String("trace_id", res.TraceID), zap.String("stage", "connect"), zap.Error(err))
		return res
	}
	defer c.Close()

	if err := c.Authenticate(sasl.NewPlainClient("", cfg.Username, cfg.Password)); err != nil {
		res.Err = fmt.Errorf("IMAP authenticate: %w", err)
		res.FailureStage = "auth"
		logger.Error("imap_poll_failed", zap.String("trace_id", res.TraceID), zap.String("stage", "authenticate"), zap.Error(err))
		return res
	}

	if _, err := c.Select(mailbox, nil).Wait(); err != nil {
		res.Err = fmt.Errorf("IMAP select %s: %w", mailbox, err)
		res.FailureStage = "select"
		logger.Error("imap_poll_failed", zap.String("trace_id", res.TraceID), zap.String("stage", "select"), zap.Error(err))
		return res
	}

	since := time.Now().AddDate(0, 0, -lookback)
	// Search both read and unread messages. Admins often open/forward Gmail
	// messages before Nexflow polls them; duplicate guards in the processors
	// prevent re-created bills when already-processed read messages are seen
	// again within the lookback window.
	criteria := &imap.SearchCriteria{Since: since}
	if cfg.FilterFrom != "" {
		criteria.Header = []imap.SearchCriteriaHeaderField{{Key: "From", Value: cfg.FilterFrom}}
	}
	if len(cfg.FilterSubjects) > 0 {
		subjectCriteria := subjectSearchCriteria(cfg.FilterSubjects)
		criteria.And(&subjectCriteria)
	}

	searchData, err := c.UIDSearch(criteria, nil).Wait()
	if err != nil {
		res.Err = fmt.Errorf("IMAP search: %w", err)
		res.FailureStage = "search"
		logger.Error("imap_poll_failed", zap.String("trace_id", res.TraceID), zap.String("stage", "search"), zap.Error(err))
		return res
	}

	uidCandidates := candidateUIDs(searchData.AllUIDs(), cfg.LastSeenUID, configuredMaxMessagesPerRun())
	uids := uidCandidates.Selected
	res.MessagesFound = uidCandidates.Total
	res.Limited = uidCandidates.Limited
	res.Backlog = uidCandidates.Backlog
	res.LastSeenUID = cfg.LastSeenUID
	emitPollProgress(cfg, res)
	if len(uids) == 0 {
		logger.Info("imap_poll_done",
			zap.String("trace_id", res.TraceID),
			zap.Int("messages_found", 0),
			zap.Int("scanned", res.Summary.Scanned),
			zap.Int64("last_seen_uid", res.LastSeenUID),
		)
		return res
	}

	logger.Info("imap_messages_found",
		zap.String("trace_id", res.TraceID),
		zap.Int("remaining", res.MessagesFound),
		zap.Int("selected", len(uids)),
		zap.Int("backlog", res.Backlog),
		zap.Int64("last_seen_uid", res.LastSeenUID),
		zap.Bool("limited", res.Limited),
	)

	var processedUIDs imap.UIDSet

	for start := 0; start < len(uids); start += imapFetchBatchSize {
		select {
		case <-ctx.Done():
			res.Err = ctx.Err()
			res.Summary.Interrupted = true
			res.Backlog = countUIDsAfter(uids, res.LastSeenUID) + uidCandidates.Backlog
			res.Limited = res.Backlog > 0
			emitPollProgress(cfg, res)
			return res
		default:
		}

		end := start + imapFetchBatchSize
		if end > len(uids) {
			end = len(uids)
		}

		summaries, err := fetchEnvelopeBatch(c, uids[start:end])
		if err != nil {
			res.Err = fmt.Errorf("IMAP fetch envelope: %w", err)
			res.FailureStage = "fetch"
			res.Backlog = countUIDsAfter(uids, res.LastSeenUID) + uidCandidates.Backlog
			res.Limited = res.Backlog > 0
			emitPollProgress(cfg, res)
			return res
		}
		duplicateMessages := map[string]bool{}
		if p != nil && p.DuplicateMessages != nil {
			messageIDs := messageIDsFromSummaries(summaries)
			if len(messageIDs) > 0 {
				duplicates, err := p.DuplicateMessages(messageIDs)
				if err != nil {
					warning := fmt.Sprintf("check duplicate messages: %v", err)
					res.ProcessWarnings = append(res.ProcessWarnings, warning)
					logger.Warn("imap_duplicate_batch_precheck_failed",
						zap.String("trace_id", res.TraceID),
						zap.Int("message_count", len(messageIDs)),
						zap.Error(err),
					)
				} else {
					duplicateMessages = duplicates
					if len(duplicates) > 0 {
						logger.Info("imap_duplicate_batch_prechecked",
							zap.String("trace_id", res.TraceID),
							zap.Int("message_count", len(messageIDs)),
							zap.Int("duplicate_count", len(duplicates)),
						)
					}
				}
			}
		}

		var batchProcessedUIDs imap.UIDSet
		batchProcessed := 0
		for _, summary := range summaries {
			res.Summary.Scanned++
			if int64(summary.UID) > res.LastSeenUID {
				res.LastSeenUID = int64(summary.UID)
			}
			envelope := summary.Envelope
			if envelope == nil {
				res.ProcessWarnings = append(res.ProcessWarnings, "อ่านข้อมูลหัวอีเมลไม่ได้")
				res.Summary.Failed++
				res.addDetail(summary, "skipped", "missing_envelope", "อ่านข้อมูลหัวอีเมลไม่ได้")
				res.Skipped++
				continue
			}

			if !matchesSubject(envelope.Subject, cfg.FilterSubjects) {
				logger.Info("imap_message_skipped",
					zap.String("trace_id", res.TraceID),
					zap.String("subject", envelope.Subject),
					zap.String("reason", "subject_filter_mismatch"),
				)
				res.Summary.SkippedUser++
				res.addDetail(summary, "skipped", "subject_filter_mismatch", "หัวข้ออีเมลไม่ตรงคำกรอง")
				res.Skipped++
				continue
			}

			if cfg.Channel == "shopee" && !isShopeeFrom(summary.FromAddr, cfg.ShopeeDomains) {
				logger.Info("imap_message_skipped",
					zap.String("trace_id", res.TraceID),
					zap.String("from", summary.FromAddr),
					zap.String("reason", "shopee_channel_non_shopee_from"),
				)
				res.Summary.SkippedUser++
				res.addDetail(summary, "skipped", "sender_not_allowed", "ผู้ส่งไม่อยู่ในรายชื่อที่ยอมรับ")
				res.Skipped++
				continue
			}

			messageID := envelope.MessageID
			if strings.TrimSpace(messageID) != "" && duplicateMessages[messageID] {
				logger.Debug("imap_message_duplicate_prechecked",
					zap.String("trace_id", res.TraceID),
					zap.String("message_id", messageID),
					zap.String("subject", envelope.Subject),
				)
				res.Summary.AlreadyProcessed++
				res.Summary.SkippedUser++
				res.addDetail(summary, "skipped", "duplicate", "เมลนี้เคยประมวลผลหรือเคยสร้างบิลแล้ว")
				res.Skipped++
				continue
			}
			if p != nil && p.DuplicateMessages == nil && p.DuplicateMessage != nil && strings.TrimSpace(messageID) != "" {
				duplicate, err := p.DuplicateMessage(messageID)
				if err != nil {
					warning := fmt.Sprintf("check duplicate message %s: %v", messageID, err)
					res.ProcessWarnings = append(res.ProcessWarnings, warning)
					logger.Warn("imap_duplicate_precheck_failed",
						zap.String("trace_id", res.TraceID),
						zap.String("message_id", messageID),
						zap.Error(err),
					)
				} else if duplicate {
					logger.Debug("imap_message_duplicate_prechecked",
						zap.String("trace_id", res.TraceID),
						zap.String("message_id", messageID),
						zap.String("subject", envelope.Subject),
					)
					res.Summary.AlreadyProcessed++
					res.Summary.SkippedUser++
					res.addDetail(summary, "skipped", "duplicate", "เมลนี้เคยประมวลผลหรือเคยสร้างบิลแล้ว")
					res.Skipped++
					continue
				}
			}

			bodyBytes, err := fetchBodyForUID(c, summary.UID)
			if err != nil {
				warning := fmt.Sprintf("fetch body uid %d: %v", summary.UID, err)
				logger.Warn("imap_message_fetch_body_failed",
					zap.String("trace_id", res.TraceID),
					zap.Uint32("uid", uint32(summary.UID)),
					zap.Error(err),
				)
				res.ProcessWarnings = append(res.ProcessWarnings, warning)
				res.Summary.Failed++
				res.addDetail(summary, "skipped", "fetch_body_failed", "ดึงเนื้อหาอีเมลไม่สำเร็จ")
				res.Skipped++
				continue
			}
			if len(bodyBytes) == 0 {
				res.ProcessWarnings = append(res.ProcessWarnings, "อีเมลไม่มีเนื้อหาที่ระบบอ่านได้")
				res.Summary.Failed++
				res.addDetail(summary, "skipped", "empty_body", "อีเมลไม่มีเนื้อหาที่ระบบอ่านได้")
				res.Skipped++
				continue
			}

			logger.Info("imap_message_received",
				zap.String("trace_id", res.TraceID),
				zap.String("message_id", messageID),
				zap.String("subject", envelope.Subject),
			)

			msgSource := source
			if !envelope.Date.IsZero() {
				msgSource.EmailDate = envelope.Date.Format(time.RFC3339)
			}

			ok, warning := dispatch(cfg, p, envelope, summary.FromAddr, bodyBytes, messageID, msgSource, logger, res.TraceID)
			if ok && summary.UID != 0 {
				processedUIDs.AddNum(summary.UID)
				batchProcessedUIDs.AddNum(summary.UID)
				batchProcessed++
				res.Processed++
				res.Summary.Created++
				res.addDetail(summary, "processed", "accepted", "ส่งเข้ากระบวนการสร้างบิลแล้ว")
			} else {
				if warning != "" {
					code, label, userSkipped := classifyDispatchWarning(warning)
					if !userSkipped {
						res.ProcessWarnings = append(res.ProcessWarnings, warning)
						res.Summary.Failed++
					} else {
						res.Summary.SkippedUser++
						if code == "duplicate" {
							res.Summary.AlreadyProcessed++
						}
					}
					res.addDetail(summary, "skipped", code, label)
				} else {
					res.Summary.SkippedUser++
					res.addDetail(summary, "skipped", "not_processed", "ไม่เข้าเงื่อนไขการสร้างบิล")
				}
				res.Skipped++
			}
		}
		if len(batchProcessedUIDs) > 0 {
			markRead(c, batchProcessedUIDs, batchProcessed, logger, res.TraceID)
		}
		emitPollProgress(cfg, res)
	}

	if len(processedUIDs) > 0 {
		logger.Info("imap_mark_read", zap.String("trace_id", res.TraceID), zap.Int("count", res.Processed))
	}

	logger.Info("imap_poll_done",
		zap.String("trace_id", res.TraceID),
		zap.String("account_id", cfg.AccountID),
		zap.Int("messages_found", res.MessagesFound),
		zap.Int("processed", res.Processed),
		zap.Int("skipped", res.Skipped),
		zap.Int("summary_scanned", res.Summary.Scanned),
		zap.Int("summary_created", res.Summary.Created),
		zap.Int("summary_already_processed", res.Summary.AlreadyProcessed),
		zap.Int("summary_skipped_user", res.Summary.SkippedUser),
		zap.Int("summary_failed", res.Summary.Failed),
		zap.Int64("last_seen_uid", res.LastSeenUID),
		zap.Bool("limited", res.Limited),
		zap.Int("backlog", res.Backlog),
		zap.Int64("duration_ms", time.Since(pollStart).Milliseconds()),
	)

	emitPollProgress(cfg, res)
	return res
}

func emitPollProgress(cfg PollConfig, res PollResult) {
	if cfg.Progress == nil {
		return
	}
	cfg.Progress(res)
}

func configuredMaxMessagesPerRun() int {
	raw := strings.TrimSpace(os.Getenv("IMAP_MAX_MESSAGES_PER_RUN"))
	if raw == "" {
		return defaultMaxMessagesPerRun
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return defaultMaxMessagesPerRun
	}
	if n < minMaxMessagesPerRun {
		return minMaxMessagesPerRun
	}
	if n > maxMaxMessagesPerRun {
		return maxMaxMessagesPerRun
	}
	return n
}

func candidateUIDs(all []imap.UID, lastSeenUID int64, maxPerRun int) imapUIDCandidates {
	if maxPerRun <= 0 {
		maxPerRun = defaultMaxMessagesPerRun
	}
	remaining := make([]imap.UID, 0, len(all))
	for _, uid := range all {
		if int64(uid) > lastSeenUID {
			remaining = append(remaining, uid)
		}
	}
	out := imapUIDCandidates{Total: len(remaining)}
	if len(remaining) == 0 {
		return out
	}
	if len(remaining) > maxPerRun {
		out.Selected = remaining[:maxPerRun]
		out.Backlog = len(remaining) - maxPerRun
		out.Limited = true
		return out
	}
	out.Selected = remaining
	return out
}

func countUIDsAfter(all []imap.UID, lastSeenUID int64) int {
	n := 0
	for _, uid := range all {
		if int64(uid) > lastSeenUID {
			n++
		}
	}
	return n
}

func (r *PollResult) addDetail(summary imapMessageSummary, status, code, label string) {
	if r == nil {
		return
	}
	const maxPollDetails = 100
	if len(r.Details) >= maxPollDetails {
		return
	}
	d := models.IMAPPollDetail{
		UID:         uint32(summary.UID),
		From:        summary.FromAddr,
		Status:      status,
		ReasonCode:  code,
		ReasonLabel: label,
	}
	if summary.Envelope != nil {
		d.MessageID = summary.Envelope.MessageID
		d.Subject = summary.Envelope.Subject
		if !summary.Envelope.Date.IsZero() {
			d.EmailDate = summary.Envelope.Date.Format(time.RFC3339)
		}
	}
	r.Details = append(r.Details, d)
}

func fetchEnvelopeBatch(c *imapclient.Client, uids []imap.UID) ([]imapMessageSummary, error) {
	if len(uids) == 0 {
		return nil, nil
	}

	var uidSet imap.UIDSet
	for _, u := range uids {
		uidSet.AddNum(u)
	}

	fetchCmd := c.Fetch(uidSet, &imap.FetchOptions{UID: true, Envelope: true})
	summaries := make([]imapMessageSummary, 0, len(uids))
	for {
		msg := fetchCmd.Next()
		if msg == nil {
			break
		}

		var summary imapMessageSummary
		for {
			item := msg.Next()
			if item == nil {
				break
			}
			switch v := item.(type) {
			case imapclient.FetchItemDataUID:
				summary.UID = v.UID
			case imapclient.FetchItemDataEnvelope:
				summary.Envelope = v.Envelope
			}
		}
		if summary.Envelope != nil && len(summary.Envelope.From) > 0 {
			summary.FromAddr = summary.Envelope.From[0].Addr()
		}
		summaries = append(summaries, summary)
	}
	if err := fetchCmd.Close(); err != nil {
		return summaries, err
	}
	return summaries, nil
}

func messageIDsFromSummaries(summaries []imapMessageSummary) []string {
	if len(summaries) == 0 {
		return nil
	}
	seen := map[string]bool{}
	ids := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		if summary.Envelope == nil {
			continue
		}
		id := strings.TrimSpace(summary.Envelope.MessageID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

func fetchBodyForUID(c *imapclient.Client, uid imap.UID) ([]byte, error) {
	if uid == 0 {
		return nil, fmt.Errorf("empty uid")
	}

	var uidSet imap.UIDSet
	uidSet.AddNum(uid)
	bodySection := &imap.FetchItemBodySection{}
	fetchCmd := c.Fetch(uidSet, &imap.FetchOptions{
		UID:         true,
		BodySection: []*imap.FetchItemBodySection{bodySection},
	})

	var bodyBytes []byte
	for {
		msg := fetchCmd.Next()
		if msg == nil {
			break
		}
		for {
			item := msg.Next()
			if item == nil {
				break
			}
			if body, ok := item.(imapclient.FetchItemDataBodySection); ok && body.Literal != nil {
				bodyBytes, _ = io.ReadAll(body.Literal)
			}
		}
	}
	if err := fetchCmd.Close(); err != nil {
		return bodyBytes, err
	}
	return bodyBytes, nil
}

func markRead(c *imapclient.Client, uidSet imap.UIDSet, count int, logger *zap.Logger, traceID string) {
	if len(uidSet) == 0 {
		return
	}
	if err := c.Store(uidSet, &imap.StoreFlags{
		Op:     imap.StoreFlagsAdd,
		Flags:  []imap.Flag{imap.FlagSeen},
		Silent: true,
	}, nil).Close(); err != nil {
		logger.Warn("imap_mark_read_failed",
			zap.String("trace_id", traceID),
			zap.Int("count", count),
			zap.Error(err),
		)
	}
}

// dispatch routes one fetched message to the right Processor based on
// account channel + Shopee subject heuristics. Returns true if any
// processor accepted the message (so it can be marked Seen).
func dispatch(
	cfg PollConfig,
	p *Processors,
	envelope *imap.Envelope,
	fromAddr string,
	bodyBytes []byte,
	messageID string,
	source MailSource,
	logger *zap.Logger,
	traceID string,
) (bool, string) {
	if p == nil {
		return false, ""
	}

	switch cfg.Channel {
	case "shopee":
		// Only honor Shopee handlers when the From address is on the
		// configured Shopee domain list — guards against test imports
		// from non-Shopee senders polluting the bill stream.
		if !isShopeeFrom(fromAddr, cfg.ShopeeDomains) {
			logger.Info("imap_message_skipped",
				zap.String("trace_id", traceID), zap.String("from", fromAddr),
				zap.String("reason", "shopee_channel_non_shopee_from"),
			)
			return false, ""
		}
		plainText, bodyHTML := extractBodyParts(bodyBytes)
		if plainText == "" {
			plainText = bodyHTML
		}
		if isShippedSubject(envelope.Subject) && p.ShopeeShipped != nil {
			if err := p.ShopeeShipped(envelope.Subject, fromAddr, plainText, bodyHTML, messageID, source); err != nil {
				if skip, ok := err.(*MessageSkipError); ok {
					return false, skipDispatchWarning(skip)
				}
				logger.Warn("imap_shopee_shipped_failed",
					zap.String("trace_id", traceID), zap.String("message_id", messageID), zap.Error(err))
				return false, err.Error()
			}
			return true, ""
		}
		if p.ShopeeOrder != nil {
			if err := p.ShopeeOrder(envelope.Subject, fromAddr, plainText, bodyHTML, messageID, source); err != nil {
				if skip, ok := err.(*MessageSkipError); ok {
					return false, skipDispatchWarning(skip)
				}
				logger.Warn("imap_shopee_order_failed",
					zap.String("trace_id", traceID), zap.String("message_id", messageID), zap.Error(err))
				return false, err.Error()
			}
			return true, ""
		}
		return false, ""

	default:
		// general / lazada → attachment pipeline
		if p.Attachment == nil {
			return false, ""
		}
		return parseAndProcess(bodyBytes, messageID, envelope.Subject, fromAddr, source, p.Attachment, logger, traceID)
	}
}

func skipDispatchWarning(skip *MessageSkipError) string {
	if skip == nil {
		return ""
	}
	label := skip.Error()
	code := strings.TrimSpace(skip.Code)
	if code == "" {
		return label
	}
	if label == "" || label == code {
		return code
	}
	return code + ": " + label
}

func classifyDispatchWarning(warning string) (code, label string, userSkipped bool) {
	lower := strings.ToLower(strings.TrimSpace(warning))
	switch {
	case lower == "":
		return "not_processed", "ไม่เข้าเงื่อนไขการสร้างบิล", true
	case strings.Contains(lower, "duplicate_or_empty") || strings.Contains(warning, "ไม่มีบิลใหม่จากเมลนี้"):
		return "duplicate_or_empty", "เมลนี้ซ้ำหรือไม่มีรายการใหม่ให้สร้างบิล", true
	case strings.Contains(lower, "duplicate") || strings.Contains(warning, "เคย"):
		return "duplicate", "เมลนี้เคยประมวลผลหรือเคยสร้างบิลแล้ว", true
	case strings.Contains(lower, "no supported attachment"):
		return "no_supported_attachment", "ไม่พบไฟล์แนบที่รองรับ", true
	case strings.Contains(lower, "empty items") || strings.Contains(lower, "no items extracted"):
		return "empty_items", "อ่านเมลได้ แต่ไม่พบรายการสินค้า", false
	case strings.Contains(lower, "empty orders"):
		return "empty_orders", "อ่านเมลได้ แต่ไม่พบเลขคำสั่งซื้อหรือรายการคำสั่งซื้อ", false
	case strings.Contains(lower, "catalog service not configured"):
		return "catalog_not_ready", "ระบบสินค้า SML ยังไม่พร้อมสำหรับการจับคู่สินค้า", false
	default:
		return "processing_failed", warning, false
	}
}

// matchesSubject is true if any keyword is contained in the subject (case-insensitive).
// Empty filter list = match everything.
func matchesSubject(subject string, filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	lower := strings.ToLower(subject)
	for _, kw := range filters {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func subjectSearchCriteria(filters []string) imap.SearchCriteria {
	clean := make([]string, 0, len(filters))
	for _, f := range filters {
		f = strings.TrimSpace(f)
		if f != "" {
			clean = append(clean, f)
		}
	}
	if len(clean) == 0 {
		return imap.SearchCriteria{}
	}
	if len(clean) == 1 {
		return imap.SearchCriteria{
			Header: []imap.SearchCriteriaHeaderField{{Key: "Subject", Value: clean[0]}},
		}
	}
	first := imap.SearchCriteria{
		Header: []imap.SearchCriteriaHeaderField{{Key: "Subject", Value: clean[0]}},
	}
	rest := subjectSearchCriteria(clean[1:])
	return imap.SearchCriteria{Or: [][2]imap.SearchCriteria{{first, rest}}}
}

// isShippedSubject returns true if the subject indicates a Shopee
// payment-or-shipping confirmation — both should produce a purchase-order
// bill in SML. The two channels Shopee uses are:
//   - เก็บเงินปลายทาง (cash on delivery): subject contains "ถูกจัดส่งแล้ว"
//     when the package ships
//   - ชำระเงินทันที (pay now): subject contains "ยืนยันการชำระเงิน"
//     when the buyer pays — this is the equivalent trigger for COD-shipped
func isShippedSubject(subject string) bool {
	return strings.Contains(subject, "ถูกจัดส่งแล้ว") ||
		strings.Contains(subject, "ยืนยันการชำระเงิน")
}

// isShopeeFrom returns true if the from address matches any configured accepted
// sender. Empty accepted-sender list means "accept every sender that passed the
// subject filter", matching the UI copy in /settings/email. Each entry may be:
//   - a domain like "shopee.co.th" → matches if from ends with "@shopee.co.th"
//   - a full email like "user@example.com" → matches the exact address (used
//     for forwarded mail where a single forwarder relays Shopee notifications
//     into the bot's inbox under their own gmail address)
func isShopeeFrom(from string, domains []string) bool {
	from = strings.ToLower(strings.TrimSpace(from))
	if from == "" {
		return false
	}
	if len(domains) == 0 {
		return true
	}
	for _, d := range domains {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "" {
			continue
		}
		// Full email entry → exact match
		if strings.Contains(d, "@") {
			if from == d {
				return true
			}
			continue
		}
		// Domain entry → suffix match against @<domain>
		if strings.HasSuffix(from, "@"+d) {
			return true
		}
	}
	return false
}

// parseAndProcess extracts qualifying attachments from raw email bytes and
// fans them out to the AttachmentProcessor. Returns true if at least one
// attachment was processed successfully.
func parseAndProcess(
	rawMsg []byte,
	messageID, subject, fromAddr string,
	source MailSource,
	processor AttachmentProcessor,
	logger *zap.Logger,
	traceID string,
) (bool, string) {
	mr, err := gomail.CreateReader(bytes.NewReader(rawMsg))
	if err != nil {
		logger.Warn("imap_message_parse_failed",
			zap.String("trace_id", traceID), zap.String("message_id", messageID), zap.Error(err))
		return false, err.Error()
	}

	processed := false
	var warnings []string
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}

		var filename, mimeType string
		switch h := part.Header.(type) {
		case *gomail.AttachmentHeader:
			filename, _ = h.Filename()
			mimeType, _, _ = h.ContentType()
		case *gomail.InlineHeader:
			mimeType, _, _ = h.ContentType()
		default:
			continue
		}

		mimeType = strings.ToLower(strings.Split(mimeType, ";")[0])
		if !isSupportedAttachment(mimeType, filename) {
			continue
		}

		data, err := io.ReadAll(part.Body)
		if err != nil || len(data) == 0 {
			continue
		}

		logger.Info("imap_attachment_parsed",
			zap.String("trace_id", traceID),
			zap.String("message_id", messageID),
			zap.String("filename", filename),
			zap.String("mime_type", mimeType),
			zap.Int("size_bytes", len(data)),
		)

		if err := processor(data, mimeType, filename, messageID, subject, fromAddr, source); err == nil {
			processed = true
		} else {
			if skip, ok := err.(*MessageSkipError); ok {
				warnings = append(warnings, skip.Error())
				continue
			}
			warnings = append(warnings, err.Error())
			logger.Warn("imap_attachment_process_failed",
				zap.String("trace_id", traceID), zap.String("message_id", messageID), zap.Error(err))
		}
	}

	if processed {
		return true, ""
	}
	if len(warnings) > 0 {
		return false, strings.Join(warnings, "\n")
	}
	return false, "no supported attachment"
}

func isSupportedAttachment(mimeType, filename string) bool {
	if strings.HasPrefix(mimeType, "image/") {
		return true
	}
	if mimeType == "application/pdf" {
		return true
	}
	lower := strings.ToLower(filename)
	for _, ext := range []string{".pdf", ".jpg", ".jpeg", ".png", ".gif", ".webp"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// extractBodyParts pulls plain text and HTML from a raw RFC 2822 message.
// Returns (plainText, htmlBody) separately so callers can send plain to AI
// and store HTML for display. Gmail truncates text/html for long emails with
// "[ข้อความตัดทอน]" — plain text part is always complete.
func extractBodyParts(rawMsg []byte) (plainText, htmlBody string) {
	mr, err := gomail.CreateReader(bytes.NewReader(rawMsg))
	if err != nil {
		return "", ""
	}

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		if h, ok := part.Header.(*gomail.InlineHeader); ok {
			mimeType, _, _ := h.ContentType()
			mimeType = strings.ToLower(strings.Split(mimeType, ";")[0])
			data, err := io.ReadAll(part.Body)
			if err != nil || len(data) == 0 {
				continue
			}
			switch mimeType {
			case "text/plain":
				if plainText == "" {
					plainText = string(data)
				}
			case "text/html":
				if htmlBody == "" {
					htmlBody = string(data)
				}
			}
		}
	}
	return plainText, htmlBody
}

// extractBodyText returns the best single text for AI processing:
// prefers plain text (complete), falls back to HTML if no plain part.
func extractBodyText(rawMsg []byte) string {
	plain, html := extractBodyParts(rawMsg)
	if plain != "" {
		return plain
	}
	return html
}

// imapNewTraceID generates a random 12-char hex trace ID for poll cycles.
func imapNewTraceID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}
