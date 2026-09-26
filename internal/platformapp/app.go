package platformapp /* 声明 platformapp 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                /* 执行当前语句并推进处理流程。 */
	"errors"                 /* 执行当前语句并推进处理流程。 */
	"flag"                   /* 执行当前语句并推进处理流程。 */
	"github.com/google/uuid" /* 执行当前语句并推进处理流程。 */
	"log/slog"               /* 执行当前语句并推进处理流程。 */
	"net/http"               /* 执行当前语句并推进处理流程。 */
	"os"                     /* 执行当前语句并推进处理流程。 */
	"os/signal"              /* 执行当前语句并推进处理流程。 */
	"path/filepath"
	"strconv" /* 执行当前语句并推进处理流程。 */
	"strings" /* 执行当前语句并推进处理流程。 */
	"syscall" /* 执行当前语句并推进处理流程。 */
	"time"    /* 执行当前语句并推进处理流程。 */

	aiadapter "iot-platform/internal/adapters/ai"                 /* 执行当前语句并推进处理流程。 */
	clickhouseadapter "iot-platform/internal/adapters/clickhouse" /* 执行当前语句并推进处理流程。 */
	kafkaadapter "iot-platform/internal/adapters/kafka"           /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/knowledge"                    /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/local"                        /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory"                       /* 执行当前语句并推进处理流程。 */
	minioadapter "iot-platform/internal/adapters/minio"           /* 执行当前语句并推进处理流程。 */
	mqttadapter "iot-platform/internal/adapters/mqtt"             /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/postgres"                     /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/rawstore"                     /* 执行当前语句并推进处理流程。 */
	redisadapter "iot-platform/internal/adapters/redis"           /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/auth"                                  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"                                /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"                                  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/httpapi"                               /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics"                               /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"                                 /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/onboarding"                            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"                                /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports"                                 /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/protocolruntime"                       /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/adapters/observability"
) /* 结束当前表达式或代码块。 */

