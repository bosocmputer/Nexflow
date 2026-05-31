package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"nexflow/internal/models"
)

type Client struct {
	apiKey        string
	model         string
	fallbackModel string
	audioModel    string
	appTitle      string
	appReferer    string
	httpClient    *http.Client
	usageLogger   UsageLogger
}

type UsageLogger interface {
	Log(models.AIUsageEntry) error
}

func NewClient(apiKey, model, fallbackModel, audioModel string) *Client {
	return &Client{
		apiKey:        apiKey,
		model:         model,
		fallbackModel: fallbackModel,
		audioModel:    audioModel,
		httpClient:    &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *Client) WithUsageLogger(logger UsageLogger) *Client {
	c.usageLogger = logger
	return c
}

func (c *Client) WithAppAttribution(title, referer string) *Client {
	c.appTitle = strings.TrimSpace(title)
	c.appReferer = strings.TrimSpace(referer)
	return c
}

type ExtractedBill struct {
	DocType       string          `json:"doc_type"`
	CustomerName  string          `json:"customer_name"`
	CustomerPhone *string         `json:"customer_phone"`
	Items         []ExtractedItem `json:"items"`
	TotalAmount   *float64        `json:"total_amount"`
	Note          *string         `json:"note"`
	Confidence    float64         `json:"confidence"`
}

// ExtractedOrder is one Shopee order from a multi-order payment-confirmation email.
type ExtractedOrder struct {
	OrderID     string          `json:"order_id"`
	SellerName  string          `json:"seller_name"`
	Items       []ExtractedItem `json:"items"`
	TotalAmount *float64        `json:"total_amount"`
	DocDate     string          `json:"doc_date"`
	Confidence  float64         `json:"confidence"`
}

type ExtractedItem struct {
	RawName  string   `json:"raw_name"`
	Qty      float64  `json:"qty"`
	Unit     string   `json:"unit"`
	Price    *float64 `json:"price"`
	ImageURL string   `json:"image_url,omitempty"`
}

type openRouterRequest struct {
	Model     string                 `json:"model"`
	Messages  []message              `json:"messages"`
	MaxTokens int                    `json:"max_tokens,omitempty"`
	SessionID string                 `json:"session_id,omitempty"`
	User      string                 `json:"user,omitempty"`
	Trace     map[string]interface{} `json:"trace,omitempty"`
}

type message struct {
	Role    string        `json:"role"`
	Content []contentPart `json:"content"`
}

type contentPart struct {
	Type     string       `json:"type"`
	Text     string       `json:"text,omitempty"`
	ImageURL *imageURLObj `json:"image_url,omitempty"`
}

type imageURLObj struct {
	URL string `json:"url"`
}

type openRouterResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage *openRouterUsage `json:"usage,omitempty"`
}

type openRouterUsage struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	Cost             float64 `json:"cost"`
}

// ExtractText sends text to OpenRouter and returns parsed bill data
func (c *Client) ExtractText(text string) (*ExtractedBill, error) {
	return c.extract("email_extract", "extract_text", c.model, []contentPart{
		{Type: "text", Text: ExtractPrompt},
		{Type: "text", Text: text},
	})
}

// ExtractImage sends base64 image to OpenRouter
func (c *Client) ExtractImage(base64Data, mimeType string) (*ExtractedBill, error) {
	return c.extract("media_extract", "extract_image", c.model, []contentPart{
		{Type: "text", Text: ExtractPrompt},
		{Type: "image_url", ImageURL: &imageURLObj{
			URL: fmt.Sprintf("data:%s;base64,%s", mimeType, base64Data),
		}},
	})
}

func (c *Client) extract(feature, operation, model string, parts []contentPart) (*ExtractedBill, error) {
	body, err := c.requestChat(feature, operation, model, parts)
	if err != nil {
		// Retry with fallback model
		if model != c.fallbackModel {
			return c.extract(feature, operation+"_fallback", c.fallbackModel, parts)
		}
		return nil, err
	}

	var bill ExtractedBill
	if err := json.Unmarshal([]byte(cleanJSONBody(body)), &bill); err != nil {
		// Non-JSON response — retry with fallback
		if model != c.fallbackModel {
			return c.extract(feature, operation+"_fallback_parse", c.fallbackModel, parts)
		}
		return nil, fmt.Errorf("parse response: %w — body: %s", err, body)
	}
	return &bill, nil
}

