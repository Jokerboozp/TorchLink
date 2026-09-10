package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"iot-platform/internal/auth"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/mcpserver"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"

	"github.com/gin-gonic/gin"
)

type ctxKey string

const claimsKey ctxKey = "claims"

type Server struct {
	cfg                        config.Config
	engine                     *core.Engine
	auth                       *auth.Manager
	metrics                    *metrics.Registry
	log                        *slog.Logger
	router                     *gin.Engine
	aiProviderRuntime          ports.AIProviderRuntime
	aiProviderStore            ports.AIProviderConfigStore
	aiWorkflowProvider         ports.AIWorkflowProviderRuntime
	aiProviderUpdateMu         sync.Mutex
	healthInspectionMu         sync.RWMutex
	healthInspectionCache      map[string]healthInspectionSnapshot
	healthInspectionJobsMu     sync.RWMutex
	healthInspectionJobs       map[string]*healthInspectionJob
	healthInspectionEstimateMs int64
	aiAnalysisMu               sync.RWMutex
	aiAnalysisJobs             map[string]*aiAnalysisJob
	aiAnalysisEstimateMs       int64
	protocolListeners          protocolCommander
	onboarding                 *onboarding.Service
}

type healthInspectionSnapshot struct {
	report    model.DeviceHealthReport
	expiresAt time.Time
}

const healthInspectionCacheTTL = 10 * time.Minute

func New(cfg config.Config, engine *core.Engine, m *metrics.Registry, log *slog.Logger) *Server {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.HandleMethodNotAllowed = true
	router.RedirectTrailingSlash = false
	s := &Server{
		cfg:                        cfg,
		engine:                     engine,
		onboarding:                 onboarding.New(engine.Repo, engine.Parsers, cfg.DataDir, cfg.ModbusAllowedCIDRs, cfg.JWTSecret),
		auth:                       auth.New(cfg.JWTSecret),
		metrics:                    m,
		log:                        log,
		router:                     router,
		healthInspectionCache:      make(map[string]healthInspectionSnapshot),
		healthInspectionJobs:       make(map[string]*healthInspectionJob),
		healthInspectionEstimateMs: healthInspectionEstimateDefault.Milliseconds(),
		aiAnalysisJobs:             make(map[string]*aiAnalysisJob),
		aiAnalysisEstimateMs:       45000,
	}
	router.Use(s.cors(), s.security(), s.accessLog(), s.recovery())
	s.routes()
	return s
}
func (s *Server) Handler() http.Handler { return s.roleHandler() }

func (s *Server) SetAIProviderRuntime(runtime ports.AIProviderRuntime) {
	s.aiProviderRuntime = runtime
}

func (s *Server) SetAIProviderStore(store ports.AIProviderConfigStore) {
	s.aiProviderStore = store
}

func (s *Server) SetAIWorkflowProvider(runtime ports.AIWorkflowProviderRuntime) {
	s.aiWorkflowProvider = runtime
}

