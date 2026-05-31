package mistral

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OCRClient calls Mistral OCR API to extract text from PDF documents.
type OCRClient struct {
	apiKey     string
	httpClient *http.Client
}

// New creates a new Mistral OCR client.
func New(apiKey string) *OCRClient {
	return &OCRClient{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// IsConfigured returns true if the API key is set.
func (c *OCRClient) IsConfigured() bool {
	return c.apiKey != ""
}

type ocrRequest struct {
	Model    string      `json:"model"`
	Document ocrDocument `json:"document"`
}

type ocrDocument struct {
	Type        string `json:"type"`
	DocumentURL string `json:"document_url"`
}

type ocrResponse struct {
	Pages []ocrPage `json:"pages"`
}

type ocrPage struct {
	Markdown string `json:"markdown"`
}

// ExtractTextFromPDF sends a base64-encoded PDF to Mistral OCR and returns
// the combined markdown text from all pages.
func (c *OCRClient) ExtractTextFromPDF(pdfBase64 string) (string, error) {
	req := ocrRequest{
		Model: "mistral-ocr-2512",
		Document: ocrDocument{
			Type:        "document_url",
			DocumentURL: "data:application/pdf;base64," + pdfBase64,
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("mistral OCR marshal: %w", err)
	}

	httpReq, err := http.NewRequest("POST", "https://api.mistral.ai/v1/ocr", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("mistral OCR new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("mistral OCR HTTP: %w", err)
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("mistral OCR status %d: %s", resp.StatusCode, string(respBytes))
	}

	var ocrResp ocrResponse
	if err := json.Unmarshal(respBytes, &ocrResp); err != nil {
		return "", fmt.Errorf("mistral OCR parse: %w", err)
	}

	var sb strings.Builder
	for _, page := range ocrResp.Pages {
		sb.WriteString(page.Markdown)
		sb.WriteString("\n\n")
	}
	return strings.TrimSpace(sb.String()), nil
}
