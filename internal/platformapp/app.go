package platformapp

import (
	"context"
	"errors"
	"flag"
	"github.com/google/uuid"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	aiadapter "iot-platform/internal/adapters/ai"
	clickhouseadapter "iot-platform/internal/adapters/clickhouse"
	kafkaadapter "iot-platform/internal/adapters/kafka"
	"iot-platform/internal/adapters/knowledge"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	minioadapter "iot-platform/internal/adapters/minio"
	mqttadapter "iot-platform/internal/adapters/mqtt"
	"iot-platform/internal/adapters/postgres"
	"iot-platform/internal/adapters/rawstore"
	redisadapter "iot-platform/internal/adapters/redis"
	"iot-platform/internal/auth"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/httpapi"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
	"iot-platform/internal/protocolruntime"
)

func Run(forcedRole string) {
	envFile := flag.String("env-file", "", "load a KEY=VALUE configuration file (existing environment variables take precedence)")
	flag.Parse()
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if *envFile != "" {
		fatal(log, "load environment file", config.LoadEnvFile(*envFile))
	}
	cfg := config.Load()
	if forcedRole != "" {
		cfg.ProcessRole = forcedRole
	}
	fatal(log, "validate configuration", cfg.Validate())
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var repo ports.Repository = memory.NewRepository()
	var aiProviderStore ports.AIProviderConfigStore
	if store, ok := repo.(ports.AIProviderConfigStore); ok {
		aiProviderStore = store
	}
	var postgresRaw, clickHouseRaw ports.RawMessageDatabase
	if raw, ok := repo.(ports.RawMessageDatabase); ok {
		postgresRaw = raw
	}
	if cfg.PostgresDSN != "" {
		r, err := postgres.New(ctx, cfg.PostgresDSN)
		fatal(log, "initialize postgres", err)
		repo = r
		if store, ok := any(r).(ports.AIProviderConfigStore); ok {
			aiProviderStore = store
		}
		postgresRaw = r
		log.Info("repository enabled", "adapter", "postgres")
	}
	if cfg.ClickHouseURL != "" {
		r, clickErr := clickhouseadapter.New(ctx, cfg.ClickHouseURL, repo)
		fatal(log, "initialize clickhouse", clickErr)
		repo = r
		clickHouseRaw = r
		log.Info("telemetry storage enabled", "adapter", "clickhouse")
	}
	if cfg.RedisAddr != "" {
		repo = redisadapter.New(repo, cfg.RedisAddr, cfg.RedisPassword)
		log.Info("hot state cache enabled", "adapter", "redis")
	}
	var archivePort ports.Archive
	if cfg.MinIOEndpoint != "" {
		m, err := minioadapter.New(cfg.MinIOEndpoint, cfg.MinIOAccessKey, cfg.MinIOSecretKey, cfg.MinIOUseTLS)
		fatal(log, "initialize minio", err)
		archivePort = m
		log.Info("archive enabled", "adapter", "minio")
	} else {
		archive, err := local.NewArchive(filepath.Join(cfg.DataDir, "objects"))
		fatal(log, "initialize local archive", err)
		archivePort = archive
	}
	var bus ports.EventBus = local.NewBus()
	if len(cfg.KafkaBrokers) > 0 {
		bus = kafkaadapter.New(cfg.KafkaBrokers)
		log.Info("event bus enabled", "adapter", "kafka", "brokers", cfg.KafkaBrokers)
	}
	localRealtime := local.NewRealtime()
	var realtime ports.RealtimePublisher = localRealtime
	registry := metrics.New()
	var mqttClient *mqttadapter.Client
	if cfg.MQTTBroker != "" {
		credentials := func() (string, string) { return cfg.MQTTUsername, cfg.MQTTPassword }
		if cfg.MQTTPassword == "" {
			manager := auth.New(cfg.JWTSecret)
			acl := []auth.ACLRule{
				{Permission: "allow", Action: "subscribe", Topic: "/iot/up/#"},
				{Permission: "allow", Action: "subscribe", Topic: "/external/raw/#"},
				{Permission: "allow", Action: "subscribe", Topic: "/jetlinks/raw/#"},
				{Permission: "allow", Action: "subscribe", Topic: "/external/video/alarm/#"},
				{Permission: "allow", Action: "subscribe", Topic: "/iot/device/state/#"},
				{Permission: "allow", Action: "publish", Topic: "/iot/#"},
			}
			credentials = func() (string, string) {
				token, err := manager.IssueWithACL("iot-platform", "system", "service", nil, acl, time.Hour)
				if err != nil {
					log.Error("issue mqtt service token", "error", err)
					return "iot-platform", ""
				}
				return "iot-platform", token
			}
		}
		mqttConnection, err := mqttadapter.NewWithCredentials(cfg.MQTTBroker, "iot-"+cfg.ProcessRole+"-"+hostname()+"-"+strconv.Itoa(os.Getpid()), credentials)
		fatal(log, "connect mqtt", err)
		mqttClient = mqttConnection
		if cfg.AccessCoordination {
			fatal(log, "configure shared MQTT ingestion", mqttClient.ConfigureSharedSubscriptions("iot-access"))
		}
		realtime = mqttClient
		if cfg.ProcessRole != "api" {
			registry.Set("mqtt_subscription_count", 4)
		}
		log.Info("realtime enabled", "adapter", "mqtt")
	}
	parsers := parser.NewPlatformRegistry(cfg.DataDir)
	engine := core.New(repo, archivePort, bus, realtime, parsers, log)
	var legacyRaw ports.RawMessageReader
	if reader, ok := archivePort.(ports.RawMessageReader); ok {
		legacyRaw = reader
	}
	engine.RawStore = rawstore.New(rawstore.Config{
		PostgreSQL:               postgresRaw,
		ClickHouse:               clickHouseRaw,
		Resolver:                 repo,
		Legacy:                   legacyRaw,
		HighFrequencyIntervalSec: cfg.RawHighFrequencyIntervalSec,
	})
	engine.VideoMediaAllowedHosts = cfg.VideoMediaHosts
	engine.RequireVideoCameraMapping = !cfg.DevMode
	engine.Metrics = registry
	var runtimeAI *aiadapter.RuntimeProvider
	var harness *aiadapter.HarnessClient
	if cfg.ProcessRole != "gateway" {
		aiPlugins := aiadapter.NewProviderRegistry()
		engine.AIPlugins = aiPlugins
		providerID := cfg.AIProvider
		if providerID == "" && os.Getenv("IOT_OLLAMA_URL") != "" {
			providerID = "ollama"
		}
		providerConfig := ports.AIPluginConfig{Provider: providerID, BaseURL: cfg.AIBaseURL, Model: cfg.AIModel, APIKey: cfg.AIAPIKey}
		if providerID == "ollama" {
			if providerConfig.BaseURL == "" {
				providerConfig.BaseURL = cfg.OllamaURL
			}
			if providerConfig.Model == "" {
				providerConfig.Model = cfg.OllamaModel
			}
		}
		if aiProviderStore != nil {
			if persisted, found, err := aiProviderStore.LoadAIProviderConfig(ctx); err != nil {
				log.Warn("load persisted AI provider config", "error", err)
			} else if found {
				providerConfig = persisted
				log.Info("restored persisted AI provider", "provider", providerConfig.Provider, "model", providerConfig.Model)
			}
		}
		// Older installations may have an activity row without the newer baseUrl
		// field. Fill only missing defaults so a persisted selection remains usable.
		if providerConfig.Provider == "ollama" {
			if providerConfig.BaseURL == "" {
				providerConfig.BaseURL = cfg.OllamaURL
			}
			if providerConfig.Model == "" {
				providerConfig.Model = cfg.OllamaModel
			}
		} else if providerConfig.Provider == "deepseek" {
			if providerConfig.BaseURL == "" {
				providerConfig.BaseURL = cfg.AIBaseURL
				if providerConfig.BaseURL == "" {
					providerConfig.BaseURL = "https://api.deepseek.com"
				}
			}
			if providerConfig.Model == "" {
				for _, item := range aiPlugins.List() {
					if item.ID == "deepseek" {
						providerConfig.Model = item.DefaultModel
						break
					}
				}
			}
			if providerConfig.APIKey == "" {
				providerConfig.APIKey = cfg.AIAPIKey
			}
		}
		var providerErr error
		runtimeAI, providerErr = aiadapter.NewRuntimeProvider(aiPlugins, providerConfig)
		fatal(log, "initialize AI provider plugin", providerErr)
		einoAI, einoErr := aiadapter.NewEino(ctx, runtimeAI)
		fatal(log, "initialize Eino AI workflows", einoErr)
		engine.AI = einoAI
		if cfg.ProcessRole != "gateway" && cfg.AIHarnessURL != "" {
			harnessModel := cfg.AIHarnessModel
			if providerConfig.Model != "" {
				harnessModel = providerConfig.Model
			}
			var harnessErr error
			harness, harnessErr = aiadapter.NewHarness(cfg.AIHarnessURL, cfg.AIHarnessToken, cfg.AIHarnessMCPURL, harnessModel, cfg.AIHarnessTimeout)
			fatal(log, "initialize AI workflow harness", harnessErr)
			configureCtx, configureCancel := context.WithTimeout(ctx, 20*time.Second)
			harnessErr = harness.ConfigureProvider(configureCtx, providerConfig)
			configureCancel()
			if harnessErr != nil {
				// Compose starts the Harness sidecar after the API so it can call the
				// platform MCP endpoint. Do not make API startup depend on that
				// ordering; retry in the background until the sidecar is ready.
				log.Warn("AI workflow provider synchronization deferred", "error", harnessErr)
				go retryHarnessProvider(ctx, runtimeAI, harness, log)
			}
			engine.AIWorkflows = harness
			log.Info("AI workflow harness enabled", "url", cfg.AIHarnessURL, "model", providerConfig.Model)
		}

	}
	if cfg.ProcessRole != "gateway" && cfg.WeaviateURL != "" {
		engine.KB = knowledge.NewWeaviate(cfg.WeaviateURL)
	} else {
		engine.KB = knowledge.NewLocal()
	}
	if cfg.ProcessRole != "gateway" {
		fatal(log, "start engine", engine.Start(ctx))
	}
	var coordinator *protocolruntime.Coordinator
	if cfg.AccessCoordination && cfg.ProcessRole != "api" {
		coordinator = protocolruntime.NewCoordinator(repo, hostname()+"-"+strconv.Itoa(os.Getpid())+"-"+uuid.NewString(), cfg.AccessNodeURL)
		go coordinator.Run(ctx)
	}
	protocolRuntime := protocolruntime.New(repo, func(c context.Context, raw model.RawMessage) error {
		_, _, err := engine.IngestRaw(c, raw)
		return err
	}, log, cfg.ModbusAllowedCIDRs...)
	protocolRuntime.SetCoordinator(coordinator)
	if cfg.ProcessRole != "api" {
		protocolRuntime.Start(ctx)
	}
	protocolListeners := protocolruntime.NewListeners(repo, cfg.DataDir, func(c context.Context, raw model.RawMessage) error {
		_, _, err := engine.IngestRaw(c, raw)
		return err
	}, log)
	protocolListeners.SetConnectionReporter(engine.ReportConnection)
	protocolListeners.SetCoordinator(coordinator)
	if cfg.ProcessRole != "api" {
		protocolListeners.Start(ctx)
	}
	if cfg.ProcessRole != "api" {
		log.Info("active protocol runtime enabled", "transports", []string{"TCP", "UDP", "MODBUS_TCP (legacy)"})
	}
	if cfg.ProcessRole != "api" && mqttClient != nil {
		standardIngress := onboarding.New(repo, parsers, cfg.DataDir, cfg.ModbusAllowedCIDRs)
		fatal(log, "subscribe standard mqtt", mqttClient.SubscribeStandard(func(c context.Context, tenant, product, device, kind string, payload []byte) error {
			if kind == "shadow-get" {
				topic, data, err := standardIngress.MQTTShadowReply(c, tenant, product, device, payload)
				if err != nil {
					return err
				}
				return mqttClient.Publish(c, topic, data, 1, false)
			}
			raw, err := standardIngress.PrepareStandard(c, tenant, product, device, kind, "MQTT", payload)
			if err != nil {
				return err
			}
			_, _, err = engine.IngestRaw(c, raw)
			return err
		}))
		fatal(log, "subscribe raw mqtt", mqttClient.SubscribeRaw(func(c context.Context, v model.RawMessage) error { _, _, err := engine.IngestRaw(c, v); return err }))
		fatal(log, "subscribe device state mqtt", mqttClient.SubscribeDeviceState(engine.UpdateDeviceState))
		fatal(log, "subscribe video mqtt", mqttClient.SubscribeVideo(func(c context.Context, v model.VideoAlarmEvent) error {
			_, _, err := engine.IngestVideo(c, v)
			return err
		}))
	}
	api := httpapi.New(cfg, engine, registry, log)
	var publishCommand func(context.Context, string, []byte, byte, bool) error
	if mqttClient != nil {
		publishCommand = mqttClient.Publish
	}
	var revokeUsername func(context.Context, string) error
	if cfg.EMQXAPIURL != "" && cfg.EMQXAPIKey != "" && cfg.EMQXAPISecret != "" {
		admin := &mqttadapter.Admin{URL: cfg.EMQXAPIURL, Key: cfg.EMQXAPIKey, Secret: cfg.EMQXAPISecret}
		revokeUsername = admin.RevokeUsername
	}
	api.SetDeviceOperations(publishCommand, revokeUsername)
	if cfg.ProcessRole != "gateway" {
		go api.RunCredentialRevocations(ctx)
	}
	if mqttClient != nil {
		api.SetMQTTHealth(mqttClient.Probe)
	}
	api.SetAIProviderRuntime(runtimeAI)
	api.SetAIProviderStore(aiProviderStore)
	if harness != nil {
		api.SetAIWorkflowProvider(harness)
	}
	api.SetProtocolListeners(protocolListeners)
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: api.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 15 * time.Minute, IdleTimeout: 2 * time.Minute}
	if cfg.ProcessRole != "gateway" {
		go func() {
			ticker := time.NewTicker(cfg.OfflineScan)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := engine.ScanOffline(ctx); err != nil {
						log.Error("offline scan failed", "error", err)
					}
				}
			}
		}()
	}
	go func() {
		log.Info("iot platform started", "addr", cfg.HTTPAddr, "devMode", cfg.DevMode)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server failed", "error", err)
			cancel()
		}
	}()
	<-ctx.Done()
	shutdown, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	_ = server.Shutdown(shutdown)
	_ = realtime.Close()
	_ = bus.Close()
	_ = repo.Close()
	log.Info("iot platform stopped")
}
func fatal(log *slog.Logger, msg string, err error) {
	if err != nil {
		log.Error(msg, "error", err)
		os.Exit(1)
	}
}

func retryHarnessProvider(ctx context.Context, runtimeAI ports.AIProviderRuntime, harness *aiadapter.HarnessClient, log *slog.Logger) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			target := runtimeAI.CurrentConfig()
			configureCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			err := harness.ConfigureProvider(configureCtx, target)
			cancel()
			if err == nil {
				// A UI update may have arrived while the sidecar request was in
				// flight. Reconcile the newest selection before returning so an
				// older retry can never leave the Harness on stale settings.
				if runtimeAI.CurrentConfig() != target {
					continue
				}
				log.Info("AI workflow provider synchronized", "provider", target.Provider, "model", target.Model)
				return
			}
		}
	}
}

func hostname() string {
	v, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return v
}