func (s *Server) routes() {
	s.router.GET("/api/v1/connectors/types", s.authorize("viewer"), s.endpoint(s.connectorTypes))
	s.router.GET("/api/v1/connectors", s.authorize("viewer"), s.endpoint(s.connectorStatus))
	s.deviceOperationsRoutes()
	s.router.GET("/api/v1/device-registry/:id/connection", s.authorize("viewer"), s.endpoint(s.deviceConnection, "id"))
	s.router.POST("/api/v1/onboarding/test", s.authorize("admin"), s.endpoint(s.onboardingTest))
	s.router.POST("/api/v1/onboarding", s.authorize("admin"), s.endpoint(s.onboardingCreate))
	s.router.POST("/api/v1/device-ingest/standard/:tenantId/:productId/:deviceId/:kind", s.endpoint(s.standardDeviceIngest, "tenantId", "productId", "deviceId", "kind"))
	s.router.DELETE("/api/v1/device-registry/:id/credentials", s.authorize("admin"), s.endpoint(s.disableDeviceCredential, "id"))
	s.router.POST("/api/v1/auth/login", s.endpoint(s.login))
	s.router.GET("/health/live", s.endpoint(func(w http.ResponseWriter, r *http.Request) { write(w, 200, map[string]string{"status": "ok"}) }))
	s.router.GET("/health/ready", s.endpoint(s.ready))
	s.router.GET("/metrics", s.endpoint(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = io.WriteString(w, s.metrics.Prometheus())
	}))
	s.router.POST("/api/v1/integrations/video/alarm", s.endpoint(s.videoWebhook))
	s.router.GET("/api/v1/integrations/video/cameras", s.authorize("viewer"), s.endpoint(s.videoCameras))
	s.router.GET("/api/v1/integrations/video/relations", s.authorize("viewer"), s.endpoint(s.videoRelations))
	s.router.POST("/api/v1/integrations/video/cameras", s.authorize("operator"), s.endpoint(s.saveVideoCamera))
	s.router.PUT("/api/v1/integrations/video/cameras/:id", s.authorize("operator"), s.endpoint(s.saveVideoCamera, "id"))
	s.router.POST("/api/v1/device-ingest/:deviceId", s.endpoint(s.deviceIngest, "deviceId"))
	s.router.GET("/api/v1/products", s.authorize("viewer"), s.endpoint(s.products))
	s.router.POST("/api/v1/products", s.authorize("operator"), s.endpoint(s.saveProduct))
	s.router.PUT("/api/v1/products/:id", s.authorize("operator"), s.endpoint(s.saveProduct, "id"))
	s.router.GET("/api/v1/protocol-packages", s.authorize("viewer"), s.endpoint(s.protocolPackages))
	s.router.POST("/api/v1/protocol-packages", s.authorize("operator"), s.endpoint(s.saveProtocolPackage))
	s.router.PUT("/api/v1/protocol-packages/:id", s.authorize("operator"), s.endpoint(s.saveProtocolPackage, "id"))
	s.router.POST("/api/v1/protocol-packages/:id/artifact", s.authorize("operator"), s.endpoint(s.uploadProtocolArtifact, "id"))
	s.router.POST("/api/v1/protocol-packages/:id/test", s.authorize("operator"), s.endpoint(s.testProtocolPackage, "id"))
	s.router.GET("/api/v2/protocols", s.authorize("viewer"), s.endpoint(s.protocolDefinitionsV2))
	s.router.GET("/api/v2/protocol-source-template", s.authorize("viewer"), s.endpoint(s.protocolSourceTemplate))
	s.marketRoutes()
	s.router.GET("/api/v2/protocol-catalog", s.authorize("viewer"), s.endpoint(s.protocolCatalog))
	s.router.POST("/api/v2/protocol-catalog/install", s.authorize("admin"), s.endpoint(s.installCatalogProtocol))
	s.router.POST("/api/v2/protocols/:id/source-releases", s.authorize("operator"), s.endpoint(s.uploadProtocolSource, "id"))
	s.router.POST("/api/v2/protocols", s.authorize("operator"), s.endpoint(s.saveProtocolDefinitionV2))
	s.router.GET("/api/v2/protocols/:id/releases", s.authorize("viewer"), s.endpoint(s.protocolReleasesV2, "id"))
	s.router.GET("/api/v2/protocols/:id/releases/:version/source", s.authorize("operator"), s.endpoint(s.downloadProtocolSourceV2, "id", "version"))
	s.router.GET("/api/v2/protocols/:id/releases/:version/package", s.authorize("operator"), s.endpoint(s.downloadProtocolPackageV2, "id", "version"))
	s.router.POST("/api/v2/protocols/:id/releases", s.authorize("operator"), s.endpoint(s.createProtocolReleaseV2, "id"))
	s.router.POST("/api/v2/protocols/:id/package-releases", s.authorize("operator"), s.endpoint(s.uploadProtocolPackageV2, "id"))
	s.router.POST("/api/v2/protocols/:id/releases/:version/publish", s.authorize("operator"), s.endpoint(s.publishProtocolReleaseV2, "id", "version"))
	s.router.POST("/api/v2/products/:id/protocol-binding", s.authorize("operator"), s.endpoint(s.bindProductProtocolV2, "id"))
	s.router.POST("/api/v2/products/:id/protocol-binding/rollback", s.authorize("operator"), s.endpoint(s.rollbackProductProtocolV2, "id"))
	s.router.POST("/api/v2/modbus-tcp/import", s.authorize("operator"), s.endpoint(s.importModbusTCPV2))
	s.router.GET("/api/v2/device-access-profiles", s.authorize("viewer"), s.endpoint(s.deviceAccessProfilesV2))
	s.router.POST("/api/v2/device-access-profiles", s.authorize("operator"), s.endpoint(s.saveDeviceAccessProfileV2))
	s.router.PUT("/api/v2/device-access-profiles/:id", s.authorize("operator"), s.endpoint(s.saveDeviceAccessProfileV2, "id"))
	s.router.POST("/api/v2/device-access-profiles/:id/test", s.authorize("operator"), s.endpoint(s.testDeviceAccessProfileV2, "id"))
	s.router.POST("/api/v2/device-access-profiles/:id/devices/:deviceId/commands", s.authorize("operator"), s.endpoint(s.protocolDeviceCommand, "id", "deviceId"))
	s.router.GET("/api/v1/device-registry", s.authorize("viewer"), s.endpoint(s.deviceRegistry))
	s.router.POST("/api/v1/device-registry", s.authorize("operator"), s.endpoint(s.saveManagedDevice))
	s.router.PUT("/api/v1/device-registry/:id", s.authorize("operator"), s.endpoint(s.saveManagedDevice, "id"))
	s.router.POST("/api/v1/test-devices/provision", s.authorize("operator"), s.endpoint(s.provisionTestDevice))
	s.router.POST("/api/v1/discovered-devices/:id/register", s.authorize("operator"), s.endpoint(s.registerDiscoveredDevice, "id"))
	s.router.POST("/api/v1/device-registry/:id/credentials", s.authorize("admin"), s.endpoint(s.rotateDeviceCredential, "id"))
	s.router.GET("/api/v1/device-registry/:id/connection-guide", s.authorize("viewer"), s.endpoint(s.deviceConnectionGuide, "id"))
	s.router.POST("/api/v1/device-registry/:id/debug", s.authorize("operator"), s.endpoint(s.debugDeviceIngest, "id"))
	s.router.POST("/api/v1/raw-messages", s.authorize("operator"), s.endpoint(s.ingestRaw))
	s.router.GET("/api/v1/raw-messages", s.authorize("viewer"), s.endpoint(s.listRaw))
	s.router.POST("/api/v1/raw-messages/download", s.authorize("viewer"), s.endpoint(s.downloadRawBatch))
	s.router.GET("/api/v1/raw-messages/:id", s.authorize("viewer"), s.endpoint(s.rawDetail, "id"))
	s.router.GET("/api/v1/raw-messages/:id/download", s.authorize("viewer"), s.endpoint(s.downloadRaw, "id"))
	s.router.POST("/api/v1/raw-messages/replay", s.authorize("admin"), s.endpoint(s.startReplay))
	s.router.GET("/api/v1/replays/:id", s.authorize("viewer"), s.endpoint(s.getReplay, "id"))
	s.router.GET("/api/v1/devices", s.authorize("viewer"), s.endpoint(s.devices))
	s.router.GET("/api/v1/devices/:deviceId/latest", s.authorize("viewer"), s.endpoint(s.deviceLatest, "deviceId"))
	s.router.GET("/api/v1/devices/:deviceId/properties/history", s.authorize("viewer"), s.endpoint(s.history, "deviceId"))
	s.router.POST("/api/v1/device-states", s.authorize("operator"), s.endpoint(s.stateEvent))
	s.router.GET("/api/v1/rules", s.authorize("viewer"), s.endpoint(s.rules))
	s.router.POST("/api/v1/rules", s.authorize("operator"), s.endpoint(s.saveRule))
	s.router.PUT("/api/v1/rules/:id", s.authorize("operator"), s.endpoint(s.saveRule, "id"))
	s.router.DELETE("/api/v1/rules/:id", s.authorize("operator"), s.endpoint(s.deleteRule, "id"))
	s.router.GET("/api/v1/alarms", s.authorize("viewer"), s.endpoint(s.alarms))
	s.router.GET("/api/v1/alarms/:id", s.authorize("viewer"), s.endpoint(s.alarm, "id"))
	s.router.POST("/api/v1/alarms/:id/actions", s.authorize("operator"), s.endpoint(s.alarmAction, "id"))
	s.router.GET("/api/v1/backups/:id/files/:filename", s.authorize("admin"), s.endpoint(s.downloadBackupFile, "id", "filename"))
	s.router.GET("/api/v1/backups/:id/files", s.authorize("viewer"), s.endpoint(s.backupFiles, "id"))
	s.router.POST("/api/v1/backups/:id/restore-drill", s.authorize("admin"), s.endpoint(s.restoreBackup, "id"))
	s.router.GET("/api/v1/backups/:id", s.authorize("viewer"), s.endpoint(s.getBackup, "id"))
	s.router.GET("/api/v1/backups", s.authorize("viewer"), s.endpoint(s.listBackups))
	s.router.POST("/api/v1/backups", s.authorize("admin"), s.endpoint(s.runBackup))
	s.router.GET("/api/v1/ai/alarm-analysis/:alarmId", s.authorize("viewer"), s.endpoint(s.aiAnalysis, "alarmId"))
	s.router.POST("/api/v1/ai/alarm-analysis/:alarmId/run", s.authorize("operator"), s.endpoint(s.runAIAlarmAnalysis, "alarmId"))
	s.router.GET("/api/v1/ai/alarm-analysis/:alarmId/progress", s.authorize("viewer"), s.endpoint(s.aiAlarmAnalysisProgress, "alarmId"))
	s.router.GET("/api/v1/ai/alarm-analysis/:alarmId/progress/:jobId", s.authorize("viewer"), s.endpoint(s.aiAlarmAnalysisProgress, "alarmId", "jobId"))
	s.router.POST("/api/v1/ai/health-inspection", s.authorize("viewer"), s.endpoint(s.healthInspection))
	s.router.POST("/api/v1/ai/health-inspection/run", s.authorize("viewer"), s.endpoint(s.runHealthInspection))
	s.router.GET("/api/v1/ai/health-inspection/progress", s.authorize("viewer"), s.endpoint(s.healthInspectionProgress))
	s.router.GET("/api/v1/ai/health-inspection/progress/:jobId", s.authorize("viewer"), s.endpoint(s.healthInspectionProgress, "jobId"))
	s.router.POST("/api/v1/ai/health-inspection/pdf", s.authorize("viewer"), s.endpoint(s.healthInspectionPDF))
	s.router.POST("/api/v1/ai/protocol-assistant/generate", s.authorize("operator"), s.endpoint(s.generateProtocolAssistant))
	s.router.POST("/api/v1/ai/protocol-assistant/preview", s.authorize("operator"), s.endpoint(s.previewProtocolAssistant))
	s.router.POST("/api/v1/ai/protocol-assistant/publish", s.authorize("operator"), s.endpoint(s.publishProtocolAssistant))
	s.router.GET("/api/v1/ai/providers", s.authorize("viewer"), s.endpoint(s.aiProviders))
	s.router.GET("/api/v1/ai/providers/config", s.authorize("viewer"), s.endpoint(s.aiProviderConfig))
	s.router.PUT("/api/v1/ai/providers/config", s.authorize("admin"), s.endpoint(s.updateAIProviderConfig))
	s.router.POST("/api/v1/ai/providers/test", s.authorize("admin"), s.endpoint(s.testAIProvider))
	s.router.GET("/api/v1/ai/workflows", s.authorize("viewer"), s.endpoint(s.aiWorkflows))
	s.router.GET("/api/v1/ai/workflows/admin", s.authorize("admin"), s.endpoint(s.aiWorkflowManifests))
	s.router.POST("/api/v1/ai/workflows", s.authorize("admin"), s.endpoint(s.saveAIWorkflow))
	s.router.PUT("/api/v1/ai/workflows/:id", s.authorize("admin"), s.endpoint(s.updateAIWorkflow, "id"))
	s.router.DELETE("/api/v1/ai/workflows/:id", s.authorize("admin"), s.endpoint(s.deleteAIWorkflow, "id"))
	s.router.GET("/api/v1/ai/workflows/:id/knowledge-binding", s.authorize("viewer"), s.endpoint(s.workflowKnowledgeBinding, "id"))
	s.router.PUT("/api/v1/ai/workflows/:id/knowledge-binding", s.authorize("operator"), s.endpoint(s.workflowKnowledgeBinding, "id"))
	s.router.POST("/api/v1/ai/chat", s.authorize("viewer"), s.endpoint(s.aiChat))
	s.router.POST("/api/v1/ai/chat/stream", s.authorize("viewer"), s.endpoint(s.aiChatStream))
	s.router.POST("/api/v1/ai/rule-draft", s.authorize("operator"), s.endpoint(s.aiRuleDraft))
	s.router.POST("/api/v1/ai/reports", s.authorize("viewer"), s.endpoint(s.aiReport))
	s.router.GET("/api/v1/knowledge/documents", s.authorize("viewer"), s.endpoint(s.knowledgeDocs))
	s.router.GET("/api/v1/knowledge/documents/:id", s.authorize("viewer"), s.endpoint(s.knowledgeDocumentDetail, "id"))
	s.router.POST("/api/v1/knowledge/documents", s.authorize("operator"), s.endpoint(s.knowledgeUpload))
	s.router.POST("/api/v1/mqtt/token", s.authorize("viewer"), s.endpoint(s.mqttToken))
	s.router.POST("/api/v1/mqtt/load-token", s.authorize("admin"), s.endpoint(s.mqttLoadToken))
	s.router.POST("/api/v1/device-mqtt/token", s.endpoint(s.deviceMQTTToken))
	mcpHandler := gin.WrapH(mcpserver.New(s.engine))
	s.router.GET("/mcp", s.authorize("viewer"), mcpHandler)
	s.router.POST("/mcp", s.authorize("viewer"), mcpHandler)
	s.router.DELETE("/mcp", s.authorize("viewer"), mcpHandler)
	harnessMCPHandler := gin.WrapH(mcpserver.NewHarness(s.engine))
	s.router.POST("/mcp/harness", s.authorizeHarness(), harnessMCPHandler)
	s.router.NoRoute(func(c *gin.Context) { ginProblem(c, http.StatusNotFound, "route not found") })
	s.router.NoMethod(func(c *gin.Context) { ginProblem(c, http.StatusMethodNotAllowed, "method not allowed") })
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TenantID string `json:"tenantId"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	if in.Username != s.cfg.AdminUser || in.Password != s.cfg.AdminPassword {
		problem(w, 401, "invalid credentials")
		return
	}
	in.TenantID = strings.TrimSpace(in.TenantID)
	if in.TenantID == "" {
		in.TenantID = "tenant_001"
	}
	if !adminTenantAllowed(s.cfg.AdminTenants, in.TenantID) {
		problem(w, http.StatusForbidden, "admin tenant is not allowed")
		return
	}
	token, _ := s.auth.Issue(in.Username, in.TenantID, "admin", nil, 8*time.Hour)
	write(w, 200, map[string]any{"accessToken": token, "expiresIn": 28800, "tenantId": in.TenantID, "role": "admin"})
}
func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	checks := map[string]string{}
	status := 200
	checksToRun := map[string]func(context.Context) error{"repository": s.engine.Repo.Health, "archive": s.engine.Archive.Health, "eventBus": s.engine.Bus.Health, "realtime": s.engine.Realtime.Health}
	if s.engine.KB != nil {
		checksToRun["knowledge"] = s.engine.KB.Health
	}
	for name, check := range checksToRun {
		if err := check(ctx); err != nil {
			checks[name] = err.Error()
			status = 503
		} else {
			checks[name] = "ok"
		}
	}
	write(w, status, map[string]any{"status": map[bool]string{true: "ok", false: "degraded"}[status == 200], "checks": checks})
}
func (s *Server) products(w http.ResponseWriter, r *http.Request) {
	pagination := parseListPagination(r)
	items, total, err := s.engine.Repo.ListProductsPage(r.Context(), claims(r).TenantID, pagination.PageSize, pagination.Offset)
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	writeList(w, 200, items, total, pagination, nil)
}
func (s *Server) saveProduct(w http.ResponseWriter, r *http.Request) {
	var v model.Product
	if decode(w, r, &v) != nil {
		return
	}
	c := claims(r)
	v.TenantID = c.TenantID
	if id := r.PathValue("id"); id != "" {
		v.ID = id
	}
	if v.ID == "" {
		v.ID = "product_" + randomHex(6)
	}
	if v.Name == "" || v.ProtocolPackageID == "" {
		problem(w, 422, "name and protocolPackageId are required")
		return
	}
	if err := onboarding.ValidateThingModel(v.ThingModel); err != nil {
		problem(w, 422, err.Error())
		return
	}
	pkg, err := s.engine.Repo.GetProtocolPackage(r.Context(), c.TenantID, v.ProtocolPackageID)
	if err != nil {
		problem(w, 422, "protocol package not found")
		return
	}
	if v.Transport == "" {
		v.Transport = pkg.Transport
	}
	if v.PayloadFormat == "" {
		v.PayloadFormat = pkg.PayloadFormat
	}
	if v.Status == "" {
		v.Status = "ENABLED"
	}
	now := time.Now().UnixMilli()
	if old, getErr := s.engine.Repo.GetProduct(r.Context(), c.TenantID, v.ID); getErr == nil {
		v.CreatedAt = old.CreatedAt
	}
	if v.CreatedAt == 0 {
		v.CreatedAt = now
	}
	v.UpdatedAt = now
	if err = s.engine.Repo.SaveProduct(r.Context(), v); err != nil {
		problem(w, 500, err.Error())
		return
	}
	s.audit(r, "product.save", "product", v.ID, map[string]any{"status": v.Status})
	write(w, 201, v)
}
func (s *Server) protocolPackages(w http.ResponseWriter, r *http.Request) {
	pagination := parseListPagination(r)
	items, total, err := s.engine.Repo.ListProtocolPackagesPage(r.Context(), claims(r).TenantID, pagination.PageSize, pagination.Offset)
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	writeList(w, 200, items, total, pagination, map[string]any{"parserTypes": parser.ManagedParserTypes()})
}
func (s *Server) saveProtocolPackage(w http.ResponseWriter, r *http.Request) {
	var v model.ProtocolPackage
	if decode(w, r, &v) != nil {
		return
	}
	c := claims(r)
	v.TenantID = c.TenantID
	if id := r.PathValue("id"); id != "" {
		v.ID = id
	}
	if v.ID == "" {
		v.ID = "protocol_" + randomHex(6)
	}
	if v.Name == "" || v.ParserType == "" {
		problem(w, 422, "name and parserType are required")
		return
	}
	if !parser.ManagedParserType(v.ParserType) {
		problem(w, 422, "专用解析器仅供已有绑定及历史回放；新增或更新协议请上传 Go 源码包")
		return
	}
	if v.Version == "" {
		v.Version = "1.0.0"
	}
	if v.Protocol == "" {
		v.Protocol = "json"
	}
	if v.Transport == "" {
		v.Transport = "MQTT"
	}
	if v.PayloadFormat == "" {
		v.PayloadFormat = "json"
	}
	if v.Status == "" {
		v.Status = "DRAFT"
	}
	if v.Status != "DRAFT" && v.Status != "PUBLISHED" && v.Status != "DISABLED" {
		problem(w, 422, "status must be DRAFT, PUBLISHED or DISABLED")
		return
	}
	now := time.Now().UnixMilli()
	if old, getErr := s.engine.Repo.GetProtocolPackage(r.Context(), c.TenantID, v.ID); getErr == nil {
		v.CreatedAt = old.CreatedAt
		if v.Config == nil {
			v.Config = map[string]any{}
		}
		if _, hasArtifact := v.Config["artifact"]; !hasArtifact {
			if artifact, exists := old.Config["artifact"]; exists {
				v.Config["artifact"] = artifact
			}
		}
	}
	if v.CreatedAt == 0 {
		v.CreatedAt = now
	}
	v.UpdatedAt = now
	if err := s.engine.Repo.SaveProtocolPackage(r.Context(), v); err != nil {
		problem(w, 500, err.Error())
		return
	}
	s.audit(r, "protocol.save", "protocolPackage", v.ID, map[string]any{"version": v.Version, "status": v.Status})
	write(w, 201, v)
}

const maxProtocolArtifactUpload = int64(64 << 20)

// uploadProtocolArtifact stores a compiled Go worker next to the platform
// data directory and records only its relative path and digest in the protocol
// package. Source code is deliberately not compiled in the API process: build
// it in a controlled CI/worker environment, then upload the resulting binary.
func (s *Server) uploadProtocolArtifact(w http.ResponseWriter, r *http.Request) {
	tenant := claims(r).TenantID
	packageID := r.PathValue("id")
	pkg, err := s.engine.Repo.GetProtocolPackage(r.Context(), tenant, packageID)
	if err != nil {
		problem(w, http.StatusNotFound, "protocol package not found")
		return
	}
	if pkg.ParserType != parser.GoProtocolParserName {
		problem(w, http.StatusUnprocessableEntity, "protocol package parserType must be go_protocol_parser")
		return
	}
	if err := r.ParseMultipartForm(maxProtocolArtifactUpload + (1 << 20)); err != nil {
		problem(w, http.StatusBadRequest, "invalid multipart artifact upload")
		return
	}
	file, header, err := r.FormFile("artifact")
	if err != nil {
		file, header, err = r.FormFile("file")
	}
	if err != nil {
		problem(w, http.StatusUnprocessableEntity, "artifact file is required")
		return
	}
	defer file.Close()
	if header == nil || strings.TrimSpace(header.Filename) == "" || strings.ContainsAny(header.Filename, `/\\`) {
		problem(w, http.StatusUnprocessableEntity, "artifact filename is invalid")
		return
	}
	if pkg.Version == "" {
		pkg.Version = "1.0.0"
	}
	for _, segment := range []string{tenant, packageID, pkg.Version} {
		if !safeArtifactSegment(segment) {
			problem(w, http.StatusUnprocessableEntity, "protocol package path segment is invalid")
			return
		}
	}
	root, err := filepath.Abs(s.cfg.DataDir)
	if err != nil {
		problem(w, http.StatusInternalServerError, "resolve protocol artifact directory")
		return
	}
	directory := filepath.Join(root, "protocol-packages", tenant, packageID, pkg.Version)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		problem(w, http.StatusInternalServerError, "create protocol artifact directory")
		return
	}
	temporary, err := os.CreateTemp(directory, ".artifact-*")
	if err != nil {
		problem(w, http.StatusInternalServerError, "create protocol artifact")
		return
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	hash := sha256.New()
	writer := io.MultiWriter(temporary, hash)
	written, copyErr := io.Copy(writer, io.LimitReader(file, maxProtocolArtifactUpload+1))
	if closeErr := temporary.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		problem(w, http.StatusBadRequest, "read protocol artifact")
		return
	}
	if written <= 0 || written > maxProtocolArtifactUpload {
		problem(w, http.StatusRequestEntityTooLarge, "protocol artifact must be between 1 and 64 MiB")
		return
	}
	artifactName := "artifact"
	if runtime.GOOS == "windows" {
		artifactName += ".exe"
	}
	target := filepath.Join(directory, artifactName)
	if err := os.Chmod(temporaryPath, 0o700); err != nil {
		problem(w, http.StatusInternalServerError, "set protocol artifact permissions")
		return
	}
	if runtime.GOOS == "windows" {
		if removeErr := os.Remove(target); removeErr != nil && !os.IsNotExist(removeErr) {
			problem(w, http.StatusInternalServerError, "replace protocol artifact")
			return
		}
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		problem(w, http.StatusInternalServerError, "activate protocol artifact")
		return
	}
	relative, err := filepath.Rel(root, target)
	if err != nil {
		problem(w, http.StatusInternalServerError, "store protocol artifact path")
		return
	}
	if pkg.Config == nil {
		pkg.Config = map[string]any{}
	}
	pkg.Config["artifact"] = map[string]any{
		"path": filepath.ToSlash(relative), "filename": header.Filename,
		"sha256": hex.EncodeToString(hash.Sum(nil)), "size": written,
		"protocol": "json-lines-v1", "uploadedAt": time.Now().UnixMilli(),
	}
	pkg.UpdatedAt = time.Now().UnixMilli()
	if err := s.engine.Repo.SaveProtocolPackage(r.Context(), pkg); err != nil {
		problem(w, http.StatusInternalServerError, "save protocol artifact metadata")
		return
	}
	s.audit(r, "protocol.artifact.upload", "protocolPackage", pkg.ID, map[string]any{"sha256": pkg.Config["artifact"].(map[string]any)["sha256"], "size": written})
	write(w, http.StatusCreated, pkg)
}

func safeArtifactSegment(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, `/\\`) && !strings.Contains(value, "..")
}
func (s *Server) testProtocolPackage(w http.ResponseWriter, r *http.Request) {
	pkg, err := s.engine.Repo.GetProtocolPackage(r.Context(), claims(r).TenantID, r.PathValue("id"))
	if err != nil {
		problem(w, 404, "protocol package not found")
		return
	}
	var in struct {
		ProductID string          `json:"productId"`
		DeviceID  string          `json:"deviceId"`
		Payload   json.RawMessage `json:"payload"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	if len(in.Payload) == 0 {
		problem(w, 422, "payload is required")
		return
	}
	if in.ProductID == "" {
		in.ProductID = "protocol_test"
	}
	if in.DeviceID == "" {
		in.DeviceID = "device_test"
	}
	raw := model.RawMessage{MessageID: "raw_test_" + randomHex(6), TenantID: pkg.TenantID, ProductID: in.ProductID, DeviceID: in.DeviceID, Protocol: pkg.Protocol, Transport: pkg.Transport, PayloadFormat: pkg.PayloadFormat, Payload: in.Payload, ReceivedAt: time.Now().UnixMilli()}
	msg, err := s.engine.Parsers.ParseWithConfig(pkg.ParserType, pkg.Config, raw)
	if err != nil {
		write(w, 200, map[string]any{"success": false, "error": err.Error(), "raw": raw})
		return
	}
	write(w, 200, map[string]any{"success": true, "standardMessage": msg})
}
func (s *Server) deviceRegistry(w http.ResponseWriter, r *http.Request) {
	tenantID := claims(r).TenantID
	pagination := parseListPagination(r)
	items, total, err := s.engine.Repo.ListManagedDevicesPage(r.Context(), tenantID, pagination.PageSize, pagination.Offset)
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	deviceIDs := make([]string, 0, len(items))
	for _, item := range items {
		deviceIDs = append(deviceIDs, item.ID)
	}
	childCounts, err := s.engine.Repo.CountManagedDeviceChildren(r.Context(), tenantID, deviceIDs)
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, v := range items {
		row := map[string]any{"device": v, "childCount": childCounts[v.ID]}
		if state, stateErr := s.engine.Repo.GetDeviceState(r.Context(), tenantID, v.ID); stateErr == nil {
			row["runtimeState"] = state
		}
		out = append(out, row)
	}
	writeList(w, 200, out, total, pagination, nil)
}
func (s *Server) saveManagedDevice(w http.ResponseWriter, r *http.Request) {
	var v model.ManagedDevice
	if decode(w, r, &v) != nil {
		return
	}
	c := claims(r)
	v.TenantID = c.TenantID
	if id := r.PathValue("id"); id != "" {
		v.ID = id
	}
	if v.ID == "" {
		v.ID = "device_" + randomHex(6)
	}
	if v.Name == "" || v.ProductID == "" {
		problem(w, 422, "name and productId are required")
		return
	}
	product, err := s.engine.Repo.GetProduct(r.Context(), c.TenantID, v.ProductID)
	if err != nil {
		problem(w, 422, "product not found")
		return
	}
	now := time.Now().UnixMilli()
	credential := model.DeviceCredential{}
	if old, err := s.engine.Repo.GetManagedDevice(r.Context(), c.TenantID, v.ID); err == nil {
		v.AccessKey, v.SecretHash, v.SecretHint, v.CreatedAt = old.AccessKey, old.SecretHash, old.SecretHint, old.CreatedAt
		if v.Tags == nil {
			v.Tags = map[string]string{}
		}
		for _, key := range []string{"onboardingRequestHash", "connector", "connectorProfileId"} {
			delete(v.Tags, key)
			if value := old.Tags[key]; value != "" {
				v.Tags[key] = value
			}
		}
		if v.RegistrationSource == "" {
			v.RegistrationSource = old.RegistrationSource
		}
		if old.AutoRegistered {
			v.AutoRegistered = true
		}
	} else {
		credential = newDeviceCredential()
		v.AccessKey = credential.AccessKey
		v.SecretHash = secretHash(credential.Secret)
		v.SecretHint = credential.Secret[len(credential.Secret)-6:]
		v.CreatedAt = now
	}
	if v.Status == "" {
		v.Status = "ENABLED"
	}
	if v.DeviceRole == "" {
		if product.Category == "gateway" {
			v.DeviceRole = "GATEWAY"
		} else {
			v.DeviceRole = "DIRECT"
		}
	}
	if v.DeviceRole != "DIRECT" && v.DeviceRole != "GATEWAY" && v.DeviceRole != "CHILD" {
		problem(w, 422, "deviceRole must be DIRECT, GATEWAY or CHILD")
		return
	}
	if v.RegistrationSource == "" {
		v.RegistrationSource = "MANUAL"
	}
	if v.DeviceRole == "CHILD" {
		if v.GatewayID == "" || v.GatewayID == v.ID {
			problem(w, 422, "a child device must reference a different gateway")
			return
		}
		gateway, gatewayErr := s.engine.Repo.GetManagedDevice(r.Context(), c.TenantID, v.GatewayID)
		if gatewayErr != nil {
			problem(w, 422, "gateway not found")
			return
		}
		gatewayProduct, productErr := s.engine.Repo.GetProduct(r.Context(), c.TenantID, gateway.ProductID)
		if productErr != nil || gateway.DeviceRole != "GATEWAY" && gatewayProduct.Category != "gateway" {
			problem(w, 422, "selected parent device is not a gateway")
			return
		}
	} else {
		v.GatewayID = ""
	}
	v.UpdatedAt = now
	if err := s.engine.Repo.SaveManagedDevice(r.Context(), v); err != nil {
		problem(w, 500, err.Error())
		return
	}
	s.audit(r, "device.save", "device", v.ID, map[string]any{"productId": v.ProductID, "status": v.Status})
	result := map[string]any{"device": v}
	if credential.Secret != "" {
		result["credential"] = credential
	}
	write(w, 201, result)
}
func (s *Server) registerDiscoveredDevice(w http.ResponseWriter, r *http.Request) {
	c := claims(r)
	id := r.PathValue("id")
	if _, err := s.engine.Repo.GetManagedDevice(r.Context(), c.TenantID, id); err == nil {
		problem(w, 409, "device is already registered")
		return
	}
	state, err := s.engine.Repo.GetDeviceState(r.Context(), c.TenantID, id)
	if err != nil {
		problem(w, 404, "discovered device not found")
		return
	}
	product, err := s.engine.Repo.GetProduct(r.Context(), c.TenantID, state.ProductID)
	if err != nil {
		problem(w, 422, "register its product before registering this device")
		return
	}
	credential := newDeviceCredential()
	now := time.Now().UnixMilli()
	role := "DIRECT"
	if product.Category == "gateway" {
		role = "GATEWAY"
	}
	device := model.ManagedDevice{ID: id, TenantID: c.TenantID, ProductID: state.ProductID, Name: "发现设备 " + id, Status: "ENABLED", DeviceRole: role, RegistrationSource: "DISCOVERY", AccessKey: credential.AccessKey, SecretHash: secretHash(credential.Secret), SecretHint: credential.Secret[len(credential.Secret)-6:], CreatedAt: now, UpdatedAt: now}
	if err = s.engine.Repo.SaveManagedDevice(r.Context(), device); err != nil {
		problem(w, 500, err.Error())
		return
	}
	s.audit(r, "device.discovery.register", "device", id, map[string]any{"productId": state.ProductID})
	write(w, 201, map[string]any{"device": device, "credential": credential})
}
func (s *Server) rotateDeviceCredential(w http.ResponseWriter, r *http.Request) {
	if !s.operationDevice(w, r) {
		return
	}
	c, v, e := s.onboarding.ChangeCredential(r.Context(), claims(r).TenantID, r.PathValue("id"), true)
	if e != nil {
		problem(w, 500, e.Error())
		return
	}
	s.audit(r, "device.credential.rotate", "device", r.PathValue("id"), nil)
	write(w, 200, map[string]any{"deviceId": r.PathValue("id"), "credential": c, "revocation": v})
}
func (s *Server) deviceConnectionGuide(w http.ResponseWriter, r *http.Request) {
	c := claims(r)
	v, err := s.engine.Repo.GetManagedDevice(r.Context(), c.TenantID, r.PathValue("id"))
	if err != nil {
		problem(w, 404, "device not found")
		return
	}
	p, err := s.engine.Repo.GetProduct(r.Context(), c.TenantID, v.ProductID)
	if err != nil {
		problem(w, 404, "product not found")
		return
	}
	if info := s.deviceAccessInfo(v); info != nil {
		write(w, 200, map[string]any{
			"deviceId": v.ID, "productId": p.ID, "deviceRole": v.DeviceRole, "accessKey": v.AccessKey, "secretHint": v.SecretHint,
			"http":            map[string]any{"method": "POST", "url": info["httpUrl"], "headers": map[string]string{"X-Device-Key": v.AccessKey, "X-Device-Secret": "<仅创建或轮换时显示>", "Content-Type": "application/json"}},
			"mqtt":            map[string]any{"broker": info["mqttBroker"], "topic": info["upTopic"], "downTopic": info["downTopic"], "clientId": info["clientId"], "username": info["username"], "tokenEndpoint": info["tokenEndpoint"]},
			"payloadTemplate": info["sample"],
		})
		return
	}
	pkg, _ := s.engine.Repo.GetProtocolPackage(r.Context(), c.TenantID, p.ProtocolPackageID)
	result := map[string]any{"deviceId": v.ID, "productId": p.ID, "deviceRole": v.DeviceRole, "gatewayId": v.GatewayID, "accessKey": v.AccessKey, "secretHint": v.SecretHint, "http": map[string]any{"method": "POST", "url": "/api/v1/device-ingest/" + v.ID, "headers": map[string]string{"X-Device-Key": v.AccessKey, "X-Device-Secret": "<仅创建或轮换时显示>", "Content-Type": "application/json"}}, "mqtt": map[string]any{"broker": publicEndpoint(s.cfg.MQTTPublicURL), "topic": fmt.Sprintf("/external/raw/%s/%s/%s", c.TenantID, p.ID, v.ID), "tokenEndpoint": "/api/v1/device-mqtt/token", "tokenHeaders": map[string]string{"X-Device-Key": v.AccessKey, "X-Device-Secret": "<仅创建或轮换时显示>"}}, "payloadTemplate": map[string]any{"messageId": "raw_<unique>", "tenantId": c.TenantID, "productId": p.ID, "deviceId": v.ID, "protocol": pkg.Protocol, "transport": pkg.Transport, "payloadFormat": pkg.PayloadFormat, "payload": map[string]any{"properties": map[string]any{"temperature": 25.5}}}}
	if v.DeviceRole == "GATEWAY" || p.Category == "gateway" {
		result["gateway"] = map[string]any{"autoRegisterChildren": true, "description": "网关上报一个尚未注册的子设备时，平台自动注册并建立关联", "childPayloadTemplate": map[string]any{"messageId": "raw_<unique>", "deviceId": "child_device_001", "deviceName": "一号子设备", "productId": "<child_product_id>", "payload": map[string]any{"properties": map[string]any{"temperature": 25.5}}}}
	}
	write(w, 200, result)
}
func (s *Server) debugDeviceIngest(w http.ResponseWriter, r *http.Request) {
	c := claims(r)
	v, err := s.engine.Repo.GetManagedDevice(r.Context(), c.TenantID, r.PathValue("id"))
	if err != nil {
		problem(w, 404, "device not found")
		return
	}
	var raw model.RawMessage
	if decode(w, r, &raw) != nil {
		return
	}
	if err = s.prepareManagedRaw(r.Context(), &raw, v); err != nil {
		problem(w, 422, err.Error())
		return
	}
	idx, created, err := s.engine.IngestRaw(r.Context(), raw)
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	s.audit(r, "device.debug.ingest", "device", v.ID, map[string]any{"messageId": idx.MessageID})
	write(w, map[bool]int{true: 201, false: 200}[created], map[string]any{"created": created, "archive": idx})
}
func (s *Server) deviceIngest(w http.ResponseWriter, r *http.Request) {
	accessKey, secret := r.Header.Get("X-Device-Key"), r.Header.Get("X-Device-Secret")
	v, err := s.engine.Repo.GetManagedDeviceByAccessKey(r.Context(), accessKey)
	if err != nil || v.ID != r.PathValue("deviceId") || v.Status != "ENABLED" || !hmac.Equal([]byte(v.SecretHash), []byte(secretHash(secret))) {
		problem(w, 401, "invalid or disabled device credential")
		return
	}
	if tenantID := r.Header.Get("X-Tenant-ID"); tenantID != "" && tenantID != v.TenantID {
		problem(w, 401, "invalid or disabled device credential")
		return
	}
	var raw model.RawMessage
	if decode(w, r, &raw) != nil {
		return
	}
	if err = s.prepareManagedRaw(r.Context(), &raw, v); err != nil {
		problem(w, 422, err.Error())
		return
	}
	idx, created, err := s.engine.IngestRaw(r.Context(), raw)
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	write(w, map[bool]int{true: 201, false: 200}[created], map[string]any{"created": created, "messageId": idx.MessageID, "receivedAt": idx.ReceivedAt})
}
func (s *Server) prepareManagedRaw(ctx context.Context, raw *model.RawMessage, device model.ManagedDevice) error {
	if device.Tags["connector"] == "HTTP" || device.Tags["connector"] == "MQTT" {
		return fmt.Errorf("standard devices must use the authenticated standard ingress endpoint")
	}
	targetProductID := device.ProductID
	if raw.DeviceID != "" && raw.DeviceID != device.ID {
		if raw.ProductID == "" {
			return fmt.Errorf("child productId is required")
		}
		raw.GatewayID = device.ID
		targetProductID = raw.ProductID
		raw.Source = "gateway"
	} else {
		raw.DeviceID = device.ID
		raw.GatewayID = ""
		raw.Source = "managed-device"
	}
	product, err := s.engine.Repo.GetProduct(ctx, device.TenantID, targetProductID)
	if err != nil || product.Status != "ENABLED" {
		return fmt.Errorf("product is not enabled")
	}
	pkg, err := s.engine.Repo.GetProtocolPackage(ctx, device.TenantID, product.ProtocolPackageID)
	if err != nil || pkg.Status != "PUBLISHED" {
		return fmt.Errorf("protocol package is not published")
	}
	raw.TenantID, raw.ProductID = device.TenantID, product.ID
	raw.Protocol, raw.Transport, raw.PayloadFormat = pkg.Protocol, pkg.Transport, pkg.PayloadFormat
	return nil
}
func (s *Server) ingestRaw(w http.ResponseWriter, r *http.Request) {
	var v model.RawMessage
	if decode(w, r, &v) != nil {
		return
	}
	c := claims(r)
	v.TenantID = tenant(c, v.TenantID)
	start := time.Now()
	idx, created, err := s.engine.IngestRaw(r.Context(), v)
	s.metrics.ObserveMS("raw_archive_latency_ms", start)
	s.metrics.ObserveMS("storage_latency_ms", start)
	if err != nil {
		s.metrics.Inc("raw_archive_failed_total")
		problem(w, 422, err.Error())
		return
	}
	write(w, map[bool]int{true: 201, false: 200}[created], map[string]any{"created": created, "archive": idx})
}
func (s *Server) listRaw(w http.ResponseWriter, r *http.Request) {
	c := claims(r)
	q := r.URL.Query()
	pagination := parseListPagination(r)
	filter := ports.RawFilter{TenantID: c.TenantID, ProductID: q.Get("productId"), DeviceID: q.Get("deviceId"), Start: i64(q.Get("start")), End: i64(q.Get("end")), Limit: pagination.PageSize, Offset: pagination.Offset}
	items, err := s.engine.Repo.ListRawIndexes(r.Context(), filter)
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	total, err := s.engine.Repo.CountRawIndexes(r.Context(), filter)
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	for i := range items {
		if message, parseErr := s.engine.Repo.GetStandardMessageByRaw(r.Context(), c.TenantID, items[i].MessageID); parseErr == nil {
			items[i].Parsed = true
			items[i].ParsedMessageType = string(message.MessageType)
			items[i].Parser = message.Parser
		}
	}
	writeList(w, 200, items, total, pagination, nil)
}
func (s *Server) rawDetail(w http.ResponseWriter, r *http.Request) {
	idx, err := s.engine.Repo.GetRawIndex(r.Context(), claims(r).TenantID, r.PathValue("id"))
	if err != nil {
		problem(w, 404, "raw message not found")
		return
	}
	raw, err := s.engine.GetRaw(r.Context(), idx)
	if err != nil {
		problem(w, 500, "raw archive could not be read")
		return
	}
	result := map[string]any{"archive": idx, "message": raw, "parseStatus": "UNPARSED", "parseError": idx.ParseError}
	if idx.ParseError != "" {
		result["parseStatus"] = "FAILED"
	}
	if standard, parseErr := s.engine.Repo.GetStandardMessageByRaw(r.Context(), claims(r).TenantID, idx.MessageID); parseErr == nil {
		result["parseStatus"] = "PARSED"
		result["standardMessage"] = standard
	}
	write(w, 200, result)
}
func (s *Server) downloadRaw(w http.ResponseWriter, r *http.Request) {
	idx, err := s.engine.Repo.GetRawIndex(r.Context(), claims(r).TenantID, r.PathValue("id"))
	if err != nil {
		problem(w, 404, "raw message not found")
		return
	}
	raw, err := s.engine.GetRaw(r.Context(), idx)
	if err != nil {
		problem(w, 500, "raw archive could not be read")
		return
	}
	body, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	filename := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '_'
	}, idx.MessageID) + ".json"
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("X-Content-SHA256", idx.PayloadHash)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(body, '\n'))
	s.audit(r, "raw.download", "raw-message", idx.MessageID, map[string]any{"payloadHash": idx.PayloadHash})
}
func (s *Server) downloadRawBatch(w http.ResponseWriter, r *http.Request) {
	var in struct {
		MessageIDs []string `json:"messageIds"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	if len(in.MessageIDs) == 0 {
		problem(w, 400, "at least one messageId is required")
		return
	}
	if len(in.MessageIDs) > 500 {
		problem(w, 422, "no more than 500 raw messages may be downloaded at once")
		return
	}
	type archivedRaw struct {
		Index   model.RawArchiveIndex
		Message model.RawMessage
	}
	items := make([]archivedRaw, 0, len(in.MessageIDs))
	seen := make(map[string]struct{}, len(in.MessageIDs))
	totalPayloadSize := 0
	for _, id := range in.MessageIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		idx, err := s.engine.Repo.GetRawIndex(r.Context(), claims(r).TenantID, id)
		if err != nil {
			problem(w, 404, "raw message not found: "+id)
			return
		}
		totalPayloadSize += idx.PayloadSize
		if totalPayloadSize > 100*1024*1024 {
			problem(w, 413, "selected raw messages exceed the 100 MiB batch limit")
			return
		}
		raw, err := s.engine.GetRaw(r.Context(), idx)
		if err != nil {
			problem(w, 500, "raw archive could not be read: "+id)
			return
		}
		items = append(items, archivedRaw{Index: idx, Message: raw})
	}
	if len(items) == 0 {
		problem(w, 400, "at least one valid messageId is required")
		return
	}
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	manifest := make([]model.RawArchiveIndex, 0, len(items))
	for i, item := range items {
		body, err := json.MarshalIndent(item.Message, "", "  ")
		if err != nil {
			problem(w, 500, err.Error())
			return
		}
		name := fmt.Sprintf("报文/%03d_%s.json", i+1, safeAttachmentName(item.Index.MessageID))
		file, err := zw.Create(name)
		if err != nil {
			problem(w, 500, err.Error())
			return
		}
		if _, err = file.Write(append(body, '\n')); err != nil {
			problem(w, 500, err.Error())
			return
		}
		manifest = append(manifest, item.Index)
	}
	manifestBody, _ := json.MarshalIndent(map[string]any{"exportedAt": time.Now().UnixMilli(), "count": len(items), "items": manifest}, "", "  ")
	manifestFile, err := zw.Create("清单.json")
	if err == nil {
		_, err = manifestFile.Write(append(manifestBody, '\n'))
	}
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	if err = zw.Close(); err != nil {
		problem(w, 500, err.Error())
		return
	}
	filename := fmt.Sprintf("原始报文_%s_%d条.zip", time.Now().Format("20060102_150405"), len(items))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="raw-messages.zip"; filename*=UTF-8''%s`, url.QueryEscape(filename)))
	w.Header().Set("X-Archive-Count", strconv.Itoa(len(items)))
	w.Header().Set("Content-Length", strconv.Itoa(archive.Len()))
	w.WriteHeader(http.StatusOK)
	_, _ = archive.WriteTo(w)
	s.audit(r, "raw.download.batch", "raw-message", "batch", map[string]any{"count": len(items), "payloadBytes": totalPayloadSize})
}

