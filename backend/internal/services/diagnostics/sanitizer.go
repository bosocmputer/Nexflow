package diagnostics

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxResponseBytes   = 2 << 20
	MaxStoredJSONBytes = 64 << 10
	MaxJSONDepth       = 12
	MaxJSONFields      = 1000
	maxStoredString    = 4096
	RedactedValue      = "[REDACTED]"
	TruncatedValue     = "[TRUNCATED]"
)

type SanitizedJSON struct {
	Body         []byte
	SHA256       string
	OriginalSize int64
	FieldCount   int
	ValidJSON    bool
	Truncated    bool
	SafeSummary  string
}

var safeResponseHeaders = map[string]struct{}{
	"Content-Type":     {},
	"X-Correlation-Id": {},
	"X-Request-Id":     {},
	"Retry-After":      {},
}

var deniedKeyFragments = []string{
	"authorization",
	"cookie",
	"token",
	"password",
	"passwd",
	"secret",
	"credential",
	"guid",
	"provider",
	"database",
	"buyer",
	"recipient",
	"address",
	"phone",
	"telephone",
	"mobile",
	"email",
}

func digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func normalizeKey(key string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, key)
}

func deniedKey(key string) bool {
	normalized := normalizeKey(key)
	for _, fragment := range deniedKeyFragments {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}

type sanitizeState struct {
	fields    int
	truncated bool
}

func truncateString(value string, state *sanitizeState) string {
	if len(value) <= maxStoredString {
		return value
	}
	state.truncated = true
	end := maxStoredString
	for end > 0 && !utf8.ValidString(value[:end]) {
		end--
	}
	return value[:end] + TruncatedValue
}

func skipDelimited(decoder *json.Decoder) error {
	depth := 1
	for depth > 0 {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		if delimiter, ok := token.(json.Delim); ok {
			switch delimiter {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
	}
	return nil
}

func skipValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if delimiter, ok := token.(json.Delim); ok && (delimiter == '{' || delimiter == '[') {
		return skipDelimited(decoder)
	}
	return nil
}

func decodeBoundedValue(decoder *json.Decoder, depth int, state *sanitizeState) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		if value, ok := token.(string); ok {
			return truncateString(value, state), nil
		}
		return token, nil
	}
	if delimiter != '{' && delimiter != '[' {
		return nil, &json.SyntaxError{}
	}
	if depth >= MaxJSONDepth {
		state.truncated = true
		if err := skipDelimited(decoder); err != nil {
			return nil, err
		}
		return TruncatedValue, nil
	}

	if delimiter == '{' {
		result := make(map[string]any)
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, &json.SyntaxError{}
			}
			if state.fields >= MaxJSONFields {
				state.truncated = true
				if err := skipValue(decoder); err != nil {
					return nil, err
				}
				continue
			}
			state.fields++
			if deniedKey(key) {
				if err := skipValue(decoder); err != nil {
					return nil, err
				}
				result[key] = RedactedValue
				continue
			}
			value, err := decodeBoundedValue(decoder, depth+1, state)
			if err != nil {
				return nil, err
			}
			result[key] = value
		}
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
		return result, nil
	}

	result := make([]any, 0)
	for decoder.More() {
		if state.fields >= MaxJSONFields {
			state.truncated = true
			if err := skipValue(decoder); err != nil {
				return nil, err
			}
			continue
		}
		state.fields++
		value, err := decodeBoundedValue(decoder, depth+1, state)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	return result, nil
}

func SanitizeJSON(raw []byte) SanitizedJSON {
	result := SanitizedJSON{
		SHA256:       digest(raw),
		OriginalSize: int64(len(raw)),
		SafeSummary:  "Non-JSON response body omitted from diagnostics",
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	state := &sanitizeState{}
	decoded, err := decodeBoundedValue(decoder, 0, state)
	if err != nil {
		return result
	}
	if _, err := decoder.Token(); err != io.EOF {
		return result
	}
	body, err := json.Marshal(decoded)
	if err != nil {
		result.SafeSummary = "JSON response could not be sanitized"
		return result
	}
	if len(body) > MaxStoredJSONBytes {
		state.truncated = true
		body = []byte(`{"_truncated":true,"summary":"Sanitized response exceeded the diagnostic storage limit"}`)
	}
	result.Body = body
	result.FieldCount = state.fields
	result.ValidJSON = true
	result.Truncated = state.truncated
	result.SafeSummary = ""
	return result
}

func SanitizeHeaders(headers map[string][]string) map[string]string {
	result := make(map[string]string, len(safeResponseHeaders))
	for key, values := range headers {
		canonical := http.CanonicalHeaderKey(key)
		if _, ok := safeResponseHeaders[canonical]; !ok || len(values) == 0 {
			continue
		}
		value := strings.TrimSpace(values[0])
		if value != "" {
			result[canonical] = value
		}
	}
	return result
}
