package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"

	"nexflow/internal/config"
	"nexflow/internal/database"
	"nexflow/internal/handlers"
	"nexflow/internal/jobs"
	"nexflow/internal/middleware"
	"nexflow/internal/models"
	"nexflow/internal/repository"
	"nexflow/internal/services/ai"
	"nexflow/internal/services/anomaly"
	"nexflow/internal/services/artifact"
	"nexflow/internal/services/catalog"
	emailservice "nexflow/internal/services/email"
	"nexflow/internal/services/events"
	"nexflow/internal/services/insight"
	lineservice "nexflow/internal/services/line"
	linenotify "nexflow/internal/services/line_notifications"
	"nexflow/internal/services/mapper"
	"nexflow/internal/services/media"
	"nexflow/internal/services/mistral"
	"nexflow/internal/services/sml"
	"nexflow/internal/worker"
)

func main() {
	cfg := config.Load()

	// Logger
	var logger *zap.Logger
	var err error
	if cfg.Env == "production" {
		logger, err = zap.NewProduction()
	} else {
		logger, err = zap.NewDevelopment()
	}
	if err != nil {
		log.Fatal("init logger:", err)
	}
	defer logger.Sync()

	// Database
	db, err := database.Connect(cfg.DatabaseURL)
	if err != nil {
		logger.Fatal("database connect", zap.Error(err))
	}
	defer db.Close()
	appCtx, stopBackgroundJobs := context.WithCancel(context.Background())
	defer stopBackgroundJobs()

	seedAdminUser(db, logger)

	// On boot, fail any outgoing chat_messages stuck in 'pending' for >5 min.
	// These rows happen when the server crashes mid-send; without cleanup they
	// stay "กำลังส่ง…" in the UI forever. 5min covers slow LINE Push without
	// false positives for normal traffic (Reply/Push complete in ms).
	if res, err := db.Exec(
		`UPDATE chat_messages
		   SET delivery_status = 'failed',
		       delivery_error  = COALESCE(NULLIF(delivery_error, ''), 'server restart or send timeout')
		 WHERE direction = 'outgoing'
		   AND delivery_status = 'pending'
		   AND created_at < NOW() - INTERVAL '5 minutes'`,
	); err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			logger.Info("startup_pending_cleanup", zap.Int64("rows", n))
		}
	} else {
		logger.Warn("startup pending cleanup", zap.Error(err))
	}

	// Repositories
	userRepo := repository.NewUserRepo(db)
	billRepo := repository.NewBillRepo(db)
	mappingRepo := repository.NewMappingRepo(db)
	insightRepo := repository.NewInsightRepo(db)
	platformRepo := repository.NewPlatformMappingRepo(db)
	auditLogRepo := repository.NewAuditLogRepo(db)
	catalogRepo := repository.NewSMLCatalogRepo(db)
	aliasRepo := repository.NewMarketplaceAliasRepo(db)
	artifactRepo := repository.NewBillArtifactRepo(db)
	channelDefaultRepo := repository.NewChannelDefaultRepo(db)
	docCounterRepo := repository.NewDocCounterRepo(db)
	chatConvRepo := repository.NewChatConversationRepo(db)
	chatMessageRepo := repository.NewChatMessageRepo(db)
	chatMediaRepo := repository.NewChatMediaRepo(db, cfg.ArtifactsDir, cfg.ArtifactsMaxBytes)
	chatQuickReplyRepo := repository.NewChatQuickReplyRepo(db)
	chatNoteRepo := repository.NewChatNoteRepo(db)
	chatTagRepo := repository.NewChatTagRepo(db)
	lineOARepo := repository.NewLineOAAccountRepo(db)
	lineNotificationRepo := repository.NewLineNotificationRepo(db)
	appSettingsRepo := repository.NewAppSettingsRepo(db)
	aiUsageRepo := repository.NewAIUsageRepo(db)
	shopeeRealtimeRepo := repository.NewShopeeRealtimeRepo(db)
	notificationRepo := repository.NewNotificationRepo(db)
	if err := appSettingsRepo.ApplyToConfig(cfg); err != nil {
		logger.Warn("apply DB instance settings", zap.Error(err))
	}
	if n, err := billRepo.BackfillShopeePurchaseDiscounts(); err != nil {
		logger.Warn("startup shopee purchase discount backfill failed", zap.Error(err))
	} else if n > 0 {
		logger.Info("startup shopee purchase discount backfilled", zap.Int("bills", n))
	}
	if n, err := billRepo.BackfillShopeePurchasePaymentSummaries(); err != nil {
		logger.Warn("startup shopee purchase payment summary backfill failed", zap.Error(err))
	} else if n > 0 {
		logger.Info("startup shopee purchase payment summary backfilled", zap.Int("bills", n))
	}
	smlReadiness := sml.NewReadinessChecker(sml.PartyConfig{
		BaseURL:    cfg.ShopeeSMLURL,
		GUID:       cfg.ShopeeSMLGUID,
		Provider:   cfg.ShopeeSMLProvider,
		ConfigFile: cfg.ShopeeSMLConfigFile,
		Database:   cfg.ShopeeSMLDatabase,
	}, logger)
	setupH := handlers.NewSetupHandler(db, cfg, appSettingsRepo, auditLogRepo, smlReadiness, logger)

	// Services
	aiClient := ai.NewClient(cfg.OpenRouterAPIKey, cfg.OpenRouterModel, cfg.OpenRouterFallback, cfg.OpenRouterAudioModel).
		WithAppAttribution(cfg.OpenRouterAppTitle, cfg.OpenRouterAppReferer).
		WithUsageLogger(aiUsageRepo)
	mapperSvc := mapper.New(mappingRepo)
	anomalySvc := anomaly.New(billRepo).WithCustomerLookup(billRepo)
	insightSvc := insight.New(aiClient)
	artifactSvc := artifact.New(cfg.ArtifactsDir, cfg.ArtifactsMaxBytes, artifactRepo, logger)
	pool := worker.New()

	// Shopee SML 248 REST clients — saleorder (default sale path), saleinvoice
	// (kept for admins who pin endpoint='saleinvoice' on a channel) + purchaseorder.
	// CustCode is filled at request time from channel_defaults — see handlers/bills.go.
	invoiceClient := sml.NewInvoiceClient(sml.InvoiceConfig{
		BaseURL:    cfg.ShopeeSMLURL,
		GUID:       cfg.ShopeeSMLGUID,
		Provider:   cfg.ShopeeSMLProvider,
		ConfigFile: cfg.ShopeeSMLConfigFile,
		Database:   cfg.ShopeeSMLDatabase,
		DocFormat:  cfg.ShopeeSMLDocFormat,
		SaleCode:   cfg.ShopeeSMLSaleCode,
		BranchCode: cfg.ShopeeSMLBranchCode,
		WHCode:     cfg.ShopeeSMLWHCode,
		ShelfCode:  cfg.ShopeeSMLShelfCode,
		UnitCode:   cfg.ShopeeSMLUnitCode,
		VATType:    cfg.ShopeeSMLVATType,
		VATRate:    cfg.ShopeeSMLVATRate,
		DocTime:    cfg.ShopeeSMLDocTime,
	}, logger)
	saleOrderClient := sml.NewSaleOrderClient(sml.SaleOrderConfig{
		BaseURL:    cfg.ShopeeSMLURL,
		GUID:       cfg.ShopeeSMLGUID,
		Provider:   cfg.ShopeeSMLProvider,
		ConfigFile: cfg.ShopeeSMLConfigFile,
		Database:   cfg.ShopeeSMLDatabase,
		DocFormat:  cfg.ShopeeSMLDocFormat,
		SaleCode:   cfg.ShopeeSMLSaleCode,
		BranchCode: cfg.ShopeeSMLBranchCode,
		WHCode:     cfg.ShopeeSMLWHCode,
		ShelfCode:  cfg.ShopeeSMLShelfCode,
		UnitCode:   cfg.ShopeeSMLUnitCode,
		VATType:    cfg.ShopeeSMLVATType,
		VATRate:    cfg.ShopeeSMLVATRate,
		DocTime:    cfg.ShopeeSMLDocTime,
	}, logger)
	productClient := sml.NewProductClient(
		cfg.ShopeeSMLURL,
		cfg.ShopeeSMLGUID,
		cfg.ShopeeSMLProvider,
		cfg.ShopeeSMLConfigFile,
		cfg.ShopeeSMLDatabase,
		logger,
	)
	poClient := sml.NewPurchaseOrderClient(sml.PurchaseOrderConfig{
		BaseURL:    cfg.ShopeeSMLURL,
		GUID:       cfg.ShopeeSMLGUID,
		Provider:   cfg.ShopeeSMLProvider,
		ConfigFile: cfg.ShopeeSMLConfigFile,
		Database:   cfg.ShopeeSMLDatabase,
		DocFormat:  cfg.ShippedSMLDocFormat,
		SaleCode:   cfg.ShopeeSMLSaleCode,
		BranchCode: cfg.ShopeeSMLBranchCode,
		WHCode:     cfg.ShopeeSMLWHCode,
		ShelfCode:  cfg.ShopeeSMLShelfCode,
		UnitCode:   cfg.ShopeeSMLUnitCode,
		VATType:    cfg.ShopeeSMLVATType,
		VATRate:    cfg.ShopeeSMLVATRate,
		DocTime:    cfg.ShopeeSMLDocTime,
	}, logger)

	// SML party cache — fetches all customer + supplier records from SML 248
	// at boot, refreshes every 6 h. Powers the /settings/channels picker.
	partyClient := sml.NewPartyClient(sml.PartyConfig{
		BaseURL:    cfg.ShopeeSMLURL,
		GUID:       cfg.ShopeeSMLGUID,
		Provider:   cfg.ShopeeSMLProvider,
		ConfigFile: cfg.ShopeeSMLConfigFile,
		Database:   cfg.ShopeeSMLDatabase,
	}, logger)
	partyCache := sml.NewPartyCache(partyClient, logger)
	partyCache.Start(context.Background())
	docNoClient := sml.NewDocNoClient(sml.PartyConfig{
		BaseURL:  cfg.ShopeeSMLURL,
		GUID:     cfg.ShopeeSMLGUID,
		Database: cfg.ShopeeSMLDatabase,
	}, logger)

	// SML warehouse cache — powers the Bill Detail send dialog warehouse/shelf
	// pickers. If the SML v4 warehouse service is not deployed yet, startup
	// continues and the UI can still fall back to manual code entry.
	warehouseClient := sml.NewWarehouseClient(sml.PartyConfig{
		BaseURL:    cfg.ShopeeSMLURL,
		GUID:       cfg.ShopeeSMLGUID,
		Provider:   cfg.ShopeeSMLProvider,
		ConfigFile: cfg.ShopeeSMLConfigFile,
		Database:   cfg.ShopeeSMLDatabase,
	}, logger)
	warehouseCache := sml.NewWarehouseCache(warehouseClient, logger)
	warehouseCache.Start(context.Background())

	// SML catalog services for Shopee email smart matching
	smlHeaders := map[string]string{
		"guid":           cfg.ShopeeSMLGUID,
		"provider":       cfg.ShopeeSMLProvider,
		"configFileName": cfg.ShopeeSMLConfigFile,
		"databaseName":   cfg.ShopeeSMLDatabase,
	}
	catalogSvc := catalog.NewSMLCatalogService(catalogRepo, cfg.ShopeeSMLURL, smlHeaders, logger)
	embSvc := catalog.NewEmbeddingService(cfg.OpenRouterAPIKey).
		WithAppAttribution(cfg.OpenRouterAppTitle, cfg.OpenRouterAppReferer).
		WithUsageLogger(aiUsageRepo)
	catalogIdx := catalog.NewCatalogIndex()
	// Load existing embeddings into memory in the background. This can be
	// expensive after a full catalog sync, so it must not block HTTP startup.
	go func() {
		if err := catalogIdx.Reload(catalogRepo); err != nil {
			logger.Warn("catalog: reload index at startup", zap.Error(err))
			return
		}
		logger.Info("catalog: index loaded", zap.Int("size", catalogIdx.Size()))
	}()

	// LINE service (legacy single instance) — kept for PushAdmin paths used by
	// insight cron, disk monitor, and email coordinator error notifications.
	// The chat inbox uses lineRegistry instead so each conversation routes to
	// the right OA's access_token.
	var lineSvc *lineservice.Service
	if cfg.LineChannelSecret != "" && cfg.LineChannelAccessToken != "" {
		lineSvc, err = lineservice.New(cfg.LineChannelSecret, cfg.LineChannelAccessToken, cfg.LineAdminUserID)
		if err != nil {
			logger.Warn("LINE service init failed", zap.Error(err))
		}
	}

	// Multi-OA registry. Seeds a default OA from LINE_* env vars on first boot
	// (when line_oa_accounts is empty) so existing single-OA installs keep
	// working without admin intervention.
	lineRegistry := lineservice.NewRegistry(lineOARepo, logger)
	if empty, _ := lineOARepo.IsEmpty(); empty {
		if cfg.LineChannelSecret != "" && cfg.LineChannelAccessToken != "" {
			seed := &models.LineOAAccount{
				Name:               "Default (from .env)",
				ChannelSecret:      cfg.LineChannelSecret,
				ChannelAccessToken: cfg.LineChannelAccessToken,
				AdminUserID:        cfg.LineAdminUserID,
				Greeting:           cfg.LineGreeting,
				Enabled:            true,
			}
			if err := lineOARepo.Create(seed); err != nil {
				logger.Warn("seed default LINE OA failed", zap.Error(err))
			} else {
				logger.Info("seeded default LINE OA from env",
					zap.String("oa_id", seed.ID),
					zap.String("name", seed.Name))
			}
		}
	}
	if err := lineRegistry.Reload(); err != nil {
		logger.Warn("LINE OA registry initial reload failed", zap.Error(err))
	}

	// Email service — multi-account coordinator. Reads imap_accounts table
	// at boot, spawns one poller goroutine per enabled row. Admin edits
	// flow back through ReloadAccount/RemoveAccount via the settings API.
	imapAccountRepo := repository.NewImapAccountRepo(db)
	imapPollJobRepo := repository.NewIMAPPollJobRepo(db)
	if n, err := imapPollJobRepo.RecoverInterrupted(); err != nil {
		logger.Warn("recover interrupted imap poll jobs failed", zap.Error(err))
	} else if n > 0 {
		logger.Warn("recovered interrupted imap poll jobs", zap.Int64("jobs", n))
	}
	imapProcessors := &emailservice.Processors{
		Attachment:    nil, // wired below once emailH is built
		ShopeeOrder:   nil,
		ShopeeShipped: nil,
	}
	imapCoordinator := emailservice.NewCoordinator(imapAccountRepo, imapPollJobRepo, imapProcessors, lineSvc, logger)

	// Mistral OCR service (optional — used for PDF extraction)
	ocrClient := mistral.New(cfg.MistralAPIKey)

	// JWT
	middleware.SetJWTSecret(cfg.JWTSecret)

	// Gin
	if cfg.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.Logger(logger))

	// CORS
	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization,Content-Type")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})

	// Health
	r.GET("/health", func(c *gin.Context) {
		dbStatus := "ok"
		if err := db.PingContext(c.Request.Context()); err != nil {
			dbStatus = "error: " + err.Error()
		}
		status := "ok"
		if dbStatus != "ok" {
			status = "degraded"
		}
		c.JSON(http.StatusOK, gin.H{
			"status":   status,
			"env":      cfg.Env,
			"database": dbStatus,
		})
	})

	// Handlers
	authH := handlers.NewAuthHandler(userRepo, cfg.JWTExpireHours, logger)
	smlBulkJobRepo := repository.NewSMLBulkJobRepo(db)
	billH := handlers.NewBillHandler(billRepo, mapperSvc, invoiceClient, saleOrderClient, poClient, docNoClient, cfg, lineSvc, auditLogRepo, catalogRepo, channelDefaultRepo, docCounterRepo, smlBulkJobRepo, artifactSvc, warehouseCache, smlReadiness, appSettingsRepo, logger)
	billH.RecoverInterruptedBulkSendJobs()
	mappingH := handlers.NewMappingHandler(mappingRepo, mapperSvc, catalogRepo, auditLogRepo, logger)
	dashH := handlers.NewDashboardHandler(billRepo, insightRepo, chatConvRepo, imapAccountRepo, lineOARepo, insightSvc, logger)
	dashH.SetSMLReadiness(smlReadiness)
	imapConfigured := false
	if accs, err := imapAccountRepo.ListEnabled(); err == nil && len(accs) > 0 {
		imapConfigured = true
	}
	dashH.SetConfigStatus(
		cfg.LineChannelSecret != "" && cfg.LineChannelAccessToken != "",
		imapConfigured,
		cfg.ShopeeSMLURL != "",
		cfg.OpenRouterAPIKey != "",
		cfg.AutoConfirmThreshold,
	)
	// Media signer for /public/media/:id?t=<token>. Falls back to JWT_SECRET
	// when MEDIA_SIGNING_KEY is empty so single-secret deployments work.
	mediaKey := cfg.MediaSigningKey
	if mediaKey == "" {
		mediaKey = cfg.JWTSecret
	}
	mediaSigner := media.NewSigner(mediaKey)

	// In-process pub/sub for SSE — webhook handlers + admin actions Publish,
	// /api/admin/events subscribers stream events to admin browsers.
	eventBroker := events.NewBroker()

	lineH := handlers.NewLineHandler(lineRegistry, chatConvRepo, chatMessageRepo, chatMediaRepo, auditLogRepo, pool, cfg, eventBroker, logger)
	chatInboxH := handlers.NewChatInboxHandler(chatConvRepo, chatMessageRepo, chatMediaRepo, billRepo, auditLogRepo, lineRegistry, aiClient, ocrClient, mediaSigner, eventBroker, cfg.PublicBaseURL, logger)
	publicMediaH := handlers.NewPublicMediaHandler(chatMediaRepo, mediaSigner, logger)
	sseH := handlers.NewSSEHandler(eventBroker, mediaSigner)
	notificationH := handlers.NewNotificationHandler(notificationRepo, eventBroker)
	lineNotificationSvc := linenotify.NewService(lineNotificationRepo, cfg.PublicBaseURL, logger)
	emailH := handlers.NewEmailHandler(aiClient, ocrClient, mapperSvc, anomalySvc, billRepo, auditLogRepo, lineSvc, cfg.AutoConfirmThreshold, logger)
	emailH.SetCatalogServices(catalogSvc, embSvc, catalogIdx, catalogRepo)
	emailH.SetChannelDefaults(channelDefaultRepo)
	emailH.SetArtifactService(artifactSvc)
	catalogH := handlers.NewCatalogHandler(catalogSvc, embSvc, catalogIdx, catalogRepo, productClient, auditLogRepo, appSettingsRepo, cfg, cfg.AutoConfirmThreshold, logger)
	go func() {
		time.Sleep(3 * time.Second)
		started, err := catalogH.StartEmbedAll("startup_auto_resume")
		if err != nil {
			logger.Warn("catalog auto-resume skipped", zap.Error(err))
			return
		}
		if started {
			logger.Info("catalog auto-resume started")
		}
	}()
	importH := handlers.NewImportHandler(platformRepo, mapperSvc, anomalySvc, saleOrderClient, billRepo, channelDefaultRepo, docCounterRepo, cfg, cfg.AutoConfirmThreshold, logger)
	shopeeH := handlers.NewShopeeImportHandler(db, billRepo, mappingRepo, auditLogRepo, cfg, channelDefaultRepo, catalogSvc, embSvc, catalogIdx, catalogRepo, logger)
	shopeeH.SetArtifactService(artifactSvc)
	shopeeRealtimeH := handlers.NewShopeeRealtimeHandler(shopeeRealtimeRepo, notificationRepo, eventBroker, shopeeH, billH, cfg, logger)
	shopeeRealtimeH.SetLineNotifier(lineNotificationSvc)
	billH.SetShopeeRealtimeSync(shopeeRealtimeRepo, eventBroker)
	lazadaH := handlers.NewLazadaImportHandler(billRepo, mappingRepo, auditLogRepo, cfg, channelDefaultRepo, catalogSvc, embSvc, catalogIdx, catalogRepo, logger)
	lazadaH.SetArtifactService(artifactSvc)
	tiktokH := handlers.NewTikTokImportHandler(billRepo, mappingRepo, auditLogRepo, cfg, channelDefaultRepo, catalogSvc, embSvc, catalogIdx, catalogRepo, logger)
	tiktokH.SetArtifactService(artifactSvc)
	aliasH := handlers.NewMarketplaceAliasHandler(aliasRepo, catalogRepo, auditLogRepo, logger)
	settingsH := handlers.NewSettingsHandler(platformRepo, logger)
	instanceSettingsH := handlers.NewInstanceSettingsHandler(appSettingsRepo, cfg, logger)
	imapSettingsH := handlers.NewIMAPSettingsHandler(imapAccountRepo, imapPollJobRepo, imapCoordinator, logger)
	channelDefaultsH := handlers.NewChannelDefaultsHandler(channelDefaultRepo, auditLogRepo, logger)
	smlPartyH := handlers.NewSMLPartyHandler(partyCache, partyClient, auditLogRepo, logger)
	smlPartyH.SetSMLConfig(cfg.ShopeeSMLURL, cfg.ShopeeSMLGUID, cfg.ShopeeSMLDatabase)
	smlWarehouseH := handlers.NewSMLWarehouseHandler(warehouseCache, logger)
	logH := handlers.NewLogHandler(auditLogRepo, logger)
	aiUsageH := handlers.NewAIUsageHandler(aiUsageRepo, logger)
	userSettingsH := handlers.NewUserSettingsHandler(userRepo, auditLogRepo, logger)

	// Webhooks (no auth)
	// Webhook routes:
	//   /webhook/line/:oaId  → multi-OA URL (admin pastes from /settings/line-oa)
	//   /webhook/line        → legacy single-OA fallback (resolves via Destination → Any())
	r.POST("/webhook/line/:oaId", lineH.Webhook)
	r.POST("/webhook/line", lineH.Webhook)
	r.POST("/webhook/shopee", shopeeRealtimeH.Webhook)

	// Public media endpoint — NO JWT, the HMAC token IS the auth.
	// LINE servers fetch this URL to deliver image messages to customers.
	r.GET("/public/media/:mediaID", publicMediaH.Serve)

	// Shopee Open API OAuth callback — NO JWT. Auth is the short-lived
	// state generated by POST /api/shopee-api/auth-url.
	r.GET("/api/shopee-api/callback", shopeeH.APICallback)

	// SSE stream — NO JWT, the ?t=<token> query param IS the auth (since
	// EventSource doesn't support custom headers). Admin first calls
	// POST /api/admin/events/token (JWT-authenticated, see below) to get a
	// short-lived signed token, then opens EventSource with ?u=<userID>&t=<token>.
	r.GET("/api/admin/events", sseH.Stream)

	// Auth (rate-limited: 10 req/min per IP)
	r.POST("/api/auth/login", middleware.AuthRateLimit(10, time.Minute), authH.Login)

	// Protected routes
	api := r.Group("/api", middleware.Auth())
	{
		api.GET("/auth/me", authH.Me)

		// SSE token issuer — admin POSTs to get a short-lived HMAC token that
		// EventSource uses as ?t=<token> on /api/admin/events. JWT-protected.
		api.POST("/admin/events/token", sseH.IssueToken)

		// In-app notifications — scoped to the logged-in operator.
		api.GET("/notifications", middleware.RequireRole("admin", "staff"), notificationH.List)
		api.GET("/notifications/count", middleware.RequireRole("admin", "staff"), notificationH.Count)
		api.POST("/notifications/:id/read", middleware.RequireRole("admin", "staff"), notificationH.MarkRead)
		api.POST("/notifications/read-all", middleware.RequireRole("admin", "staff"), notificationH.MarkAllRead)

		// LINE order notifications — admin-only, separate from LINE chat feature.
		lineNotificationH := handlers.NewLineNotificationHandler(lineOARepo, lineNotificationRepo, lineRegistry, auditLogRepo, cfg, logger)
		lineNotificationGroup := api.Group("/settings/line-notifications")
		lineNotificationGroup.Use(middleware.RequireRole("admin"))
		{
			lineNotificationGroup.GET("", lineNotificationH.Overview)
			lineNotificationGroup.POST("/senders", lineNotificationH.CreateSender)
			lineNotificationGroup.PUT("/senders/:id", lineNotificationH.UpdateSender)
			lineNotificationGroup.POST("/senders/:id/test", lineNotificationH.TestSender)
			lineNotificationGroup.POST("/recipients", lineNotificationH.CreateRecipient)
			lineNotificationGroup.PUT("/recipients/:id", lineNotificationH.UpdateRecipient)
			lineNotificationGroup.DELETE("/recipients/:id", lineNotificationH.DeleteRecipient)
			lineNotificationGroup.POST("/recipients/:id/test", lineNotificationH.TestRecipient)
		}

		// Old Data (archive / purge) — admin only
		oldDataH := handlers.NewOldDataHandler(db, logger)
		api.GET("/bills/old-data/summary", middleware.RequireRole("admin"), oldDataH.Summary)
		api.POST("/bills/old-data/archive", middleware.RequireRole("admin"), oldDataH.Archive)
		api.POST("/bills/old-data/purge", middleware.RequireRole("admin"), oldDataH.Purge)

		// Bills
		api.GET("/bills", billH.List)
		api.GET("/bills/counts", billH.Counts)
		api.POST("/bills/bulk-send-jobs", middleware.RequireRole("admin", "staff"), billH.CreateBulkSendJob)
		api.GET("/bills/bulk-send-jobs", middleware.RequireRole("admin", "staff"), billH.ListBulkSendJobs)
		api.GET("/bills/bulk-send-jobs/active", middleware.RequireRole("admin", "staff"), billH.GetActiveBulkSendJob)
		api.GET("/bills/bulk-send-jobs/:job_id", middleware.RequireRole("admin", "staff"), billH.GetBulkSendJob)
		api.POST("/bills/bulk-send-jobs/:job_id/retry-failed", middleware.RequireRole("admin", "staff"), billH.RetryFailedBulkSendJob)
		api.GET("/bills/:id", billH.Get)
		api.GET("/bills/:id/timeline", billH.Timeline)
		api.POST("/bills/:id/retry", billH.Retry)
		api.POST("/bills/:id/ensure-shopee-shipping-line", middleware.RequireRole("admin", "staff"), billH.EnsureShopeeShippingLine)
		api.GET("/bills/:id/latest-doc-no", middleware.RequireRole("admin", "staff"), billH.LatestDocNo)
		api.POST("/bills/:id/regenerate-doc-no", middleware.RequireRole("admin", "staff"), billH.RegenerateDocNo)
		api.POST("/bills/:id/shopee-realtime/recreate-route", middleware.RequireRole("admin", "staff"), billH.RecreateShopeeRealtimeDocumentRoute)
		api.POST("/bills/:id/archive", middleware.RequireRole("admin", "staff"), billH.Archive)
		api.POST("/bills/:id/restore", middleware.RequireRole("admin", "staff"), billH.Restore)
		api.DELETE("/bills/:id", middleware.RequireRole("admin"), billH.Delete)
		api.PUT("/bills/:id/items/:item_id", middleware.RequireRole("admin", "staff"), billH.UpdateItem)
		api.POST("/bills/:id/items", middleware.RequireRole("admin", "staff"), billH.AddItem)
		api.DELETE("/bills/:id/items/:item_id", middleware.RequireRole("admin", "staff"), billH.DeleteItemRow)
		api.GET("/bills/:id/artifacts", billH.ListArtifacts)
		api.GET("/bills/:id/artifacts/:artifact_id/download", billH.DownloadArtifact)
		api.GET("/bills/:id/artifacts/:artifact_id/preview", billH.PreviewArtifact)
		api.POST("/bills/:id/artifacts/:artifact_id/print-events", billH.RecordArtifactPrint)

		// Mappings
		api.GET("/mappings", mappingH.List)
		api.POST("/mappings", middleware.RequireRole("admin", "staff"), mappingH.Create)
		api.PUT("/mappings/:id", middleware.RequireRole("admin", "staff"), mappingH.Update)
		api.DELETE("/mappings/:id", middleware.RequireRole("admin"), mappingH.Delete)
		api.GET("/mappings/stats", mappingH.Stats)
		api.POST("/mappings/feedback", middleware.RequireRole("admin", "staff"), mappingH.Feedback)

		// Dashboard
		api.GET("/dashboard/stats", dashH.Stats)
		api.GET("/dashboard/insights", dashH.Insights)
		api.POST("/dashboard/insights/generate", middleware.RequireRole("admin"), dashH.GenerateInsight)

		// Settings
		api.GET("/settings/status", dashH.SettingsStatus)
		api.GET("/setup/status", middleware.RequireRole("admin"), setupH.Status)
		api.POST("/setup/reset-test-data", middleware.RequireRole("admin"), setupH.ResetTestData)
		api.GET("/settings/instance", middleware.RequireRole("admin"), instanceSettingsH.Get)
		api.PUT("/settings/instance", middleware.RequireRole("admin"), instanceSettingsH.Update)
		api.POST("/settings/instance/restart", middleware.RequireRole("admin"), instanceSettingsH.Restart)
		api.POST("/settings/instance/test-connection", middleware.RequireRole("admin"), instanceSettingsH.TestConnection)

		// Logs (Activity Log)
		api.GET("/logs", middleware.RequireRole("admin", "staff"), logH.List)
		api.GET("/ai-usage/summary", middleware.RequireRole("admin"), aiUsageH.Summary)
		api.GET("/ai-usage/logs", middleware.RequireRole("admin"), aiUsageH.Logs)

		// Import (Lazada)
		api.POST("/import/upload", middleware.RequireRole("admin", "staff"), importH.Upload)
		api.POST("/import/confirm", middleware.RequireRole("admin", "staff"), importH.Confirm)

		// Shopee import — saleinvoice REST API (SML 224)
		api.GET("/settings/shopee-config", shopeeH.GetConfig)
		api.GET("/settings/shopee-api/status", middleware.RequireRole("admin", "staff"), shopeeH.GetAPIStatus)
		api.GET("/settings/shopee-settlement-defaults", middleware.RequireRole("admin", "staff"), shopeeH.GetSettlementDefaults)
		api.PUT("/settings/shopee-settlement-defaults", middleware.RequireRole("admin"), shopeeH.UpdateSettlementDefaults)
		api.GET("/shopee-api/connections", middleware.RequireRole("admin", "staff"), shopeeH.ListAPIConnections)
		api.PATCH("/shopee-api/connections/:id", middleware.RequireRole("admin"), shopeeH.UpdateAPIConnection)
		api.POST("/shopee-api/auth-url", middleware.RequireRole("admin"), shopeeH.CreateAPIAuthURL)
		api.GET("/shopee-settlements", middleware.RequireRole("admin", "staff"), shopeeH.ListSettlementRuns)
		api.GET("/shopee-settlements/counts", middleware.RequireRole("admin", "staff"), shopeeH.SettlementRunCounts)
		api.POST("/shopee-settlements/preview", middleware.RequireRole("admin", "staff"), shopeeH.CreateSettlementPreview)
		api.GET("/shopee-settlements/:id", middleware.RequireRole("admin", "staff"), shopeeH.GetSettlementRun)
		api.POST("/shopee-settlements/:id/reconcile", middleware.RequireRole("admin", "staff"), shopeeH.ReconcileSettlementRun)
		api.POST("/shopee-settlements/:id/send", middleware.RequireRole("admin", "staff"), shopeeH.SendSettlementRun)
		api.POST("/shopee-settlements/:id/hide", middleware.RequireRole("admin", "staff"), shopeeH.HideSettlementRun)
		api.POST("/shopee-settlements/:id/restore", middleware.RequireRole("admin", "staff"), shopeeH.RestoreSettlementRun)
		api.GET("/import/shopee/runs", middleware.RequireRole("admin", "staff"), shopeeH.ListRuns)
		api.POST("/import/shopee/preview", middleware.RequireRole("admin", "staff"), shopeeH.Preview)
		api.POST("/import/shopee/api/preview", middleware.RequireRole("admin", "staff"), shopeeH.PreviewFromAPI)
		api.POST("/import/shopee/confirm", middleware.RequireRole("admin", "staff"), shopeeH.Confirm)
		api.GET("/shopee-operations/readiness", middleware.RequireRole("admin", "staff"), shopeeRealtimeH.Readiness)
		api.GET("/shopee-operations/orders", middleware.RequireRole("admin", "staff"), shopeeRealtimeH.ListOrders)
		api.GET("/shopee-operations/counts", middleware.RequireRole("admin", "staff"), shopeeRealtimeH.Counts)
		api.POST("/shopee-operations/sync", middleware.RequireRole("admin", "staff"), shopeeRealtimeH.SyncNow)
		api.POST("/shopee-operations/:shop_id/:order_sn/create-document", middleware.RequireRole("admin", "staff"), shopeeRealtimeH.CreateDocument)
		api.POST("/shopee-operations/:shop_id/:order_sn/save-erp", middleware.RequireRole("admin", "staff"), shopeeRealtimeH.SaveERP)
		api.GET("/shopee-operations/:shop_id/:order_sn/shipping-parameters", middleware.RequireRole("admin", "staff"), shopeeRealtimeH.ShippingParameters)
		api.POST("/shopee-operations/:shop_id/:order_sn/reconcile-shipping", middleware.RequireRole("admin", "staff"), shopeeRealtimeH.ReconcileShipping)
		api.GET("/shopee-operations/:shop_id/:order_sn/tracking", middleware.RequireRole("admin", "staff"), shopeeRealtimeH.Tracking)
		api.GET("/shopee-operations/:shop_id/:order_sn/timeline", middleware.RequireRole("admin", "staff"), shopeeRealtimeH.Timeline)
		api.POST("/shopee-operations/:shop_id/:order_sn/ship", middleware.RequireRole("admin", "staff"), shopeeRealtimeH.ShipOrder)
		api.POST("/shopee-operations/:shop_id/:order_sn/shipping-document/create", middleware.RequireRole("admin", "staff"), shopeeRealtimeH.ShippingDocumentCreate)
		api.GET("/shopee-operations/:shop_id/:order_sn/shipping-document/result", middleware.RequireRole("admin", "staff"), shopeeRealtimeH.ShippingDocumentResult)
		api.GET("/shopee-operations/:shop_id/:order_sn/shipping-document/download", middleware.RequireRole("admin", "staff"), shopeeRealtimeH.ShippingDocumentDownload)
		api.GET("/shopee-operations/diagnostics", middleware.RequireRole("admin"), shopeeRealtimeH.Diagnostics)

		// Lazada import — same manual-review flow as Shopee Excel
		api.GET("/settings/lazada-config", lazadaH.GetConfig)
		api.GET("/import/lazada/runs", middleware.RequireRole("admin", "staff"), lazadaH.ListRuns)
		api.POST("/import/lazada/preview", middleware.RequireRole("admin", "staff"), lazadaH.Preview)
		api.POST("/import/lazada/confirm", middleware.RequireRole("admin", "staff"), lazadaH.Confirm)

		// TikTok import — same manual-review flow as Shopee/Lazada Excel
		api.GET("/settings/tiktok-config", tiktokH.GetConfig)
		api.GET("/import/tiktok/runs", middleware.RequireRole("admin", "staff"), tiktokH.ListRuns)
		api.POST("/import/tiktok/preview", middleware.RequireRole("admin", "staff"), tiktokH.Preview)
		api.POST("/import/tiktok/confirm", middleware.RequireRole("admin", "staff"), tiktokH.Confirm)

		// Marketplace aliases (review queue)
		api.GET("/marketplace-aliases/review-groups", middleware.RequireRole("admin", "staff"), aliasH.ReviewGroups)
		api.POST("/marketplace-aliases/confirm", middleware.RequireRole("admin", "staff"), aliasH.Confirm)

		// Platform column mappings
		api.GET("/settings/column-mappings/:platform", settingsH.GetColumnMappings)
		api.PUT("/settings/column-mappings/:platform", middleware.RequireRole("admin"), settingsH.UpdateColumnMappings)

		// Channel defaults (admin only) — per-(channel, bill_type) party config
		api.GET("/settings/channel-defaults", middleware.RequireRole("admin"), channelDefaultsH.List)
		api.PUT("/settings/channel-defaults", middleware.RequireRole("admin"), channelDefaultsH.Upsert)

		// SML party master proxy — search customers/suppliers from cache
		api.GET("/sml/customers", middleware.RequireRole("admin", "staff"), smlPartyH.SearchCustomers)
		api.POST("/sml/customers", middleware.RequireRole("admin", "staff"), smlPartyH.CreateCustomer)
		api.GET("/sml/suppliers", middleware.RequireRole("admin", "staff"), smlPartyH.SearchSuppliers)
		api.POST("/sml/suppliers", middleware.RequireRole("admin", "staff"), smlPartyH.CreateSupplier)
		api.POST("/sml/refresh-parties", middleware.RequireRole("admin"), smlPartyH.Refresh)
		api.GET("/sml/parties/last-sync", middleware.RequireRole("admin", "staff"), smlPartyH.LastSync)
		api.GET("/sml/doc-formats", middleware.RequireRole("admin", "staff"), smlPartyH.DocFormats)
		api.GET("/sml/branches", middleware.RequireRole("admin", "staff"), smlPartyH.Branches)
		api.GET("/sml/sales", middleware.RequireRole("admin", "staff"), smlPartyH.Sales)
		api.GET("/sml/expenses", middleware.RequireRole("admin", "staff"), smlPartyH.Expenses)
		api.GET("/sml/incomes", middleware.RequireRole("admin", "staff"), smlPartyH.Incomes)
		api.GET("/sml/passbooks", middleware.RequireRole("admin", "staff"), smlPartyH.Passbooks)
		api.GET("/sml/units", middleware.RequireRole("admin", "staff"), catalogH.GetUnits)
		api.GET("/sml/warehouses", middleware.RequireRole("admin", "staff"), smlWarehouseH.SearchWarehouses)
		api.GET("/sml/warehouses/:code/shelves", middleware.RequireRole("admin", "staff"), smlWarehouseH.SearchShelves)
		api.POST("/sml/refresh-warehouses", middleware.RequireRole("admin"), smlWarehouseH.Refresh)
		api.GET("/sml/warehouses/last-sync", middleware.RequireRole("admin", "staff"), smlWarehouseH.LastSync)

		// IMAP accounts (admin only) — multi-mailbox config
		api.GET("/settings/imap-accounts", middleware.RequireRole("admin"), imapSettingsH.List)
		api.POST("/settings/imap-accounts", middleware.RequireRole("admin"), imapSettingsH.Create)
		api.POST("/settings/imap-accounts/test", middleware.RequireRole("admin"), imapSettingsH.TestConnection)
		api.POST("/settings/imap-accounts/list-folders", middleware.RequireRole("admin"), imapSettingsH.ListFolders)
		api.GET("/settings/imap-accounts/:id", middleware.RequireRole("admin"), imapSettingsH.Get)
		api.PUT("/settings/imap-accounts/:id", middleware.RequireRole("admin"), imapSettingsH.Update)
		api.DELETE("/settings/imap-accounts/:id", middleware.RequireRole("admin"), imapSettingsH.Delete)
		api.POST("/settings/imap-accounts/:id/poll", middleware.RequireRole("admin"), imapSettingsH.PollNow)
		api.POST("/settings/imap-accounts/:id/poll-jobs", middleware.RequireRole("admin"), imapSettingsH.CreatePollJob)
		api.POST("/settings/imap-accounts/:id/reset-progress", middleware.RequireRole("admin"), imapSettingsH.ResetProgress)
		api.GET("/settings/imap-poll-jobs/active", middleware.RequireRole("admin"), imapSettingsH.ListActivePollJobs)
		api.GET("/settings/imap-poll-jobs/:job_id", middleware.RequireRole("admin"), imapSettingsH.GetPollJob)

		api.GET("/settings/users", middleware.RequireRole("admin"), userSettingsH.List)
		api.POST("/settings/users", middleware.RequireRole("admin"), userSettingsH.Create)
		api.PUT("/settings/users/:id", middleware.RequireRole("admin"), userSettingsH.Update)
		api.DELETE("/settings/users/:id", middleware.RequireRole("admin"), userSettingsH.Delete)

		// Catalog (SML product catalog + smart matching)
		api.GET("/catalog", catalogH.List)
		api.GET("/catalog/stats", catalogH.Stats)
		api.GET("/catalog/search", catalogH.Search)
		api.GET("/catalog/hidden-codes", middleware.RequireRole("admin"), catalogH.HiddenCodes)
		api.GET("/catalog/:code/image", catalogH.GetImage)
		api.GET("/catalog/:code/images", catalogH.GetImages)
		api.GET("/catalog/:code/images/:roworder", catalogH.GetImageByRoworder)
		api.GET("/catalog/:code/units", middleware.RequireRole("admin", "staff"), catalogH.GetProductUnits)
		api.GET("/catalog/:code", catalogH.GetOne)
		api.POST("/catalog/products", middleware.RequireRole("admin", "staff"), catalogH.CreateProduct)
		api.POST("/catalog/sync", middleware.RequireRole("admin"), catalogH.SyncFromAPI)
		api.POST("/catalog/refresh-batch", middleware.RequireRole("admin"), catalogH.RefreshBatch)
		api.POST("/catalog/import-csv", middleware.RequireRole("admin"), catalogH.ImportCSV)
		api.POST("/catalog/embed-all", middleware.RequireRole("admin"), catalogH.EmbedAll)
		api.POST("/catalog/reload-index", middleware.RequireRole("admin"), catalogH.ReloadIndex)
		api.POST("/catalog/:code/embed", middleware.RequireRole("admin"), catalogH.EmbedOne)
		api.POST("/catalog/:code/refresh", middleware.RequireRole("admin"), catalogH.RefreshOne)
		api.DELETE("/catalog/:code", middleware.RequireRole("admin"), catalogH.DeleteOne)

		// Confirm catalog match for a needs_review bill item
		api.POST("/bills/:id/items/:item_id/confirm-match", middleware.RequireRole("admin", "staff"), catalogH.ConfirmMatch)

		// Chat inbox (LINE OA human-to-human conversations)
		chatGroup := api.Group("/admin/conversations")
		chatGroup.Use(middleware.RequireRole("admin", "staff"))
		{
			chatGroup.GET("", chatInboxH.ListConversations)
			chatGroup.GET("/unread-count", chatInboxH.UnreadCount)
			chatGroup.GET("/:lineUserId/messages", chatInboxH.ListMessages)
			chatGroup.POST("/:lineUserId/messages", chatInboxH.SendReply)
			chatGroup.POST("/:lineUserId/messages/media", chatInboxH.SendMedia)
			chatGroup.POST("/:lineUserId/mark-read", chatInboxH.MarkRead)
			chatGroup.PATCH("/:lineUserId/status", chatInboxH.SetStatus)
			chatGroup.PATCH("/:lineUserId/phone", chatInboxH.SetPhone)

			// Phase 4.8 internal notes (admin-only annotations).
			chatNotesH := handlers.NewChatNotesHandler(chatNoteRepo, auditLogRepo)
			chatGroup.GET("/:lineUserId/notes", chatNotesH.List)
			chatGroup.POST("/:lineUserId/notes", chatNotesH.Create)
			chatGroup.PUT("/:lineUserId/notes/:noteId", chatNotesH.Update)
			chatGroup.DELETE("/:lineUserId/notes/:noteId", chatNotesH.Delete)

			// Phase 4.9 tags — m2m attach for a single conversation.
			chatTagsH := handlers.NewChatTagsHandler(chatTagRepo, auditLogRepo, eventBroker)
			chatGroup.GET("/:lineUserId/tags", chatTagsH.TagsForConversation)
			chatGroup.PUT("/:lineUserId/tags", chatTagsH.SetTagsForConversation)
			chatGroup.GET("/:lineUserId/messages/:messageId/media", chatInboxH.DownloadMedia)
			chatGroup.POST("/:lineUserId/messages/:messageId/extract", chatInboxH.ExtractFromMedia)
			chatGroup.POST("/:lineUserId/bills", chatInboxH.CreateBill)
			chatGroup.GET("/:lineUserId/history", chatInboxH.CustomerHistory)
		}

		// LINE OA accounts (admin-only) — /settings/line-oa CRUD + test button
		lineOAH := handlers.NewLineOAHandler(lineOARepo, lineRegistry, auditLogRepo, logger)
		lineOAGroup := api.Group("/settings/line-oa")
		lineOAGroup.Use(middleware.RequireRole("admin"))
		{
			lineOAGroup.GET("", lineOAH.List)
			lineOAGroup.GET("/:id", lineOAH.Get)
			lineOAGroup.POST("", lineOAH.Create)
			lineOAGroup.PUT("/:id", lineOAH.Update)
			lineOAGroup.DELETE("/:id", lineOAH.Delete)
			lineOAGroup.POST("/:id/test", lineOAH.Test)
		}

		// Quick reply templates for the chat composer (Phase 4.4)
		quickReplyH := handlers.NewChatQuickReplyHandler(chatQuickReplyRepo, auditLogRepo)
		qrGroup := api.Group("/admin/quick-replies")
		qrGroup.Use(middleware.RequireRole("admin", "staff"))
		{
			qrGroup.GET("", quickReplyH.List)
			qrGroup.POST("", middleware.RequireRole("admin"), quickReplyH.Create)
			qrGroup.PUT("/:id", middleware.RequireRole("admin"), quickReplyH.Update)
			qrGroup.DELETE("/:id", middleware.RequireRole("admin"), quickReplyH.Delete)
		}

		// Phase 4.9 — global chat tag CRUD. /settings/chat-tags admin page.
		tagsAdminH := handlers.NewChatTagsHandler(chatTagRepo, auditLogRepo, eventBroker)
		tagsGroup := api.Group("/settings/chat-tags")
		tagsGroup.Use(middleware.RequireRole("admin", "staff"))
		{
			tagsGroup.GET("", tagsAdminH.ListAll)
			tagsGroup.POST("", middleware.RequireRole("admin"), tagsAdminH.Create)
			tagsGroup.PUT("/:id", middleware.RequireRole("admin"), tagsAdminH.Update)
			tagsGroup.DELETE("/:id", middleware.RequireRole("admin"), tagsAdminH.Delete)
		}
	}

	// Background jobs
	c := cron.New()
	go lineNotificationSvc.StartWorker(appCtx, 15*time.Second, 10)
	if cfg.ShopeeRealtimeOpsEnabled {
		go shopeeRealtimeH.StartReconcileWorker(appCtx, 5*time.Second, 10)
	}
	if cfg.ShopeeRealtimeOpsEnabled && cfg.ShopeeRealtimeSyncIntervalSeconds > 0 {
		interval := time.Duration(cfg.ShopeeRealtimeSyncIntervalSeconds) * time.Second
		if _, err := c.AddFunc("@every "+interval.String(), func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if n, err := shopeeRealtimeH.SyncAllActive(ctx, 14); err != nil {
				logger.Warn("shopee realtime scheduled sync failed", zap.Error(err))
			} else {
				logger.Info("shopee realtime scheduled sync completed", zap.Int("orders", n))
			}
		}); err != nil {
			logger.Warn("register shopee realtime scheduled sync", zap.Error(err))
		}
	}
	insightCron := jobs.NewInsightCron(insightSvc, billRepo, insightRepo, lineSvc, cfg.InsightLineNotify, logger)
	insightCron.Register(c, cfg.InsightCronHour)

	// Backup cron runs pg_dump from inside the backend container against the
	// postgres service on the Docker network. Output goes to /app/backups
	// (mounted to ~/nexflow/backups on the host via docker-compose.yml).
	backupCron := jobs.NewBackupCron(
		"postgres", "5432",
		cfg.DBUser, "nexflow", cfg.DBPassword,
		"/app/backups", logger,
	)
	backupCron.Register(c, cfg.BackupCronHour)

	diskMon := jobs.NewDiskMonitor(cfg.DiskWarnPercent, lineSvc, logger)
	diskMon.Register(c)

	if cfg.DataLifecycleEnabled {
		lifecycle := jobs.NewDataLifecycle(
			db,
			cfg.HotLogDays,
			cfg.AutoArchiveDays,
			cfg.SummaryRetentionDays,
			cfg.PurgeBatchSize,
			logger,
		)
		lifecycle.Register(c, cfg.DataLifecycleCronHour)
	}

	if lineSvc != nil {
		tokenChecker := jobs.NewTokenChecker(lineSvc, logger)
		tokenChecker.Register(c)
	}

	// Hourly: clear replyTokens > 1h old so admin replies don't waste a
	// LINE round-trip on a token we know is dead.
	replyTokenCleanup := jobs.NewReplyTokenCleanup(db, logger)
	replyTokenCleanup.Register(c)

	// Daily 9am Bangkok: ping PUBLIC_BASE_URL/health to detect when the
	// Cloudflare Quick Tunnel URL has rolled (cloudflared restart). Without
	// this admin only finds out via "ลูกค้าได้รับรูปไม่ครบ" days later.
	tunnelMon := jobs.NewTunnelDriftMonitor(cfg.PublicBaseURL, lineSvc, logger)
	tunnelMon.Register(c)

	// Wire processors into the coordinator now that emailH is built, then
	// boot the multi-account poller. Coordinator reads imap_accounts and
	// spawns one goroutine per enabled row. Empty list = no polling, no
	// errors — admin needs to add accounts via /settings/email.
	imapProcessors.Attachment = emailH.ProcessAttachment
	imapProcessors.ShopeeOrder = emailH.ProcessShopeeEmailBody
	imapProcessors.ShopeeShipped = emailH.ProcessShopeeShippedEmailBody
	imapProcessors.DuplicateMessage = billRepo.FindByEmailMessageID
	imapProcessors.DuplicateMessages = billRepo.FindExistingEmailMessageIDs

	if err := imapCoordinator.Start(context.Background()); err != nil {
		logger.Error("imap coordinator start failed", zap.Error(err))
	}
	defer imapCoordinator.Stop()

	c.Start()
	defer c.Stop()

	// HTTP server
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		logger.Info("server starting", zap.String("port", cfg.Port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server error", zap.Error(err))
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("server shutdown error", zap.Error(err))
	}
	logger.Info("server stopped")
}

// seedAdminUser creates a default admin if no users exist
func seedAdminUser(db *sql.DB, logger *zap.Logger) {
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		logger.Error("seed: count users", zap.Error(err))
		return
	}
	if count > 0 {
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("admin1234"), bcrypt.DefaultCost)
	if err != nil {
		logger.Error("seed: bcrypt", zap.Error(err))
		return
	}

	_, err = db.Exec(
		`INSERT INTO users (email, name, role, password_hash) VALUES ($1, $2, $3, $4)`,
		"admin@nexflow.local", "Admin", "admin", string(hash),
	)
	if err != nil {
		logger.Error("seed: insert admin", zap.Error(err))
		return
	}
	logger.Info("seeded default admin: admin@nexflow.local / admin1234")
}
