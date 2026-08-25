package catalog

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/itemcode"
)

// -------------------------------------------------------------------
// SMLCatalogService — deterministic SML sync and lookup
// -------------------------------------------------------------------

type SMLCatalogService struct {
	repo       *repository.SMLCatalogRepo
	smlBaseURL string
	smlHeaders map[string]string
	httpClient *http.Client
	logger     *zap.Logger
	// Background embed state
	embedRunning atomic.Int32
	embedMu      sync.RWMutex
	embedStatus  EmbedStatus
	// Background sync state. Full SML catalog sync can take minutes, so the
	// HTTP handler starts it asynchronously and reports progress via stats.
	syncRunning        atomic.Int32
	syncMu             sync.RWMutex
	syncStatus         SyncStatus
	unitCatalogClient  stockCatalogPager
	unitCatalogEnabled bool
	unitCatalogOwner   string
}

type SyncStatus struct {
	Running          bool       `json:"running"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
	Count            int        `json:"count"`
	UnitCatalogCount int        `json:"unit_catalog_count"`
	Error            string     `json:"error,omitempty"`
}

func (s *SMLCatalogService) WithUnitCatalog(client stockCatalogPager, enabled bool, instanceID string) *SMLCatalogService {
	s.unitCatalogClient = client
	s.unitCatalogEnabled = enabled
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		instanceID = "nexflow"
	}
	s.unitCatalogOwner = fmt.Sprintf("%s:%d", instanceID, time.Now().UnixNano())
	return s
}

type EmbedStatus struct {
	Running    bool       `json:"running"`
	SessionID  string     `json:"session_id,omitempty"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Total      int        `json:"total"`
	Done       int        `json:"done"`
	Errors     int        `json:"errors"`
	Error      string     `json:"error,omitempty"`
}

func NewSMLCatalogService(
	repo *repository.SMLCatalogRepo,
	smlBaseURL string,
	smlHeaders map[string]string,
	logger *zap.Logger,
) *SMLCatalogService {
	return &SMLCatalogService{
		repo:       repo,
		smlBaseURL: smlBaseURL,
		smlHeaders: smlHeaders,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		logger:     logger,
	}
}

// -------------------------------------------------------------------
// Sync from SML REST API (/product/v4)
// -------------------------------------------------------------------

// smlProductV4Response — best-effort struct; handles both page-based and array responses
type smlProductV4Response struct {
	// Some SML versions return top-level array "data"
	Data  json.RawMessage `json:"data"`
	Items json.RawMessage `json:"items"`
	// Pagination hints (may be absent)
	Total   *int  `json:"total"`
	HasMore *bool `json:"has_more"`
}

type smlProductItem struct {
	Code                 string            `json:"code"`
	Name                 string            `json:"name_1"`
	Name2                string            `json:"name_2"`
	Unit                 string            `json:"unit_standard"`
	GroupCode            string            `json:"group_main"`
	GroupCodeV1          string            `json:"group_code"`
	BalanceQty           float64           `json:"balance_qty"`
	ItemType             int               `json:"item_type"`
	SetDefinition        *smlSetDefinition `json:"set_definition"`
	SetComponents        []smlSetComponent `json:"set_components"`
	ImageCount           int               `json:"image_count"`
	PrimaryImageRoworder *int              `json:"primary_image_roworder"`
	PrimaryImageGuid     string            `json:"primary_image_guid"`
	PrimaryImageBytes    int64             `json:"primary_image_bytes"`
}

type smlSetDefinition struct {
	ComponentCount int               `json:"component_count"`
	DocumentValid  bool              `json:"document_valid"`
	StockValid     bool              `json:"stock_valid"`
	WarningCodes   []string          `json:"warning_codes"`
	Hash           string            `json:"hash"`
	Components     []smlSetComponent `json:"components"`
}

