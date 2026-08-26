package stockrecalc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
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
	recalcEvidenceChunk = 500
)

type Worker struct {
	bills       *repository.BillRepo
	settings    *repository.AppSettingsRepo
	config      *config.Config
	stockClient *sml.StockSyncClient
	workerID    string
	log         *zap.Logger
}

type stockDemandEvidenceGroup struct {
	Request sml.StockDemandEvidenceRequestLine
	Members []repository.StockRecalcDemandLine
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
	evidenceGroups, err := groupDemandEvidence(demand.Lines)
	if err != nil {
		return err
	}
	verifiedEvidence := make([]repository.VerifiedStockEvidence, 0, len(demand.Lines))
	for start := 0; start < len(evidenceGroups); start += recalcEvidenceChunk {
		end := start + recalcEvidenceChunk
		if end > len(evidenceGroups) {
			end = len(evidenceGroups)
		}
		request := sml.StockDemandEvidenceBatchRequest{Lines: make([]sml.StockDemandEvidenceRequestLine, 0, end-start)}
		for _, group := range evidenceGroups[start:end] {
			request.Lines = append(request.Lines, group.Request)
		}
		response, err := w.stockClient.DemandEvidenceBatch(ctx, request)
		if err != nil {
			return fmt.Errorf("verify SML document demand evidence: %w", err)
		}
		verified, err := verifyDemandEvidence(
			evidenceGroups[start:end], response,
			w.config.SMLStockSourceFingerprint, time.Now(),
		)
		if err != nil {
			return err
		}
		verifiedEvidence = append(verifiedEvidence, verified...)
	}
	if err := w.bills.CompleteStockRecalcJob(ctx, job.ID, w.workerID, verifiedEvidence); err != nil {
		return fmt.Errorf("release verified reservations: %w", err)
	}
	return nil
}

func verifyDemandEvidence(
	groups []stockDemandEvidenceGroup,
	response *sml.StockDemandEvidenceBatchResponse,
	expectedFingerprint string,
	now time.Time,
) ([]repository.VerifiedStockEvidence, error) {
	expectedFingerprint = strings.TrimSpace(expectedFingerprint)
	if expectedFingerprint == "" || response == nil || response.SchemaVersion != "stock-demand-evidence-v1" || response.SourceSemanticsFingerprint != expectedFingerprint {
		return nil, errors.New("SML document evidence capability or fingerprint is not approved")
	}
	snapshot, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(response.SourceSnapshotAt))
	if err != nil || snapshot.After(now.Add(time.Minute)) || now.Sub(snapshot) > 5*time.Minute {
		return nil, errors.New("SML document evidence snapshot is invalid or stale")
	}
	expected := make(map[string]stockDemandEvidenceGroup, len(groups))
	for _, group := range groups {
		if _, duplicate := expected[group.Request.EvidenceID]; duplicate {
			return nil, fmt.Errorf("duplicate expected evidence identity %s", group.Request.EvidenceID)
		}
		expected[group.Request.EvidenceID] = group
	}
	verified := make([]repository.VerifiedStockEvidence, 0)
	seen := make(map[string]struct{}, len(response.Lines))
	for _, actual := range response.Lines {
		group, ok := expected[actual.EvidenceID]
		if !ok {
			return nil, fmt.Errorf("SML returned unexpected evidence identity %s", actual.EvidenceID)
		}
		if _, duplicate := seen[actual.EvidenceID]; duplicate {
			return nil, fmt.Errorf("SML returned duplicate evidence identity %s", actual.EvidenceID)
		}
		seen[actual.EvidenceID] = struct{}{}
		if actual.Status != "verified" || strings.TrimSpace(actual.EvidenceHash) == "" ||
			actual.DocNo != group.Request.DocNo || actual.Route != group.Request.Route || actual.ItemCode != group.Request.ItemCode ||
			actual.WarehouseCode != group.Request.WarehouseCode || actual.LocationCode != group.Request.LocationCode {
			return nil, fmt.Errorf("SML document evidence %s is not verified: %s", actual.EvidenceID, actual.Reason)
		}
		expectedQty, expectedOK := new(big.Rat).SetString(group.Request.ExpectedBaseQtyExact)
		actualExpectedQty, actualExpectedOK := new(big.Rat).SetString(actual.ExpectedBaseQtyExact)
		actualQty, actualOK := new(big.Rat).SetString(actual.ActualBaseQtyExact)
		if !expectedOK || !actualExpectedOK || !actualOK || expectedQty.Cmp(actualExpectedQty) != 0 || expectedQty.Cmp(actualQty) != 0 {
			return nil, fmt.Errorf("SML document evidence %s quantity does not match", actual.EvidenceID)
		}
		for _, member := range group.Members {
			verified = append(verified, repository.VerifiedStockEvidence{
				StockRecalcDemandLine: member, EvidenceGroupID: group.Request.EvidenceID,
				DocumentScopeExpectedBaseQtyExact: group.Request.ExpectedBaseQtyExact,
				ActualBaseQtyExact:                actual.ActualBaseQtyExact, SourceFingerprint: expectedFingerprint,
				EvidenceHash: actual.EvidenceHash, VerifiedSourceSnapshotAt: snapshot,
			})
		}
	}
	if len(seen) != len(expected) {
		return nil, errors.New("SML document evidence response is incomplete")
	}
	return verified, nil
}