func (c *Client) requestChat(feature, operation, model string, parts []contentPart) (string, error) {
	reqBody := openRouterRequest{
		Model: model,
		Messages: []message{
			{Role: "user", Content: parts},
		},
	}
	return c.doRequest(feature, operation, reqBody)
}

// ExtractPDF sends a base64-encoded PDF to OpenRouter (Gemini Flash supports inline PDF)
func (c *Client) ExtractPDF(base64Data string) (*ExtractedBill, error) {
	return c.extract("media_extract", "extract_pdf", c.model, []contentPart{
		{Type: "text", Text: ExtractPrompt},
		{Type: "image_url", ImageURL: &imageURLObj{
			URL: "data:application/pdf;base64," + base64Data,
		}},
	})
}

// ExtractOrders parses a Shopee payment-confirmation email body and returns
// one ExtractedOrder per order_id found. Falls back to a single-order slice
// wrapping ExtractText output if AI returns non-array JSON.
func (c *Client) ExtractOrders(text string) ([]ExtractedOrder, error) {
	parts := []contentPart{
		{Type: "text", Text: ExtractShopeeOrdersPrompt},
		{Type: "text", Text: text},
	}

	body, err := c.requestChat("shopee_email_parse", "extract_orders", c.model, parts)
	if err == nil {
		orders, parseErr := parseExtractedOrders(body)
		if parseErr == nil {
			return orders, nil
		}
		err = parseErr
	}

	if c.model != c.fallbackModel {
		fallbackBody, fallbackErr := c.requestChat("shopee_email_parse", "extract_orders_fallback", c.fallbackModel, parts)
		if fallbackErr == nil {
			return parseExtractedOrders(fallbackBody)
		}
		return nil, fallbackErr
	}
	return nil, err
}

// ExtractOrdersWithHTML is like ExtractOrders but also sends the email HTML so
// the AI can correlate each item with its product image URL.
func (c *Client) ExtractOrdersWithHTML(text, html string) ([]ExtractedOrder, error) {
	parts := []contentPart{
		{Type: "text", Text: ExtractShopeeOrdersPrompt},
		{Type: "text", Text: text},
	}
	if html != "" {
		parts = append(parts, contentPart{
			Type: "text",
			Text: "HTML ของ email (ใช้ดึง image_url ต่อสินค้าแต่ละรายการจาก <img> ที่อยู่ใกล้ชื่อสินค้า):\n" + html,
		})
	}

	body, err := c.requestChat("shopee_email_parse", "extract_orders_html", c.model, parts)
	if err == nil {
		orders, parseErr := parseExtractedOrders(body)
		if parseErr == nil {
			return orders, nil
		}
		err = parseErr
	}

	if c.model != c.fallbackModel {
		fallbackBody, fallbackErr := c.requestChat("shopee_email_parse", "extract_orders_html_fallback", c.fallbackModel, parts)
		if fallbackErr == nil {
			return parseExtractedOrders(fallbackBody)
		}
		return nil, fallbackErr
	}
	return nil, err
}

func parseExtractedOrders(body string) ([]ExtractedOrder, error) {
	body = cleanJSONBody(body)
	var orders []ExtractedOrder
	if jsonErr := json.Unmarshal([]byte(body), &orders); jsonErr == nil && len(orders) > 0 {
		return orders, nil
	}

	// AI might have returned a single object — wrap it
	var single ExtractedOrder
	if jsonErr := json.Unmarshal([]byte(body), &single); jsonErr == nil && single.OrderID != "" {
		return []ExtractedOrder{single}, nil
	}

	return nil, fmt.Errorf("ExtractOrders: could not parse AI response as order array: %s", body)
}

func cleanJSONBody(body string) string {
	body = strings.TrimSpace(body)
	if !strings.HasPrefix(body, "```") {
		return body
	}
	body = strings.TrimPrefix(body, "```json")
	body = strings.TrimPrefix(body, "```JSON")
	body = strings.TrimPrefix(body, "```")
	body = strings.TrimSpace(body)
	body = strings.TrimSuffix(body, "```")
	return strings.TrimSpace(body)
}