type smlSetComponent struct {
	LineNumber int     `json:"line_number"`
	RowOrder   int     `json:"row_order"`
	ItemCode   string  `json:"item_code"`
	ItemName   string  `json:"item_name"`
	ItemType   int     `json:"item_type"`
	UnitCode   string  `json:"unit_code"`
	Qty        float64 `json:"qty"`
	Price      float64 `json:"price"`
	SumAmount  float64 `json:"sum_amount"`
	PriceRatio float64 `json:"price_ratio"`
	UnitFactor float64 `json:"unit_factor"`
	Active     bool    `json:"active"`
	UnitValid  bool    `json:"unit_valid"`
}

// SyncFromAPI syncs catalog from SML /product/v4 endpoint.
// Returns (inserted+updated, error).
func (s *SMLCatalogService) SyncFromAPI() (int, error) {
	url := fmt.Sprintf("%s/api/v1/ic/products", s.smlBaseURL)
	total := 0
	page := 1
	runStartedAt := time.Now().UTC()
	upsertErrors := 0
	if s.unitCatalogEnabled {
		if err := s.repo.BeginCatalogReconciliation(context.Background()); err != nil {
			return 0, fmt.Errorf("pause marketplace stock for catalog reconciliation: %w", err)
		}
	}

	for {
		pageURL := fmt.Sprintf("%s?page=%d&size=200", url, page)
		req, err := http.NewRequest("GET", pageURL, nil)
		if err != nil {
			return total, fmt.Errorf("build request: %w", err)
		}
		for k, v := range s.smlHeaders {
			req.Header.Set(k, v)
		}

		resp, err := s.httpClient.Do(req)
		if err != nil {
			return total, fmt.Errorf("GET %s: %w", pageURL, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return total, fmt.Errorf("SML API %d: %s", resp.StatusCode, string(body))
		}

		items, _, maxPage, err := parseProductV4Response(body)
		if err != nil {
			return total, fmt.Errorf("parse page %d: %w", page, err)
		}

		for _, it := range items {
			unit := it.Unit
			groupCode := it.GroupCode
			if groupCode == "" {
				groupCode = it.GroupCodeV1
			}
			ci := models.CatalogItem{
				ItemCode:             it.Code,
				ItemName:             it.Name,
				ItemName2:            it.Name2,
				UnitCode:             unit,
				GroupCode:            groupCode,
				ItemType:             it.ItemType,
				SetDocumentValid:     it.ItemType != 3,
				SetStockValid:        it.ItemType != 3,
				ImageCount:           it.ImageCount,
				PrimaryImageRoworder: it.PrimaryImageRoworder,
				PrimaryImageGuid:     it.PrimaryImageGuid,
				ImageMetadataSynced:  true,
			}
			definition := it.SetDefinition
			if definition != nil {
				ci.SetComponentCount = definition.ComponentCount
				ci.SetDefinitionHash = definition.Hash
				ci.SetDocumentValid = definition.DocumentValid
				ci.SetStockValid = definition.StockValid
				ci.SetWarningCodes = append([]string(nil), definition.WarningCodes...)
				for _, component := range definition.Components {
					ci.SetComponents = append(ci.SetComponents, models.CatalogSetComponent{
						LineNumber: component.LineNumber, RowOrder: component.RowOrder,
						ItemCode: component.ItemCode, ItemName: component.ItemName,
						ItemType: component.ItemType, UnitCode: component.UnitCode,
						Qty: component.Qty, Price: component.Price, SumAmount: component.SumAmount,
						PriceRatio: component.PriceRatio, UnitFactor: component.UnitFactor,
						Active: component.Active, UnitValid: component.UnitValid,
					})
				}
			}
			if it.PrimaryImageBytes > 0 {
				bytes := it.PrimaryImageBytes
				ci.PrimaryImageBytes = &bytes
			}
			qty := it.BalanceQty
			ci.BalanceQty = &qty
			if err := s.repo.UpsertAt(ci, runStartedAt); err != nil {
				upsertErrors++
				s.logger.Warn("catalog: upsert failed",
					zap.String("code", it.Code), zap.Error(err))
			} else {
				total++
			}
		}

		if len(items) == 0 {
			break
		}
		if maxPage > 0 && page >= maxPage {
			break
		}
		page++
	}
	if upsertErrors > 0 {
		return total, fmt.Errorf("catalog sync stored %d products but %d products failed; inactive products were not finalized", total, upsertErrors)
	}
	if s.unitCatalogEnabled {
		if s.unitCatalogClient == nil {
			return total, fmt.Errorf("unit catalog sync is enabled but the SML stock catalog client is unavailable")
		}
		unitCount, err := runUnitCatalogGeneration(context.Background(), s.repo, s.unitCatalogClient, s.unitCatalogOwner, runStartedAt)
		if err != nil {
			return total, fmt.Errorf("sync SML unit generation: %w", err)
		}
		s.syncMu.Lock()
		s.syncStatus.UnitCatalogCount = unitCount
		s.syncMu.Unlock()
	} else if err := s.repo.FinalizeSuccessfulSync(runStartedAt); err != nil {
		return total, fmt.Errorf("finalize catalog sync: %w", err)
	}

	s.logger.Info("catalog: sync from API complete", zap.Int("count", total))
	return total, nil
}

func (s *SMLCatalogService) BeginSync() bool {
	if !s.syncRunning.CompareAndSwap(0, 1) {
		return false
	}
	now := time.Now()
	s.syncMu.Lock()
	s.syncStatus = SyncStatus{Running: true, StartedAt: &now}
	s.syncMu.Unlock()
	return true
}

func (s *SMLCatalogService) FinishSync(count int, err error) {
	now := time.Now()
	s.syncMu.Lock()
	s.syncStatus.Running = false
	s.syncStatus.FinishedAt = &now
	s.syncStatus.Count = count
	s.syncStatus.Error = ""
	if err != nil {
		s.syncStatus.Error = err.Error()
	}
	s.syncMu.Unlock()
	s.syncRunning.Store(0)
}

func (s *SMLCatalogService) IsSyncRunning() bool {
	return s.syncRunning.Load() == 1
}

func (s *SMLCatalogService) SyncStatus() SyncStatus {
	s.syncMu.RLock()
	defer s.syncMu.RUnlock()
	status := s.syncStatus
	status.Running = s.IsSyncRunning()
	return status
}

// singleProductV3Response is the shape of GET /v3/api/product/{code}.
// Different from the /product/v4 list shape used by SyncFromAPI — the single
// endpoint uses "name" instead of "name_1" and doesn't include prices.
type singleProductV3Response struct {
	Success bool `json:"success"`
	Data    struct {
		Code          string            `json:"code"`
		Name          string            `json:"name"`
		Name1         string            `json:"name_1"`
		Name2         string            `json:"name_2"`
		UnitStandard  string            `json:"unit_standard"`
		GroupMain     string            `json:"group_main"`
		GroupCode     string            `json:"group_code"`
		BalanceQty    float64           `json:"balance_qty"`
		ImageCount    int               `json:"image_count"`
		ImageRow      *int              `json:"primary_image_roworder"`
		ImageGuid     string            `json:"primary_image_guid"`
		ImageBytes    int64             `json:"primary_image_bytes"`
		ItemType      int               `json:"item_type"`
		SetDefinition *smlSetDefinition `json:"set_definition"`
		SetComponents []smlSetComponent `json:"set_components"`
		Units         []struct {
			UnitCode string `json:"unit_code"`
		} `json:"units"`
	} `json:"data"`
}

// RefreshOne re-fetches a single product from SML 248 and upserts it into
// sml_catalog. Used by the per-row "รีเฟรช" button on /settings/catalog.
//
// Why not reuse SyncFromAPI: that endpoint pages through the entire SML
// catalog (~minutes for thousands of items). This shortcut takes one HTTP
// round-trip and only refreshes the fields that are likely to drift after
// an SML-side rename: name, unit, group, balance_qty.
//
// Product-level price is intentionally neither read nor written. The legacy
// database column remains untouched only for rollback compatibility.
//
// Returns:
//   - nil with `notFound = true` when SML returned 404 (caller should tell
//     the user the product no longer exists in SML and offer Delete).
//   - the upserted item otherwise.
func (s *SMLCatalogService) RefreshOne(itemCode string) (item *models.CatalogItem, notFound bool, err error) {
	return s.RefreshOneContext(context.Background(), itemCode)
}

// RefreshOneContext is the bounded read-through used by marketplace imports.
// The caller owns the deadline so one slow SML lookup cannot stall a batch.
func (s *SMLCatalogService) RefreshOneContext(ctx context.Context, itemCode string) (item *models.CatalogItem, notFound bool, err error) {
	endpoint := fmt.Sprintf("%s/api/v1/ic/products/%s", s.smlBaseURL, url.PathEscape(itemCode))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, false, err
	}
	for k, v := range s.smlHeaders {
		req.Header.Set(k, v)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("catalog read-through: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return nil, true, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("SML API %d: %s", resp.StatusCode, string(body))
	}
	var r singleProductV3Response
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, false, fmt.Errorf("parse: %w — body: %s", err, string(body))
	}
	// SML 248 returns either:
	//   - 200 {"success":false}  (some versions)
	//   - 200 {"success":true, "data":null}  (current — what 192.168.2.248 returns)
	//   - 200 {"success":true, "data":{"code":"", ...}}  (defensive)
	// All three mean "no such product" → caller should offer Delete instead.
	if !r.Success || r.Data.Code == "" {
		return nil, true, nil
	}
	d := r.Data
	unit := d.UnitStandard
	if unit == "" && len(d.Units) > 0 {
		unit = d.Units[0].UnitCode
	}
	name := d.Name
	if name == "" {
		name = d.Name1
	}
	ci := models.CatalogItem{
		ItemCode:             d.Code,
		ItemName:             name,
		ItemName2:            d.Name2,
		UnitCode:             unit,
		GroupCode:            firstNonEmpty(d.GroupMain, d.GroupCode),
		ImageCount:           d.ImageCount,
		PrimaryImageRoworder: d.ImageRow,
		PrimaryImageGuid:     d.ImageGuid,
		ImageMetadataSynced:  true,
		ItemType:             d.ItemType,
		SetDocumentValid:     d.ItemType != 3,
		SetStockValid:        d.ItemType != 3,
	}
	definition := d.SetDefinition
	if definition != nil {
		ci.SetComponentCount = definition.ComponentCount
		ci.SetDefinitionHash = definition.Hash
		ci.SetDocumentValid = definition.DocumentValid
		ci.SetStockValid = definition.StockValid
		ci.SetWarningCodes = append([]string(nil), definition.WarningCodes...)
		ci.SetComponents = catalogSetComponents(definition.Components)
	} else if len(d.SetComponents) > 0 {
		ci.SetComponentCount = len(d.SetComponents)
		ci.SetComponents = catalogSetComponents(d.SetComponents)
	}
	if d.ImageBytes > 0 {
		bytes := d.ImageBytes
		ci.PrimaryImageBytes = &bytes
	}
	bq := d.BalanceQty
	ci.BalanceQty = &bq
	if err := s.repo.Upsert(ci); err != nil {
		return nil, false, fmt.Errorf("upsert: %w", err)
	}
	// Re-fetch so the caller sees the canonical row and timestamps.
	out, err := s.repo.GetOne(itemCode)
	if err != nil {
		return nil, false, fmt.Errorf("readback: %w", err)
	}
	return out, false, nil
}

