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
	"iot-platform/internal/adapters/embedding"
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
	"iot-platform/internal/ratelimit"

	"iot-platform/internal/adapters/observability"
	"iot-platform/internal/opscenter"
	"iot-platform/internal/video"
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
	var postgresRepo *postgres.Repository
	opsPrefs, _ := repo.(ports.OpsPreferenceStore)
	videoStore, _ := repo.(ports.VideoStore)
	knowledgeStore, _ := repo.(ports.KnowledgeReindexStore)
	var aiProviderStore ports.AIProviderConfigStore
	if store, ok := repo.(ports.AIProviderConfigStore); ok {
		aiProviderStore = store
	}
	var postgresRaw, clickHouseRaw ports.RawMessageDatabase
	if raw, ok := repo.(ports.RawMessageDatabase); ok {
		postgresRaw = raw
	}
	if cfg.PostgresDSN != "" {
		r, err := postgres.NewWithOptions(ctx, cfg.PostgresDSN, postgres.PoolOptions{MaxConns: int32(positiveOr(cfg.PostgresMaxConns, 64)), MaxConnLifetime: cfg.PostgresMaxConnLifetime, HealthCheckPeriod: cfg.PostgresHealthCheckPeriod, ConnectTimeout: cfg.PostgresConnectTimeout, ReadDSN: cfg.PostgresReadDSN, MaxReplicaLag: cfg.PostgresMaxReplicaLag})
		fatal(log, "initialize postgres", err)
		repo = r
		postgresRepo = r
		opsPrefs = r
		videoStore = r
		knowledgeStore = r
		if store, ok := any(r).(ports.AIProviderConfigStore); ok {
			aiProviderStore = store
		}
		postgresRaw = r
		log.Info("repository enabled", "adapter", "postgres")
	}
	if cfg.ClickHouseURL != "" {
		r, clickErr := clickhouseadapter.NewWithOptions(ctx, cfg.ClickHouseURL, repo, clickhouseadapter.Options{Cluster: cfg.ClickHouseCluster, InsertQuorum: cfg.ClickHouseInsertQuorum})
		fatal(log, "initialize clickhouse", clickErr)
		repo = r
		clickHouseRaw = r
		log.Info("telemetry storage enabled", "adapter", "clickhouse")
	}
	// Rate budgets are shared through Redis when configured; otherwise (or
	// during a Redis outage) each process enforces budget/IOT_CLUSTER_INSTANCES.
	var sharedLimits ratelimit.Limiter
	if cfg.RedisAddr != "" || (cfg.RedisMasterName != "" && len(cfg.RedisSentinels) > 0) {
		redisClient := redisadapter.NewClient(redisadapter.Options{Addr: cfg.RedisAddr, Password: cfg.RedisPassword, MasterName: cfg.RedisMasterName, Sentinels: cfg.RedisSentinels})
		repo = redisadapter.New(repo, redisClient)
		sharedLimits = redisadapter.NewRateLimiter(redisClient)
		log.Info("hot state cache enabled", "adapter", "redis")
	}
	limits := ratelimit.NewCluster(sharedLimits, int(cfg.ClusterInstances))
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
	var kafkaBus *kafkaadapter.Bus
	if len(cfg.KafkaBrokers) > 0 {
		kafkaBus = kafkaadapter.New(cfg.KafkaBrokers)
		kafkaBus.SetLogger(log)
		kafkaBus.SetAutoCreateTopics(cfg.KafkaAutoCreateTopics)
		// Parallel lanes keep each device's messages in order.
		kafkaBus.SetConsumerConcurrency(positiveOr(cfg.KafkaConsumerConcurrency, 64))
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
		mqttConnection, err := mqttadapter.NewDurableWithCredentials(cfg.MQTTBroker, mqttInboxDir(cfg), credentials)
		fatal(log, "connect mqtt", err)
		mqttClient = mqttConnection
		if cfg.Runs(config.ComponentAccess) {
			registry.Set("mqtt_ingress_enabled", 1)
		}
		go func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			var lastDropped int64 = -1
			for tick := 0; ; tick++ {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					registry.Set("mqtt_subscription_count", float64(mqttConnection.SubscriptionCount()))
					pending, rejected, corrupt := mqttConnection.InboxCounts()
					registry.Set("mqtt_inbox_pending", float64(pending))
					registry.Set("mqtt_inbox_rejected", float64(rejected))
					registry.Set("mqtt_inbox_corrupt", float64(corrupt))
					// Messages the broker drops for this session never reach the
					// inbox although devices got PUBACK; only the broker can count them.
					if emqxAdmin == nil {
						registry.Set("mqtt_broker_observation_ok", 0)
						continue
					}
					if tick%3 != 0 {
						continue
					}
					sampleCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
					queued, dropped, sampleErr := emqxAdmin.SessionQueue(sampleCtx, mqttConnection.ClientID())
					cancel()
					if sampleErr != nil {
						registry.Set("mqtt_broker_observation_ok", 0)
						continue
					}
					registry.Set("mqtt_broker_observation_ok", 1)
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

		log.Info("realtime enabled", "adapter", "mqtt")
	}
	parsers := parser.NewPlatformRegistry(cfg.DataDir)
	engine := core.New(httpapi.ScopedRepository(repo), archivePort, bus, realtime, parsers, log)
	engine.SetIdentity(cfg.InstanceID)
	engine.PublishExternalTopics = cfg.PublishExternalTopics
	registry.SetProcessInfo(cfg.ProcessRole, cfg.InstanceID)
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
						processorLag, err := kafkaBus.GroupLag(ctx, core.GroupProcessor, model.TopicDeviceBusiness)
						if err != nil {
							continue
						}
						backlog := parserLag + processorLag
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
	var harness *aiadapter.HarnessPool
	if cfg.Runs(config.ComponentAIRuntime) {
		aiPlugins := aiadapter.NewProviderRegistry()
		engine.AIPlugins = aiPlugins
		providerID := cfg.AIProvider
		if providerID == "" {
			providerID = "deepseek"
		}
		providerConfig := ports.AIPluginConfig{Provider: providerID, BaseURL: cfg.AIBaseURL, Model: cfg.AIModel, APIKey: cfg.AIAPIKey}
		if aiProviderStore != nil {
			if persisted, found, err := aiProviderStore.LoadAIProviderConfig(ctx); err != nil {
				log.Warn("load persisted AI provider config", "error", err)
			} else if found {
				providerConfig = persisted
				log.Info("restored persisted AI provider", "provider", providerConfig.Provider, "model", providerConfig.Model)
			}
		}
		// Fill missing official provider settings from the deployment defaults.
		if providerConfig.Provider == "deepseek" {
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
		engine.AI = runtimeAI
		if cfg.AIHarnessURL != "" {
			harnessModel := cfg.AIHarnessModel
			if providerConfig.Model != "" {
				harnessModel = providerConfig.Model
			}
			var harnessErr error
			harness, harnessErr = aiadapter.NewHarnessPool(cfg.AIHarnessURL, cfg.AIHarnessToken, cfg.AIHarnessMCPURL, harnessModel, cfg.AIHarnessTimeout)
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
			log.Info("AI workflow harness enabled", "urls", cfg.AIHarnessURL, "instances", harness.Size(), "model", providerConfig.Model)
		}

	}
	var knowledgeRuntime *core.KnowledgeRuntime
	if cfg.Runs(config.ComponentManagement) && postgresRepo != nil {
		embeddingConfig := embedding.NormalizeConfig(ports.EmbeddingConfig{BaseURL: cfg.EmbeddingURL, Model: cfg.EmbeddingModel, APIKey: cfg.EmbeddingAPIKey, Dimensions: cfg.EmbeddingDimensions, BatchSize: cfg.EmbeddingBatchSize, QueryInstruction: cfg.EmbeddingQueryPrompt, TimeoutSeconds: int(cfg.EmbeddingTimeout / time.Second)})
		if saved, found, loadErr := postgresRepo.LoadEmbeddingConfig(ctx, false); loadErr != nil {
			log.Warn("load embedding config", "error", loadErr)
		} else if found {
			embeddingConfig = saved
		}
		activeConfig := embeddingConfig
		if saved, found, loadErr := postgresRepo.LoadEmbeddingConfig(ctx, true); loadErr != nil {
			log.Warn("load active embedding config", "error", loadErr)
		} else if found {
			activeConfig = saved
		}
		factory := func(c ports.EmbeddingConfig) (ports.KnowledgeBase, error) {
			client, err := embedding.ClientForConfig(c)
			if err != nil {
				return nil, err
			}
			return knowledge.NewPostgres(postgresRepo.Pool(), client, knowledge.PostgresOptions{Provider: c.BaseURL, Dimensions: c.Dimensions, Preprocessing: client.Signature(), ExpectedConfig: &c}), nil
		}
		var knowledgeErr error
		knowledgeRuntime, knowledgeErr = core.NewKnowledgeRuntime(embeddingConfig, activeConfig, factory, postgresRepo, postgresRepo, knowledgeStore, postgresRepo, engine.Archive, log)
		fatal(log, "initialize postgres knowledge index", knowledgeErr)
		engine.KB = knowledgeRuntime
		engine.KnowledgeReindex = knowledgeRuntime.StatusView
		go knowledgeRuntime.Run(ctx)
		log.Info("knowledge index enabled", "adapter", "postgres-pgvector", "embeddingModel", embeddingConfig.Model, "apiKeyConfigured", embeddingConfig.APIKey != "")
	} else {
		engine.KB = knowledge.NewLocal()
	}
	var opsService *opscenter.Service
	if cfg.Runs(config.ComponentManagement) || cfg.Runs(config.ComponentJobs) {
		opsService = newOpsCenter(cfg, opsPrefs, log)
	}
	if cfg.Runs(config.ComponentManagement) {
		// Dashboard provisioning is an idempotent upsert by UID.
		go opsService.RunDefaultDashboards(ctx)
	}
	if cfg.Runs(config.ComponentJobs) {
		// Alertmanager deduplicates identical alerts, so a notification
		// resent by another jobs replica after a rebalance has no effect.
		fatal(log, "start device alarm notifications", opsService.StartDeviceNotifications(ctx, bus, filepath.Join(cfg.DataDir, "ops-state", "device-notifications")))
	}
	components := core.Components{Parser: cfg.Runs(config.ComponentParser), Processor: cfg.Runs(config.ComponentProcessor), Jobs: cfg.Runs(config.ComponentJobs), OfflineScan: cfg.OfflineScan}
	fatal(log, "start engine", engine.StartWith(ctx, components))
	log.Info("process role started", "role", cfg.ProcessRole, "instance", cfg.InstanceID, "parser", components.Parser, "processor", components.Processor, "jobs", components.Jobs, "access", cfg.Runs(config.ComponentAccess), "management", cfg.Runs(config.ComponentManagement))
	var coordinator *protocolruntime.Coordinator
	if cfg.AccessCoordination && cfg.Runs(config.ComponentAccess) {
		coordinator = protocolruntime.NewCoordinator(repo, cfg.InstanceID+"-"+strconv.Itoa(os.Getpid())+"-"+uuid.NewString(), cfg.AccessNodeURL)
		go coordinator.Run(ctx)
	}
	protocolRuntime := protocolruntime.New(repo, func(c context.Context, raw model.RawMessage) error {
		_, _, err := engine.IngestRaw(c, raw)
		return err
	}, log, cfg.ModbusAllowedCIDRs...)
	protocolRuntime.SetCoordinator(coordinator)
	if cfg.Runs(config.ComponentAccess) {
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
	if cfg.Runs(config.ComponentAccess) {
		protocolListeners.Start(ctx)
		log.Info("active protocol runtime enabled", "transports", []string{"TCP", "UDP", "MODBUS_TCP (legacy)"})
	}
	if cfg.Runs(config.ComponentAccess) && mqttClient != nil {
		standardIngress := onboarding.New(repo, parsers, cfg.DataDir, cfg.ModbusAllowedCIDRs)
		standardIngress.Limiter = limits
		fatal(log, "subscribe standard mqtt", mqttClient.SubscribeStandard(func(c context.Context, tenant, product, device, kind string, payload []byte) error {
			raw, err := standardIngress.PrepareStandard(c, tenant, product, device, kind, "MQTT", payload)
			if err != nil {
				if errors.Is(err, onboarding.ErrAuth) || errors.Is(err, model.ErrInvalidIngress) {
					return mqttadapter.Reject(err)
				}
				return err
			}
			raw.ReceivedAt = mqttadapter.ReceivedAt(c)
			return engine.IngestMQTT(c, raw)
		}))
		fatal(log, "subscribe raw mqtt", mqttClient.SubscribeRaw(engine.IngestMQTT))
		fatal(log, "subscribe device state mqtt", mqttClient.SubscribeDeviceState(engine.UpdateDeviceState))
		fatal(log, "subscribe video mqtt", mqttClient.SubscribeVideo(func(c context.Context, v model.VideoAlarmEvent) error {
			_, _, err := engine.IngestVideo(c, v)
			return err
		}))
	}
	stopCapacity, capacityErr := startLocalCapacity(&cfg, log)
	if capacityErr != nil {
		log.Warn("local capacity controller unavailable", "error", capacityErr)
	}
	api := httpapi.New(cfg, engine, registry, log)
	api.SetRateLimiter(limits)
	storageStats := func() {
		if ch, ok := clickHouseRaw.(*clickhouseadapter.Repository); ok {
			st := ch.BatchStats()
			registry.Set("clickhouse_insert_batches", float64(st.Batches))
			registry.Set("clickhouse_insert_rows", float64(st.Rows))
			registry.Set("clickhouse_insert_failed", float64(st.Failed))
			registry.Set("clickhouse_insert_max_rows", float64(st.MaxRows))
			registry.Set("clickhouse_insert_inflight", float64(st.Inflight))
		}
		if pg, ok := postgresRaw.(*postgres.Repository); ok {
			if lag, usable := pg.ReplicaLag(); lag >= 0 || usable {
				registry.Set("postgres_replica_lag_ms", float64(lag))
				registry.Set("postgres_replica_reads", map[bool]float64{true: 1, false: 0}[usable])
			}
		}
	}
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				registry.Set("rate_limit_shared_errors", float64(limits.SharedErrors()))
				storageStats()
			}
		}
	}()
	var publishCommand func(context.Context, string, []byte, byte, bool) error
	if mqttClient != nil {
		publishCommand = mqttClient.Publish
	}
	var revokeUsername func(context.Context, string) error
	if emqxAdmin != nil {
		revokeUsername = emqxAdmin.RevokeUsername
	}
	api.SetDeviceOperations(publishCommand, revokeUsername)
	if cfg.Runs(config.ComponentJobs) {
		engine.RunSingleton(ctx, "credential-revocation", 30*time.Second, api.RetryCredentialRevocationsOnce)
		go api.RunOnboardingTasks(ctx)
		api.RunExternalData(ctx)
	}
	if mqttClient != nil {
		api.SetMQTTHealth(mqttClient.Probe)
		api.SetCapacityMQTT(mqttClient)
	}
	api.SetAIProviderRuntime(runtimeAI)
	if knowledgeRuntime != nil {
		api.SetEmbeddingRuntime(knowledgeRuntime)
	}
	if postgresRepo != nil {
		api.SetKnowledgeJobs(postgresRepo)
	}
	api.SetAIProviderStore(aiProviderStore)
	if harness != nil {
		api.SetAIWorkflowProvider(harness)
	}
	api.SetProtocolListeners(protocolListeners)
	if cfg.Runs(config.ComponentManagement) {
		api.SetOpsCenter(opsService)
	}
	if cfg.Runs(config.ComponentManagement) {
		// The live module is optional: when the media server is absent,
		// misconfigured or down, only live features report that state.
		if problem := cfg.Video.Problem(); cfg.Video.Deployed() && problem != nil {
			log.Error("camera live module configuration is invalid; live view stays unavailable", "error", problem)
		}
		newVideo := func() *video.Service {
			return video.New(cfg.Video, videoStore, api.VideoCameraLookup, api.VideoAuthorize, log)
		}
		if cfg.NodeURL == "" {
			liveVideo := newVideo()
			api.SetVideo(liveVideo)
			liveVideo.Start(ctx)
		} else {
			// Several API instances: one holds video/control and runs the
			// module; the others forward live routes to it.
			api.SetVideo(newVideo())
			control := &videoControl{}
			api.SetVideoRouting(control.state)
			go control.run(ctx, repo, cfg.InstanceID+"-"+uuid.NewString(), cfg.NodeURL, func(leaderCtx context.Context) {
				live := newVideo()
				api.SetVideo(live)
				live.Start(leaderCtx)
			}, func() { api.SetVideo(newVideo()) }, log, videoControlRenew)
		}
		if cfg.Video.Deployed() {
			log.Info("camera live module configured", "mediaApi", cfg.Video.MediaAPIURL, "transcode", cfg.Video.Transcode)
		}
	}
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: api.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 15 * time.Minute, IdleTimeout: 2 * time.Minute}
	go func() {
		log.Info("iot platform started", "addr", cfg.HTTPAddr, "devMode", cfg.DevMode)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server failed", "error", err)
			cancel()
		}
	}()
	<-ctx.Done()
	stopCapacity()
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

func retryHarnessProvider(ctx context.Context, runtimeAI ports.AIProviderRuntime, harness *aiadapter.HarnessPool, log *slog.Logger) {
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

// mqttInboxDir keeps the historical per-role path unless IOT_INSTANCE_ID is
// set, so upgrading a single instance never orphans unacknowledged messages.
// Replicas of one role must each set a distinct IOT_INSTANCE_ID.
func mqttInboxDir(cfg config.Config) string {
	role := cfg.ProcessRole
	if role == "" {
		role = config.RoleCombined
	}
	if cfg.InstanceIDExplicit {
		return filepath.Join(cfg.DataDir, "mqtt-inbox", role, cfg.InstanceID)
	}
	return filepath.Join(cfg.DataDir, "mqtt-inbox", role)
}

func positiveOr(value int64, fallback int) int {
	if value > 0 {
		return int(value)
	}
	return fallback
}
