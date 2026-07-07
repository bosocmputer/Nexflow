package nextstepnotifications

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/events"
	"nexflow/internal/services/sml"
)

const (
	defaultPollInterval = 60 * time.Second
	defaultPollTimeout  = 45 * time.Second
	defaultPageSize     = 50
	defaultPageCap      = 10
	errorLogInterval    = 5 * time.Minute
)

type MarketplaceClient interface {
	IsConfigured() bool
	Fetch(context.Context, sml.NextStepMarketplaceRequest) (*sml.NextStepMarketplaceData, error)
}

type SeenRepository interface {
	BaselineCompleted(context.Context) (bool, error)
	MarkBaselineCompleted(context.Context) error
	UpsertSeen(context.Context, repository.NextStepMarketplaceSeenInput) (bool, error)
	MarkNotified(context.Context, string) error
	DeleteIfUnnotified(context.Context, string) error
}

type NotificationRepository interface {
	CreateForRoles(context.Context, []string, models.NotificationInput) ([]models.Notification, error)
	UnreadCount(context.Context, string) (int, error)
	UnreadCountsBySource(context.Context, string) (map[string]int, error)
}

type EventPublisher interface {
	Publish(events.Event)
}

type Worker struct {
	client       MarketplaceClient
	seenRepo     SeenRepository
	notifyRepo   NotificationRepository
	broker       EventPublisher
	logger       *zap.Logger
	location     *time.Location
	interval     time.Duration
	pollTimeout  time.Duration
	pageSize     int
	pageCap      int
	now          func() time.Time
	lastErrorLog time.Time

	mu      sync.Mutex
	running bool
}

type PollResult struct {
	Baseline        bool
	OrdersSeen      int
	Inserted        int
	Notifications   int
	SkippedInactive int
}

func NewWorker(client MarketplaceClient, seenRepo SeenRepository, notifyRepo NotificationRepository, broker EventPublisher, logger *zap.Logger) *Worker {
	loc, err := time.LoadLocation("Asia/Bangkok")
	if err != nil {
		loc = time.FixedZone("Asia/Bangkok", 7*60*60)
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Worker{
		client:      client,
		seenRepo:    seenRepo,
		notifyRepo:  notifyRepo,
		broker:      broker,
		logger:      logger,
		location:    loc,
		interval:    defaultPollInterval,
		pollTimeout: defaultPollTimeout,
		pageSize:    defaultPageSize,
		pageCap:     defaultPageCap,
		now:         time.Now,
	}
}

func (w *Worker) Start(ctx context.Context) {
	if w == nil || w.client == nil || w.seenRepo == nil || w.notifyRepo == nil {
		return
	}
	if !w.client.IsConfigured() {
		w.logger.Info("nextstep notifications disabled: SML marketplace client is not configured")
		return
	}
	go w.loop(ctx)
}

func (w *Worker) loop(ctx context.Context) {
	initial := time.NewTimer(10 * time.Second)
	defer initial.Stop()
	select {
	case <-ctx.Done():
		return
	case <-initial.C:
		w.pollAndLog(ctx)
	}

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.pollAndLog(ctx)
		}
	}
}

func (w *Worker) pollAndLog(ctx context.Context) {
	start := w.now()
	result, err := w.PollOnce(ctx)
	if err != nil {
		w.logPollError(err)
		return
	}
	w.logger.Debug("nextstep notification poll completed",
		zap.Bool("baseline", result.Baseline),
		zap.Int("orders_seen", result.OrdersSeen),
		zap.Int("inserted", result.Inserted),
		zap.Int("notifications", result.Notifications),
		zap.Int("skipped_inactive", result.SkippedInactive),
		zap.Duration("duration", w.now().Sub(start)),
	)
}

func (w *Worker) PollOnce(ctx context.Context) (PollResult, error) {
	var result PollResult
	if w == nil || w.client == nil || w.seenRepo == nil || w.notifyRepo == nil {
		return result, nil
	}
	if !w.client.IsConfigured() {
		return result, nil
	}
	if !w.beginPoll() {
		return result, nil
	}
	defer w.endPoll()

	pollCtx, cancel := context.WithTimeout(ctx, w.pollTimeout)
	defer cancel()

	baselineCompleted, err := w.seenRepo.BaselineCompleted(pollCtx)
	if err != nil {
		return result, fmt.Errorf("baseline state: %w", err)
	}
	result.Baseline = !baselineCompleted

	orders, err := w.fetchToday(pollCtx)
	if err != nil {
		return result, err
	}
	result.OrdersSeen = len(orders)

	for _, order := range orders {
		docNo := strings.TrimSpace(order.DocNo)
		if docNo == "" {
			continue
		}
		inserted, err := w.seenRepo.UpsertSeen(pollCtx, repository.NextStepMarketplaceSeenInput{
			DocNo:   docNo,
			DocDate: strings.TrimSpace(order.DocDate),
			Status:  strings.TrimSpace(order.Status),
		})
		if err != nil {
			return result, fmt.Errorf("record seen %s: %w", docNo, err)
		}
		if !inserted {
			continue
		}
		result.Inserted++
		if result.Baseline {
			continue
		}
		if !nextStepOrderShouldNotify(order.Status) {
			result.SkippedInactive++
			continue
		}
		created, err := w.publishOrderNotification(pollCtx, order)
		if err != nil {
			_ = w.seenRepo.DeleteIfUnnotified(context.Background(), docNo)
			return result, err
		}
		if created > 0 {
			if err := w.seenRepo.MarkNotified(pollCtx, docNo); err != nil {
				w.logger.Warn("nextstep notification mark notified failed", zap.String("doc_no", docNo), zap.Error(err))
			}
			result.Notifications += created
		}
	}
	if result.Baseline {
		if err := w.seenRepo.MarkBaselineCompleted(pollCtx); err != nil {
			return result, fmt.Errorf("mark baseline completed: %w", err)
		}
		w.logger.Info("nextstep notification baseline completed", zap.Int("orders_seen", result.OrdersSeen))
	}
	return result, nil
}