// parseProductV4Response handles several possible SML API response shapes
func parseProductV4Response(body []byte) (items []smlProductItem, currentPage, maxPage int, err error) {
	// Try array directly first
	var asArray []smlProductItem
	if jsonErr := json.Unmarshal(body, &asArray); jsonErr == nil {
		return asArray, 0, 0, nil
	}

	// Try wrapped response with SML pages object
	var wrapped struct {
		Data  []smlProductItem `json:"data"`
		Items []smlProductItem `json:"items"`
		Meta  *struct {
			Total int `json:"total"`
			Page  int `json:"page"`
			Size  int `json:"size"`
		} `json:"meta"`
		Pages *struct {
			Page    int `json:"page"`
			MaxPage int `json:"max_page"`
		} `json:"pages"`
	}
	if jsonErr := json.Unmarshal(body, &wrapped); jsonErr != nil {
		return nil, 0, 0, fmt.Errorf("parse response: %w", jsonErr)
	}
	items = wrapped.Data
	if len(items) == 0 {
		items = wrapped.Items
	}
	if wrapped.Pages != nil {
		currentPage = wrapped.Pages.Page
		maxPage = wrapped.Pages.MaxPage
	}
	if wrapped.Meta != nil {
		currentPage = wrapped.Meta.Page
		if wrapped.Meta.Size > 0 {
			maxPage = (wrapped.Meta.Total + wrapped.Meta.Size - 1) / wrapped.Meta.Size
		}
	}
	return items, currentPage, maxPage, nil
}

