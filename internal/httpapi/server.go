package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"archive/zip"    /* 执行当前语句并推进处理流程。 */
	"bytes"          /* 执行当前语句并推进处理流程。 */
	"context"        /* 执行当前语句并推进处理流程。 */
	"crypto/hmac"    /* 执行当前语句并推进处理流程。 */
	"crypto/rand"    /* 执行当前语句并推进处理流程。 */
	"crypto/sha256"  /* 执行当前语句并推进处理流程。 */
	"encoding/hex"   /* 执行当前语句并推进处理流程。 */
	"encoding/json"  /* 执行当前语句并推进处理流程。 */
	"errors"         /* 执行当前语句并推进处理流程。 */
	"fmt"            /* 执行当前语句并推进处理流程。 */
	"io"             /* 执行当前语句并推进处理流程。 */
	"log/slog"       /* 执行当前语句并推进处理流程。 */
	"mime/multipart" /* 执行当前语句并推进处理流程。 */
	"net/http"       /* 执行当前语句并推进处理流程。 */
	"net/url"        /* 执行当前语句并推进处理流程。 */
	"strconv"        /* 执行当前语句并推进处理流程。 */
	"strings"        /* 执行当前语句并推进处理流程。 */
	"sync"           /* 执行当前语句并推进处理流程。 */
	"time"           /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/auth"       /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"     /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"       /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/mcpserver"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics"    /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"      /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/onboarding" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"     /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports"      /* 执行当前语句并推进处理流程。 */

	"github.com/gin-gonic/gin" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type ctxKey string /* 定义 ctxKey 类型。 */

const claimsKey ctxKey = "claims" /* 声明 claimsKey。 */

