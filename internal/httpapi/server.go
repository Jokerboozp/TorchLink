package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"iot-platform/internal/aiworkflow"
	"iot-platform/internal/auth"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/externaldata"
	"iot-platform/internal/firesafety"
	"iot-platform/internal/logctx"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/netguard"
	"iot-platform/internal/notify"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/opscenter"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
	"iot-platform/internal/sites"
	"iot-platform/internal/version"
	"iot-platform/internal/video"
)

type ctxKey string

const claimsKey ctxKey = "claims"

type Server struct {
	// ai runs the business AI workflows for this server's requests.
	ai         *aiworkflow.Service
	dashboards dashboardCache
	cfg        config.Config
	engine     *core.Engine
	auth       *auth.Manager
	// harnessAuth signs and verifies Harness MCP credentials with their own key.
	harnessAuth                *auth.Manager
	metrics                    *metrics.Registry
	log                        *slog.Logger
	router                     *gin.Engine
	aiProviderRuntime          ports.AIProviderRuntime
	embeddingRuntime           ports.EmbeddingRuntime
	knowledgeJobs              ports.KnowledgeDocumentJobs
	aiProviderStore            ports.AIProviderConfigStore
	aiWorkflowProvider         ports.AIWorkflowProviderRuntime
	aiProviderUpdateMu         sync.Mutex
	healthInspectionMu         sync.RWMutex // 仅保护本进程的耗时估算；任务状态保存在仓储中。
	healthInspectionEstimateMs int64
	logins                     *loginLimiter
	inspectionPDFs             *inspectionPDFCache
	inspectionRequests         chan struct{}
	aiAnalysisMu               sync.RWMutex
	aiAnalysisEstimateMs       int64
	protocolListeners          protocolCommander
	onboarding                 *onboarding.Service
	events                     *eventSnapshots
	ops                        *opscenter.Service
	video                      atomic.Pointer[video.Service]
	videoOwner                 func() (local bool, endpoint string)
	fireSafety                 *firesafety.Service
	sites                      *sites.Service
	notifications              *notify.Service
	access                     accessCache
	externalData               *externaldata.Service
	capacityMQTT               ports.CapacityRetainedCleaner
	messageTopicKafka          messageTopicKafkaAdmin
	messageTopicMQTTReady      func(context.Context) error
	messageTopicCredentialsMu  sync.Mutex
	// aiSync serialises AI settings and Agent changes with the periodic
	// reconciliation of API replicas and Harness instances.
	aiSync          sync.Locker
	aiManifestStore ports.AIWorkflowManifestStore
	// optionalHealth lists dependencies that degrade but never fail readiness.
	optionalHealth map[string]func(context.Context) error
}