// -------------------------------------------------------------------
// Sync from CSV upload
// -------------------------------------------------------------------

// SyncFromCSV parses a CSV file (UTF-8) and upserts all rows.
// Expected header (case-insensitive, flexible):
//
//	item_code, item_name, [item_name2], [unit_code], [wh_code], [shelf_code], [group_code]
func (s *SMLCatalogService) SyncFromCSV(data []byte) (int, error) {
	// Strip BOM if present (Excel CSV often has BOM)
	content := data
	if len(content) >= 3 && content[0] == 0xEF && content[1] == 0xBB && content[2] == 0xBF {
		content = content[3:]
	}

	r := csv.NewReader(bytes.NewReader(content))
	r.LazyQuotes = true
	r.TrimLeadingSpace = true

	records, err := r.ReadAll()
	if err != nil {
		return 0, fmt.Errorf("parse CSV: %w", err)
	}
	if len(records) < 2 {
		return 0, fmt.Errorf("CSV must have header + at least one row")
	}

	// Parse header to build column index map
	colIdx := map[string]int{}
	for i, h := range records[0] {
		colIdx[normalizeHeader(h)] = i
	}

	required := []string{"item_code", "item_name"}
	for _, req := range required {
		if _, ok := colIdx[req]; !ok {
			return 0, fmt.Errorf("missing required column: %s (found: %v)", req, records[0])
		}
	}

	count := 0
	for rowNum, row := range records[1:] {
		get := func(key string) string {
			idx, ok := colIdx[key]
			if !ok || idx >= len(row) {
				return ""
			}
			return strings.TrimSpace(row[idx])
		}

		code := get("item_code")
		name := get("item_name")
		if code == "" || name == "" {
			s.logger.Debug("catalog: skip empty row", zap.Int("row", rowNum+2))
			continue
		}

		ci := models.CatalogItem{
			ItemCode:         code,
			ItemName:         name,
			ItemName2:        get("item_name2"),
			UnitCode:         get("unit_code"),
			WHCode:           get("wh_code"),
			ShelfCode:        get("shelf_code"),
			GroupCode:        get("group_code"),
			SetDocumentValid: true,
			SetStockValid:    true,
		}
		if qtyStr := get("balance_qty"); qtyStr != "" {
			if q, err := strconv.ParseFloat(qtyStr, 64); err == nil {
				ci.BalanceQty = &q
			}
		}

		if err := s.repo.Upsert(ci); err != nil {
			s.logger.Warn("catalog: CSV upsert failed",
				zap.String("code", code), zap.Error(err))
		} else {
			count++
		}
	}

	s.logger.Info("catalog: CSV import complete", zap.Int("count", count))
	return count, nil
}