func groupDemandEvidence(lines []repository.StockRecalcDemandLine) ([]stockDemandEvidenceGroup, error) {
	type accumulator struct {
		request sml.StockDemandEvidenceRequestLine
		total   *big.Rat
		members []repository.StockRecalcDemandLine
	}
	values := map[string]*accumulator{}
	for _, line := range lines {
		quantity, ok := new(big.Rat).SetString(strings.TrimSpace(line.ExpectedBaseQtyExact))
		if !ok || quantity.Sign() <= 0 {
			return nil, fmt.Errorf("invalid exact reservation demand for %s", line.EvidenceID)
		}
		key := strings.Join([]string{line.SMLAttemptID, line.DocNo, line.Route, line.ItemCode, line.Warehouse, line.Location}, "\x1f")
		group := values[key]
		if group == nil {
			hash := sha256.Sum256([]byte(key))
			group = &accumulator{request: sml.StockDemandEvidenceRequestLine{
				EvidenceID: "demand:" + hex.EncodeToString(hash[:]), DocNo: line.DocNo, Route: line.Route,
				ItemCode: line.ItemCode, WarehouseCode: line.Warehouse, LocationCode: line.Location,
			}, total: new(big.Rat)}
			values[key] = group
		}
		group.total.Add(group.total, quantity)
		group.members = append(group.members, line)
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	groups := make([]stockDemandEvidenceGroup, 0, len(keys))
	for _, key := range keys {
		value := values[key]
		value.request.ExpectedBaseQtyExact = exactDecimalString(value.total)
		groups = append(groups, stockDemandEvidenceGroup{Request: value.request, Members: value.members})
	}
	return groups, nil
}

func exactDecimalString(value *big.Rat) string {
	if value == nil || value.Sign() == 0 {
		return "0"
	}
	denominator := new(big.Int).Set(value.Denom())
	remainder := new(big.Int)
	twos, fives := 0, 0
	for {
		quotient := new(big.Int)
		quotient.QuoRem(denominator, big.NewInt(2), remainder)
		if remainder.Sign() != 0 {
			break
		}
		denominator = quotient
		twos++
	}
	for {
		quotient := new(big.Int)
		quotient.QuoRem(denominator, big.NewInt(5), remainder)
		if remainder.Sign() != 0 {
			break
		}
		denominator = quotient
		fives++
	}
	scale := twos
	if fives > scale {
		scale = fives
	}
	if denominator.Cmp(big.NewInt(1)) != 0 {
		scale = 18
	}
	formatted := value.FloatString(scale)
	if !strings.Contains(formatted, ".") {
		return formatted
	}
	return strings.TrimRight(strings.TrimRight(formatted, "0"), ".")
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