func New(cfg config.Config, engine *core.Engine, m *metrics.Registry, log *slog.Logger) *Server {
	// Handler logs carry the request ID, tenant and user from the request
	// context; wrap a logger that does not do so already.
	if log != nil {
		if _, ok := log.Handler().(*logctx.Handler); !ok {
			log = slog.New(logctx.NewHandler(log.Handler()))
		}
	}
	if _, ok := engine.Repo.(*deviceScopeRepository); !ok {
		engine.Repo = ScopedRepository(engine.Repo)
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.HandleMethodNotAllowed = true
	router.RedirectTrailingSlash = false
	s := &Server{
		cfg:                        cfg,
		engine:                     engine,
		fireSafety:                 firesafety.New(engine.Repo),
		sites:                      siteService(engine),
		onboarding:                 onboarding.New(engine.Repo, engine.Parsers, cfg.DataDir, cfg.ModbusAllowedCIDRs),
		auth:                       auth.New(cfg.JWTSecret),
		harnessAuth:                auth.New(auth.HarnessSecret(cfg.JWTSecret, cfg.HarnessJWTSecret)),
		metrics:                    m,
		log:                        log,
		router:                     router,
		healthInspectionEstimateMs: healthInspectionEstimateDefault.Milliseconds(),
		logins:                     newLoginLimiter(nil),
		inspectionPDFs:             newInspectionPDFCache(),
		inspectionRequests:         make(chan struct{}, 8),
		aiAnalysisEstimateMs:       45000,
		events:                     newEventSnapshots(),
	}
	if engine.HarnessTokens == nil {
		// Chat and business runs sign MCP credentials with the Harness key
		// unless the process wired a dedicated issuer.
		engine.HarnessTokens = s.harnessAuth
	}
	s.ai = aiworkflow.New(engine, s)
	s.ai.Quota = &aiQuota{server: s, now: time.Now}
	s.onboarding.LoadRaw = engine.GetRaw
	s.onboarding.RequirePrepared = true
	s.onboarding.PublicHTTP = publicEndpoint(cfg.DeviceHTTPPublicURL)
	s.onboarding.PublicMQTT = publicEndpoint(cfg.MQTTPublicURL)
	if engine.MessageTopics != nil {
		engine.MessageTopics.SetAccessResolver(s.messageTopicIdentity)
	}
	var externalErr error
	s.externalData, externalErr = externaldata.New(s.unscopedRepo().ExternalDataStore(), cfg.JWTSecret, s.authorizeExternalSource, s.deliverExternalEvent)
	if externalErr != nil {
		log.Error("external data initialization failed", "error", externalErr)
	} else {
		s.externalData.SetOutbound(netguard.Policy{Allowed: cfg.ExternalDataAllowedCIDRs})
	}
	router.Use(requestID(), s.cors(), s.security(), s.accessLog(), s.recovery())
	s.routes()
	return s
}
func (s *Server) Handler() http.Handler { return s.videoRouting(s.roleHandler()) }

func (s *Server) SetKnowledgeJobs(jobs ports.KnowledgeDocumentJobs) { s.knowledgeJobs = jobs }

func (s *Server) SetEmbeddingRuntime(runtime ports.EmbeddingRuntime) { s.embeddingRuntime = runtime }

func (s *Server) SetAIProviderRuntime(runtime ports.AIProviderRuntime) {
	s.aiProviderRuntime = runtime
}

func (s *Server) SetAIProviderStore(store ports.AIProviderConfigStore) {
	s.aiProviderStore = store
}

func (s *Server) SetAIWorkflowProvider(runtime ports.AIWorkflowProviderRuntime) {
	s.aiWorkflowProvider = runtime
}

// SetAISync shares the reconciliation lock and the desired Agent store.
func (s *Server) SetAISync(lock sync.Locker, manifests ports.AIWorkflowManifestStore) {
	s.aiSync, s.aiManifestStore = lock, manifests
}

func (s *Server) lockAISync() func() {
	if s.aiSync == nil {
		return func() {}
	}
	s.aiSync.Lock()
	return s.aiSync.Unlock
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
	in.TenantID = strings.TrimSpace(in.TenantID)
	if in.TenantID == "" {
		in.TenantID = "tenant_001"
	}
	account := in.TenantID + "\x00" + in.Username
	if wait := s.logins.retryAfter(account); wait > 0 {
		lockedOut(w, wait)
		return
	}
	if in.Username != s.cfg.AdminUser {
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		s.loginManaged(recorder, r, in.Username, in.Password, in.TenantID)
		// Only a wrong password counts; an unavailable user store does not.
		if recorder.status == http.StatusOK || recorder.status == http.StatusUnauthorized {
			s.logins.record(account, recorder.status == http.StatusOK)
		}
		return
	}
	given, want := sha256.Sum256([]byte(in.Password)), sha256.Sum256([]byte(s.cfg.AdminPassword))
	if subtle.ConstantTimeCompare(given[:], want[:]) != 1 {
		s.logins.record(account, false)
		problem(w, 401, "invalid credentials")
		return
	}
	s.logins.record(account, true)
	if !adminTenantAllowed(s.cfg.AdminTenants, in.TenantID) {
		problem(w, http.StatusForbidden, "admin tenant is not allowed")
		return
	}
	token, err := s.auth.IssueWithVersion(in.Username, in.TenantID, "admin", s.adminSessionVersion(), 8*time.Hour)
	if err != nil {
		s.failure(w, r, err, "无法签发登录凭据")
		return
	}
	write(w, 200, map[string]any{"accessToken": token, "expiresIn": 28800, "tenantId": in.TenantID, "role": "admin", "permissions": []string{"*"}, "platformVersion": version.Version})
}

// adminSessionVersion derives the built-in administrator's session version
// from its password: changing IOT_ADMIN_PASSWORD invalidates earlier tokens.
func (s *Server) adminSessionVersion() int64 {
	sum := sha256.Sum256([]byte("admin-session:" + s.cfg.AdminPassword))
	return int64(binary.BigEndian.Uint64(sum[:8]) >> 1)
}

// reissueToken renews the caller's management token for background work,
// keeping the session version that lets revocation apply to the copy.
func (s *Server) reissueToken(c auth.Claims, ttl time.Duration) (string, error) {
	if c.TokenUse == "user" {
		return s.auth.IssueUser(c.Username, c.TenantID, c.SessionVersion, ttl)
	}
	return s.auth.IssueWithVersion(c.Username, c.TenantID, c.Role, c.SessionVersion, ttl)
}

// readinessTimeout bounds each dependency check; checks run concurrently so
// one slow dependency cannot make the others look unavailable.
const readinessTimeout = 3 * time.Second

// SetOptionalHealth registers a dependency whose failure degrades the
// platform but must not take the instance out of a load balancer, such as
// the Redis cache.
func (s *Server) SetOptionalHealth(name string, check func(context.Context) error) {
	if s.optionalHealth == nil {
		s.optionalHealth = map[string]func(context.Context) error{}
	}
	s.optionalHealth[name] = check
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	// Each role is ready when the dependencies of its own components are.
	required := map[string]func(context.Context) error{"repository": s.engine.Repo.Health, "eventBus": s.engine.Bus.Health, "realtime": s.engine.Realtime.Health}
	if s.cfg.Runs(config.ComponentAccess) || s.cfg.Runs(config.ComponentParser) || s.cfg.Runs(config.ComponentManagement) {
		required["archive"] = s.engine.Archive.Health
	}
	if s.engine.KB != nil && s.cfg.Runs(config.ComponentManagement) {
		required["knowledge"] = s.engine.KB.Health
	}
	type result struct {
		name     string
		optional bool
		err      error
		took     time.Duration
	}
	results := make(chan result, len(required)+len(s.optionalHealth))
	run := func(name string, optional bool, check func(context.Context) error) {
		ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
		defer cancel()
		start := time.Now()
		err := check(ctx)
		results <- result{name: name, optional: optional, err: err, took: time.Since(start)}
	}
	for name, check := range required {
		go run(name, false, check)
	}
	for name, check := range s.optionalHealth {
		go run(name, true, check)
	}
	checks := map[string]string{}
	durations := map[string]int64{}
	status, degraded := http.StatusOK, false
	for range len(required) + len(s.optionalHealth) {
		res := <-results
		durations[res.name] = res.took.Milliseconds()
		ok := 1.0
		switch {
		case res.err == nil:
			checks[res.name] = "ok"
		case res.optional:
			// The probe is unauthenticated: report which dependency failed, not why.
			s.log.WarnContext(r.Context(), "optional readiness check failed", "dependency", res.name, "error", res.err)
			checks[res.name] = "degraded"
			degraded, ok = true, 0
		default:
			s.log.WarnContext(r.Context(), "readiness check failed", "dependency", res.name, "error", res.err)
			checks[res.name] = "unavailable"
			status, degraded, ok = http.StatusServiceUnavailable, true, 0
		}
		if s.metrics != nil {
			s.metrics.Set(metrics.Series("readiness_ok", "dependency", res.name), ok)
		}
	}
	role := s.cfg.ProcessRole
	if role == "" {
		role = config.RoleCombined
	}
	write(w, status, map[string]any{"status": map[bool]string{false: "ok", true: "degraded"}[degraded], "checks": checks, "durationsMs": durations, "role": role, "instance": s.cfg.InstanceID})
}
func (s *Server) products(w http.ResponseWriter, r *http.Request) {
	pagination := parseListPagination(r)
	items, total, err := s.engine.Repo.ListProductsPage(r.Context(), claims(r).TenantID, pagination.PageSize, pagination.Offset)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	for i := range items {
		status, ready, e := s.onboarding.TemplateReadiness(r.Context(), claims(r).TenantID, items[i].ID)
		if e != nil {
			s.failure(w, r, e, "读取模板准备状态失败")
			return
		}
		items[i].PreparationStatus, items[i].Reusable = status, ready
	}
	writeList(w, 200, items, total, pagination, nil)
}

// productBindingCheck lists templates without a usable protocol; their raw
// messages fail to parse unless they use the platform's standard format.
func (s *Server) productBindingCheck(w http.ResponseWriter, r *http.Request) {
	items, err := s.engine.UnboundProducts(r.Context(), claims(r).TenantID)
	if err != nil {
		s.failure(w, r, err, "检查设备模板协议绑定失败")
		return
	}
	write(w, 200, map[string]any{"items": items})
}

func (s *Server) saveProduct(w http.ResponseWriter, r *http.Request) {
	var v model.Product
	if decode(w, r, &v) != nil {
		return
	}
	c := claims(r)
	v.TenantID = c.TenantID
	v.PreparationStatus = ""
	v.Reusable = false
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
	if err := model.ValidateDeviceTiming(v.ReportIntervalSec, v.OfflineToleranceSec); err != nil {
		problem(w, 422, err.Error())
		return
	}
	if v.VerificationRules != nil {
		rules, e := onboarding.NormalizeVerificationRules(*v.VerificationRules)
		if e != nil {
			s.enrollProblem(w, r, e)
			return
		}
		v.VerificationRules = &rules
	}
	pkg, err := s.productProtocol(r.Context(), c.TenantID, v.ProtocolPackageID)
	if err != nil {
		problem(w, 422, "协议不可用，请选择内置标准上报或已发布的协议版本")
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
	newProduct, timingChanged := false, false
	if old, getErr := s.engine.Repo.GetProduct(r.Context(), c.TenantID, v.ID); getErr == nil {
		v.CreatedAt = old.CreatedAt
		timingChanged = old.ReportIntervalSec != v.ReportIntervalSec || old.OfflineToleranceSec != v.OfflineToleranceSec
		if v.ProtocolPackageID != old.ProtocolPackageID {
			problem(w, 409, "协议版本变更请在模板准备流程中保存候选配置并明确应用")
			return
		}
		if v.VerificationRules == nil {
			v.VerificationRules = old.VerificationRules
		}
		before := model.TemplateCandidate{Product: old, VerificationRules: onboarding.ProductVerificationRules(old)}
		after := model.TemplateCandidate{Product: v, VerificationRules: onboarding.ProductVerificationRules(v)}
		if onboarding.CandidateFingerprint(before) != onboarding.CandidateFingerprint(after) {
			_, count, e := s.engine.Repo.ListManagedDevicesFiltered(r.Context(), ports.DeviceFilter{TenantID: c.TenantID, RestrictProducts: true, ProductIDs: []string{v.ID}}, 1, 0)
			if e != nil {
				s.failure(w, r, e, "读取模板使用情况失败")
				return
			}
			if count > 0 {
				problem(w, 409, "运行中的模板配置请通过模板准备流程联调后应用")
				return
			}
		}
	} else if errors.Is(getErr, model.ErrNotFound) {
		newProduct = true
	} else {
		s.internalError(w, r, getErr)
		return
	}
	if v.CreatedAt == 0 {
		v.CreatedAt = now
	}
	v.UpdatedAt = now
	if err = s.engine.Repo.SaveProduct(r.Context(), v); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.engine.ProtocolsChanged(c.TenantID)
	// Versioned protocols are parsed by the bound release; the binding is their single source.
	_, releaseErr := s.engine.Repo.GetProtocolRelease(r.Context(), c.TenantID, pkg.Protocol, pkg.Version)
	if newProduct && v.ProtocolPackageID != parser.StandardProtocolID+"@1.0.0" && releaseErr == nil {
		if _, err := s.bindProtocolRelease(r, pkg.Protocol, pkg.Version, v.ID); err != nil {
			bindingProblem(w, err)
			return
		}
		v, err = s.engine.Repo.GetProduct(r.Context(), c.TenantID, v.ID)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
	}
	if timingChanged {
		s.applyTemplateTiming(c.TenantID, v.ID)
	}
	s.audit(r, "product.save", "product", v.ID, map[string]any{"status": v.Status, "reportIntervalSec": v.ReportIntervalSec, "offlineToleranceSec": v.OfflineToleranceSec})
	write(w, 201, v)
}

// applyTemplateTiming moves existing device states of a template to its new
// reporting timing in the background; a large template must not hold the
// request open.
func (s *Server) applyTemplateTiming(tenant, productID string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if changed, err := s.engine.ApplyDeviceTiming(ctx, tenant, productID, ""); err != nil {
			s.log.Error("apply template reporting timing failed", "tenant", tenant, "product", productID, "updated", changed, "error", err)
		}
	}()
}

func (s *Server) protocolPackages(w http.ResponseWriter, r *http.Request) {
	pagination := parseListPagination(r)
	items, total, err := s.engine.Repo.ListProtocolPackagesPage(r.Context(), claims(r).TenantID, pagination.PageSize, pagination.Offset)
	if err != nil {
		s.internalError(w, r, err)
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
	if v.ParserType == parser.GoProtocolParserName {
		problem(w, 422, "Go 协议请通过源码编译、样例验证和版本发布接口管理")
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
		s.internalError(w, r, err)
		return
	}
	s.audit(r, "protocol.save", "protocolPackage", v.ID, map[string]any{"version": v.Version, "status": v.Status})
	write(w, 201, v)
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
	filter, err := s.deviceFilter(r.Context(), tenantID, r.URL.Query())
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	// The scope-aware repository filters limited users before totals and pagination.
	items, total, err := s.engine.Repo.ListManagedDevicesFiltered(r.Context(), filter, pagination.PageSize, pagination.Offset)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	deviceIDs := make([]string, 0, len(items))
	for _, item := range items {
		deviceIDs = append(deviceIDs, item.ID)
	}
	childCounts, err := s.engine.Repo.CountManagedDeviceChildren(r.Context(), tenantID, deviceIDs)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	productIDs := make([]string, 0, len(items))
	for _, item := range items {
		productIDs = append(productIDs, item.ProductID)
	}
	products, err := s.engine.Repo.GetProductsByIDs(r.Context(), tenantID, productIDs)
	if err != nil {
		products = map[string]model.Product{}
		for _, id := range productIDs {
			if product, getErr := s.engine.Repo.GetProduct(r.Context(), tenantID, id); getErr == nil {
				products[id] = product
			}
		}
	}
	states, err := s.engine.Repo.GetDeviceStatesByIDs(r.Context(), tenantID, deviceIDs)
	if err != nil {
		states = map[string]model.DeviceState{}
		for _, id := range deviceIDs {
			if state, getErr := s.engine.Repo.GetDeviceState(r.Context(), tenantID, id); getErr == nil {
				states[id] = state
			}
		}
	}
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
	out := make([]map[string]any, 0, len(items))
	for _, v := range items {
		product := products[v.ProductID]
		row := map[string]any{"device": v.Public(product), "childCount": childCounts[v.ID], "credentialSupported": v.UsesPlatformCredentials(product)}
		if parent := parents[v.GatewayID]; parent != nil {
			row["parent"] = parent
		}
		if state, ok := states[v.ID]; ok {
			row["runtimeState"] = state
		}
		out = append(out, row)
	}
	writeList(w, 200, out, total, pagination, nil)
}
func (s *Server) saveManagedDevice(w http.ResponseWriter, r *http.Request) {
	// An embedded ManagedDevice would promote UnmarshalJSON and silently ignore
	// Trial. Decode the method-free alias, then normalize its historical tags.
	type deviceFields model.ManagedDevice
	var input struct {
		deviceFields
		Trial bool `json:"trial,omitempty"`
	}
	if decode(w, r, &input) != nil {
		return
	}
	v := model.ManagedDevice(input.deviceFields)
	v.NormalizeConnectionTags()
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
	if err := model.ValidateDeviceTiming(v.ReportIntervalSec, v.OfflineToleranceSec); err != nil {
		problem(w, 422, err.Error())
		return
	}
	product, err := s.engine.Repo.GetProduct(r.Context(), c.TenantID, v.ProductID)
	if err != nil {
		problem(w, 422, "product not found")
		return
	}
	now := time.Now().UnixMilli()
	created, timingChanged := false, false
	if old, err := s.engine.Repo.GetManagedDevice(r.Context(), c.TenantID, v.ID); err == nil {
		timingChanged = old.ReportIntervalSec != v.ReportIntervalSec || old.OfflineToleranceSec != v.OfflineToleranceSec
		if old.RegistrationSource == "PROTOCOL_CHILD_AUTO" {
			if v.ProductID != old.ProductID || v.GatewayID != old.GatewayID || v.DeviceRole != "CHILD" {
				problem(w, 422, "自动注册子设备的产品与主设备归属不可直接改写")
				return
			}
		}
		if v.ProductID != old.ProductID {
			problem(w, 409, "已登记设备不能直接更换设备模板，请从目标模板重新接入")
			return
		}
		v.AccessKey, v.SecretHash, v.SecretHint, v.CreatedAt = old.AccessKey, old.SecretHash, old.SecretHint, old.CreatedAt
		if v.Tags == nil {
			v.Tags = map[string]string{}
		}
		// Platform connection fields keep their stored values; the connection and
		// child address may be re-selected but are not cleared by an edit.
		v.Connector, v.ChildType, v.OnboardingRequestHash = old.Connector, old.ChildType, old.OnboardingRequestHash
		if v.ConnectorProfileID == "" {
			v.ConnectorProfileID = old.ConnectorProfileID
		}
		if v.ChildAddress == "" {
			v.ChildAddress = old.ChildAddress
		}
		if v.RegistrationSource == "" {
			v.RegistrationSource = old.RegistrationSource
		}
		if old.AutoRegistered {
			v.AutoRegistered = true
		}
	} else if errors.Is(err, model.ErrNotFound) {
		created = true
		v.CreatedAt = now
	} else {
		s.failure(w, r, err, "读取设备登记信息失败")
		return
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
		// The parent must be registered as a gateway; the template category alone does not grant it.
		if gateway.DeviceRole != "GATEWAY" {
			problem(w, 422, "selected parent device is not a gateway")
			return
		}
	} else {
		v.GatewayID = ""
	}
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
			s.internalError(w, r, profileErr)
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
	if created {
		s.enrollCompatibleDevice(w, r, v, product, input.Trial, "device.save")
		return
	}
	v.UpdatedAt = now
	if err := s.engine.Repo.SaveManagedDevice(r.Context(), v); err != nil {
		s.internalError(w, r, err)
		return
	}
	if timingChanged {
		if _, err := s.engine.ApplyDeviceTiming(r.Context(), c.TenantID, v.ProductID, v.ID); err != nil {
			s.log.ErrorContext(r.Context(), "apply device reporting timing failed", "device", v.ID, "error", err)
		}
	}
	s.audit(r, "device.save", "device", v.ID, map[string]any{"productId": v.ProductID, "status": v.Status})
	write(w, 201, map[string]any{"device": v.Public(product)})
}
func (s *Server) registerDiscoveredDevice(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Trial              bool   `json:"trial,omitempty"`
		ConnectorProfileID string `json:"connectorProfileId,omitempty"`
	}
	// Discovery confirmation historically accepted an empty body.
	if r.Body != nil && r.ContentLength != 0 {
		if decode(w, r, &input) != nil {
			return
		}
	}
	c := claims(r)
	id := r.PathValue("id")
	if _, err := s.engine.Repo.GetManagedDevice(r.Context(), c.TenantID, id); err == nil {
		problem(w, 409, "device is already registered")
		return
	} else if !errors.Is(err, model.ErrNotFound) {
		s.failure(w, r, err, "读取设备登记信息失败")
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
	device := model.ManagedDevice{ID: id, TenantID: c.TenantID, ProductID: state.ProductID, Name: "发现设备 " + id, Status: "ENABLED", ConnectorProfileID: input.ConnectorProfileID}
	s.enrollCompatibleDevice(w, r, device, product, input.Trial, "device.discovery.register")
}

func (s *Server) rotateDeviceCredential(w http.ResponseWriter, r *http.Request) {
	if !s.operationDevice(w, r) {
		return
	}
	c, v, e := s.onboarding.ChangeCredential(r.Context(), claims(r).TenantID, r.PathValue("id"), true)
	if e != nil {
		status := http.StatusInternalServerError
		if errors.Is(e, onboarding.ErrCredentialUnsupported) {
			status = http.StatusUnprocessableEntity
		}
		problem(w, status, e.Error())
		return
	}
	s.audit(r, "device.credential.rotate", "device", r.PathValue("id"), nil)
	write(w, 200, map[string]any{"deviceId": r.PathValue("id"), "credential": c, "revocation": v})
}
func (s *Server) debugDeviceIngest(w http.ResponseWriter, r *http.Request) {
	// 接入测试报文仍走正常归档与解析链路；设备归属从当前租户的登记记录确定。
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
	if ingestBusy(w, err) {
		return
	}
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	s.audit(r, "device.debug.ingest", "device", v.ID, map[string]any{"messageId": idx.MessageID})
	write(w, map[bool]int{true: 201, false: 200}[created], map[string]any{"created": created, "archive": idx})
}
func (s *Server) deviceIngest(w http.ResponseWriter, r *http.Request) {
	accessKey, secret := r.Header.Get("X-Device-Key"), r.Header.Get("X-Device-Secret")
	v, err := s.onboarding.Authenticate(r.Context(), accessKey, secret)
	if deviceAuthUnavailable(w, err) {
		return
	}
	if err != nil || v.ID != r.PathValue("deviceId") {
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
	// Credential-authenticated reports are field evidence; debug ingress keeps its own source.
	raw.Source = "device-http"
	idx, created, err := s.engine.IngestRaw(r.Context(), raw)
	if ingestBusy(w, err) {
		return
	}
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	write(w, map[bool]int{true: 201, false: 200}[created], map[string]any{"created": created, "messageId": idx.MessageID, "receivedAt": idx.ReceivedAt})
}
func (s *Server) prepareManagedRaw(ctx context.Context, raw *model.RawMessage, device model.ManagedDevice) error {
	if device.Connector == "HTTP" || device.Connector == "MQTT" {
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
	s.metrics.ObserveIn("raw_archive_duration_seconds", metrics.RequestBuckets, time.Since(start).Seconds())
	if ingestBusy(w, err) {
		return
	}
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
	filter, filterErr := parseRawFilter(q)
	if filterErr != nil {
		problem(w, http.StatusBadRequest, filterErr.Error())
		return
	}
	filter.TenantID, filter.Limit, filter.Offset = c.TenantID, pagination.PageSize, pagination.Offset
	items, err := s.engine.Repo.ListRawIndexes(r.Context(), filter)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	total, err := s.engine.Repo.CountRawIndexes(r.Context(), filter)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.MessageID)
	}
	messages, err := s.engine.Repo.GetStandardMessagesByRawIDs(r.Context(), c.TenantID, ids)
	if err != nil {
		messages = map[string]model.StandardMessage{}
		for _, id := range ids {
			if message, getErr := s.engine.Repo.GetStandardMessageByRaw(r.Context(), c.TenantID, id); getErr == nil {
				messages[id] = message
			}
		}
	}
	for i := range items {
		if message, ok := messages[items[i].MessageID]; ok {
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
		s.failure(w, r, err, "raw archive could not be read")
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
		s.failure(w, r, err, "raw archive could not be read")
		return
	}
	body, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		s.internalError(w, r, err)
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
			s.failure(w, r, err, "raw archive could not be read: "+id)
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
			s.internalError(w, r, err)
			return
		}
		name := fmt.Sprintf("报文/%03d_%s.json", i+1, safeAttachmentName(item.Index.MessageID))
		file, err := zw.Create(name)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		if _, err = file.Write(append(body, '\n')); err != nil {
			s.internalError(w, r, err)
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
		s.internalError(w, r, err)
		return
	}
	if err = zw.Close(); err != nil {
		s.internalError(w, r, err)
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
	v.CapacityRunID = capacityRequestRunID(r)
	task, err := s.engine.StartReplay(r.Context(), v)
	if errors.Is(err, core.ErrReplayBusy) {
		problem(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	write(w, 202, task)
}

// cancelReplay stops a running replay; it must reach the API instance that
// runs it, which a single-replica deployment always does.
func (s *Server) cancelReplay(w http.ResponseWriter, r *http.Request) {
	if limited(r.Context()) {
		problem(w, 404, "replay not found")
		return
	}
	if err := s.engine.CancelReplay(claims(r).TenantID, r.PathValue("id")); err != nil {
		problem(w, http.StatusConflict, err.Error())
		return
	}
	s.audit(r, "replay.cancel", "replay", r.PathValue("id"), nil)
	write(w, http.StatusAccepted, map[string]any{"cancelling": true})
}
func (s *Server) getReplay(w http.ResponseWriter, r *http.Request) {
	v, err := s.engine.Repo.GetReplay(r.Context(), r.PathValue("id"))
	if err != nil {
		problem(w, 404, "replay not found")
		return
	}
	// Only full-scope users can start a replay, so a limited user never owns one.
	if v.TenantID != claims(r).TenantID || limited(r.Context()) {
		problem(w, 404, "replay not found")
		return
	}
	write(w, 200, s.engine.RefreshReplay(r.Context(), v))
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
		s.internalError(w, r, err)
		return
	}
	_, online, err := s.engine.Repo.CountDeviceStates(r.Context(), tenantID, unregisteredOnly)
	if err != nil {
		s.internalError(w, r, err)
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
	if !validPropertyName(property) {
		problem(w, 422, "property name is invalid")
		return
	}
	pagination := parseListPagination(r)
	items, total, err := s.engine.Repo.PropertyHistoryPage(r.Context(), claims(r).TenantID, r.PathValue("deviceId"), property, i64(q.Get("start")), i64(q.Get("end")), pagination.PageSize, pagination.Offset)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeList(w, 200, items, total, pagination, nil)
}

// validPropertyName bounds the free-form property name used by history
// queries: non-identifier names reach the storage layer as string literals.
func validPropertyName(name string) bool {
	if len(name) > 256 {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
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
		s.internalError(w, r, err)
		return
	}
	writeList(w, 200, v, total, pagination, nil)
}

// ruleFields lists the thing-model fields of a product for the rule editor.
func (s *Server) ruleFields(w http.ResponseWriter, r *http.Request) {
	productID := strings.TrimSpace(r.URL.Query().Get("productId"))
	if productID == "" {
		problem(w, http.StatusUnprocessableEntity, "请选择设备模板")
		return
	}
	product, err := s.engine.Repo.GetProduct(r.Context(), claims(r).TenantID, productID)
	if err != nil {
		problem(w, http.StatusNotFound, "设备模板不存在")
		return
	}
	write(w, http.StatusOK, map[string]any{"items": core.RuleFields(product)})
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
			s.internalError(w, r, err)
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
			s.internalError(w, r, err)
			return
		}
	}
	if err := s.engine.Repo.SaveRule(r.Context(), v); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.engine.RulesChanged(c.TenantID)
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
		s.internalError(w, r, err)
		return
	}
	total, err := s.engine.Repo.CountAlarms(r.Context(), filter)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	deviceIDs := make([]string, 0, len(items))
	for _, item := range items {
		deviceIDs = append(deviceIDs, item.DeviceID)
	}
	cameras, err := s.engine.ListCameraSummariesForDevices(r.Context(), claims(r).TenantID, deviceIDs)
	if err == nil {
		for index := range items {
			if linked := cameras[items[index].DeviceID]; len(linked) > 0 || items[index].Source != "video" {
				items[index].Cameras = linked
			}
		}
	} else {
		for index := range items {
			if summaries, getErr := s.engine.ListCameraSummaries(r.Context(), items[index].TenantID, items[index].DeviceID); getErr == nil {
				items[index].Cameras = summaries
			}
		}
	}
	writeList(w, 200, items, total, pagination, nil)
}
func (s *Server) alarm(w http.ResponseWriter, r *http.Request) {
	v, err := s.engine.Repo.GetAlarm(r.Context(), claims(r).TenantID, r.PathValue("id"))
	if err != nil {
		problem(w, 404, "alarm not found")
		return
	}
	if cameras, cameraErr := s.engine.ListCameraSummaries(r.Context(), v.TenantID, v.DeviceID); cameraErr == nil && (len(cameras) > 0 || v.Source != "video") {
		// Video analysis alarms name the camera itself as their source; keep
		// the camera summary recorded with the alarm when no device is linked.
		v.Cameras = cameras
	}
	if v.Location == nil {
		if state, err := s.sites.Snapshot(r.Context(), v.TenantID); err == nil {
			if v.Location = sites.Locate(state, v.DeviceID, v.ComponentID); v.Location != nil {
				v.Location.Current = true
			}
		}
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

func (s *Server) mqttToken(w http.ResponseWriter, r *http.Request) {
	c := claims(r)
	// Only the built-in administrator reaches this handler (allowsRoute
	// refuses managed users), so the tenant-wide wildcard subscriptions
	// never reach an account limited to some devices.
	scope := []string{fmt.Sprintf("/iot/parsed/%s/#", c.TenantID), fmt.Sprintf("/iot/alarm/%s/#", c.TenantID), fmt.Sprintf("/iot/device/state/%s/#", c.TenantID), fmt.Sprintf("/iot/ui-action/%s", c.TenantID)}
	// Broker-only credentials: never usable as a console token.
	token, err := s.auth.IssueBrowserMQTT(c.Username, c.TenantID, scope, 15*time.Minute)
	if err != nil {
		s.failure(w, r, err, "创建消息令牌失败")
		return
	}
	write(w, 200, map[string]any{"username": auth.BrowserMQTTUsername(c.Username), "token": token, "expiresIn": 900, "subscriptions": scope, "websocketUrl": s.mqttWebSocketURL(r)})
}

// standardDeviceTokenTTL keeps standard device tokens short unless revoked
// credentials can be banned and kicked at the broker at once; the broker
// disconnects a session when its token expires, so short tokens force every
// device to reconnect that often.
func (s *Server) standardDeviceTokenTTL() time.Duration {
	if s.onboarding.RevokeUsername == nil {
		return 5 * time.Minute
	}
	if s.cfg.MQTTDeviceTokenTTL <= 0 {
		return 24 * time.Hour
	}
	return s.cfg.MQTTDeviceTokenTTL
}

// ingestBusy answers 429 with Retry-After while ingest is paused by backpressure.
func ingestBusy(w http.ResponseWriter, err error) bool {
	if !errors.Is(err, model.ErrBackpressure) {
		return false
	}
	w.Header().Set("Retry-After", "15")
	problem(w, http.StatusTooManyRequests, err.Error())
	return true
}

// deviceAuthUnavailable answers 503 when credentials could not be checked, so a
// repository failure is not reported to the device as a revoked credential.
func deviceAuthUnavailable(w http.ResponseWriter, err error) bool {
	if !errors.Is(err, onboarding.ErrUnavailable) {
		return false
	}
	w.Header().Set("Retry-After", "5")
	problem(w, http.StatusServiceUnavailable, "device credential check temporarily unavailable")
	return true
}

func (s *Server) deviceMQTTToken(w http.ResponseWriter, r *http.Request) {
	accessKey, secret := r.Header.Get("X-Device-Key"), r.Header.Get("X-Device-Secret")
	v, err := s.onboarding.Authenticate(r.Context(), accessKey, secret)
	if deviceAuthUnavailable(w, err) {
		return
	}
	if err != nil {
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
	if v.Connector == "MQTT" || v.Connector == "HTTP" {
		acl = nil
		ttl = s.standardDeviceTokenTTL()
		topic = fmt.Sprintf("/iot/up/%s/%s/%s/property", v.TenantID, v.ProductID, v.ID)
	}
	for _, kind := range []string{"property", "event", "alarm", "state", "command-reply"} {
		acl = append(acl, auth.ACLRule{Permission: "allow", Action: "publish", Topic: fmt.Sprintf("/iot/up/%s/%s/%s/%s", v.TenantID, v.ProductID, v.ID, kind)})
	}
	acl = append(acl, auth.ACLRule{Permission: "allow", Action: "subscribe", Topic: fmt.Sprintf("/iot/down/%s/%s/%s/command", v.TenantID, v.ProductID, v.ID)})

	receiptTopic := fmt.Sprintf("/iot/down/%s/%s/%s/receipt", v.TenantID, v.ProductID, v.ID)
	acl = append(acl, auth.ACLRule{Permission: "allow", Action: "subscribe", Topic: receiptTopic})

	token, err := s.auth.IssueWithACL(v.AccessKey, v.TenantID, "device", nil, acl, ttl)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	response := map[string]any{"username": v.AccessKey, "token": token, "expiresIn": int(ttl.Seconds()), "publishTopic": topic, "receiptTopic": receiptTopic, "websocketUrl": s.mqttWebSocketURL(r)}
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
		s.internalError(w, r, err)
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
func (s *Server) videoCameras(w http.ResponseWriter, r *http.Request) {
	pagination := parseListPagination(r)
	items, total, err := s.engine.Repo.ListVideoCameraMappingsPage(r.Context(), claims(r).TenantID, pagination.PageSize, pagination.Offset)
	if err != nil {
		s.internalError(w, r, err)
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
	writeList(w, 200, s.attachLiveSummaries(r, items), total, pagination, nil)
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
		s.internalError(w, r, err)
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
	var previousCamera *model.VideoCameraMapping
	if previous, err := s.engine.Repo.GetVideoCameraMapping(r.Context(), c.TenantID, v.CameraID); err == nil {
		previousCamera = &previous
		if strings.HasPrefix(previous.VideoPlatformID, "gb28181/") {
			v.VideoPlatformID = previous.VideoPlatformID
		}
	}
	v.StreamURL = ""
	v.StreamType = ""
	v.SDKEndpoint = ""
	v.SDKCameraID = ""
	v.SDKCredentialRef = ""
	v.UpdatedAt = time.Now().UnixMilli()
	if err := s.engine.Repo.SaveVideoCameraMapping(r.Context(), v); err != nil {
		s.internalError(w, r, err)
		return
	}
	// Live configuration is stored separately and is never touched here; only
	// playback that depended on the old association is ended.
	s.videoCameraChanged(previousCamera, v)
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
	s.engine.RecordAudit(r.Context(), model.AuditLog{ID: "audit_" + randomHex(10), TenantID: c.TenantID, Actor: c.Username, Action: action, TargetType: targetType, TargetID: targetID, Details: details, CreatedAt: time.Now().UnixMilli()})
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
		if claimsValue.TokenUse != "" && claimsValue.TokenUse != "user" {
			ginProblem(c, http.StatusForbidden, "此专用凭据不能用于管理接口")
			c.Abort()
			return
		}
		if claimsValue.TokenUse == "" && s.cfg.AdminUser != "" && claimsValue.Username == s.cfg.AdminUser && claimsValue.SessionVersion != s.adminSessionVersion() {
			ginProblem(c, http.StatusUnauthorized, "管理员凭据已更新，请重新登录")
			c.Abort()
			return
		}
		allowed := claimsValue.Role == "admin" || claimsValue.Role == role || role == "viewer" && (claimsValue.Role == "operator" || claimsValue.Role == "viewer")
		if claimsValue.TokenUse == "user" {
			user, permissions, err := s.managedIdentity(c.Request.Context(), claimsValue)
			if err != nil {
				ginProblem(c, 401, "账户已停用或会话已失效，请重新登录")
				c.Abort()
				return
			}
			allowed = allowsRoute(permissions, c.Request.Method, c.FullPath())
			scope := s.scopeFor(user, permissions, claimsValue.TenantID)
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), deviceScopeKey{}, scope))
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), permissionsKey{}, permissions))
			if allowed {
				// The status stays 403 (the established contract); the code tells
				// a scope denial from a missing permission.
				if !s.allowScopedRequest(c, scope) {
					ginProblemCode(c, http.StatusForbidden, codeDeviceScopeDenied, "该资源或操作超出当前账户的设备范围")
					c.Abort()
					return
				}
			}
		}
		if !allowed {
			ginProblemCode(c, http.StatusForbidden, codeRoleDenied, "insufficient role")
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
		claimsValue, err := s.harnessAuth.Parse(token)
		if err != nil {
			ginProblem(c, http.StatusUnauthorized, err.Error())
			c.Abort()
			return
		}
		if claimsValue.TokenUse != "harness" || !claimsValue.HasAudience(auth.HarnessAudience) || claimsValue.RunID == "" || claimsValue.TenantID == "" {
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
		if claimsValue.ManagedUser {
			user, permissions, err := s.managedIdentity(c.Request.Context(), claimsValue)
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
			} else if !chatAllowed(permissions) {
				ginProblem(c, http.StatusForbidden, "无智能助手访问权限")
				c.Abort()
				return
			}
			ctx := context.WithValue(c.Request.Context(), deviceScopeKey{}, s.scopeFor(user, permissions, claimsValue.TenantID))
			ctx = context.WithValue(ctx, permissionsKey{}, permissions)
			claimsValue.Scopes = intersectScopes(claimsValue.Scopes, workflowScopes(ctx))
			claimsValue.Permissions = permissionList(permissions)
			c.Request = c.Request.WithContext(ctx)
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
				c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Device-Key, X-Device-Secret, X-Video-Platform-ID, X-Timestamp, X-Signature, X-Request-ID")
				c.Header("Access-Control-Expose-Headers", "X-Request-ID")
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

// slowRequest is the duration above which a successful request is still
// logged at info level. Event streams are long-lived by design and excluded.
const slowRequest = 3 * time.Second

// accessLog records failed and slow requests at info level or above.
// Routine successful requests (page polling, Prometheus scrapes, health
// checks) are debug records so they do not flood the collected logs; set
// IOT_LOG_LEVEL=debug to see every request.

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)

// requestID keeps a caller's or proxy's X-Request-ID, or assigns one, and
// echoes it so logs, error references and the client name the same request.
func requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if !validRequestID.MatchString(id) {
			id = randomHex(8)
		}
		c.Header("X-Request-ID", id)
		c.Request = c.Request.WithContext(logctx.WithRequestID(c.Request.Context(), id))
		c.Next()
	}
}

func requestIDFrom(ctx context.Context) string { return logctx.RequestID(ctx) }

func (s *Server) accessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		status, duration := c.Writer.Status(), time.Since(start)
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		if s.metrics != nil && !strings.HasPrefix(c.Writer.Header().Get("Content-Type"), "text/event-stream") {
			s.metrics.ObserveIn(metrics.Series("http_request_duration_seconds", "route", route, "method", c.Request.Method, "code", strconv.Itoa(status)), metrics.RequestBuckets, duration.Seconds())
		}
		if s.log == nil {
			return
		}
		level := slog.LevelDebug
		switch {
		case status >= 500:
			level = slog.LevelWarn
		case status >= 400:
			level = slog.LevelInfo
		case duration >= slowRequest && !strings.HasPrefix(c.Writer.Header().Get("Content-Type"), "text/event-stream"):
			level = slog.LevelInfo
		}
		// requestId, tenantId and user come from the context (logctx).
		s.log.Log(c.Request.Context(), level, "http request", "method", c.Request.Method, "path", c.Request.URL.Path, "route", c.FullPath(), "status", status, "duration", duration.String())
	}
}

