package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/sml"
)

type staticGatewayCapabilityFetcher struct {
	capability *sml.GatewayCapabilities
	err        error
	calls      int
}

func TestPreviewShopeeSMLRouteBundleReturnsSignedTenMinuteEvidenceWithOneCapabilityRead(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	fetcher := &staticGatewayCapabilityFetcher{capability: validSalesGatewayCapability()}
	h := NewChannelDefaultsHandler(repository.NewChannelDefaultRepo(db), nil, false, "active", zap.NewNop()).
		WithShopeeSMLRouteBundle(fetcher, map[string]string{
			"saleinvoice": "active", "creditnote": "shadow",
		}, strings.Repeat("k", 32), "aoy")
	req := validShopeeSMLBundleRequest()
	body, _ := json.Marshal(req)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/settings/shopee-sml-route-bundle/preview", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	h.PreviewShopeeSMLRouteBundle(ctx)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"preview_token"`) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if fetcher.calls != 1 {
		t.Fatalf("capability calls=%d, want 1", fetcher.calls)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("Preview must not query or write channel settings: %v", err)
	}
}

func TestConfiguredServerRejectsSingleShopeeRouteWrite(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	h := NewChannelDefaultsHandler(repository.NewChannelDefaultRepo(db), nil, false, "active", zap.NewNop()).
		WithShopeeSMLRouteBundle(&staticGatewayCapabilityFetcher{}, map[string]string{}, strings.Repeat("k", 32), "aoy")
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/settings/channel-defaults", bytes.NewReader(validChannelDefaultJSON(nil)))
	ctx.Request.Header.Set("Content-Type", "application/json")
	h.Upsert(ctx)
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "route_bundle_required") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func (f *staticGatewayCapabilityFetcher) Fetch(context.Context) (*sml.GatewayCapabilities, error) {
	f.calls++
	return f.capability, f.err
}

func validSalesGatewayCapability() *sml.GatewayCapabilities {
	return &sml.GatewayCapabilities{
		ContractRevision: sml.SalesProfileContractRevision,
		DocumentProfile: sml.GatewayDocumentProfileCapability{
			Versions: []string{sml.InvoiceDocumentProfileVersion}, Routes: sml.SalesDocumentProfileRoutes(),
			MaxRequestBytes: sml.MaxInvoiceDocumentBytes, MaxInputItems: sml.MaxInvoiceDocumentItems,
			MaxExpandedItems: sml.MaxInvoiceDocumentItems, MaxTextCharacters: 255,
		},
		Cancellation: sml.GatewayCancellationCapability{FullDocumentOnly: true, SourceLockWaitSeconds: 3},
	}
}

func validShopeeSMLBundleRequest() shopeeSMLRouteBundleRequest {
	return shopeeSMLRouteBundleRequest{
		MainRoute: "saleinvoice", CancellationRoute: "creditnote",
		ExpectedMainConfigVersion: 2, ExpectedCancelConfigVersion: 3,
		Main: models.ChannelDefault{
			Endpoint: "/api/v1/ic/sale-invoices", PartyCode: "AR-1", DocFormatCode: "SI",
			DocPrefix: "BF-INV", DocRunningFormat: "YYMM####", WHCode: "AB-1", ShelfCode: "001",
			VATType: 1, VATRate: 7, InquiryType: 0,
		},
		Cancellation: models.ChannelDefault{
			Endpoint: "/api/v1/ic/sale-invoices/:doc_no/cancel", DocFormatCode: "CN",
			DocPrefix: "CN", DocRunningFormat: "YYMM####", VATType: -1, VATRate: -1, InquiryType: -1,
		},
	}
}

func TestNormalizeShopeeSMLRouteBundleRejectsIncompatiblePairAndEndpointInjection(t *testing.T) {
	req := validShopeeSMLBundleRequest()
	req.MainRoute = "saleorder"
	if _, err := normalizeShopeeSMLRouteBundle(req); err == nil || !strings.Contains(err.Error(), "ใช้ร่วมกันไม่ได้") {
		t.Fatalf("incompatible pair error=%v", err)
	}

	req = validShopeeSMLBundleRequest()
	req.Main.Endpoint = "http://169.254.169.254/latest/meta-data"
	if _, err := normalizeShopeeSMLRouteBundle(req); err == nil || !strings.Contains(err.Error(), "ปลายทางเอกสารหลัก") {
		t.Fatalf("absolute endpoint injection error=%v", err)
	}
}

func TestShopeeSMLRouteBundlePreviewTokenBindsTenantPayloadVersionsCapabilityAndModes(t *testing.T) {
	req := validShopeeSMLBundleRequest()
	normalized, err := normalizeShopeeSMLRouteBundle(req)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := normalized.bundleHash()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	claims := shopeeSMLRouteBundlePreviewClaims{
		Tenant: "aoy", PayloadHash: hash, MainConfigVersion: 2, CancelConfigVersion: 3,
		CapabilityRevision: sml.SalesProfileContractRevision, MainMode: "active", CancellationMode: "shadow",
		ExpiresAt: now.Add(10 * time.Minute).Unix(),
	}
	secret := strings.Repeat("s", 32)
	token, err := signShopeeSMLRouteBundlePreview(claims, secret)
	if err != nil {
		t.Fatal(err)
	}
	expected := claims
	expected.ExpiresAt = 0
	if err := validateShopeeSMLRouteBundlePreview(token, secret, expected, now); err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}
	expected.CancellationMode = "active"
	if err := validateShopeeSMLRouteBundlePreview(token, secret, expected, now); err == nil {
		t.Fatal("mode change after Preview must be rejected")
	}
	expected = claims
	expected.ExpiresAt = 0
	if err := validateShopeeSMLRouteBundlePreview(token, secret, expected, now.Add(11*time.Minute)); err == nil {
		t.Fatal("expired Preview token must be rejected")
	}
}

func TestBundleCapabilityReadinessRequiresBothRoutesActive(t *testing.T) {
	h := &ChannelDefaultsHandler{profileRouteModes: map[string]string{
		"saleinvoice": "active", "creditnote": "shadow",
	}}
	req := validShopeeSMLBundleRequest()
	normalized, err := normalizeShopeeSMLRouteBundle(req)
	if err != nil {
		t.Fatal(err)
	}
	readiness := h.bundleReadiness(normalized.Main, normalized.Cancellation, "saleinvoice", "creditnote", validSalesGatewayCapability(), nil)
	if readiness["capability_compatible"] != true || readiness["automation_ready"] != false {
		t.Fatalf("readiness=%+v", readiness)
	}
}
