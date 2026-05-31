package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/itemcode"
)

const EmbeddingModel = "openai/text-embedding-3-small"
const embeddingAPIURL = "https://openrouter.ai/api/v1/embeddings"

// -------------------------------------------------------------------
// Types
// -------------------------------------------------------------------

type CatalogIndex struct {
	mu    sync.RWMutex
	items []indexedItem
}

type indexedItem struct {
	models.CatalogMatch
	embedding []float64
}

// -------------------------------------------------------------------
// Embedding Service — uses OpenRouter (openai/text-embedding-3-small)
// -------------------------------------------------------------------

type EmbeddingService struct {
	apiKey      string
	appTitle    string
	appReferer  string
	client      *http.Client
	usageLogger interface {
		Log(models.AIUsageEntry) error
	}
}

func NewEmbeddingService(apiKey string) *EmbeddingService {
	return &EmbeddingService{
		apiKey: apiKey,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (e *EmbeddingService) WithUsageLogger(logger interface {
	Log(models.AIUsageEntry) error
}) *EmbeddingService {
	e.usageLogger = logger
	return e
}

func (e *EmbeddingService) WithAppAttribution(title, referer string) *EmbeddingService {
	e.appTitle = strings.TrimSpace(title)
	e.appReferer = strings.TrimSpace(referer)
	return e
}

func (e *EmbeddingService) IsConfigured() bool {
	return e.apiKey != ""
}

// EmbedText calls OpenRouter text-embedding-3-small and returns a 1536-dim vector.
func (e *EmbeddingService) EmbedText(text string) ([]float64, error) {
	return e.EmbedTextWithSession(text, "")
}

func (e *EmbeddingService) EmbedTextWithSession(text, sessionID string) ([]float64, error) {
	if !e.IsConfigured() {
		return nil, fmt.Errorf("OPENROUTER_API_KEY not configured")
	}
	start := time.Now()
	if sessionID == "" {
		sessionID = newOpenRouterSessionID("catalog-embed", "embed-text")
	}

	reqBody, _ := json.Marshal(map[string]interface{}{
		"model":      EmbeddingModel,
		"input":      text,
		"session_id": sessionID,
		"user":       "nexflow-backend",
		"trace": map[string]interface{}{
			"trace_id":        sessionID,
			"trace_name":      "Nexflow catalog embedding",
			"span_name":       "embed_text",
			"generation_name": "catalog_embed:embed_text",
			"environment":     "production-main",
			"feature":         "catalog_embed",
		},
	})

	req, err := http.NewRequest(http.MethodPost, embeddingAPIURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	req.Header.Set("Content-Type", "application/json")
	e.setOpenRouterHeaders(req)

	resp, err := e.client.Do(req)
	if err != nil {
		e.logUsage(models.AIUsageEntry{
			Provider: "openrouter", Model: EmbeddingModel, Feature: "catalog_embed",
			Operation: "embed_text", Status: "error", Error: err.Error(),
			DurationMs: intPtr(int(time.Since(start).Milliseconds())),
			Metadata:   map[string]interface{}{"session_id": sessionID},
		})
		return nil, fmt.Errorf("openrouter embedding call: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		e.logUsage(models.AIUsageEntry{
			Provider: "openrouter", Model: EmbeddingModel, Feature: "catalog_embed",
			Operation: "embed_text", Status: "error", Error: fmt.Sprintf("status %d", resp.StatusCode),
			DurationMs: intPtr(int(time.Since(start).Milliseconds())),
			Metadata:   map[string]interface{}{"session_id": sessionID},
		})
		return nil, fmt.Errorf("openrouter embedding %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
		Usage *struct {
			PromptTokens int `json:"prompt_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage,omitempty"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal embedding response: %w", err)
	}
	if len(result.Data) == 0 || len(result.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("empty embedding in response")
	}
	inputTokens := estimateTokens(text)
	totalTokens := inputTokens
	if result.Usage != nil {
		if result.Usage.PromptTokens > 0 {
			inputTokens = result.Usage.PromptTokens
		}
		if result.Usage.TotalTokens > 0 {
			totalTokens = result.Usage.TotalTokens
		}
	}
	e.logUsage(models.AIUsageEntry{
		Provider: "openrouter", Model: EmbeddingModel, Feature: "catalog_embed",
		Operation: "embed_text", Status: "success", InputTokens: inputTokens,
		TotalTokens: totalTokens, EstimatedCostUSD: estimateEmbeddingCostUSD(inputTokens),
		DurationMs: intPtr(int(time.Since(start).Milliseconds())),
		Metadata:   map[string]interface{}{"session_id": sessionID},
	})
	return result.Data[0].Embedding, nil
}

func (e *EmbeddingService) setOpenRouterHeaders(req *http.Request) {
	title := e.appTitle
	if title == "" {
		title = "Nexflow"
	}
	req.Header.Set("X-OpenRouter-Title", title)
	req.Header.Set("X-Title", title)
	if e.appReferer != "" {
		req.Header.Set("HTTP-Referer", e.appReferer)
	}
}

func newOpenRouterSessionID(feature, operation string) string {
	return fmt.Sprintf("nexflow:%s:%s:%s", safeTracePart(feature), safeTracePart(operation), time.Now().UTC().Format("20060102T150405.000000000Z"))
}

func safeTracePart(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == ':':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func (e *EmbeddingService) logUsage(entry models.AIUsageEntry) {
	if e.usageLogger == nil {
		return
	}
	_ = e.usageLogger.Log(entry)
}

func estimateTokens(text string) int {
	if len(text) == 0 {
		return 0
	}
	return (len(text) + 3) / 4
}

func estimateEmbeddingCostUSD(inputTokens int) float64 {
	return (float64(inputTokens) / 1_000_000) * 0.02
}

func intPtr(v int) *int { return &v }

// -------------------------------------------------------------------
// Cosine Similarity
// -------------------------------------------------------------------

func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// -------------------------------------------------------------------
// CatalogIndex — in-memory search
// -------------------------------------------------------------------

func NewCatalogIndex() *CatalogIndex {
	return &CatalogIndex{}
}

func (idx *CatalogIndex) Reload(repo *repository.SMLCatalogRepo) error {
	dbItems, err := repo.LoadAllEmbeddings()
	if err != nil {
		return fmt.Errorf("load embeddings: %w", err)
	}

	items := make([]indexedItem, 0, len(dbItems))
	for _, d := range dbItems {
		price := 0.0
		if d.Price != nil {
			price = *d.Price
		}
		codeMeta := itemcode.Inspect(d.ItemCode)
		items = append(items, indexedItem{
			CatalogMatch: models.CatalogMatch{
				ItemCode:             d.ItemCode,
				ItemName:             d.ItemName,
				ItemName2:            d.ItemName2,
				UnitCode:             d.UnitCode,
				WHCode:               d.WHCode,
				ShelfCode:            d.ShelfCode,
				Price:                price,
				ImageCount:           d.ImageCount,
				PrimaryImageRoworder: d.PrimaryImageRoworder,
				PrimaryImageGuid:     d.PrimaryImageGuid,
				PrimaryImageBytes:    d.PrimaryImageBytes,
				HasHiddenChars:       codeMeta.HasHiddenChars,
				CleanItemCode:        codeMeta.CleanItemCode,
			},
			embedding: d.Embedding,
		})
	}

	idx.mu.Lock()
	idx.items = items
	idx.mu.Unlock()
	return nil
}

func (idx *CatalogIndex) Size() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.items)
}

func (idx *CatalogIndex) Search(queryEmb []float64, topK int) []models.CatalogMatch {
	idx.mu.RLock()
	items := idx.items
	idx.mu.RUnlock()

	type scored struct {
		idx   int
		score float64
	}

	scores := make([]scored, 0, len(items))
	for i, it := range items {
		s := cosineSimilarity(queryEmb, it.embedding)
		scores = append(scores, scored{i, s})
	}

	for i := 1; i < len(scores); i++ {
		for j := i; j > 0 && scores[j].score > scores[j-1].score; j-- {
			scores[j], scores[j-1] = scores[j-1], scores[j]
		}
	}

	n := topK
	if n > len(scores) {
		n = len(scores)
	}
	result := make([]models.CatalogMatch, n)
	for i := 0; i < n; i++ {
		result[i] = items[scores[i].idx].CatalogMatch
		result[i].Score = scores[i].score
	}
	return result
}
