package tiktokshop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	tikTokOrderReconcilePageSize = 20
	tikTokOrderReconcileMaxPages = 10
	tikTokOrderReconcileMaxSpan  = 24 * time.Hour
)

var (
	ErrInvalidOrderReconcileInput = errors.New("invalid TikTok Shop order reconciliation input")
	ErrOrderReconcileFailed       = errors.New("TikTok Shop order reconciliation failed")
)

type TikTokOrderReconcileRequest struct {
	ShopID       string `json:"shop_id"`
	UpdateTimeGE int64  `json:"update_time_ge"`
	UpdateTimeLT int64  `json:"update_time_lt"`
}

type TikTokOrderReconcileRun struct {
	ID          string
	ShopID      string
	WindowStart time.Time
	WindowEnd   time.Time
}

type TikTokOrderReconcileProgress struct {
	PageCount        int
	DiscoveredCount  int
	SnapshottedCount int
	LastPageToken    string
	SearchRequestIDs []string
}

type TikTokOrderReconcileResult struct {
	RunID            string    `json:"run_id"`
	ShopID           string    `json:"shop_id"`
	WindowStart      time.Time `json:"window_start"`
	WindowEnd        time.Time `json:"window_end"`
	PageCount        int       `json:"page_count"`
	DiscoveredCount  int       `json:"discovered_count"`
	SnapshottedCount int       `json:"snapshotted_count"`
}

type orderReconcileGateway interface {
	SearchOrders(context.Context, GatewayOrderSearchRequest) (*GatewayOrderSearchResponse, error)
}

type orderReconcileSnapshotter interface {
	Sync(context.Context, TikTokOrderSnapshotRequest) (*TikTokOrderSnapshotResult, error)
}

type orderReconcileStore interface {
	StartManualRun(context.Context, TikTokOrderReconcileRequest) (TikTokOrderReconcileRun, error)
	RecordPage(context.Context, string, TikTokOrderReconcileProgress) error
	CompleteRun(context.Context, string, TikTokOrderReconcileProgress) error
	FailRun(context.Context, string, string, string) error
}

type TikTokOrderReconciler struct {
	gateway   orderReconcileGateway
	snapshots orderReconcileSnapshotter
	store     orderReconcileStore
}

func NewTikTokOrderReconciler(gateway orderReconcileGateway, snapshots orderReconcileSnapshotter, store orderReconcileStore) *TikTokOrderReconciler {
	return &TikTokOrderReconciler{gateway: gateway, snapshots: snapshots, store: store}
}

func (s *TikTokOrderReconciler) Reconcile(ctx context.Context, input TikTokOrderReconcileRequest) (*TikTokOrderReconcileResult, error) {
	input.ShopID = strings.TrimSpace(input.ShopID)
	if s == nil || s.gateway == nil || s.snapshots == nil || s.store == nil || input.Validate() != nil {
		return nil, ErrInvalidOrderReconcileInput
	}
	run, err := s.store.StartManualRun(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("%w: start run", ErrOrderReconcileFailed)
	}
	return s.execute(ctx, run)
}