// TranscribeAudio calls OpenRouter Whisper endpoint and returns transcribed text
func (c *Client) TranscribeAudio(audioData []byte) (string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	fw, err := w.CreateFormFile("file", "audio.m4a")
	if err != nil {
		return "", err
	}
	if _, err := fw.Write(audioData); err != nil {
		return "", err
	}
	if err := w.WriteField("model", c.audioModel); err != nil {
		return "", err
	}
	w.Close()

	req, err := http.NewRequest("POST",
		"https://openrouter.ai/api/v1/audio/transcriptions", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	c.setOpenRouterHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("transcribe request: %w", err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		c.logUsage(models.AIUsageEntry{
			Provider: "openrouter", Model: c.audioModel, Feature: "audio_transcription",
			Operation: "transcribe_audio", Status: "error", Error: fmt.Sprintf("status %d", resp.StatusCode),
		})
		return "", fmt.Errorf("transcribe status %d: %s", resp.StatusCode, respData)
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(respData, &result); err != nil {
		return "", fmt.Errorf("parse transcription: %w — body: %s", err, respData)
	}
	c.logUsage(models.AIUsageEntry{
		Provider: "openrouter", Model: c.audioModel, Feature: "audio_transcription",
		Operation: "transcribe_audio", Status: "success",
		Metadata: map[string]interface{}{"bytes": len(audioData)},
	})
	return result.Text, nil
}

// GenerateInsight generates daily AI insight text
func (c *Client) GenerateInsight(statsJSON string) (string, error) {
	prompt := fmt.Sprintf(InsightPrompt, statsJSON)
	reqBody := openRouterRequest{
		Model: c.model,
		Messages: []message{
			{Role: "user", Content: []contentPart{{Type: "text", Text: prompt}}},
		},
	}
	return c.doRequest("daily_insight", "generate_daily_insight", reqBody)
}

// ChatMessage is a single turn in a conversation history
type ChatMessage struct {
	Role    string
	Content string
}

// SalesChatResult holds the chatbot reply and optional extracted order
// ChatSales / ChatSalesWithContext / ExtractOrderFromHistory were removed in
// session 13 along with the AI chatbot ("น้องบิล"). LINE conversations are now
// human-to-human via the /messages inbox; AI is only used for media extraction
// (ExtractImage / ExtractPDF / ExtractText / TranscribeAudio) which is still
// reachable from the chat inbox manual-trigger button + email pipeline.

func (c *Client) doRequest(feature, operation string, reqBody openRouterRequest) (string, error) {
	if reqBody.MaxTokens == 0 {
		reqBody.MaxTokens = 4096
	}
	if reqBody.SessionID == "" {
		reqBody.SessionID = newOpenRouterSessionID(feature, operation)
	}
	if reqBody.User == "" {
		reqBody.User = "nexflow-backend"
	}
	if reqBody.Trace == nil {
		reqBody.Trace = openRouterTrace(reqBody.SessionID, feature, operation)
	}
	start := time.Now()
	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	c.setOpenRouterHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logUsage(models.AIUsageEntry{
			Provider: "openrouter", Model: reqBody.Model, Feature: feature, Operation: operation,
			Status: "error", Error: err.Error(), DurationMs: intPtr(int(time.Since(start).Milliseconds())),
			Metadata: map[string]interface{}{"session_id": reqBody.SessionID},
		})
		return "", fmt.Errorf("openrouter request: %w", err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		c.logUsage(models.AIUsageEntry{
			Provider: "openrouter", Model: reqBody.Model, Feature: feature, Operation: operation,
			Status: "error", Error: fmt.Sprintf("status %d", resp.StatusCode),
			DurationMs: intPtr(int(time.Since(start).Milliseconds())),
			Metadata: map[string]interface{}{
				"response":   truncate(string(respData), 500),
				"session_id": reqBody.SessionID,
			},
		})
		return "", fmt.Errorf("openrouter status %d: %s", resp.StatusCode, string(respData))
	}

	var orResp openRouterResponse
	if err := json.Unmarshal(respData, &orResp); err != nil {
		c.logUsage(models.AIUsageEntry{
			Provider: "openrouter", Model: reqBody.Model, Feature: feature, Operation: operation,
			Status: "error", Error: "parse response: " + err.Error(), DurationMs: intPtr(int(time.Since(start).Milliseconds())),
			Metadata: map[string]interface{}{"session_id": reqBody.SessionID},
		})
		return "", fmt.Errorf("parse openrouter response: %w", err)
	}
	if len(orResp.Choices) == 0 {
		c.logUsage(models.AIUsageEntry{
			Provider: "openrouter", Model: reqBody.Model, Feature: feature, Operation: operation,
			Status: "error", Error: "empty choices", DurationMs: intPtr(int(time.Since(start).Milliseconds())),
			Metadata: map[string]interface{}{"session_id": reqBody.SessionID},
		})
		return "", fmt.Errorf("empty choices from openrouter")
	}
	entry := models.AIUsageEntry{
		Provider: "openrouter", Model: reqBody.Model, Feature: feature, Operation: operation,
		Status: "success", DurationMs: intPtr(int(time.Since(start).Milliseconds())),
		Metadata: map[string]interface{}{"session_id": reqBody.SessionID},
	}
	if orResp.Usage != nil {
		entry.InputTokens = orResp.Usage.PromptTokens
		entry.OutputTokens = orResp.Usage.CompletionTokens
		entry.TotalTokens = orResp.Usage.TotalTokens
		entry.EstimatedCostUSD = orResp.Usage.Cost
	}
	if entry.TotalTokens == 0 {
		entry.InputTokens = estimateTokensFromRequest(reqBody)
		entry.OutputTokens = estimateTokens(orResp.Choices[0].Message.Content)
		entry.TotalTokens = entry.InputTokens + entry.OutputTokens
	}
	if entry.EstimatedCostUSD == 0 {
		entry.EstimatedCostUSD = estimateCostUSD(reqBody.Model, entry.InputTokens, entry.OutputTokens)
	}
	c.logUsage(entry)
	return orResp.Choices[0].Message.Content, nil
}

func (c *Client) logUsage(entry models.AIUsageEntry) {
	if c.usageLogger == nil {
		return
	}
	_ = c.usageLogger.Log(entry)
}

func (c *Client) setOpenRouterHeaders(req *http.Request) {
	title := c.appTitle
	if title == "" {
		title = "Nexflow"
	}
	req.Header.Set("X-OpenRouter-Title", title)
	req.Header.Set("X-Title", title)
	if c.appReferer != "" {
		req.Header.Set("HTTP-Referer", c.appReferer)
	}
}

func newOpenRouterSessionID(feature, operation string) string {
	return fmt.Sprintf("nexflow:%s:%s:%s", safeTracePart(feature), safeTracePart(operation), time.Now().UTC().Format("20060102T150405.000000000Z"))
}

func openRouterTrace(sessionID, feature, operation string) map[string]interface{} {
	return map[string]interface{}{
		"trace_id":        sessionID,
		"trace_name":      "Nexflow " + feature,
		"span_name":       operation,
		"generation_name": feature + ":" + operation,
		"environment":     "production-main",
		"feature":         feature,
	}
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

func estimateTokensFromRequest(req openRouterRequest) int {
	var chars int
	for _, msg := range req.Messages {
		for _, part := range msg.Content {
			chars += len(part.Text)
			if part.ImageURL != nil {
				chars += len(part.ImageURL.URL) / 8
			}
		}
	}
	return estimateTokensByChars(chars)
}

func estimateTokens(text string) int {
	return estimateTokensByChars(len(text))
}

func estimateTokensByChars(chars int) int {
	if chars <= 0 {
		return 0
	}
	return (chars + 3) / 4
}

func estimateCostUSD(model string, inputTokens, outputTokens int) float64 {
	type price struct{ in, out float64 }
	prices := map[string]price{
		"google/gemini-2.5-flash":       {in: 0.30, out: 2.50},
		"google/gemini-flash-1.5":       {in: 0.075, out: 0.30},
		"anthropic/claude-3-5-haiku":    {in: 0.80, out: 4.00},
		"openai/text-embedding-3-small": {in: 0.02, out: 0},
	}
	p, ok := prices[model]
	if !ok {
		p = price{in: 0.50, out: 1.50}
	}
	return (float64(inputTokens)/1_000_000)*p.in + (float64(outputTokens)/1_000_000)*p.out
}

func intPtr(v int) *int { return &v }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