func catalogSetComponents(components []smlSetComponent) []models.CatalogSetComponent {
	result := make([]models.CatalogSetComponent, 0, len(components))
	for _, component := range components {
		result = append(result, models.CatalogSetComponent{
			LineNumber: component.LineNumber, RowOrder: component.RowOrder,
			ItemCode: component.ItemCode, ItemName: component.ItemName,
			ItemType: component.ItemType, UnitCode: component.UnitCode,
			Qty: component.Qty, Price: component.Price, SumAmount: component.SumAmount,
			PriceRatio: component.PriceRatio, UnitFactor: component.UnitFactor,
			Active: component.Active, UnitValid: component.UnitValid,
		})
	}
	return result
}

func normalizeHeader(h string) string {
	// lowercase + replace spaces/hyphens with underscore
	h = strings.ToLower(strings.TrimSpace(h))
	h = strings.ReplaceAll(h, " ", "_")
	h = strings.ReplaceAll(h, "-", "_")
	// Map common aliases
	switch h {
	case "code", "sku", "รหัสสินค้า":
		return "item_code"
	case "name", "product_name", "ชื่อสินค้า":
		return "item_name"
	case "name2", "ชื่อสินค้า2":
		return "item_name2"
	case "unit", "หน่วย":
		return "unit_code"
	case "wh", "warehouse", "คลัง":
		return "wh_code"
	case "shelf", "ชั้น":
		return "shelf_code"
	}
	return h
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// -------------------------------------------------------------------
// Embed operations
// -------------------------------------------------------------------

// EmbedProduct generates and stores embedding for a single item
func (s *SMLCatalogService) EmbedProduct(embSvc *EmbeddingService, itemCode string) error {
	return s.embedProductWithSession(embSvc, itemCode, newOpenRouterSessionID("catalog-embed-single", itemCode))
}

func (s *SMLCatalogService) embedProductWithSession(embSvc *EmbeddingService, itemCode, sessionID string) error {
	item, err := s.repo.GetOne(itemCode)
	if err != nil || item == nil {
		return fmt.Errorf("item not found: %s", itemCode)
	}

	text := item.ItemName
	if item.ItemName2 != "" {
		text += " " + item.ItemName2
	}

	emb, err := embSvc.EmbedTextWithSession(text, sessionID)
	if err != nil {
		_ = s.repo.SetEmbeddingError(itemCode)
		return fmt.Errorf("embed %s: %w", itemCode, err)
	}

	return s.repo.SetEmbedding(itemCode, emb, EmbeddingModel)
}

// EmbedAllPending runs background embedding for all pending items.
// Returns (done, errors).
func (s *SMLCatalogService) EmbedAllPending(embSvc *EmbeddingService) (int, int, error) {
	if !s.embedRunning.CompareAndSwap(0, 1) {
		return 0, 0, fmt.Errorf("embedding already running")
	}
	defer s.embedRunning.Store(0)

	done, errs := 0, 0
	sessionID := newOpenRouterSessionID("catalog-embed-all", "pending")
	startedAt := time.Now()
	total, _ := s.repo.CountPending()
	s.setEmbedStatus(EmbedStatus{
		Running:   true,
		SessionID: sessionID,
		StartedAt: &startedAt,
		Total:     total,
	})
	defer func() {
		finishedAt := time.Now()
		status := s.EmbedStatus()
		status.Running = false
		status.FinishedAt = &finishedAt
		status.Done = done
		status.Errors = errs
		s.setEmbedStatus(status)
	}()
	s.logger.Info("catalog: embed-all session started", zap.String("session_id", sessionID), zap.Int("total", total))
	for {
		batch, err := s.repo.GetPendingBatch(50)
		if err != nil {
			status := s.EmbedStatus()
			status.Error = err.Error()
			status.Done = done
			status.Errors = errs
			s.setEmbedStatus(status)
			return done, errs, err
		}
		if len(batch) == 0 {
			break
		}
		for _, item := range batch {
			if err := s.embedProductWithSession(embSvc, item.ItemCode, sessionID); err != nil {
				s.logger.Warn("catalog: embed error", zap.String("code", item.ItemCode), zap.Error(err))
				errs++
			} else {
				done++
			}
			status := s.EmbedStatus()
			status.Done = done
			status.Errors = errs
			s.setEmbedStatus(status)
			time.Sleep(50 * time.Millisecond) // small pause to avoid bursting OpenRouter
		}
	}
	s.logger.Info("catalog: embed all complete", zap.Int("done", done), zap.Int("errors", errs))
	return done, errs, nil
}

// IsEmbedRunning returns true if background embedding is in progress
func (s *SMLCatalogService) IsEmbedRunning() bool {
	return s.embedRunning.Load() == 1
}

func (s *SMLCatalogService) EmbedStatus() EmbedStatus {
	s.embedMu.RLock()
	defer s.embedMu.RUnlock()
	return s.embedStatus
}

func (s *SMLCatalogService) setEmbedStatus(status EmbedStatus) {
	s.embedMu.Lock()
	defer s.embedMu.Unlock()
	s.embedStatus = status
}

// -------------------------------------------------------------------
// Similarity Search (text-based Levenshtein fallback if no embedding)
// -------------------------------------------------------------------

// SearchByText does fuzzy text search using Levenshtein distance
// (used as fallback when embedding is unavailable or catalog is not embedded)
func (s *SMLCatalogService) SearchByText(query string, topK int) ([]models.CatalogMatch, error) {
	allItems, err := s.repo.ListAllNames()
	if err != nil {
		return nil, err
	}

	queryLower := strings.ToLower(query)
	type scored struct {
		item  models.CatalogItem
		score float64
	}
	results := make([]scored, 0, len(allItems))
	for _, it := range allItems {
		results = append(results, scored{it, catalogTextScore(queryLower, it)})
	}

	// Sort descending
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].score > results[j-1].score; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}

	n := topK
	if n > len(results) {
		n = len(results)
	}
	matches := make([]models.CatalogMatch, 0, n)
	for i := 0; i < n; i++ {
		it := results[i].item
		codeMeta := itemcode.Inspect(it.ItemCode)
		matches = append(matches, models.CatalogMatch{
			ItemCode:             it.ItemCode,
			ItemName:             it.ItemName,
			ItemName2:            it.ItemName2,
			UnitCode:             it.UnitCode,
			WHCode:               it.WHCode,
			ShelfCode:            it.ShelfCode,
			ImageCount:           it.ImageCount,
			PrimaryImageRoworder: it.PrimaryImageRoworder,
			PrimaryImageGuid:     it.PrimaryImageGuid,
			PrimaryImageBytes:    it.PrimaryImageBytes,
			HasHiddenChars:       codeMeta.HasHiddenChars,
			CleanItemCode:        codeMeta.CleanItemCode,
			HiddenCharKinds:      codeMeta.Kinds,
			Score:                results[i].score,
		})
	}
	return matches, nil
}

