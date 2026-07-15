package gatewayauth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	HeaderTenant    = "X-Nexflow-Tenant"
	HeaderTimestamp = "X-Nexflow-Timestamp"
	HeaderNonce     = "X-Nexflow-Nonce"
	HeaderSignature = "X-Nexflow-Signature"
)

var (
	ErrMissingAuthentication = errors.New("gateway authentication headers are incomplete")
	ErrExpiredRequest        = errors.New("gateway request timestamp is outside the allowed window")
	ErrInvalidSignature      = errors.New("gateway request signature is invalid")
	ErrReplay                = errors.New("gateway request nonce was already used")
)

type NonceStore interface {
	Consume(ctx context.Context, tenant, nonce string, expiresAt time.Time) error
}

type SecretResolver func(ctx context.Context, tenant string) (string, error)

type Identity struct {
	Tenant    string
	Nonce     string
	Timestamp time.Time
}

type Verifier struct {
	ResolveSecret SecretResolver
	Nonces        NonceStore
	MaxSkew       time.Duration
	Now           func() time.Time
}

func Sign(secret, method, requestURI, tenant, timestamp, nonce string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical(method, requestURI, tenant, timestamp, nonce, body)))
	return hex.EncodeToString(mac.Sum(nil))
}

func Apply(req *http.Request, tenant, secret string, body []byte, now time.Time, nonce string) error {
	tenant = strings.TrimSpace(tenant)
	secret = strings.TrimSpace(secret)
	if tenant == "" || secret == "" {
		return ErrMissingAuthentication
	}
	if strings.TrimSpace(nonce) == "" {
		var err error
		nonce, err = randomNonce()
		if err != nil {
			return err
		}
	}
	timestamp := strconv.FormatInt(now.Unix(), 10)
	req.Header.Set(HeaderTenant, tenant)
	req.Header.Set(HeaderTimestamp, timestamp)
	req.Header.Set(HeaderNonce, nonce)
	req.Header.Set(HeaderSignature, Sign(secret, req.Method, req.URL.RequestURI(), tenant, timestamp, nonce, body))
	return nil
}

func (v Verifier) Verify(ctx context.Context, req *http.Request, body []byte) (*Identity, error) {
	tenant := strings.TrimSpace(req.Header.Get(HeaderTenant))
	timestampRaw := strings.TrimSpace(req.Header.Get(HeaderTimestamp))
	nonce := strings.TrimSpace(req.Header.Get(HeaderNonce))
	gotSignature := strings.ToLower(strings.TrimSpace(req.Header.Get(HeaderSignature)))
	if tenant == "" || timestampRaw == "" || nonce == "" || gotSignature == "" || v.ResolveSecret == nil {
		return nil, ErrMissingAuthentication
	}
	timestampUnix, err := strconv.ParseInt(timestampRaw, 10, 64)
	if err != nil {
		return nil, ErrExpiredRequest
	}
	now := time.Now()
	if v.Now != nil {
		now = v.Now()
	}
	maxSkew := v.MaxSkew
	if maxSkew <= 0 {
		maxSkew = 5 * time.Minute
	}
	timestamp := time.Unix(timestampUnix, 0)
	if timestamp.Before(now.Add(-maxSkew)) || timestamp.After(now.Add(maxSkew)) {
		return nil, ErrExpiredRequest
	}
	secret, err := v.ResolveSecret(ctx, tenant)
	if err != nil || strings.TrimSpace(secret) == "" {
		return nil, ErrInvalidSignature
	}
	wantSignature := Sign(secret, req.Method, req.URL.RequestURI(), tenant, timestampRaw, nonce, body)
	if subtle.ConstantTimeCompare([]byte(gotSignature), []byte(wantSignature)) != 1 {
		return nil, ErrInvalidSignature
	}
	if v.Nonces != nil {
		if err := v.Nonces.Consume(ctx, tenant, nonce, now.Add(maxSkew)); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrReplay, err)
		}
	}
	return &Identity{Tenant: tenant, Nonce: nonce, Timestamp: timestamp}, nil
}

func canonical(method, requestURI, tenant, timestamp, nonce string, body []byte) string {
	sum := sha256.Sum256(body)
	return strings.Join([]string{
		strings.ToUpper(strings.TrimSpace(method)),
		requestURI,
		strings.TrimSpace(tenant),
		strings.TrimSpace(timestamp),
		strings.TrimSpace(nonce),
		hex.EncodeToString(sum[:]),
	}, "\n")
}

func randomNonce() (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate gateway nonce: %w", err)
	}
	return hex.EncodeToString(b), nil
}
