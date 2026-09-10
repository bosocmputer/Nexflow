package tiktokgateway

import (
	"net/url"
	"testing"
)

func TestBuildSellerAuthorizationURLUsesROWServiceEndpoint(t *testing.T) {
	urlValue, err := BuildSellerAuthorizationURL("7683174272727025429", "state with spaces+symbols")
	if err != nil {
		t.Fatalf("BuildSellerAuthorizationURL() error = %v", err)
	}
	parsed, err := url.Parse(urlValue)
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	if parsed.Scheme != "https" || parsed.Host != "services.tiktokshop.com" || parsed.Path != "/open/authorize" {
		t.Fatalf("authorization URL = %q", urlValue)
	}
	if parsed.Query().Get("service_id") != "7683174272727025429" {
		t.Fatalf("service_id = %q", parsed.Query().Get("service_id"))
	}
	if parsed.Query().Get("state") != "state with spaces+symbols" {
		t.Fatalf("state = %q", parsed.Query().Get("state"))
	}
}

func TestBuildSellerAuthorizationURLRejectsUnsafeInput(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		serviceID string
		state     string
	}{
		{name: "missing service", state: "state"},
		{name: "non numeric service", serviceID: "service/123", state: "state"},
		{name: "missing state", serviceID: "7683174272727025429"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := BuildSellerAuthorizationURL(testCase.serviceID, testCase.state); err == nil {
				t.Fatal("expected invalid authorization URL input")
			}
		})
	}
}
