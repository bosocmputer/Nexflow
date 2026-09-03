package sml

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	SalesProfileContractRevision = "sml-sales-document-profile-v2-20260903"
	maxGatewayCapabilityBytes    = 64 << 10
	defaultCapabilityTimeout     = 3 * time.Second
)

type GatewayDocumentProfileCapability struct {
	Versions          []string `json:"versions"`
	Routes            []string `json:"routes"`
	MaxRequestBytes   int      `json:"max_request_bytes"`
	MaxInputItems     int      `json:"max_input_items"`
	MaxExpandedItems  int      `json:"max_expanded_items"`
	MaxTextCharacters int      `json:"max_text_characters"`
}

type GatewayCancellationCapability struct {
	FullDocumentOnly      bool `json:"full_document_only"`
	SourceLockWaitSeconds int  `json:"source_lock_wait_seconds"`
}

type GatewayCapabilities struct {
	ContractRevision  string                           `json:"contract_revision"`
	DocumentProfile   GatewayDocumentProfileCapability `json:"document_profile"`
	Cancellation      GatewayCancellationCapability    `json:"cancellation"`
	CorrelationHeader string                           `json:"correlation_header"`
}

type GatewayCapabilityClient struct {
	cfg        PartyConfig
	httpClient *http.Client
}

func NewGatewayCapabilityClient(cfg PartyConfig) *GatewayCapabilityClient {
	return &GatewayCapabilityClient{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: defaultCapabilityTimeout},
	}
}

func (c *GatewayCapabilityClient) WithHTTPClient(client *http.Client) *GatewayCapabilityClient {
	if client != nil {
		c.httpClient = client
	}
	return c
}

func (c *GatewayCapabilityClient) Fetch(ctx context.Context) (*GatewayCapabilities, error) {
	if c == nil || strings.TrimSpace(c.cfg.BaseURL) == "" || strings.TrimSpace(c.cfg.GUID) == "" || strings.TrimSpace(c.cfg.Database) == "" {
		return nil, fmt.Errorf("SML Gateway capability client is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.cfg.BaseURL, "/")+"/api/v1/capabilities", nil)
	if err != nil {
		return nil, fmt.Errorf("build SML Gateway capability request: %w", err)
	}
	req.Header.Set("X-Tenant", c.cfg.Database)
	req.Header.Set("X-Api-Key", c.cfg.GUID)
	req.Header.Set("guid", c.cfg.GUID)
	req.Header.Set("provider", c.cfg.Provider)
	req.Header.Set("configFileName", c.cfg.ConfigFile)
	req.Header.Set("databaseName", c.cfg.Database)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("load SML Gateway capabilities: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxGatewayCapabilityBytes))
		return nil, fmt.Errorf("SML Gateway capability handshake returned HTTP %d", resp.StatusCode)
	}
	var wire struct {
		Success bool                `json:"success"`
		Data    GatewayCapabilities `json:"data"`
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, maxGatewayCapabilityBytes))
	if err := decoder.Decode(&wire); err != nil {
		return nil, fmt.Errorf("decode SML Gateway capabilities: %w", err)
	}
	if !wire.Success {
		return nil, fmt.Errorf("SML Gateway capability handshake was not successful")
	}
	return &wire.Data, nil
}

func SalesDocumentProfileRoutes() []string {
	return []string{"creditnote", "saleinvoice", "saleinvoicecancel", "saleorder", "saleordercancel"}
}

// ValidateGatewayProfileCapability fails closed before Preview/Enable when the
// deployed Gateway and this application do not share the exact contract.
func ValidateGatewayProfileCapability(capability *GatewayCapabilities, routeModes map[string]string, requiredRoutes []string, requireActive bool) error {
	if capability == nil {
		return fmt.Errorf("SML Gateway capability is missing")
	}
	if capability.ContractRevision != SalesProfileContractRevision {
		return fmt.Errorf("SML Gateway contract revision mismatch")
	}
	if !containsString(capability.DocumentProfile.Versions, InvoiceDocumentProfileVersion) {
		return fmt.Errorf("SML Gateway does not support %s", InvoiceDocumentProfileVersion)
	}
	if capability.DocumentProfile.MaxRequestBytes < MaxInvoiceDocumentBytes ||
		capability.DocumentProfile.MaxInputItems < MaxInvoiceDocumentItems ||
		capability.DocumentProfile.MaxExpandedItems < MaxInvoiceDocumentItems {
		return fmt.Errorf("SML Gateway document limits are below the required contract")
	}
	if !capability.Cancellation.FullDocumentOnly || capability.Cancellation.SourceLockWaitSeconds != 3 {
		return fmt.Errorf("SML Gateway cancellation semantics are incompatible")
	}
	for _, route := range requiredRoutes {
		if !containsString(capability.DocumentProfile.Routes, route) {
			return fmt.Errorf("SML Gateway does not support route %s", route)
		}
		mode, ok := routeModes[route]
		if !ok {
			return fmt.Errorf("document profile route mode is missing for %s", route)
		}
		if mode != "off" && mode != "shadow" && mode != "active" {
			return fmt.Errorf("document profile route mode is invalid for %s", route)
		}
		if requireActive && mode != "active" {
			return fmt.Errorf("document profile route %s must be active before automation can be enabled", route)
		}
	}
	return nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
