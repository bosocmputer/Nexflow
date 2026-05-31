package ai

import (
	"net/http"
	"strings"
	"testing"
)

func TestOpenRouterAppAttributionHeaders(t *testing.T) {
	client := NewClient("key", "model", "fallback", "audio").
		WithAppAttribution("Nexflow", "https://example.test")
	req, err := http.NewRequest(http.MethodPost, "https://openrouter.ai/api/v1/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}

	client.setOpenRouterHeaders(req)

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

func TestOpenRouterSessionIDIsNexflowScoped(t *testing.T) {
	sessionID := newOpenRouterSessionID("Catalog Embed", "Embed Text")
	if !strings.HasPrefix(sessionID, "nexflow:catalog-embed:embed-text:") {
		t.Fatalf("sessionID = %q, want nexflow scoped prefix", sessionID)
	}
}
