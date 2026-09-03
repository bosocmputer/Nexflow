package sml

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestGatewayCapabilityClientFetchesSingleVersionedContract(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/capabilities" {
			t.Fatalf("request=%s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Tenant") != "aoy" || r.Header.Get("X-Api-Key") != "secret" {
			t.Fatalf("missing authenticated tenant headers: %+v", r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"contract_revision":"sml-sales-document-profile-v2-20260903","document_profile":{"versions":["sml-document-v1"],"routes":["creditnote","saleinvoice","saleinvoicecancel","saleorder","saleordercancel"],"max_request_bytes":2097152,"max_input_items":500,"max_expanded_items":500,"max_expanded_bytes":2097152,"max_text_characters":255},"cancellation":{"full_document_only":true,"source_lock_wait_seconds":3},"correlation_header":"X-Correlation-ID"}}`))
	}))
	defer server.Close()

	client := NewGatewayCapabilityClient(PartyConfig{BaseURL: server.URL, GUID: "secret", Database: "aoy"}).WithHTTPClient(server.Client())
	capability, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || capability.ContractRevision != SalesProfileContractRevision ||
		!reflect.DeepEqual(capability.DocumentProfile.Routes, SalesDocumentProfileRoutes()) {
		t.Fatalf("calls=%d capability=%+v", calls, capability)
	}
}

func TestGatewayCapabilityClientFailsClosedForOldGateway(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	client := NewGatewayCapabilityClient(PartyConfig{BaseURL: server.URL, GUID: "secret", Database: "aoy"}).WithHTTPClient(server.Client())
	if _, err := client.Fetch(context.Background()); err == nil {
		t.Fatal("old gateway without capability handshake must fail closed")
	}
}

func TestValidateGatewayProfileCapabilityRejectsMismatchOrInactiveRoute(t *testing.T) {
	valid := &GatewayCapabilities{
		ContractRevision: SalesProfileContractRevision,
		DocumentProfile: GatewayDocumentProfileCapability{
			Versions: []string{InvoiceDocumentProfileVersion}, Routes: SalesDocumentProfileRoutes(),
			MaxRequestBytes: MaxInvoiceDocumentBytes, MaxInputItems: 500, MaxExpandedItems: 500, MaxExpandedBytes: MaxInvoiceDocumentBytes,
		},
		Cancellation: GatewayCancellationCapability{FullDocumentOnly: true, SourceLockWaitSeconds: 3},
	}
	active := map[string]string{"saleinvoice": "active", "creditnote": "active"}
	if err := ValidateGatewayProfileCapability(valid, active, []string{"saleinvoice", "creditnote"}, true); err != nil {
		t.Fatalf("valid capability rejected: %v", err)
	}

	wrongRevision := *valid
	wrongRevision.ContractRevision = "old"
	if err := ValidateGatewayProfileCapability(&wrongRevision, active, []string{"saleinvoice"}, true); err == nil {
		t.Fatal("contract revision mismatch must fail closed")
	}
	missingRoute := *valid
	missingRoute.DocumentProfile = valid.DocumentProfile
	missingRoute.DocumentProfile.Routes = []string{"saleinvoice"}
	if err := ValidateGatewayProfileCapability(&missingRoute, active, []string{"creditnote"}, true); err == nil {
		t.Fatal("missing route must fail closed")
	}
	if err := ValidateGatewayProfileCapability(valid, map[string]string{"saleinvoice": "shadow"}, []string{"saleinvoice"}, true); err == nil {
		t.Fatal("automation must reject a non-active route")
	}
}
