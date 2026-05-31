package emailservice

import (
	"context"
	"fmt"
	"sync"

	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-sasl"
	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/repository"
	lineservice "nexflow/internal/services/line"
)

// Coordinator manages one AccountPoller per enabled imap_accounts row.
// Hot-reloads on admin edits via ReloadAccount/RemoveAccount so the
// server never has to restart to pick up config changes.
type Coordinator struct {
	repo       *repository.ImapAccountRepo
	jobRepo    *repository.IMAPPollJobRepo
	processors *Processors
	lineSvc    *lineservice.Service
	logger     *zap.Logger

	ctx    context.Context
	cancel context.CancelFunc

	mu      sync.Mutex
	pollers map[string]*AccountPoller
}

func NewCoordinator(
	repo *repository.ImapAccountRepo,
	jobRepo *repository.IMAPPollJobRepo,
	processors *Processors,
	lineSvc *lineservice.Service,
	logger *zap.Logger,
) *Coordinator {
	return &Coordinator{
		repo:       repo,
		jobRepo:    jobRepo,
		processors: processors,
		lineSvc:    lineSvc,
		logger:     logger.With(zap.String("component", "imap_coordinator")),
		pollers:    map[string]*AccountPoller{},
	}
}

// Start loads every enabled account from the DB and spawns its poller.
// Safe to call once at boot. Subsequent admin edits use ReloadAccount.
func (c *Coordinator) Start(parent context.Context) error {
	c.ctx, c.cancel = context.WithCancel(parent)

	accounts, err := c.repo.ListEnabled()
	if err != nil {
		return fmt.Errorf("coordinator load accounts: %w", err)
	}

	c.logger.Info("coordinator_start", zap.Int("enabled_accounts", len(accounts)))
	for _, a := range accounts {
		c.startPoller(a, true)
	}
	return nil
}

// Stop cancels every poller and waits (with a per-poller timeout) for them
// to exit. Called at server shutdown.
func (c *Coordinator) Stop() {
	c.mu.Lock()
	pollers := make([]*AccountPoller, 0, len(c.pollers))
	for _, p := range c.pollers {
		pollers = append(pollers, p)
	}
	c.pollers = map[string]*AccountPoller{}
	c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}
	for _, p := range pollers {
		p.Stop()
	}
}

// ReloadAccount stops any existing poller for this id then starts a fresh
// one if the row is currently enabled. Idempotent.
func (c *Coordinator) ReloadAccount(id string) error {
	return c.reloadAccount(id, true)
}

// ReloadAccountIdle restarts the background ticker without the immediate
// startup poll. Used after a manual one-off poll has already run.
func (c *Coordinator) ReloadAccountIdle(id string) error {
	return c.reloadAccount(id, false)
}

func (c *Coordinator) reloadAccount(id string, pollInitial bool) error {
	account, err := c.repo.GetByID(id)
	if err != nil {
		return fmt.Errorf("coordinator reload: %w", err)
	}

	c.mu.Lock()
	existing := c.pollers[id]
	c.mu.Unlock()

	if existing != nil {
		existing.Stop()
		c.mu.Lock()
		delete(c.pollers, id)
		c.mu.Unlock()
	}

	if account == nil || !account.Enabled {
		c.logger.Info("coordinator_account_inactive", zap.String("account_id", id))
		return nil
	}

	c.startPoller(account, pollInitial)
	return nil
}

// RemoveAccount stops the poller for this id (if any) without trying to
// reload from DB. Used when an admin deletes a row.
func (c *Coordinator) RemoveAccount(id string) {
	c.mu.Lock()
	p := c.pollers[id]
	delete(c.pollers, id)
	c.mu.Unlock()
	if p != nil {
		p.Stop()
	}
}

// PollNow runs one immediate poll for the named account, regardless of
// whether the goroutine is mid-interval. Returns the cycle result.
func (c *Coordinator) PollNow(id string) (PollResult, error) {
	c.mu.Lock()
	p := c.pollers[id]
	c.mu.Unlock()
	if p == nil {
		// Account might be disabled or not yet loaded — spin up an ad-hoc
		// poller just for this one cycle so the admin's "test poll" works.
		account, err := c.repo.GetByID(id)
		if err != nil {
			return PollResult{}, err
		}
		if account == nil {
			return PollResult{}, fmt.Errorf("account not found")
		}
		oneOff := NewAccountPoller(account.ID, c.repo, c.jobRepo, c.processors, c.lineSvc, c.logger)
		return oneOff.PollNow(c.ctx), nil
	}
	return p.PollNow(c.ctx), nil
}

func (c *Coordinator) StartPollJob(id, userID, userEmail string) (*models.IMAPPollJob, error) {
	if c.jobRepo == nil {
		return nil, fmt.Errorf("imap poll job repository not configured")
	}
	account, err := c.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, fmt.Errorf("account not found")
	}
	if !account.Enabled {
		return nil, fmt.Errorf("account disabled")
	}

	job, existing, err := c.jobRepo.CreateOrGetActive(repository.CreateIMAPPollJobInput{
		AccountID:      account.ID,
		CreatedBy:      userID,
		CreatedByEmail: userEmail,
	})
	if err != nil {
		return nil, err
	}
	if existing {
		return job, nil
	}

	c.mu.Lock()
	p := c.pollers[id]
	c.mu.Unlock()
	if p == nil {
		p = NewAccountPoller(account.ID, c.repo, c.jobRepo, c.processors, c.lineSvc, c.logger)
	}
	go p.RunPollJob(c.ctx, job.ID)
	return job, nil
}

// IsPolling reports whether the account currently has a poll cycle or
// background backlog drain in flight. It is intentionally best-effort for UI.
func (c *Coordinator) IsPolling(id string) bool {
	c.mu.Lock()
	p := c.pollers[id]
	c.mu.Unlock()
	if p == nil {
		return false
	}
	return p.IsPolling()
}

// TestConnection runs a dry connect+auth+select-mailbox against the supplied
// account values without saving anything. Used by "ทดสอบการเชื่อมต่อ" button.
func (c *Coordinator) TestConnection(ctx context.Context, a *models.IMAPAccount) error {
	cfg := pollConfigFromAccount(a)
	mailbox := cfg.Mailbox
	if mailbox == "" {
		mailbox = "INBOX"
	}

	errCh := make(chan error, 1)
	go func() {
		addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
		client, err := imapclient.DialTLS(addr, nil)
		if err != nil {
			errCh <- fmt.Errorf("IMAP connect %s: %w", addr, err)
			return
		}
		defer client.Close()

		if err := client.Authenticate(sasl.NewPlainClient("", cfg.Username, cfg.Password)); err != nil {
			errCh <- fmt.Errorf("IMAP authenticate: %w", err)
			return
		}
		if _, err := client.Select(mailbox, nil).Wait(); err != nil {
			errCh <- fmt.Errorf("IMAP select %s: %w", mailbox, err)
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

func (c *Coordinator) startPoller(a *models.IMAPAccount, pollInitial bool) {
	p := NewAccountPoller(a.ID, c.repo, c.jobRepo, c.processors, c.lineSvc, c.logger)
	p.skipInitial = !pollInitial
	c.mu.Lock()
	c.pollers[a.ID] = p
	c.mu.Unlock()
	p.Start(c.ctx)
	c.logger.Info("coordinator_poller_started",
		zap.String("account_id", a.ID),
		zap.String("name", a.Name),
		zap.String("channel", a.Channel),
		zap.Int("interval_sec", a.PollIntervalSeconds),
	)
}