func Run(forcedRole string) { /* 定义 Run 函数。 */
	envFile := flag.String("env-file", "", "load a KEY=VALUE configuration file (existing environment variables take precedence)") /* 更新 envFile 的值。 */
	flag.Parse()                                                                                                                   /* 执行当前语句并推进处理流程。 */
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))                                   /* 更新 log 的值。 */
	if *envFile != "" {                                                                                                            /* 判断条件并选择处理分支。 */
		fatal(log, "load environment file", config.LoadEnvFile(*envFile)) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	cfg := config.Load()  /* 更新 cfg 的值。 */
	if forcedRole != "" { /* 判断条件并选择处理分支。 */
		cfg.ProcessRole = forcedRole /* 更新 cfg.ProcessRole 的值。 */
	} /* 结束当前表达式或代码块。 */
	fatal(log, "validate configuration", cfg.Validate()) /* 执行当前语句并推进处理流程。 */
	var logPush *observability.LokiPush
	if cfg.Ops.LogPushURL != "" {
		// Host-run processes (local source debugging) ship their own logs; containers
		// are collected by the log collector and leave this unset.
		logPush = observability.NewLokiPush(cfg.Ops.LogPushURL, cfg.Ops.LogPushTenant, cfg.Ops.WithDefaults().LogServiceName)
		log = slog.New(observability.NewTeeHandler(log.Handler(), slog.NewJSONHandler(logPush, &slog.HandlerOptions{Level: slog.LevelInfo})))
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM) /* 更新 cancel 的值。 */
	defer cancel()                                                                           /* 安排函数结束时执行清理。 */
	var repo ports.Repository = memory.NewRepository()                                       /* 声明 repo。 */
	opsPrefs, _ := repo.(ports.OpsPreferenceStore)
	var aiProviderStore ports.AIProviderConfigStore          /* 声明 aiProviderStore。 */
	if store, ok := repo.(ports.AIProviderConfigStore); ok { /* 判断条件并选择处理分支。 */
		aiProviderStore = store /* 更新 aiProviderStore 的值。 */
	} /* 结束当前表达式或代码块。 */
	var postgresRaw, clickHouseRaw ports.RawMessageDatabase /* 声明 postgresRaw。 */
	if raw, ok := repo.(ports.RawMessageDatabase); ok {     /* 判断条件并选择处理分支。 */
		postgresRaw = raw /* 更新 postgresRaw 的值。 */
	} /* 结束当前表达式或代码块。 */
	if cfg.PostgresDSN != "" { /* 判断条件并选择处理分支。 */
		r, err := postgres.New(ctx, cfg.PostgresDSN) /* 更新 err 的值。 */
		fatal(log, "initialize postgres", err)       /* 执行当前语句并推进处理流程。 */
		repo = r                                     /* 更新 repo 的值。 */
		opsPrefs = r
		if store, ok := any(r).(ports.AIProviderConfigStore); ok { /* 判断条件并选择处理分支。 */
			aiProviderStore = store /* 更新 aiProviderStore 的值。 */
		} /* 结束当前表达式或代码块。 */
		postgresRaw = r                                       /* 更新 postgresRaw 的值。 */
		log.Info("repository enabled", "adapter", "postgres") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if cfg.ClickHouseURL != "" { /* 判断条件并选择处理分支。 */
		r, clickErr := clickhouseadapter.New(ctx, cfg.ClickHouseURL, repo) /* 更新 clickErr 的值。 */
		fatal(log, "initialize clickhouse", clickErr)                      /* 执行当前语句并推进处理流程。 */
		repo = r                                                           /* 更新 repo 的值。 */
		clickHouseRaw = r                                                  /* 更新 clickHouseRaw 的值。 */
		log.Info("telemetry storage enabled", "adapter", "clickhouse")     /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if cfg.RedisAddr != "" { /* 判断条件并选择处理分支。 */
		repo = redisadapter.New(repo, cfg.RedisAddr, cfg.RedisPassword) /* 更新 repo 的值。 */
		log.Info("hot state cache enabled", "adapter", "redis")         /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	var archivePort ports.Archive /* 声明 archivePort。 */
	if cfg.MinIOEndpoint != "" {  /* 判断条件并选择处理分支。 */
		m, err := minioadapter.New(cfg.MinIOEndpoint, cfg.MinIOAccessKey, cfg.MinIOSecretKey, cfg.MinIOUseTLS) /* 更新 err 的值。 */
		fatal(log, "initialize minio", err)                                                                    /* 执行当前语句并推进处理流程。 */
		archivePort = m                                                                                        /* 更新 archivePort 的值。 */
		log.Info("archive enabled", "adapter", "minio")                                                        /* 执行当前语句并推进处理流程。 */
	} else { /* 结束当前表达式或代码块。 */
		archive, err := local.NewArchive(filepath.Join(cfg.DataDir, "objects")) /* 更新 err 的值。 */
		fatal(log, "initialize local archive", err)                             /* 执行当前语句并推进处理流程。 */
		archivePort = archive                                                   /* 更新 archivePort 的值。 */
	} /* 结束当前表达式或代码块。 */
	localBus := local.NewBus()
	// Without Kafka, automatic alarm analysis still runs apart from the alarm
	// path, with the same concurrency as its Kafka consumer group.
	localBus.SetAsyncTopic(model.TopicAlarmRaised, positiveOr(cfg.AIAnalysisConcurrency, 1), 1000)
	var bus ports.EventBus = localBus /* 声明 bus。 */
	var kafkaBus *kafkaadapter.Bus
	if len(cfg.KafkaBrokers) > 0 { /* 判断条件并选择处理分支。 */
		kafkaBus = kafkaadapter.New(cfg.KafkaBrokers)
		// Parallel lanes keep each device's (or alarm's) messages in order;
		// automatic alarm analysis has its own, smaller limit.
		kafkaBus.SetConsumerConcurrency(positiveOr(cfg.KafkaConsumerConcurrency, 8), map[string]int{model.TopicAlarmRaised: positiveOr(cfg.AIAnalysisConcurrency, 1)})
		bus = kafkaBus
		log.Info("event bus enabled", "adapter", "kafka", "brokers", cfg.KafkaBrokers) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	localRealtime := local.NewRealtime()                 /* 更新 localRealtime 的值。 */
	var realtime ports.RealtimePublisher = localRealtime /* 声明 realtime。 */
	registry := metrics.New()                            /* 更新 registry 的值。 */
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
	var mqttClient *mqttadapter.Client /* 声明 mqttClient。 */
	if cfg.MQTTBroker != "" {          /* 判断条件并选择处理分支。 */
		credentials := func() (string, string) { return cfg.MQTTUsername, cfg.MQTTPassword } /* 更新 credentials 的值。 */
		if cfg.MQTTPassword == "" {                                                          /* 判断条件并选择处理分支。 */
			manager := auth.New(cfg.JWTSecret) /* 更新 manager 的值。 */
			acl := []auth.ACLRule{             /* 更新 acl 的值。 */
				{Permission: "allow", Action: "subscribe", Topic: "/iot/up/#"},               /* 执行当前语句并推进处理流程。 */
				{Permission: "allow", Action: "subscribe", Topic: "/external/raw/#"},         /* 执行当前语句并推进处理流程。 */
				{Permission: "allow", Action: "subscribe", Topic: "/jetlinks/raw/#"},         /* 执行当前语句并推进处理流程。 */
				{Permission: "allow", Action: "subscribe", Topic: "/external/video/alarm/#"}, /* 执行当前语句并推进处理流程。 */
				{Permission: "allow", Action: "subscribe", Topic: "/iot/device/state/#"},     /* 执行当前语句并推进处理流程。 */
				{Permission: "allow", Action: "publish", Topic: "/iot/#"},                    /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			credentials = func() (string, string) { /* 更新 credentials 的值。 */
				token, err := manager.IssueWithACL("iot-platform", "system", "service", nil, acl, time.Hour) /* 检查错误并决定后续处理。 */
				if err != nil {                                                                              /* 判断条件并选择处理分支。 */
					log.Error("issue mqtt service token", "error", err) /* 执行当前语句并推进处理流程。 */
					return "iot-platform", ""                           /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
				return "iot-platform", token /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		mqttConnection, err := mqttadapter.NewDurableWithCredentials(cfg.MQTTBroker, filepath.Join(cfg.DataDir, "mqtt-inbox", cfg.ProcessRole), credentials) /* 更新 err 的值。 */
		fatal(log, "connect mqtt", err)                                                                                                                      /* 执行当前语句并推进处理流程。 */
		mqttClient = mqttConnection                                                                                                                          /* 更新 mqttClient 的值。 */
		go func() {                                                                                                                                          /* 执行当前语句并推进处理流程。 */
			ticker := time.NewTicker(5 * time.Second) /* 更新 ticker 的值。 */
			defer ticker.Stop()                       /* 安排函数结束时执行清理。 */
			for {                                     /* 循环处理当前数据。 */
				select { /* 根据条件选择处理路径。 */
				case <-ctx.Done(): /* 处理当前分支。 */
					return /* 返回当前处理结果。 */
				case <-ticker.C: /* 处理当前分支。 */
					pending, rejected, corrupt := mqttConnection.InboxCounts() /* 更新 corrupt 的值。 */
					registry.Set("mqtt_inbox_pending", float64(pending))       /* 执行当前语句并推进处理流程。 */
					registry.Set("mqtt_inbox_rejected", float64(rejected))     /* 执行当前语句并推进处理流程。 */
					registry.Set("mqtt_inbox_corrupt", float64(corrupt))       /* 执行当前语句并推进处理流程。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
		}() /* 结束当前表达式或代码块。 */
		if cfg.AccessCoordination { /* 判断条件并选择处理分支。 */
			fatal(log, "configure shared MQTT ingestion", mqttClient.ConfigureSharedSubscriptions("iot-access")) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		realtime = mqttClient         /* 更新 realtime 的值。 */
		if cfg.ProcessRole != "api" { /* 判断条件并选择处理分支。 */
			registry.Set("mqtt_subscription_count", 4) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		log.Info("realtime enabled", "adapter", "mqtt") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	parsers := parser.NewPlatformRegistry(cfg.DataDir)                                           /* 更新 parsers 的值。 */
	engine := core.New(httpapi.ScopedRepository(repo), archivePort, bus, realtime, parsers, log) /* 更新 engine 的值。 */
	var legacyRaw ports.RawMessageReader                                                         /* 声明 legacyRaw。 */
	if reader, ok := archivePort.(ports.RawMessageReader); ok {                                  /* 判断条件并选择处理分支。 */
		legacyRaw = reader /* 更新 legacyRaw 的值。 */
	} /* 结束当前表达式或代码块。 */
	engine.RawStore = rawstore.New(rawstore.Config{ /* 更新 engine.RawStore 的值。 */
		PostgreSQL:               postgresRaw,                     /* 执行当前语句并推进处理流程。 */
		ClickHouse:               clickHouseRaw,                   /* 执行当前语句并推进处理流程。 */
		Resolver:                 repo,                            /* 执行当前语句并推进处理流程。 */
		Legacy:                   legacyRaw,                       /* 执行当前语句并推进处理流程。 */
		HighFrequencyIntervalSec: cfg.RawHighFrequencyIntervalSec, /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */
	engine.VideoMediaAllowedHosts = cfg.VideoMediaHosts /* 更新 engine.VideoMediaAllowedHosts 的值。 */
	engine.RequireVideoCameraMapping = !cfg.DevMode     /* 更新 engine.RequireVideoCameraMapping 的值。 */
	engine.Metrics = registry                           /* 更新 engine.Metrics 的值。 */
	var runtimeAI *aiadapter.RuntimeProvider            /* 声明 runtimeAI。 */
	var harness *aiadapter.HarnessClient                /* 声明 harness。 */
	if cfg.ProcessRole != "gateway" {                   /* 判断条件并选择处理分支。 */
		aiPlugins := aiadapter.NewProviderRegistry()               /* 更新 aiPlugins 的值。 */
		engine.AIPlugins = aiPlugins                               /* 更新 engine.AIPlugins 的值。 */
		providerID := cfg.AIProvider                               /* 更新 providerID 的值。 */
		if providerID == "" && os.Getenv("IOT_OLLAMA_URL") != "" { /* 判断条件并选择处理分支。 */
			providerID = "ollama" /* 更新 providerID 的值。 */
		} /* 结束当前表达式或代码块。 */
		providerConfig := ports.AIPluginConfig{Provider: providerID, BaseURL: cfg.AIBaseURL, Model: cfg.AIModel, APIKey: cfg.AIAPIKey} /* 更新 providerConfig 的值。 */
		if providerID == "ollama" {                                                                                                    /* 判断条件并选择处理分支。 */
			if providerConfig.BaseURL == "" { /* 判断条件并选择处理分支。 */
				providerConfig.BaseURL = cfg.OllamaURL /* 更新 providerConfig.BaseURL 的值。 */
			} /* 结束当前表达式或代码块。 */
			if providerConfig.Model == "" { /* 判断条件并选择处理分支。 */
				providerConfig.Model = cfg.OllamaModel /* 更新 providerConfig.Model 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if aiProviderStore != nil { /* 判断条件并选择处理分支。 */
			if persisted, found, err := aiProviderStore.LoadAIProviderConfig(ctx); err != nil { /* 判断条件并选择处理分支。 */
				log.Warn("load persisted AI provider config", "error", err) /* 执行当前语句并推进处理流程。 */
			} else if found { /* 结束当前表达式或代码块。 */
				providerConfig = persisted                                                                                     /* 更新 providerConfig 的值。 */
				log.Info("restored persisted AI provider", "provider", providerConfig.Provider, "model", providerConfig.Model) /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		// Older installations may have an activity row without the newer baseUrl
		// field. Fill only missing defaults so a persisted selection remains usable.
		if providerConfig.Provider == "ollama" { /* 判断条件并选择处理分支。 */
			if providerConfig.BaseURL == "" { /* 判断条件并选择处理分支。 */
				providerConfig.BaseURL = cfg.OllamaURL /* 更新 providerConfig.BaseURL 的值。 */
			} /* 结束当前表达式或代码块。 */
			if providerConfig.Model == "" { /* 判断条件并选择处理分支。 */
				providerConfig.Model = cfg.OllamaModel /* 更新 providerConfig.Model 的值。 */
			} /* 结束当前表达式或代码块。 */
		} else if providerConfig.Provider == "deepseek" { /* 结束当前表达式或代码块。 */
			if providerConfig.BaseURL == "" { /* 判断条件并选择处理分支。 */
				providerConfig.BaseURL = cfg.AIBaseURL /* 更新 providerConfig.BaseURL 的值。 */
				if providerConfig.BaseURL == "" {      /* 判断条件并选择处理分支。 */
					providerConfig.BaseURL = "https://api.deepseek.com" /* 更新 providerConfig.BaseURL 的值。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
			if providerConfig.Model == "" { /* 判断条件并选择处理分支。 */
				for _, item := range aiPlugins.List() { /* 循环处理当前数据。 */
					if item.ID == "deepseek" { /* 判断条件并选择处理分支。 */
						providerConfig.Model = item.DefaultModel /* 更新 providerConfig.Model 的值。 */
						break                                    /* 执行当前语句并推进处理流程。 */
					} /* 结束当前表达式或代码块。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
			if providerConfig.APIKey == "" { /* 判断条件并选择处理分支。 */
				providerConfig.APIKey = cfg.AIAPIKey /* 更新 providerConfig.APIKey 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		var providerErr error                                                            /* 声明 providerErr。 */
		runtimeAI, providerErr = aiadapter.NewRuntimeProvider(aiPlugins, providerConfig) /* 更新 providerErr 的值。 */
		fatal(log, "initialize AI provider plugin", providerErr)                         /* 执行当前语句并推进处理流程。 */
		einoAI, einoErr := aiadapter.NewEino(ctx, runtimeAI)                             /* 更新 einoErr 的值。 */
		fatal(log, "initialize Eino AI workflows", einoErr)                              /* 执行当前语句并推进处理流程。 */
		engine.AI = einoAI                                                               /* 更新 engine.AI 的值。 */
		if cfg.ProcessRole != "gateway" && cfg.AIHarnessURL != "" {                      /* 判断条件并选择处理分支。 */
			harnessModel := cfg.AIHarnessModel /* 更新 harnessModel 的值。 */
			if providerConfig.Model != "" {    /* 判断条件并选择处理分支。 */
				harnessModel = providerConfig.Model /* 更新 harnessModel 的值。 */
			} /* 结束当前表达式或代码块。 */
			var harnessErr error                                                                                                                      /* 声明 harnessErr。 */
			harness, harnessErr = aiadapter.NewHarness(cfg.AIHarnessURL, cfg.AIHarnessToken, cfg.AIHarnessMCPURL, harnessModel, cfg.AIHarnessTimeout) /* 更新 harnessErr 的值。 */
			fatal(log, "initialize AI workflow harness", harnessErr)                                                                                  /* 执行当前语句并推进处理流程。 */
			configureCtx, configureCancel := context.WithTimeout(ctx, 20*time.Second)                                                                 /* 更新 configureCancel 的值。 */
			harnessErr = harness.ConfigureProvider(configureCtx, providerConfig)                                                                      /* 更新 harnessErr 的值。 */
			configureCancel()                                                                                                                         /* 执行当前语句并推进处理流程。 */
			if harnessErr != nil {                                                                                                                    /* 判断条件并选择处理分支。 */
				// Compose starts the Harness sidecar after the API so it can call the
				// platform MCP endpoint. Do not make API startup depend on that
				// ordering; retry in the background until the sidecar is ready.
				log.Warn("AI workflow provider synchronization deferred", "error", harnessErr) /* 执行当前语句并推进处理流程。 */
				go retryHarnessProvider(ctx, runtimeAI, harness, log)                          /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			engine.AIWorkflows = harness /* 更新 engine.AIWorkflows 的值。 */
			// Business AI runs (alarm analysis, inspection, reports, protocol
			// assistant, rule drafts) sign their MCP credentials with the API secret.
			engine.HarnessTokens = auth.New(cfg.JWTSecret)
			log.Info("AI workflow harness enabled", "url", cfg.AIHarnessURL, "model", providerConfig.Model) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */

	} /* 结束当前表达式或代码块。 */
	if cfg.ProcessRole != "gateway" && cfg.WeaviateURL != "" { /* 判断条件并选择处理分支。 */
		engine.KB = knowledge.NewWeaviate(cfg.WeaviateURL) /* 更新 engine.KB 的值。 */
	} else { /* 结束当前表达式或代码块。 */
		engine.KB = knowledge.NewLocal() /* 更新 engine.KB 的值。 */
	} /* 结束当前表达式或代码块。 */
	if cfg.ProcessRole != "gateway" { /* 判断条件并选择处理分支。 */
		fatal(log, "start engine", engine.Start(ctx)) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	var coordinator *protocolruntime.Coordinator            /* 声明 coordinator。 */
	if cfg.AccessCoordination && cfg.ProcessRole != "api" { /* 判断条件并选择处理分支。 */
		coordinator = protocolruntime.NewCoordinator(repo, hostname()+"-"+strconv.Itoa(os.Getpid())+"-"+uuid.NewString(), cfg.AccessNodeURL) /* 更新 coordinator 的值。 */
		go coordinator.Run(ctx)                                                                                                              /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	protocolRuntime := protocolruntime.New(repo, func(c context.Context, raw model.RawMessage) error { /* 更新 protocolRuntime 的值。 */
		_, _, err := engine.IngestRaw(c, raw) /* 更新 err 的值。 */
		return err                            /* 返回当前处理结果。 */
	}, log, cfg.ModbusAllowedCIDRs...) /* 结束当前表达式或代码块。 */
	protocolRuntime.SetCoordinator(coordinator) /* 执行当前语句并推进处理流程。 */
	if cfg.ProcessRole != "api" {               /* 判断条件并选择处理分支。 */
		protocolRuntime.Start(ctx) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	protocolListeners := protocolruntime.NewListeners(repo, cfg.DataDir, func(c context.Context, raw model.RawMessage) error { /* 更新 protocolListeners 的值。 */
		_, _, err := engine.IngestRaw(c, raw) /* 更新 err 的值。 */
		return err                            /* 返回当前处理结果。 */
	}, log) /* 结束当前表达式或代码块。 */
	protocolListeners.SetAllowedCIDRs(cfg.ModbusAllowedCIDRs)        /* 执行当前语句并推进处理流程。 */
	protocolListeners.SetConnectionReporter(engine.ReportConnection) /* 执行当前语句并推进处理流程。 */
	protocolListeners.SetCoordinator(coordinator)                    /* 执行当前语句并推进处理流程。 */
	if cfg.ProcessRole != "api" {                                    /* 判断条件并选择处理分支。 */
		protocolListeners.Start(ctx) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if cfg.ProcessRole != "api" { /* 判断条件并选择处理分支。 */
		log.Info("active protocol runtime enabled", "transports", []string{"TCP", "UDP", "MODBUS_TCP (legacy)"}) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if cfg.ProcessRole != "api" && mqttClient != nil { /* 判断条件并选择处理分支。 */
		standardIngress := onboarding.New(repo, parsers, cfg.DataDir, cfg.ModbusAllowedCIDRs)                                                                    /* 更新 standardIngress 的值。 */
		fatal(log, "subscribe standard mqtt", mqttClient.SubscribeStandard(func(c context.Context, tenant, product, device, kind string, payload []byte) error { /* 执行当前语句并推进处理流程。 */
			raw, err := standardIngress.PrepareStandard(c, tenant, product, device, kind, "MQTT", payload) /* 更新 err 的值。 */
			if err != nil {                                                                                /* 判断条件并选择处理分支。 */
				if errors.Is(err, onboarding.ErrAuth) || errors.Is(err, model.ErrInvalidIngress) { /* 判断条件并选择处理分支。 */
					return mqttadapter.Reject(err) /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			raw.ReceivedAt = mqttadapter.ReceivedAt(c) /* 更新 raw.ReceivedAt 的值。 */
			_, _, err = engine.IngestRaw(c, raw)       /* 更新 err 的值。 */
			return err                                 /* 返回当前处理结果。 */
		})) /* 结束当前表达式或代码块。 */
		fatal(log, "subscribe raw mqtt", mqttClient.SubscribeRaw(func(c context.Context, v model.RawMessage) error { _, _, err := engine.IngestRaw(c, v); return err })) /* 执行当前语句并推进处理流程。 */
		fatal(log, "subscribe device state mqtt", mqttClient.SubscribeDeviceState(engine.UpdateDeviceState))                                                             /* 执行当前语句并推进处理流程。 */
		fatal(log, "subscribe video mqtt", mqttClient.SubscribeVideo(func(c context.Context, v model.VideoAlarmEvent) error {                                            /* 执行当前语句并推进处理流程。 */
			_, _, err := engine.IngestVideo(c, v) /* 更新 err 的值。 */
			return err                            /* 返回当前处理结果。 */
		})) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	api := httpapi.New(cfg, engine, registry, log)                             /* 更新 api 的值。 */
	var publishCommand func(context.Context, string, []byte, byte, bool) error /* 声明 publishCommand。 */
	if mqttClient != nil {                                                     /* 判断条件并选择处理分支。 */
		publishCommand = mqttClient.Publish /* 更新 publishCommand 的值。 */
	} /* 结束当前表达式或代码块。 */
	var revokeUsername func(context.Context, string) error                       /* 声明 revokeUsername。 */
	if cfg.EMQXAPIURL != "" && cfg.EMQXAPIKey != "" && cfg.EMQXAPISecret != "" { /* 判断条件并选择处理分支。 */
		admin := &mqttadapter.Admin{URL: cfg.EMQXAPIURL, Key: cfg.EMQXAPIKey, Secret: cfg.EMQXAPISecret} /* 更新 admin 的值。 */
		revokeUsername = admin.RevokeUsername                                                            /* 更新 revokeUsername 的值。 */
	} /* 结束当前表达式或代码块。 */
	api.SetDeviceOperations(publishCommand, revokeUsername) /* 执行当前语句并推进处理流程。 */
	if cfg.ProcessRole != "gateway" {                       /* 判断条件并选择处理分支。 */
		go api.RunCredentialRevocations(ctx) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if mqttClient != nil { /* 判断条件并选择处理分支。 */
		api.SetMQTTHealth(mqttClient.Probe) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	api.SetAIProviderRuntime(runtimeAI)     /* 执行当前语句并推进处理流程。 */
	api.SetAIProviderStore(aiProviderStore) /* 执行当前语句并推进处理流程。 */
	if harness != nil {                     /* 判断条件并选择处理分支。 */
		api.SetAIWorkflowProvider(harness) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	api.SetProtocolListeners(protocolListeners) /* 执行当前语句并推进处理流程。 */
	if cfg.ProcessRole != "gateway" {
		api.SetOpsCenter(newOpsCenter(cfg, opsPrefs, log))
	}
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: api.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 15 * time.Minute, IdleTimeout: 2 * time.Minute} /* 更新 server 的值。 */
	if cfg.ProcessRole != "gateway" {                                                                                                                                                                    /* 判断条件并选择处理分支。 */
		go func() { /* 执行当前语句并推进处理流程。 */
			ticker := time.NewTicker(cfg.OfflineScan) /* 更新 ticker 的值。 */
			defer ticker.Stop()                       /* 安排函数结束时执行清理。 */
			for {                                     /* 循环处理当前数据。 */
				select { /* 根据条件选择处理路径。 */
				case <-ctx.Done(): /* 处理当前分支。 */
					return /* 返回当前处理结果。 */
				case <-ticker.C: /* 处理当前分支。 */
					if err := engine.ScanOffline(ctx); err != nil { /* 判断条件并选择处理分支。 */
						log.Error("offline scan failed", "error", err) /* 执行当前语句并推进处理流程。 */
					} /* 结束当前表达式或代码块。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
		}() /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	go func() { /* 执行当前语句并推进处理流程。 */
		log.Info("iot platform started", "addr", cfg.HTTPAddr, "devMode", cfg.DevMode)           /* 执行当前语句并推进处理流程。 */
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) { /* 判断条件并选择处理分支。 */
			log.Error("http server failed", "error", err) /* 执行当前语句并推进处理流程。 */
			cancel()                                      /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	}() /* 结束当前表达式或代码块。 */
	<-ctx.Done()                                                                /* 执行当前语句并推进处理流程。 */
	shutdown, stop := context.WithTimeout(context.Background(), 15*time.Second) /* 更新 stop 的值。 */
	defer stop()                                                                /* 安排函数结束时执行清理。 */
	_ = server.Shutdown(shutdown)                                               /* 更新 _ 的值。 */
	_ = realtime.Close()                                                        /* 更新 _ 的值。 */
	_ = bus.Close()                                                             /* 更新 _ 的值。 */
	_ = repo.Close()                                                            /* 更新 _ 的值。 */
	log.Info("iot platform stopped")                                            /* 执行当前语句并推进处理流程。 */
	if logPush != nil {
		flushCtx, flushCancel := context.WithTimeout(context.Background(), 5*time.Second)
		logPush.Close(flushCtx)
		flushCancel()
	}
} /* 结束当前表达式或代码块。 */
func fatal(log *slog.Logger, msg string, err error) { /* 定义 fatal 函数。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		log.Error(msg, "error", err) /* 执行当前语句并推进处理流程。 */
		os.Exit(1)                   /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func retryHarnessProvider(ctx context.Context, runtimeAI ports.AIProviderRuntime, harness *aiadapter.HarnessClient, log *slog.Logger) { /* 定义 retryHarnessProvider 函数。 */
	ticker := time.NewTicker(5 * time.Second) /* 更新 ticker 的值。 */
	defer ticker.Stop()                       /* 安排函数结束时执行清理。 */
	for {                                     /* 循环处理当前数据。 */
		select { /* 根据条件选择处理路径。 */
		case <-ctx.Done(): /* 处理当前分支。 */
			return /* 返回当前处理结果。 */
		case <-ticker.C: /* 处理当前分支。 */
			target := runtimeAI.CurrentConfig()                              /* 更新 target 的值。 */
			configureCtx, cancel := context.WithTimeout(ctx, 20*time.Second) /* 更新 cancel 的值。 */
			err := harness.ConfigureProvider(configureCtx, target)           /* 更新 err 的值。 */
			cancel()                                                         /* 执行当前语句并推进处理流程。 */
			if err == nil {                                                  /* 判断条件并选择处理分支。 */
				// A UI update may have arrived while the sidecar request was in
				// flight. Reconcile the newest selection before returning so an
				// older retry can never leave the Harness on stale settings.
				if runtimeAI.CurrentConfig() != target { /* 判断条件并选择处理分支。 */
					continue /* 执行当前语句并推进处理流程。 */
				} /* 结束当前表达式或代码块。 */
				log.Info("AI workflow provider synchronized", "provider", target.Provider, "model", target.Model) /* 执行当前语句并推进处理流程。 */
				return                                                                                            /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func hostname() string { /* 定义 hostname 函数。 */
	v, err := os.Hostname() /* 更新 err 的值。 */
	if err != nil {         /* 判断条件并选择处理分支。 */
		return "unknown" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return v /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func positiveOr(value int64, fallback int) int {
	if value > 0 {
		return int(value)
	}
	return fallback
}
