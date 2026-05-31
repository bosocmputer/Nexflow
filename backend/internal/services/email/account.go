package emailservice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/repository"
	lineservice "nexflow/internal/services/line"
)

// AccountPoller wraps one IMAP account with its own ticker goroutine.
// The coordinator owns N of these (one per enabled account).
type AccountPoller struct {
	accountID  string
	repo       *repository.ImapAccountRepo
	jobRepo    *repository.IMAPPollJobRepo
	processors *Processors
	lineSvc    *lineservice.Service
	logger     *zap.Logger

	cancel context.CancelFunc
	done   chan struct{}

	mu          sync.Mutex
	running     bool
	skipInitial bool
	pollMu      sync.Mutex
}

// alertThrottle — minimum gap between LINE admin notifications per account
// when consecutive_failures stays ≥ 3. Prevents spamming during long outages.
const alertThrottle = 1 * time.Hour

// alertThreshold — number of consecutive failed polls before paging admin.
const alertThreshold = 3

const (
	backlogDrainDelay     = 2 * time.Second
	backlogDrainMaxRounds = 20
)

func NewAccountPoller(
	accountID string,
	repo *repository.ImapAccountRepo,
	jobRepo *repository.IMAPPollJobRepo,
	processors *Processors,
	lineSvc *lineservice.Service,
	logger *zap.Logger,
) *AccountPoller {
	return &AccountPoller{
		accountID:  accountID,
		repo:       repo,
		jobRepo:    jobRepo,
		processors: processors,
		lineSvc:    lineSvc,
		logger:     logger.With(zap.String("account_id", accountID)),
	}
}

// Start spawns the poll loop. Calling Start a second time is a no-op.
func (p *AccountPoller) Start(parent context.Context) {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	p.cancel = cancel
	p.done = make(chan struct{})
	p.running = true
	p.mu.Unlock()

	go p.run(ctx)
}

// Stop cancels the poll loop and waits for the goroutine to exit.
// Safe to call multiple times.
func (p *AccountPoller) Stop() {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return
	}
	cancel := p.cancel
	done := p.done
	p.running = false
	p.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		// Bound the wait so a stuck IMAP connection can't hang shutdown.
		select {
		case <-done:
		case <-time.After(8 * time.Second):
			p.logger.Warn("imap_poller_stop_timeout")
		}
	}
}

