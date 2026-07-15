package shopeegateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"nexflow/internal/services/gatewayauth"
	"nexflow/internal/services/shopeeapi"
)

const maxTenantShopRoutes = 1000

type RouteSyncStore interface {
	ActiveTenants(ctx context.Context) ([]Tenant, error)
	SyncTenantRoutes(ctx context.Context, tenantID string, shopIDs []int64) error
}

type RouteSyncResult struct {
	Tenants int
	Synced  int
	Failed  int
	Routes  int
}

type RouteSyncWorker struct {
	store     RouteSyncStore
	masterKey string
	client    *http.Client
	logger    *zap.Logger
	now       func() time.Time
}

func NewRouteSyncWorker(cfg Config, store RouteSyncStore, logger *zap.Logger) *RouteSyncWorker {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &RouteSyncWorker{
		store:     store,
		masterKey: cfg.InternalMasterKey,
		client:    &http.Client{Timeout: cfg.TenantHTTPTimeout},
		logger:    logger,
		now:       time.Now,
	}
}

func (w *RouteSyncWorker) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	w.syncAndLog(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.syncAndLog(ctx)
		}
	}
}

func (w *RouteSyncWorker) SyncOnce(ctx context.Context) (RouteSyncResult, error) {
	if w == nil || w.store == nil {
		return RouteSyncResult{}, errors.New("route sync worker is not configured")
	}
	tenants, err := w.store.ActiveTenants(ctx)
	if err != nil {
		return RouteSyncResult{}, err
	}
	result := RouteSyncResult{Tenants: len(tenants)}
	errorsByTenant := make([]error, 0)
	for _, tenant := range tenants {
		shopIDs, err := w.fetchTenantRoutes(ctx, tenant)
		if err != nil {
			result.Failed++
			errorsByTenant = append(errorsByTenant, fmt.Errorf("%s: %w", tenant.Slug, err))
			continue
		}
		if err := w.store.SyncTenantRoutes(ctx, tenant.ID, shopIDs); err != nil {
			result.Failed++
			errorsByTenant = append(errorsByTenant, fmt.Errorf("%s: %w", tenant.Slug, err))
			continue
		}
		result.Synced++
		result.Routes += len(shopIDs)
	}
	return result, errors.Join(errorsByTenant...)
}

func (w *RouteSyncWorker) syncAndLog(ctx context.Context) {
	startedAt := w.now()
	result, err := w.SyncOnce(ctx)
	fields := []zap.Field{
		zap.Int("tenant_count", result.Tenants),
		zap.Int("tenant_synced", result.Synced),
		zap.Int("tenant_failed", result.Failed),
		zap.Int("route_count", result.Routes),
		zap.Int64("duration_ms", w.now().Sub(startedAt).Milliseconds()),
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		w.logger.Warn("shopee_gateway_route_sync_failed", append(fields, zap.Error(err))...)
		return
	}
	w.logger.Debug("shopee_gateway_route_sync_complete", fields...)
}

func (w *RouteSyncWorker) fetchTenantRoutes(ctx context.Context, tenant Tenant) ([]int64, error) {
	secret, err := DeriveTenantSecret(w.masterKey, tenant.Slug)
	if err != nil {
		return nil, err
	}
	body := []byte(`{}`)
	endpoint := strings.TrimRight(tenant.BackendURL, "/") + shopeeapi.GatewayTenantRoutesPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", randomHex(12))
	if err := gatewayauth.Apply(req, tenant.Slug, secret, body, w.now(), randomHex(18)); err != nil {
		return nil, err
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 128<<10))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tenant route endpoint returned HTTP %d", resp.StatusCode)
	}
	var envelope struct {
		Data *struct {
			ShopIDs *[]int64 `json:"shop_ids"`
		} `json:"data"`
	}
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return nil, errors.New("tenant route response is invalid")
	}
	if envelope.Data == nil || envelope.Data.ShopIDs == nil {
		return nil, errors.New("tenant route response is missing shop_ids")
	}
	routes := *envelope.Data.ShopIDs
	if len(routes) > maxTenantShopRoutes {
		return nil, errors.New("tenant route response exceeds the shop limit")
	}
	shopIDs := make([]int64, 0, len(routes))
	seen := make(map[int64]struct{}, len(routes))
	for _, shopID := range routes {
		if shopID <= 0 {
			return nil, errors.New("tenant route response contains an invalid shop ID")
		}
		if _, exists := seen[shopID]; exists {
			continue
		}
		seen[shopID] = struct{}{}
		shopIDs = append(shopIDs, shopID)
	}
	return shopIDs, nil
}
