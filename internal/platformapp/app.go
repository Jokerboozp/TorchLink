package platformapp

import (
	"context"
	"errors"
	"flag"
	"fmt"
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
	"iot-platform/internal/messagetopics"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/netguard"
	"iot-platform/internal/notify"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
	"iot-platform/internal/protocolbuild"
	"iot-platform/internal/protocolrunner"
	"iot-platform/internal/protocolruntime"
	"iot-platform/internal/ratelimit"
	"iot-platform/internal/retention"
	"iot-platform/internal/version"

	"iot-platform/internal/adapters/observability"
	"iot-platform/internal/opscenter"
	"iot-platform/internal/video"
)

func runProtocolRunner(log *slog.Logger) {
	socket := strings.TrimSpace(os.Getenv("IOT_PROTOCOL_RUNNER_SOCKET"))
	if socket == "" {
		fatal(log, "start protocol runner", errors.New("IOT_PROTOCOL_RUNNER_SOCKET is required"))
	}
	dir := strings.TrimSpace(os.Getenv("IOT_PROTOCOL_RUNNER_DIR"))
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "protocol-runner")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	fatal(log, "protocol runner", (&protocolrunner.Server{Dir: dir, Log: log}).Serve(ctx, socket))
}

// useProtocolRunner sends protocol compilation and execution to the runner;
// without one, uploaded code runs inside this process. Container deployments
// set IOT_PROTOCOL_SANDBOX=runner, which makes the runner mandatory.
func useProtocolRunner(cfg config.Config, log *slog.Logger) {
	if cfg.ProtocolRunnerSocket == "" {
		log.Warn("protocol code runs inside the platform process without isolation", "devMode", cfg.DevMode)
		return
	}
	client := protocolrunner.NewClient(cfg.ProtocolRunnerSocket)
	parser.SetExecutor(client)
	protocolbuild.SetBuilder(client)
	healthCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Health(healthCtx); err != nil {
		log.Warn("protocol runner not reachable yet", "socket", cfg.ProtocolRunnerSocket, "error", err)
	} else {
		log.Info("protocol code runs in the isolated protocol runner", "socket", cfg.ProtocolRunnerSocket)
	}
}

func Run(forcedRole string) {
	envFile := flag.String("env-file", "", "load a KEY=VALUE configuration file (existing environment variables take precedence)")
	flag.Parse()
	logLevel := new(slog.LevelVar)
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	if *envFile != "" {
		fatal(log, "load environment file", config.LoadEnvFile(*envFile))
	}
	if flag.Arg(0) == "healthcheck" {
		os.Exit(healthcheck(os.Getenv("IOT_HTTP_ADDR")))
	}
	if flag.Arg(0) == "migrate" {
		os.Exit(migrateCommand(flag.Args()[1:]))
	}
	level, err := config.LogLevel()
	fatal(log, "validate configuration", err)
	logLevel.Set(level)
	// The protocol runner needs none of the platform configuration or
	// secrets; it only compiles and executes uploaded protocol code.
	if strings.TrimSpace(os.Getenv("IOT_PROCESS_ROLE")) == config.RoleProtocolRunner {
		runProtocolRunner(log)
		return
	}
	cfg := config.Load()
	if forcedRole != "" {
		cfg.ProcessRole = forcedRole
	}
	fatal(log, "validate configuration", cfg.Validate())
	useProtocolRunner(cfg, log)
	a := &app{cfg: cfg, log: log}
	if cfg.Ops.LogPushURL != "" {
		// Host-run processes (local source debugging) ship their own logs; containers
		// are collected by the log collector and leave this unset.
		a.logPush = observability.NewLokiPush(cfg.Ops.LogPushURL, cfg.Ops.LogPushTenant, cfg.Ops.WithDefaults().LogServiceName)
		a.log = slog.New(observability.NewTeeHandler(log.Handler(), slog.NewJSONHandler(a.logPush, &slog.HandlerOptions{Level: logLevel})))
	}
	var cancel context.CancelFunc
	a.ctx, cancel = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	a.cancel = cancel
	a.openStorage()
	a.openMessaging()
	a.buildEngine()
	a.startAI()
	a.startKnowledge()
	a.startOps()
	a.startAPI()
	a.startAccess()
	a.startJobs()
	a.wireAPI()
	a.serve()
}