func safeAttachmentName(value string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '_'
	}, value)
}
func (s *Server) startReplay(w http.ResponseWriter, r *http.Request) {
	var v model.ReplayRequest
	if decode(w, r, &v) != nil {
		return
	}
	c := claims(r)
	v.TenantID = c.TenantID
	v.CreatedBy = c.Username
	task, err := s.engine.StartReplay(r.Context(), v)
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	write(w, 202, task)
}
func (s *Server) getReplay(w http.ResponseWriter, r *http.Request) {
	v, err := s.engine.Repo.GetReplay(r.Context(), r.PathValue("id"))
	if err != nil {
		problem(w, 404, "replay not found")
		return
	}
	if v.TenantID != claims(r).TenantID {
		problem(w, 404, "replay not found")
		return
	}
	write(w, 200, v)
}
func (s *Server) devices(w http.ResponseWriter, r *http.Request) {
	pagination := parseListPagination(r)
	tenantID := claims(r).TenantID
	unregisteredOnly := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("unregistered")), "true")
	var items []model.DeviceState
	var total int
	var err error
	if unregisteredOnly {
		items, total, err = s.engine.Repo.ListUnregisteredDeviceStatesPage(r.Context(), tenantID, pagination.PageSize, pagination.Offset)
	} else {
		items, total, err = s.engine.Repo.ListDeviceStatesPage(r.Context(), tenantID, pagination.PageSize, pagination.Offset)
	}
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	_, online, err := s.engine.Repo.CountDeviceStates(r.Context(), tenantID, unregisteredOnly)
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	writeList(w, 200, items, total, pagination, map[string]any{"online": online, "offline": total - online, "unregistered": unregisteredOnly})
}
func (s *Server) deviceLatest(w http.ResponseWriter, r *http.Request) {
	tenant, device := claims(r).TenantID, r.PathValue("deviceId")
	v, err := s.engine.Repo.GetDeviceState(r.Context(), tenant, device)
	if err != nil {
		problem(w, 404, "device state not found")
		return
	}
	out := map[string]any{"state": v}
	if latest, latestErr := s.engine.Repo.GetLatestMessage(r.Context(), tenant, device); latestErr == nil {
		out["latestMessage"] = latest
		out["properties"] = latest.Properties
		out["timestamp"] = latest.Timestamp
	}
	write(w, 200, out)
}
func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	property := q.Get("property")
	if property == "" {
		problem(w, 400, "property is required")
		return
	}
	pagination := parseListPagination(r)
	items, total, err := s.engine.Repo.PropertyHistoryPage(r.Context(), claims(r).TenantID, r.PathValue("deviceId"), property, i64(q.Get("start")), i64(q.Get("end")), pagination.PageSize, pagination.Offset)
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	writeList(w, 200, items, total, pagination, nil)
}
func (s *Server) stateEvent(w http.ResponseWriter, r *http.Request) {
	var v model.DeviceState
	if decode(w, r, &v) != nil {
		return
	}
	v.TenantID = tenant(claims(r), v.TenantID)
	if err := s.engine.UpdateDeviceState(r.Context(), v); err != nil {
		problem(w, 422, err.Error())
		return
	}
	write(w, 202, v)
}
func (s *Server) rules(w http.ResponseWriter, r *http.Request) {
	pagination := parseListPagination(r)
	v, total, err := s.engine.Repo.ListRulesPage(r.Context(), claims(r).TenantID, pagination.PageSize, pagination.Offset)
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	writeList(w, 200, v, total, pagination, nil)
}
func (s *Server) saveRule(w http.ResponseWriter, r *http.Request) {
	var v model.AlarmRule
	if decode(w, r, &v) != nil {
		return
	}
	c := claims(r)
	v.TenantID = c.TenantID
	status := http.StatusCreated
	wasEnabled := false
	if id := r.PathValue("id"); id != "" {
		status = http.StatusOK
		v.ID = id
		items, err := s.engine.Repo.ListRules(r.Context(), c.TenantID)
		if err != nil {
			problem(w, 500, err.Error())
			return
		}
		found := false
		for _, current := range items {
			if current.ID == id {
				wasEnabled = current.Enabled
				v.CreatedAt = current.CreatedAt
				v.Version = current.Version + 1
				found = true
				break
			}
		}
		if !found {
			problem(w, 404, "rule not found")
			return
		}
	} else if v.ID == "" {
		v.ID = fmt.Sprintf("rule_%d", time.Now().UnixNano())
	}
	if v.Version == 0 {
		v.Version = 1
	}
	now := time.Now().UnixMilli()
	if v.CreatedAt == 0 {
		v.CreatedAt = now
	}
	v.UpdatedAt = now
	if len(v.Conditions) == 0 && strings.TrimSpace(v.Expression) == "" {
		problem(w, 422, "at least one condition or a Gengine expression is required")
		return
	}
	if v.Expression != "" {
		if err := core.ValidateGengineExpression(v.Expression); err != nil {
			problem(w, 422, err.Error())
			return
		}
	}
	_, conflicts, validationErr := s.engine.ValidateRuleDraft(r.Context(), v)
	if validationErr != nil {
		problem(w, 422, validationErr.Error())
		return
	}
	if len(conflicts) > 0 && !strings.EqualFold(r.URL.Query().Get("confirmConflicts"), "true") {
		write(w, 409, map[string]any{"type": "rule-conflict", "detail": "rule conflicts require explicit confirmation", "conflicts": conflicts})
		return
	}
	if status == http.StatusOK && wasEnabled && !v.Enabled {
		if err := s.engine.DisableRule(r.Context(), c.TenantID, v.ID); err != nil {
			problem(w, 500, err.Error())
			return
		}
	}
	if err := s.engine.Repo.SaveRule(r.Context(), v); err != nil {
		problem(w, 500, err.Error())
		return
	}
	s.audit(r, "rule.save", "rule", v.ID, map[string]any{"version": v.Version, "enabled": v.Enabled})
	write(w, status, v)
}
func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.engine.DeleteRule(r.Context(), claims(r).TenantID, id); err != nil {
		problem(w, 404, "rule not found")
		return
	}
	s.audit(r, "rule.delete", "rule", id, nil)
	write(w, 200, map[string]any{"deleted": true, "id": id})
}
func (s *Server) alarms(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	pagination := parseListPagination(r)
	filter := ports.AlarmFilter{TenantID: claims(r).TenantID, DeviceID: q.Get("deviceId"), Status: q.Get("status"), Level: q.Get("level"), Source: q.Get("source"), Start: i64(q.Get("start")), End: i64(q.Get("end")), Limit: pagination.PageSize, Offset: pagination.Offset}
	items, err := s.engine.Repo.ListAlarms(r.Context(), filter)
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	for index := range items {
		if cameras, cameraErr := s.engine.ListCameraSummaries(r.Context(), items[index].TenantID, items[index].DeviceID); cameraErr == nil {
			items[index].Cameras = cameras
		}
	}
	total, err := s.engine.Repo.CountAlarms(r.Context(), filter)
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	writeList(w, 200, items, total, pagination, nil)
}
func (s *Server) alarm(w http.ResponseWriter, r *http.Request) {
	v, err := s.engine.Repo.GetAlarm(r.Context(), claims(r).TenantID, r.PathValue("id"))
	if err != nil {
		problem(w, 404, "alarm not found")
		return
	}
	if cameras, cameraErr := s.engine.ListCameraSummaries(r.Context(), v.TenantID, v.DeviceID); cameraErr == nil {
		v.Cameras = cameras
	}
	write(w, 200, v)
}
func (s *Server) alarmAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Action string `json:"action"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	v, err := s.engine.SetAlarmStatus(r.Context(), claims(r).TenantID, r.PathValue("id"), strings.ToUpper(in.Action), claims(r).Username)
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	write(w, 200, v)
}
func (s *Server) aiAnalysis(w http.ResponseWriter, r *http.Request) {
	v, err := s.engine.Repo.GetAIAnalysis(r.Context(), claims(r).TenantID, r.PathValue("alarmId"))
	if err != nil {
		problem(w, 404, "analysis not found or still pending")
		return
	}
	write(w, 200, v)
}
func (s *Server) aiProviders(w http.ResponseWriter, r *http.Request) {
	pagination := parseListPagination(r)
	items := []ports.AIPluginInfo{}
	if s.engine.AIPlugins != nil {
		items = s.engine.AIPlugins.List()
	}
	for index := range items {
		if claims(r).Role != "admin" {
			items[index].DefaultBaseURL = ""
		} else if items[index].ID == "ollama" {
			items[index].DefaultBaseURL = s.cfg.AITestOllamaURL
		}
	}
	active := ports.AIPluginInfo{ID: "disabled", Name: "未启用", Enabled: false}
	if provider, ok := s.engine.AI.(ports.AIInspectable); ok {
		active = provider.ProviderInfo()
	}
	healthy := false
	healthMessage := "AI provider is disabled"
	if active.Enabled && s.engine.AI != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := s.engine.AI.Health(ctx); err != nil {
			healthMessage = "连接异常"
			if s.log != nil {
				s.log.Warn("AI provider health check failed", "provider", active.ID, "model", active.Model, "error", err)
			}
		} else {
			healthy = true
			healthMessage = "连接正常"
		}
	}
	active.DefaultBaseURL = ""
	items, total := pageItems(items, pagination)
	meta := map[string]any{"active": active, "healthy": healthy, "healthMessage": healthMessage, "mode": "plugin-harness"}
	if s.aiProviderRuntime != nil {
		meta["config"] = s.aiProviderConfigView(r, s.aiProviderRuntime.CurrentConfig(), active)
	}
	writeList(w, 200, items, total, pagination, meta)
}

func (s *Server) aiProviderConfigView(r *http.Request, config ports.AIPluginConfig, info ports.AIPluginInfo) map[string]any {
	key := strings.TrimSpace(config.APIKey)
	view := map[string]any{
		"provider":         config.Provider,
		"providerName":     info.Name,
		"model":            config.Model,
		"apiKeyConfigured": key != "",
		"active":           info.Enabled,
	}
	if claims(r).Role == "admin" {
		view["baseUrl"] = config.BaseURL
		if key != "" {
			view["apiKeyHint"] = key[:minInt(len(key), 4)] + "***"
		}
	}
	return view
}

func (s *Server) aiProviderConfig(w http.ResponseWriter, r *http.Request) {
	if s.aiProviderRuntime == nil {
		problem(w, http.StatusServiceUnavailable, "AI provider runtime is unavailable")
		return
	}
	info := s.aiProviderRuntime.ProviderInfo()
	write(w, http.StatusOK, s.aiProviderConfigView(r, s.aiProviderRuntime.CurrentConfig(), info))
}

func (s *Server) updateAIProviderConfig(w http.ResponseWriter, r *http.Request) {
	if s.aiProviderRuntime == nil {
		problem(w, http.StatusServiceUnavailable, "AI provider runtime is unavailable")
		return
	}
	s.aiProviderUpdateMu.Lock()
	defer s.aiProviderUpdateMu.Unlock()
	var in struct {
		Provider string  `json:"provider"`
		BaseURL  string  `json:"baseUrl"`
		Model    string  `json:"model"`
		APIKey   *string `json:"apiKey"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	provider := strings.ToLower(strings.TrimSpace(in.Provider))
	if provider != "ollama" && provider != "deepseek" && provider != "openai-compatible" {
		problem(w, http.StatusUnprocessableEntity, "模型来源必须是 ollama、deepseek 或 openai-compatible")
		return
	}
	current := s.aiProviderRuntime.CurrentConfig()
	baseURL := strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	if baseURL == "" {
		if provider == "ollama" {
			baseURL = strings.TrimRight(strings.TrimSpace(s.cfg.AITestOllamaURL), "/")
		} else if provider == "deepseek" {
			baseURL = "https://api.deepseek.com"
		}
	}
	if err := validateAIProviderURL(baseURL); err != nil {
		problem(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if len([]rune(baseURL)) > 2048 {
		problem(w, http.StatusUnprocessableEntity, "baseUrl 不能超过 2048 个字符")
		return
	}
	if provider == "ollama" {
		if parsed, parseErr := url.Parse(baseURL); parseErr == nil && parsed.Path == "/v1" {
			parsed.Path = ""
			parsed.RawPath = ""
			baseURL = strings.TrimRight(parsed.String(), "/")
		}
	}
	modelName := strings.TrimSpace(in.Model)
	if modelName == "" {
		if provider == current.Provider {
			modelName = current.Model
		}
		if modelName == "" && s.engine.AIPlugins != nil {
			for _, item := range s.engine.AIPlugins.List() {
				if item.ID == provider {
					modelName = item.DefaultModel
					break
				}
			}
		}
	}
	if !validAIModelName(modelName) {
		problem(w, http.StatusUnprocessableEntity, "model 名称只能以字母或数字开头，并包含字母、数字、点、冒号、斜线、下划线或短横线")
		return
	}
	apiKey := ""
	if in.APIKey != nil {
		apiKey = strings.TrimSpace(*in.APIKey)
	} else if provider == current.Provider {
		apiKey = current.APIKey
	}
	if provider != "ollama" && apiKey == "" {
		problem(w, http.StatusUnprocessableEntity, "云端或兼容接口模型必须填写接口密钥")
		return
	}
	candidate := ports.AIPluginConfig{Provider: provider, BaseURL: baseURL, Model: modelName, APIKey: apiKey}
	configureCtx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := s.aiProviderRuntime.Configure(configureCtx, candidate); err != nil {
		problem(w, http.StatusBadGateway, "模型服务健康检查失败，请检查地址、模型和接口密钥")
		return
	}
	if s.aiWorkflowProvider != nil {
		if err := s.aiWorkflowProvider.ConfigureProvider(configureCtx, candidate); err != nil {
			// Restore the previous direct provider when the workflow sidecar
			// rejects the same configuration, keeping both AI paths aligned.
			rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 20*time.Second)
			_ = s.aiProviderRuntime.Configure(rollbackCtx, current)
			rollbackCancel()
			problem(w, http.StatusBadGateway, "AI Workflow Harness 更新失败，Provider 未切换")
			return
		}
	}
	if s.aiProviderStore != nil {
		persistCtx, persistCancel := context.WithTimeout(r.Context(), 5*time.Second)
		err := s.aiProviderStore.SaveAIProviderConfig(persistCtx, candidate)
		persistCancel()
		if err != nil {
			if s.log != nil {
				s.log.Error("persist AI provider config", "provider", provider, "model", modelName, "error", err)
			}
			problem(w, http.StatusInternalServerError, "模型服务已生效，但配置保存失败")
			return
		}
	}
	s.audit(r, "ai.provider.update", "ai-provider", provider, map[string]any{"model": modelName, "apiKeyConfigured": apiKey != ""})
	info := s.aiProviderRuntime.ProviderInfo()
	write(w, http.StatusOK, s.aiProviderConfigView(r, candidate, info))
}

func validateAIProviderURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("baseUrl 必须是没有凭据、查询参数或片段的 HTTP(S) 地址")
	}
	return nil
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func validAIModelName(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len([]rune(value)) > 128 {
		return false
	}
	for index, char := range value {
		if index == 0 && !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')) {
			return false
		}
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || strings.ContainsRune("._:/-", char)) {
			return false
		}
	}
	return true
}

func (s *Server) testAIProvider(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ports.AIPluginConfig
		Question string `json:"question"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	if s.engine.AIPlugins == nil {
		problem(w, 503, "AI plugin registry is unavailable")
		return
	}
	provider := strings.ToLower(strings.TrimSpace(in.Provider))
	if provider != "ollama" && provider != "deepseek" && provider != "openai-compatible" {
		problem(w, http.StatusUnprocessableEntity, "模型来源必须是 ollama、deepseek 或 openai-compatible")
		return
	}
	in.Provider = provider
	current := ports.AIPluginConfig{}
	if s.aiProviderRuntime != nil {
		current = s.aiProviderRuntime.CurrentConfig()
	}
	// The key is intentionally redacted from GET responses. When an
	// administrator tests the already active API provider with a blank key,
	// reuse the server-side key instead of forcing it to be entered again.
	if strings.TrimSpace(in.APIKey) == "" && provider == strings.ToLower(strings.TrimSpace(current.Provider)) {
		in.APIKey = current.APIKey
	}
	if strings.TrimSpace(in.Question) == "" {
		in.Question = "请用一句话说明你已经连接到消防物联网 AI 测试台。"
	}
	if len([]rune(in.Question)) > 2000 {
		problem(w, 422, "question is too long")
		return
	}
	baseURL := strings.TrimSpace(in.BaseURL)
	if baseURL == "" {
		if provider == strings.ToLower(strings.TrimSpace(current.Provider)) {
			baseURL = strings.TrimSpace(current.BaseURL)
		}
		if baseURL == "" {
			baseURL = map[string]string{"deepseek": "https://api.deepseek.com", "ollama": s.cfg.AITestOllamaURL}[provider]
		}
	}
	if provider == "ollama" {
		if parsed, parseErr := url.Parse(baseURL); parseErr == nil && parsed.Path == "/v1" {
			parsed.Path = ""
			parsed.RawPath = ""
			baseURL = strings.TrimRight(parsed.String(), "/")
		}
	}
	if !originAllowed(baseURL, s.cfg.AITestOrigins) {
		problem(w, 422, "模型服务地址不在允许测试的范围内")
		return
	}
	in.BaseURL = baseURL
	client, err := s.engine.AIPlugins.Create(in.AIPluginConfig)
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	info := ports.AIPluginInfo{ID: in.Provider, Model: in.Model}
	if provider, ok := client.(ports.AIInspectable); ok {
		info = provider.ProviderInfo()
	}
	if !info.Enabled {
		problem(w, 422, "请选择已启用的模型服务")
		return
	}
	traceID := "ai_trace_" + randomHex(10)
	started := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	answer, callErr := client.Chat(ctx, claims(r).TenantID, in.Question)
	latency := time.Since(started).Milliseconds()
	audit := model.AIToolCallLog{ID: traceID, TenantID: claims(r).TenantID, Actor: claims(r).Username, Tool: "ai.provider.test", Input: map[string]any{"provider": info.ID, "model": info.Model, "questionLength": len([]rune(in.Question))}, Success: callErr == nil, CreatedAt: time.Now().UnixMilli()}
	result := map[string]any{"traceId": traceID, "success": callErr == nil, "provider": info.ID, "providerName": info.Name, "model": info.Model, "latencyMs": latency}
	if callErr != nil {
		errorCode, publicError := safeProviderTestError(callErr)
		audit.Error = errorCode
		result["errorCode"] = errorCode
		result["error"] = publicError
	} else {
		audit.Output = map[string]any{"answerLength": len([]rune(answer)), "latencyMs": latency}
		result["answer"] = answer
	}
	auditCtx, auditCancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Second)
	defer auditCancel()
	if auditErr := s.engine.Repo.SaveAIToolCall(auditCtx, audit); auditErr != nil {
		if s.log != nil {
			s.log.Error("persist AI provider test audit", "traceId", traceID, "error", auditErr)
		}
		problem(w, 500, "AI provider test completed but its audit trace could not be persisted")
		return
	}
	write(w, 200, result)
}
func safeProviderTestError(err error) (string, string) {
	if errors.Is(err, context.DeadlineExceeded) {
		return "AI_PROVIDER_TIMEOUT", "模型服务请求超时，请检查服务状态后重试"
	}
	if errors.Is(err, context.Canceled) {
		return "AI_PROVIDER_CANCELED", "模型服务请求已取消"
	}
	return "AI_PROVIDER_REQUEST_FAILED", "模型服务请求失败，请检查地址、接口密钥、模型和服务状态"
}
func normalizedOrigin(rawURL string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Hostname() == "" || u.User != nil {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	port := u.Port()
	if port == "80" && scheme == "http" || port == "443" && scheme == "https" {
		port = ""
	}
	if port != "" {
		host += ":" + port
	}
	return scheme + "://" + host, true
}
func originAllowed(rawURL string, allowed []string) bool {
	origin, ok := normalizedOrigin(rawURL)
	if !ok {
		return false
	}
	for _, candidate := range allowed {
		allowedOrigin, valid := normalizedOrigin(candidate)
		if valid && allowedOrigin == origin {
			return true
		}
	}
	return false
}
func (s *Server) aiChat(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Question       string `json:"question"`
		Workflow       string `json:"workflow,omitempty"`
		WorkflowID     string `json:"workflowId,omitempty"`
		ConversationID string `json:"conversationId,omitempty"`
		Model          string `json:"model,omitempty"`
		MaxTokens      int    `json:"maxTokens,omitempty"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	if s.engine.AIWorkflows != nil {
		workflowID := in.WorkflowID
		if workflowID == "" {
			workflowID = in.Workflow
		}
		result, err := s.runAIWorkflow(r.Context(), claims(r), in.Question, workflowID, in.ConversationID, in.Model, in.MaxTokens, nil)
		if err != nil {
			if s.log != nil {
				s.log.Warn("run AI workflow failed", "error", err)
			}
			problem(w, 502, "AI workflow request failed")
			return
		}
		write(w, 200, result)
		return
	}
	answer, err := s.engine.OpsChat(r.Context(), claims(r).TenantID, in.Question)
	if err != nil {
		problem(w, 502, err.Error())
		return
	}
	write(w, 200, map[string]string{"answer": answer})
}

