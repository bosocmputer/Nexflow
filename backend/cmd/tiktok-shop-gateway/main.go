package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/services/gatewayauth"
	"nexflow/internal/services/tiktokshop"
	"nexflow/internal/tiktokgateway"
)

func main() {
	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	defer func() { _ = logger.Sync() }()

	config, err := tiktokgateway.LoadConfig()
	if err != nil {
		logger.Fatal("tiktok_gateway_config_invalid", zap.Error(err))
	}
	database, err := tiktokgateway.Connect(config.DatabaseURL)
	if err != nil {
		logger.Fatal("tiktok_gateway_database_failed", zap.Error(err))
	}
	defer database.Close()
	repository := tiktokgateway.NewRepository(database)
	tenants, err := tiktokgateway.LoadTenantRegistry(config.TenantRegistryPath)
	if err != nil {
		logger.Fatal("tiktok_gateway_registry_invalid", zap.Error(err))
	}
	if err := repository.SyncTenants(context.Background(), tenants); err != nil {
		logger.Fatal("tiktok_gateway_registry_sync_failed", zap.Error(err))
	}

	externalClient := &http.Client{Timeout: config.ExternalHTTPTimeout}
	tokenClient, err := tiktokshop.NewTokenClient(tiktokshop.TokenClientConfig{
		AppKey: config.AppKey, AppSecret: config.AppSecret, HTTPClient: externalClient,
	})
	if err != nil {
		logger.Fatal("tiktok_gateway_token_client_invalid", zap.Error(err))
	}
	shopClient, err := tiktokshop.NewShopClient(tiktokshop.ShopClientConfig{
		BaseURL: config.TikTokShopBaseURL, AppKey: config.AppKey, AppSecret: config.AppSecret, HTTPClient: externalClient,
	})
	if err != nil {
		logger.Fatal("tiktok_gateway_shop_client_invalid", zap.Error(err))
	}
	stateSigner, err := tiktokgateway.NewOAuthStateSigner(config.OAuthSigningKey)
	if err != nil {
		logger.Fatal("tiktok_gateway_oauth_signer_invalid", zap.Error(err))
	}
	tokenCipher, err := tiktokgateway.NewTokenCipher(config.TokenEncryptionKey)
	if err != nil {
		logger.Fatal("tiktok_gateway_token_cipher_invalid", zap.Error(err))
	}
	oauthService, err := tiktokgateway.NewOAuthService(tiktokgateway.OAuthServiceConfig{
		ServiceID: config.ServiceID, EncryptionKeyVersion: 1,
	}, repository, stateSigner, tokenCipher, tokenClient, shopClient)
	if err != nil {
		logger.Fatal("tiktok_gateway_oauth_service_invalid", zap.Error(err))
	}

	verifier := gatewayauth.Verifier{
		ResolveSecret: func(ctx context.Context, tenant string) (string, error) {
			registered, err := repository.TenantBySlug(ctx, tenant)
			if err != nil || registered == nil || !registered.Enabled {
				return "", fmt.Errorf("tenant unavailable")
			}
			return tiktokgateway.DeriveTenantSecret(config.InternalMasterKey, tenant)
		},
		Nonces: repository, MaxSkew: 5 * time.Minute,
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	tiktokgateway.NewHandler(oauthService, verifier, repository, config, logger).Register(router)

	server := &http.Server{
		Addr: ":" + config.Port, Handler: router,
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
	}
	go func() {
		logger.Info("tiktok_gateway_started", zap.String("port", config.Port), zap.Int("tenant_count", len(tenants)))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("tiktok_gateway_server_failed", zap.Error(err))
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("tiktok_gateway_shutdown_failed", zap.Error(err))
	}
}