func catalogTextScore(queryLower string, it models.CatalogItem) float64 {
	codeLower := strings.ToLower(it.ItemCode)
	score := textSimilarity(queryLower, strings.ToLower(it.ItemCode+" "+it.ItemName+" "+it.ItemName2))
	switch {
	case codeLower == queryLower:
		return 1
	case strings.HasPrefix(codeLower, queryLower):
		return maxFloat64(score, 0.98)
	case strings.Contains(codeLower, queryLower):
		return maxFloat64(score, 0.95)
	default:
		return score
	}
}

// textSimilarity returns a 0–1 score using token overlap + substring check
func textSimilarity(a, b string) float64 {
	if a == b {
		return 1.0
	}
	if strings.Contains(b, a) {
		return 0.9
	}
	if strings.Contains(a, b) {
		return 0.85
	}
	// Token-level Jaccard
	aTok := tokenize(a)
	bTok := tokenize(b)
	if len(aTok) == 0 || len(bTok) == 0 {
		return 0
	}
	inter := 0
	for _, t := range aTok {
		for _, s := range bTok {
			if t == s {
				inter++
				break
			}
		}
	}
	union := len(aTok) + len(bTok) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func maxFloat64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func tokenize(s string) []string {
	var tokens []string
	for _, t := range strings.Fields(s) {
		t = strings.Trim(t, ".,;:!?")
		if len(t) >= 2 {
			tokens = append(tokens, t)
		}
	}
	return tokens
}
