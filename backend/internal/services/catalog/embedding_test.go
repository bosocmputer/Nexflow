package catalog

import (
	"net/http"
	"strings"
	"testing"
)

func TestEmbeddingOpenRouterAppAttributionHeaders(t *testing.T) {
	svc := NewEmbeddingService("key").
		WithAppAttribution("Nexflow", "https://example.test")
	req, err := http.NewRequest(http.MethodPost, embeddingAPIURL, nil)
	if err != nil {
		t.Fatal(err)
	}

	svc.setOpenRouterHeaders(req)

	if got := req.Header.Get("X-OpenRouter-Title"); got != "Nexflow" {
		t.Fatalf("X-OpenRouter-Title = %q, want Nexflow", got)
	}
	if got := req.Header.Get("X-Title"); got != "Nexflow" {
		t.Fatalf("X-Title = %q, want Nexflow", got)
	}
	if got := req.Header.Get("HTTP-Referer"); got != "https://example.test" {
		t.Fatalf("HTTP-Referer = %q, want https://example.test", got)
	}
}

func TestEmbeddingSessionIDIsNexflowScoped(t *testing.T) {
	sessionID := newOpenRouterSessionID("Catalog Embed All", "Pending")
	if !strings.HasPrefix(sessionID, "nexflow:catalog-embed-all:pending:") {
		t.Fatalf("sessionID = %q, want nexflow scoped prefix", sessionID)
	}
}
