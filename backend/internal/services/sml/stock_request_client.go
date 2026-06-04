package sml

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"go.uber.org/zap"
)

const stockRequestPath = "/SMLJavaWebService/rest/v1/processstockrequest"
const stockChunkSize = 100
const stockErrorBodyLimit = 2048

// StockRequestClient calls the SML processstockrequest endpoint to trigger
// cost recalculation after a document is posted. This endpoint lives directly
// on the SML Java server (NOT sml-api-byboss) — never send X-DB-* headers here.
type StockRequestClient struct {
	baseURL  string
	provider string
	database string
	logger   *zap.Logger
}

type stockRequestPayload struct {
	ProviderCode string   `json:"providerCode"`
	DatabaseName string   `json:"databaseName"`
	ItemCode     []string `json:"itemCode"`
}

func NewStockRequestClient(baseURL, provider, database string, logger *zap.Logger) *StockRequestClient {
	return &StockRequestClient{
		baseURL:  strings.TrimRight(baseURL, "/"),
		provider: provider,
		database: database,
		logger:   logger,
	}
}

// ProcessStockRequest deduplicates and trims rawCodes, then POSTs them in
// chunks of stockChunkSize. Errors from individual chunks are collected and
// returned together so a single slow chunk does not abort the rest.
// No X-DB-* headers are sent — this URL is not sml-api-byboss.
func (c *StockRequestClient) ProcessStockRequest(ctx context.Context, rawCodes []string) error {
	codes := dedupeTrimCodes(rawCodes)
	if len(codes) == 0 {
		return nil
	}

	url := c.baseURL + stockRequestPath
	var errs []string
	total := (len(codes) + stockChunkSize - 1) / stockChunkSize

	for i := 0; i < len(codes); i += stockChunkSize {
		end := i + stockChunkSize
		if end > len(codes) {
			end = len(codes)
		}
		chunkNum := i/stockChunkSize + 1

		payload := stockRequestPayload{
			ProviderCode: c.provider,
			DatabaseName: c.database,
			ItemCode:     codes[i:end],
		}
		body, err := marshalASCII(payload)
		if err != nil {
			errs = append(errs, fmt.Sprintf("chunk %d/%d marshal: %v", chunkNum, total, err))
			continue
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			errs = append(errs, fmt.Sprintf("chunk %d/%d build: %v", chunkNum, total, err))
			continue
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			errs = append(errs, fmt.Sprintf("chunk %d/%d http: %v", chunkNum, total, err))
			continue
		}
		if resp.StatusCode >= 300 {
			errBody := readStockErrorBody(resp.Body)
			if errBody != "" {
				errs = append(errs, fmt.Sprintf("chunk %d/%d HTTP %d: %s", chunkNum, total, resp.StatusCode, errBody))
			} else {
				errs = append(errs, fmt.Sprintf("chunk %d/%d HTTP %d", chunkNum, total, resp.StatusCode))
			}
		}
		if err := resp.Body.Close(); err != nil && c.logger != nil {
			c.logger.Warn("stock request response close failed", zap.Error(err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%d/%d chunks failed: %s", len(errs), total, strings.Join(errs, "; "))
	}
	return nil
}

func readStockErrorBody(body io.Reader) string {
	if body == nil {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(body, stockErrorBodyLimit+1))
	if err != nil {
		return fmt.Sprintf("read response body: %v", err)
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return ""
	}
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > stockErrorBodyLimit {
		text = text[:stockErrorBodyLimit] + "...(truncated)"
	}
	return text
}

// dedupeTrimCodes trims whitespace, removes empty strings, and deduplicates
// while preserving first-seen order.
func dedupeTrimCodes(codes []string) []string {
	seen := make(map[string]struct{}, len(codes))
	out := make([]string, 0, len(codes))
	for _, c := range codes {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	return out
}
