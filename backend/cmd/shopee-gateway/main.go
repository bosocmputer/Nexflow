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
	"nexflow/internal/services/shopeeapi"
	"nexflow/internal/shopeegateway"
)

func main() {
	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	defer func() { _ = logger.Sync() }()
	cfg, err := shopeegateway.LoadConfig()
	if err != nil {
		logger.Fatal("shopee_gateway_config_invalid", zap.Error(err))
	}
	db, err := shopeegateway.Connect(cfg.DatabaseURL)
	if err != nil {
		logger.Fatal("shopee_gateway_database_failed", zap.Error(err))
	}
	defer db.Close()
	repo := shopeegateway.NewRepository(db)
	tenants, err := shopeegateway.LoadTenantRegistry(cfg.TenantRegistryPath)
	if err != nil {
		logger.Fatal("shopee_gateway_registry_invalid", zap.Error(err))
	}
	if err := repo.SyncTenants(context.Background(), tenants); err != nil {
		logger.Fatal("shopee_gateway_registry_sync_failed", zap.Error(err))
	}
	provider := shopeeapi.New(shopeeapi.Config{
		BaseURL:    cfg.ShopeeBaseURL,
		PartnerID:  cfg.ShopeePartnerID,
		PartnerKey: cfg.ShopeePartnerKey,
		HTTPClient: &http.Client{Timeout: cfg.ExternalHTTPTimeout},
	})
	service, err := shopeegateway.NewService(cfg, repo, provider, logger)
	if err != nil {
		logger.Fatal("shopee_gateway_service_failed", zap.Error(err))
	}
	verifier := gatewayauth.Verifier{
		ResolveSecret: func(ctx context.Context, tenant string) (string, error) {
			registered, err := repo.TenantBySlug(ctx, tenant)
			if err != nil || registered == nil || !registered.Enabled {
				return "", fmt.Errorf("tenant unavailable")
			}
			return shopeegateway.DeriveTenantSecret(cfg.InternalMasterKey, tenant)
		},
		Nonces:  repo,
		MaxSkew: 5 * time.Minute,
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	handler := shopeegateway.NewHandler(service, verifier, repo, repo, cfg, logger)
	handler.Register(router)
	worker := shopeegateway.NewDeliveryWorker(cfg, repo, logger)
	routeSyncWorker := shopeegateway.NewRouteSyncWorker(cfg, repo, logger)
	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()
	go worker.Start(workerCtx, 2*time.Second, 20)
	go routeSyncWorker.Start(workerCtx, time.Minute)
	server := &http.Server{Addr: ":" + cfg.Port, Handler: router, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		logger.Info("shopee_gateway_started", zap.String("port", cfg.Port), zap.String("environment", cfg.Environment), zap.Int("tenant_count", len(tenants)))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("shopee_gateway_server_failed", zap.Error(err))
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("shopee_gateway_shutdown_failed", zap.Error(err))
	}
}