// app holds what one platform process opened and started; Run builds it in
// order: storage, messaging, engine, AI, knowledge, operations, API, access,
// jobs, then serves HTTP until a shutdown signal.
type app struct {
	ctx     context.Context
	cancel  context.CancelFunc
	cfg     config.Config
	log     *slog.Logger
	logPush *observability.LokiPush
	closers []func()

	repo              ports.Repository
	postgresRepo      *postgres.Repository
	opsPrefs          ports.OpsPreferenceStore
	videoStore        ports.VideoStore
	knowledgeStore    ports.KnowledgeReindexStore
	aiRunStore        ports.AIRunStore
	conversationStore ports.AIConversationStore
	manifestStore     ports.AIWorkflowManifestStore
	signalStore       ports.DeviceSignalStore
	telemetryStats    ports.DeviceTelemetryStats
	aiProviderStore   ports.AIProviderConfigStore
	postgresRaw       ports.RawMessageDatabase
	clickHouseRaw     ports.RawMessageDatabase
	limits            *ratelimit.Cluster
	archive           ports.Archive
	// cacheHealth reports the optional Redis cache for readiness.
	cacheHealth func(context.Context) error

	bus           ports.EventBus
	kafkaBus      *kafkaadapter.Bus
	kafkaSecurity kafkaadapter.SecurityConfig
	realtime      ports.RealtimePublisher
	registry      *metrics.Registry
	mqttClient    *mqttadapter.Client
	// The EMQX management API is optional: it enables immediate credential
	// revocation and broker-side drop counters for the platform session.
	emqxAdmin *mqttadapter.Admin

	parsers           *parser.Registry
	engine            *core.Engine
	runtimeAI         *aiadapter.RuntimeProvider
	harness           *aiadapter.HarnessPool
	aiSync            *aiadapter.ProviderSync
	knowledgeRuntime  *core.KnowledgeRuntime
	opsService        *opscenter.Service
	stopCapacity      func()
	api               *httpapi.Server
	protocolListeners *protocolruntime.Listeners
}

// every runs fn on each tick until the process stops.
func (a *app) every(interval time.Duration, fn func()) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-ticker.C:
				fn()
			}
		}
	}()
}

func (a *app) openStorage() {
	cfg, log := a.cfg, a.log
	a.repo = memory.NewRepository()
	a.opsPrefs, _ = a.repo.(ports.OpsPreferenceStore)
	a.videoStore, _ = a.repo.(ports.VideoStore)
	a.knowledgeStore, _ = a.repo.(ports.KnowledgeReindexStore)
	a.aiRunStore, _ = a.repo.(ports.AIRunStore)
	a.conversationStore, _ = a.repo.(ports.AIConversationStore)
	// Captured before the ClickHouse and Redis decorators, which embed only
	// ports.Repository and hide the dynamic Agent store.
	a.manifestStore, _ = a.repo.(ports.AIWorkflowManifestStore)
	a.signalStore, _ = a.repo.(ports.DeviceSignalStore)
	a.telemetryStats, _ = a.repo.(ports.DeviceTelemetryStats)
	a.aiProviderStore, _ = a.repo.(ports.AIProviderConfigStore)
	a.postgresRaw, _ = a.repo.(ports.RawMessageDatabase)
	if cfg.PostgresDSN != "" {
		r, err := postgres.NewWithOptions(a.ctx, cfg.PostgresDSN, postgres.PoolOptions{MaxConns: int32(positiveOr(cfg.PostgresMaxConns, 64)), MaxConnLifetime: cfg.PostgresMaxConnLifetime, HealthCheckPeriod: cfg.PostgresHealthCheckPeriod, ConnectTimeout: cfg.PostgresConnectTimeout, ReadDSN: cfg.PostgresReadDSN, MaxReplicaLag: cfg.PostgresMaxReplicaLag})
		fatal(log, "initialize postgres", err)
		a.repo, a.postgresRepo = r, r
		a.opsPrefs, a.videoStore, a.knowledgeStore, a.aiRunStore, a.conversationStore, a.manifestStore = r, r, r, r, r, r
		a.signalStore, a.telemetryStats = r, r
		if store, ok := any(r).(ports.AIProviderConfigStore); ok {
			a.aiProviderStore = store
		}
		a.postgresRaw = r
		log.Info("repository enabled", "adapter", "postgres")
	}
	if cfg.ClickHouseURL != "" {
		r, err := clickhouseadapter.NewWithOptions(a.ctx, cfg.ClickHouseURL, a.repo, clickhouseadapter.Options{Cluster: cfg.ClickHouseCluster, InsertQuorum: cfg.ClickHouseInsertQuorum, TelemetryTTLDays: cfg.Retention.TelemetryDays, RawTTLDays: cfg.Retention.ClickRawDays})
		fatal(log, "initialize clickhouse", err)
		a.repo = r
		a.clickHouseRaw = r
		// Telemetry properties live in ClickHouse while it is configured.
		a.telemetryStats = r
		if a.postgresRepo != nil {
			// ClickHouse keeps the properties of telemetry messages; the
			// ClickHouse repository restores them on read.
			a.postgresRepo.SetExternalTelemetryProperties(true)
		}
		log.Info("telemetry storage enabled", "adapter", "clickhouse")
	}
	// Rate budgets are shared through Redis when configured; otherwise (or
	// during a Redis outage) each process enforces budget/IOT_CLUSTER_INSTANCES.
	var sharedLimits ratelimit.Limiter
	if cfg.RedisAddr != "" || (cfg.RedisMasterName != "" && len(cfg.RedisSentinels) > 0) {
		redisClient := redisadapter.NewClient(redisadapter.Options{Addr: cfg.RedisAddr, Password: cfg.RedisPassword, MasterName: cfg.RedisMasterName, Sentinels: cfg.RedisSentinels})
		cache := redisadapter.New(a.repo, redisClient)
		a.cacheHealth = cache.CacheHealth
		a.repo = cache
		sharedLimits = redisadapter.NewRateLimiter(redisClient)
		log.Info("hot state cache enabled", "adapter", "redis")
	}
	a.limits = ratelimit.NewCluster(sharedLimits, int(cfg.ClusterInstances))
	if cfg.MinIOEndpoint != "" {
		m, err := minioadapter.New(cfg.MinIOEndpoint, cfg.MinIOAccessKey, cfg.MinIOSecretKey, cfg.MinIOUseTLS)
		fatal(log, "initialize minio", err)
		a.archive = m
		log.Info("archive enabled", "adapter", "minio")
	} else {
		archive, err := local.NewArchive(filepath.Join(cfg.DataDir, "objects"))
		fatal(log, "initialize local archive", err)
		a.archive = archive
	}
}

