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
	"strings"
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

	"iot-platform/internal/adapters/observability"
	"iot-platform/internal/opscenter"
)

func Run(forcedRole string) {
	envFile := flag.String("env-file", "", "load a KEY=VALUE configuration file (existing environment variables take precedence)")
	flag.Parse()
	logLevel := new(slog.LevelVar)
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	if *envFile != "" {
		fatal(log, "load environment file", config.LoadEnvFile(*envFile))
	}
	level, err := config.LogLevel()
	fatal(log, "validate configuration", err)
	logLevel.Set(level)
	cfg := config.Load()
	if forcedRole != "" {
		cfg.ProcessRole = forcedRole
	}
	fatal(log, "validate configuration", cfg.Validate())
	var logPush *observability.LokiPush
	if cfg.Ops.LogPushURL != "" {
		// Host-run processes (local source debugging) ship their own logs; containers
		// are collected by the log collector and leave this unset.
		logPush = observability.NewLokiPush(cfg.Ops.LogPushURL, cfg.Ops.LogPushTenant, cfg.Ops.WithDefaults().LogServiceName)
		log = slog.New(observability.NewTeeHandler(log.Handler(), slog.NewJSONHandler(logPush, &slog.HandlerOptions{Level: logLevel})))
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var repo ports.Repository = memory.NewRepository()
	opsPrefs, _ := repo.(ports.OpsPreferenceStore)
	var aiProviderStore ports.AIProviderConfigStore
	if store, ok := repo.(ports.AIProviderConfigStore); ok {
		aiProviderStore = store
	}
	var postgresRaw, clickHouseRaw ports.RawMessageDatabase
	if raw, ok := repo.(ports.RawMessageDatabase); ok {
		postgresRaw = raw
	}
	if cfg.PostgresDSN != "" {
		r, err := postgres.NewWithMaxConns(ctx, cfg.PostgresDSN, int32(positiveOr(cfg.PostgresMaxConns, 64)))
		fatal(log, "initialize postgres", err)
		repo = r
		opsPrefs = r
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
	localBus := local.NewBus()
	// Without Kafka, automatic alarm analysis still runs apart from the alarm
	// path, with the same concurrency as its Kafka consumer group.
	localBus.SetAsyncTopic(model.TopicAlarmRaised, positiveOr(cfg.AIAnalysisConcurrency, 1), 1000)
	var bus ports.EventBus = localBus
	var kafkaBus *kafkaadapter.Bus
	if len(cfg.KafkaBrokers) > 0 {
		kafkaBus = kafkaadapter.New(cfg.KafkaBrokers)
		kafkaBus.SetLogger(log)
		// Parallel lanes keep each device's (or alarm's) messages in order;
		// automatic alarm analysis has its own, smaller limit.
		kafkaBus.SetConsumerConcurrency(positiveOr(cfg.KafkaConsumerConcurrency, 64), map[string]int{model.TopicAlarmRaised: positiveOr(cfg.AIAnalysisConcurrency, 1)})
		bus = kafkaBus
		log.Info("event bus enabled", "adapter", "kafka", "brokers", cfg.KafkaBrokers)
	}
	localRealtime := local.NewRealtime()
	var realtime ports.RealtimePublisher = localRealtime
	registry := metrics.New()
	if kafkaBus != nil {
		// kafka_lag is the total backlog of this process's consumer groups;
		// kafka_lag_<group> breaks it down. Sampling errors keep the last value.
		go func() {
			ticker := time.NewTicker(15 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					lags, err := kafkaBus.ConsumerLag(ctx)
					if err != nil {
						log.Warn("sample kafka consumer lag", "error", err)
						continue
					}
					var total int64
					for group, lag := range lags {
						total += lag
						registry.Set("kafka_lag_"+strings.NewReplacer("iot-platform-", "", "-", "_", ".", "_").Replace(group), float64(lag))
					}
					registry.Set("kafka_lag", float64(total))
				}
			}
		}()
	}
	var mqttClient *mqttadapter.Client
	// The EMQX management API is optional: it enables immediate credential
	// revocation and broker-side drop counters for the platform session.
	var emqxAdmin *mqttadapter.Admin
	if cfg.EMQXAPIURL != "" && cfg.EMQXAPIKey != "" && cfg.EMQXAPISecret != "" {
		emqxAdmin = &mqttadapter.Admin{URL: cfg.EMQXAPIURL, Key: cfg.EMQXAPIKey, Secret: cfg.EMQXAPISecret}
	}
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
		mqttConnection, err := mqttadapter.NewDurableWithCredentials(cfg.MQTTBroker, filepath.Join(cfg.DataDir, "mqtt-inbox", cfg.ProcessRole), credentials)
		fatal(log, "connect mqtt", err)
		mqttClient = mqttConnection
		go func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			var lastDropped int64 = -1
			for tick := 0; ; tick++ {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					pending, rejected, corrupt := mqttConnection.InboxCounts()
					registry.Set("mqtt_inbox_pending", float64(pending))
					registry.Set("mqtt_inbox_rejected", float64(rejected))
					registry.Set("mqtt_inbox_corrupt", float64(corrupt))
					// Messages the broker drops for this session never reach the
					// inbox although devices got PUBACK; only the broker can count them.
					if emqxAdmin == nil || tick%3 != 0 {
						continue
					}
					sampleCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
					queued, dropped, sampleErr := emqxAdmin.SessionQueue(sampleCtx, mqttConnection.ClientID())
					cancel()
					if sampleErr != nil {
						continue
					}
					registry.Set("mqtt_broker_queue", float64(queued))
					registry.Set("mqtt_broker_dropped", float64(dropped))
					if lastDropped >= 0 && dropped > lastDropped {
						log.Error("MQTT broker dropped messages for the platform session; ingestion is slower than devices publish", "dropped", dropped-lastDropped, "brokerQueue", queued)
					}
					lastDropped = dropped
				}
			}
		}()
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
	engine := core.New(httpapi.ScopedRepository(repo), archivePort, bus, realtime, parsers, log)
	if kafkaBus != nil {
		// Backpressure: stop taking new raw messages while parsing and storage
		// are far behind, so ingest does not starve them of database capacity.
		if cfg.IngestMaxBacklog > 0 {
			go func() {
				ticker := time.NewTicker(15 * time.Second)
				defer ticker.Stop()
				paused := false
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						parserLag, err := kafkaBus.GroupLag(ctx, "parser", model.TopicRaw)
						if err != nil {
							continue
						}
						storageLag, err := kafkaBus.GroupLag(ctx, "storage", model.TopicPropertyReport, model.TopicEventReport, model.TopicParsed)
						if err != nil {
							continue
						}
						backlog := parserLag + storageLag
						registry.Set("pipeline_backlog", float64(backlog))
						next := paused
						if !paused && backlog > cfg.IngestMaxBacklog {
							next = true
						} else if paused && backlog < cfg.IngestMaxBacklog*8/10 {
							next = false
						}
						if next != paused {
							paused = next
							engine.SetIngestPaused(paused)
							if paused {
								log.Error("raw ingest paused: processing backlog above limit", "backlog", backlog, "limit", cfg.IngestMaxBacklog)
							} else {
								log.Info("raw ingest resumed", "backlog", backlog)
							}
						}
						registry.Set("ingest_paused", map[bool]float64{true: 1, false: 0}[paused])
					}
				}
			}()
		}
	}
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
		if providerID == "" {
			providerID = "deepseek"
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
			} else if found && !(cfg.AIProvider == "deepseek" && persisted.Provider == "ollama" && strings.HasPrefix(strings.ToLower(persisted.Model), "qwen")) { /* Retire the previously bundled Qwen selection; keep explicitly configured external providers. */
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
			if providerConfig.Provider != "deepseek" || strings.TrimSpace(providerConfig.APIKey) != "" {
				harnessErr = harness.ConfigureProvider(configureCtx, providerConfig)
			} else {
				log.Warn("DeepSeek API key is not configured; enter it in model management")
			}
			configureCancel()
			if harnessErr != nil {
				// Compose starts the Harness sidecar after the API so it can call the
				// platform MCP endpoint. Do not make API startup depend on that
				// ordering; retry in the background until the sidecar is ready.
				log.Warn("AI workflow provider synchronization deferred", "error", harnessErr)
				go retryHarnessProvider(ctx, runtimeAI, harness, log)
			}
			engine.AIWorkflows = harness
			// Business AI runs (alarm analysis, inspection, reports, protocol
			// assistant, rule drafts) sign their MCP credentials with the API secret.
			engine.HarnessTokens = auth.New(cfg.JWTSecret)
			log.Info("AI workflow harness enabled", "url", cfg.AIHarnessURL, "model", providerConfig.Model)
		}

	}
	if cfg.ProcessRole != "gateway" && cfg.WeaviateURL != "" {
		engine.KB = knowledge.NewWeaviate(cfg.WeaviateURL)
	} else {
		engine.KB = knowledge.NewLocal()
	}
	var opsService *opscenter.Service
	if cfg.ProcessRole != "gateway" {
		opsService = newOpsCenter(cfg, opsPrefs, log)
		fatal(log, "start device alarm notifications", opsService.StartDeviceNotifications(ctx, bus, filepath.Join(cfg.DataDir, "ops-state", "device-notifications")))
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
	protocolListeners.SetAllowedCIDRs(cfg.ModbusAllowedCIDRs)
	protocolListeners.SetMaxSessions(int(cfg.ProtocolListenerMaxSessions))
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				registry.Set("protocol_listener_rejected_total", float64(protocolListeners.RejectedSessions()))
			}
		}
	}()
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
			raw, err := standardIngress.PrepareStandard(c, tenant, product, device, kind, "MQTT", payload)
			if err != nil {
				if errors.Is(err, onboarding.ErrAuth) || errors.Is(err, model.ErrInvalidIngress) {
					return mqttadapter.Reject(err)
				}
				return err
			}
			raw.ReceivedAt = mqttadapter.ReceivedAt(c)
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
	if emqxAdmin != nil {
		revokeUsername = emqxAdmin.RevokeUsername
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
	if cfg.ProcessRole != "gateway" {
		api.SetOpsCenter(opsService)
	}
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
	if logPush != nil {
		flushCtx, flushCancel := context.WithTimeout(context.Background(), 5*time.Second)
		logPush.Close(flushCtx)
		flushCancel()
	}
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

func positiveOr(value int64, fallback int) int {
	if value > 0 {
		return int(value)
	}
	return fallback
}