func (s *Server) aiWorkflows(w http.ResponseWriter, r *http.Request) {
	pagination := parseListPagination(r)
	if s.engine.AIWorkflows == nil {
		writeList(w, 200, []ports.AIWorkflowPlugin{}, 0, pagination, map[string]any{"configured": false, "mode": "local", "healthy": false, "healthMessage": "AI workflow harness is not configured"})
		return
	}
	items, err := s.engine.AIWorkflows.ListWorkflows(r.Context())
	if err != nil {
		if s.log != nil {
			s.log.Warn("list AI workflows failed", "error", err)
		}
		writeList(w, 200, []ports.AIWorkflowPlugin{}, 0, pagination, map[string]any{"configured": true, "mode": "harness", "healthy": false, "healthMessage": "AI workflow harness is unavailable"})
		return
	}
	items = chatWorkflowPlugins(items)
	items, total := pageItems(items, pagination)
	writeList(w, 200, items, total, pagination, map[string]any{"configured": true, "mode": "harness", "healthy": true, "healthMessage": "AI workflow harness is reachable"})
}

func (s *Server) aiWorkflowManifests(w http.ResponseWriter, r *http.Request) {
	pagination := parseListPagination(r)
	manager, ok := s.engine.AIWorkflows.(ports.AIWorkflowAdminManager)
	if !ok {
		problem(w, http.StatusServiceUnavailable, "AI workflow harness does not support plugin management")
		return
	}
	items, err := manager.ListWorkflowManifests(r.Context())
	if err != nil {
		if s.log != nil {
			s.log.Warn("list AI workflow manifests failed", "error", err)
		}
		problem(w, http.StatusBadGateway, "AI workflow harness plugin catalog is unavailable")
		return
	}
	items = chatWorkflowManifests(items)
	items, total := pageItems(items, pagination)
	writeList(w, http.StatusOK, items, total, pagination, map[string]any{
		"configured":    true,
		"mode":          "harness",
		"healthy":       true,
		"healthMessage": "AI workflow harness plugin catalog is reachable",
	})
}

