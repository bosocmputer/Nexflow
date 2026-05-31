package lineservice

import (
	"fmt"
	"sync"

	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/repository"
)

// Registry holds one *Service per LINE OA, keyed by line_oa_accounts.id.
// Webhook + chat_inbox handlers call Get(oaID) to obtain the service for
// signature validation / Push / GetProfile.
//
// The registry is loaded on boot from line_oa_accounts.enabled=TRUE rows and
// can be refreshed via Reload() whenever an admin adds/edits/deletes an OA
// through /settings/line-oa.
type Registry struct {
	repo    *repository.LineOAAccountRepo
	logger  *zap.Logger
	mu      sync.RWMutex
	byID    map[string]*Service       // oa_id → service
	byBot   map[string]*Service       // bot_user_id → service (for legacy webhook routing by Destination)
	byIDOA  map[string]*models.LineOAAccount // oa_id → account snapshot (for greeting + admin_user_id)
}

func NewRegistry(repo *repository.LineOAAccountRepo, logger *zap.Logger) *Registry {
	return &Registry{
		repo:   repo,
		logger: logger,
		byID:   map[string]*Service{},
		byBot:  map[string]*Service{},
		byIDOA: map[string]*models.LineOAAccount{},
	}
}

// Reload re-reads all enabled OAs from the DB and rebuilds the lookup maps.
// Safe to call concurrently with Get — uses RWMutex.
func (r *Registry) Reload() error {
	rows, err := r.repo.ListEnabled()
	if err != nil {
		return fmt.Errorf("reload line OA registry: %w", err)
	}
	byID := map[string]*Service{}
	byBot := map[string]*Service{}
	byIDOA := map[string]*models.LineOAAccount{}
	for _, a := range rows {
		svc, err := New(a.ChannelSecret, a.ChannelAccessToken, a.AdminUserID)
		if err != nil {
			if r.logger != nil {
				r.logger.Warn("line OA registry: skip invalid account",
					zap.String("oa_id", a.ID), zap.String("name", a.Name), zap.Error(err))
			}
			continue
		}
		byID[a.ID] = svc
		if a.BotUserID != "" {
			byBot[a.BotUserID] = svc
		}
		byIDOA[a.ID] = a
	}
	r.mu.Lock()
	r.byID = byID
	r.byBot = byBot
	r.byIDOA = byIDOA
	r.mu.Unlock()
	if r.logger != nil {
		r.logger.Info("line OA registry loaded",
			zap.Int("count", len(byID)))
	}
	return nil
}

// Get returns the service for an OA id, or nil if not found / disabled.
func (r *Registry) Get(oaID string) *Service {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byID[oaID]
}

// GetByBotUserID looks up the service by the OA's own LINE bot user ID
// (the `destination` field in webhook payloads). Useful as a fallback when
// the URL doesn't carry the OA ID.
func (r *Registry) GetByBotUserID(botUserID string) *Service {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byBot[botUserID]
}

// Account returns the cached models.LineOAAccount for an OA id (read-only —
// callers must NOT mutate). Used to read greeting/admin_user_id fields without
// hitting the DB on every webhook event.
func (r *Registry) Account(oaID string) *models.LineOAAccount {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byIDOA[oaID]
}

// Any returns one arbitrary service from the registry — used by legacy single-OA
// code paths (e.g. PushAdmin called outside a per-OA context). Returns nil if
// the registry is empty.
func (r *Registry) Any() *Service {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, s := range r.byID {
		return s
	}
	return nil
}

// AnyAccount returns one arbitrary OA account from the registry — companion
// to Any() for accessing greeting/admin_user_id on legacy code paths.
func (r *Registry) AnyAccount() *models.LineOAAccount {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, a := range r.byIDOA {
		return a
	}
	return nil
}

// Count returns the number of services currently loaded — for /api/dashboard
// status reporting.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byID)
}
