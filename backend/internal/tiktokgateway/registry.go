package tiktokgateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

type TenantDefinition struct {
	Slug          string
	PublicBaseURL string
	BackendURL    string
}

type registryFile struct {
	Instances []registryInstance `json:"instances"`
}

type registryInstance struct {
	Name              string `json:"name"`
	PublicURL         string `json:"public_url"`
	BackendPort       int    `json:"backend_port"`
	GatewayBackendURL string `json:"gateway_backend_url"`
}

func LoadTenantRegistry(path string) ([]TenantDefinition, error) {
	body, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return nil, fmt.Errorf("read tenant registry: %w", err)
	}
	var raw registryFile
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode tenant registry: %w", err)
	}
	seen := make(map[string]struct{}, len(raw.Instances))
	output := make([]TenantDefinition, 0, len(raw.Instances))
	for _, item := range raw.Instances {
		slug := strings.ToLower(strings.TrimSpace(item.Name))
		if !tenantSlugPattern.MatchString(slug) {
			return nil, fmt.Errorf("invalid tenant slug %q", item.Name)
		}
		if _, exists := seen[slug]; exists {
			return nil, fmt.Errorf("duplicate tenant slug %q", slug)
		}
		seen[slug] = struct{}{}
		publicURL, err := normalizePublicBaseURL(item.PublicURL)
		if err != nil {
			return nil, fmt.Errorf("tenant %s: %w", slug, err)
		}
		backendURL := strings.TrimRight(strings.TrimSpace(item.GatewayBackendURL), "/")
		if backendURL == "" && item.BackendPort > 0 {
			backendURL = fmt.Sprintf("http://172.17.0.1:%d", item.BackendPort)
		}
		if err := validateBackendURL(backendURL); err != nil {
			return nil, fmt.Errorf("tenant %s: %w", slug, err)
		}
		output = append(output, TenantDefinition{Slug: slug, PublicBaseURL: publicURL, BackendURL: backendURL})
	}
	if len(output) == 0 {
		return nil, errors.New("tenant registry is empty")
	}
	return output, nil
}

func validateBackendURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return errors.New("gateway backend URL must be an absolute HTTP(S) URL")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return errors.New("gateway backend URL must not include query or fragment")
	}
	return nil
}