func (s *Server) recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			// A handler that already started a response aborts it on
			// purpose; net/http then closes the connection quietly.
			if recovered == http.ErrAbortHandler {
				panic(recovered)
			}
			reference := requestIDFrom(c.Request.Context())
			if reference == "" {
				reference = randomHex(6)
			}
			if s.log != nil {
				s.log.Error("http panic recovered", "reference", reference, "method", c.Request.Method, "path", c.Request.URL.Path, "error", fmt.Sprint(recovered), "stack", string(debug.Stack()))
			}
			c.JSON(http.StatusInternalServerError, gin.H{"type": "about:blank", "title": http.StatusText(http.StatusInternalServerError), "status": http.StatusInternalServerError, "detail": "服务内部错误（编号 " + reference + "）", "traceId": reference})
			c.Abort()
		}()
		c.Next()
	}
}

// Machine-readable error codes in problem responses. Callers decide by code
// rather than by the Chinese detail text, which may change.
const (
	codeRoleDenied        = "ROLE_DENIED"
	codeDeviceScopeDenied = "DEVICE_SCOPE_DENIED"
)

func ginProblemCode(c *gin.Context, status int, code, detail string) {
	c.JSON(status, gin.H{"type": "about:blank", "title": http.StatusText(status), "status": status, "code": code, "detail": detail})
}

