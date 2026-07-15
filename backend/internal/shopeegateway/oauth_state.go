package shopeegateway

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const oauthStateTTL = 15 * time.Minute

var ErrInvalidOAuthState = errors.New("invalid OAuth state")

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
	nonceBytes := make([]byte, 18)
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
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", OAuthStateClaims{}, err
	}
	payloadEncoded := base64.RawURLEncoding.EncodeToString(payload)
	signature := s.sign(payloadEncoded)
	return payloadEncoded + "." + signature, claims, nil
}

func (s *OAuthStateSigner) Verify(state string, now time.Time) (OAuthStateClaims, error) {
	if s == nil || len(s.key) == 0 {
		return OAuthStateClaims{}, ErrInvalidOAuthState
	}
	parts := strings.Split(strings.TrimSpace(state), ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return OAuthStateClaims{}, ErrInvalidOAuthState
	}
	want := s.sign(parts[0])
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(parts[1])), []byte(want)) != 1 {
		return OAuthStateClaims{}, ErrInvalidOAuthState
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return OAuthStateClaims{}, ErrInvalidOAuthState
	}
	var claims OAuthStateClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return OAuthStateClaims{}, ErrInvalidOAuthState
	}
	if !tenantSlugPattern.MatchString(claims.Tenant) || claims.UserID == "" || claims.ReturnURL == "" || claims.Nonce == "" || claims.ExpiresAt <= now.Unix() {
		return OAuthStateClaims{}, ErrInvalidOAuthState
	}
	return claims, nil
}

func (s *OAuthStateSigner) sign(payload string) string {
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func HashOAuthState(state string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(state)))
	return hex.EncodeToString(sum[:])
}

func ValidateTenantReturnURL(publicBaseURL, returnURL string) error {
	base, err := url.Parse(strings.TrimSpace(publicBaseURL))
	if err != nil || base.Scheme != "https" || base.Host == "" {
		return errors.New("tenant public URL is invalid")
	}
	target, err := url.Parse(strings.TrimSpace(returnURL))
	if err != nil || target.Scheme != "https" || target.Host == "" || target.User != nil {
		return errors.New("return_url must be an absolute HTTPS URL")
	}
	if !strings.EqualFold(base.Scheme, target.Scheme) || !strings.EqualFold(base.Host, target.Host) {
		return errors.New("return_url must use the tenant domain")
	}
	if target.Fragment != "" {
		return errors.New("return_url must not include a fragment")
	}
	return nil
}