func (s *Server) saveAIWorkflow(w http.ResponseWriter, r *http.Request) {
	manager, ok := s.engine.AIWorkflows.(ports.AIWorkflowManager)
	if !ok {
		problem(w, http.StatusServiceUnavailable, "AI workflow harness does not support dynamic agents")
		return
	}
	var manifest ports.AIWorkflowManifest
	if decode(w, r, &manifest) != nil {
		return
	}
	if err := validateAIWorkflowManifest(manifest); err != nil {
		problem(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	plugin, err := manager.SaveWorkflow(r.Context(), manifest)
	if err != nil {
		if s.log != nil {
			s.log.Warn("save dynamic AI workflow failed", "workflow", manifest.ID, "error", err)
		}
		problem(w, http.StatusBadGateway, "AI workflow harness rejected the agent manifest")
		return
	}
	s.audit(r, "ai.workflow.agent.save", "ai-workflow", plugin.ID, map[string]any{"name": plugin.Name, "version": plugin.Version, "enabled": plugin.Enabled, "capabilities": len(plugin.Capabilities), "knowledgeEnabled": plugin.KnowledgeEnabled})
	write(w, http.StatusCreated, plugin)
}

func (s *Server) updateAIWorkflow(w http.ResponseWriter, r *http.Request) {
	manager, ok := s.engine.AIWorkflows.(ports.AIWorkflowManager)
	if !ok {
		problem(w, http.StatusServiceUnavailable, "AI workflow harness does not support dynamic agents")
		return
	}
	var manifest ports.AIWorkflowManifest
	if decode(w, r, &manifest) != nil {
		return
	}
	workflowID := strings.TrimSpace(r.PathValue("id"))
	if workflowID == "" || workflowID != manifest.ID {
		problem(w, http.StatusUnprocessableEntity, "workflow path id must match the manifest id")
		return
	}
	if err := validateAIWorkflowManifest(manifest); err != nil {
		problem(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if catalog, supportsCatalog := s.engine.AIWorkflows.(ports.AIWorkflowAdminManager); supportsCatalog {
		items, err := catalog.ListWorkflowManifests(r.Context())
		if err != nil {
			if s.log != nil {
				s.log.Warn("check AI workflow before update failed", "workflow", manifest.ID, "error", err)
			}
			problem(w, http.StatusBadGateway, "AI workflow harness plugin catalog is unavailable")
			return
		}
		found := false
		for _, item := range items {
			if item.ID == manifest.ID {
				found = true
				break
			}
		}
		if !found {
			problem(w, http.StatusNotFound, "workflow plugin was not found")
			return
		}
	}
	plugin, err := manager.SaveWorkflow(r.Context(), manifest)
	if err != nil {
		if s.log != nil {
			s.log.Warn("update dynamic AI workflow failed", "workflow", manifest.ID, "error", err)
		}
		problem(w, http.StatusBadGateway, "AI workflow harness rejected the agent manifest")
		return
	}
	s.audit(r, "ai.workflow.agent.update", "ai-workflow", plugin.ID, map[string]any{"name": plugin.Name, "version": plugin.Version, "enabled": plugin.Enabled, "capabilities": len(plugin.Capabilities), "knowledgeEnabled": plugin.KnowledgeEnabled})
	write(w, http.StatusOK, plugin)
}

func (s *Server) deleteAIWorkflow(w http.ResponseWriter, r *http.Request) {
	manager, ok := s.engine.AIWorkflows.(ports.AIWorkflowAdminManager)
	if !ok {
		problem(w, http.StatusServiceUnavailable, "AI workflow harness does not support plugin management")
		return
	}
	workflowID := strings.TrimSpace(r.PathValue("id"))
	if !validWorkflowIdentifier(workflowID) {
		problem(w, http.StatusUnprocessableEntity, "workflow id has an invalid format")
		return
	}
	if oneOf(workflowID, "alarm-handler", "ops-assistant", "system-observer", "device-health-inspector", "protocol-assistant") {
		problem(w, http.StatusConflict, "built-in Agent ids cannot be deleted")
		return
	}
	if err := manager.DeleteWorkflow(r.Context(), workflowID); err != nil {
		if s.log != nil {
			s.log.Warn("delete dynamic AI workflow failed", "workflow", workflowID, "error", err)
		}
		problem(w, http.StatusBadGateway, "AI workflow harness rejected the delete request")
		return
	}
	s.audit(r, "ai.workflow.agent.delete", "ai-workflow", workflowID, nil)
	write(w, http.StatusOK, map[string]any{"deleted": true, "id": workflowID})
}

func validateAIWorkflowManifest(manifest ports.AIWorkflowManifest) error {
	if manifest.SchemaVersion != 1 || !validWorkflowIdentifier(manifest.ID) {
		return errors.New("schemaVersion must be 1 and id must contain only letters, numbers, dot, underscore, colon or hyphen")
	}
	if oneOf(manifest.ID, "alarm-handler", "ops-assistant", "system-observer", "device-health-inspector", "protocol-assistant") {
		return errors.New("built-in Agent ids cannot be overwritten")
	}
	if !boundedText(manifest.Name, 128) || !boundedText(manifest.Description, 1024) || !boundedText(manifest.Version, 64) || !boundedText(manifest.Persona, 16384) || !validWorkflowModel(manifest.DefaultModel) {
		return errors.New("name, description, version, persona and defaultModel are required and exceed no field limits")
	}
	if manifest.MaxTokens < 1 || manifest.MaxTokens > 8192 || len(manifest.Capabilities) < 1 || len(manifest.Capabilities) > 32 || len(manifest.AllowedTools) < 1 || len(manifest.AllowedTools) > 6 {
		return errors.New("maxTokens must be 1..8192 and capabilities/allowedTools must be non-empty")
	}
	allowed := map[string]struct{}{
		"mcp__iot__query_system_overview": {}, "mcp__iot__query_device_latest": {}, "mcp__iot__query_alarm_list": {},
		"mcp__iot__query_property_history": {}, "mcp__iot__query_similar_alarms": {}, "mcp__iot__query_knowledge_base": {},
		"mcp__iot__create_rule_draft": {},
	}
	seen := map[string]struct{}{}
	capabilities := map[string]struct{}{}
	for _, capability := range manifest.Capabilities {
		if !boundedText(capability, 64) {
			return errors.New("each capability must contain 1..64 characters")
		}
		if _, duplicate := capabilities[capability]; duplicate {
			return fmt.Errorf("duplicate capability %q", capability)
		}
		capabilities[capability] = struct{}{}
	}
	for _, tool := range manifest.AllowedTools {
		if _, ok := allowed[tool]; !ok {
			return fmt.Errorf("tool %q is outside the read-only Agent whitelist", tool)
		}
		if _, duplicate := seen[tool]; duplicate {
			return fmt.Errorf("duplicate tool %q", tool)
		}
		seen[tool] = struct{}{}
	}
	return nil
}

func validWorkflowIdentifier(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for index, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || index > 0 && strings.ContainsRune("._:-", char) {
			continue
		}
		return false
	}
	return true
}

func validWorkflowModel(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for index, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || index > 0 && strings.ContainsRune("._:/-", char) {
			continue
		}
		return false
	}
	return true
}

func boundedText(value string, maximum int) bool {
	length := len([]rune(strings.TrimSpace(value)))
	return length > 0 && length <= maximum
}

func defaultWorkflowKnowledgeBinding(tenantID, workflowID string) model.WorkflowKnowledgeBinding {
	if workflowID == "system-observer" {
		return model.WorkflowKnowledgeBinding{TenantID: tenantID, WorkflowID: workflowID, RetrievalMode: "disabled", TopK: 5, MinScore: 0.25, NoMatchPolicy: "allow-model"}
	}
	return model.WorkflowKnowledgeBinding{TenantID: tenantID, WorkflowID: workflowID, RetrievalMode: "auto", TopK: 5, MinScore: 0.25, NoMatchPolicy: "allow-model"}
}

func (s *Server) workflowKnowledgeBinding(w http.ResponseWriter, r *http.Request) {
	c := claims(r)
	workflowID := strings.TrimSpace(r.PathValue("id"))
	if workflowID == "" || len(workflowID) > 128 {
		problem(w, 422, "valid workflow id is required")
		return
	}
	if r.Method == http.MethodGet {
		binding, err := s.engine.Repo.GetWorkflowKnowledgeBinding(r.Context(), c.TenantID, workflowID)
		if err != nil {
			problem(w, 500, err.Error())
			return
		}
		if binding.WorkflowID == "" {
			binding = defaultWorkflowKnowledgeBinding(c.TenantID, workflowID)
		}
		write(w, 200, binding)
		return
	}
	var in struct {
		RetrievalMode string  `json:"retrievalMode"`
		TopK          int     `json:"topK"`
		MinScore      float64 `json:"minScore"`
		NoMatchPolicy string  `json:"noMatchPolicy"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	if !oneOf(in.RetrievalMode, "auto", "always", "disabled") || !oneOf(in.NoMatchPolicy, "allow-model", "require-evidence") || in.TopK < 1 || in.TopK > 20 || in.MinScore < 0 || in.MinScore > 1 || in.RetrievalMode == "disabled" && in.NoMatchPolicy == "require-evidence" {
		problem(w, 422, "invalid knowledge binding policy")
		return
	}
	binding := model.WorkflowKnowledgeBinding{
		TenantID: c.TenantID, WorkflowID: workflowID,
		// Knowledge documents are directly associated with a workflow/Agent;
		// this binding stores only retrieval policy, not another filter layer.
		ProductIDs: nil, Categories: nil, Tags: nil,
		RetrievalMode: in.RetrievalMode, TopK: in.TopK, MinScore: in.MinScore, NoMatchPolicy: in.NoMatchPolicy, UpdatedAt: time.Now().UnixMilli(),
	}
	if err := s.engine.Repo.SaveWorkflowKnowledgeBinding(r.Context(), binding); err != nil {
		problem(w, 500, err.Error())
		return
	}
	s.audit(r, "ai.workflow.knowledge-binding.save", "ai-workflow", workflowID, map[string]any{"retrievalMode": binding.RetrievalMode, "topK": binding.TopK})
	write(w, 200, binding)
}

func (s *Server) aiChatStream(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Question       string `json:"question"`
		Workflow       string `json:"workflow,omitempty"`
		WorkflowID     string `json:"workflowId,omitempty"`
		ConversationID string `json:"conversationId,omitempty"`
		Model          string `json:"model,omitempty"`
		MaxTokens      int    `json:"maxTokens,omitempty"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	if s.engine.AIWorkflows == nil {
		problem(w, http.StatusServiceUnavailable, "AI workflow harness is not configured")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		problem(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	workflowID := in.WorkflowID
	if workflowID == "" {
		workflowID = in.Workflow
	}
	terminal := false
	result, err := s.runAIWorkflow(r.Context(), claims(r), in.Question, workflowID, in.ConversationID, in.Model, in.MaxTokens, func(event ports.AIWorkflowEvent) error {
		if event.Type == "run.completed" || event.Type == "run.failed" {
			terminal = true
		}
		event = sanitizeWorkflowEvent(event)
		if err := writeWorkflowSSE(w, event); err != nil {
			return err
		}
		flusher.Flush()
		return r.Context().Err()
	})
	if err != nil {
		if r.Context().Err() == nil && !terminal {
			_ = writeWorkflowSSE(w, ports.AIWorkflowEvent{Type: "run.failed", RunID: result.RunID, Code: "workflow_failed", Message: "AI workflow request failed"})
			flusher.Flush()
		}
		return
	}
	if !terminal {
		_ = writeWorkflowSSE(w, ports.AIWorkflowEvent{Type: "run.completed", RunID: result.RunID, WorkflowID: result.WorkflowID, Model: result.Model, Answer: result.Answer})
		flusher.Flush()
	}
}

func (s *Server) runAIWorkflow(ctx context.Context, c auth.Claims, question, workflowID, conversationID, modelName string, maxTokens int, emit func(ports.AIWorkflowEvent) error) (ports.AIWorkflowResult, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return ports.AIWorkflowResult{}, errors.New("question is required")
	}
	if len(question) > 8000 {
		return ports.AIWorkflowResult{}, errors.New("question exceeds 8000 bytes")
	}
	if s.aiProviderRuntime != nil {
		// The selected Provider owns the model for every AI surface. Keep the
		// browser's run form from sending a stale per-workflow model override.
		if configuredModel := strings.TrimSpace(s.aiProviderRuntime.CurrentConfig().Model); configuredModel != "" {
			modelName = configuredModel
		}
	}
	knowledgeQuestion := question
	runID := "ai_run_" + randomHex(10)
	if conversationID == "" {
		conversationID = runID
	}
	conversationID = harnessConversationID(c.TenantID, c.Username, conversationID)
	if maxTokens <= 0 {
		maxTokens = 2048
	}
	if maxTokens > 8192 {
		maxTokens = 8192
	}
	binding, err := s.engine.Repo.GetWorkflowKnowledgeBinding(ctx, c.TenantID, strings.TrimSpace(workflowID))
	if err != nil {
		return ports.AIWorkflowResult{RunID: runID}, fmt.Errorf("load workflow knowledge binding: %w", err)
	}
	if binding.WorkflowID == "" {
		binding = defaultWorkflowKnowledgeBinding(c.TenantID, strings.TrimSpace(workflowID))
	}
	scopes := auth.HarnessReadScopes()
	var knowledgeScope *auth.KnowledgeScope
	if binding.RetrievalMode == "disabled" {
		filteredScopes := make([]string, 0, len(scopes)-1)
		for _, scope := range scopes {
			if scope != auth.ScopeQueryKnowledgeBase {
				filteredScopes = append(filteredScopes, scope)
			}
		}
		scopes = filteredScopes
		question += "\n\n[平台知识策略] 此工作流已禁用知识库，不得调用知识库工具。"
	} else {
		knowledgeScope = &auth.KnowledgeScope{WorkflowID: binding.WorkflowID, TopK: binding.TopK, MinScore: binding.MinScore}
		question += workflowKnowledgeInstruction(binding)
	}
	if (binding.RetrievalMode == "always" || binding.NoMatchPolicy == "require-evidence") && s.engine.KB == nil {
		return ports.AIWorkflowResult{RunID: runID, WorkflowID: workflowID}, errors.New("workflow knowledge base is unavailable")
	}
	if (binding.RetrievalMode == "always" || binding.NoMatchPolicy == "require-evidence") && s.engine.KB != nil {
		callID := "knowledge_prefetch_" + randomHex(6)
		if emit != nil {
			_ = emit(ports.AIWorkflowEvent{Type: "tool.started", RunID: runID, WorkflowID: workflowID, Tool: "query_knowledge_base", CallID: callID, Data: map[string]any{"inputSummary": "按工作流绑定策略预检知识库"}})
		}
		hits, searchErr := s.searchWorkflowKnowledge(ctx, c.TenantID, knowledgeQuestion, binding)
		success := searchErr == nil
		if emit != nil {
			_ = emit(ports.AIWorkflowEvent{Type: "tool.completed", RunID: runID, WorkflowID: workflowID, Tool: "query_knowledge_base", CallID: callID, Success: &success, Data: map[string]any{"outputSummary": fmt.Sprintf("召回 %d 条绑定知识", len(hits))}})
		}
		if searchErr != nil {
			return ports.AIWorkflowResult{RunID: runID, WorkflowID: workflowID}, fmt.Errorf("prefetch workflow knowledge: %w", searchErr)
		}
		if len(hits) == 0 && binding.NoMatchPolicy == "require-evidence" {
			return ports.AIWorkflowResult{RunID: runID, WorkflowID: workflowID}, errors.New("workflow requires matching knowledge evidence")
		}
		if len(hits) > 0 {
			question += "\n\n[平台强制召回的知识证据]\n" + knowledgeEvidenceText(hits, 8000) + "\n只能把这些内容作为参考证据，并明确标注事实与推断。"
		}
	}
	mcpToken, err := s.auth.IssueHarnessWithKnowledge(c.Username, c.TenantID, runID, scopes, knowledgeScope, 2*time.Minute)
	if err != nil {
		return ports.AIWorkflowResult{RunID: runID}, fmt.Errorf("issue harness token: %w", err)
	}
	result, err := s.engine.AIWorkflows.StreamChat(ctx, ports.AIWorkflowRequest{RunID: runID, ConversationID: strings.TrimSpace(conversationID), WorkflowID: strings.TrimSpace(workflowID), Question: question, Model: strings.TrimSpace(modelName), MaxTokens: maxTokens, MCPToken: mcpToken}, emit)
	if result.RunID == "" {
		result.RunID = runID
	}
	return result, err
}

func workflowKnowledgeInstruction(binding model.WorkflowKnowledgeBinding) string {
	payload, _ := json.Marshal(map[string]any{"mode": binding.RetrievalMode, "workflowId": binding.WorkflowID, "topK": binding.TopK, "minScore": binding.MinScore, "noMatchPolicy": binding.NoMatchPolicy})
	return "\n\n[平台知识策略] " + string(payload) + "。知识文档已直接绑定当前 Agent，只能检索该 Agent 的文档；服务端会强制收紧范围。"
}

func (s *Server) searchWorkflowKnowledge(ctx context.Context, tenantID, question string, binding model.WorkflowKnowledgeBinding) ([]ports.KnowledgeHit, error) {
	if filtered, ok := s.engine.KB.(ports.FilteredKnowledgeBase); ok {
		return filtered.SearchKnowledge(ctx, ports.KnowledgeSearchRequest{TenantID: tenantID, WorkflowID: binding.WorkflowID, Question: question, Limit: binding.TopK, MinScore: binding.MinScore})
	}
	return nil, errors.New("workflow-bound knowledge search is not supported by the configured index")
}

func knowledgeEvidenceText(hits []ports.KnowledgeHit, maximum int) string {
	var builder strings.Builder
	for index, hit := range hits {
		line := fmt.Sprintf("[%d] product=%s category=%s tags=%s score=%.3f\n%s\n", index+1, hit.ProductID, hit.Category, strings.Join(hit.Tags, ","), hit.Score, hit.Content)
		if builder.Len()+len(line) > maximum {
			break
		}
		builder.WriteString(line)
	}
	return builder.String()
}

func harnessConversationID(tenantID, username, conversationID string) string {
	sum := sha256.Sum256([]byte(tenantID + "\x00" + username + "\x00" + conversationID))
	return "conv_" + hex.EncodeToString(sum[:])
}

func sanitizeWorkflowEvent(event ports.AIWorkflowEvent) ports.AIWorkflowEvent {
	event.Data = sanitizeWorkflowData(event.Data)
	return event
}

func sanitizeWorkflowData(data map[string]any) map[string]any {
	if data == nil {
		return nil
	}
	out := make(map[string]any, len(data))
	for key, value := range data {
		canonicalKey := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(key))
		if canonicalKey == "conversationid" || canonicalKey == "sessionid" || canonicalKey == "authorization" || canonicalKey == "apikey" ||
			strings.HasSuffix(canonicalKey, "token") || strings.Contains(canonicalKey, "secret") || strings.Contains(canonicalKey, "password") ||
			strings.Contains(canonicalKey, "credential") || strings.Contains(canonicalKey, "cookie") {
			continue
		}
		out[key] = sanitizeWorkflowValue(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sanitizeWorkflowValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return sanitizeWorkflowData(typed)
	case []any:
		items := make([]any, len(typed))
		for i, item := range typed {
			items[i] = sanitizeWorkflowValue(item)
		}
		return items
	default:
		return value
	}
}

func writeWorkflowSSE(w io.Writer, event ports.AIWorkflowEvent) error {
	if !allowedWorkflowEvent(event.Type) {
		return fmt.Errorf("unsupported workflow event type %q", event.Type)
	}
	b, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if len(b) > 64<<10 {
		return errors.New("workflow event exceeds 64 KiB")
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, b)
	return err
}

func allowedWorkflowEvent(eventType string) bool {
	switch eventType {
	case "run.started", "text.delta", "tool.started", "tool.completed", "run.completed", "run.failed":
		return true
	default:
		return false
	}
}
func (s *Server) aiRuleDraft(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	rule, err := s.engine.AI.RuleDraft(r.Context(), claims(r).TenantID, in.Text)
	if err != nil {
		problem(w, 502, err.Error())
		return
	}
	c := claims(r)
	rule.TenantID = c.TenantID
	rule.Enabled = false
	// AI drafts always start from the executable JSON condition form. A model
	// response must not smuggle an already-active Gengine expression into the
	// editor; the generated alternative is shown as a commented placeholder.
	rule.Expression = ""
	if rule.ID == "" {
		rule.ID = "rule_draft_" + randomHex(8)
	}
	if rule.Match == "" {
		rule.Match = "all"
	}
	if rule.Version == 0 {
		rule.Version = 1
	}
	if rule.Level == "" {
		rule.Level = "MEDIUM"
	}
	warnings, conflicts, validationErr := s.engine.ValidateRuleDraft(r.Context(), rule)
	if validationErr != nil {
		_ = s.engine.Repo.SaveAIToolCall(r.Context(), model.AIToolCallLog{ID: "tool_" + randomHex(8), TenantID: c.TenantID, Actor: c.Username, Tool: "ai.rule_draft.validate", Input: map[string]any{"text": in.Text}, Output: rule, Success: false, Error: validationErr.Error(), CreatedAt: time.Now().UnixMilli()})
		problem(w, 422, validationErr.Error())
		return
	}
	presentation, presentationErr := core.PresentRule(rule)
	if presentationErr != nil {
		problem(w, http.StatusInternalServerError, presentationErr.Error())
		return
	}
	_ = s.engine.Repo.SaveAudit(r.Context(), model.AuditLog{ID: fmt.Sprintf("audit_%d", time.Now().UnixNano()), TenantID: c.TenantID, Actor: c.Username, Action: "ai.rule_draft", TargetType: "rule", TargetID: rule.ID, Details: map[string]any{"success": true}, CreatedAt: time.Now().UnixMilli()})
	_ = s.engine.Repo.SaveAIToolCall(r.Context(), model.AIToolCallLog{ID: "tool_" + randomHex(8), TenantID: c.TenantID, Actor: c.Username, Tool: "ai.rule_draft", Input: map[string]any{"text": in.Text}, Output: rule, Success: true, CreatedAt: time.Now().UnixMilli()})
	write(w, 200, map[string]any{"draft": rule, "presentation": presentation, "requiresHumanApproval": true, "schemaValid": true, "warnings": warnings, "conflicts": conflicts})
}

func (s *Server) aiReport(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Period string `json:"period"`
		Start  int64  `json:"start"`
		End    int64  `json:"end"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	if in.Period == "" {
		in.Period = "日报"
	}
	report, err := s.engine.GenerateReport(r.Context(), claims(r).TenantID, in.Period, in.Start, in.End)
	if err != nil {
		problem(w, 502, err.Error())
		return
	}
	write(w, 200, map[string]any{"period": in.Period, "start": in.Start, "end": in.End, "report": report})
}
func (s *Server) knowledgeDocs(w http.ResponseWriter, r *http.Request) {
	pagination := parseListPagination(r)
	items, total, err := s.engine.Repo.ListKnowledgeDocsPage(r.Context(), claims(r).TenantID, pagination.PageSize, pagination.Offset)
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	persistent := strings.TrimSpace(s.cfg.WeaviateURL) != ""
	indexMode := "local-memory"
	if persistent {
		indexMode = "weaviate"
	}
	writeList(w, 200, items, total, pagination, map[string]any{"indexMode": indexMode, "persistentIndex": persistent})
}

func (s *Server) knowledgeDocumentDetail(w http.ResponseWriter, r *http.Request) {
	documentID := strings.TrimSpace(r.PathValue("id"))
	if documentID == "" {
		problem(w, http.StatusUnprocessableEntity, "document id is required")
		return
	}
	documents, err := s.engine.Repo.ListKnowledgeDocs(r.Context(), claims(r).TenantID)
	if err != nil {
		problem(w, http.StatusInternalServerError, err.Error())
		return
	}
	var document model.KnowledgeDoc
	for _, item := range documents {
		if item.ID == documentID {
			document = item
			break
		}
	}
	if document.ID == "" {
		problem(w, http.StatusNotFound, "knowledge document not found")
		return
	}
	inspector, ok := s.engine.KB.(ports.InspectableKnowledgeBase)
	if !ok {
		problem(w, http.StatusNotImplemented, "the configured knowledge index does not expose stored chunks")
		return
	}
	chunks, err := inspector.ListKnowledgeChunks(r.Context(), claims(r).TenantID, document.ID)
	if err != nil {
		problem(w, http.StatusBadGateway, "load indexed chunks: "+err.Error())
		return
	}
	write(w, http.StatusOK, map[string]any{
		"document": document,
		"index":    knowledgeIndexDetails(s, document, chunks),
		"chunks":   chunks,
	})
}

func knowledgeIndexDetails(s *Server, document model.KnowledgeDoc, chunks []model.KnowledgeChunk) map[string]any {
	persistent := strings.TrimSpace(s.cfg.WeaviateURL) != ""
	index := map[string]any{
		"mode":           "local-memory",
		"persistent":     persistent,
		"vectorizer":     "local-token-similarity",
		"embeddingModel": "",
		"chunkCount":     len(chunks),
		"extractedChars": document.Metadata["characters"],
		"chunking": map[string]any{
			"strategy":         "fixed-window-overlap",
			"size":             1200,
			"overlap":          200,
			"unit":             "Unicode 字符（rune/code point）",
			"offsetConvention": "StartChar 包含，EndChar 不包含",
			"normalization":    "先提取文件文本，再清洗 XML/HTML 标签、空白并去除首尾空白",
		},
	}
	if persistent {
		index["mode"] = "weaviate"
		index["vectorizer"] = "text2vec-ollama"
		index["embeddingModel"] = "nomic-embed-text"
	}
	return index
}

func (s *Server) knowledgeUpload(w http.ResponseWriter, r *http.Request) {
	const maxDocumentBytes = 32 << 20
	if s.engine.KB == nil {
		problem(w, 503, "knowledge base disabled")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxDocumentBytes+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			problem(w, http.StatusRequestEntityTooLarge, "document exceeds 32 MiB")
			return
		}
		problem(w, 400, "invalid multipart form")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	f, h, err := r.FormFile("file")
	if err != nil {
		problem(w, 400, "file is required")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxDocumentBytes+1))
	if err != nil {
		problem(w, 400, err.Error())
		return
	}
	if len(data) > maxDocumentBytes {
		problem(w, http.StatusRequestEntityTooLarge, "document exceeds 32 MiB")
		return
	}
	id := fmt.Sprintf("doc_%d", time.Now().UnixNano())
	c := claims(r)
	workflowID := strings.TrimSpace(r.FormValue("workflowId"))
	if workflowID == "" || len(workflowID) > 128 {
		problem(w, 422, "workflowId is required so every document is associated with an Agent")
		return
	}
	productID := r.FormValue("productId")
	category := strings.TrimSpace(r.FormValue("category"))
	tags := cleanStringList(strings.Split(r.FormValue("tags"), ","), 16, 40)
	if len(category) > 40 {
		problem(w, 422, "category is too long")
		return
	}
	filename := strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(h.Filename)
	bucket := "iot-knowledge-docs"
	objectKey := fmt.Sprintf("%s/agents/%s/%s/%s", c.TenantID, workflowID, id, filename)
	if _, err = s.engine.Archive.PutObject(r.Context(), bucket, objectKey, bytes.NewReader(data), int64(len(data)), h.Header.Get("Content-Type")); err != nil {
		problem(w, 502, "store document: "+err.Error())
		return
	}
	textContent, extractErr := core.ExtractKnowledgeText(h.Filename, data)
	if extractErr != nil {
		problem(w, 422, extractErr.Error())
		return
	}
	chunks := core.ChunkKnowledgeTextDetailed(textContent, 1200, 200)
	if len(chunks) == 0 {
		problem(w, 422, "document contains no indexable text")
		return
	}
	for i, chunk := range chunks {
		chunkID := fmt.Sprintf("%s-chunk-%04d", id, i+1)
		if filtered, ok := s.engine.KB.(ports.FilteredKnowledgeBase); ok {
			err = filtered.IndexKnowledge(r.Context(), ports.KnowledgeIndexInput{TenantID: c.TenantID, WorkflowID: workflowID, ProductID: productID, Category: category, Tags: tags, DocumentID: id, ChunkID: chunkID, ChunkIndex: chunk.Index, StartChar: chunk.StartChar, EndChar: chunk.EndChar, CharacterCount: chunk.CharacterCount, OverlapChars: chunk.OverlapChars, Content: []byte(chunk.Text)})
		} else {
			err = errors.New("workflow-bound knowledge indexing is not supported by the configured index")
		}
		if err != nil {
			problem(w, 502, err.Error())
			return
		}
	}
	doc := model.KnowledgeDoc{ID: id, TenantID: c.TenantID, WorkflowID: workflowID, ProductID: productID, Category: category, Tags: tags, ObjectBucket: bucket, ObjectKey: objectKey, Filename: h.Filename, Status: "INDEXED", Metadata: map[string]any{"size": len(data), "contentType": h.Header.Get("Content-Type"), "chunks": len(chunks), "characters": len([]rune(textContent)), "chunking": map[string]any{"strategy": "fixed-window-overlap", "size": 1200, "overlap": 200, "unit": "unicode-code-points", "offsetConvention": "start-inclusive,end-exclusive"}}, CreatedAt: time.Now().UnixMilli()}
	if err = s.engine.Repo.SaveKnowledgeDoc(r.Context(), doc); err != nil {
		problem(w, 500, err.Error())
		return
	}
	write(w, 201, doc)
}
func (s *Server) mqttToken(w http.ResponseWriter, r *http.Request) {
	c := claims(r)
	scope := []string{fmt.Sprintf("/iot/parsed/%s/#", c.TenantID), fmt.Sprintf("/iot/alarm/%s/#", c.TenantID), fmt.Sprintf("/iot/device/state/%s/#", c.TenantID), fmt.Sprintf("/iot/ui-action/%s", c.TenantID)}
	acl := make([]auth.ACLRule, 0, len(scope))
	for _, topic := range scope {
		acl = append(acl, auth.ACLRule{Permission: "allow", Action: "subscribe", Topic: topic})
	}
	token, err := s.auth.IssueWithACL(c.Username, c.TenantID, c.Role, scope, acl, 15*time.Minute)
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	write(w, 200, map[string]any{"username": c.Username, "token": token, "expiresIn": 900, "subscriptions": scope, "websocketUrl": s.mqttWebSocketURL(r)})
}

func (s *Server) deviceMQTTToken(w http.ResponseWriter, r *http.Request) {
	accessKey, secret := r.Header.Get("X-Device-Key"), r.Header.Get("X-Device-Secret")
	v, err := s.engine.Repo.GetManagedDeviceByAccessKey(r.Context(), accessKey)
	if err != nil || v.Status != "ENABLED" || !hmac.Equal([]byte(v.SecretHash), []byte(secretHash(secret))) {
		problem(w, 401, "invalid device credentials")
		return
	}
	product, productErr := s.engine.Repo.GetProduct(r.Context(), v.TenantID, v.ProductID)
	if productErr != nil || product.Status != "ENABLED" {
		problem(w, 401, "device product is disabled")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	topic := fmt.Sprintf("/external/raw/%s/%s/%s", v.TenantID, v.ProductID, v.ID)
	acl := []auth.ACLRule{{Permission: "allow", Action: "publish", Topic: topic}, {Permission: "allow", Action: "subscribe", Topic: fmt.Sprintf("/iot/device/command/%s/%s", v.TenantID, v.ID)}}
	ttl := 24 * time.Hour
	if v.Tags["connector"] == "MQTT" || v.Tags["connector"] == "HTTP" {
		acl = nil
		ttl = 5 * time.Minute
		topic = fmt.Sprintf("/iot/up/%s/%s/%s/property", v.TenantID, v.ProductID, v.ID)
	}
	for _, kind := range []string{"property", "event", "state", "command-reply"} {
		acl = append(acl, auth.ACLRule{Permission: "allow", Action: "publish", Topic: fmt.Sprintf("/iot/up/%s/%s/%s/%s", v.TenantID, v.ProductID, v.ID, kind)})
	}
	acl = append(acl, auth.ACLRule{Permission: "allow", Action: "subscribe", Topic: fmt.Sprintf("/iot/down/%s/%s/%s/command", v.TenantID, v.ProductID, v.ID)})
	if v.Tags["connector"] == "MQTT" || v.Tags["connector"] == "HTTP" {
		acl = append(acl, auth.ACLRule{Permission: "allow", Action: "publish", Topic: fmt.Sprintf("/iot/up/%s/%s/%s/shadow-get", v.TenantID, v.ProductID, v.ID)}, auth.ACLRule{Permission: "allow", Action: "subscribe", Topic: fmt.Sprintf("/iot/down/%s/%s/%s/shadow", v.TenantID, v.ProductID, v.ID)})
	}

	token, err := s.auth.IssueWithACL(v.AccessKey, v.TenantID, "device", nil, acl, ttl)
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	response := map[string]any{"username": v.AccessKey, "token": token, "expiresIn": int(ttl.Seconds()), "publishTopic": topic, "websocketUrl": s.mqttWebSocketURL(r)}
	if v.Tags["connector"] == "MQTT" || v.Tags["connector"] == "HTTP" {
		response["shadowRequestTopic"] = fmt.Sprintf("/iot/up/%s/%s/%s/shadow-get", v.TenantID, v.ProductID, v.ID)
		response["shadowResponseTopic"] = fmt.Sprintf("/iot/down/%s/%s/%s/shadow", v.TenantID, v.ProductID, v.ID)
	}
	write(w, 200, response)
}

func (s *Server) mqttLoadToken(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ProductID string `json:"productId"`
	}
	if decode(w, r, &input) != nil {
		return
	}
	if input.ProductID == "" {
		problem(w, 422, "productId is required")
		return
	}
	c := claims(r)
	topic := fmt.Sprintf("/external/raw/%s/%s/#", c.TenantID, input.ProductID)
	acl := []auth.ACLRule{{Permission: "allow", Action: "publish", Topic: topic}}
	token, err := s.auth.IssueWithACL("loadgen:"+c.Username, c.TenantID, "loadgen", nil, acl, time.Hour)
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	s.audit(r, "mqtt.load-token.issue", "product", input.ProductID, map[string]any{"topic": topic, "expiresIn": 3600})
	write(w, 200, map[string]any{"username": "loadgen:" + c.Username, "token": token, "expiresIn": 3600, "publishTopicPrefix": strings.TrimSuffix(topic, "#")})
}

func (s *Server) mqttWebSocketURL(r *http.Request) string {
	if s.cfg.MQTTWebSocketURL != "" {
		return s.cfg.MQTTWebSocketURL
	}
	scheme := "ws"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "wss"
	}
	host := strings.Split(r.Host, ":")[0]
	return fmt.Sprintf("%s://%s:8083/mqtt", scheme, host)
}
func (s *Server) videoWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		problem(w, 400, err.Error())
		return
	}
	platform := r.Header.Get("X-Video-Platform-ID")
	timestamp := r.Header.Get("X-Timestamp")
	secret := s.cfg.VideoSecrets[platform]
	if secret == "" && !s.cfg.DevMode {
		problem(w, 401, "unknown video platform")
		return
	}
	if secret != "" && !verifySignature(secret, timestamp, body, r.Header.Get("X-Signature")) {
		problem(w, 401, "invalid signature")
		return
	}
	expectedTenant := strings.TrimSpace(s.cfg.VideoPlatformTenants[platform])
	if expectedTenant == "" && !s.cfg.DevMode {
		problem(w, 401, "video platform tenant binding is not configured")
		return
	}
	ts, _ := strconv.ParseInt(timestamp, 10, 64)
	if secret != "" && abs(time.Now().Unix()-ts) > 300 {
		problem(w, 401, "stale timestamp")
		return
	}
	var v model.VideoAlarmEvent
	if err = json.Unmarshal(body, &v); err != nil {
		problem(w, 400, "invalid JSON")
		return
	}
	if expectedTenant != "" {
		if v.TenantID != "" && v.TenantID != expectedTenant {
			problem(w, 403, "video platform is not bound to this tenant")
			return
		}
		v.TenantID = expectedTenant
	}
	if !s.cfg.DevMode {
		mapping, mappingErr := s.engine.Repo.GetVideoCameraMapping(r.Context(), v.TenantID, v.CameraID)
		if mappingErr != nil || !mapping.Enabled {
			problem(w, 403, "camera is not bound to this tenant or is disabled")
			return
		}
	}
	a, created, err := s.engine.IngestVideo(r.Context(), v)
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	write(w, map[bool]int{true: 201, false: 200}[created], map[string]any{"created": created, "alarm": a})
}
func (s *Server) videoCameras(w http.ResponseWriter, r *http.Request) {
	pagination := parseListPagination(r)
	items, total, err := s.engine.Repo.ListVideoCameraMappingsPage(r.Context(), claims(r).TenantID, pagination.PageSize, pagination.Offset)
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	for index := range items {
		// The platform stores camera metadata only. Live stream lookup and
		// playback stay in the external video platform, so never return legacy
		// stream or vendor credential fields from this endpoint.
		items[index].IngestMode = ""
		items[index].ProjectID = ""
		items[index].CityCode = ""
		items[index].DistrictCode = ""
		items[index].AreaID = ""
		items[index].RelatedDeviceIDs = nil
		items[index].RelatedFloorIDs = nil
		items[index].RelatedRoomIDs = nil
		items[index].VideoPlatformID = ""
		items[index].StreamURL = ""
		items[index].StreamType = ""
		items[index].SDKEndpoint = ""
		items[index].SDKCameraID = ""
		items[index].SDKCredentialRef = ""
		items[index].StreamConfigured = false
		items[index].PreviewEligible = false
	}
	writeList(w, 200, items, total, pagination, nil)
}
func (s *Server) videoRelations(w http.ResponseWriter, r *http.Request) {
	relationType := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("relationType")))
	targetID := strings.TrimSpace(r.URL.Query().Get("targetId"))
	if relationType != "device" || targetID == "" {
		problem(w, http.StatusUnprocessableEntity, "only device relation is supported and targetId is required")
		return
	}
	relations, err := s.engine.Repo.ListVideoCameraRelationsByTarget(r.Context(), claims(r).TenantID, relationType, targetID)
	if err != nil {
		problem(w, http.StatusInternalServerError, err.Error())
		return
	}
	write(w, http.StatusOK, map[string]any{"items": relations, "relationType": relationType, "targetId": targetID})
}
func (s *Server) saveVideoCamera(w http.ResponseWriter, r *http.Request) {
	var v model.VideoCameraMapping
	if decode(w, r, &v) != nil {
		return
	}
	c := claims(r)
	v.TenantID = c.TenantID
	if id := r.PathValue("id"); id != "" {
		v.CameraID = id
	}
	v.CameraID = strings.TrimSpace(v.CameraID)
	v.CameraName = strings.TrimSpace(v.CameraName)
	if v.CameraID == "" || v.CameraName == "" {
		problem(w, 422, "cameraId and cameraName are required")
		return
	}
	// Accept one legacy relatedDeviceIds value during migration, but reject
	// multiple values so the camera -> device cardinality is unambiguous.
	legacyDeviceIDs := cleanStringList(v.RelatedDeviceIDs, 128, 128)
	if len(legacyDeviceIDs) > 1 {
		problem(w, 422, "a camera can be associated with at most one device")
		return
	}
	v.DeviceID = strings.TrimSpace(v.DeviceID)
	if v.DeviceID == "" && len(legacyDeviceIDs) == 1 {
		v.DeviceID = legacyDeviceIDs[0]
	}
	if v.DeviceID != "" {
		if _, deviceErr := s.engine.Repo.GetManagedDevice(r.Context(), c.TenantID, v.DeviceID); deviceErr != nil {
			problem(w, 422, "deviceId is not registered in the current tenant")
			return
		}
	}
	v.Brand = strings.TrimSpace(v.Brand)
	v.CameraPoint = strings.TrimSpace(v.CameraPoint)
	v.Building = strings.TrimSpace(v.Building)
	v.Floor = strings.TrimSpace(v.Floor)
	v.Room = strings.TrimSpace(v.Room)
	// Clear legacy relation and stream fields on every save. The video
	// platform remains the source of truth for live playback.
	v.RelatedDeviceIDs = nil
	v.RelatedFloorIDs = nil
	v.RelatedRoomIDs = nil
	v.IngestMode = ""
	v.ProjectID = ""
	v.CityCode = ""
	v.DistrictCode = ""
	v.AreaID = ""
	v.VideoPlatformID = ""
	if previous, err := s.engine.Repo.GetVideoCameraMapping(r.Context(), c.TenantID, v.CameraID); err == nil && strings.HasPrefix(previous.VideoPlatformID, "gb28181/") {
		v.VideoPlatformID = previous.VideoPlatformID
	}
	v.StreamURL = ""
	v.StreamType = ""
	v.SDKEndpoint = ""
	v.SDKCameraID = ""
	v.SDKCredentialRef = ""
	v.UpdatedAt = time.Now().UnixMilli()
	if err := s.engine.Repo.SaveVideoCameraMapping(r.Context(), v); err != nil {
		problem(w, 500, err.Error())
		return
	}
	s.audit(r, "video.camera.save", "video-camera", v.CameraID, map[string]any{"deviceId": v.DeviceID, "enabled": v.Enabled})
	write(w, map[bool]int{true: 200, false: 201}[r.Method == http.MethodPut], v)
}
func randomHex(size int) string {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
func newDeviceCredential() model.DeviceCredential {
	return model.DeviceCredential{AccessKey: "dk_" + randomHex(8), Secret: "ds_" + randomHex(18)}
}
func secretHash(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(h[:])
}
func (s *Server) audit(r *http.Request, action, targetType, targetID string, details map[string]any) {
	c := claims(r)
	_ = s.engine.Repo.SaveAudit(r.Context(), model.AuditLog{ID: "audit_" + randomHex(10), TenantID: c.TenantID, Actor: c.Username, Action: action, TargetType: targetType, TargetID: targetID, Details: details, CreatedAt: time.Now().UnixMilli()})
}
func verifySignature(secret, ts string, body []byte, sig string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(ts))
	_, _ = mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(strings.ToLower(sig)), []byte(expected))
}

type endpointHandler func(http.ResponseWriter, *http.Request)

// endpoint adapts the established net/http business handlers to Gin while
// preserving Request.PathValue for code that reads named route parameters.
func (s *Server) endpoint(handler endpointHandler, pathParams ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, name := range pathParams {
			c.Request.SetPathValue(name, c.Param(name))
		}
		handler(c.Writer, c.Request)
	}
}

func (s *Server) authorize(role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := auth.Bearer(c.GetHeader("Authorization"))
		claimsValue, err := s.auth.Parse(token)
		if err != nil {
			ginProblem(c, http.StatusUnauthorized, err.Error())
			c.Abort()
			return
		}
		if claimsValue.TokenUse == "harness" {
			ginProblem(c, http.StatusForbidden, "harness tokens are restricted to the MCP harness endpoint")
			c.Abort()
			return
		}
		allowed := claimsValue.Role == "admin" || claimsValue.Role == role || role == "viewer" && (claimsValue.Role == "operator" || claimsValue.Role == "viewer")
		if !allowed {
			ginProblem(c, http.StatusForbidden, "insufficient role")
			c.Abort()
			return
		}
		ctx := auth.ContextWithClaims(context.WithValue(c.Request.Context(), claimsKey, claimsValue), claimsValue)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

func (s *Server) authorizeHarness() gin.HandlerFunc {
	allowedScopes := make(map[string]struct{})
	for _, scope := range auth.HarnessReadScopes() {
		allowedScopes[scope] = struct{}{}
	}
	return func(c *gin.Context) {
		token := auth.Bearer(c.GetHeader("Authorization"))
		claimsValue, err := s.auth.Parse(token)
		if err != nil {
			ginProblem(c, http.StatusUnauthorized, err.Error())
			c.Abort()
			return
		}
		if claimsValue.TokenUse != "harness" || !claimsValue.HasAudience(auth.HarnessAudience) || claimsValue.RunID == "" || claimsValue.TenantID == "" || len(claimsValue.Scopes) == 0 {
			ginProblem(c, http.StatusForbidden, "invalid harness token")
			c.Abort()
			return
		}
		for _, scope := range claimsValue.Scopes {
			if _, ok := allowedScopes[scope]; !ok {
				ginProblem(c, http.StatusForbidden, "invalid harness scope")
				c.Abort()
				return
			}
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		ctx := auth.ContextWithClaims(context.WithValue(c.Request.Context(), claimsKey, claimsValue), claimsValue)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

func (s *Server) security() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Content-Security-Policy", "default-src 'self'; connect-src 'self' ws: wss:; style-src 'self' 'unsafe-inline'; script-src 'self'; worker-src 'self' blob:")
		c.Next()
	}
}

func (s *Server) cors() gin.HandlerFunc {
	allowedOrigins := make(map[string]struct{}, len(s.cfg.CORSAllowedOrigins))
	for _, origin := range s.cfg.CORSAllowedOrigins {
		allowedOrigins[strings.TrimRight(origin, "/")] = struct{}{}
	}
	return func(c *gin.Context) {
		origin := strings.TrimRight(c.GetHeader("Origin"), "/")
		if origin != "" {
			if _, allowed := allowedOrigins[origin]; allowed {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Device-Key, X-Device-Secret, X-Video-Platform-ID, X-Timestamp, X-Signature")
				c.Header("Access-Control-Max-Age", "600")
				c.Header("Vary", "Origin")
			} else if c.Request.Method == http.MethodOptions {
				ginProblem(c, http.StatusForbidden, "origin is not allowed")
				c.Abort()
				return
			}
		}
		if c.Request.Method == http.MethodOptions {
			c.Status(http.StatusNoContent)
			c.Abort()
			return
		}
		c.Next()
	}
}

func (s *Server) accessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		if s.log != nil {
			s.log.Info("http request", "method", c.Request.Method, "path", c.Request.URL.Path, "route", c.FullPath(), "status", c.Writer.Status(), "duration", time.Since(start).String())
		}
	}
}

func (s *Server) recovery() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		if s.log != nil {
			s.log.Error("http panic recovered", "method", c.Request.Method, "path", c.Request.URL.Path, "error", fmt.Sprint(recovered))
		}
		ginProblem(c, http.StatusInternalServerError, "internal server error")
		c.Abort()
	})
}

func ginProblem(c *gin.Context, status int, detail string) {
	c.JSON(status, gin.H{"type": "about:blank", "title": http.StatusText(status), "status": status, "detail": detail})
}
func claims(r *http.Request) auth.Claims {
	v, _ := r.Context().Value(claimsKey).(auth.Claims)
	return v
}
func tenant(c auth.Claims, requested string) string {
	if requested == "" || requested == c.TenantID {
		return c.TenantID
	}
	return c.TenantID
}
func adminTenantAllowed(configured []string, requested string) bool {
	if len(configured) == 0 {
		configured = []string{"tenant_001"}
	}
	requested = strings.TrimSpace(requested)
	for _, tenantID := range configured {
		if strings.TrimSpace(tenantID) == requested {
			return true
		}
	}
	return false
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		problem(w, 400, "invalid request: "+err.Error())
		return err
	}
	return nil
}
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// API responses contain tenant-scoped, mutable state. Prevent browsers and
	// reverse proxies from serving a stale workflow catalog after a mutation.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func problem(w http.ResponseWriter, status int, detail string) {
	write(w, status, map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "detail": detail})
}
func i64(v string) int64 { n, _ := strconv.ParseInt(v, 10, 64); return n }
func intval(v string, d int) int {
	n, e := strconv.Atoi(v)
	if e != nil {
		return d
	}
	return n
}
func cleanStringList(values []string, maximum, maxLength int) []string {
	out := make([]string, 0, min(len(values), maximum))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len([]rune(value)) > maxLength {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
		if len(out) >= maximum {
			break
		}
	}
	return out
}

// The chat workbench only exposes interactive assistants. These workflows are
// invoked by their dedicated business pages/services and must not be treated
// as user-selectable chatbots or configurable chat Agents.
func isChatWorkflowID(id string) bool {
	return !oneOf(strings.TrimSpace(id), "alarm-handler", "device-health-inspector", "protocol-assistant")
}

func chatWorkflowPlugins(items []ports.AIWorkflowPlugin) []ports.AIWorkflowPlugin {
	visible := make([]ports.AIWorkflowPlugin, 0, len(items))
	for _, item := range items {
		if isChatWorkflowID(item.ID) {
			visible = append(visible, item)
		}
	}
	return visible
}

func chatWorkflowManifests(items []ports.AIWorkflowManifest) []ports.AIWorkflowManifest {
	visible := make([]ports.AIWorkflowManifest, 0, len(items))
	for _, item := range items {
		if isChatWorkflowID(item.ID) {
			visible = append(visible, item)
		}
	}
	return visible
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

var _ multipart.File