func (a *app) openMessaging() {
	cfg, log := a.cfg, a.log
	a.bus = local.NewBus()
	a.kafkaSecurity = kafkaadapter.SecurityConfig{Username: cfg.KafkaSASLUsername, Password: cfg.KafkaSASLPassword, Mechanism: cfg.KafkaSASLMechanism, TLS: cfg.KafkaTLS, CAFile: cfg.KafkaTLSCAFile}
	if len(cfg.KafkaBrokers) > 0 {
		kafkaBus, err := kafkaadapter.NewWithSecurity(cfg.KafkaBrokers, a.kafkaSecurity)
		fatal(log, "initialize Kafka security", err)
		kafkaBus.SetLogger(log)
		kafkaBus.SetAutoCreateTopics(cfg.KafkaAutoCreateTopics)
		// Parallel lanes keep each device's messages in order.
		kafkaBus.SetConsumerConcurrency(positiveOr(cfg.KafkaConsumerConcurrency, 64))
		kafkaBus.SetMaxBlock(cfg.ConsumerMaxBlock)
		a.kafkaBus, a.bus = kafkaBus, kafkaBus
		log.Info("event bus enabled", "adapter", "kafka", "brokers", cfg.KafkaBrokers)
	}
	a.realtime = local.NewRealtime()
	a.registry = metrics.New()
	if a.kafkaBus != nil {
		a.kafkaBus.SetMetrics(a.registry)
		// kafka_lag is the total backlog of this process's consumer groups;
		// kafka_lag_<group> breaks it down. Sampling errors keep the last value.
		a.every(15*time.Second, func() {
			lags, err := a.kafkaBus.ConsumerLag(a.ctx)
			if err != nil {
				log.Warn("sample kafka consumer lag", "error", err)
				return
			}
			var total int64
			for group, lag := range lags {
				total += lag
				a.registry.Set("kafka_lag_"+strings.NewReplacer("iot-platform-", "", "-", "_", ".", "_").Replace(group), float64(lag))
			}
			a.registry.Set("kafka_lag", float64(total))
		})
	}
	if cfg.EMQXAPIURL != "" && cfg.EMQXAPIKey != "" && cfg.EMQXAPISecret != "" {
		a.emqxAdmin = &mqttadapter.Admin{URL: cfg.EMQXAPIURL, Key: cfg.EMQXAPIKey, Secret: cfg.EMQXAPISecret, ToolUsername: cfg.MQTTToolUsername}
	}
	if cfg.MQTTBroker != "" {
		a.connectMQTT()
	}
}