func (p *AccountPoller) run(ctx context.Context) {
	defer close(p.done)

	if !p.skipInitial {
		// Always poll once on start so admins see immediate feedback after
		// adding or editing an account.
		res := p.pollCycle(ctx)
		p.drainBacklog(ctx, res)
		if ctx.Err() != nil {
			return
		}
	}

	// Initial interval comes from the first DB read; re-read each tick so
	// admin can change the cadence without restarting the poller.
	for {
		account, err := p.repo.GetByID(p.accountID)
		if err != nil || account == nil || !account.Enabled {
			return
		}
		interval := time.Duration(account.PollIntervalSeconds) * time.Second
		if interval < 5*time.Minute {
			interval = 5 * time.Minute
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
		res := p.pollCycle(ctx)
		p.drainBacklog(ctx, res)
	}
}

// PollNow runs one cycle immediately, ignoring the interval. Used by the
// "poll-now" admin button. Returns the result for the caller to surface.
func (p *AccountPoller) PollNow(ctx context.Context) PollResult {
	if !p.pollMu.TryLock() {
		err := fmt.Errorf("poll already running")
		p.logger.Info("imap_poll_skipped_busy")
		return PollResult{Err: err}
	}
	res := p.pollCycleLocked(ctx)
	p.pollMu.Unlock()
	p.startBacklogDrain(ctx, res)
	return res
}

func (p *AccountPoller) IsPolling() bool {
	if !p.pollMu.TryLock() {
		return true
	}
	p.pollMu.Unlock()
	return false
}

func (p *AccountPoller) pollCycle(ctx context.Context) PollResult {
	if !p.pollMu.TryLock() {
		p.logger.Info("imap_poll_skipped_busy")
		return PollResult{}
	}
	defer p.pollMu.Unlock()
	return p.pollCycleLocked(ctx)
}

func (p *AccountPoller) pollCycleLocked(ctx context.Context) PollResult {
	account, err := p.repo.GetByID(p.accountID)
	if err != nil {
		p.logger.Error("imap_poller_load_failed", zap.Error(err))
		return PollResult{Err: err}
	}
	if account == nil {
		// Row was deleted — stop the goroutine.
		p.logger.Info("imap_poller_account_gone")
		if p.cancel != nil {
			p.cancel()
		}
		return PollResult{}
	}
	if !account.Enabled {
		// Disabled — caller will Stop() us shortly via coordinator.
		return PollResult{}
	}

	return p.pollLoadedAccount(ctx, account, nil)
}

func (p *AccountPoller) pollLoadedAccount(ctx context.Context, account *models.IMAPAccount, progress func(PollResult)) PollResult {
	cfg := pollConfigFromAccount(account)
	cfg.Progress = progress
	res := PollOnce(ctx, cfg, p.processors, p.logger)
	canceled := errors.Is(res.Err, context.Canceled)
	if canceled && res.Processed == 0 && res.Skipped == 0 && res.LastSeenUID <= account.LastSeenUID {
		p.logger.Info("imap_poll_interrupted_by_shutdown",
			zap.String("trace_id", res.TraceID),
			zap.Int("messages_found", res.MessagesFound),
			zap.Int("processed", res.Processed),
			zap.Int("skipped", res.Skipped),
			zap.Int64("last_seen_uid", res.LastSeenUID),
		)
		return res
	}

	errMsg := ""
	if res.Err != nil && !canceled {
		errMsg = res.Err.Error()
	} else if len(res.ProcessWarnings) > 0 {
		errMsg = strings.Join(compactWarnings(res.ProcessWarnings, 8), "\n")
	}
	if updateErr := p.repo.UpdatePollStatus(
		account.ID,
		res.Status(),
		errMsg,
		res.MessagesFound,
		res.Processed,
		res.Skipped,
		res.Summary,
		res.Details,
		res.LastSeenUID,
		res.Limited,
		res.Backlog,
	); updateErr != nil {
		p.logger.Warn("imap_poller_status_update_failed", zap.Error(updateErr))
	}

	if res.Err != nil && !canceled {
		p.maybeAlertAdmin(account, res)
	}

	return res
}

func (p *AccountPoller) RunPollJob(ctx context.Context, jobID string) {
	if p.jobRepo == nil {
		p.logger.Warn("imap_poll_job_repo_missing", zap.String("job_id", jobID))
		return
	}

	p.pollMu.Lock()
	defer p.pollMu.Unlock()

	if err := p.jobRepo.Start(jobID); err != nil {
		p.logger.Warn("imap_poll_job_start_failed", zap.String("job_id", jobID), zap.Error(err))
		return
	}

	var progress imapPollJobProgress
	var lastErr string
	status := models.IMAPPollJobCompleted

	for round := 1; round <= backlogDrainMaxRounds+1; round++ {
		account, err := p.repo.GetByID(p.accountID)
		if err != nil {
			lastErr = err.Error()
			status = models.IMAPPollJobFailed
			break
		}
		if account == nil || !account.Enabled {
			lastErr = "imap account not found or disabled"
			status = models.IMAPPollJobFailed
			break
		}

		base := progress.snapshotBase()
		res := p.pollLoadedAccount(ctx, account, func(partial PollResult) {
			progress.applyPartial(base, partial)
			if err := p.jobRepo.UpdateProgress(jobID, progress.toRepoInput()); err != nil {
				p.logger.Warn("imap_poll_job_progress_failed",
					zap.String("job_id", jobID),
					zap.String("trace_id", partial.TraceID),
					zap.Error(err),
				)
			}
		})
		progress.applyFinal(base, res)
		lastErr = progress.LastError
		if err := p.jobRepo.UpdateProgress(jobID, progress.toRepoInput()); err != nil {
			p.logger.Warn("imap_poll_job_progress_failed", zap.String("job_id", jobID), zap.Error(err))
		}

		if res.Err != nil {
			if errors.Is(res.Err, context.Canceled) {
				status = models.IMAPPollJobFailed
				lastErr = "server stopped before job completed"
			} else {
				status = models.IMAPPollJobFailed
				lastErr = res.Err.Error()
			}
			break
		}
		if !shouldDrainBacklog(res) {
			if progress.FailedCount > 0 {
				status = models.IMAPPollJobCompletedWithErrors
			}
			break
		}
		select {
		case <-ctx.Done():
			status = models.IMAPPollJobFailed
			lastErr = "server stopped before job completed"
			_ = p.jobRepo.UpdateProgress(jobID, progress.toRepoInput())
			_ = p.jobRepo.Finish(jobID, status, lastErr)
			return
		case <-time.After(backlogDrainDelay):
		}
	}
	if status == models.IMAPPollJobCompleted && progress.FailedCount > 0 {
		status = models.IMAPPollJobCompletedWithErrors
	}
	if err := p.jobRepo.Finish(jobID, status, lastErr); err != nil {
		p.logger.Warn("imap_poll_job_finish_failed", zap.String("job_id", jobID), zap.Error(err))
	}
}

func (p *AccountPoller) startBacklogDrain(ctx context.Context, previous PollResult) {
	if !shouldDrainBacklog(previous) {
		return
	}
	go p.drainBacklog(ctx, previous)
}

func (p *AccountPoller) drainBacklog(ctx context.Context, previous PollResult) {
	for round := 1; round <= backlogDrainMaxRounds && shouldDrainBacklog(previous); round++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(backlogDrainDelay):
		}

		if !p.pollMu.TryLock() {
			p.logger.Info("imap_backlog_drain_skipped_busy",
				zap.String("trace_id", previous.TraceID),
				zap.Int("backlog", previous.Backlog),
			)
			return
		}
		p.logger.Info("imap_backlog_drain_continue",
			zap.String("previous_trace_id", previous.TraceID),
			zap.Int("round", round),
			zap.Int("previous_backlog", previous.Backlog),
		)
		previous = p.pollCycleLocked(ctx)
		p.pollMu.Unlock()
	}
	if shouldDrainBacklog(previous) {
		p.logger.Info("imap_backlog_drain_paused",
			zap.String("trace_id", previous.TraceID),
			zap.Int("backlog", previous.Backlog),
			zap.Int("max_rounds", backlogDrainMaxRounds),
		)
	}
}