func (s *TikTokOrderReconciler) execute(ctx context.Context, run TikTokOrderReconcileRun) (*TikTokOrderReconcileResult, error) {
	if strings.TrimSpace(run.ID) == "" || !tikTokNumericIDPattern.MatchString(strings.TrimSpace(run.ShopID)) ||
		run.WindowStart.IsZero() || !run.WindowStart.Before(run.WindowEnd) || run.WindowEnd.Sub(run.WindowStart) > tikTokOrderReconcileMaxSpan {
		return nil, ErrInvalidOrderReconcileInput
	}
	progress := TikTokOrderReconcileProgress{SearchRequestIDs: make([]string, 0, tikTokOrderReconcileMaxPages)}
	seenOrders := make(map[string]struct{})
	seenTokens := make(map[string]struct{})
	pageToken := ""
	for page := 0; page < tikTokOrderReconcileMaxPages; page++ {
		response, err := s.gateway.SearchOrders(ctx, GatewayOrderSearchRequest{
			ShopID: run.ShopID,
			Search: SearchOrdersRequest{
				PageSize: tikTokOrderReconcilePageSize, PageToken: pageToken,
				SortField: OrderSortFieldUpdateTime, SortOrder: OrderSortAscending,
				Filters: OrderSearchFilters{UpdateTimeGE: run.WindowStart.Unix(), UpdateTimeLT: run.WindowEnd.Unix()},
			},
		})
		if err != nil {
			return nil, s.fail(ctx, run.ID, "search_failed", "TikTok Shop Order List request failed")
		}
		if response == nil || strings.TrimSpace(response.UpstreamRequestID) == "" {
			return nil, s.fail(ctx, run.ID, "search_response_invalid", "TikTok Shop Order List response was incomplete")
		}
		nextPageToken := strings.TrimSpace(response.NextPageToken)
		if nextPageToken != "" {
			if nextPageToken == pageToken {
				return nil, s.fail(ctx, run.ID, "page_token_repeated", "TikTok Shop repeated an order page token")
			}
			if _, duplicate := seenTokens[nextPageToken]; duplicate {
				return nil, s.fail(ctx, run.ID, "page_token_repeated", "TikTok Shop repeated an order page token")
			}
			seenTokens[nextPageToken] = struct{}{}
		}
		orderIDs := make([]string, 0, len(response.Orders))
		for _, order := range response.Orders {
			orderID := strings.TrimSpace(order.ID)
			if !tikTokNumericIDPattern.MatchString(orderID) || order.UpdateTime < run.WindowStart.Unix() || order.UpdateTime >= run.WindowEnd.Unix() {
				return nil, s.fail(ctx, run.ID, "search_response_invalid", "TikTok Shop returned an order outside the reconciliation contract")
			}
			if _, duplicate := seenOrders[orderID]; duplicate {
				return nil, s.fail(ctx, run.ID, "duplicate_order", "TikTok Shop returned a duplicate order across pages")
			}
			seenOrders[orderID] = struct{}{}
			orderIDs = append(orderIDs, orderID)
		}
		if len(orderIDs) > 0 {
			snapshotResult, err := s.snapshots.Sync(ctx, TikTokOrderSnapshotRequest{ShopID: run.ShopID, OrderIDs: orderIDs})
			if err != nil || snapshotResult == nil || snapshotResult.SyncedCount != len(orderIDs) {
				return nil, s.fail(ctx, run.ID, "snapshot_failed", "TikTok Shop order detail snapshot failed")
			}
			progress.SnapshottedCount += snapshotResult.SyncedCount
		}
		progress.PageCount++
		progress.DiscoveredCount += len(orderIDs)
		progress.LastPageToken = nextPageToken
		progress.SearchRequestIDs = append(progress.SearchRequestIDs, strings.TrimSpace(response.UpstreamRequestID))
		if err := s.store.RecordPage(ctx, run.ID, progress); err != nil {
			return nil, s.fail(ctx, run.ID, "progress_persist_failed", "TikTok Shop reconciliation progress could not be saved")
		}
		if nextPageToken == "" {
			if err := s.store.CompleteRun(ctx, run.ID, progress); err != nil {
				return nil, s.fail(ctx, run.ID, "completion_persist_failed", "TikTok Shop reconciliation completion could not be saved")
			}
			return &TikTokOrderReconcileResult{
				RunID: run.ID, ShopID: run.ShopID, WindowStart: run.WindowStart, WindowEnd: run.WindowEnd,
				PageCount: progress.PageCount, DiscoveredCount: progress.DiscoveredCount,
				SnapshottedCount: progress.SnapshottedCount,
			}, nil
		}
		pageToken = nextPageToken
	}
	return nil, s.fail(ctx, run.ID, "page_limit_reached", "TikTok Shop reconciliation exceeded the bounded page limit")
}

func (s *TikTokOrderReconciler) fail(ctx context.Context, runID, code, message string) error {
	_ = s.store.FailRun(ctx, runID, code, message)
	return fmt.Errorf("%w: %s", ErrOrderReconcileFailed, code)
}

func validTikTokOrderReconcileRequest(input TikTokOrderReconcileRequest) bool {
	if !tikTokNumericIDPattern.MatchString(strings.TrimSpace(input.ShopID)) || input.UpdateTimeGE <= 0 || input.UpdateTimeLT <= input.UpdateTimeGE {
		return false
	}
	return time.Unix(input.UpdateTimeLT, 0).Sub(time.Unix(input.UpdateTimeGE, 0)) <= tikTokOrderReconcileMaxSpan
}

func (input TikTokOrderReconcileRequest) Validate() error {
	if !validTikTokOrderReconcileRequest(input) {
		return ErrInvalidOrderReconcileInput
	}
	return nil
}
