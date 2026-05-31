package media

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Signer issues + verifies HMAC tokens for the public /public/media/:id
// endpoint. LINE's servers fetch media via these URLs to deliver to the
// customer; the URLs are short-lived (default 1h) and signed so a leaked URL
// stops working before it can be abused.
//
// Token format: base64url(exp_unix.signature)  where signature = HMAC-SHA256(
//   key, mediaID + "." + exp_unix
// )
type Signer struct {
	key     []byte
	defTTL  time.Duration
}

func NewSigner(key string) *Signer {
	if key == "" {
		key = "nexflow-default-media-key" // safer than empty; main wires real key
	}
	return &Signer{key: []byte(key), defTTL: 1 * time.Hour}
}

// Sign returns a token that authorises GET /public/media/<mediaID> for ttl.
// Pass ttl=0 for the default 1h.
func (s *Signer) Sign(mediaID string, ttl time.Duration) string {
	if ttl <= 0 {
		ttl = s.defTTL
	}
	exp := time.Now().Add(ttl).Unix()
	return s.signWith(mediaID, exp)
}

func (s *Signer) signWith(mediaID string, exp int64) string {
	expStr := strconv.FormatInt(exp, 10)
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(mediaID + "." + expStr))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return expStr + "." + sig
}

// Verify checks token against mediaID and current time.
// Returns nil on success, error otherwise.
func (s *Signer) Verify(mediaID, token string) error {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return fmt.Errorf("malformed token")
	}
	exp, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return fmt.Errorf("malformed exp")
	}
	if time.Now().Unix() > exp {
		return fmt.Errorf("token expired")
	}
	expected := s.signWith(mediaID, exp)
	if !hmac.Equal([]byte(token), []byte(expected)) {
		return fmt.Errorf("bad signature")
	}
	return nil
}

// PublicURL builds the externally-reachable URL for a media row, including a
// fresh signed token. baseURL must be the externally accessible base (e.g.
// Cloudflare Tunnel URL); when empty, returns "" so callers can detect that
// public-URL serving is not configured yet.
func (s *Signer) PublicURL(baseURL, mediaID string) string {
	if baseURL == "" {
		return ""
	}
	tok := s.Sign(mediaID, 0)
	// url-escape the token in case future formats add characters
	return strings.TrimRight(baseURL, "/") +
		"/public/media/" + mediaID + "?t=" + url.QueryEscape(tok)
}
