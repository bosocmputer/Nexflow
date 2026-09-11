package tiktokgateway

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	oauthStateTTL        = 15 * time.Minute
	oauthStateNonceBytes = 18
	maxOAuthStateLength  = 128
)

var (
	tenantSlugPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)
	ErrInvalidOAuthState = errors.New("invalid TikTok Shop OAuth state")
)

type OAuthStateClaims struct {
	Tenant    string `json:"tenant"`
	UserID    string `json:"user_id"`
	ReturnURL string `json:"return_url"`
	Nonce     string `json:"nonce"`
	ExpiresAt int64  `json:"exp"`
}

type OAuthStateSigner struct {
	key []byte
}

func NewOAuthStateSigner(encodedKey string) (*OAuthStateSigner, error) {
	key, err := decodeKey(encodedKey)
	if err != nil {
		return nil, fmt.Errorf("OAuth signing key: %w", err)
	}
	return &OAuthStateSigner{key: key}, nil
}

func (s *OAuthStateSigner) Create(tenant, userID, returnURL string, now time.Time) (string, OAuthStateClaims, error) {
	if s == nil || len(s.key) == 0 {
		return "", OAuthStateClaims{}, ErrInvalidOAuthState
	}
	tenant = strings.ToLower(strings.TrimSpace(tenant))
	userID = strings.TrimSpace(userID)
	returnURL = strings.TrimSpace(returnURL)
	if !tenantSlugPattern.MatchString(tenant) || userID == "" || returnURL == "" {
		return "", OAuthStateClaims{}, ErrInvalidOAuthState
	}
	nonceBytes := make([]byte, oauthStateNonceBytes)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", OAuthStateClaims{}, fmt.Errorf("generate OAuth nonce: %w", err)
	}
	claims := OAuthStateClaims{
		Tenant:    tenant,
		UserID:    userID,
		ReturnURL: returnURL,
		Nonce:     base64.RawURLEncoding.EncodeToString(nonceBytes),
		ExpiresAt: now.Add(oauthStateTTL).Unix(),
	}
	payload := claims.Nonce + "." + strconv.FormatInt(claims.ExpiresAt, 10)
	return payload + "." + s.sign(payload), claims, nil
}

func (s *OAuthStateSigner) Verify(state string, now time.Time) (OAuthStateClaims, error) {
	if s == nil || len(s.key) == 0 {
		return OAuthStateClaims{}, ErrInvalidOAuthState
	}
	state = strings.TrimSpace(state)
	if state == "" || len(state) > maxOAuthStateLength {
		return OAuthStateClaims{}, ErrInvalidOAuthState
	}
	parts := strings.Split(state, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return OAuthStateClaims{}, ErrInvalidOAuthState
	}
	nonce, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(nonce) != oauthStateNonceBytes {
		return OAuthStateClaims{}, ErrInvalidOAuthState
	}
	expiresAt, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || expiresAt <= now.Unix() {
		return OAuthStateClaims{}, ErrInvalidOAuthState
	}
	payload := parts[0] + "." + parts[1]
	want := s.sign(payload)
	if subtle.ConstantTimeCompare([]byte(parts[2]), []byte(want)) != 1 {
		return OAuthStateClaims{}, ErrInvalidOAuthState
	}
	return OAuthStateClaims{Nonce: parts[0], ExpiresAt: expiresAt}, nil
}

func (s *OAuthStateSigner) sign(payload string) string {
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func HashOAuthState(state string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(state)))
	return hex.EncodeToString(sum[:])
}

func ValidateTenantReturnURL(publicBaseURL, returnURL string) error {
	base, err := url.Parse(strings.TrimSpace(publicBaseURL))
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil {
		return errors.New("tenant public URL is invalid")
	}
	target, err := url.Parse(strings.TrimSpace(returnURL))
	if err != nil || target.Scheme != "https" || target.Host == "" || target.User != nil {
		return errors.New("return_url must be an absolute HTTPS URL")
	}
	if !strings.EqualFold(base.Scheme, target.Scheme) || !strings.EqualFold(base.Host, target.Host) || target.Fragment != "" {
		return errors.New("return_url must use the tenant domain without a fragment")
	}
	return nil
}
