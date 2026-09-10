package tiktokgateway

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

const sellerAuthorizationBaseURL = "https://services.tiktokshop.com/open/authorize"

var serviceIDPattern = regexp.MustCompile(`^[0-9]{1,64}$`)

// BuildSellerAuthorizationURL builds TikTok Shop's rest-of-world seller
// authorization link. The caller owns persisting and consuming the state
// exactly once; this function deliberately does not create a state by itself.
func BuildSellerAuthorizationURL(serviceID, state string) (string, error) {
	serviceID = strings.TrimSpace(serviceID)
	state = strings.TrimSpace(state)
	if !serviceIDPattern.MatchString(serviceID) || state == "" || len(state) > 4096 {
		return "", errors.New("invalid TikTok Shop seller authorization input")
	}
	u, err := url.Parse(sellerAuthorizationBaseURL)
	if err != nil {
		return "", err
	}
	query := u.Query()
	query.Set("service_id", serviceID)
	query.Set("state", state)
	u.RawQuery = query.Encode()
	return u.String(), nil
}
