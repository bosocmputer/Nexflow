package diagnostics

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSanitizeJSONRedactsSecretsAndBuyerPIIRecursively(t *testing.T) {
	raw := []byte(`{
		"result":"contact buyer@example.test or 0812345678; token=message-secret",
		"nested":{
			"access_token":"token-value",
			"Password":"secret-value",
			"buyer_phone":"0812345678",
			"email":"buyer@example.test",
			"customer_name":"Buyer Name",
			"tax_id":"1234567890123",
			"safe":"visible"
		},
		"items":[{"provider_secret":"hidden","item_code":"SKU-001"}]
	}`)

	got := SanitizeJSON(raw)
	if !got.ValidJSON {
		t.Fatal("expected valid JSON")
	}
	text := string(got.Body)
	for _, forbidden := range []string{"token-value", "secret-value", "message-secret", "0812345678", "buyer@example.test", "Buyer Name", "1234567890123", "hidden"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("sanitized JSON contains %q: %s", forbidden, text)
		}
	}
	for _, wanted := range []string{"visible", "SKU-001", RedactedValue} {
		if !strings.Contains(text, wanted) {
			t.Fatalf("sanitized JSON does not contain %q: %s", wanted, text)
		}
	}
}

func TestSanitizeJSONBoundsDepthAndFieldCount(t *testing.T) {
	deep := map[string]any{"leaf": "visible"}
	for index := 0; index < MaxJSONDepth+4; index++ {
		deep = map[string]any{"nested": deep}
	}
	wide := make(map[string]any, MaxJSONFields+50)
	for index := 0; index < MaxJSONFields+50; index++ {
		wide[strings.Repeat("x", 4)+string(rune(index+1000))] = index
	}
	raw, err := json.Marshal(map[string]any{"deep": deep, "wide": wide})
	if err != nil {
		t.Fatal(err)
	}

	got := SanitizeJSON(raw)
	if !got.ValidJSON || !got.Truncated {
		t.Fatalf("expected bounded, truncated JSON, got %+v", got)
	}
	if got.FieldCount > MaxJSONFields {
		t.Fatalf("field count %d exceeds maximum %d", got.FieldCount, MaxJSONFields)
	}
	if len(got.Body) > MaxStoredJSONBytes {
		t.Fatalf("stored body %d exceeds maximum %d", len(got.Body), MaxStoredJSONBytes)
	}
}

func TestSanitizeJSONRejectsNonJSONBodyButKeepsEvidence(t *testing.T) {
	raw := []byte("upstream database password=secret")
	got := SanitizeJSON(raw)

	if got.ValidJSON || len(got.Body) != 0 {
		t.Fatalf("non-JSON body must not be persisted: %+v", got)
	}
	if got.OriginalSize != int64(len(raw)) || got.SHA256 == "" || got.SafeSummary == "" {
		t.Fatalf("expected hash, size, and safe summary: %+v", got)
	}
	if strings.Contains(got.SafeSummary, "secret") {
		t.Fatalf("summary exposed raw body: %q", got.SafeSummary)
	}
}

func TestSanitizeJSONCapsStoredBodyAt64KiB(t *testing.T) {
	raw, err := json.Marshal(map[string]any{"message": strings.Repeat("a", MaxStoredJSONBytes*2)})
	if err != nil {
		t.Fatal(err)
	}

	got := SanitizeJSON(raw)
	if !got.ValidJSON || !got.Truncated {
		t.Fatalf("expected valid truncated JSON: %+v", got)
	}
	if len(got.Body) > MaxStoredJSONBytes {
		t.Fatalf("stored body %d exceeds maximum %d", len(got.Body), MaxStoredJSONBytes)
	}
}

func TestSanitizeHeadersUsesStrictAllowlist(t *testing.T) {
	got := SanitizeHeaders(map[string][]string{
		"Content-Type":     {"application/json"},
		"X-Correlation-ID": {"trace-001"},
		"X-Request-ID":     {"request-001"},
		"Retry-After":      {"2"},
		"Authorization":    {"Bearer secret"},
		"Set-Cookie":       {"session=secret"},
	})

	if len(got) != 4 {
		t.Fatalf("expected four safe headers, got %#v", got)
	}
	if _, ok := got["Authorization"]; ok {
		t.Fatal("authorization header leaked")
	}
	if _, ok := got["Set-Cookie"]; ok {
		t.Fatal("cookie header leaked")
	}
}
