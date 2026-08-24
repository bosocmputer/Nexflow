package stockrecalc

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"nexflow/internal/config"
	"nexflow/internal/repository"
	"nexflow/internal/services/sml"
)

const (
	recalcLeaseDuration = 5 * time.Minute
	recalcBalanceChunk  = 500
)

type Worker struct {
	bills       *repository.BillRepo
	settings    *repository.AppSettingsRepo
	config      *config.Config
	stockClient *sml.StockSyncClient
	workerID    string
	log         *zap.Logger
}

func NewWorker(
	bills *repository.BillRepo,
	settings *repository.AppSettingsRepo,
	cfg *config.Config,
	stockClient *sml.StockSyncClient,
	log *zap.Logger,
) *Worker {
	instance := "nexflow"
	if cfg != nil && strings.TrimSpace(cfg.ShopeeGatewayTenant) != "" {
		instance = strings.TrimSpace(cfg.ShopeeGatewayTenant)
	}
	return &Worker{bills: bills, settings: settings, config: cfg, stockClient: stockClient,
		workerID: fmt.Sprintf("%s-stock-recalc-%d", instance, time.Now().UnixNano()), log: log}
}

func (w *Worker) Start(ctx context.Context) {
	if w == nil || w.config == nil || !w.config.MarketplaceReservationLedgerEnabled ||
		w.bills == nil || w.settings == nil || w.stockClient == nil || !w.stockClient.IsConfigured() {
		return
	}
	go func() {
		w.drain(ctx, 3)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.drain(ctx, 3)
			}
		}
	}()
}

func (w *Worker) drain(ctx context.Context, limit int) {
	for i := 0; i < limit; i++ {
		job, err := w.bills.ClaimStockRecalcJob(ctx, w.workerID, recalcLeaseDuration)
		if err != nil {
			w.warn("claim durable stock recalculation", err)
			return
		}
		if job == nil {
			return
		}
		runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		err = w.process(runCtx, job)
		cancel()
		if err != nil {
			if markErr := w.bills.FailStockRecalcJob(context.Background(), job.ID, w.workerID, err.Error()); markErr != nil {
				w.warn("record durable stock recalculation failure", markErr)
			}
			w.warn("durable stock recalculation", err)
		}
	}
}

func (w *Worker) process(ctx context.Context, job *repository.StockRecalcJob) error {
	demand, err := w.bills.StockRecalcDemand(ctx, job.ID)
	if err != nil {
		return fmt.Errorf("load reservation demand: %w", err)
	}
	if len(demand.ItemCodes) == 0 {
		return errors.New("no awaiting reservation demand for stock recalculation")
	}
	if strings.TrimSpace(demand.Warehouse) == "" || strings.TrimSpace(demand.Location) == "" {
		return errors.New("reservation stock scope is missing")
	}
	sort.Strings(demand.ItemCodes)
	if job.ProcessStockSucceededAt == nil {
		runtimeSettings, err := w.settings.SMLRuntimeSettings(w.config)
		if err != nil {
			return fmt.Errorf("load SML runtime settings: %w", err)
		}
		if strings.TrimSpace(runtimeSettings.StockRequestURL) == "" || strings.TrimSpace(runtimeSettings.Provider) == "" || strings.TrimSpace(runtimeSettings.Database) == "" {
			return errors.New("SML processstockrequest runtime is incomplete")
		}
		requestClient := sml.NewStockRequestClient(runtimeSettings.StockRequestURL, runtimeSettings.Provider, runtimeSettings.Database, w.log)
		if err := requestClient.ProcessStockRequest(ctx, demand.ItemCodes); err != nil {
			return fmt.Errorf("processstockrequest: %w", err)
		}
		if err := w.bills.MarkStockRecalcProcessed(ctx, job.ID, w.workerID); err != nil {
			return fmt.Errorf("persist processstockrequest success: %w", err)
		}
	}

	asOfDate := todayBangkok()
	scopeID := "recalc:" + job.ID
	for start := 0; start < len(demand.ItemCodes); start += recalcBalanceChunk {
		end := start + recalcBalanceChunk
		if end > len(demand.ItemCodes) {
			end = len(demand.ItemCodes)
		}
		chunk := demand.ItemCodes[start:end]
		response, err := w.stockClient.BalancesBatch(ctx, sml.StockBalanceBatchRequest{
			AsOfDate: asOfDate,
			Scopes: []sml.StockBalanceScopeRequest{{
				ScopeID: scopeID, ItemCodes: chunk, ScopeMode: "selected",
				Locations: []sml.StockLocationPair{{Warehouse: demand.Warehouse, Location: demand.Location}},
			}},
		})
		if err != nil {
			return fmt.Errorf("verify balance after processstockrequest: %w", err)
		}
		if err := verifyBalanceChunk(response, scopeID, chunk); err != nil {
			return err
		}
	}
	if err := w.bills.CompleteStockRecalcJob(ctx, job.ID, w.workerID); err != nil {
		return fmt.Errorf("release verified reservations: %w", err)
	}
	return nil
}

func verifyBalanceChunk(response *sml.StockBalanceBatchResponse, scopeID string, requested []string) error {
	if response == nil || len(response.Scopes) != 1 || response.Scopes[0].ScopeID != scopeID {
		return errors.New("SML balance verification returned a different scope")
	}
	seen := make(map[string]struct{}, len(response.Scopes[0].Items))
	for _, item := range response.Scopes[0].Items {
		seen[strings.TrimSpace(item.ItemCode)] = struct{}{}
	}
	for _, code := range requested {
		if _, ok := seen[strings.TrimSpace(code)]; !ok {
			return fmt.Errorf("SML balance verification missing item %s", code)
		}
	}
	return nil
}

func todayBangkok() string {
	location, err := time.LoadLocation("Asia/Bangkok")
	if err != nil {
		location = time.FixedZone("Asia/Bangkok", 7*60*60)
	}
	return time.Now().In(location).Format("2006-01-02")
}

func (w *Worker) warn(message string, err error) {
	if w.log != nil {
		w.log.Warn(message, zap.Error(err))
	}
}