func problemCode(w http.ResponseWriter, status int, code, detail string) {
	write(w, status, map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "code": code, "detail": detail})
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

// internalError answers an unexpected failure without exposing its text:
// storage and dependency errors can carry hosts, SQL or credentials. The
// reference in the response matches the logged error.
func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	reference := requestIDFrom(r.Context())
	if reference == "" {
		reference = randomHex(6)
	}
	s.log.ErrorContext(r.Context(), "request failed", "reference", reference, "method", r.Method, "path", r.URL.Path, "error", err)
	write(w, http.StatusInternalServerError, map[string]any{"type": "about:blank", "title": http.StatusText(http.StatusInternalServerError), "status": http.StatusInternalServerError, "detail": "服务内部错误，请稍后重试；如持续出现请提供编号 " + reference + " 联系管理员", "traceId": reference})
}

// metricsAuthorized checks the optional IOT_METRICS_TOKEN bearer token.
func (s *Server) metricsAuthorized(r *http.Request) bool {
	if s.cfg.MetricsToken == "" {
		return true
	}
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && subtle.ConstantTimeCompare([]byte(strings.TrimSpace(got)), []byte(s.cfg.MetricsToken)) == 1
}

// failure answers 500 with a Chinese hint and a reference, and logs err under
// that reference so the operator can find the cause ("request failed").
// Handlers use it instead of problem(w, 500, …), which loses the error.
func (s *Server) failure(w http.ResponseWriter, r *http.Request, err error, detail string) {
	if err == nil {
		err = errors.New(detail)
	}
	reference := requestIDFrom(r.Context())
	if reference == "" {
		reference = randomHex(6)
	}
	if s.log != nil {
		s.log.ErrorContext(r.Context(), "request failed", "reference", reference, "method", r.Method, "path", r.URL.Path, "detail", detail, "error", err)
	}
	write(w, http.StatusInternalServerError, map[string]any{"type": "about:blank", "title": http.StatusText(http.StatusInternalServerError), "status": http.StatusInternalServerError, "detail": detail + "（编号 " + reference + "）", "traceId": reference})
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
	return !oneOf(strings.TrimSpace(id), aiworkflow.BusinessWorkflowIDs()...)
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
