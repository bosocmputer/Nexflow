package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"

	"nexflow/internal/config"
	"nexflow/internal/services/gatewayauth"
	"nexflow/internal/services/shopeeapi"
)

func TestGatewayInternalConnectionRequiresSignedRequest(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewShopeeGatewayInternalHandler(db, &config.Config{ShopeeOpenAPIMode: "gateway", ShopeeGatewayTenant: "aoy", ShopeeGatewayInternalSecret: "secret"}, nil, nil)
	handler.Register(router)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/internal/v1/shopee-gateway/connections/upsert", bytes.NewReader([]byte(`{}`))))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestGatewayInternalConnectionStoresMetadataWithoutTokens(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := &config.Config{
		ShopeeOpenAPIMode: "gateway", ShopeeGatewayTenant: "aoy", ShopeeGatewayInternalSecret: "tenant-secret", ShopeeOpenAPIEnv: "live",
	}
	payload := shopeeapi.GatewayConnectionPayload{
		GatewayConnectionID: "11111111-1111-1111-1111-111111111111",
		ShopID:              99, ShopName: "AOY", Environment: "live",
		AccessExpiresAt:  time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		RefreshExpiresAt: time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
	}
	body, _ := json.Marshal(payload)
	mock.ExpectExec("INSERT INTO shopee_gateway_request_nonces").
		WithArgs("aoy", "nonce-1", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM shopee_gateway_request_nonces").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO shopee_api_connections").
		WithArgs(int64(99), int64(0), "AOY", "AOY", sqlmock.AnyArg(), sqlmock.AnyArg(), "live", payload.GatewayConnectionID, "Shop 99").
		WillReturnResult(sqlmock.NewResult(0, 1))

	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewShopeeGatewayInternalHandler(db, cfg, nil, nil).Register(router)
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/shopee-gateway/connections/upsert", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if err := gatewayauth.Apply(req, "aoy", "tenant-secret", body, time.Now(), "nonce-1"); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