func shouldDrainBacklog(res PollResult) bool {
	if res.Err != nil {
		return false
	}
	return res.Limited || res.Backlog > 0
}

type imapPollJobProgress struct {
	TotalCount    int
	ScannedCount  int
	CreatedCount  int
	SkippedCount  int
	FailedCount   int
	BacklogCount  int
	ReasonCounts  map[string]int
	LatestDetails []models.IMAPPollDetail
	LastError     string
}

type imapPollJobProgressBase struct {
	TotalCount    int
	ScannedCount  int
	CreatedCount  int
	SkippedCount  int
	FailedCount   int
	ReasonCounts  map[string]int
	LatestDetails []models.IMAPPollDetail
}

func (p *imapPollJobProgress) snapshotBase() imapPollJobProgressBase {
	return imapPollJobProgressBase{
		TotalCount:    p.TotalCount,
		ScannedCount:  p.ScannedCount,
		CreatedCount:  p.CreatedCount,
		SkippedCount:  p.SkippedCount,
		FailedCount:   p.FailedCount,
		ReasonCounts:  copyReasonCounts(p.ReasonCounts),
		LatestDetails: append([]models.IMAPPollDetail{}, p.LatestDetails...),
	}
}

func (p *imapPollJobProgress) applyPartial(base imapPollJobProgressBase, res PollResult) {
	if p.ReasonCounts == nil {
		p.ReasonCounts = map[string]int{}
	}
	if base.TotalCount > 0 {
		p.TotalCount = base.TotalCount
	} else {
		p.TotalCount = res.MessagesFound
	}
	p.ScannedCount = base.ScannedCount + res.Summary.Scanned
	p.CreatedCount = base.CreatedCount + res.Summary.Created
	p.SkippedCount = base.SkippedCount + pollJobSkippedCount(res.Summary)
	p.FailedCount = base.FailedCount + res.Summary.Failed
	p.BacklogCount = res.Backlog
	p.ReasonCounts = mergeReasonCounts(base.ReasonCounts, res.Details)
	p.LatestDetails = capPollJobDetails(append(append([]models.IMAPPollDetail{}, base.LatestDetails...), res.Details...))
	if res.Err != nil {
		p.LastError = res.Err.Error()
	} else if len(res.ProcessWarnings) > 0 {
		p.LastError = strings.Join(compactWarnings(res.ProcessWarnings, 4), "\n")
	}
}

func (p *imapPollJobProgress) applyFinal(base imapPollJobProgressBase, res PollResult) {
	p.applyPartial(base, res)
}