func (w *Worker) fetchToday(ctx context.Context) ([]sml.NextStepMarketplaceOrder, error) {
	today := w.now().In(w.location).Format("2006-01-02")
	includeOrders := true
	orders := []sml.NextStepMarketplaceOrder{}
	for page := 1; page <= w.pageCap; page++ {
		data, err := w.client.Fetch(ctx, sml.NextStepMarketplaceRequest{
			DateFrom:      today,
			DateTo:        today,
			Page:          page,
			Size:          w.pageSize,
			IncludeOrders: &includeOrders,
		})
		if err != nil {
			return nil, fmt.Errorf("fetch NextStep marketplace page %d: %w", page, err)
		}
		orders = append(orders, data.Orders...)
		if len(data.Orders) < w.pageSize || data.Meta.Total <= page*w.pageSize {
			return orders, nil
		}
	}
	w.logger.Warn("nextstep notification poll reached page cap",
		zap.Int("page_cap", w.pageCap),
		zap.Int("page_size", w.pageSize),
		zap.Int("orders_seen", len(orders)),
	)
	return orders, nil
}

func (w *Worker) publishOrderNotification(ctx context.Context, order sml.NextStepMarketplaceOrder) (int, error) {
	docNo := strings.TrimSpace(order.DocNo)
	if docNo == "" {
		return 0, nil
	}
	created, err := w.notifyRepo.CreateForRoles(ctx, []string{"admin", "staff"}, models.NotificationInput{
		Source:     "nextstep_marketplace",
		Severity:   "info",
		Title:      "มีออเดอร์ NextStep Marketplace ใหม่",
		Body:       nextStepNotificationBody(order),
		ActionURL:  nextStepNotificationActionURL(order),
		EntityType: "nextstep_order",
		EntityID:   docNo,
		DedupeKey:  "nextstep:new_order:" + docNo,
	})
	if err != nil {
		return 0, fmt.Errorf("create NextStep notification %s: %w", docNo, err)
	}
	for _, n := range created {
		unread, _ := w.notifyRepo.UnreadCount(ctx, n.RecipientID)
		bySource, _ := w.notifyRepo.UnreadCountsBySource(ctx, n.RecipientID)
		if bySource == nil {
			bySource = map[string]int{}
		}
		if w.broker == nil {
			continue
		}
		w.broker.Publish(events.Event{
			Type:         events.TypeNotificationCreated,
			TargetUserID: n.RecipientID,
			Payload:      map[string]any{"notification": n, "unread_count": unread, "unread_by_source": bySource},
		})
		w.broker.Publish(events.Event{
			Type:         events.TypeNotificationUnreadChanged,
			TargetUserID: n.RecipientID,
			Payload:      map[string]any{"total": unread, "unread_by_source": bySource},
		})
	}
	return len(created), nil
}

func (w *Worker) beginPoll() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running {
		return false
	}
	w.running = true
	return true
}

func (w *Worker) endPoll() {
	w.mu.Lock()
	w.running = false
	w.mu.Unlock()
}

func (w *Worker) logPollError(err error) {
	now := w.now()
	if w.lastErrorLog.IsZero() || now.Sub(w.lastErrorLog) >= errorLogInterval {
		w.lastErrorLog = now
		w.logger.Warn("nextstep notification poll failed", zap.Error(err))
	}
}

func nextStepOrderShouldNotify(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "success", "cancel":
		return false
	default:
		return true
	}
}

func nextStepNotificationBody(order sml.NextStepMarketplaceOrder) string {
	parts := []string{strings.TrimSpace(order.DocNo)}
	if label := nextStepStatusLabel(order.Status); label != "" {
		parts = append(parts, label)
	}
	if order.TotalAmount > 0 {
		parts = append(parts, fmt.Sprintf("ยอด %.2f", order.TotalAmount))
	}
	if strings.TrimSpace(order.DocTime) != "" {
		parts = append(parts, strings.TrimSpace(order.DocTime))
	}
	return strings.Join(parts, " · ")
}

func nextStepNotificationActionURL(order sml.NextStepMarketplaceOrder) string {
	docNo := strings.TrimSpace(order.DocNo)
	docDate := strings.TrimSpace(order.DocDate)
	if docDate == "" {
		docDate = time.Now().Format("2006-01-02")
	}
	q := url.Values{}
	q.Set("from_date", docDate)
	q.Set("to_date", docDate)
	if docNo != "" {
		q.Set("search", docNo)
	}
	return "/nextstep-marketplace?" + q.Encode()
}

func nextStepStatusLabel(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "pending":
		return "รอดำเนินการ"
	case "packing":
		return "แพ็กของ"
	case "payment":
		return "รอชำระ"
	case "success":
		return "สำเร็จ"
	case "cancel":
		return "ยกเลิก"
	default:
		return strings.TrimSpace(status)
	}
}