func (a *app) connectMQTT() {
	cfg, log := a.cfg, a.log
	credentials := func() (string, string) { return cfg.MQTTUsername, cfg.MQTTPassword }
	if cfg.MQTTPassword == "" {
		manager := auth.New(cfg.JWTSecret)
		acl := []auth.ACLRule{
			{Permission: "allow", Action: "subscribe", Topic: "/iot/up/#"},
			{Permission: "allow", Action: "subscribe", Topic: "/external/raw/#"},
			{Permission: "allow", Action: "subscribe", Topic: "/jetlinks/raw/#"},
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
	a.mqttClient = mqttConnection
	if cfg.Runs(config.ComponentAccess) {
		a.registry.Set("mqtt_ingress_enabled", 1)
	}
	registry, emqxAdmin := a.registry, a.emqxAdmin
	var lastDropped int64 = -1
	tick := 0
	a.every(5*time.Second, func() {
		defer func() { tick++ }()
		registry.Set("mqtt_subscription_count", float64(mqttConnection.SubscriptionCount()))
		pending, rejected, corrupt := mqttConnection.InboxCounts()
		registry.Set("mqtt_inbox_pending", float64(pending))
		registry.Set("mqtt_inbox_rejected", float64(rejected))
		registry.Set("mqtt_inbox_corrupt", float64(corrupt))
		// Messages the broker drops for this session never reach the
		// inbox although devices got PUBACK; only the broker can count them.
		if emqxAdmin == nil {
			registry.Set("mqtt_broker_observation_ok", 0)
			return
		}
		if tick%3 != 0 {
			return
		}
		sampleCtx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
		queued, dropped, sampleErr := emqxAdmin.SessionQueue(sampleCtx, mqttConnection.ClientID())
		cancel()
		if sampleErr != nil {
			registry.Set("mqtt_broker_observation_ok", 0)
			return
		}
		registry.Set("mqtt_broker_observation_ok", 1)
		registry.Set("mqtt_broker_queue", float64(queued))
		registry.Set("mqtt_broker_dropped", float64(dropped))
		if lastDropped >= 0 && dropped > lastDropped {
			log.Error("MQTT broker dropped messages for the platform session; ingestion is slower than devices publish", "dropped", dropped-lastDropped, "brokerQueue", queued)
		}
		lastDropped = dropped
	})
	if cfg.AccessCoordination {
		fatal(log, "configure shared MQTT ingestion", a.mqttClient.ConfigureSharedSubscriptions("iot-access"))
	}
	a.realtime = a.mqttClient
	log.Info("realtime enabled", "adapter", "mqtt")
}

func (a *app) buildEngine() {
	cfg, log := a.cfg, a.log
	a.parsers = parser.NewPlatformRegistry(cfg.DataDir)
	engine := core.New(httpapi.ScopedRepository(a.repo), a.archive, a.bus, a.realtime, a.parsers, log)
	a.engine = engine
	engine.AIRuns = a.aiRunStore
	engine.AIConversations = a.conversationStore
	engine.DeviceSignals, engine.TelemetryStats = a.signalStore, a.telemetryStats
	engine.SignalOptions = core.DeviceSignalOptions{Window: cfg.DeviceSignalWindow, RaiseAlarms: cfg.DeviceSignalAlarm}
	engine.SetIdentity(cfg.InstanceID)
	engine.PublishExternalTopics = cfg.PublishExternalTopics
	engine.MediaOutbound = netguard.Policy{Allowed: cfg.ExternalDataAllowedCIDRs}
	a.registry.SetProcessInfo(cfg.ProcessRole, cfg.InstanceID, version.Version)
	log.Info("platform build", "version", version.Version, "revision", version.Commit(), "role", cfg.ProcessRole)
	if a.kafkaBus != nil && cfg.IngestMaxBacklog > 0 {
		a.watchBacklog()
	}
	var legacyRaw ports.RawMessageReader
	if reader, ok := a.archive.(ports.RawMessageReader); ok {
		legacyRaw = reader
	}
	engine.RawStore = rawstore.New(rawstore.Config{
		PostgreSQL:               a.postgresRaw,
		ClickHouse:               a.clickHouseRaw,
		Resolver:                 a.repo,
		Legacy:                   legacyRaw,
		HighFrequencyIntervalSec: cfg.RawHighFrequencyIntervalSec,
	})
	engine.Metrics = a.registry
}

// watchBacklog applies backpressure: stop taking new raw messages while
// parsing and storage are far behind, so ingest does not starve them of
// database capacity.
func (a *app) watchBacklog() {
	limit := a.cfg.IngestMaxBacklog
	paused := false
	a.every(15*time.Second, func() {
		parserLag, err := a.kafkaBus.GroupLag(a.ctx, "parser", model.TopicRaw)
		if err != nil {
			return
		}
		processorLag, err := a.kafkaBus.GroupLag(a.ctx, core.GroupProcessor, model.TopicDeviceBusiness)
		if err != nil {
			return
		}
		backlog := parserLag + processorLag
		a.registry.Set("pipeline_backlog", float64(backlog))
		next := paused
		if !paused && backlog > limit {
			next = true
		} else if paused && backlog < limit*8/10 {
			next = false
		}
		if next != paused {
			paused = next
			a.engine.SetIngestPaused(paused)
			if paused {
				a.log.Error("raw ingest paused: processing backlog above limit", "backlog", backlog, "limit", limit)
			} else {
				a.log.Info("raw ingest resumed", "backlog", backlog)
			}
		}
		a.registry.Set("ingest_paused", map[bool]float64{true: 1, false: 0}[paused])
	})
}

func (a *app) startAI() {
	cfg, log, engine := a.cfg, a.log, a.engine
	if !cfg.Runs(config.ComponentAIRuntime) {
		return
	}
	aiPlugins := aiadapter.NewProviderRegistry()
	engine.AIPlugins = aiPlugins
	providerID := cfg.AIProvider
	if providerID == "" {
		providerID = "deepseek"
	}
	providerConfig := ports.AIPluginConfig{Provider: providerID, BaseURL: cfg.AIBaseURL, Model: cfg.AIModel, APIKey: cfg.AIAPIKey}
	if a.aiProviderStore != nil {
		if persisted, found, err := a.aiProviderStore.LoadAIProviderConfig(a.ctx); err != nil {
			log.Warn("load persisted AI provider config", "error", err)
		} else if found {
			providerConfig = persisted
			log.Info("restored persisted AI provider", "provider", providerConfig.Provider, "model", providerConfig.Model)
		}
	}
	// Fill missing official provider settings from the deployment defaults.
	completeProvider := func(c ports.AIPluginConfig) ports.AIPluginConfig {
		if c.Provider != "deepseek" {
			return c
		}
		if c.BaseURL == "" {
			c.BaseURL = cfg.AIBaseURL
			if c.BaseURL == "" {
				c.BaseURL = "https://api.deepseek.com"
			}
		}
		if c.Model == "" {
			for _, item := range aiPlugins.List() {
				if item.ID == "deepseek" {
					c.Model = item.DefaultModel
					break
				}
			}
		}
		if c.APIKey == "" {
			c.APIKey = cfg.AIAPIKey
		}
		return c
	}
	providerConfig = completeProvider(providerConfig)
	var err error
	a.runtimeAI, err = aiadapter.NewRuntimeProvider(aiPlugins, providerConfig)
	fatal(log, "initialize AI provider plugin", err)
	engine.AI = a.runtimeAI
	if cfg.AIHarnessURL != "" {
		harnessModel := cfg.AIHarnessModel
		if providerConfig.Model != "" {
			harnessModel = providerConfig.Model
		}
		a.harness, err = aiadapter.NewHarnessPool(cfg.AIHarnessURL, cfg.AIHarnessToken, cfg.AIHarnessMCPURL, harnessModel, cfg.AIHarnessTimeout)
		fatal(log, "initialize AI workflow harness", err)
		if providerConfig.Provider == "deepseek" && strings.TrimSpace(providerConfig.APIKey) == "" {
			log.Warn("DeepSeek API key is not configured; enter it in model management")
		}
		engine.AIWorkflows = a.harness
		engine.BusinessRunTimeout = cfg.AIBusinessTimeout
		engine.ChatRunTimeout = cfg.AIHarnessTimeout
		// Chat and business AI runs sign their MCP credentials with the
		// Harness key, which /mcp/harness verifies.
		engine.HarnessTokens = auth.New(auth.HarnessSecret(cfg.JWTSecret, cfg.HarnessJWTSecret))
		log.Info("AI workflow harness enabled", "urls", cfg.AIHarnessURL, "instances", a.harness.Size(), "model", providerConfig.Model)
	}
	a.aiSync = aiadapter.NewProviderSync(a.runtimeAI, a.harness, a.aiProviderStore, a.manifestStore, completeProvider)
	// Compose starts the Harness after the API (it calls the platform MCP
	// endpoint), so the first pass may fail; later passes also follow
	// settings saved on other replicas and Harness restarts.
	go reconcileAI(a.ctx, a.aiSync, log)
}

func (a *app) startKnowledge() {
	cfg, log, postgresRepo := a.cfg, a.log, a.postgresRepo
	embedding.SetLocalHosts(cfg.LocalAIHosts)
	aiadapter.SetLocalHosts(cfg.LocalAIHosts)
	if !cfg.Runs(config.ComponentManagement) || postgresRepo == nil {
		a.engine.KB = knowledge.NewLocal()
		return
	}
	embeddingConfig := embedding.NormalizeConfig(ports.EmbeddingConfig{BaseURL: cfg.EmbeddingURL, Model: cfg.EmbeddingModel, APIKey: cfg.EmbeddingAPIKey, Dimensions: cfg.EmbeddingDimensions, BatchSize: cfg.EmbeddingBatchSize, QueryInstruction: cfg.EmbeddingQueryPrompt, TimeoutSeconds: int(cfg.EmbeddingTimeout / time.Second)})
	if saved, found, loadErr := postgresRepo.LoadEmbeddingConfig(a.ctx, false); loadErr != nil {
		log.Warn("load embedding config", "error", loadErr)
	} else if found {
		embeddingConfig = saved
	}
	activeConfig := embeddingConfig
	if saved, found, loadErr := postgresRepo.LoadEmbeddingConfig(a.ctx, true); loadErr != nil {
		log.Warn("load active embedding config", "error", loadErr)
	} else if found {
		activeConfig = saved
	}
	var reranker ports.Reranker
	if cfg.RerankURL != "" {
		r, rerankErr := embedding.NewReranker(cfg.RerankURL, cfg.RerankTimeout)
		fatal(log, "initialize knowledge reranker", rerankErr)
		reranker = r
	}
	factory := func(c ports.EmbeddingConfig) (ports.KnowledgeBase, error) {
		client, err := embedding.ClientForConfig(c)
		if err != nil {
			return nil, err
		}
		return knowledge.NewPostgres(postgresRepo.Pool(), client, knowledge.PostgresOptions{Provider: c.BaseURL, Dimensions: c.Dimensions, Preprocessing: client.Signature(), ExpectedConfig: &c, Reranker: reranker}), nil
	}
	runtime, err := core.NewKnowledgeRuntime(embeddingConfig, activeConfig, factory, postgresRepo, postgresRepo, a.knowledgeStore, postgresRepo, a.engine.Archive, log)
	fatal(log, "initialize postgres knowledge index", err)
	a.knowledgeRuntime = runtime
	a.engine.KB = runtime
	a.engine.KnowledgeReindex = runtime.StatusView
	go runtime.Run(a.ctx)
	log.Info("knowledge index enabled", "adapter", "postgres-pgvector", "embeddingModel", embeddingConfig.Model, "bundledService", embedding.IsLocal(embeddingConfig), "apiKeyConfigured", embeddingConfig.APIKey != "", "rerank", reranker != nil)
}

func (a *app) startOps() {
	cfg := a.cfg
	if cfg.Runs(config.ComponentManagement) || cfg.Runs(config.ComponentJobs) {
		a.opsService = newOpsCenter(cfg, a.opsPrefs, a.log)
	}
	if cfg.Runs(config.ComponentManagement) {
		// Dashboard provisioning is an idempotent upsert by UID.
		go a.opsService.RunDefaultDashboards(a.ctx)
	}
	if cfg.Runs(config.ComponentJobs) {
		// Alertmanager deduplicates identical alerts, so a notification
		// resent by another jobs replica after a rebalance has no effect.
		fatal(a.log, "start device alarm notifications", a.opsService.StartDeviceNotifications(a.ctx, a.bus, filepath.Join(cfg.DataDir, "ops-state", "device-notifications")))
	}
}

// startAPI creates the HTTP API before the engine's consumers start, so the
// user and scope resolvers are installed before anything is published,
// including in worker-only processes which expose no business routes.
func (a *app) startAPI() {
	log := a.log
	components := core.Components{Parser: a.cfg.Runs(config.ComponentParser), Processor: a.cfg.Runs(config.ComponentProcessor), Jobs: a.cfg.Runs(config.ComponentJobs), OfflineScan: a.cfg.OfflineScan, DeviceSignals: a.cfg.DeviceSignalInterval}
	var capacityErr error
	a.stopCapacity, capacityErr = startLocalCapacity(&a.cfg, log)
	if capacityErr != nil {
		log.Warn("local capacity controller unavailable", "error", capacityErr)
	}
	cfg := a.cfg
	a.api = httpapi.New(cfg, a.engine, a.registry, log)
	if a.cacheHealth != nil {
		a.api.SetOptionalHealth("cache", a.cacheHealth)
	}
	if cfg.Notify.Enabled && (cfg.Runs(config.ComponentManagement) || cfg.Runs(config.ComponentJobs)) {
		var store notify.Store = notify.NewMemoryStore()
		if a.postgresRepo != nil {
			store = a.postgresRepo.NotificationStore()
		}
		cipher, err := notify.NewCipher(cfg.JWTSecret)
		fatal(log, "initialize alarm notifications", err)
		notifications := &notify.Service{Store: store, Directory: a.api.NotificationDirectory(), Cipher: cipher, Sender: notify.NewSender(cfg.Notify.AllowedCIDRs), Metrics: a.registry, Log: log, WebURL: cfg.Notify.WebURL}
		a.api.SetNotifications(notifications)
		if cfg.Runs(config.ComponentJobs) {
			// Tasks are leased in the store, so every jobs replica may deliver.
			fatal(log, "subscribe alarm notifications", a.bus.Subscribe(a.ctx, model.TopicAlarmReported, "alarm-notifications", notifications.HandleReported))
			fatal(log, "subscribe alarm recovery notifications", a.bus.Subscribe(a.ctx, model.TopicAlarmRecovered, "alarm-recovery-notifications", notifications.HandleRecovered))
			go notifications.Run(a.ctx, 2*time.Second)
		}
	}
	fatal(log, "start engine", a.engine.StartWith(a.ctx, components))
	log.Info("process role started", "role", cfg.ProcessRole, "instance", cfg.InstanceID, "parser", components.Parser, "processor", components.Processor, "jobs", components.Jobs, "access", cfg.Runs(config.ComponentAccess), "management", cfg.Runs(config.ComponentManagement))
}

// startAccess starts device ingress: the active protocol runtime, protocol
// listeners and the MQTT subscriptions.
func (a *app) startAccess() {
	cfg, log, engine, repo := a.cfg, a.log, a.engine, a.repo
	ingest := func(c context.Context, raw model.RawMessage) error {
		_, _, err := engine.IngestRaw(c, raw)
		return err
	}
	var coordinator *protocolruntime.Coordinator
	if cfg.AccessCoordination && cfg.Runs(config.ComponentAccess) {
		coordinator = protocolruntime.NewCoordinator(repo, cfg.InstanceID+"-"+strconv.Itoa(os.Getpid())+"-"+uuid.NewString(), cfg.AccessNodeURL)
		go coordinator.Run(a.ctx)
	}
	protocolRuntime := protocolruntime.New(repo, ingest, log, cfg.ModbusAllowedCIDRs...)
	protocolRuntime.SetCoordinator(coordinator)
	if cfg.Runs(config.ComponentAccess) {
		protocolRuntime.Start(a.ctx)
	}
	listeners := protocolruntime.NewListeners(repo, cfg.DataDir, ingest, log)
	a.protocolListeners = listeners
	listeners.SetAllowedCIDRs(cfg.ModbusAllowedCIDRs)
	listeners.SetMaxSessions(int(cfg.ProtocolListenerMaxSessions))
	a.every(5*time.Second, func() {
		a.registry.Set("protocol_listener_rejected_total", float64(listeners.RejectedSessions()))
		a.registry.Set("protocol_listener_dropped_frames_total", float64(listeners.DroppedFrames()))
	})
	listeners.SetConnectionReporter(engine.ReportConnection)
	listeners.SetCoordinator(coordinator)
	if cfg.Runs(config.ComponentAccess) {
		listeners.Start(a.ctx)
		log.Info("active protocol runtime enabled", "transports", []string{"TCP", "UDP", "MODBUS_TCP (legacy)"})
	}
	if cfg.Runs(config.ComponentAccess) && a.mqttClient != nil {
		standardIngress := onboarding.New(repo, a.parsers, cfg.DataDir, cfg.ModbusAllowedCIDRs)
		standardIngress.Limiter = a.limits
		fatal(log, "subscribe standard mqtt", a.mqttClient.SubscribeStandard(func(c context.Context, tenant, product, device, kind string, payload []byte) error {
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
		fatal(log, "subscribe raw mqtt", a.mqttClient.SubscribeRaw(engine.IngestMQTT))
		fatal(log, "subscribe device state mqtt", a.mqttClient.SubscribeDeviceState(engine.UpdateDeviceState))
	}
	a.api.SetRateLimiter(a.limits)
	a.every(15*time.Second, func() {
		a.registry.Set("rate_limit_shared_errors", float64(a.limits.SharedErrors()))
		a.storageStats()
	})
	var publishCommand func(context.Context, string, []byte, byte, bool) error
	if a.mqttClient != nil {
		publishCommand = a.mqttClient.Publish
	}
	var revokeUsername func(context.Context, string) error
	if a.emqxAdmin != nil {
		revokeUsername = a.emqxAdmin.RevokeUsername
	}
	a.api.SetDeviceOperations(publishCommand, revokeUsername)
	if a.emqxAdmin != nil {
		a.api.SetMessageTopicMQTTReadiness(a.emqxAdmin.CheckTopicAuthorization)
	}
	if cfg.KafkaAdminURL != "" {
		admin, err := kafkaadapter.NewConsumerAdmin(cfg.KafkaBrokers, a.kafkaSecurity, cfg.KafkaAdminURL, cfg.KafkaAdminUsername, cfg.KafkaAdminPassword)
		fatal(log, "initialize Kafka consumer administration", err)
		a.closers = append(a.closers, admin.Close)
		admin.SetConsumerBrokers(cfg.KafkaPublicBrokers)
		a.api.SetMessageTopicKafkaAdmin(admin)
	}
}

func (a *app) storageStats() {
	if ch, ok := a.clickHouseRaw.(*clickhouseadapter.Repository); ok {
		st := ch.BatchStats()
		a.registry.Set("clickhouse_insert_batches", float64(st.Batches))
		a.registry.Set("clickhouse_insert_rows", float64(st.Rows))
		a.registry.Set("clickhouse_insert_failed", float64(st.Failed))
		a.registry.Set("clickhouse_insert_max_rows", float64(st.MaxRows))
		a.registry.Set("clickhouse_insert_inflight", float64(st.Inflight))
	}
	if pg, ok := a.postgresRaw.(*postgres.Repository); ok {
		if lag, usable := pg.ReplicaLag(); lag >= 0 || usable {
			a.registry.Set("postgres_replica_lag_ms", float64(lag))
			a.registry.Set("postgres_replica_reads", map[bool]float64{true: 1, false: 0}[usable])
		}
	}
}

func (a *app) startJobs() {
	cfg, ctx, engine, api := a.cfg, a.ctx, a.engine, a.api
	if cfg.Runs(config.ComponentJobs) && a.postgresRepo != nil && cfg.Retention.Enabled {
		location, err := time.LoadLocation(cfg.Retention.Timezone)
		if err != nil {
			location = time.Local
		}
		purger := retention.New(cfg.Retention, a.postgresRepo, a.registry, a.log, location)
		engine.RunSingleton(ctx, "retention", 10*time.Minute, purger.Tick)
	}
	if !cfg.Runs(config.ComponentJobs) {
		return
	}
	engine.RunSingleton(ctx, "credential-revocation", 30*time.Second, api.RetryCredentialRevocationsOnce)
	engine.RunSingleton(ctx, "object-cleanup", 10*time.Minute, engine.CleanupObjectsOnce)
	engine.RunSingleton(ctx, "message-topic-revocation", 30*time.Second, api.RetryMessageTopicRevocationsOnce)
	queryScheduler := messagetopics.NewQueryScheduler(engine.MessageTopics)
	engine.RunSingleton(ctx, "message-topic-queries", 5*time.Second, func(runCtx context.Context) error {
		return queryScheduler.RunOnce(runCtx, func(sendCtx context.Context, protocol, topic string, payload []byte) error {
			if protocol == "mqtt" && cfg.MQTTBroker != "" && engine.Realtime != nil {
				return engine.Realtime.Publish(sendCtx, topic, payload, 1, false)
			}
			if protocol == "kafka" && len(cfg.KafkaBrokers) > 0 && engine.Bus != nil {
				return engine.Bus.Publish(sendCtx, topic, topic, payload)
			}
			return fmt.Errorf("%s 消息通道未启用", protocol)
		})
	})
	go api.RunOnboardingTasks(ctx)
	api.RunExternalData(ctx)
}

// wireAPI hands the API the optional runtimes it reports on and manages.
func (a *app) wireAPI() {
	cfg, api := a.cfg, a.api
	if a.mqttClient != nil {
		api.SetMQTTHealth(a.mqttClient.Probe)
		api.SetCapacityMQTT(a.mqttClient)
	}
	api.SetAIProviderRuntime(a.runtimeAI)
	if a.knowledgeRuntime != nil {
		api.SetEmbeddingRuntime(a.knowledgeRuntime)
	}
	if a.postgresRepo != nil {
		api.SetKnowledgeJobs(a.postgresRepo)
	}
	api.SetAIProviderStore(a.aiProviderStore)
	if a.harness != nil {
		api.SetAIWorkflowProvider(a.harness)
	}
	if a.aiSync != nil {
		api.SetAISync(a.aiSync, a.manifestStore)
	}
	api.SetProtocolListeners(a.protocolListeners)
	if cfg.Runs(config.ComponentManagement) {
		api.SetOpsCenter(a.opsService)
		a.startVideo()
	}
}

// startVideo runs the optional live module: when the media server is absent,
// misconfigured or down, only live features report that state.
func (a *app) startVideo() {
	cfg, log, api := a.cfg, a.log, a.api
	if problem := cfg.Video.Problem(); cfg.Video.Deployed() && problem != nil {
		log.Error("camera live module configuration is invalid; live view stays unavailable", "error", problem)
	}
	newVideo := func() *video.Service {
		return video.New(cfg.Video, a.videoStore, api.VideoCameraLookup, api.VideoAuthorize, log)
	}
	if cfg.NodeURL == "" {
		liveVideo := newVideo()
		api.SetVideo(liveVideo)
		liveVideo.Start(a.ctx)
	} else {
		// Several API instances: one holds video/control and runs the
		// module; the others forward live routes to it.
		api.SetVideo(newVideo())
		control := &videoControl{}
		api.SetVideoRouting(control.state)
		go control.run(a.ctx, a.repo, cfg.InstanceID+"-"+uuid.NewString(), cfg.NodeURL, func(leaderCtx context.Context) {
			live := newVideo()
			api.SetVideo(live)
			live.Start(leaderCtx)
		}, func() { api.SetVideo(newVideo()) }, log, videoControlRenew)
	}
	if cfg.Video.Deployed() {
		log.Info("camera live module configured", "mediaApi", cfg.Video.MediaAPIURL, "transcode", cfg.Video.Transcode)
	}
}

// serve answers HTTP until a shutdown signal or a listener failure, then
// stops the process's components.
func (a *app) serve() {
	cfg, log := a.cfg, a.log
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: a.api.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 15 * time.Minute, IdleTimeout: 2 * time.Minute}
	go func() {
		log.Info("iot platform started", "addr", cfg.HTTPAddr, "devMode", cfg.DevMode)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server failed", "error", err)
			a.cancel()
		}
	}()
	<-a.ctx.Done()
	a.stopCapacity()
	shutdown, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	_ = server.Shutdown(shutdown)
	_ = a.realtime.Close()
	_ = a.bus.Close()
	_ = a.repo.Close()
	log.Info("iot platform stopped")
	if a.logPush != nil {
		flushCtx, flushCancel := context.WithTimeout(context.Background(), 5*time.Second)
		a.logPush.Close(flushCtx)
		flushCancel()
	}
	for _, closeFn := range a.closers {
		closeFn()
	}
}
func fatal(log *slog.Logger, msg string, err error) {
	if err != nil {
		log.Error(msg, "error", err)
		os.Exit(1)
	}
}

// aiReconcileInterval is how quickly API replicas and Harness instances pick
// up AI settings saved elsewhere or lost by a Harness restart.
const aiReconcileInterval = 10 * time.Second

func reconcileAI(ctx context.Context, reconciler *aiadapter.ProviderSync, log *slog.Logger) {
	ticker := time.NewTicker(aiReconcileInterval)
	defer ticker.Stop()
	lastErr := ""
	for {
		passCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		err := reconciler.Reconcile(passCtx)
		cancel()
		switch {
		case err != nil && err.Error() != lastErr:
			log.Warn("AI settings reconciliation incomplete; retrying", "error", err)
			lastErr = err.Error()
		case err == nil && lastErr != "":
			log.Info("AI settings reconciled")
			lastErr = ""
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
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