func pollJobSkippedCount(summary models.IMAPPollSummary) int {
	skipped := summary.Scanned - summary.Created - summary.Failed
	if skipped < 0 {
		return 0
	}
	return skipped
}

func (p imapPollJobProgress) toRepoInput() repository.UpdateIMAPPollJobProgressInput {
	return repository.UpdateIMAPPollJobProgressInput{
		TotalCount:    p.TotalCount,
		ScannedCount:  p.ScannedCount,
		CreatedCount:  p.CreatedCount,
		SkippedCount:  p.SkippedCount,
		FailedCount:   p.FailedCount,
		BacklogCount:  p.BacklogCount,
		ReasonCounts:  p.ReasonCounts,
		LatestDetails: p.LatestDetails,
		LastError:     p.LastError,
	}
}

func copyReasonCounts(in map[string]int) map[string]int {
	out := map[string]int{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func mergeReasonCounts(base map[string]int, details []models.IMAPPollDetail) map[string]int {
	out := copyReasonCounts(base)
	for _, detail := range details {
		key := strings.TrimSpace(detail.ReasonCode)
		if key == "" {
			key = strings.TrimSpace(detail.Status)
		}
		if key == "" {
			key = "unknown"
		}
		out[key]++
	}
	return out
}

func capPollJobDetails(in []models.IMAPPollDetail) []models.IMAPPollDetail {
	const limit = 100
	if len(in) <= limit {
		return in
	}
	return append([]models.IMAPPollDetail{}, in[len(in)-limit:]...)
}

// maybeAlertAdmin pushes a LINE message to the admin if this account has
// failed ≥ alertThreshold times in a row AND we haven't alerted within
// alertThrottle. Re-reads the row after UpdatePollStatus so consecutive_failures
// is current.
func (p *AccountPoller) maybeAlertAdmin(_ *models.IMAPAccount, res PollResult) {
	if p.lineSvc == nil {
		return
	}
	fresh, err := p.repo.GetByID(p.accountID)
	if err != nil || fresh == nil {
		return
	}
	if fresh.ConsecutiveFailures < alertThreshold {
		return
	}
	if fresh.LastAdminAlertAt != nil && time.Since(*fresh.LastAdminAlertAt) < alertThrottle {
		return
	}

	msg := fmt.Sprintf(
		"⚠️ Nexflow IMAP fail\nInbox: %s (%s)\nFails: %d ครั้งติด\nError: %s",
		fresh.Name, fresh.Username, fresh.ConsecutiveFailures, truncate(res.Err.Error(), 200),
	)
	if err := p.lineSvc.PushAdmin(msg); err != nil {
		p.logger.Warn("imap_admin_alert_failed", zap.Error(err))
		return
	}
	if err := p.repo.MarkAlertSent(fresh.ID); err != nil {
		p.logger.Warn("imap_admin_alert_stamp_failed", zap.Error(err))
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func compactWarnings(warnings []string, limit int) []string {
	if limit <= 0 {
		limit = 8
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(warnings))
	for _, w := range warnings {
		w = strings.TrimSpace(w)
		if w == "" || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	if len(out) <= limit {
		return out
	}
	hidden := len(out) - limit
	return append(out[:limit], fmt.Sprintf("มีคำเตือนอื่นอีก %d รายการ", hidden))
}

// pollConfigFromAccount snapshots the DB row into a value struct so the
// goroutine isn't holding the *IMAPAccount across a long-running poll.
func pollConfigFromAccount(a *models.IMAPAccount) PollConfig {
	return PollConfig{
		AccountID:      a.ID,
		AccountName:    a.Name,
		Host:           a.Host,
		Port:           a.Port,
		Username:       a.Username,
		Password:       a.Password,
		Mailbox:        a.Mailbox,
		FilterFrom:     a.FilterFrom,
		FilterSubjects: parseCSV(a.FilterSubjects, true),
		LookbackDays:   a.LookbackDays,
		LastSeenUID:    a.LastSeenUID,
		Channel:        a.Channel,
		ShopeeDomains:  parseCSV(a.ShopeeDomains, true),
	}
}

// parseCSV splits a comma-separated string and trims whitespace + drops
// empty entries. lower=true normalizes to lowercase for case-insensitive matching.
func parseCSV(s string, lower bool) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if lower {
			p = strings.ToLower(p)
		}
		out = append(out, p)
	}
	return out
}