type Server struct { /* 定义 Server 类型。 */
	cfg                        config.Config                   /* 执行当前语句并推进处理流程。 */
	engine                     *core.Engine                    /* 执行当前语句并推进处理流程。 */
	auth                       *auth.Manager                   /* 执行当前语句并推进处理流程。 */
	metrics                    *metrics.Registry               /* 执行当前语句并推进处理流程。 */
	log                        *slog.Logger                    /* 执行当前语句并推进处理流程。 */
	router                     *gin.Engine                     /* 执行当前语句并推进处理流程。 */
	aiProviderRuntime          ports.AIProviderRuntime         /* 执行当前语句并推进处理流程。 */
	aiProviderStore            ports.AIProviderConfigStore     /* 执行当前语句并推进处理流程。 */
	aiWorkflowProvider         ports.AIWorkflowProviderRuntime /* 执行当前语句并推进处理流程。 */
	aiProviderUpdateMu         sync.Mutex                      /* 执行当前语句并推进处理流程。 */
	healthInspectionMu         sync.RWMutex                    // 仅保护本进程的耗时估算；任务状态保存在仓储中。
	healthInspectionEstimateMs int64
	aiAnalysisMu               sync.RWMutex              /* 执行当前语句并推进处理流程。 */
	aiAnalysisJobs             map[string]*aiAnalysisJob /* 执行当前语句并推进处理流程。 */
	aiAnalysisEstimateMs       int64                     /* 执行当前语句并推进处理流程。 */
	protocolListeners          protocolCommander         /* 执行当前语句并推进处理流程。 */
	onboarding                 *onboarding.Service       /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

const healthInspectionCacheTTL = 10 * time.Minute /* 声明 healthInspectionCacheTTL。 */

func New(cfg config.Config, engine *core.Engine, m *metrics.Registry, log *slog.Logger) *Server { /* 定义 New 函数。 */
	if _, ok := engine.Repo.(*deviceScopeRepository); !ok { /* 判断条件并选择处理分支。 */
		engine.Repo = &deviceScopeRepository{Repository: engine.Repo} /* 更新 engine.Repo 的值。 */
	} /* 结束当前表达式或代码块。 */
	gin.SetMode(gin.ReleaseMode)         /* 执行当前语句并推进处理流程。 */
	router := gin.New()                  /* 更新 router 的值。 */
	router.HandleMethodNotAllowed = true /* 更新 router.HandleMethodNotAllowed 的值。 */
	router.RedirectTrailingSlash = false /* 更新 router.RedirectTrailingSlash 的值。 */
	s := &Server{                        /* 更新 s 的值。 */
		cfg:                        cfg,                                                                              /* 执行当前语句并推进处理流程。 */
		engine:                     engine,                                                                           /* 执行当前语句并推进处理流程。 */
		onboarding:                 onboarding.New(engine.Repo, engine.Parsers, cfg.DataDir, cfg.ModbusAllowedCIDRs), /* 执行当前语句并推进处理流程。 */
		auth:                       auth.New(cfg.JWTSecret),                                                          /* 执行当前语句并推进处理流程。 */
		metrics:                    m,                                                                                /* 执行当前语句并推进处理流程。 */
		log:                        log,                                                                              /* 执行当前语句并推进处理流程。 */
		router:                     router,                                                                           /* 执行当前语句并推进处理流程。 */
		healthInspectionEstimateMs: healthInspectionEstimateDefault.Milliseconds(),                                   /* 执行当前语句并推进处理流程。 */
		aiAnalysisJobs:             make(map[string]*aiAnalysisJob),                                                  /* 执行当前语句并推进处理流程。 */
		aiAnalysisEstimateMs:       45000,                                                                            /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	router.Use(s.cors(), s.security(), s.accessLog(), s.recovery()) /* 执行当前语句并推进处理流程。 */
	s.routes()                                                      /* 执行当前语句并推进处理流程。 */
	return s                                                        /* 返回当前处理结果。 */
}                                       /* 结束当前表达式或代码块。 */
func (s *Server) Handler() http.Handler { return s.roleHandler() } /* 定义 Handler 函数。 */

func (s *Server) SetAIProviderRuntime(runtime ports.AIProviderRuntime) { /* 定义 SetAIProviderRuntime 函数。 */
	s.aiProviderRuntime = runtime /* 更新 s.aiProviderRuntime 的值。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) SetAIProviderStore(store ports.AIProviderConfigStore) { /* 定义 SetAIProviderStore 函数。 */
	s.aiProviderStore = store /* 更新 s.aiProviderStore 的值。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) SetAIWorkflowProvider(runtime ports.AIWorkflowProviderRuntime) { /* 定义 SetAIWorkflowProvider 函数。 */
	s.aiWorkflowProvider = runtime /* 更新 s.aiWorkflowProvider 的值。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) routes() { /* 定义 routes 函数。 */
	s.accessRoutes() /* 执行当前语句并推进处理流程。 */
	s.deletionRoutes()
	s.router.GET("/api/v1/connectors/types", s.authorize("viewer"), s.endpoint(s.connectorTypes))                       /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/connectors", s.authorize("viewer"), s.endpoint(s.connectorStatus))                            /* 执行当前语句并推进处理流程。 */
	s.deviceOperationsRoutes()                                                                                          /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/device-registry/:id/connection", s.authorize("viewer"), s.endpoint(s.deviceConnection, "id")) /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/onboarding/preflight", s.authorize("operator"), s.endpoint(s.onboardingPreflight))
	s.router.POST("/api/v1/onboarding", s.authorize("operator"), s.endpoint(s.onboardingEnroll))
	s.router.POST("/api/v1/device-ingest/standard/:tenantId/:productId/:deviceId/:kind", s.endpoint(s.standardDeviceIngest, "tenantId", "productId", "deviceId", "kind")) /* 执行当前语句并推进处理流程。 */
	s.router.DELETE("/api/v1/device-registry/:id/credentials", s.authorize("admin"), s.endpoint(s.disableDeviceCredential, "id"))                                         /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/auth/login", s.endpoint(s.login))                                                                                                              /* 执行当前语句并推进处理流程。 */
	s.router.GET("/health/live", s.endpoint(func(w http.ResponseWriter, r *http.Request) { write(w, 200, map[string]string{"status": "ok"}) }))                           /* 执行当前语句并推进处理流程。 */
	s.router.GET("/health/ready", s.endpoint(s.ready))                                                                                                                    /* 执行当前语句并推进处理流程。 */
	s.router.GET("/metrics", s.endpoint(func(w http.ResponseWriter, r *http.Request) {                                                                                    /* 执行当前语句并推进处理流程。 */
		w.Header().Set("Content-Type", "text/plain; version=0.0.4") /* 执行当前语句并推进处理流程。 */
		_, _ = io.WriteString(w, s.metrics.Prometheus())            /* 更新 _ 的值。 */
	})) /* 结束当前表达式或代码块。 */
	s.router.POST("/api/v1/integrations/video/alarm", s.endpoint(s.videoWebhook))                                                                                  /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/integrations/video/cameras", s.authorize("viewer"), s.endpoint(s.videoCameras))                                                          /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/integrations/video/relations", s.authorize("viewer"), s.endpoint(s.videoRelations))                                                      /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/integrations/video/cameras", s.authorize("operator"), s.endpoint(s.saveVideoCamera))                                                    /* 执行当前语句并推进处理流程。 */
	s.router.PUT("/api/v1/integrations/video/cameras/:id", s.authorize("operator"), s.endpoint(s.saveVideoCamera, "id"))                                           /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/device-ingest/:deviceId", s.endpoint(s.deviceIngest, "deviceId"))                                                                       /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/products", s.authorize("viewer"), s.endpoint(s.products))                                                                                /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/products", s.authorize("operator"), s.endpoint(s.saveProduct))                                                                          /* 执行当前语句并推进处理流程。 */
	s.router.PUT("/api/v1/products/:id", s.authorize("operator"), s.endpoint(s.saveProduct, "id"))                                                                 /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/protocol-packages", s.authorize("viewer"), s.endpoint(s.protocolPackages))                                                               /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/protocol-packages", s.authorize("operator"), s.endpoint(s.saveProtocolPackage))                                                         /* 执行当前语句并推进处理流程。 */
	s.router.PUT("/api/v1/protocol-packages/:id", s.authorize("operator"), s.endpoint(s.saveProtocolPackage, "id"))                                                /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/protocol-packages/:id/test", s.authorize("operator"), s.endpoint(s.testProtocolPackage, "id"))                                          /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v2/protocols", s.authorize("viewer"), s.endpoint(s.protocolDefinitionsV2))                                                                  /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v2/protocol-source-template", s.authorize("viewer"), s.endpoint(s.protocolSourceTemplate))                                                  /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v2/protocols/:id/source-releases", s.authorize("operator"), s.endpoint(s.uploadProtocolSource, "id"))                                      /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v2/protocols", s.authorize("operator"), s.endpoint(s.saveProtocolDefinitionV2))                                                            /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v2/protocols/:id/releases/:version/preview", s.authorize("operator"), s.endpoint(s.previewGeneratedRelease, "id", "version"))              /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v2/protocols/:id/releases", s.authorize("viewer"), s.endpoint(s.protocolReleasesV2, "id"))                                                  /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v2/protocols/:id/releases/:version/source", s.authorize("operator"), s.endpoint(s.downloadProtocolSourceV2, "id", "version"))               /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v2/protocols/:id/releases/:version/package", s.authorize("operator"), s.endpoint(s.downloadProtocolPackageV2, "id", "version"))             /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v2/protocols/:id/releases", s.authorize("operator"), s.endpoint(s.createProtocolReleaseV2, "id"))                                          /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v2/protocols/:id/package-releases", s.authorize("operator"), s.endpoint(s.uploadProtocolPackageV2, "id"))                                  /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v2/protocols/:id/releases/:version/publish", s.authorize("operator"), s.endpoint(s.publishProtocolReleaseV2, "id", "version"))             /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v2/products/:id/protocol-binding", s.authorize("viewer"), s.endpoint(s.getProductProtocolBinding, "id"))                                    /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/device-registry/:id/children", s.authorize("viewer"), s.endpoint(s.deviceChildren, "id"))                                                /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v2/products/:id/protocol-binding", s.authorize("operator"), s.endpoint(s.bindProductProtocolV2, "id"))                                     /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v2/products/:id/protocol-binding/rollback", s.authorize("operator"), s.endpoint(s.rollbackProductProtocolV2, "id"))                        /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v2/modbus-tcp/import", s.authorize("operator"), s.endpoint(s.importModbusTCPV2))                                                           /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v2/device-access-profiles", s.authorize("viewer"), s.endpoint(s.deviceAccessProfilesV2))                                                    /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v2/device-access-profiles", s.authorize("operator"), s.endpoint(s.saveDeviceAccessProfileV2))                                              /* 执行当前语句并推进处理流程。 */
	s.router.PUT("/api/v2/device-access-profiles/:id", s.authorize("operator"), s.endpoint(s.saveDeviceAccessProfileV2, "id"))                                     /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v2/device-access-profiles/:id/test", s.authorize("operator"), s.endpoint(s.testDeviceAccessProfileV2, "id"))                               /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v2/device-access-profiles/:id/devices/:deviceId/commands", s.authorize("operator"), s.endpoint(s.protocolDeviceCommand, "id", "deviceId")) /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/device-registry", s.authorize("viewer"), s.endpoint(s.deviceRegistry))                                                                   /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/device-registry", s.authorize("operator"), s.endpoint(s.saveManagedDevice))                                                             /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/device-registry/:id/children", s.authorize("operator"), s.endpoint(s.registerConfiguredChild, "id"))
	s.router.PUT("/api/v1/device-registry/:id", s.authorize("operator"), s.endpoint(s.saveManagedDevice, "id"))                                          /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/test-devices/provision", s.authorize("operator"), s.endpoint(s.provisionTestDevice))                                          /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/discovered-devices/:id/register", s.authorize("operator"), s.endpoint(s.registerDiscoveredDevice, "id"))                      /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/device-registry/:id/credentials", s.authorize("admin"), s.endpoint(s.rotateDeviceCredential, "id"))                           /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/device-registry/:id/debug", s.authorize("operator"), s.endpoint(s.debugDeviceIngest, "id"))                                   /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/raw-messages", s.authorize("operator"), s.endpoint(s.ingestRaw))                                                              /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/raw-messages", s.authorize("viewer"), s.endpoint(s.listRaw))                                                                   /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/raw-messages/download", s.authorize("viewer"), s.endpoint(s.downloadRawBatch))                                                /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/raw-messages/:id", s.authorize("viewer"), s.endpoint(s.rawDetail, "id"))                                                       /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/raw-messages/:id/download", s.authorize("viewer"), s.endpoint(s.downloadRaw, "id"))                                            /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/raw-messages/replay", s.authorize("admin"), s.endpoint(s.startReplay))                                                        /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/replays/:id", s.authorize("viewer"), s.endpoint(s.getReplay, "id"))                                                            /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/dashboard", s.authorize("viewer"), s.endpoint(s.dashboard))                                                                    /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/devices", s.authorize("viewer"), s.endpoint(s.devices))                                                                        /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/devices/:deviceId/latest", s.authorize("viewer"), s.endpoint(s.deviceLatest, "deviceId"))                                      /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/devices/:deviceId/properties/history", s.authorize("viewer"), s.endpoint(s.history, "deviceId"))                               /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/device-states", s.authorize("operator"), s.endpoint(s.stateEvent))                                                            /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/rules", s.authorize("viewer"), s.endpoint(s.rules))                                                                            /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/rules", s.authorize("operator"), s.endpoint(s.saveRule))                                                                      /* 执行当前语句并推进处理流程。 */
	s.router.PUT("/api/v1/rules/:id", s.authorize("operator"), s.endpoint(s.saveRule, "id"))                                                             /* 执行当前语句并推进处理流程。 */
	s.router.DELETE("/api/v1/rules/:id", s.authorize("operator"), s.endpoint(s.deleteRule, "id"))                                                        /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/alarms", s.authorize("viewer"), s.endpoint(s.alarms))                                                                          /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/alarms/:id", s.authorize("viewer"), s.endpoint(s.alarm, "id"))                                                                 /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/alarms/:id/actions", s.authorize("operator"), s.endpoint(s.alarmAction, "id"))                                                /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/backups/:id/files/:filename", s.authorize("admin"), s.endpoint(s.downloadBackupFile, "id", "filename"))                        /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/backups/:id/files", s.authorize("viewer"), s.endpoint(s.backupFiles, "id"))                                                    /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/backups/:id/restore-drill", s.authorize("admin"), s.endpoint(s.restoreBackup, "id"))                                          /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/backups/:id", s.authorize("viewer"), s.endpoint(s.getBackup, "id"))                                                            /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/backups", s.authorize("viewer"), s.endpoint(s.listBackups))                                                                    /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/backups", s.authorize("admin"), s.endpoint(s.runBackup))                                                                      /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/ai/alarm-analysis/:alarmId", s.authorize("viewer"), s.endpoint(s.aiAnalysis, "alarmId"))                                       /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/ai/alarm-analysis/:alarmId/run", s.authorize("operator"), s.endpoint(s.runAIAlarmAnalysis, "alarmId"))                        /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/ai/alarm-analysis/:alarmId/progress", s.authorize("viewer"), s.endpoint(s.aiAlarmAnalysisProgress, "alarmId"))                 /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/ai/alarm-analysis/:alarmId/progress/:jobId", s.authorize("viewer"), s.endpoint(s.aiAlarmAnalysisProgress, "alarmId", "jobId")) /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/ai/health-inspection", s.authorize("viewer"), s.endpoint(s.healthInspection))                                                 /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/ai/health-inspection/run", s.authorize("viewer"), s.endpoint(s.runHealthInspection))                                          /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/ai/health-inspection/progress", s.authorize("viewer"), s.endpoint(s.healthInspectionProgress))                                 /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/ai/health-inspection/progress/:jobId", s.authorize("viewer"), s.endpoint(s.healthInspectionProgress, "jobId"))                 /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/ai/health-inspection/pdf", s.authorize("viewer"), s.endpoint(s.healthInspectionPDF))                                          /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/ai/protocol-assistant/generate", s.authorize("operator"), s.endpoint(s.generateProtocolAssistant))                            /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/ai/protocol-assistant/preview", s.authorize("operator"), s.endpoint(s.previewProtocolAssistant))                              /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/ai/protocol-assistant/publish", s.authorize("operator"), s.endpoint(s.publishProtocolAssistant))                              /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/ai/providers", s.authorize("viewer"), s.endpoint(s.aiProviders))                                                               /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/ai/providers/config", s.authorize("viewer"), s.endpoint(s.aiProviderConfig))                                                   /* 执行当前语句并推进处理流程。 */
	s.router.PUT("/api/v1/ai/providers/config", s.authorize("admin"), s.endpoint(s.updateAIProviderConfig))                                              /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/ai/providers/test", s.authorize("admin"), s.endpoint(s.testAIProvider))                                                       /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/ai/workflows", s.authorize("viewer"), s.endpoint(s.aiWorkflows))                                                               /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/ai/workflows/admin", s.authorize("admin"), s.endpoint(s.aiWorkflowManifests))                                                  /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/ai/workflows", s.authorize("admin"), s.endpoint(s.saveAIWorkflow))                                                            /* 执行当前语句并推进处理流程。 */
	s.router.PUT("/api/v1/ai/workflows/:id", s.authorize("admin"), s.endpoint(s.updateAIWorkflow, "id"))                                                 /* 执行当前语句并推进处理流程。 */
	s.router.DELETE("/api/v1/ai/workflows/:id", s.authorize("admin"), s.endpoint(s.deleteAIWorkflow, "id"))                                              /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/ai/workflows/:id/knowledge-binding", s.authorize("viewer"), s.endpoint(s.workflowKnowledgeBinding, "id"))                      /* 执行当前语句并推进处理流程。 */
	s.router.PUT("/api/v1/ai/workflows/:id/knowledge-binding", s.authorize("operator"), s.endpoint(s.workflowKnowledgeBinding, "id"))                    /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/ai/chat", s.authorize("viewer"), s.endpoint(s.aiChat))                                                                        /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/ai/chat/stream", s.authorize("viewer"), s.endpoint(s.aiChatStream))                                                           /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/ai/rule-draft", s.authorize("operator"), s.endpoint(s.aiRuleDraft))                                                           /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/ai/reports", s.authorize("viewer"), s.endpoint(s.aiReport))                                                                   /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/knowledge/documents", s.authorize("viewer"), s.endpoint(s.knowledgeDocs))                                                      /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/knowledge/documents/:id", s.authorize("viewer"), s.endpoint(s.knowledgeDocumentDetail, "id"))                                  /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/knowledge/documents", s.authorize("operator"), s.endpoint(s.knowledgeUpload))                                                 /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/mqtt/token", s.authorize("viewer"), s.endpoint(s.mqttToken))                                                                  /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/mqtt/load-token", s.authorize("admin"), s.endpoint(s.mqttLoadToken))                                                          /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/device-mqtt/token", s.endpoint(s.deviceMQTTToken))                                                                            /* 执行当前语句并推进处理流程。 */
	mcpHandler := gin.WrapH(mcpserver.New(s.engine))                                                                                                     /* 更新 mcpHandler 的值。 */
	s.router.GET("/mcp", s.authorize("viewer"), mcpHandler)                                                                                              /* 执行当前语句并推进处理流程。 */
	s.router.POST("/mcp", s.authorize("viewer"), mcpHandler)                                                                                             /* 执行当前语句并推进处理流程。 */
	s.router.DELETE("/mcp", s.authorize("viewer"), mcpHandler)                                                                                           /* 执行当前语句并推进处理流程。 */
	harnessMCPHandler := gin.WrapH(mcpserver.NewHarness(s.engine))                                                                                       /* 更新 harnessMCPHandler 的值。 */
	s.router.POST("/mcp/harness", s.authorizeHarness(), harnessMCPHandler)                                                                               /* 执行当前语句并推进处理流程。 */
	s.router.NoRoute(func(c *gin.Context) { ginProblem(c, http.StatusNotFound, "route not found") })                                                     /* 执行当前语句并推进处理流程。 */
	s.router.NoMethod(func(c *gin.Context) { ginProblem(c, http.StatusMethodNotAllowed, "method not allowed") })                                         /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) login(w http.ResponseWriter, r *http.Request) { /* 定义 login 函数。 */
	var in struct { /* 声明 in。 */
		Username string `json:"username"` /* 执行当前语句并推进处理流程。 */
		Password string `json:"password"` /* 执行当前语句并推进处理流程。 */
		TenantID string `json:"tenantId"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if decode(w, r, &in) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	in.TenantID = strings.TrimSpace(in.TenantID) /* 更新 in.TenantID 的值。 */
	if in.TenantID == "" {                       /* 判断条件并选择处理分支。 */
		in.TenantID = "tenant_001" /* 更新 in.TenantID 的值。 */
	} /* 结束当前表达式或代码块。 */
	if in.Username != s.cfg.AdminUser { /* 判断条件并选择处理分支。 */
		s.loginManaged(w, r, in.Username, in.Password, in.TenantID) /* 执行当前语句并推进处理流程。 */
		return                                                      /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if in.Password != s.cfg.AdminPassword { /* 判断条件并选择处理分支。 */
		problem(w, 401, "invalid credentials") /* 执行当前语句并推进处理流程。 */
		return                                 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !adminTenantAllowed(s.cfg.AdminTenants, in.TenantID) { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusForbidden, "admin tenant is not allowed") /* 执行当前语句并推进处理流程。 */
		return                                                          /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	token, _ := s.auth.Issue(in.Username, in.TenantID, "admin", nil, 8*time.Hour)                                                                   /* 更新 _ 的值。 */
	write(w, 200, map[string]any{"accessToken": token, "expiresIn": 28800, "tenantId": in.TenantID, "role": "admin", "permissions": []string{"*"}}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) ready(w http.ResponseWriter, r *http.Request) { /* 定义 ready 函数。 */
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)                                                                                                                                       /* 更新 cancel 的值。 */
	defer cancel()                                                                                                                                                                                       /* 安排函数结束时执行清理。 */
	checks := map[string]string{}                                                                                                                                                                        /* 更新 checks 的值。 */
	status := 200                                                                                                                                                                                        /* 更新 status 的值。 */
	checksToRun := map[string]func(context.Context) error{"repository": s.engine.Repo.Health, "archive": s.engine.Archive.Health, "eventBus": s.engine.Bus.Health, "realtime": s.engine.Realtime.Health} /* 更新 checksToRun 的值。 */
	if s.engine.KB != nil {                                                                                                                                                                              /* 判断条件并选择处理分支。 */
		checksToRun["knowledge"] = s.engine.KB.Health /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	for name, check := range checksToRun { /* 循环处理当前数据。 */
		if err := check(ctx); err != nil { /* 判断条件并选择处理分支。 */
			checks[name] = err.Error() /* 更新 checks[name] 的值。 */
			status = 503               /* 更新 status 的值。 */
		} else { /* 结束当前表达式或代码块。 */
			checks[name] = "ok" /* 更新 checks[name] 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	write(w, status, map[string]any{"status": map[bool]string{true: "ok", false: "degraded"}[status == 200], "checks": checks}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) products(w http.ResponseWriter, r *http.Request) { /* 定义 products 函数。 */
	pagination := parseListPagination(r)                                                                                         /* 更新 pagination 的值。 */
	items, total, err := s.engine.Repo.ListProductsPage(r.Context(), claims(r).TenantID, pagination.PageSize, pagination.Offset) /* 更新 err 的值。 */
	if err != nil {                                                                                                              /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	writeList(w, 200, items, total, pagination, nil) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) saveProduct(w http.ResponseWriter, r *http.Request) { /* 定义 saveProduct 函数。 */
	var v model.Product          /* 声明 v。 */
	if decode(w, r, &v) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	c := claims(r)                         /* 更新 c 的值。 */
	v.TenantID = c.TenantID                /* 更新 v.TenantID 的值。 */
	if id := r.PathValue("id"); id != "" { /* 判断条件并选择处理分支。 */
		v.ID = id /* 更新 v.ID 的值。 */
	} /* 结束当前表达式或代码块。 */
	if v.ID == "" { /* 判断条件并选择处理分支。 */
		v.ID = "product_" + randomHex(6) /* 更新 v.ID 的值。 */
	} /* 结束当前表达式或代码块。 */
	if v.Name == "" || v.ProtocolPackageID == "" { /* 判断条件并选择处理分支。 */
		problem(w, 422, "name and protocolPackageId are required") /* 执行当前语句并推进处理流程。 */
		return                                                     /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err := onboarding.ValidateThingModel(v.ThingModel); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	pkg, err := s.productProtocol(r.Context(), c.TenantID, v.ProtocolPackageID) /* 更新 err 的值。 */
	if err != nil {                                                             /* 判断条件并选择处理分支。 */
		problem(w, 422, "协议不可用，请选择内置标准上报或已发布的协议版本") /* 执行当前语句并推进处理流程。 */
		return                                      /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.Transport == "" { /* 判断条件并选择处理分支。 */
		v.Transport = pkg.Transport /* 更新 v.Transport 的值。 */
	} /* 结束当前表达式或代码块。 */
	if v.PayloadFormat == "" { /* 判断条件并选择处理分支。 */
		v.PayloadFormat = pkg.PayloadFormat /* 更新 v.PayloadFormat 的值。 */
	} /* 结束当前表达式或代码块。 */
	if v.Status == "" { /* 判断条件并选择处理分支。 */
		v.Status = "ENABLED" /* 更新 v.Status 的值。 */
	} /* 结束当前表达式或代码块。 */
	now := time.Now().UnixMilli()                                                              /* 更新 now 的值。 */
	newProduct := false                                                                        /* 更新 newProduct 的值。 */
	if old, getErr := s.engine.Repo.GetProduct(r.Context(), c.TenantID, v.ID); getErr == nil { /* 判断条件并选择处理分支。 */
		v.CreatedAt = old.CreatedAt /* 更新 v.CreatedAt 的值。 */
	} else if errors.Is(getErr, model.ErrNotFound) { /* 结束当前表达式或代码块。 */
		newProduct = true /* 更新 newProduct 的值。 */
	} else { /* 结束当前表达式或代码块。 */
		problem(w, 500, getErr.Error()) /* 执行当前语句并推进处理流程。 */
		return                          /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.CreatedAt == 0 { /* 判断条件并选择处理分支。 */
		v.CreatedAt = now /* 更新 v.CreatedAt 的值。 */
	} /* 结束当前表达式或代码块。 */
	v.UpdatedAt = now                                                /* 更新 v.UpdatedAt 的值。 */
	if err = s.engine.Repo.SaveProduct(r.Context(), v); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	// Versioned protocols are parsed by the bound release; the binding is their single source.
	_, releaseErr := s.engine.Repo.GetProtocolRelease(r.Context(), c.TenantID, pkg.Protocol, pkg.Version)
	if newProduct && v.ProtocolPackageID != parser.StandardProtocolID+"@1.0.0" && releaseErr == nil {
		if _, err := s.bindProtocolRelease(r, pkg.Protocol, pkg.Version, v.ID); err != nil {
			bindingProblem(w, err)
			return /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		v, err = s.engine.Repo.GetProduct(r.Context(), c.TenantID, v.ID) /* 更新 err 的值。 */
		if err != nil {                                                  /* 判断条件并选择处理分支。 */
			problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
			return                       /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	s.audit(r, "product.save", "product", v.ID, map[string]any{"status": v.Status}) /* 执行当前语句并推进处理流程。 */
	write(w, 201, v)                                                                /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) protocolPackages(w http.ResponseWriter, r *http.Request) { /* 定义 protocolPackages 函数。 */
	pagination := parseListPagination(r)                                                                                                 /* 更新 pagination 的值。 */
	items, total, err := s.engine.Repo.ListProtocolPackagesPage(r.Context(), claims(r).TenantID, pagination.PageSize, pagination.Offset) /* 更新 err 的值。 */
	if err != nil {                                                                                                                      /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	writeList(w, 200, items, total, pagination, map[string]any{"parserTypes": parser.ManagedParserTypes()}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) saveProtocolPackage(w http.ResponseWriter, r *http.Request) { /* 定义 saveProtocolPackage 函数。 */
	var v model.ProtocolPackage  /* 声明 v。 */
	if decode(w, r, &v) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	c := claims(r)                         /* 更新 c 的值。 */
	v.TenantID = c.TenantID                /* 更新 v.TenantID 的值。 */
	if id := r.PathValue("id"); id != "" { /* 判断条件并选择处理分支。 */
		v.ID = id /* 更新 v.ID 的值。 */
	} /* 结束当前表达式或代码块。 */
	if v.ID == "" { /* 判断条件并选择处理分支。 */
		v.ID = "protocol_" + randomHex(6) /* 更新 v.ID 的值。 */
	} /* 结束当前表达式或代码块。 */
	if v.Name == "" || v.ParserType == "" { /* 判断条件并选择处理分支。 */
		problem(w, 422, "name and parserType are required") /* 执行当前语句并推进处理流程。 */
		return                                              /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.ParserType == parser.GoProtocolParserName { /* 判断条件并选择处理分支。 */
		problem(w, 422, "Go 协议请通过源码编译、样例验证和版本发布接口管理") /* 执行当前语句并推进处理流程。 */
		return                                        /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !parser.ManagedParserType(v.ParserType) { /* 判断条件并选择处理分支。 */
		problem(w, 422, "专用解析器仅供已有绑定及历史回放；新增或更新协议请上传 Go 源码包") /* 执行当前语句并推进处理流程。 */
		return                                                /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.Version == "" { /* 判断条件并选择处理分支。 */
		v.Version = "1.0.0" /* 更新 v.Version 的值。 */
	} /* 结束当前表达式或代码块。 */
	if v.Protocol == "" { /* 判断条件并选择处理分支。 */
		v.Protocol = "json" /* 更新 v.Protocol 的值。 */
	} /* 结束当前表达式或代码块。 */
	if v.Transport == "" { /* 判断条件并选择处理分支。 */
		v.Transport = "MQTT" /* 更新 v.Transport 的值。 */
	} /* 结束当前表达式或代码块。 */
	if v.PayloadFormat == "" { /* 判断条件并选择处理分支。 */
		v.PayloadFormat = "json" /* 更新 v.PayloadFormat 的值。 */
	} /* 结束当前表达式或代码块。 */
	if v.Status == "" { /* 判断条件并选择处理分支。 */
		v.Status = "DRAFT" /* 更新 v.Status 的值。 */
	} /* 结束当前表达式或代码块。 */
	if v.Status != "DRAFT" && v.Status != "PUBLISHED" && v.Status != "DISABLED" { /* 判断条件并选择处理分支。 */
		problem(w, 422, "status must be DRAFT, PUBLISHED or DISABLED") /* 执行当前语句并推进处理流程。 */
		return                                                         /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	now := time.Now().UnixMilli()                                                                      /* 更新 now 的值。 */
	if old, getErr := s.engine.Repo.GetProtocolPackage(r.Context(), c.TenantID, v.ID); getErr == nil { /* 判断条件并选择处理分支。 */
		v.CreatedAt = old.CreatedAt /* 更新 v.CreatedAt 的值。 */
		if v.Config == nil {        /* 判断条件并选择处理分支。 */
			v.Config = map[string]any{} /* 更新 v.Config 的值。 */
		} /* 结束当前表达式或代码块。 */
		if _, hasArtifact := v.Config["artifact"]; !hasArtifact { /* 判断条件并选择处理分支。 */
			if artifact, exists := old.Config["artifact"]; exists { /* 判断条件并选择处理分支。 */
				v.Config["artifact"] = artifact /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if v.CreatedAt == 0 { /* 判断条件并选择处理分支。 */
		v.CreatedAt = now /* 更新 v.CreatedAt 的值。 */
	} /* 结束当前表达式或代码块。 */
	v.UpdatedAt = now                                                         /* 更新 v.UpdatedAt 的值。 */
	if err := s.engine.Repo.SaveProtocolPackage(r.Context(), v); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.audit(r, "protocol.save", "protocolPackage", v.ID, map[string]any{"version": v.Version, "status": v.Status}) /* 执行当前语句并推进处理流程。 */
	write(w, 201, v)                                                                                               /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) testProtocolPackage(w http.ResponseWriter, r *http.Request) { /* 定义 testProtocolPackage 函数。 */
	pkg, err := s.engine.Repo.GetProtocolPackage(r.Context(), claims(r).TenantID, r.PathValue("id")) /* 更新 err 的值。 */
	if err != nil {                                                                                  /* 判断条件并选择处理分支。 */
		problem(w, 404, "protocol package not found") /* 执行当前语句并推进处理流程。 */
		return                                        /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var in struct { /* 声明 in。 */
		ProductID string          `json:"productId"` /* 执行当前语句并推进处理流程。 */
		DeviceID  string          `json:"deviceId"`  /* 执行当前语句并推进处理流程。 */
		Payload   json.RawMessage `json:"payload"`   /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if decode(w, r, &in) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(in.Payload) == 0 { /* 判断条件并选择处理分支。 */
		problem(w, 422, "payload is required") /* 执行当前语句并推进处理流程。 */
		return                                 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if in.ProductID == "" { /* 判断条件并选择处理分支。 */
		in.ProductID = "protocol_test" /* 更新 in.ProductID 的值。 */
	} /* 结束当前表达式或代码块。 */
	if in.DeviceID == "" { /* 判断条件并选择处理分支。 */
		in.DeviceID = "device_test" /* 更新 in.DeviceID 的值。 */
	} /* 结束当前表达式或代码块。 */
	raw := model.RawMessage{MessageID: "raw_test_" + randomHex(6), TenantID: pkg.TenantID, ProductID: in.ProductID, DeviceID: in.DeviceID, Protocol: pkg.Protocol, Transport: pkg.Transport, PayloadFormat: pkg.PayloadFormat, Payload: in.Payload, ReceivedAt: time.Now().UnixMilli()} /* 更新 raw 的值。 */
	msg, err := s.engine.Parsers.ParseWithConfig(pkg.ParserType, pkg.Config, raw)                                                                                                                                                                                                       /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                                                                     /* 判断条件并选择处理分支。 */
		write(w, 200, map[string]any{"success": false, "error": err.Error(), "raw": raw}) /* 执行当前语句并推进处理流程。 */
		return                                                                            /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 200, map[string]any{"success": true, "standardMessage": msg}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) deviceRegistry(w http.ResponseWriter, r *http.Request) { /* 定义 deviceRegistry 函数。 */
	tenantID := claims(r).TenantID       /* 更新 tenantID 的值。 */
	pagination := parseListPagination(r) /* 更新 pagination 的值。 */
	filter, err := s.deviceFilter(r.Context(), tenantID, r.URL.Query())
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	// The scope-aware repository filters limited users before totals and pagination.
	items, total, err := s.engine.Repo.ListManagedDevicesFiltered(r.Context(), filter, pagination.PageSize, pagination.Offset)
	if err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	deviceIDs := make([]string, 0, len(items)) /* 更新 deviceIDs 的值。 */
	for _, item := range items {               /* 循环处理当前数据。 */
		deviceIDs = append(deviceIDs, item.ID) /* 更新 deviceIDs 的值。 */
	} /* 结束当前表达式或代码块。 */
	childCounts, err := s.engine.Repo.CountManagedDeviceChildren(r.Context(), tenantID, deviceIDs) /* 更新 err 的值。 */
	if err != nil {                                                                                /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	productIDs := make([]string, 0, len(items)) /* 更新 productIDs 的值。 */
	for _, item := range items {                /* 循环处理当前数据。 */
		productIDs = append(productIDs, item.ProductID) /* 更新 productIDs 的值。 */
	} /* 结束当前表达式或代码块。 */
	products, err := s.engine.Repo.GetProductsByIDs(r.Context(), tenantID, productIDs) /* 更新 err 的值。 */
	if err != nil {                                                                    /* 判断条件并选择处理分支。 */
		products = map[string]model.Product{} /* 更新 products 的值。 */
		for _, id := range productIDs {       /* 循环处理当前数据。 */
			if product, getErr := s.engine.Repo.GetProduct(r.Context(), tenantID, id); getErr == nil { /* 判断条件并选择处理分支。 */
				products[id] = product /* 更新 products[id] 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	states, err := s.engine.Repo.GetDeviceStatesByIDs(r.Context(), tenantID, deviceIDs) /* 更新 err 的值。 */
	if err != nil {                                                                     /* 判断条件并选择处理分支。 */
		states = map[string]model.DeviceState{} /* 更新 states 的值。 */
		for _, id := range deviceIDs {          /* 循环处理当前数据。 */
			if state, getErr := s.engine.Repo.GetDeviceState(r.Context(), tenantID, id); getErr == nil { /* 判断条件并选择处理分支。 */
				states[id] = state /* 更新 states[id] 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	// Parents may be on another page; resolve their names within the caller's scope.
	parents := map[string]map[string]string{}
	for _, v := range items {
		if v.GatewayID == "" {
			continue
		}
		if _, seen := parents[v.GatewayID]; seen {
			continue
		}
		parents[v.GatewayID] = nil
		if parent, getErr := s.engine.Repo.GetManagedDevice(r.Context(), tenantID, v.GatewayID); getErr == nil {
			parents[v.GatewayID] = map[string]string{"id": parent.ID, "name": parent.Name}
		}
	}
	out := make([]map[string]any, 0, len(items)) /* 更新 out 的值。 */
	for _, v := range items {                    /* 循环处理当前数据。 */
		product := products[v.ProductID]                                                                                                               /* 更新 product 的值。 */
		row := map[string]any{"device": v.Public(product), "childCount": childCounts[v.ID], "credentialSupported": v.UsesPlatformCredentials(product)} /* 更新 row 的值。 */
		if parent := parents[v.GatewayID]; parent != nil {
			row["parent"] = parent
		}
		if state, ok := states[v.ID]; ok { /* 判断条件并选择处理分支。 */
			row["runtimeState"] = state /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		out = append(out, row) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	writeList(w, 200, out, total, pagination, nil) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) saveManagedDevice(w http.ResponseWriter, r *http.Request) { /* 定义 saveManagedDevice 函数。 */
	var v model.ManagedDevice    /* 声明 v。 */
	if decode(w, r, &v) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	c := claims(r)                         /* 更新 c 的值。 */
	v.TenantID = c.TenantID                /* 更新 v.TenantID 的值。 */
	if id := r.PathValue("id"); id != "" { /* 判断条件并选择处理分支。 */
		v.ID = id /* 更新 v.ID 的值。 */
	} /* 结束当前表达式或代码块。 */
	if v.ID == "" { /* 判断条件并选择处理分支。 */
		v.ID = "device_" + randomHex(6) /* 更新 v.ID 的值。 */
	} /* 结束当前表达式或代码块。 */
	if v.Name == "" || v.ProductID == "" { /* 判断条件并选择处理分支。 */
		problem(w, 422, "name and productId are required") /* 执行当前语句并推进处理流程。 */
		return                                             /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	product, err := s.engine.Repo.GetProduct(r.Context(), c.TenantID, v.ProductID) /* 更新 err 的值。 */
	if err != nil {                                                                /* 判断条件并选择处理分支。 */
		problem(w, 422, "product not found") /* 执行当前语句并推进处理流程。 */
		return                               /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	now := time.Now().UnixMilli()                                                              /* 更新 now 的值。 */
	credential := model.DeviceCredential{}                                                     /* 更新 credential 的值。 */
	created := false                                                                           /* 更新 created 的值。 */
	if old, err := s.engine.Repo.GetManagedDevice(r.Context(), c.TenantID, v.ID); err == nil { /* 判断条件并选择处理分支。 */
		if old.RegistrationSource == "PROTOCOL_CHILD_AUTO" { /* 判断条件并选择处理分支。 */
			if v.ProductID != old.ProductID || v.GatewayID != old.GatewayID || v.DeviceRole != "CHILD" { /* 判断条件并选择处理分支。 */
				problem(w, 422, "自动注册子设备的产品与主设备归属不可直接改写") /* 执行当前语句并推进处理流程。 */
				return                                    /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		v.AccessKey, v.SecretHash, v.SecretHint, v.CreatedAt = old.AccessKey, old.SecretHash, old.SecretHint, old.CreatedAt /* 更新 v.CreatedAt 的值。 */
		if v.Tags == nil {                                                                                                  /* 判断条件并选择处理分支。 */
			v.Tags = map[string]string{} /* 更新 v.Tags 的值。 */
		} /* 结束当前表达式或代码块。 */
		// Platform connection fields keep their stored values; the connection and
		// child address may be re-selected but are not cleared by an edit.
		v.Connector, v.ChildType, v.OnboardingRequestHash = old.Connector, old.ChildType, old.OnboardingRequestHash
		if v.ConnectorProfileID == "" {
			v.ConnectorProfileID = old.ConnectorProfileID
		}
		if v.ChildAddress == "" {
			v.ChildAddress = old.ChildAddress
		}
		if v.RegistrationSource == "" { /* 判断条件并选择处理分支。 */
			v.RegistrationSource = old.RegistrationSource /* 更新 v.RegistrationSource 的值。 */
		} /* 结束当前表达式或代码块。 */
		if old.AutoRegistered { /* 判断条件并选择处理分支。 */
			v.AutoRegistered = true /* 更新 v.AutoRegistered 的值。 */
		} /* 结束当前表达式或代码块。 */
	} else { /* 结束当前表达式或代码块。 */
		created = true    /* 更新 created 的值。 */
		v.CreatedAt = now /* 更新 v.CreatedAt 的值。 */
	} /* 结束当前表达式或代码块。 */
	if v.Status == "" { /* 判断条件并选择处理分支。 */
		v.Status = "ENABLED" /* 更新 v.Status 的值。 */
	} /* 结束当前表达式或代码块。 */
	if v.DeviceRole == "" { /* 判断条件并选择处理分支。 */
		if product.Category == "gateway" { /* 判断条件并选择处理分支。 */
			v.DeviceRole = "GATEWAY" /* 更新 v.DeviceRole 的值。 */
		} else { /* 结束当前表达式或代码块。 */
			v.DeviceRole = "DIRECT" /* 更新 v.DeviceRole 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if v.DeviceRole != "DIRECT" && v.DeviceRole != "GATEWAY" && v.DeviceRole != "CHILD" { /* 判断条件并选择处理分支。 */
		problem(w, 422, "deviceRole must be DIRECT, GATEWAY or CHILD") /* 执行当前语句并推进处理流程。 */
		return                                                         /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.RegistrationSource == "" { /* 判断条件并选择处理分支。 */
		v.RegistrationSource = "MANUAL" /* 更新 v.RegistrationSource 的值。 */
	} /* 结束当前表达式或代码块。 */
	if v.DeviceRole == "CHILD" { /* 判断条件并选择处理分支。 */
		if v.GatewayID == "" || v.GatewayID == v.ID { /* 判断条件并选择处理分支。 */
			problem(w, 422, "a child device must reference a different gateway") /* 执行当前语句并推进处理流程。 */
			return                                                               /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		gateway, gatewayErr := s.engine.Repo.GetManagedDevice(r.Context(), c.TenantID, v.GatewayID) /* 更新 gatewayErr 的值。 */
		if gatewayErr != nil {                                                                      /* 判断条件并选择处理分支。 */
			problem(w, 422, "gateway not found") /* 执行当前语句并推进处理流程。 */
			return                               /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		// The parent must be registered as a gateway; the template category alone does not grant it.
		if gateway.DeviceRole != "GATEWAY" {
			problem(w, 422, "selected parent device is not a gateway") /* 执行当前语句并推进处理流程。 */
			return                                                     /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} else { /* 结束当前表达式或代码块。 */
		v.GatewayID = "" /* 更新 v.GatewayID 的值。 */
	} /* 结束当前表达式或代码块。 */
	if v.Tags == nil {
		v.Tags = map[string]string{}
	}
	if v.DeviceRole == "CHILD" {
		parent, parentErr := s.engine.Repo.GetManagedDevice(r.Context(), c.TenantID, v.GatewayID)
		if parentErr != nil {
			problem(w, 422, "所属主设备已不存在")
			return
		}
		// A child uses its parent's physical connection; the caller cannot bind
		// it to an unrelated tenant-wide listener.
		if requested := v.ConnectorProfileID; requested != "" && requested != parent.ConnectorProfileID {
			problem(w, 422, "子设备只能继承所属主设备的连接")
			return
		}
		v.ConnectorProfileID = parent.ConnectorProfileID
	} else if requested := v.ConnectorProfileID; requested != "" {
		profiles, profileErr := s.engine.Repo.ListDeviceAccessProfiles(r.Context(), c.TenantID)
		if profileErr != nil {
			problem(w, 500, profileErr.Error())
			return
		}
		valid := false
		for _, candidate := range profiles {
			if candidate.ID == requested && candidate.ProductID == v.ProductID && (candidate.DeviceID == "" || candidate.DeviceID == v.ID) {
				valid = true
				break
			}
		}
		if !valid {
			problem(w, 422, "接入点不可用，请重新选择当前设备模板的接入点")
			return
		}
	}
	if created { /* 判断条件并选择处理分支。 */
		v.Connector, v.ChildType, v.OnboardingRequestHash = "", "", ""
		if product.ProtocolPackageID == parser.StandardProtocolID+"@1.0.0" { /* 判断条件并选择处理分支。 */
			v.Connector = "MQTT"             /* 执行当前语句并推进处理流程。 */
			if product.Transport == "HTTP" { /* 判断条件并选择处理分支。 */
				v.Connector = "HTTP" /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		v.AccessKey = model.ProtocolDeviceAccessKey(v.TenantID, v.ID) /* 更新 v.AccessKey 的值。 */
		v.SecretHash, v.SecretHint = "", ""                           /* 更新 v.SecretHint 的值。 */
		if v.UsesPlatformCredentials(product) {                       /* 判断条件并选择处理分支。 */
			credential = newDeviceCredential()                          /* 更新 credential 的值。 */
			v.AccessKey = credential.AccessKey                          /* 更新 v.AccessKey 的值。 */
			v.SecretHash = secretHash(credential.Secret)                /* 更新 v.SecretHash 的值。 */
			v.SecretHint = credential.Secret[len(credential.Secret)-6:] /* 更新 v.SecretHint 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	v.UpdatedAt = now                                                       /* 更新 v.UpdatedAt 的值。 */
	if err := s.engine.Repo.SaveManagedDevice(r.Context(), v); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.audit(r, "device.save", "device", v.ID, map[string]any{"productId": v.ProductID, "status": v.Status}) /* 执行当前语句并推进处理流程。 */
	result := map[string]any{"device": v.Public(product)}                                                   /* 更新 result 的值。 */
	if credential.Secret != "" {                                                                            /* 判断条件并选择处理分支。 */
		result["credential"] = credential /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 201, result) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) registerDiscoveredDevice(w http.ResponseWriter, r *http.Request) { /* 定义 registerDiscoveredDevice 函数。 */
	c := claims(r)                                                                         /* 更新 c 的值。 */
	id := r.PathValue("id")                                                                /* 更新 id 的值。 */
	if _, err := s.engine.Repo.GetManagedDevice(r.Context(), c.TenantID, id); err == nil { /* 判断条件并选择处理分支。 */
		problem(w, 409, "device is already registered") /* 执行当前语句并推进处理流程。 */
		return                                          /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	state, err := s.engine.Repo.GetDeviceState(r.Context(), c.TenantID, id) /* 更新 err 的值。 */
	if err != nil {                                                         /* 判断条件并选择处理分支。 */
		problem(w, 404, "discovered device not found") /* 执行当前语句并推进处理流程。 */
		return                                         /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	product, err := s.engine.Repo.GetProduct(r.Context(), c.TenantID, state.ProductID) /* 更新 err 的值。 */
	if err != nil {                                                                    /* 判断条件并选择处理分支。 */
		problem(w, 422, "register its product before registering this device") /* 执行当前语句并推进处理流程。 */
		return                                                                 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	credential := model.DeviceCredential{} /* 更新 credential 的值。 */
	now := time.Now().UnixMilli()          /* 更新 now 的值。 */
	role := "DIRECT"                       /* 更新 role 的值。 */
	if product.Category == "gateway" {     /* 判断条件并选择处理分支。 */
		role = "GATEWAY" /* 更新 role 的值。 */
	} /* 结束当前表达式或代码块。 */
	device := model.ManagedDevice{ID: id, TenantID: c.TenantID, ProductID: state.ProductID, Name: "发现设备 " + id, Status: "ENABLED", DeviceRole: role, RegistrationSource: "DISCOVERY", AccessKey: model.ProtocolDeviceAccessKey(c.TenantID, id), CreatedAt: now, UpdatedAt: now} /* 更新 device 的值。 */
	if device.UsesPlatformCredentials(product) {                                                                                                                                                                                                                                /* 判断条件并选择处理分支。 */
		credential = newDeviceCredential()                               /* 更新 credential 的值。 */
		device.AccessKey = credential.AccessKey                          /* 更新 device.AccessKey 的值。 */
		device.SecretHash = secretHash(credential.Secret)                /* 更新 device.SecretHash 的值。 */
		device.SecretHint = credential.Secret[len(credential.Secret)-6:] /* 更新 device.SecretHint 的值。 */
	} /* 结束当前表达式或代码块。 */
	if err = s.engine.Repo.SaveManagedDevice(r.Context(), device); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.audit(r, "device.discovery.register", "device", id, map[string]any{"productId": state.ProductID}) /* 执行当前语句并推进处理流程。 */
	result := map[string]any{"device": device.Public(product)}                                          /* 更新 result 的值。 */
	if credential.Secret != "" {                                                                        /* 判断条件并选择处理分支。 */
		result["credential"] = credential /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 201, result) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) rotateDeviceCredential(w http.ResponseWriter, r *http.Request) { /* 定义 rotateDeviceCredential 函数。 */
	if !s.operationDevice(w, r) { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	c, v, e := s.onboarding.ChangeCredential(r.Context(), claims(r).TenantID, r.PathValue("id"), true) /* 更新 e 的值。 */
	if e != nil {                                                                                      /* 判断条件并选择处理分支。 */
		status := http.StatusInternalServerError               /* 更新 status 的值。 */
		if errors.Is(e, onboarding.ErrCredentialUnsupported) { /* 判断条件并选择处理分支。 */
			status = http.StatusUnprocessableEntity /* 更新 status 的值。 */
		} /* 结束当前表达式或代码块。 */
		problem(w, status, e.Error()) /* 执行当前语句并推进处理流程。 */
		return                        /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.audit(r, "device.credential.rotate", "device", r.PathValue("id"), nil)                       /* 执行当前语句并推进处理流程。 */
	write(w, 200, map[string]any{"deviceId": r.PathValue("id"), "credential": c, "revocation": v}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) debugDeviceIngest(w http.ResponseWriter, r *http.Request) { /* 定义 debugDeviceIngest 函数。 */
	// 接入测试报文仍走正常归档与解析链路；设备归属从当前租户的登记记录确定。
	c := claims(r)                                                                       /* 更新 c 的值。 */
	v, err := s.engine.Repo.GetManagedDevice(r.Context(), c.TenantID, r.PathValue("id")) /* 更新 err 的值。 */
	if err != nil {                                                                      /* 判断条件并选择处理分支。 */
		problem(w, 404, "device not found") /* 执行当前语句并推进处理流程。 */
		return                              /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var raw model.RawMessage       /* 声明 raw。 */
	if decode(w, r, &raw) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err = s.prepareManagedRaw(r.Context(), &raw, v); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	idx, created, err := s.engine.IngestRaw(r.Context(), raw) /* 更新 err 的值。 */
	if err != nil {                                           /* 判断条件并选择处理分支。 */
		problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.audit(r, "device.debug.ingest", "device", v.ID, map[string]any{"messageId": idx.MessageID})              /* 执行当前语句并推进处理流程。 */
	write(w, map[bool]int{true: 201, false: 200}[created], map[string]any{"created": created, "archive": idx}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) deviceIngest(w http.ResponseWriter, r *http.Request) { /* 定义 deviceIngest 函数。 */
	accessKey, secret := r.Header.Get("X-Device-Key"), r.Header.Get("X-Device-Secret") /* 更新 secret 的值。 */
	v, err := s.onboarding.Authenticate(r.Context(), accessKey, secret)                /* 更新 err 的值。 */
	if err != nil || v.ID != r.PathValue("deviceId") {                                 /* 判断条件并选择处理分支。 */
		problem(w, 401, "invalid or disabled device credential") /* 执行当前语句并推进处理流程。 */
		return                                                   /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if tenantID := r.Header.Get("X-Tenant-ID"); tenantID != "" && tenantID != v.TenantID { /* 判断条件并选择处理分支。 */
		problem(w, 401, "invalid or disabled device credential") /* 执行当前语句并推进处理流程。 */
		return                                                   /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var raw model.RawMessage       /* 声明 raw。 */
	if decode(w, r, &raw) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err = s.prepareManagedRaw(r.Context(), &raw, v); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	// Credential-authenticated reports are field evidence; debug ingress keeps its own source.
	raw.Source = "device-http"
	idx, created, err := s.engine.IngestRaw(r.Context(), raw) /* 更新 err 的值。 */
	if err != nil {                                           /* 判断条件并选择处理分支。 */
		problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	write(w, map[bool]int{true: 201, false: 200}[created], map[string]any{"created": created, "messageId": idx.MessageID, "receivedAt": idx.ReceivedAt}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) prepareManagedRaw(ctx context.Context, raw *model.RawMessage, device model.ManagedDevice) error { /* 定义 prepareManagedRaw 函数。 */
	if device.Connector == "HTTP" || device.Connector == "MQTT" { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("standard devices must use the authenticated standard ingress endpoint") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	targetProductID := device.ProductID                  /* 更新 targetProductID 的值。 */
	if raw.DeviceID != "" && raw.DeviceID != device.ID { /* 判断条件并选择处理分支。 */
		if raw.ProductID == "" { /* 判断条件并选择处理分支。 */
			return fmt.Errorf("child productId is required") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		raw.GatewayID = device.ID       /* 更新 raw.GatewayID 的值。 */
		targetProductID = raw.ProductID /* 更新 targetProductID 的值。 */
		raw.Source = "gateway"          /* 更新 raw.Source 的值。 */
	} else { /* 结束当前表达式或代码块。 */
		raw.DeviceID = device.ID      /* 更新 raw.DeviceID 的值。 */
		raw.GatewayID = ""            /* 更新 raw.GatewayID 的值。 */
		raw.Source = "managed-device" /* 更新 raw.Source 的值。 */
	} /* 结束当前表达式或代码块。 */
	product, err := s.engine.Repo.GetProduct(ctx, device.TenantID, targetProductID) /* 更新 err 的值。 */
	if err != nil || product.Status != "ENABLED" {                                  /* 判断条件并选择处理分支。 */
		return fmt.Errorf("product is not enabled") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	pkg, err := s.engine.Repo.GetProtocolPackage(ctx, device.TenantID, product.ProtocolPackageID) /* 更新 err 的值。 */
	if err != nil || pkg.Status != "PUBLISHED" {                                                  /* 判断条件并选择处理分支。 */
		return fmt.Errorf("protocol package is not published") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	raw.TenantID, raw.ProductID = device.TenantID, product.ID                                       /* 更新 raw.ProductID 的值。 */
	raw.Protocol, raw.Transport, raw.PayloadFormat = pkg.Protocol, pkg.Transport, pkg.PayloadFormat /* 更新 raw.PayloadFormat 的值。 */
	return nil                                                                                      /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) ingestRaw(w http.ResponseWriter, r *http.Request) { /* 定义 ingestRaw 函数。 */
	var v model.RawMessage       /* 声明 v。 */
	if decode(w, r, &v) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	c := claims(r)                                          /* 更新 c 的值。 */
	v.TenantID = tenant(c, v.TenantID)                      /* 更新 v.TenantID 的值。 */
	start := time.Now()                                     /* 更新 start 的值。 */
	idx, created, err := s.engine.IngestRaw(r.Context(), v) /* 更新 err 的值。 */
	s.metrics.ObserveMS("raw_archive_latency_ms", start)    /* 执行当前语句并推进处理流程。 */
	s.metrics.ObserveMS("storage_latency_ms", start)        /* 执行当前语句并推进处理流程。 */
	if err != nil {                                         /* 判断条件并选择处理分支。 */
		s.metrics.Inc("raw_archive_failed_total") /* 执行当前语句并推进处理流程。 */
		problem(w, 422, err.Error())              /* 执行当前语句并推进处理流程。 */
		return                                    /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	write(w, map[bool]int{true: 201, false: 200}[created], map[string]any{"created": created, "archive": idx}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) listRaw(w http.ResponseWriter, r *http.Request) { /* 定义 listRaw 函数。 */
	c := claims(r)                                                                                                                                                                                                         /* 更新 c 的值。 */
	q := r.URL.Query()                                                                                                                                                                                                     /* 更新 q 的值。 */
	pagination := parseListPagination(r)                                                                                                                                                                                   /* 更新 pagination 的值。 */
	filter := ports.RawFilter{TenantID: c.TenantID, ProductID: q.Get("productId"), DeviceID: q.Get("deviceId"), Start: i64(q.Get("start")), End: i64(q.Get("end")), Limit: pagination.PageSize, Offset: pagination.Offset} /* 更新 filter 的值。 */
	var items []model.RawArchiveIndex                                                                                                                                                                                      /* 声明 items。 */
	var total int                                                                                                                                                                                                          /* 声明 total。 */
	var err error                                                                                                                                                                                                          /* 声明 err。 */
	if limited(r.Context()) {                                                                                                                                                                                              /* 判断条件并选择处理分支。 */
		all, scanErr := s.engine.Repo.(*deviceScopeRepository).scopedRaw(r.Context(), filter) /* 更新 scanErr 的值。 */
		err = scanErr                                                                         /* 更新 err 的值。 */
		if err == nil {                                                                       /* 判断条件并选择处理分支。 */
			total = len(all)                                    /* 更新 total 的值。 */
			items = pageSlice(all, filter.Limit, filter.Offset) /* 更新 items 的值。 */
		} /* 结束当前表达式或代码块。 */
	} else { /* 结束当前表达式或代码块。 */
		items, err = s.engine.Repo.ListRawIndexes(r.Context(), filter) /* 更新 err 的值。 */
	} /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !limited(r.Context()) { /* 判断条件并选择处理分支。 */
		total, err = s.engine.Repo.CountRawIndexes(r.Context(), filter) /* 更新 err 的值。 */
		if err != nil {                                                 /* 判断条件并选择处理分支。 */
			problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
			return                       /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	ids := make([]string, 0, len(items)) /* 更新 ids 的值。 */
	for _, item := range items {         /* 循环处理当前数据。 */
		ids = append(ids, item.MessageID) /* 更新 ids 的值。 */
	} /* 结束当前表达式或代码块。 */
	messages, err := s.engine.Repo.GetStandardMessagesByRawIDs(r.Context(), c.TenantID, ids) /* 更新 err 的值。 */
	if err != nil {                                                                          /* 判断条件并选择处理分支。 */
		messages = map[string]model.StandardMessage{} /* 更新 messages 的值。 */
		for _, id := range ids {                      /* 循环处理当前数据。 */
			if message, getErr := s.engine.Repo.GetStandardMessageByRaw(r.Context(), c.TenantID, id); getErr == nil { /* 判断条件并选择处理分支。 */
				messages[id] = message /* 更新 messages[id] 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	for i := range items { /* 循环处理当前数据。 */
		if message, ok := messages[items[i].MessageID]; ok { /* 判断条件并选择处理分支。 */
			items[i].Parsed = true                                   /* 更新 items[i].Parsed 的值。 */
			items[i].ParsedMessageType = string(message.MessageType) /* 更新 items[i].ParsedMessageType 的值。 */
			items[i].Parser = message.Parser                         /* 更新 items[i].Parser 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	writeList(w, 200, items, total, pagination, nil) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) rawDetail(w http.ResponseWriter, r *http.Request) { /* 定义 rawDetail 函数。 */
	idx, err := s.engine.Repo.GetRawIndex(r.Context(), claims(r).TenantID, r.PathValue("id")) /* 更新 err 的值。 */
	if err != nil {                                                                           /* 判断条件并选择处理分支。 */
		problem(w, 404, "raw message not found") /* 执行当前语句并推进处理流程。 */
		return                                   /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	raw, err := s.engine.GetRaw(r.Context(), idx) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		problem(w, 500, "raw archive could not be read") /* 执行当前语句并推进处理流程。 */
		return                                           /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	result := map[string]any{"archive": idx, "message": raw, "parseStatus": "UNPARSED", "parseError": idx.ParseError} /* 更新 result 的值。 */
	if idx.ParseError != "" {                                                                                         /* 判断条件并选择处理分支。 */
		result["parseStatus"] = "FAILED" /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if standard, parseErr := s.engine.Repo.GetStandardMessageByRaw(r.Context(), claims(r).TenantID, idx.MessageID); parseErr == nil { /* 判断条件并选择处理分支。 */
		result["parseStatus"] = "PARSED"     /* 执行当前语句并推进处理流程。 */
		result["standardMessage"] = standard /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 200, result) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) downloadRaw(w http.ResponseWriter, r *http.Request) { /* 定义 downloadRaw 函数。 */
	idx, err := s.engine.Repo.GetRawIndex(r.Context(), claims(r).TenantID, r.PathValue("id")) /* 更新 err 的值。 */
	if err != nil {                                                                           /* 判断条件并选择处理分支。 */
		problem(w, 404, "raw message not found") /* 执行当前语句并推进处理流程。 */
		return                                   /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	raw, err := s.engine.GetRaw(r.Context(), idx) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		problem(w, 500, "raw archive could not be read") /* 执行当前语句并推进处理流程。 */
		return                                           /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	body, err := json.MarshalIndent(raw, "", "  ") /* 更新 err 的值。 */
	if err != nil {                                /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	filename := strings.Map(func(r rune) rune { /* 更新 filename 的值。 */
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' { /* 判断条件并选择处理分支。 */
			return r /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return '_' /* 返回当前处理结果。 */
	}, idx.MessageID) + ".json" /* 结束当前表达式或代码块。 */
	w.Header().Set("Content-Type", "application/json; charset=utf-8")                                        /* 执行当前语句并推进处理流程。 */
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))                /* 执行当前语句并推进处理流程。 */
	w.Header().Set("X-Content-SHA256", idx.PayloadHash)                                                      /* 执行当前语句并推进处理流程。 */
	w.WriteHeader(http.StatusOK)                                                                             /* 执行当前语句并推进处理流程。 */
	_, _ = w.Write(append(body, '\n'))                                                                       /* 更新 _ 的值。 */
	s.audit(r, "raw.download", "raw-message", idx.MessageID, map[string]any{"payloadHash": idx.PayloadHash}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) downloadRawBatch(w http.ResponseWriter, r *http.Request) { /* 定义 downloadRawBatch 函数。 */
	var in struct { /* 声明 in。 */
		MessageIDs []string `json:"messageIds"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if decode(w, r, &in) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(in.MessageIDs) == 0 { /* 判断条件并选择处理分支。 */
		problem(w, 400, "at least one messageId is required") /* 执行当前语句并推进处理流程。 */
		return                                                /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(in.MessageIDs) > 500 { /* 判断条件并选择处理分支。 */
		problem(w, 422, "no more than 500 raw messages may be downloaded at once") /* 执行当前语句并推进处理流程。 */
		return                                                                     /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	type archivedRaw struct { /* 定义 archivedRaw 类型。 */
		Index   model.RawArchiveIndex /* 执行当前语句并推进处理流程。 */
		Message model.RawMessage      /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	items := make([]archivedRaw, 0, len(in.MessageIDs))   /* 更新 items 的值。 */
	seen := make(map[string]struct{}, len(in.MessageIDs)) /* 更新 seen 的值。 */
	totalPayloadSize := 0                                 /* 更新 totalPayloadSize 的值。 */
	for _, id := range in.MessageIDs {                    /* 循环处理当前数据。 */
		id = strings.TrimSpace(id) /* 更新 id 的值。 */
		if id == "" {              /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if _, ok := seen[id]; ok { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		seen[id] = struct{}{}                                                      /* 更新 seen[id] 的值。 */
		idx, err := s.engine.Repo.GetRawIndex(r.Context(), claims(r).TenantID, id) /* 更新 err 的值。 */
		if err != nil {                                                            /* 判断条件并选择处理分支。 */
			problem(w, 404, "raw message not found: "+id) /* 执行当前语句并推进处理流程。 */
			return                                        /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		totalPayloadSize += idx.PayloadSize   /* 更新 totalPayloadSize 的值。 */
		if totalPayloadSize > 100*1024*1024 { /* 判断条件并选择处理分支。 */
			problem(w, 413, "selected raw messages exceed the 100 MiB batch limit") /* 执行当前语句并推进处理流程。 */
			return                                                                  /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		raw, err := s.engine.GetRaw(r.Context(), idx) /* 更新 err 的值。 */
		if err != nil {                               /* 判断条件并选择处理分支。 */
			problem(w, 500, "raw archive could not be read: "+id) /* 执行当前语句并推进处理流程。 */
			return                                                /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		items = append(items, archivedRaw{Index: idx, Message: raw}) /* 更新 items 的值。 */
	} /* 结束当前表达式或代码块。 */
	if len(items) == 0 { /* 判断条件并选择处理分支。 */
		problem(w, 400, "at least one valid messageId is required") /* 执行当前语句并推进处理流程。 */
		return                                                      /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var archive bytes.Buffer                                 /* 声明 archive。 */
	zw := zip.NewWriter(&archive)                            /* 更新 zw 的值。 */
	manifest := make([]model.RawArchiveIndex, 0, len(items)) /* 更新 manifest 的值。 */
	for i, item := range items {                             /* 循环处理当前数据。 */
		body, err := json.MarshalIndent(item.Message, "", "  ") /* 更新 err 的值。 */
		if err != nil {                                         /* 判断条件并选择处理分支。 */
			problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
			return                       /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		name := fmt.Sprintf("报文/%03d_%s.json", i+1, safeAttachmentName(item.Index.MessageID)) /* 更新 name 的值。 */
		file, err := zw.Create(name)                                                          /* 更新 err 的值。 */
		if err != nil {                                                                       /* 判断条件并选择处理分支。 */
			problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
			return                       /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if _, err = file.Write(append(body, '\n')); err != nil { /* 判断条件并选择处理分支。 */
			problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
			return                       /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		manifest = append(manifest, item.Index) /* 更新 manifest 的值。 */
	} /* 结束当前表达式或代码块。 */
	manifestBody, _ := json.MarshalIndent(map[string]any{"exportedAt": time.Now().UnixMilli(), "count": len(items), "items": manifest}, "", "  ") /* 更新 _ 的值。 */
	manifestFile, err := zw.Create("清单.json")                                                                                                     /* 更新 err 的值。 */
	if err == nil {                                                                                                                               /* 判断条件并选择处理分支。 */
		_, err = manifestFile.Write(append(manifestBody, '\n')) /* 更新 err 的值。 */
	} /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err = zw.Close(); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	filename := fmt.Sprintf("原始报文_%s_%d条.zip", time.Now().Format("20060102_150405"), len(items))                                                  /* 更新 filename 的值。 */
	w.Header().Set("Content-Type", "application/zip")                                                                                             /* 执行当前语句并推进处理流程。 */
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="raw-messages.zip"; filename*=UTF-8''%s`, url.QueryEscape(filename))) /* 执行当前语句并推进处理流程。 */
	w.Header().Set("X-Archive-Count", strconv.Itoa(len(items)))                                                                                   /* 执行当前语句并推进处理流程。 */
	w.Header().Set("Content-Length", strconv.Itoa(archive.Len()))                                                                                 /* 执行当前语句并推进处理流程。 */
	w.WriteHeader(http.StatusOK)                                                                                                                  /* 执行当前语句并推进处理流程。 */
	_, _ = archive.WriteTo(w)                                                                                                                     /* 更新 _ 的值。 */
	s.audit(r, "raw.download.batch", "raw-message", "batch", map[string]any{"count": len(items), "payloadBytes": totalPayloadSize})               /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func safeAttachmentName(value string) string { /* 定义 safeAttachmentName 函数。 */
	return strings.Map(func(r rune) rune { /* 返回当前处理结果。 */
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' { /* 判断条件并选择处理分支。 */
			return r /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return '_' /* 返回当前处理结果。 */
	}, value) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) startReplay(w http.ResponseWriter, r *http.Request) { /* 定义 startReplay 函数。 */
	var v model.ReplayRequest    /* 声明 v。 */
	if decode(w, r, &v) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	c := claims(r)                                    /* 更新 c 的值。 */
	v.TenantID = c.TenantID                           /* 更新 v.TenantID 的值。 */
	v.CreatedBy = c.Username                          /* 更新 v.CreatedBy 的值。 */
	task, err := s.engine.StartReplay(r.Context(), v) /* 更新 err 的值。 */
	if err != nil {                                   /* 判断条件并选择处理分支。 */
		problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 202, task) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) getReplay(w http.ResponseWriter, r *http.Request) { /* 定义 getReplay 函数。 */
	v, err := s.engine.Repo.GetReplay(r.Context(), r.PathValue("id")) /* 更新 err 的值。 */
	if err != nil {                                                   /* 判断条件并选择处理分支。 */
		problem(w, 404, "replay not found") /* 执行当前语句并推进处理流程。 */
		return                              /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.TenantID != claims(r).TenantID { /* 判断条件并选择处理分支。 */
		problem(w, 404, "replay not found") /* 执行当前语句并推进处理流程。 */
		return                              /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 200, v) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) devices(w http.ResponseWriter, r *http.Request) { /* 定义 devices 函数。 */
	pagination := parseListPagination(r)                                                                /* 更新 pagination 的值。 */
	tenantID := claims(r).TenantID                                                                      /* 更新 tenantID 的值。 */
	unregisteredOnly := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("unregistered")), "true") /* 更新 unregisteredOnly 的值。 */
	var items []model.DeviceState                                                                       /* 声明 items。 */
	var total int                                                                                       /* 声明 total。 */
	var err error                                                                                       /* 声明 err。 */
	if unregisteredOnly {                                                                               /* 判断条件并选择处理分支。 */
		items, total, err = s.engine.Repo.ListUnregisteredDeviceStatesPage(r.Context(), tenantID, pagination.PageSize, pagination.Offset) /* 更新 err 的值。 */
	} else { /* 结束当前表达式或代码块。 */
		items, total, err = s.engine.Repo.ListDeviceStatesPage(r.Context(), tenantID, pagination.PageSize, pagination.Offset) /* 更新 err 的值。 */
	} /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	_, online, err := s.engine.Repo.CountDeviceStates(r.Context(), tenantID, unregisteredOnly) /* 更新 err 的值。 */
	if err != nil {                                                                            /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	writeList(w, 200, items, total, pagination, map[string]any{"online": online, "offline": total - online, "unregistered": unregisteredOnly}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) deviceLatest(w http.ResponseWriter, r *http.Request) { /* 定义 deviceLatest 函数。 */
	tenant, device := claims(r).TenantID, r.PathValue("deviceId")       /* 更新 device 的值。 */
	v, err := s.engine.Repo.GetDeviceState(r.Context(), tenant, device) /* 更新 err 的值。 */
	if err != nil {                                                     /* 判断条件并选择处理分支。 */
		problem(w, 404, "device state not found") /* 执行当前语句并推进处理流程。 */
		return                                    /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	out := map[string]any{"state": v}                                                                       /* 更新 out 的值。 */
	if latest, latestErr := s.engine.Repo.GetLatestMessage(r.Context(), tenant, device); latestErr == nil { /* 判断条件并选择处理分支。 */
		out["latestMessage"] = latest         /* 执行当前语句并推进处理流程。 */
		out["properties"] = latest.Properties /* 执行当前语句并推进处理流程。 */
		out["timestamp"] = latest.Timestamp   /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 200, out) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) history(w http.ResponseWriter, r *http.Request) { /* 定义 history 函数。 */
	q := r.URL.Query()            /* 更新 q 的值。 */
	property := q.Get("property") /* 更新 property 的值。 */
	if property == "" {           /* 判断条件并选择处理分支。 */
		problem(w, 400, "property is required") /* 执行当前语句并推进处理流程。 */
		return                                  /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	pagination := parseListPagination(r)                                                                                                                                                                       /* 更新 pagination 的值。 */
	items, total, err := s.engine.Repo.PropertyHistoryPage(r.Context(), claims(r).TenantID, r.PathValue("deviceId"), property, i64(q.Get("start")), i64(q.Get("end")), pagination.PageSize, pagination.Offset) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                            /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	writeList(w, 200, items, total, pagination, nil) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) stateEvent(w http.ResponseWriter, r *http.Request) { /* 定义 stateEvent 函数。 */
	var v model.DeviceState      /* 声明 v。 */
	if decode(w, r, &v) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	v.TenantID = tenant(claims(r), v.TenantID)                         /* 更新 v.TenantID 的值。 */
	if err := s.engine.UpdateDeviceState(r.Context(), v); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 202, v) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) rules(w http.ResponseWriter, r *http.Request) { /* 定义 rules 函数。 */
	pagination := parseListPagination(r)                                                                                  /* 更新 pagination 的值。 */
	v, total, err := s.engine.Repo.ListRulesPage(r.Context(), claims(r).TenantID, pagination.PageSize, pagination.Offset) /* 更新 err 的值。 */
	if err != nil {                                                                                                       /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	writeList(w, 200, v, total, pagination, nil) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) saveRule(w http.ResponseWriter, r *http.Request) { /* 定义 saveRule 函数。 */
	var v model.AlarmRule        /* 声明 v。 */
	if decode(w, r, &v) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	c := claims(r)                         /* 更新 c 的值。 */
	v.TenantID = c.TenantID                /* 更新 v.TenantID 的值。 */
	status := http.StatusCreated           /* 更新 status 的值。 */
	wasEnabled := false                    /* 更新 wasEnabled 的值。 */
	if id := r.PathValue("id"); id != "" { /* 判断条件并选择处理分支。 */
		status = http.StatusOK                                         /* 更新 status 的值。 */
		v.ID = id                                                      /* 更新 v.ID 的值。 */
		items, err := s.engine.Repo.ListRules(r.Context(), c.TenantID) /* 更新 err 的值。 */
		if err != nil {                                                /* 判断条件并选择处理分支。 */
			problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
			return                       /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		found := false                  /* 更新 found 的值。 */
		for _, current := range items { /* 循环处理当前数据。 */
			if current.ID == id { /* 判断条件并选择处理分支。 */
				wasEnabled = current.Enabled    /* 更新 wasEnabled 的值。 */
				v.CreatedAt = current.CreatedAt /* 更新 v.CreatedAt 的值。 */
				v.Version = current.Version + 1 /* 更新 v.Version 的值。 */
				found = true                    /* 更新 found 的值。 */
				break                           /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if !found { /* 判断条件并选择处理分支。 */
			problem(w, 404, "rule not found") /* 执行当前语句并推进处理流程。 */
			return                            /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} else if v.ID == "" { /* 结束当前表达式或代码块。 */
		v.ID = fmt.Sprintf("rule_%d", time.Now().UnixNano()) /* 更新 v.ID 的值。 */
	} /* 结束当前表达式或代码块。 */
	if v.Version == 0 { /* 判断条件并选择处理分支。 */
		v.Version = 1 /* 更新 v.Version 的值。 */
	} /* 结束当前表达式或代码块。 */
	now := time.Now().UnixMilli() /* 更新 now 的值。 */
	if v.CreatedAt == 0 {         /* 判断条件并选择处理分支。 */
		v.CreatedAt = now /* 更新 v.CreatedAt 的值。 */
	} /* 结束当前表达式或代码块。 */
	v.UpdatedAt = now                                                    /* 更新 v.UpdatedAt 的值。 */
	if len(v.Conditions) == 0 && strings.TrimSpace(v.Expression) == "" { /* 判断条件并选择处理分支。 */
		problem(w, 422, "at least one condition or a Gengine expression is required") /* 执行当前语句并推进处理流程。 */
		return                                                                        /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.Expression != "" { /* 判断条件并选择处理分支。 */
		if err := core.ValidateGengineExpression(v.Expression); err != nil { /* 判断条件并选择处理分支。 */
			problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
			return                       /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	_, conflicts, validationErr := s.engine.ValidateRuleDraft(r.Context(), v) /* 更新 validationErr 的值。 */
	if validationErr != nil {                                                 /* 判断条件并选择处理分支。 */
		problem(w, 422, validationErr.Error()) /* 执行当前语句并推进处理流程。 */
		return                                 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(conflicts) > 0 && !strings.EqualFold(r.URL.Query().Get("confirmConflicts"), "true") { /* 判断条件并选择处理分支。 */
		write(w, 409, map[string]any{"type": "rule-conflict", "detail": "rule conflicts require explicit confirmation", "conflicts": conflicts}) /* 执行当前语句并推进处理流程。 */
		return                                                                                                                                   /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if status == http.StatusOK && wasEnabled && !v.Enabled { /* 判断条件并选择处理分支。 */
		if err := s.engine.DisableRule(r.Context(), c.TenantID, v.ID); err != nil { /* 判断条件并选择处理分支。 */
			problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
			return                       /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if err := s.engine.Repo.SaveRule(r.Context(), v); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.audit(r, "rule.save", "rule", v.ID, map[string]any{"version": v.Version, "enabled": v.Enabled}) /* 执行当前语句并推进处理流程。 */
	write(w, status, v)                                                                               /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request) { /* 定义 deleteRule 函数。 */
	id := r.PathValue("id")                                                          /* 更新 id 的值。 */
	if err := s.engine.DeleteRule(r.Context(), claims(r).TenantID, id); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 404, "rule not found") /* 执行当前语句并推进处理流程。 */
		return                            /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.audit(r, "rule.delete", "rule", id, nil)               /* 执行当前语句并推进处理流程。 */
	write(w, 200, map[string]any{"deleted": true, "id": id}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) alarms(w http.ResponseWriter, r *http.Request) { /* 定义 alarms 函数。 */
	q := r.URL.Query()                                                                                                                                                                                                                                                         /* 更新 q 的值。 */
	pagination := parseListPagination(r)                                                                                                                                                                                                                                       /* 更新 pagination 的值。 */
	filter := ports.AlarmFilter{TenantID: claims(r).TenantID, DeviceID: q.Get("deviceId"), Status: q.Get("status"), Level: q.Get("level"), Source: q.Get("source"), Start: i64(q.Get("start")), End: i64(q.Get("end")), Limit: pagination.PageSize, Offset: pagination.Offset} /* 更新 filter 的值。 */
	var items []model.Alarm                                                                                                                                                                                                                                                    /* 声明 items。 */
	var total int                                                                                                                                                                                                                                                              /* 声明 total。 */
	var err error                                                                                                                                                                                                                                                              /* 声明 err。 */
	if limited(r.Context()) {                                                                                                                                                                                                                                                  /* 判断条件并选择处理分支。 */
		all, scanErr := s.engine.Repo.(*deviceScopeRepository).scopedAlarms(r.Context(), filter) /* 更新 scanErr 的值。 */
		err = scanErr                                                                            /* 更新 err 的值。 */
		if err == nil {                                                                          /* 判断条件并选择处理分支。 */
			total = len(all)                                    /* 更新 total 的值。 */
			items = pageSlice(all, filter.Limit, filter.Offset) /* 更新 items 的值。 */
		} /* 结束当前表达式或代码块。 */
	} else { /* 结束当前表达式或代码块。 */
		items, err = s.engine.Repo.ListAlarms(r.Context(), filter) /* 更新 err 的值。 */
	} /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	deviceIDs := make([]string, 0, len(items)) /* 更新 deviceIDs 的值。 */
	for _, item := range items {               /* 循环处理当前数据。 */
		deviceIDs = append(deviceIDs, item.DeviceID) /* 更新 deviceIDs 的值。 */
	} /* 结束当前表达式或代码块。 */
	cameras, err := s.engine.ListCameraSummariesForDevices(r.Context(), claims(r).TenantID, deviceIDs) /* 更新 err 的值。 */
	if err == nil {                                                                                    /* 判断条件并选择处理分支。 */
		for index := range items { /* 循环处理当前数据。 */
			items[index].Cameras = cameras[items[index].DeviceID] /* 更新 items[index].Cameras 的值。 */
		} /* 结束当前表达式或代码块。 */
	} else { /* 结束当前表达式或代码块。 */
		for index := range items { /* 循环处理当前数据。 */
			if summaries, getErr := s.engine.ListCameraSummaries(r.Context(), items[index].TenantID, items[index].DeviceID); getErr == nil { /* 判断条件并选择处理分支。 */
				items[index].Cameras = summaries /* 更新 items[index].Cameras 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if !limited(r.Context()) { /* 判断条件并选择处理分支。 */
		total, err = s.engine.Repo.CountAlarms(r.Context(), filter) /* 更新 err 的值。 */
		if err != nil {                                             /* 判断条件并选择处理分支。 */
			problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
			return                       /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	writeList(w, 200, items, total, pagination, nil) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) alarm(w http.ResponseWriter, r *http.Request) { /* 定义 alarm 函数。 */
	v, err := s.engine.Repo.GetAlarm(r.Context(), claims(r).TenantID, r.PathValue("id")) /* 更新 err 的值。 */
	if err != nil {                                                                      /* 判断条件并选择处理分支。 */
		problem(w, 404, "alarm not found") /* 执行当前语句并推进处理流程。 */
		return                             /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if cameras, cameraErr := s.engine.ListCameraSummaries(r.Context(), v.TenantID, v.DeviceID); cameraErr == nil { /* 判断条件并选择处理分支。 */
		v.Cameras = cameras /* 更新 v.Cameras 的值。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 200, v) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) alarmAction(w http.ResponseWriter, r *http.Request) { /* 定义 alarmAction 函数。 */
	var in struct { /* 声明 in。 */
		Action string `json:"action"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if decode(w, r, &in) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	v, err := s.engine.SetAlarmStatus(r.Context(), claims(r).TenantID, r.PathValue("id"), strings.ToUpper(in.Action), claims(r).Username) /* 更新 err 的值。 */
	if err != nil {                                                                                                                       /* 判断条件并选择处理分支。 */
		problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 200, v) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
// aiAnalysis returns the newest analysis variant the caller's role may read.
// Knowledge-based variants stay hidden from roles without knowledge access.
func (s *Server) aiAnalysis(w http.ResponseWriter, r *http.Request) { /* 定义 aiAnalysis 函数。 */
	var latest model.AIAnalysis
	found := false
	for _, scope := range alarmAnalysisViewScopes(r.Context()) {
		v, err := s.engine.Repo.GetAIAnalysis(r.Context(), claims(r).TenantID, r.PathValue("alarmId"), scope)
		if errors.Is(err, model.ErrNotFound) {
			continue
		}
		if err != nil {
			problem(w, 500, err.Error())
			return
		}
		if !found || v.CreatedAt > latest.CreatedAt {
			latest, found = v, true
		}
	}
	if !found {
		problem(w, 404, "analysis not found or still pending") /* 执行当前语句并推进处理流程。 */
		return                                                 /* 返回当前处理结果。 */
	}
	write(w, 200, latest) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) aiProviders(w http.ResponseWriter, r *http.Request) { /* 定义 aiProviders 函数。 */
	pagination := parseListPagination(r) /* 更新 pagination 的值。 */
	items := []ports.AIPluginInfo{}      /* 更新 items 的值。 */
	if s.engine.AIPlugins != nil {       /* 判断条件并选择处理分支。 */
		items = s.engine.AIPlugins.List() /* 更新 items 的值。 */
	} /* 结束当前表达式或代码块。 */
	for index := range items { /* 循环处理当前数据。 */
		if !s.canConfigureAI(r) { /* 判断条件并选择处理分支。 */
			items[index].DefaultBaseURL = "" /* 更新 items[index].DefaultBaseURL 的值。 */
		} else if items[index].ID == "ollama" { /* 结束当前表达式或代码块。 */
			items[index].DefaultBaseURL = s.cfg.AITestOllamaURL /* 更新 items[index].DefaultBaseURL 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	active := ports.AIPluginInfo{ID: "disabled", Name: "未启用", Enabled: false} /* 更新 active 的值。 */
	if provider, ok := s.engine.AI.(ports.AIInspectable); ok {                /* 判断条件并选择处理分支。 */
		active = provider.ProviderInfo() /* 更新 active 的值。 */
	} /* 结束当前表达式或代码块。 */
	healthy := false                           /* 更新 healthy 的值。 */
	healthMessage := "AI provider is disabled" /* 更新 healthMessage 的值。 */
	if active.Enabled && s.engine.AI != nil {  /* 判断条件并选择处理分支。 */
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second) /* 更新 cancel 的值。 */
		defer cancel()                                                 /* 安排函数结束时执行清理。 */
		if err := s.engine.AI.Health(ctx); err != nil {                /* 判断条件并选择处理分支。 */
			healthMessage = "连接异常" /* 更新 healthMessage 的值。 */
			if s.log != nil {      /* 判断条件并选择处理分支。 */
				s.log.Warn("AI provider health check failed", "provider", active.ID, "model", active.Model, "error", err) /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} else { /* 结束当前表达式或代码块。 */
			healthy = true         /* 更新 healthy 的值。 */
			healthMessage = "连接正常" /* 更新 healthMessage 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	active.DefaultBaseURL = ""                                                                                             /* 更新 active.DefaultBaseURL 的值。 */
	items, total := pageItems(items, pagination)                                                                           /* 更新 total 的值。 */
	meta := map[string]any{"active": active, "healthy": healthy, "healthMessage": healthMessage, "mode": "plugin-harness"} /* 更新 meta 的值。 */
	if s.aiProviderRuntime != nil {                                                                                        /* 判断条件并选择处理分支。 */
		meta["config"] = s.aiProviderConfigView(r, s.aiProviderRuntime.CurrentConfig(), active) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	writeList(w, 200, items, total, pagination, meta) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func effectiveAIMaxTokens(value int) int {
	if value < 128 || value > 8192 {
		return 2048
	}
	return value
}

func (s *Server) aiProviderConfigView(r *http.Request, config ports.AIPluginConfig, info ports.AIPluginInfo) map[string]any { /* 定义 aiProviderConfigView 函数。 */
	key := strings.TrimSpace(config.APIKey) /* 更新 key 的值。 */
	view := map[string]any{                 /* 更新 view 的值。 */
		"provider":         config.Provider, /* 执行当前语句并推进处理流程。 */
		"providerName":     info.Name,       /* 执行当前语句并推进处理流程。 */
		"model":            config.Model,    /* 执行当前语句并推进处理流程。 */
		"maxTokens":        effectiveAIMaxTokens(config.MaxTokens),
		"apiKeyConfigured": key != "",    /* 执行当前语句并推进处理流程。 */
		"active":           info.Enabled, /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if s.canConfigureAI(r) { /* 判断条件并选择处理分支。 */
		view["baseUrl"] = config.BaseURL /* 执行当前语句并推进处理流程。 */
		if key != "" {                   /* 判断条件并选择处理分支。 */
			view["apiKeyHint"] = key[:minInt(len(key), 4)] + "***" /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return view /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) aiProviderConfig(w http.ResponseWriter, r *http.Request) { /* 定义 aiProviderConfig 函数。 */
	if s.aiProviderRuntime == nil { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusServiceUnavailable, "AI provider runtime is unavailable") /* 执行当前语句并推进处理流程。 */
		return                                                                          /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	info := s.aiProviderRuntime.ProviderInfo()                                                    /* 更新 info 的值。 */
	write(w, http.StatusOK, s.aiProviderConfigView(r, s.aiProviderRuntime.CurrentConfig(), info)) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) updateAIProviderConfig(w http.ResponseWriter, r *http.Request) { /* 定义 updateAIProviderConfig 函数。 */
	if s.aiProviderRuntime == nil { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusServiceUnavailable, "AI provider runtime is unavailable") /* 执行当前语句并推进处理流程。 */
		return                                                                          /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.aiProviderUpdateMu.Lock()         /* 执行当前语句并推进处理流程。 */
	defer s.aiProviderUpdateMu.Unlock() /* 安排函数结束时执行清理。 */
	var in struct {                     /* 声明 in。 */
		Provider  string  `json:"provider"` /* 执行当前语句并推进处理流程。 */
		BaseURL   string  `json:"baseUrl"`  /* 执行当前语句并推进处理流程。 */
		Model     string  `json:"model"`    /* 执行当前语句并推进处理流程。 */
		APIKey    *string `json:"apiKey"`   /* 执行当前语句并推进处理流程。 */
		MaxTokens *int    `json:"maxTokens"`
	} /* 结束当前表达式或代码块。 */
	if decode(w, r, &in) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	provider := strings.ToLower(strings.TrimSpace(in.Provider))                            /* 更新 provider 的值。 */
	if provider != "ollama" && provider != "deepseek" && provider != "openai-compatible" { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusUnprocessableEntity, "模型来源必须是 ollama、deepseek 或 openai-compatible") /* 执行当前语句并推进处理流程。 */
		return                                                                                    /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	current := s.aiProviderRuntime.CurrentConfig()                   /* 更新 current 的值。 */
	baseURL := strings.TrimRight(strings.TrimSpace(in.BaseURL), "/") /* 更新 baseURL 的值。 */
	if baseURL == "" {                                               /* 判断条件并选择处理分支。 */
		if provider == "ollama" { /* 判断条件并选择处理分支。 */
			baseURL = strings.TrimRight(strings.TrimSpace(s.cfg.AITestOllamaURL), "/") /* 更新 baseURL 的值。 */
		} else if provider == "deepseek" { /* 结束当前表达式或代码块。 */
			baseURL = "https://api.deepseek.com" /* 更新 baseURL 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if err := validateAIProviderURL(baseURL); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusUnprocessableEntity, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                                                  /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len([]rune(baseURL)) > 2048 { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusUnprocessableEntity, "baseUrl 不能超过 2048 个字符") /* 执行当前语句并推进处理流程。 */
		return                                                              /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if provider == "ollama" { /* 判断条件并选择处理分支。 */
		if parsed, parseErr := url.Parse(baseURL); parseErr == nil && parsed.Path == "/v1" { /* 判断条件并选择处理分支。 */
			parsed.Path = ""                                  /* 更新 parsed.Path 的值。 */
			parsed.RawPath = ""                               /* 更新 parsed.RawPath 的值。 */
			baseURL = strings.TrimRight(parsed.String(), "/") /* 更新 baseURL 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	modelName := strings.TrimSpace(in.Model) /* 更新 modelName 的值。 */
	if modelName == "" {                     /* 判断条件并选择处理分支。 */
		if provider == current.Provider { /* 判断条件并选择处理分支。 */
			modelName = current.Model /* 更新 modelName 的值。 */
		} /* 结束当前表达式或代码块。 */
		if modelName == "" && s.engine.AIPlugins != nil { /* 判断条件并选择处理分支。 */
			for _, item := range s.engine.AIPlugins.List() { /* 循环处理当前数据。 */
				if item.ID == provider { /* 判断条件并选择处理分支。 */
					modelName = item.DefaultModel /* 更新 modelName 的值。 */
					break                         /* 执行当前语句并推进处理流程。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if !validAIModelName(modelName) { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusUnprocessableEntity, "model 名称只能以字母或数字开头，并包含字母、数字、点、冒号、斜线、下划线或短横线") /* 执行当前语句并推进处理流程。 */
		return                                                                                    /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	apiKey := ""          /* 更新 apiKey 的值。 */
	if in.APIKey != nil { /* 判断条件并选择处理分支。 */
		apiKey = strings.TrimSpace(*in.APIKey) /* 更新 apiKey 的值。 */
	} else if provider == current.Provider { /* 结束当前表达式或代码块。 */
		apiKey = current.APIKey /* 更新 apiKey 的值。 */
	} /* 结束当前表达式或代码块。 */
	if provider != "ollama" && apiKey == "" { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusUnprocessableEntity, "云端或兼容接口模型必须填写接口密钥") /* 执行当前语句并推进处理流程。 */
		return                                                          /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	maxTokens := effectiveAIMaxTokens(current.MaxTokens)
	if in.MaxTokens != nil {
		if *in.MaxTokens < 128 || *in.MaxTokens > 8192 {
			problem(w, http.StatusUnprocessableEntity, "最大输出词元必须在 128 到 8192 之间")
			return
		}
		maxTokens = *in.MaxTokens
	}
	candidate := ports.AIPluginConfig{Provider: provider, BaseURL: baseURL, Model: modelName, APIKey: apiKey, MaxTokens: maxTokens} /* 更新 candidate 的值。 */
	configureCtx, cancel := context.WithTimeout(r.Context(), 20*time.Second)                                                        /* 更新 cancel 的值。 */
	defer cancel()                                                                                                                  /* 安排函数结束时执行清理。 */
	if err := s.aiProviderRuntime.Configure(configureCtx, candidate); err != nil {                                                  /* 判断条件并选择处理分支。 */
		problem(w, http.StatusBadGateway, "模型服务健康检查失败，请检查地址、模型和接口密钥") /* 执行当前语句并推进处理流程。 */
		return                                                        /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if s.aiWorkflowProvider != nil { /* 判断条件并选择处理分支。 */
		if err := s.aiWorkflowProvider.ConfigureProvider(configureCtx, candidate); err != nil { /* 判断条件并选择处理分支。 */
			// Restore the previous direct provider when the workflow sidecar
			// rejects the same configuration, keeping both AI paths aligned.
			rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 20*time.Second) /* 更新 rollbackCancel 的值。 */
			_ = s.aiProviderRuntime.Configure(rollbackCtx, current)                                  /* 更新 _ 的值。 */
			rollbackCancel()                                                                         /* 执行当前语句并推进处理流程。 */
			problem(w, http.StatusBadGateway, "AI Workflow Harness 更新失败，Provider 未切换")               /* 执行当前语句并推进处理流程。 */
			return                                                                                   /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if s.aiProviderStore != nil { /* 判断条件并选择处理分支。 */
		persistCtx, persistCancel := context.WithTimeout(r.Context(), 5*time.Second) /* 更新 persistCancel 的值。 */
		err := s.aiProviderStore.SaveAIProviderConfig(persistCtx, candidate)         /* 更新 err 的值。 */
		persistCancel()                                                              /* 执行当前语句并推进处理流程。 */
		if err != nil {                                                              /* 判断条件并选择处理分支。 */
			if s.log != nil { /* 判断条件并选择处理分支。 */
				s.log.Error("persist AI provider config", "provider", provider, "model", modelName, "error", err) /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			problem(w, http.StatusInternalServerError, "模型服务已生效，但配置保存失败") /* 执行当前语句并推进处理流程。 */
			return                                                        /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	s.audit(r, "ai.provider.update", "ai-provider", provider, map[string]any{"model": modelName, "apiKeyConfigured": apiKey != ""}) /* 执行当前语句并推进处理流程。 */
	info := s.aiProviderRuntime.ProviderInfo()                                                                                      /* 更新 info 的值。 */
	write(w, http.StatusOK, s.aiProviderConfigView(r, candidate, info))                                                             /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func validateAIProviderURL(raw string) error { /* 定义 validateAIProviderURL 函数。 */
	u, err := url.Parse(raw)                                                                                                                /* 更新 err 的值。 */
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") { /* 判断条件并选择处理分支。 */
		return errors.New("baseUrl 必须是没有凭据、查询参数或片段的 HTTP(S) 地址") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func minInt(left, right int) int { /* 定义 minInt 函数。 */
	if left < right { /* 判断条件并选择处理分支。 */
		return left /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return right /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func validAIModelName(value string) bool { /* 定义 validAIModelName 函数。 */
	value = strings.TrimSpace(value)             /* 更新 value 的值。 */
	if value == "" || len([]rune(value)) > 128 { /* 判断条件并选择处理分支。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for index, char := range value { /* 循环处理当前数据。 */
		if index == 0 && !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')) { /* 判断条件并选择处理分支。 */
			return false /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || strings.ContainsRune("._:/-", char)) { /* 判断条件并选择处理分支。 */
			return false /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return true /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) testAIProvider(w http.ResponseWriter, r *http.Request) { /* 定义 testAIProvider 函数。 */
	var in struct { /* 声明 in。 */
		ports.AIPluginConfig        /* 执行当前语句并推进处理流程。 */
		Question             string `json:"question"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if decode(w, r, &in) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if s.engine.AIPlugins == nil { /* 判断条件并选择处理分支。 */
		problem(w, 503, "AI plugin registry is unavailable") /* 执行当前语句并推进处理流程。 */
		return                                               /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	provider := strings.ToLower(strings.TrimSpace(in.Provider))                            /* 更新 provider 的值。 */
	if provider != "ollama" && provider != "deepseek" && provider != "openai-compatible" { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusUnprocessableEntity, "模型来源必须是 ollama、deepseek 或 openai-compatible") /* 执行当前语句并推进处理流程。 */
		return                                                                                    /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	in.Provider = provider            /* 更新 in.Provider 的值。 */
	current := ports.AIPluginConfig{} /* 更新 current 的值。 */
	if s.aiProviderRuntime != nil {   /* 判断条件并选择处理分支。 */
		current = s.aiProviderRuntime.CurrentConfig() /* 更新 current 的值。 */
	} /* 结束当前表达式或代码块。 */
	// The key is intentionally redacted from GET responses. When an
	// administrator tests the already active API provider with a blank key,
	// reuse the server-side key instead of forcing it to be entered again.
	if strings.TrimSpace(in.APIKey) == "" && provider == strings.ToLower(strings.TrimSpace(current.Provider)) { /* 判断条件并选择处理分支。 */
		in.APIKey = current.APIKey /* 更新 in.APIKey 的值。 */
	} /* 结束当前表达式或代码块。 */
	if strings.TrimSpace(in.Question) == "" { /* 判断条件并选择处理分支。 */
		in.Question = "请用一句话说明你已经连接到消防物联网 AI 测试台。" /* 更新 in.Question 的值。 */
	} /* 结束当前表达式或代码块。 */
	if len([]rune(in.Question)) > 2000 { /* 判断条件并选择处理分支。 */
		problem(w, 422, "question is too long") /* 执行当前语句并推进处理流程。 */
		return                                  /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	baseURL := strings.TrimSpace(in.BaseURL) /* 更新 baseURL 的值。 */
	if baseURL == "" {                       /* 判断条件并选择处理分支。 */
		if provider == strings.ToLower(strings.TrimSpace(current.Provider)) { /* 判断条件并选择处理分支。 */
			baseURL = strings.TrimSpace(current.BaseURL) /* 更新 baseURL 的值。 */
		} /* 结束当前表达式或代码块。 */
		if baseURL == "" { /* 判断条件并选择处理分支。 */
			baseURL = map[string]string{"deepseek": "https://api.deepseek.com", "ollama": s.cfg.AITestOllamaURL}[provider] /* 更新 baseURL 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if provider == "ollama" { /* 判断条件并选择处理分支。 */
		if parsed, parseErr := url.Parse(baseURL); parseErr == nil && parsed.Path == "/v1" { /* 判断条件并选择处理分支。 */
			parsed.Path = ""                                  /* 更新 parsed.Path 的值。 */
			parsed.RawPath = ""                               /* 更新 parsed.RawPath 的值。 */
			baseURL = strings.TrimRight(parsed.String(), "/") /* 更新 baseURL 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if err := validateAIProviderURL(baseURL); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusUnprocessableEntity, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                                                  /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	in.BaseURL = baseURL                                        /* 更新 in.BaseURL 的值。 */
	client, err := s.engine.AIPlugins.Create(in.AIPluginConfig) /* 更新 err 的值。 */
	if err != nil {                                             /* 判断条件并选择处理分支。 */
		problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	info := ports.AIPluginInfo{ID: in.Provider, Model: in.Model} /* 更新 info 的值。 */
	if provider, ok := client.(ports.AIInspectable); ok {        /* 判断条件并选择处理分支。 */
		info = provider.ProviderInfo() /* 更新 info 的值。 */
	} /* 结束当前表达式或代码块。 */
	if !info.Enabled { /* 判断条件并选择处理分支。 */
		problem(w, 422, "请选择已启用的模型服务") /* 执行当前语句并推进处理流程。 */
		return                         /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	traceID := "ai_trace_" + randomHex(10)                                                                                                                                                                                                                                                                /* 更新 traceID 的值。 */
	started := time.Now()                                                                                                                                                                                                                                                                                 /* 更新 started 的值。 */
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)                                                                                                                                                                                                                                       /* 更新 cancel 的值。 */
	defer cancel()                                                                                                                                                                                                                                                                                        /* 安排函数结束时执行清理。 */
	answer, callErr := client.Chat(ctx, claims(r).TenantID, in.Question)                                                                                                                                                                                                                                  /* 更新 callErr 的值。 */
	latency := time.Since(started).Milliseconds()                                                                                                                                                                                                                                                         /* 更新 latency 的值。 */
	audit := model.AIToolCallLog{ID: traceID, TenantID: claims(r).TenantID, Actor: claims(r).Username, Tool: "ai.provider.test", Input: map[string]any{"provider": info.ID, "model": info.Model, "questionLength": len([]rune(in.Question))}, Success: callErr == nil, CreatedAt: time.Now().UnixMilli()} /* 更新 audit 的值。 */
	result := map[string]any{"traceId": traceID, "success": callErr == nil, "provider": info.ID, "providerName": info.Name, "model": info.Model, "latencyMs": latency}                                                                                                                                    /* 更新 result 的值。 */
	if callErr != nil {                                                                                                                                                                                                                                                                                   /* 判断条件并选择处理分支。 */
		errorCode, publicError := safeProviderTestError(callErr) /* 更新 publicError 的值。 */
		audit.Error = errorCode                                  /* 验证实际结果符合预期。 */
		result["errorCode"] = errorCode                          /* 执行当前语句并推进处理流程。 */
		result["error"] = publicError                            /* 执行当前语句并推进处理流程。 */
	} else { /* 结束当前表达式或代码块。 */
		audit.Output = map[string]any{"answerLength": len([]rune(answer)), "latencyMs": latency} /* 更新 audit.Output 的值。 */
		result["answer"] = answer                                                                /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	auditCtx, auditCancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Second) /* 更新 auditCancel 的值。 */
	defer auditCancel()                                                                             /* 安排函数结束时执行清理。 */
	if auditErr := s.engine.Repo.SaveAIToolCall(auditCtx, audit); auditErr != nil {                 /* 判断条件并选择处理分支。 */
		if s.log != nil { /* 判断条件并选择处理分支。 */
			s.log.Error("persist AI provider test audit", "traceId", traceID, "error", auditErr) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		problem(w, 500, "AI provider test completed but its audit trace could not be persisted") /* 执行当前语句并推进处理流程。 */
		return                                                                                   /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 200, result) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func safeProviderTestError(err error) (string, string) { /* 定义 safeProviderTestError 函数。 */
	if errors.Is(err, context.DeadlineExceeded) { /* 判断条件并选择处理分支。 */
		return "AI_PROVIDER_TIMEOUT", "模型服务请求超时，请检查服务状态后重试" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if errors.Is(err, context.Canceled) { /* 判断条件并选择处理分支。 */
		return "AI_PROVIDER_CANCELED", "模型服务请求已取消" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return "AI_PROVIDER_REQUEST_FAILED", "模型服务请求失败，请检查地址、接口密钥、模型和服务状态" /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) aiChat(w http.ResponseWriter, r *http.Request) { /* 定义 aiChat 函数。 */
	var in struct { /* 声明 in。 */
		Question       string `json:"question"`                 /* 执行当前语句并推进处理流程。 */
		Workflow       string `json:"workflow,omitempty"`       /* 执行当前语句并推进处理流程。 */
		WorkflowID     string `json:"workflowId,omitempty"`     /* 执行当前语句并推进处理流程。 */
		ConversationID string `json:"conversationId,omitempty"` /* 执行当前语句并推进处理流程。 */
		Model          string `json:"model,omitempty"`          /* 执行当前语句并推进处理流程。 */
		MaxTokens      int    `json:"maxTokens,omitempty"`      /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if decode(w, r, &in) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if s.engine.AIWorkflows != nil { /* 判断条件并选择处理分支。 */
		workflowID := in.WorkflowID /* 更新 workflowID 的值。 */
		if workflowID == "" {       /* 判断条件并选择处理分支。 */
			workflowID = in.Workflow /* 更新 workflowID 的值。 */
		} /* 结束当前表达式或代码块。 */
		result, err := s.runAIWorkflow(r.Context(), claims(r), in.Question, workflowID, in.ConversationID, in.Model, in.MaxTokens, nil) /* 检查错误并决定后续处理。 */
		if err != nil {                                                                                                                 /* 判断条件并选择处理分支。 */
			if s.log != nil { /* 判断条件并选择处理分支。 */
				s.log.Warn("run AI workflow failed", "error", err) /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			problem(w, 502, "AI workflow request failed") /* 执行当前语句并推进处理流程。 */
			return                                        /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		write(w, 200, result) /* 执行当前语句并推进处理流程。 */
		return                /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	problem(w, http.StatusServiceUnavailable, core.ErrAIWorkflowsUnavailable.Error())
} /* 结束当前表达式或代码块。 */

func (s *Server) aiWorkflows(w http.ResponseWriter, r *http.Request) { /* 定义 aiWorkflows 函数。 */
	pagination := parseListPagination(r) /* 更新 pagination 的值。 */
	// 知识库页除聊天智能体外，还需要为告警研判智能体上传文档和配置检索策略；Harness 暂不可用时仍可管理这些文档。
	forKnowledge := r.URL.Query().Get("purpose") == "knowledge"
	if s.engine.AIWorkflows == nil { /* 判断条件并选择处理分支。 */
		items := []ports.AIWorkflowPlugin{}
		if forKnowledge {
			items = append(items, alarmAnalysisWorkflowPlugin())
		}
		items, total := pageItems(items, pagination)
		writeList(w, 200, items, total, pagination, map[string]any{"configured": false, "mode": "local", "healthy": false, "healthMessage": "未配置 AI 工作流服务（Harness），智能助手问答暂不可用"}) /* 执行当前语句并推进处理流程。 */
		return                                                                                                                                                                   /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	items, err := s.engine.AIWorkflows.ListWorkflows(r.Context()) /* 更新 err 的值。 */
	if err != nil {                                               /* 判断条件并选择处理分支。 */
		if s.log != nil { /* 判断条件并选择处理分支。 */
			s.log.Warn("list AI workflows failed", "error", err) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		writeList(w, 200, []ports.AIWorkflowPlugin{}, 0, pagination, map[string]any{"configured": true, "mode": "harness", "healthy": false, "healthMessage": "AI workflow harness is unavailable"}) /* 执行当前语句并推进处理流程。 */
		return                                                                                                                                                                                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if forKnowledge {
		items = knowledgeWorkflowPlugins(items)
	} else {
		items = chatWorkflowPlugins(items) /* 更新 items 的值。 */
	}
	items, total := pageItems(items, pagination)                                                                                                                             /* 更新 total 的值。 */
	writeList(w, 200, items, total, pagination, map[string]any{"configured": true, "mode": "harness", "healthy": true, "healthMessage": "AI workflow harness is reachable"}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func alarmAnalysisWorkflowPlugin() ports.AIWorkflowPlugin {
	return ports.AIWorkflowPlugin{ID: model.AlarmAnalysisWorkflowID, Name: "AI 告警研判", Description: "告警详情中的智能研判；仅有知识库权限的角色手动研判时检索本智能体的文档。", Enabled: true, KnowledgeEnabled: true}
}

// knowledgeWorkflowPlugins lists the Agents that own knowledge documents: chat
// Agents plus the alarm analysis Agent.
func knowledgeWorkflowPlugins(items []ports.AIWorkflowPlugin) []ports.AIWorkflowPlugin {
	visible := chatWorkflowPlugins(items)
	for _, item := range items {
		if item.ID == model.AlarmAnalysisWorkflowID {
			return append(visible, item)
		}
	}
	return append(visible, alarmAnalysisWorkflowPlugin())
}

func (s *Server) aiWorkflowManifests(w http.ResponseWriter, r *http.Request) { /* 定义 aiWorkflowManifests 函数。 */
	pagination := parseListPagination(r)                               /* 更新 pagination 的值。 */
	manager, ok := s.engine.AIWorkflows.(ports.AIWorkflowAdminManager) /* 更新 ok 的值。 */
	if !ok {                                                           /* 判断条件并选择处理分支。 */
		problem(w, http.StatusServiceUnavailable, "AI workflow harness does not support plugin management") /* 执行当前语句并推进处理流程。 */
		return                                                                                              /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	items, err := manager.ListWorkflowManifests(r.Context()) /* 更新 err 的值。 */
	if err != nil {                                          /* 判断条件并选择处理分支。 */
		if s.log != nil { /* 判断条件并选择处理分支。 */
			s.log.Warn("list AI workflow manifests failed", "error", err) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		problem(w, http.StatusBadGateway, "AI workflow harness plugin catalog is unavailable") /* 执行当前语句并推进处理流程。 */
		return                                                                                 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	items = chatWorkflowManifests(items)                                  /* 更新 items 的值。 */
	items, total := pageItems(items, pagination)                          /* 更新 total 的值。 */
	writeList(w, http.StatusOK, items, total, pagination, map[string]any{ /* 执行当前语句并推进处理流程。 */
		"configured":    true,                                              /* 执行当前语句并推进处理流程。 */
		"mode":          "harness",                                         /* 执行当前语句并推进处理流程。 */
		"healthy":       true,                                              /* 执行当前语句并推进处理流程。 */
		"healthMessage": "AI workflow harness plugin catalog is reachable", /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) saveAIWorkflow(w http.ResponseWriter, r *http.Request) { /* 定义 saveAIWorkflow 函数。 */
	manager, ok := s.engine.AIWorkflows.(ports.AIWorkflowManager) /* 更新 ok 的值。 */
	if !ok {                                                      /* 判断条件并选择处理分支。 */
		problem(w, http.StatusServiceUnavailable, "AI workflow harness does not support dynamic agents") /* 执行当前语句并推进处理流程。 */
		return                                                                                           /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var manifest ports.AIWorkflowManifest /* 声明 manifest。 */
	if decode(w, r, &manifest) != nil {   /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err := validateAIWorkflowManifest(manifest); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusUnprocessableEntity, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                                                  /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	plugin, err := manager.SaveWorkflow(r.Context(), manifest) /* 更新 err 的值。 */
	if err != nil {                                            /* 判断条件并选择处理分支。 */
		if s.log != nil { /* 判断条件并选择处理分支。 */
			s.log.Warn("save dynamic AI workflow failed", "workflow", manifest.ID, "error", err) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		problem(w, http.StatusBadGateway, "AI workflow harness rejected the agent manifest") /* 执行当前语句并推进处理流程。 */
		return                                                                               /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.audit(r, "ai.workflow.agent.save", "ai-workflow", plugin.ID, map[string]any{"name": plugin.Name, "version": plugin.Version, "enabled": plugin.Enabled, "capabilities": len(plugin.Capabilities), "knowledgeEnabled": plugin.KnowledgeEnabled}) /* 执行当前语句并推进处理流程。 */
	write(w, http.StatusCreated, plugin)                                                                                                                                                                                                             /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) updateAIWorkflow(w http.ResponseWriter, r *http.Request) { /* 定义 updateAIWorkflow 函数。 */
	manager, ok := s.engine.AIWorkflows.(ports.AIWorkflowManager) /* 更新 ok 的值。 */
	if !ok {                                                      /* 判断条件并选择处理分支。 */
		problem(w, http.StatusServiceUnavailable, "AI workflow harness does not support dynamic agents") /* 执行当前语句并推进处理流程。 */
		return                                                                                           /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var manifest ports.AIWorkflowManifest /* 声明 manifest。 */
	if decode(w, r, &manifest) != nil {   /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	workflowID := strings.TrimSpace(r.PathValue("id")) /* 更新 workflowID 的值。 */
	if workflowID == "" || workflowID != manifest.ID { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusUnprocessableEntity, "workflow path id must match the manifest id") /* 执行当前语句并推进处理流程。 */
		return                                                                                    /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err := validateAIWorkflowManifest(manifest); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusUnprocessableEntity, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                                                  /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if catalog, supportsCatalog := s.engine.AIWorkflows.(ports.AIWorkflowAdminManager); supportsCatalog { /* 判断条件并选择处理分支。 */
		items, err := catalog.ListWorkflowManifests(r.Context()) /* 更新 err 的值。 */
		if err != nil {                                          /* 判断条件并选择处理分支。 */
			if s.log != nil { /* 判断条件并选择处理分支。 */
				s.log.Warn("check AI workflow before update failed", "workflow", manifest.ID, "error", err) /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			problem(w, http.StatusBadGateway, "AI workflow harness plugin catalog is unavailable") /* 执行当前语句并推进处理流程。 */
			return                                                                                 /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		found := false               /* 更新 found 的值。 */
		for _, item := range items { /* 循环处理当前数据。 */
			if item.ID == manifest.ID { /* 判断条件并选择处理分支。 */
				found = true /* 更新 found 的值。 */
				break        /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if !found { /* 判断条件并选择处理分支。 */
			problem(w, http.StatusNotFound, "workflow plugin was not found") /* 执行当前语句并推进处理流程。 */
			return                                                           /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	plugin, err := manager.SaveWorkflow(r.Context(), manifest) /* 更新 err 的值。 */
	if err != nil {                                            /* 判断条件并选择处理分支。 */
		if s.log != nil { /* 判断条件并选择处理分支。 */
			s.log.Warn("update dynamic AI workflow failed", "workflow", manifest.ID, "error", err) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		problem(w, http.StatusBadGateway, "AI workflow harness rejected the agent manifest") /* 执行当前语句并推进处理流程。 */
		return                                                                               /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.audit(r, "ai.workflow.agent.update", "ai-workflow", plugin.ID, map[string]any{"name": plugin.Name, "version": plugin.Version, "enabled": plugin.Enabled, "capabilities": len(plugin.Capabilities), "knowledgeEnabled": plugin.KnowledgeEnabled}) /* 执行当前语句并推进处理流程。 */
	write(w, http.StatusOK, plugin)                                                                                                                                                                                                                    /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) deleteAIWorkflow(w http.ResponseWriter, r *http.Request) { /* 定义 deleteAIWorkflow 函数。 */
	manager, ok := s.engine.AIWorkflows.(ports.AIWorkflowAdminManager) /* 更新 ok 的值。 */
	if !ok {                                                           /* 判断条件并选择处理分支。 */
		problem(w, http.StatusServiceUnavailable, "AI workflow harness does not support plugin management") /* 执行当前语句并推进处理流程。 */
		return                                                                                              /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	workflowID := strings.TrimSpace(r.PathValue("id")) /* 更新 workflowID 的值。 */
	if !validWorkflowIdentifier(workflowID) {          /* 判断条件并选择处理分支。 */
		problem(w, http.StatusUnprocessableEntity, "workflow id has an invalid format") /* 执行当前语句并推进处理流程。 */
		return                                                                          /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if oneOf(workflowID, "alarm-handler", "ops-assistant", "system-observer", "device-health-inspector", "protocol-assistant", "rule-drafter") { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusConflict, "built-in Agent ids cannot be deleted") /* 执行当前语句并推进处理流程。 */
		return                                                                  /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err := manager.DeleteWorkflow(r.Context(), workflowID); err != nil { /* 判断条件并选择处理分支。 */
		if s.log != nil { /* 判断条件并选择处理分支。 */
			s.log.Warn("delete dynamic AI workflow failed", "workflow", workflowID, "error", err) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		problem(w, http.StatusBadGateway, "AI workflow harness rejected the delete request") /* 执行当前语句并推进处理流程。 */
		return                                                                               /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.audit(r, "ai.workflow.agent.delete", "ai-workflow", workflowID, nil)     /* 执行当前语句并推进处理流程。 */
	write(w, http.StatusOK, map[string]any{"deleted": true, "id": workflowID}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func validateAIWorkflowManifest(manifest ports.AIWorkflowManifest) error { /* 定义 validateAIWorkflowManifest 函数。 */
	if manifest.SchemaVersion != 1 || !validWorkflowIdentifier(manifest.ID) { /* 判断条件并选择处理分支。 */
		return errors.New("schemaVersion must be 1 and id must contain only letters, numbers, dot, underscore, colon or hyphen") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if oneOf(manifest.ID, "alarm-handler", "ops-assistant", "system-observer", "device-health-inspector", "protocol-assistant", "rule-drafter") { /* 判断条件并选择处理分支。 */
		return errors.New("built-in Agent ids cannot be overwritten") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !boundedText(manifest.Name, 128) || !boundedText(manifest.Description, 1024) || !boundedText(manifest.Version, 64) || !boundedText(manifest.Persona, 16384) || !validWorkflowModel(manifest.DefaultModel) { /* 判断条件并选择处理分支。 */
		return errors.New("name, description, version, persona and defaultModel are required and exceed no field limits") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if manifest.MaxTokens < 1 || manifest.MaxTokens > 8192 || len(manifest.Capabilities) < 1 || len(manifest.Capabilities) > 32 || len(manifest.AllowedTools) < 1 || len(manifest.AllowedTools) > 6 { /* 判断条件并选择处理分支。 */
		return errors.New("maxTokens must be 1..8192 and capabilities/allowedTools must be non-empty") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	allowed := map[string]struct{}{ /* 更新 allowed 的值。 */
		"mcp__iot__query_system_overview": {}, "mcp__iot__query_device_latest": {}, "mcp__iot__query_alarm_list": {}, /* 执行当前语句并推进处理流程。 */
		"mcp__iot__query_property_history": {}, "mcp__iot__query_similar_alarms": {}, "mcp__iot__query_knowledge_base": {}, /* 执行当前语句并推进处理流程。 */
		"mcp__iot__create_rule_draft": {}, /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	seen := map[string]struct{}{}                      /* 更新 seen 的值。 */
	capabilities := map[string]struct{}{}              /* 更新 capabilities 的值。 */
	for _, capability := range manifest.Capabilities { /* 循环处理当前数据。 */
		if !boundedText(capability, 64) { /* 判断条件并选择处理分支。 */
			return errors.New("each capability must contain 1..64 characters") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if _, duplicate := capabilities[capability]; duplicate { /* 判断条件并选择处理分支。 */
			return fmt.Errorf("duplicate capability %q", capability) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		capabilities[capability] = struct{}{} /* 更新 capabilities[capability] 的值。 */
	} /* 结束当前表达式或代码块。 */
	for _, tool := range manifest.AllowedTools { /* 循环处理当前数据。 */
		if _, ok := allowed[tool]; !ok { /* 判断条件并选择处理分支。 */
			return fmt.Errorf("tool %q is outside the read-only Agent whitelist", tool) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if _, duplicate := seen[tool]; duplicate { /* 判断条件并选择处理分支。 */
			return fmt.Errorf("duplicate tool %q", tool) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		seen[tool] = struct{}{} /* 更新 seen[tool] 的值。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func validWorkflowIdentifier(value string) bool { /* 定义 validWorkflowIdentifier 函数。 */
	if value == "" || len(value) > 128 { /* 判断条件并选择处理分支。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for index, char := range value { /* 循环处理当前数据。 */
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || index > 0 && strings.ContainsRune("._:-", char) { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return true /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func validWorkflowModel(value string) bool { /* 定义 validWorkflowModel 函数。 */
	if value == "" || len(value) > 128 { /* 判断条件并选择处理分支。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for index, char := range value { /* 循环处理当前数据。 */
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || index > 0 && strings.ContainsRune("._:/-", char) { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return true /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func boundedText(value string, maximum int) bool { /* 定义 boundedText 函数。 */
	length := len([]rune(strings.TrimSpace(value))) /* 更新 length 的值。 */
	return length > 0 && length <= maximum          /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func defaultWorkflowKnowledgeBinding(tenantID, workflowID string) model.WorkflowKnowledgeBinding { /* 定义 defaultWorkflowKnowledgeBinding 函数。 */
	return core.DefaultWorkflowKnowledgeBinding(tenantID, workflowID)
} /* 结束当前表达式或代码块。 */

func (s *Server) workflowKnowledgeBinding(w http.ResponseWriter, r *http.Request) { /* 定义 workflowKnowledgeBinding 函数。 */
	c := claims(r)                                     /* 更新 c 的值。 */
	workflowID := strings.TrimSpace(r.PathValue("id")) /* 更新 workflowID 的值。 */
	if workflowID == "" || len(workflowID) > 128 {     /* 判断条件并选择处理分支。 */
		problem(w, 422, "valid workflow id is required") /* 执行当前语句并推进处理流程。 */
		return                                           /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if r.Method == http.MethodGet { /* 判断条件并选择处理分支。 */
		binding, err := s.engine.Repo.GetWorkflowKnowledgeBinding(r.Context(), c.TenantID, workflowID) /* 更新 err 的值。 */
		if err != nil {                                                                                /* 判断条件并选择处理分支。 */
			problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
			return                       /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if binding.WorkflowID == "" { /* 判断条件并选择处理分支。 */
			binding = defaultWorkflowKnowledgeBinding(c.TenantID, workflowID) /* 更新 binding 的值。 */
		} /* 结束当前表达式或代码块。 */
		write(w, 200, binding) /* 执行当前语句并推进处理流程。 */
		return                 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var in struct { /* 声明 in。 */
		RetrievalMode string  `json:"retrievalMode"` /* 执行当前语句并推进处理流程。 */
		TopK          int     `json:"topK"`          /* 执行当前语句并推进处理流程。 */
		MinScore      float64 `json:"minScore"`      /* 执行当前语句并推进处理流程。 */
		NoMatchPolicy string  `json:"noMatchPolicy"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if decode(w, r, &in) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !oneOf(in.RetrievalMode, "auto", "always", "disabled") || !oneOf(in.NoMatchPolicy, "allow-model", "require-evidence") || in.TopK < 1 || in.TopK > 20 || in.MinScore < 0 || in.MinScore > 1 || in.RetrievalMode == "disabled" && in.NoMatchPolicy == "require-evidence" { /* 判断条件并选择处理分支。 */
		problem(w, 422, "invalid knowledge binding policy") /* 执行当前语句并推进处理流程。 */
		return                                              /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	binding := model.WorkflowKnowledgeBinding{ /* 更新 binding 的值。 */
		TenantID: c.TenantID, WorkflowID: workflowID, /* 执行当前语句并推进处理流程。 */
		// Knowledge documents are directly associated with a workflow/Agent;
		// this binding stores only retrieval policy, not another filter layer.
		ProductIDs: nil, Categories: nil, Tags: nil, /* 执行当前语句并推进处理流程。 */
		RetrievalMode: in.RetrievalMode, TopK: in.TopK, MinScore: in.MinScore, NoMatchPolicy: in.NoMatchPolicy, UpdatedAt: time.Now().UnixMilli(), /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if err := s.engine.Repo.SaveWorkflowKnowledgeBinding(r.Context(), binding); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.audit(r, "ai.workflow.knowledge-binding.save", "ai-workflow", workflowID, map[string]any{"retrievalMode": binding.RetrievalMode, "topK": binding.TopK}) /* 执行当前语句并推进处理流程。 */
	write(w, 200, binding)                                                                                                                                    /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) aiChatStream(w http.ResponseWriter, r *http.Request) { /* 定义 aiChatStream 函数。 */
	var in struct { /* 声明 in。 */
		Question       string `json:"question"`                 /* 执行当前语句并推进处理流程。 */
		Workflow       string `json:"workflow,omitempty"`       /* 执行当前语句并推进处理流程。 */
		WorkflowID     string `json:"workflowId,omitempty"`     /* 执行当前语句并推进处理流程。 */
		ConversationID string `json:"conversationId,omitempty"` /* 执行当前语句并推进处理流程。 */
		Model          string `json:"model,omitempty"`          /* 执行当前语句并推进处理流程。 */
		MaxTokens      int    `json:"maxTokens,omitempty"`      /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if decode(w, r, &in) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if s.engine.AIWorkflows == nil { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusServiceUnavailable, "未配置 AI 工作流服务（Harness），智能助手问答暂不可用") /* 执行当前语句并推进处理流程。 */
		return                                                                        /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	flusher, ok := w.(http.Flusher) /* 更新 ok 的值。 */
	if !ok {                        /* 判断条件并选择处理分支。 */
		problem(w, http.StatusInternalServerError, "streaming is not supported") /* 执行当前语句并推进处理流程。 */
		return                                                                   /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8") /* 执行当前语句并推进处理流程。 */
	w.Header().Set("Cache-Control", "no-cache, no-transform")          /* 执行当前语句并推进处理流程。 */
	w.Header().Set("Connection", "keep-alive")                         /* 执行当前语句并推进处理流程。 */
	w.Header().Set("X-Accel-Buffering", "no")                          /* 执行当前语句并推进处理流程。 */
	w.WriteHeader(http.StatusOK)                                       /* 执行当前语句并推进处理流程。 */
	flusher.Flush()                                                    /* 执行当前语句并推进处理流程。 */

	workflowID := in.WorkflowID /* 更新 workflowID 的值。 */
	if workflowID == "" {       /* 判断条件并选择处理分支。 */
		workflowID = in.Workflow /* 更新 workflowID 的值。 */
	} /* 结束当前表达式或代码块。 */
	terminal := false                                                                                                                                                    /* 更新 terminal 的值。 */
	result, err := s.runAIWorkflow(r.Context(), claims(r), in.Question, workflowID, in.ConversationID, in.Model, in.MaxTokens, func(event ports.AIWorkflowEvent) error { /* 更新 err 的值。 */
		if event.Type == "run.completed" || event.Type == "run.failed" { /* 判断条件并选择处理分支。 */
			terminal = true /* 更新 terminal 的值。 */
		} /* 结束当前表达式或代码块。 */
		event = sanitizeWorkflowEvent(event)               /* 更新 event 的值。 */
		if err := writeWorkflowSSE(w, event); err != nil { /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		flusher.Flush()          /* 执行当前语句并推进处理流程。 */
		return r.Context().Err() /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		if r.Context().Err() == nil && !terminal { /* 判断条件并选择处理分支。 */
			_ = writeWorkflowSSE(w, ports.AIWorkflowEvent{Type: "run.failed", RunID: result.RunID, Code: "workflow_failed", Message: "AI workflow request failed"}) /* 更新 _ 的值。 */
			flusher.Flush()                                                                                                                                         /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !terminal { /* 判断条件并选择处理分支。 */
		_ = writeWorkflowSSE(w, ports.AIWorkflowEvent{Type: "run.completed", RunID: result.RunID, WorkflowID: result.WorkflowID, Model: result.Model, Answer: result.Answer}) /* 更新 _ 的值。 */
		flusher.Flush()                                                                                                                                                       /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) runAIWorkflow(ctx context.Context, c auth.Claims, question, workflowID, conversationID, modelName string, maxTokens int, emit func(ports.AIWorkflowEvent) error) (ports.AIWorkflowResult, error) { /* 定义 runAIWorkflow 函数。 */
	question = strings.TrimSpace(question) /* 更新 question 的值。 */
	if question == "" {                    /* 判断条件并选择处理分支。 */
		return ports.AIWorkflowResult{}, errors.New("question is required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(question) > 8000 { /* 判断条件并选择处理分支。 */
		return ports.AIWorkflowResult{}, errors.New("question exceeds 8000 bytes") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if s.aiProviderRuntime != nil { /* 判断条件并选择处理分支。 */
		// The selected Provider owns the model for every AI surface. Keep the
		// browser's run form from sending a stale per-workflow model override.
		config := s.aiProviderRuntime.CurrentConfig()
		if configuredModel := strings.TrimSpace(config.Model); configuredModel != "" { /* 判断条件并选择处理分支。 */
			modelName = configuredModel /* 更新 modelName 的值。 */
		} /* 结束当前表达式或代码块。 */
		if maxTokens <= 0 {
			maxTokens = effectiveAIMaxTokens(config.MaxTokens)
		}
	} /* 结束当前表达式或代码块。 */
	knowledgeQuestion := question      /* 更新 knowledgeQuestion 的值。 */
	runID := "ai_run_" + randomHex(10) /* 更新 runID 的值。 */
	if conversationID == "" {          /* 判断条件并选择处理分支。 */
		conversationID = runID /* 更新 conversationID 的值。 */
	} /* 结束当前表达式或代码块。 */
	if c.TokenUse == "user" {
		conversationID += "\x00" + requestAccessVersion(ctx, c)
	}
	conversationID = harnessConversationID(c.TenantID, c.Username, conversationID) /* 更新 conversationID 的值。 */
	if maxTokens <= 0 {                                                            /* 判断条件并选择处理分支。 */
		maxTokens = 2048 /* 更新 maxTokens 的值。 */
	} /* 结束当前表达式或代码块。 */
	if maxTokens > 8192 { /* 判断条件并选择处理分支。 */
		maxTokens = 8192 /* 更新 maxTokens 的值。 */
	} /* 结束当前表达式或代码块。 */
	binding, err := s.engine.Repo.GetWorkflowKnowledgeBinding(ctx, c.TenantID, strings.TrimSpace(workflowID)) /* 更新 err 的值。 */
	if err != nil {                                                                                           /* 判断条件并选择处理分支。 */
		return ports.AIWorkflowResult{RunID: runID}, fmt.Errorf("load workflow knowledge binding: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if binding.WorkflowID == "" { /* 判断条件并选择处理分支。 */
		binding = defaultWorkflowKnowledgeBinding(c.TenantID, strings.TrimSpace(workflowID)) /* 更新 binding 的值。 */
	} /* 结束当前表达式或代码块。 */
	scopes := workflowScopes(ctx) /* 更新 scopes 的值。 */
	if len(intersectScopes(scopes, []string{auth.ScopeQueryKnowledgeBase})) == 0 {
		if binding.RetrievalMode == "always" || binding.NoMatchPolicy == "require-evidence" {
			return ports.AIWorkflowResult{RunID: runID, WorkflowID: workflowID}, errors.New("当前用户无此工作流所需的知识库访问权限")
		}
		binding.RetrievalMode = "disabled"
	}
	var knowledgeScope *auth.KnowledgeScope  /* 声明 knowledgeScope。 */
	if binding.RetrievalMode == "disabled" { /* 判断条件并选择处理分支。 */
		filteredScopes := make([]string, 0, len(scopes)) /* 更新 filteredScopes 的值。 */
		for _, scope := range scopes {                   /* 循环处理当前数据。 */
			if scope != auth.ScopeQueryKnowledgeBase { /* 判断条件并选择处理分支。 */
				filteredScopes = append(filteredScopes, scope) /* 更新 filteredScopes 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		scopes = filteredScopes                          /* 更新 scopes 的值。 */
		question += "\n\n[平台知识策略] 此工作流已禁用知识库，不得调用知识库工具。" /* 更新 question 的值。 */
	} else { /* 结束当前表达式或代码块。 */
		knowledgeScope = &auth.KnowledgeScope{WorkflowID: binding.WorkflowID, TopK: binding.TopK, MinScore: binding.MinScore} /* 更新 knowledgeScope 的值。 */
		question += workflowKnowledgeInstruction(binding)                                                                     /* 更新 question 的值。 */
	} /* 结束当前表达式或代码块。 */
	if (binding.RetrievalMode == "always" || binding.NoMatchPolicy == "require-evidence") && s.engine.KB == nil { /* 判断条件并选择处理分支。 */
		return ports.AIWorkflowResult{RunID: runID, WorkflowID: workflowID}, errors.New("workflow knowledge base is unavailable") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if (binding.RetrievalMode == "always" || binding.NoMatchPolicy == "require-evidence") && s.engine.KB != nil { /* 判断条件并选择处理分支。 */
		callID := "knowledge_prefetch_" + randomHex(6) /* 更新 callID 的值。 */
		if emit != nil {                               /* 判断条件并选择处理分支。 */
			_ = emit(ports.AIWorkflowEvent{Type: "tool.started", RunID: runID, WorkflowID: workflowID, Tool: "query_knowledge_base", CallID: callID, Data: map[string]any{"inputSummary": "按工作流绑定策略预检知识库"}}) /* 更新 _ 的值。 */
		} /* 结束当前表达式或代码块。 */
		hits, searchErr := s.searchWorkflowKnowledge(ctx, c.TenantID, knowledgeQuestion, binding) /* 更新 searchErr 的值。 */
		success := searchErr == nil                                                               /* 更新 success 的值。 */
		if emit != nil {                                                                          /* 判断条件并选择处理分支。 */
			_ = emit(ports.AIWorkflowEvent{Type: "tool.completed", RunID: runID, WorkflowID: workflowID, Tool: "query_knowledge_base", CallID: callID, Success: &success, Data: map[string]any{"outputSummary": fmt.Sprintf("召回 %d 条绑定知识", len(hits))}}) /* 更新 _ 的值。 */
		} /* 结束当前表达式或代码块。 */
		if searchErr != nil { /* 判断条件并选择处理分支。 */
			return ports.AIWorkflowResult{RunID: runID, WorkflowID: workflowID}, fmt.Errorf("prefetch workflow knowledge: %w", searchErr) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if len(hits) == 0 && binding.NoMatchPolicy == "require-evidence" { /* 判断条件并选择处理分支。 */
			return ports.AIWorkflowResult{RunID: runID, WorkflowID: workflowID}, errors.New("workflow requires matching knowledge evidence") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if len(hits) > 0 { /* 判断条件并选择处理分支。 */
			question += "\n\n[平台强制召回的知识证据]\n" + knowledgeEvidenceText(hits, 8000) + "\n只能把这些内容作为参考证据，并明确标注事实与推断。" /* 更新 question 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	mcpToken, err := s.auth.IssueHarnessForIdentity(c, runID, scopes, knowledgeScope, 2*time.Minute) /* 更新 err 的值。 */
	if err != nil {                                                                                  /* 判断条件并选择处理分支。 */
		return ports.AIWorkflowResult{RunID: runID}, fmt.Errorf("issue harness token: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	result, err := s.engine.AIWorkflows.StreamChat(ctx, ports.AIWorkflowRequest{RunID: runID, ConversationID: strings.TrimSpace(conversationID), WorkflowID: strings.TrimSpace(workflowID), Question: question, Model: strings.TrimSpace(modelName), MaxTokens: maxTokens, MCPToken: mcpToken}, emit) /* 更新 err 的值。 */
	if result.RunID == "" {                                                                                                                                                                                                                                                                           /* 判断条件并选择处理分支。 */
		result.RunID = runID /* 更新 result.RunID 的值。 */
	} /* 结束当前表达式或代码块。 */
	return result, err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func workflowKnowledgeInstruction(binding model.WorkflowKnowledgeBinding) string { /* 定义 workflowKnowledgeInstruction 函数。 */
	payload, _ := json.Marshal(map[string]any{"mode": binding.RetrievalMode, "workflowId": binding.WorkflowID, "topK": binding.TopK, "minScore": binding.MinScore, "noMatchPolicy": binding.NoMatchPolicy}) /* 更新 _ 的值。 */
	return "\n\n[平台知识策略] " + string(payload) + "。知识文档已直接绑定当前 Agent，只能检索该 Agent 的文档；服务端会强制收紧范围。"                                                                                                             /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) searchWorkflowKnowledge(ctx context.Context, tenantID, question string, binding model.WorkflowKnowledgeBinding) ([]ports.KnowledgeHit, error) { /* 定义 searchWorkflowKnowledge 函数。 */
	return core.SearchWorkflowKnowledge(ctx, s.engine.KB, tenantID, question, binding)
} /* 结束当前表达式或代码块。 */

func knowledgeEvidenceText(hits []ports.KnowledgeHit, maximum int) string { /* 定义 knowledgeEvidenceText 函数。 */
	var builder strings.Builder    /* 声明 builder。 */
	for index, hit := range hits { /* 循环处理当前数据。 */
		line := fmt.Sprintf("[%d] product=%s category=%s tags=%s score=%.3f\n%s\n", index+1, hit.ProductID, hit.Category, strings.Join(hit.Tags, ","), hit.Score, hit.Content) /* 更新 line 的值。 */
		if builder.Len()+len(line) > maximum {                                                                                                                                 /* 判断条件并选择处理分支。 */
			break /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		builder.WriteString(line) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return builder.String() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func harnessConversationID(tenantID, username, conversationID string) string { /* 定义 harnessConversationID 函数。 */
	sum := sha256.Sum256([]byte(tenantID + "\x00" + username + "\x00" + conversationID)) /* 更新 sum 的值。 */
	return "conv_" + hex.EncodeToString(sum[:])                                          /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func sanitizeWorkflowEvent(event ports.AIWorkflowEvent) ports.AIWorkflowEvent { /* 定义 sanitizeWorkflowEvent 函数。 */
	event.Data = sanitizeWorkflowData(event.Data) /* 更新 event.Data 的值。 */
	return event                                  /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func sanitizeWorkflowData(data map[string]any) map[string]any { /* 定义 sanitizeWorkflowData 函数。 */
	if data == nil { /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	out := make(map[string]any, len(data)) /* 更新 out 的值。 */
	for key, value := range data {         /* 循环处理当前数据。 */
		canonicalKey := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(key))                                                  /* 更新 canonicalKey 的值。 */
		if canonicalKey == "conversationid" || canonicalKey == "sessionid" || canonicalKey == "authorization" || canonicalKey == "apikey" || /* 判断条件并选择处理分支。 */
			strings.HasSuffix(canonicalKey, "token") || strings.Contains(canonicalKey, "secret") || strings.Contains(canonicalKey, "password") || /* 执行当前语句并推进处理流程。 */
			strings.Contains(canonicalKey, "credential") || strings.Contains(canonicalKey, "cookie") { /* 执行当前语句并推进处理流程。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		out[key] = sanitizeWorkflowValue(value) /* 更新 out[key] 的值。 */
	} /* 结束当前表达式或代码块。 */
	if len(out) == 0 { /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return out /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func sanitizeWorkflowValue(value any) any { /* 定义 sanitizeWorkflowValue 函数。 */
	switch typed := value.(type) { /* 根据条件选择处理路径。 */
	case map[string]any: /* 处理当前分支。 */
		return sanitizeWorkflowData(typed) /* 返回当前处理结果。 */
	case []any: /* 处理当前分支。 */
		items := make([]any, len(typed)) /* 更新 items 的值。 */
		for i, item := range typed {     /* 循环处理当前数据。 */
			items[i] = sanitizeWorkflowValue(item) /* 更新 items[i] 的值。 */
		} /* 结束当前表达式或代码块。 */
		return items /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return value /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func writeWorkflowSSE(w io.Writer, event ports.AIWorkflowEvent) error { /* 定义 writeWorkflowSSE 函数。 */
	if !allowedWorkflowEvent(event.Type) { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("unsupported workflow event type %q", event.Type) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	b, err := json.Marshal(event) /* 更新 err 的值。 */
	if err != nil {               /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(b) > 64<<10 { /* 判断条件并选择处理分支。 */
		return errors.New("workflow event exceeds 64 KiB") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, b) /* 更新 err 的值。 */
	return err                                                        /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func allowedWorkflowEvent(eventType string) bool { /* 定义 allowedWorkflowEvent 函数。 */
	switch eventType { /* 根据条件选择处理路径。 */
	case "run.started", "text.delta", "tool.started", "tool.completed", "run.completed", "run.failed": /* 处理当前分支。 */
		return true /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) aiRuleDraft(w http.ResponseWriter, r *http.Request) { /* 定义 aiRuleDraft 函数。 */
	var in struct { /* 声明 in。 */
		Text string `json:"text"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if decode(w, r, &in) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rule, err := s.engine.DraftRule(aiRunContext(r.Context(), claims(r)), claims(r).TenantID, in.Text) /* 由 rule-drafter 工作流生成。 */
	if err != nil {                                                                                    /* 判断条件并选择处理分支。 */
		problem(w, 502, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	c := claims(r)             /* 更新 c 的值。 */
	rule.TenantID = c.TenantID /* 更新 rule.TenantID 的值。 */
	rule.Enabled = false       /* 更新 rule.Enabled 的值。 */
	// AI drafts always start from the executable JSON condition form. A model
	// response must not smuggle an already-active Gengine expression into the
	// editor; the generated alternative is shown as a commented placeholder.
	rule.Expression = "" /* 更新 rule.Expression 的值。 */
	if rule.ID == "" {   /* 判断条件并选择处理分支。 */
		rule.ID = "rule_draft_" + randomHex(8) /* 更新 rule.ID 的值。 */
	} /* 结束当前表达式或代码块。 */
	if rule.Match == "" { /* 判断条件并选择处理分支。 */
		rule.Match = "all" /* 更新 rule.Match 的值。 */
	} /* 结束当前表达式或代码块。 */
	if rule.Version == 0 { /* 判断条件并选择处理分支。 */
		rule.Version = 1 /* 更新 rule.Version 的值。 */
	} /* 结束当前表达式或代码块。 */
	if rule.Level == "" { /* 判断条件并选择处理分支。 */
		rule.Level = "MEDIUM" /* 更新 rule.Level 的值。 */
	} /* 结束当前表达式或代码块。 */
	warnings, conflicts, validationErr := s.engine.ValidateRuleDraft(r.Context(), rule) /* 更新 validationErr 的值。 */
	if validationErr != nil {                                                           /* 判断条件并选择处理分支。 */
		_ = s.engine.Repo.SaveAIToolCall(r.Context(), model.AIToolCallLog{ID: "tool_" + randomHex(8), TenantID: c.TenantID, Actor: c.Username, Tool: "ai.rule_draft.validate", Input: map[string]any{"text": in.Text}, Output: rule, Success: false, Error: validationErr.Error(), CreatedAt: time.Now().UnixMilli()}) /* 更新 _ 的值。 */
		problem(w, 422, validationErr.Error())                                                                                                                                                                                                                                                                         /* 执行当前语句并推进处理流程。 */
		return                                                                                                                                                                                                                                                                                                         /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	presentation, presentationErr := core.PresentRule(rule) /* 更新 presentationErr 的值。 */
	if presentationErr != nil {                             /* 判断条件并选择处理分支。 */
		problem(w, http.StatusInternalServerError, presentationErr.Error()) /* 执行当前语句并推进处理流程。 */
		return                                                              /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	_ = s.engine.Repo.SaveAudit(r.Context(), model.AuditLog{ID: fmt.Sprintf("audit_%d", time.Now().UnixNano()), TenantID: c.TenantID, Actor: c.Username, Action: "ai.rule_draft", TargetType: "rule", TargetID: rule.ID, Details: map[string]any{"success": true}, CreatedAt: time.Now().UnixMilli()}) /* 更新 _ 的值。 */
	_ = s.engine.Repo.SaveAIToolCall(r.Context(), model.AIToolCallLog{ID: "tool_" + randomHex(8), TenantID: c.TenantID, Actor: c.Username, Tool: "ai.rule_draft", Input: map[string]any{"text": in.Text}, Output: rule, Success: true, CreatedAt: time.Now().UnixMilli()})                             /* 更新 _ 的值。 */
	write(w, 200, map[string]any{"draft": rule, "presentation": presentation, "requiresHumanApproval": true, "schemaValid": true, "warnings": warnings, "conflicts": conflicts})                                                                                                                       /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) aiReport(w http.ResponseWriter, r *http.Request) { /* 定义 aiReport 函数。 */
	var in struct { /* 声明 in。 */
		Period string `json:"period"` /* 执行当前语句并推进处理流程。 */
		Start  int64  `json:"start"`  /* 执行当前语句并推进处理流程。 */
		End    int64  `json:"end"`    /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if decode(w, r, &in) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if in.Period == "" { /* 判断条件并选择处理分支。 */
		in.Period = "日报" /* 更新 in.Period 的值。 */
	} /* 结束当前表达式或代码块。 */
	report, err := s.engine.GenerateReport(aiRunContext(r.Context(), claims(r)), claims(r).TenantID, in.Period, in.Start, in.End) /* 更新 err 的值。 */
	if err != nil {                                                                                                               /* 判断条件并选择处理分支。 */
		problem(w, 502, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 200, map[string]any{"period": in.Period, "start": in.Start, "end": in.End, "report": report}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) knowledgeDocs(w http.ResponseWriter, r *http.Request) { /* 定义 knowledgeDocs 函数。 */
	pagination := parseListPagination(r)                                                                                              /* 更新 pagination 的值。 */
	items, total, err := s.engine.Repo.ListKnowledgeDocsPage(r.Context(), claims(r).TenantID, pagination.PageSize, pagination.Offset) /* 更新 err 的值。 */
	if err != nil {                                                                                                                   /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	persistent := strings.TrimSpace(s.cfg.WeaviateURL) != "" /* 更新 persistent 的值。 */
	indexMode := "local-memory"                              /* 更新 indexMode 的值。 */
	if persistent {                                          /* 判断条件并选择处理分支。 */
		indexMode = "weaviate" /* 更新 indexMode 的值。 */
	} /* 结束当前表达式或代码块。 */
	writeList(w, 200, items, total, pagination, map[string]any{"indexMode": indexMode, "persistentIndex": persistent}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) knowledgeDocumentDetail(w http.ResponseWriter, r *http.Request) { /* 定义 knowledgeDocumentDetail 函数。 */
	documentID := strings.TrimSpace(r.PathValue("id")) /* 更新 documentID 的值。 */
	if documentID == "" {                              /* 判断条件并选择处理分支。 */
		problem(w, http.StatusUnprocessableEntity, "document id is required") /* 执行当前语句并推进处理流程。 */
		return                                                                /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	documents, err := s.engine.Repo.ListKnowledgeDocs(r.Context(), claims(r).TenantID) /* 更新 err 的值。 */
	if err != nil {                                                                    /* 判断条件并选择处理分支。 */
		problem(w, http.StatusInternalServerError, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                                                  /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var document model.KnowledgeDoc  /* 声明 document。 */
	for _, item := range documents { /* 循环处理当前数据。 */
		if item.ID == documentID { /* 判断条件并选择处理分支。 */
			document = item /* 更新 document 的值。 */
			break           /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if document.ID == "" { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusNotFound, "knowledge document not found") /* 执行当前语句并推进处理流程。 */
		return                                                          /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	inspector, ok := s.engine.KB.(ports.InspectableKnowledgeBase) /* 更新 ok 的值。 */
	if !ok {                                                      /* 判断条件并选择处理分支。 */
		problem(w, http.StatusNotImplemented, "the configured knowledge index does not expose stored chunks") /* 执行当前语句并推进处理流程。 */
		return                                                                                                /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	chunks, err := inspector.ListKnowledgeChunks(r.Context(), claims(r).TenantID, document.ID) /* 更新 err 的值。 */
	if err != nil {                                                                            /* 判断条件并选择处理分支。 */
		problem(w, http.StatusBadGateway, "load indexed chunks: "+err.Error()) /* 执行当前语句并推进处理流程。 */
		return                                                                 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	write(w, http.StatusOK, map[string]any{ /* 执行当前语句并推进处理流程。 */
		"document": document,                                   /* 执行当前语句并推进处理流程。 */
		"index":    knowledgeIndexDetails(s, document, chunks), /* 执行当前语句并推进处理流程。 */
		"chunks":   chunks,                                     /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func knowledgeIndexDetails(s *Server, document model.KnowledgeDoc, chunks []model.KnowledgeChunk) map[string]any { /* 定义 knowledgeIndexDetails 函数。 */
	persistent := strings.TrimSpace(s.cfg.WeaviateURL) != "" /* 更新 persistent 的值。 */
	index := map[string]any{                                 /* 更新 index 的值。 */
		"mode":           "local-memory",                  /* 执行当前语句并推进处理流程。 */
		"persistent":     persistent,                      /* 执行当前语句并推进处理流程。 */
		"vectorizer":     "local-token-similarity",        /* 执行当前语句并推进处理流程。 */
		"embeddingModel": "",                              /* 执行当前语句并推进处理流程。 */
		"chunkCount":     len(chunks),                     /* 执行当前语句并推进处理流程。 */
		"extractedChars": document.Metadata["characters"], /* 执行当前语句并推进处理流程。 */
		"chunking": map[string]any{ /* 执行当前语句并推进处理流程。 */
			"strategy":         "fixed-window-overlap",              /* 执行当前语句并推进处理流程。 */
			"size":             1200,                                /* 执行当前语句并推进处理流程。 */
			"overlap":          200,                                 /* 执行当前语句并推进处理流程。 */
			"unit":             "Unicode 字符（rune/code point）",       /* 执行当前语句并推进处理流程。 */
			"offsetConvention": "StartChar 包含，EndChar 不包含",          /* 执行当前语句并推进处理流程。 */
			"normalization":    "先提取文件文本，再清洗 XML/HTML 标签、空白并去除首尾空白", /* 执行当前语句并推进处理流程。 */
		}, /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if persistent { /* 判断条件并选择处理分支。 */
		index["mode"] = "weaviate"                   /* 执行当前语句并推进处理流程。 */
		index["vectorizer"] = "text2vec-ollama"      /* 执行当前语句并推进处理流程。 */
		index["embeddingModel"] = "nomic-embed-text" /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return index /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) knowledgeUpload(w http.ResponseWriter, r *http.Request) { /* 定义 knowledgeUpload 函数。 */
	const maxDocumentBytes = 32 << 20 /* 声明 maxDocumentBytes。 */
	if s.engine.KB == nil {           /* 判断条件并选择处理分支。 */
		problem(w, 503, "knowledge base disabled") /* 执行当前语句并推进处理流程。 */
		return                                     /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	r.Body = http.MaxBytesReader(w, r.Body, maxDocumentBytes+(1<<20)) /* 更新 r.Body 的值。 */
	if err := r.ParseMultipartForm(1 << 20); err != nil {             /* 判断条件并选择处理分支。 */
		var maxErr *http.MaxBytesError /* 声明 maxErr。 */
		if errors.As(err, &maxErr) {   /* 判断条件并选择处理分支。 */
			problem(w, http.StatusRequestEntityTooLarge, "document exceeds 32 MiB") /* 执行当前语句并推进处理流程。 */
			return                                                                  /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		problem(w, 400, "invalid multipart form") /* 执行当前语句并推进处理流程。 */
		return                                    /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if r.MultipartForm != nil { /* 判断条件并选择处理分支。 */
		defer r.MultipartForm.RemoveAll() /* 安排函数结束时执行清理。 */
	} /* 结束当前表达式或代码块。 */
	f, h, err := r.FormFile("file") /* 更新 err 的值。 */
	if err != nil {                 /* 判断条件并选择处理分支。 */
		problem(w, 400, "file is required") /* 执行当前语句并推进处理流程。 */
		return                              /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer f.Close()                                                /* 安排函数结束时执行清理。 */
	data, err := io.ReadAll(io.LimitReader(f, maxDocumentBytes+1)) /* 更新 err 的值。 */
	if err != nil {                                                /* 判断条件并选择处理分支。 */
		problem(w, 400, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(data) > maxDocumentBytes { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusRequestEntityTooLarge, "document exceeds 32 MiB") /* 执行当前语句并推进处理流程。 */
		return                                                                  /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	id := fmt.Sprintf("doc_%d", time.Now().UnixNano())         /* 更新 id 的值。 */
	c := claims(r)                                             /* 更新 c 的值。 */
	workflowID := strings.TrimSpace(r.FormValue("workflowId")) /* 更新 workflowID 的值。 */
	if workflowID == "" || len(workflowID) > 128 {             /* 判断条件并选择处理分支。 */
		problem(w, 422, "workflowId is required so every document is associated with an Agent") /* 执行当前语句并推进处理流程。 */
		return                                                                                  /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	productID := r.FormValue("productId")                                    /* 更新 productID 的值。 */
	category := strings.TrimSpace(r.FormValue("category"))                   /* 更新 category 的值。 */
	tags := cleanStringList(strings.Split(r.FormValue("tags"), ","), 16, 40) /* 更新 tags 的值。 */
	if len(category) > 40 {                                                  /* 判断条件并选择处理分支。 */
		problem(w, 422, "category is too long") /* 执行当前语句并推进处理流程。 */
		return                                  /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	filename := strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(h.Filename)                                                                         /* 更新 filename 的值。 */
	bucket := "iot-knowledge-docs"                                                                                                                              /* 更新 bucket 的值。 */
	objectKey := fmt.Sprintf("%s/agents/%s/%s/%s", c.TenantID, workflowID, id, filename)                                                                        /* 更新 objectKey 的值。 */
	if _, err = s.engine.Archive.PutObject(r.Context(), bucket, objectKey, bytes.NewReader(data), int64(len(data)), h.Header.Get("Content-Type")); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 502, "store document: "+err.Error()) /* 执行当前语句并推进处理流程。 */
		return                                          /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	textContent, extractErr := core.ExtractKnowledgeText(h.Filename, data) /* 更新 extractErr 的值。 */
	if extractErr != nil {                                                 /* 判断条件并选择处理分支。 */
		problem(w, 422, extractErr.Error()) /* 执行当前语句并推进处理流程。 */
		return                              /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	chunks := core.ChunkKnowledgeTextDetailed(textContent, 1200, 200) /* 更新 chunks 的值。 */
	if len(chunks) == 0 {                                             /* 判断条件并选择处理分支。 */
		problem(w, 422, "document contains no indexable text") /* 执行当前语句并推进处理流程。 */
		return                                                 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for i, chunk := range chunks { /* 循环处理当前数据。 */
		chunkID := fmt.Sprintf("%s-chunk-%04d", id, i+1)                   /* 更新 chunkID 的值。 */
		if filtered, ok := s.engine.KB.(ports.FilteredKnowledgeBase); ok { /* 判断条件并选择处理分支。 */
			err = filtered.IndexKnowledge(r.Context(), ports.KnowledgeIndexInput{TenantID: c.TenantID, WorkflowID: workflowID, ProductID: productID, Category: category, Tags: tags, DocumentID: id, ChunkID: chunkID, ChunkIndex: chunk.Index, StartChar: chunk.StartChar, EndChar: chunk.EndChar, CharacterCount: chunk.CharacterCount, OverlapChars: chunk.OverlapChars, Content: []byte(chunk.Text)}) /* 更新 err 的值。 */
		} else { /* 结束当前表达式或代码块。 */
			err = errors.New("workflow-bound knowledge indexing is not supported by the configured index") /* 更新 err 的值。 */
		} /* 结束当前表达式或代码块。 */
		if err != nil { /* 判断条件并选择处理分支。 */
			problem(w, 502, err.Error()) /* 执行当前语句并推进处理流程。 */
			return                       /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	doc := model.KnowledgeDoc{ID: id, TenantID: c.TenantID, WorkflowID: workflowID, ProductID: productID, Category: category, Tags: tags, ObjectBucket: bucket, ObjectKey: objectKey, Filename: h.Filename, Status: "INDEXED", Metadata: map[string]any{"size": len(data), "contentType": h.Header.Get("Content-Type"), "chunks": len(chunks), "characters": len([]rune(textContent)), "chunking": map[string]any{"strategy": "fixed-window-overlap", "size": 1200, "overlap": 200, "unit": "unicode-code-points", "offsetConvention": "start-inclusive,end-exclusive"}}, CreatedAt: time.Now().UnixMilli()} /* 更新 doc 的值。 */
	if err = s.engine.Repo.SaveKnowledgeDoc(r.Context(), doc); err != nil {                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 201, doc) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) mqttToken(w http.ResponseWriter, r *http.Request) { /* 定义 mqttToken 函数。 */
	c := claims(r)                                                                                                                                                                                              /* 更新 c 的值。 */
	scope := []string{fmt.Sprintf("/iot/parsed/%s/#", c.TenantID), fmt.Sprintf("/iot/alarm/%s/#", c.TenantID), fmt.Sprintf("/iot/device/state/%s/#", c.TenantID), fmt.Sprintf("/iot/ui-action/%s", c.TenantID)} /* 更新 scope 的值。 */
	if c.TokenUse == "user" {                                                                                                                                                                                   /* 判断条件并选择处理分支。 */
		_, p, err := s.managedIdentity(r, c) /* 更新 err 的值。 */
		if err != nil {                      /* 判断条件并选择处理分支。 */
			problem(w, 401, "会话已失效") /* 执行当前语句并推进处理流程。 */
			return                   /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		scope = []string{}                      /* 更新 scope 的值。 */
		if p["menu:raw"] || p["menu:devices"] { /* 判断条件并选择处理分支。 */
			scope = append(scope, fmt.Sprintf("/iot/parsed/%s/#", c.TenantID), fmt.Sprintf("/iot/device/state/%s/#", c.TenantID)) /* 更新 scope 的值。 */
		} /* 结束当前表达式或代码块。 */
		if p["menu:alarms"] || p["menu:dashboard"] { /* 判断条件并选择处理分支。 */
			scope = append(scope, fmt.Sprintf("/iot/alarm/%s/#", c.TenantID)) /* 更新 scope 的值。 */
		} /* 结束当前表达式或代码块。 */
		token, err := s.auth.IssueBrowserMQTT(c.Username, c.TenantID, scope, 15*time.Minute) /* 更新 err 的值。 */
		if err != nil {                                                                      /* 判断条件并选择处理分支。 */
			problem(w, 500, "创建消息令牌失败") /* 执行当前语句并推进处理流程。 */
			return                      /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		write(w, 200, map[string]any{"username": c.Username, "token": token, "expiresIn": 900, "subscriptions": scope, "websocketUrl": s.mqttWebSocketURL(r)}) /* 执行当前语句并推进处理流程。 */
		return                                                                                                                                                 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	acl := make([]auth.ACLRule, 0, len(scope)) /* 更新 acl 的值。 */
	for _, topic := range scope {              /* 循环处理当前数据。 */
		acl = append(acl, auth.ACLRule{Permission: "allow", Action: "subscribe", Topic: topic}) /* 更新 acl 的值。 */
	} /* 结束当前表达式或代码块。 */
	token, err := s.auth.IssueWithACL(c.Username, c.TenantID, c.Role, scope, acl, 15*time.Minute) /* 更新 err 的值。 */
	if err != nil {                                                                               /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 200, map[string]any{"username": c.Username, "token": token, "expiresIn": 900, "subscriptions": scope, "websocketUrl": s.mqttWebSocketURL(r)}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) deviceMQTTToken(w http.ResponseWriter, r *http.Request) { /* 定义 deviceMQTTToken 函数。 */
	accessKey, secret := r.Header.Get("X-Device-Key"), r.Header.Get("X-Device-Secret") /* 更新 secret 的值。 */
	v, err := s.onboarding.Authenticate(r.Context(), accessKey, secret)                /* 更新 err 的值。 */
	if err != nil {                                                                    /* 判断条件并选择处理分支。 */
		problem(w, 401, "invalid device credentials") /* 执行当前语句并推进处理流程。 */
		return                                        /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	product, productErr := s.engine.Repo.GetProduct(r.Context(), v.TenantID, v.ProductID) /* 更新 productErr 的值。 */
	if productErr != nil || product.Status != "ENABLED" {                                 /* 判断条件并选择处理分支。 */
		problem(w, 401, "device product is disabled") /* 执行当前语句并推进处理流程。 */
		return                                        /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	w.Header().Set("Cache-Control", "no-store")                                                                                                                                                  /* 执行当前语句并推进处理流程。 */
	topic := fmt.Sprintf("/external/raw/%s/%s/%s", v.TenantID, v.ProductID, v.ID)                                                                                                                /* 更新 topic 的值。 */
	acl := []auth.ACLRule{{Permission: "allow", Action: "publish", Topic: topic}, {Permission: "allow", Action: "subscribe", Topic: fmt.Sprintf("/iot/device/command/%s/%s", v.TenantID, v.ID)}} /* 更新 acl 的值。 */
	ttl := 24 * time.Hour                                                                                                                                                                        /* 更新 ttl 的值。 */
	if v.Connector == "MQTT" || v.Connector == "HTTP" {                                                                                                                                          /* 判断条件并选择处理分支。 */
		acl = nil                                                                       /* 更新 acl 的值。 */
		ttl = 5 * time.Minute                                                           /* 更新 ttl 的值。 */
		topic = fmt.Sprintf("/iot/up/%s/%s/%s/property", v.TenantID, v.ProductID, v.ID) /* 更新 topic 的值。 */
	} /* 结束当前表达式或代码块。 */
	for _, kind := range []string{"property", "event", "state", "command-reply"} { /* 循环处理当前数据。 */
		acl = append(acl, auth.ACLRule{Permission: "allow", Action: "publish", Topic: fmt.Sprintf("/iot/up/%s/%s/%s/%s", v.TenantID, v.ProductID, v.ID, kind)}) /* 更新 acl 的值。 */
	} /* 结束当前表达式或代码块。 */
	acl = append(acl, auth.ACLRule{Permission: "allow", Action: "subscribe", Topic: fmt.Sprintf("/iot/down/%s/%s/%s/command", v.TenantID, v.ProductID, v.ID)}) /* 更新 acl 的值。 */

	token, err := s.auth.IssueWithACL(v.AccessKey, v.TenantID, "device", nil, acl, ttl) /* 检查错误并决定后续处理。 */
	if err != nil {                                                                     /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	response := map[string]any{"username": v.AccessKey, "token": token, "expiresIn": int(ttl.Seconds()), "publishTopic": topic, "websocketUrl": s.mqttWebSocketURL(r)} /* 更新 response 的值。 */
	write(w, 200, response)                                                                                                                                            /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) mqttLoadToken(w http.ResponseWriter, r *http.Request) { /* 定义 mqttLoadToken 函数。 */
	var input struct { /* 声明 input。 */
		ProductID string `json:"productId"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if decode(w, r, &input) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if input.ProductID == "" { /* 判断条件并选择处理分支。 */
		problem(w, 422, "productId is required") /* 执行当前语句并推进处理流程。 */
		return                                   /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	c := claims(r)                                                                                       /* 更新 c 的值。 */
	topic := fmt.Sprintf("/external/raw/%s/%s/#", c.TenantID, input.ProductID)                           /* 更新 topic 的值。 */
	acl := []auth.ACLRule{{Permission: "allow", Action: "publish", Topic: topic}}                        /* 更新 acl 的值。 */
	token, err := s.auth.IssueWithACL("loadgen:"+c.Username, c.TenantID, "loadgen", nil, acl, time.Hour) /* 检查错误并决定后续处理。 */
	if err != nil {                                                                                      /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.audit(r, "mqtt.load-token.issue", "product", input.ProductID, map[string]any{"topic": topic, "expiresIn": 3600})                                          /* 执行当前语句并推进处理流程。 */
	write(w, 200, map[string]any{"username": "loadgen:" + c.Username, "token": token, "expiresIn": 3600, "publishTopicPrefix": strings.TrimSuffix(topic, "#")}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) mqttWebSocketURL(r *http.Request) string { /* 定义 mqttWebSocketURL 函数。 */
	if s.cfg.MQTTWebSocketURL != "" { /* 判断条件并选择处理分支。 */
		return s.cfg.MQTTWebSocketURL /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	scheme := "ws"                                                                     /* 更新 scheme 的值。 */
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") { /* 判断条件并选择处理分支。 */
		scheme = "wss" /* 更新 scheme 的值。 */
	} /* 结束当前表达式或代码块。 */
	host := strings.Split(r.Host, ":")[0]                 /* 更新 host 的值。 */
	return fmt.Sprintf("%s://%s:8083/mqtt", scheme, host) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) videoWebhook(w http.ResponseWriter, r *http.Request) { /* 定义 videoWebhook 函数。 */
	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20)) /* 更新 err 的值。 */
	if err != nil {                                         /* 判断条件并选择处理分支。 */
		problem(w, 400, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	platform := r.Header.Get("X-Video-Platform-ID") /* 更新 platform 的值。 */
	timestamp := r.Header.Get("X-Timestamp")        /* 更新 timestamp 的值。 */
	secret := s.cfg.VideoSecrets[platform]          /* 更新 secret 的值。 */
	if secret == "" && !s.cfg.DevMode {             /* 判断条件并选择处理分支。 */
		problem(w, 401, "unknown video platform") /* 执行当前语句并推进处理流程。 */
		return                                    /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if secret != "" && !verifySignature(secret, timestamp, body, r.Header.Get("X-Signature")) { /* 判断条件并选择处理分支。 */
		problem(w, 401, "invalid signature") /* 执行当前语句并推进处理流程。 */
		return                               /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	expectedTenant := strings.TrimSpace(s.cfg.VideoPlatformTenants[platform]) /* 更新 expectedTenant 的值。 */
	if expectedTenant == "" && !s.cfg.DevMode {                               /* 判断条件并选择处理分支。 */
		problem(w, 401, "video platform tenant binding is not configured") /* 执行当前语句并推进处理流程。 */
		return                                                             /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	ts, _ := strconv.ParseInt(timestamp, 10, 64)         /* 更新 _ 的值。 */
	if secret != "" && abs(time.Now().Unix()-ts) > 300 { /* 判断条件并选择处理分支。 */
		problem(w, 401, "stale timestamp") /* 执行当前语句并推进处理流程。 */
		return                             /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var v model.VideoAlarmEvent                     /* 声明 v。 */
	if err = json.Unmarshal(body, &v); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 400, "invalid JSON") /* 执行当前语句并推进处理流程。 */
		return                          /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if expectedTenant != "" { /* 判断条件并选择处理分支。 */
		if v.TenantID != "" && v.TenantID != expectedTenant { /* 判断条件并选择处理分支。 */
			problem(w, 403, "video platform is not bound to this tenant") /* 执行当前语句并推进处理流程。 */
			return                                                        /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		v.TenantID = expectedTenant /* 更新 v.TenantID 的值。 */
	} /* 结束当前表达式或代码块。 */
	if !s.cfg.DevMode { /* 判断条件并选择处理分支。 */
		mapping, mappingErr := s.engine.Repo.GetVideoCameraMapping(r.Context(), v.TenantID, v.CameraID) /* 更新 mappingErr 的值。 */
		if mappingErr != nil || !mapping.Enabled {                                                      /* 判断条件并选择处理分支。 */
			problem(w, 403, "camera is not bound to this tenant or is disabled") /* 执行当前语句并推进处理流程。 */
			return                                                               /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	a, created, err := s.engine.IngestVideo(r.Context(), v) /* 更新 err 的值。 */
	if err != nil {                                         /* 判断条件并选择处理分支。 */
		problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	write(w, map[bool]int{true: 201, false: 200}[created], map[string]any{"created": created, "alarm": a}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) videoCameras(w http.ResponseWriter, r *http.Request) { /* 定义 videoCameras 函数。 */
	pagination := parseListPagination(r)                                                                                                    /* 更新 pagination 的值。 */
	items, total, err := s.engine.Repo.ListVideoCameraMappingsPage(r.Context(), claims(r).TenantID, pagination.PageSize, pagination.Offset) /* 更新 err 的值。 */
	if err != nil {                                                                                                                         /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for index := range items { /* 循环处理当前数据。 */
		// The platform stores camera metadata only. Live stream lookup and
		// playback stay in the external video platform, so never return legacy
		// stream or vendor credential fields from this endpoint.
		items[index].IngestMode = ""          /* 更新 items[index].IngestMode 的值。 */
		items[index].ProjectID = ""           /* 更新 items[index].ProjectID 的值。 */
		items[index].CityCode = ""            /* 更新 items[index].CityCode 的值。 */
		items[index].DistrictCode = ""        /* 更新 items[index].DistrictCode 的值。 */
		items[index].AreaID = ""              /* 更新 items[index].AreaID 的值。 */
		items[index].RelatedDeviceIDs = nil   /* 更新 items[index].RelatedDeviceIDs 的值。 */
		items[index].RelatedFloorIDs = nil    /* 更新 items[index].RelatedFloorIDs 的值。 */
		items[index].RelatedRoomIDs = nil     /* 更新 items[index].RelatedRoomIDs 的值。 */
		items[index].VideoPlatformID = ""     /* 更新 items[index].VideoPlatformID 的值。 */
		items[index].StreamURL = ""           /* 更新 items[index].StreamURL 的值。 */
		items[index].StreamType = ""          /* 更新 items[index].StreamType 的值。 */
		items[index].SDKEndpoint = ""         /* 更新 items[index].SDKEndpoint 的值。 */
		items[index].SDKCameraID = ""         /* 更新 items[index].SDKCameraID 的值。 */
		items[index].SDKCredentialRef = ""    /* 更新 items[index].SDKCredentialRef 的值。 */
		items[index].StreamConfigured = false /* 更新 items[index].StreamConfigured 的值。 */
		items[index].PreviewEligible = false  /* 更新 items[index].PreviewEligible 的值。 */
	} /* 结束当前表达式或代码块。 */
	writeList(w, 200, items, total, pagination, nil) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) videoRelations(w http.ResponseWriter, r *http.Request) { /* 定义 videoRelations 函数。 */
	relationType := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("relationType"))) /* 更新 relationType 的值。 */
	targetID := strings.TrimSpace(r.URL.Query().Get("targetId"))                          /* 更新 targetID 的值。 */
	if relationType != "device" || targetID == "" {                                       /* 判断条件并选择处理分支。 */
		problem(w, http.StatusUnprocessableEntity, "only device relation is supported and targetId is required") /* 执行当前语句并推进处理流程。 */
		return                                                                                                   /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	relations, err := s.engine.Repo.ListVideoCameraRelationsByTarget(r.Context(), claims(r).TenantID, relationType, targetID) /* 更新 err 的值。 */
	if err != nil {                                                                                                           /* 判断条件并选择处理分支。 */
		problem(w, http.StatusInternalServerError, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                                                  /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	write(w, http.StatusOK, map[string]any{"items": relations, "relationType": relationType, "targetId": targetID}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) saveVideoCamera(w http.ResponseWriter, r *http.Request) { /* 定义 saveVideoCamera 函数。 */
	var v model.VideoCameraMapping /* 声明 v。 */
	if decode(w, r, &v) != nil {   /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	c := claims(r)                         /* 更新 c 的值。 */
	v.TenantID = c.TenantID                /* 更新 v.TenantID 的值。 */
	if id := r.PathValue("id"); id != "" { /* 判断条件并选择处理分支。 */
		v.CameraID = id /* 更新 v.CameraID 的值。 */
	} /* 结束当前表达式或代码块。 */
	v.CameraID = strings.TrimSpace(v.CameraID)     /* 更新 v.CameraID 的值。 */
	v.CameraName = strings.TrimSpace(v.CameraName) /* 更新 v.CameraName 的值。 */
	if v.CameraID == "" || v.CameraName == "" {    /* 判断条件并选择处理分支。 */
		problem(w, 422, "cameraId and cameraName are required") /* 执行当前语句并推进处理流程。 */
		return                                                  /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	// Accept one legacy relatedDeviceIds value during migration, but reject
	// multiple values so the camera -> device cardinality is unambiguous.
	legacyDeviceIDs := cleanStringList(v.RelatedDeviceIDs, 128, 128) /* 更新 legacyDeviceIDs 的值。 */
	if len(legacyDeviceIDs) > 1 {                                    /* 判断条件并选择处理分支。 */
		problem(w, 422, "a camera can be associated with at most one device") /* 执行当前语句并推进处理流程。 */
		return                                                                /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	v.DeviceID = strings.TrimSpace(v.DeviceID)         /* 更新 v.DeviceID 的值。 */
	if v.DeviceID == "" && len(legacyDeviceIDs) == 1 { /* 判断条件并选择处理分支。 */
		v.DeviceID = legacyDeviceIDs[0] /* 更新 v.DeviceID 的值。 */
	} /* 结束当前表达式或代码块。 */
	if v.DeviceID != "" { /* 判断条件并选择处理分支。 */
		if _, deviceErr := s.engine.Repo.GetManagedDevice(r.Context(), c.TenantID, v.DeviceID); deviceErr != nil { /* 判断条件并选择处理分支。 */
			problem(w, 422, "deviceId is not registered in the current tenant") /* 执行当前语句并推进处理流程。 */
			return                                                              /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	v.Brand = strings.TrimSpace(v.Brand)             /* 更新 v.Brand 的值。 */
	v.CameraPoint = strings.TrimSpace(v.CameraPoint) /* 更新 v.CameraPoint 的值。 */
	v.Building = strings.TrimSpace(v.Building)       /* 更新 v.Building 的值。 */
	v.Floor = strings.TrimSpace(v.Floor)             /* 更新 v.Floor 的值。 */
	v.Room = strings.TrimSpace(v.Room)               /* 更新 v.Room 的值。 */
	// Clear legacy relation and stream fields on every save. The video
	// platform remains the source of truth for live playback.
	v.RelatedDeviceIDs = nil                                                                                                                                              /* 更新 v.RelatedDeviceIDs 的值。 */
	v.RelatedFloorIDs = nil                                                                                                                                               /* 更新 v.RelatedFloorIDs 的值。 */
	v.RelatedRoomIDs = nil                                                                                                                                                /* 更新 v.RelatedRoomIDs 的值。 */
	v.IngestMode = ""                                                                                                                                                     /* 更新 v.IngestMode 的值。 */
	v.ProjectID = ""                                                                                                                                                      /* 更新 v.ProjectID 的值。 */
	v.CityCode = ""                                                                                                                                                       /* 更新 v.CityCode 的值。 */
	v.DistrictCode = ""                                                                                                                                                   /* 更新 v.DistrictCode 的值。 */
	v.AreaID = ""                                                                                                                                                         /* 更新 v.AreaID 的值。 */
	v.VideoPlatformID = ""                                                                                                                                                /* 更新 v.VideoPlatformID 的值。 */
	if previous, err := s.engine.Repo.GetVideoCameraMapping(r.Context(), c.TenantID, v.CameraID); err == nil && strings.HasPrefix(previous.VideoPlatformID, "gb28181/") { /* 判断条件并选择处理分支。 */
		v.VideoPlatformID = previous.VideoPlatformID /* 更新 v.VideoPlatformID 的值。 */
	} /* 结束当前表达式或代码块。 */
	v.StreamURL = ""                                                             /* 更新 v.StreamURL 的值。 */
	v.StreamType = ""                                                            /* 更新 v.StreamType 的值。 */
	v.SDKEndpoint = ""                                                           /* 更新 v.SDKEndpoint 的值。 */
	v.SDKCameraID = ""                                                           /* 更新 v.SDKCameraID 的值。 */
	v.SDKCredentialRef = ""                                                      /* 更新 v.SDKCredentialRef 的值。 */
	v.UpdatedAt = time.Now().UnixMilli()                                         /* 更新 v.UpdatedAt 的值。 */
	if err := s.engine.Repo.SaveVideoCameraMapping(r.Context(), v); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.audit(r, "video.camera.save", "video-camera", v.CameraID, map[string]any{"deviceId": v.DeviceID, "enabled": v.Enabled}) /* 执行当前语句并推进处理流程。 */
	write(w, map[bool]int{true: 200, false: 201}[r.Method == http.MethodPut], v)                                              /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func randomHex(size int) string { /* 定义 randomHex 函数。 */
	b := make([]byte, size)                 /* 更新 b 的值。 */
	if _, err := rand.Read(b); err != nil { /* 判断条件并选择处理分支。 */
		return fmt.Sprintf("%d", time.Now().UnixNano()) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return hex.EncodeToString(b) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func newDeviceCredential() model.DeviceCredential { /* 定义 newDeviceCredential 函数。 */
	return model.DeviceCredential{AccessKey: "dk_" + randomHex(8), Secret: "ds_" + randomHex(18)} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func secretHash(secret string) string { /* 定义 secretHash 函数。 */
	h := sha256.Sum256([]byte(secret)) /* 更新 h 的值。 */
	return hex.EncodeToString(h[:])    /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) audit(r *http.Request, action, targetType, targetID string, details map[string]any) { /* 定义 audit 函数。 */
	c := claims(r)                                                                                                                                                                                                                                   /* 更新 c 的值。 */
	_ = s.engine.Repo.SaveAudit(r.Context(), model.AuditLog{ID: "audit_" + randomHex(10), TenantID: c.TenantID, Actor: c.Username, Action: action, TargetType: targetType, TargetID: targetID, Details: details, CreatedAt: time.Now().UnixMilli()}) /* 更新 _ 的值。 */
} /* 结束当前表达式或代码块。 */
func verifySignature(secret, ts string, body []byte, sig string) bool { /* 定义 verifySignature 函数。 */
	mac := hmac.New(sha256.New, []byte(secret))                       /* 更新 mac 的值。 */
	_, _ = mac.Write([]byte(ts))                                      /* 更新 _ 的值。 */
	_, _ = mac.Write(body)                                            /* 更新 _ 的值。 */
	expected := hex.EncodeToString(mac.Sum(nil))                      /* 更新 expected 的值。 */
	return hmac.Equal([]byte(strings.ToLower(sig)), []byte(expected)) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

type endpointHandler func(http.ResponseWriter, *http.Request) /* 定义 endpointHandler 类型。 */

// endpoint adapts the established net/http business handlers to Gin while
// preserving Request.PathValue for code that reads named route parameters.
func (s *Server) endpoint(handler endpointHandler, pathParams ...string) gin.HandlerFunc { /* 定义 endpoint 函数。 */
	return func(c *gin.Context) { /* 返回当前处理结果。 */
		for _, name := range pathParams { /* 循环处理当前数据。 */
			c.Request.SetPathValue(name, c.Param(name)) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		handler(c.Writer, c.Request) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) authorize(role string) gin.HandlerFunc { /* 定义 authorize 函数。 */
	return func(c *gin.Context) { /* 返回当前处理结果。 */
		token := auth.Bearer(c.GetHeader("Authorization")) /* 更新 token 的值。 */
		claimsValue, err := s.auth.Parse(token)            /* 更新 err 的值。 */
		if err != nil {                                    /* 判断条件并选择处理分支。 */
			ginProblem(c, http.StatusUnauthorized, err.Error()) /* 执行当前语句并推进处理流程。 */
			c.Abort()                                           /* 执行当前语句并推进处理流程。 */
			return                                              /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if claimsValue.TokenUse != "" && claimsValue.TokenUse != "user" { /* 判断条件并选择处理分支。 */
			ginProblem(c, http.StatusForbidden, "harness tokens are restricted to the MCP harness endpoint") /* 执行当前语句并推进处理流程。 */
			c.Abort()                                                                                        /* 执行当前语句并推进处理流程。 */
			return                                                                                           /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		allowed := claimsValue.Role == "admin" || claimsValue.Role == role || role == "viewer" && (claimsValue.Role == "operator" || claimsValue.Role == "viewer") /* 更新 allowed 的值。 */
		if claimsValue.TokenUse == "user" {                                                                                                                        /* 判断条件并选择处理分支。 */
			user, permissions, err := s.managedIdentity(c.Request, claimsValue) /* 更新 err 的值。 */
			if err != nil {                                                     /* 判断条件并选择处理分支。 */
				ginProblem(c, 401, "账户已停用或会话已失效，请重新登录") /* 执行当前语句并推进处理流程。 */
				c.Abort()                               /* 执行当前语句并推进处理流程。 */
				return                                  /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			allowed = allowsRoute(permissions, c.Request.Method, c.FullPath())                                 /* 更新 allowed 的值。 */
			scope := scopeFor(user, permissions, claimsValue.TenantID)                                         /* 更新 scope 的值。 */
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), deviceScopeKey{}, scope)) /* 更新 c.Request 的值。 */
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), permissionsKey{}, permissions))
			if allowed { /* 判断条件并选择处理分支。 */
				allowed = s.allowScopedRequest(c, scope) /* 更新 allowed 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if !allowed { /* 判断条件并选择处理分支。 */
			ginProblem(c, http.StatusForbidden, "insufficient role") /* 执行当前语句并推进处理流程。 */
			c.Abort()                                                /* 执行当前语句并推进处理流程。 */
			return                                                   /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		ctx := auth.ContextWithClaims(context.WithValue(c.Request.Context(), claimsKey, claimsValue), claimsValue) /* 更新 ctx 的值。 */
		c.Request = c.Request.WithContext(ctx)                                                                     /* 更新 c.Request 的值。 */
		c.Next()                                                                                                   /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) authorizeHarness() gin.HandlerFunc { /* 定义 authorizeHarness 函数。 */
	allowedScopes := make(map[string]struct{})       /* 更新 allowedScopes 的值。 */
	for _, scope := range auth.HarnessReadScopes() { /* 循环处理当前数据。 */
		allowedScopes[scope] = struct{}{} /* 更新 allowedScopes[scope] 的值。 */
	} /* 结束当前表达式或代码块。 */
	return func(c *gin.Context) { /* 返回当前处理结果。 */
		token := auth.Bearer(c.GetHeader("Authorization")) /* 更新 token 的值。 */
		claimsValue, err := s.auth.Parse(token)            /* 更新 err 的值。 */
		if err != nil {                                    /* 判断条件并选择处理分支。 */
			ginProblem(c, http.StatusUnauthorized, err.Error()) /* 执行当前语句并推进处理流程。 */
			c.Abort()                                           /* 执行当前语句并推进处理流程。 */
			return                                              /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if claimsValue.TokenUse != "harness" || !claimsValue.HasAudience(auth.HarnessAudience) || claimsValue.RunID == "" || claimsValue.TenantID == "" { /* 判断条件并选择处理分支。 */
			ginProblem(c, http.StatusForbidden, "invalid harness token") /* 执行当前语句并推进处理流程。 */
			c.Abort()                                                    /* 执行当前语句并推进处理流程。 */
			return                                                       /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		for _, scope := range claimsValue.Scopes { /* 循环处理当前数据。 */
			if _, ok := allowedScopes[scope]; !ok { /* 判断条件并选择处理分支。 */
				ginProblem(c, http.StatusForbidden, "invalid harness scope") /* 执行当前语句并推进处理流程。 */
				c.Abort()                                                    /* 执行当前语句并推进处理流程。 */
				return                                                       /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if claimsValue.ManagedUser {
			user, permissions, err := s.managedIdentity(c.Request, claimsValue)
			if err != nil {
				ginProblem(c, http.StatusUnauthorized, "账户已停用或会话已失效，请重新登录")
				c.Abort()
				return
			}
			if claimsValue.Workflow != "" {
				if !businessWorkflowAllowed(permissions, claimsValue.Workflow) {
					ginProblem(c, http.StatusForbidden, "无此智能功能的访问权限")
					c.Abort()
					return
				}
			} else if !permissions["menu:ai"] || !(permissions["POST /api/v1/ai/chat"] || permissions["POST /api/v1/ai/chat/stream"]) {
				ginProblem(c, http.StatusForbidden, "无智能助手访问权限")
				c.Abort()
				return
			}
			ctx := context.WithValue(c.Request.Context(), deviceScopeKey{}, scopeFor(user, permissions, claimsValue.TenantID))
			ctx = context.WithValue(ctx, permissionsKey{}, permissions)
			claimsValue.Scopes = intersectScopes(claimsValue.Scopes, workflowScopes(ctx))
			claimsValue.Permissions = permissionList(permissions)
			c.Request = c.Request.WithContext(ctx)
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)                                      /* 更新 c.Request.Body 的值。 */
		ctx := auth.ContextWithClaims(context.WithValue(c.Request.Context(), claimsKey, claimsValue), claimsValue) /* 更新 ctx 的值。 */
		c.Request = c.Request.WithContext(ctx)                                                                     /* 更新 c.Request 的值。 */
		c.Next()                                                                                                   /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) security() gin.HandlerFunc { /* 定义 security 函数。 */
	return func(c *gin.Context) { /* 返回当前处理结果。 */
		c.Header("X-Content-Type-Options", "nosniff")                                                                                                                        /* 执行当前语句并推进处理流程。 */
		c.Header("X-Frame-Options", "DENY")                                                                                                                                  /* 执行当前语句并推进处理流程。 */
		c.Header("Referrer-Policy", "no-referrer")                                                                                                                           /* 执行当前语句并推进处理流程。 */
		c.Header("Content-Security-Policy", "default-src 'self'; connect-src 'self' ws: wss:; style-src 'self' 'unsafe-inline'; script-src 'self'; worker-src 'self' blob:") /* 执行当前语句并推进处理流程。 */
		c.Next()                                                                                                                                                             /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) cors() gin.HandlerFunc { /* 定义 cors 函数。 */
	allowedOrigins := make(map[string]struct{}, len(s.cfg.CORSAllowedOrigins)) /* 更新 allowedOrigins 的值。 */
	for _, origin := range s.cfg.CORSAllowedOrigins {                          /* 循环处理当前数据。 */
		allowedOrigins[strings.TrimRight(origin, "/")] = struct{}{} /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return func(c *gin.Context) { /* 返回当前处理结果。 */
		origin := strings.TrimRight(c.GetHeader("Origin"), "/") /* 更新 origin 的值。 */
		if origin != "" {                                       /* 判断条件并选择处理分支。 */
			if _, allowed := allowedOrigins[origin]; allowed { /* 判断条件并选择处理分支。 */
				c.Header("Access-Control-Allow-Origin", origin)                                                                                                       /* 执行当前语句并推进处理流程。 */
				c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")                                                                           /* 执行当前语句并推进处理流程。 */
				c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Device-Key, X-Device-Secret, X-Video-Platform-ID, X-Timestamp, X-Signature") /* 执行当前语句并推进处理流程。 */
				c.Header("Access-Control-Max-Age", "600")                                                                                                             /* 执行当前语句并推进处理流程。 */
				c.Header("Vary", "Origin")                                                                                                                            /* 执行当前语句并推进处理流程。 */
			} else if c.Request.Method == http.MethodOptions { /* 结束当前表达式或代码块。 */
				ginProblem(c, http.StatusForbidden, "origin is not allowed") /* 执行当前语句并推进处理流程。 */
				c.Abort()                                                    /* 执行当前语句并推进处理流程。 */
				return                                                       /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if c.Request.Method == http.MethodOptions { /* 判断条件并选择处理分支。 */
			c.Status(http.StatusNoContent) /* 执行当前语句并推进处理流程。 */
			c.Abort()                      /* 执行当前语句并推进处理流程。 */
			return                         /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		c.Next() /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) accessLog() gin.HandlerFunc { /* 定义 accessLog 函数。 */
	return func(c *gin.Context) { /* 返回当前处理结果。 */
		start := time.Now() /* 更新 start 的值。 */
		c.Next()            /* 执行当前语句并推进处理流程。 */
		if s.log != nil {   /* 判断条件并选择处理分支。 */
			s.log.Info("http request", "method", c.Request.Method, "path", c.Request.URL.Path, "route", c.FullPath(), "status", c.Writer.Status(), "duration", time.Since(start).String()) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) recovery() gin.HandlerFunc { /* 定义 recovery 函数。 */
	return gin.CustomRecovery(func(c *gin.Context, recovered any) { /* 返回当前处理结果。 */
		if s.log != nil { /* 判断条件并选择处理分支。 */
			s.log.Error("http panic recovered", "method", c.Request.Method, "path", c.Request.URL.Path, "error", fmt.Sprint(recovered)) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		ginProblem(c, http.StatusInternalServerError, "internal server error") /* 执行当前语句并推进处理流程。 */
		c.Abort()                                                              /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func ginProblem(c *gin.Context, status int, detail string) { /* 定义 ginProblem 函数。 */
	c.JSON(status, gin.H{"type": "about:blank", "title": http.StatusText(status), "status": status, "detail": detail}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func claims(r *http.Request) auth.Claims { /* 定义 claims 函数。 */
	v, _ := r.Context().Value(claimsKey).(auth.Claims) /* 更新 _ 的值。 */
	return v                                           /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func tenant(c auth.Claims, requested string) string { /* 定义 tenant 函数。 */
	if requested == "" || requested == c.TenantID { /* 判断条件并选择处理分支。 */
		return c.TenantID /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return c.TenantID /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func adminTenantAllowed(configured []string, requested string) bool { /* 定义 adminTenantAllowed 函数。 */
	if len(configured) == 0 { /* 判断条件并选择处理分支。 */
		configured = []string{"tenant_001"} /* 更新 configured 的值。 */
	} /* 结束当前表达式或代码块。 */
	requested = strings.TrimSpace(requested) /* 更新 requested 的值。 */
	for _, tenantID := range configured {    /* 循环处理当前数据。 */
		if strings.TrimSpace(tenantID) == requested { /* 判断条件并选择处理分支。 */
			return true /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return false /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func decode(w http.ResponseWriter, r *http.Request, v any) error { /* 定义 decode 函数。 */
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20) /* 更新 r.Body 的值。 */
	d := json.NewDecoder(r.Body)                    /* 更新 d 的值。 */
	d.DisallowUnknownFields()                       /* 执行当前语句并推进处理流程。 */
	if err := d.Decode(v); err != nil {             /* 判断条件并选择处理分支。 */
		problem(w, 400, "invalid request: "+err.Error()) /* 执行当前语句并推进处理流程。 */
		return err                                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func write(w http.ResponseWriter, status int, v any) { /* 定义 write 函数。 */
	w.Header().Set("Content-Type", "application/json; charset=utf-8") /* 执行当前语句并推进处理流程。 */
	// API responses contain tenant-scoped, mutable state. Prevent browsers and
	// reverse proxies from serving a stale workflow catalog after a mutation.
	w.Header().Set("Cache-Control", "no-store") /* 执行当前语句并推进处理流程。 */
	w.WriteHeader(status)                       /* 执行当前语句并推进处理流程。 */
	_ = json.NewEncoder(w).Encode(v)            /* 更新 _ 的值。 */
} /* 结束当前表达式或代码块。 */
func problem(w http.ResponseWriter, status int, detail string) { /* 定义 problem 函数。 */
	write(w, status, map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "detail": detail}) /* 执行当前语句并推进处理流程。 */
}                        /* 结束当前表达式或代码块。 */
func i64(v string) int64 { n, _ := strconv.ParseInt(v, 10, 64); return n } /* 定义 i64 函数。 */
func intval(v string, d int) int { /* 定义 intval 函数。 */
	n, e := strconv.Atoi(v) /* 更新 e 的值。 */
	if e != nil {           /* 判断条件并选择处理分支。 */
		return d /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return n /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func cleanStringList(values []string, maximum, maxLength int) []string { /* 定义 cleanStringList 函数。 */
	out := make([]string, 0, min(len(values), maximum)) /* 更新 out 的值。 */
	seen := map[string]struct{}{}                       /* 更新 seen 的值。 */
	for _, value := range values {                      /* 循环处理当前数据。 */
		value = strings.TrimSpace(value)                   /* 更新 value 的值。 */
		if value == "" || len([]rune(value)) > maxLength { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		key := strings.ToLower(value) /* 更新 key 的值。 */
		if _, ok := seen[key]; ok {   /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		seen[key] = struct{}{}   /* 更新 seen[key] 的值。 */
		out = append(out, value) /* 更新 out 的值。 */
		if len(out) >= maximum { /* 判断条件并选择处理分支。 */
			break /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return out /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// The chat workbench only exposes interactive assistants. These workflows are
// invoked by their dedicated business pages/services and must not be treated
// as user-selectable chatbots or configurable chat Agents.
func isChatWorkflowID(id string) bool { /* 定义 isChatWorkflowID 函数。 */
	return !oneOf(strings.TrimSpace(id), core.BusinessWorkflowIDs()...) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func chatWorkflowPlugins(items []ports.AIWorkflowPlugin) []ports.AIWorkflowPlugin { /* 定义 chatWorkflowPlugins 函数。 */
	visible := make([]ports.AIWorkflowPlugin, 0, len(items)) /* 更新 visible 的值。 */
	for _, item := range items {                             /* 循环处理当前数据。 */
		if isChatWorkflowID(item.ID) { /* 判断条件并选择处理分支。 */
			visible = append(visible, item) /* 更新 visible 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return visible /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func chatWorkflowManifests(items []ports.AIWorkflowManifest) []ports.AIWorkflowManifest { /* 定义 chatWorkflowManifests 函数。 */
	visible := make([]ports.AIWorkflowManifest, 0, len(items)) /* 更新 visible 的值。 */
	for _, item := range items {                               /* 循环处理当前数据。 */
		if isChatWorkflowID(item.ID) { /* 判断条件并选择处理分支。 */
			visible = append(visible, item) /* 更新 visible 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return visible /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func oneOf(value string, allowed ...string) bool { /* 定义 oneOf 函数。 */
	for _, candidate := range allowed { /* 循环处理当前数据。 */
		if value == candidate { /* 判断条件并选择处理分支。 */
			return true /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return false /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func abs(v int64) int64 { /* 定义 abs 函数。 */
	if v < 0 { /* 判断条件并选择处理分支。 */
		return -v /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return v /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

var _ multipart.File /* 声明 _。 */
