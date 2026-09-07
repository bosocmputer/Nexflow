package sml

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"time"
)

const MaxDiagnosticResponseBytes = 2 << 20

type HTTPExchangeRequest struct {
	Method        string
	CanonicalPath string
	ContentType   string
	CorrelationID string
}

type HTTPExchangeResult struct {
	Status           string
	HTTPStatus       *int
	ResponseHeaders  http.Header
	Body             []byte
	ResponseHash     string
	ResponseSize     int64
	Truncated        bool
	ErrorCode        string
	ErrorClass       string
	SafeErrorSummary string
	Duration         time.Duration
}

// HTTPExchangeHooks are diagnostic-only. Panics are contained and empty IDs
// disable finalization, so evidence failures never alter the SML result.
type HTTPExchangeHooks struct {
	Before func(HTTPExchangeRequest) string
	After  func(exchangeID string, result HTTPExchangeResult)
}

func beginHTTPExchange(hooks *HTTPExchangeHooks, request HTTPExchangeRequest) (id string) {
	if hooks == nil || hooks.Before == nil {
		return ""
	}
	defer func() {
		if recover() != nil {
			id = ""
		}
	}()
	return hooks.Before(request)
}

func finishHTTPExchange(hooks *HTTPExchangeHooks, exchangeID string, result HTTPExchangeResult) {
	if hooks == nil || hooks.After == nil || exchangeID == "" {
		return
	}
	defer func() { _ = recover() }()
	hooks.After(exchangeID, result)
}

func readDiagnosticResponse(response *http.Response) ([]byte, string, int64, bool, error) {
	if response == nil || response.Body == nil {
		return nil, "", 0, false, nil
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxDiagnosticResponseBytes))
	if err != nil {
		return body, "", int64(len(body)), false, err
	}
	sum := sha256.Sum256(body)
	responseSize := int64(len(body))
	truncated := len(body) == MaxDiagnosticResponseBytes
	if response.ContentLength >= 0 {
		responseSize = response.ContentLength
		truncated = response.ContentLength > MaxDiagnosticResponseBytes
	}
	return body, hex.EncodeToString(sum[:]), responseSize, truncated, nil
}

func exchangeResult(
	response *http.Response,
	body []byte,
	hash string,
	size int64,
	truncated bool,
	duration time.Duration,
	succeeded bool,
	code string,
	readErr error,
) HTTPExchangeResult {
	result := HTTPExchangeResult{
		Body: body, ResponseHash: hash, ResponseSize: size,
		Truncated: truncated, ErrorCode: code, Duration: duration,
	}
	if response != nil {
		status := response.StatusCode
		result.HTTPStatus = &status
		result.ResponseHeaders = response.Header.Clone()
	}
	switch {
	case readErr != nil:
		result.Status = "unknown"
		result.ErrorClass = "transport"
		result.SafeErrorSummary = "SML response could not be read completely"
	case truncated:
		result.Status = "unknown"
		result.ErrorClass = "response_limit"
		result.SafeErrorSummary = "SML response exceeded the diagnostic read limit"
	case succeeded:
		result.Status = "succeeded"
	case response != nil:
		result.Status = "failed"
		result.ErrorClass = "business"
		if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
			result.SafeErrorSummary = "SML rejected the document"
		} else {
			result.SafeErrorSummary = "SML returned HTTP " + response.Status
		}
	default:
		result.Status = "unknown"
		result.ErrorClass = "transport"
		result.SafeErrorSummary = "SML transport request failed"
	}
	return result
}
