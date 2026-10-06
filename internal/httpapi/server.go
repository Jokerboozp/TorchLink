package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"iot-platform/internal/devicescope"
	"log/slog"
	"net/http"
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
	"iot-platform/internal/netguard"
	"iot-platform/internal/notify"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/opscenter"
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
	harnessAuth        *auth.Manager
	metrics            *metrics.Registry
	log                *slog.Logger
	router             *gin.Engine
	aiProviderRuntime  ports.AIProviderRuntime
	embeddingRuntime   ports.EmbeddingRuntime
	knowledgeJobs      ports.KnowledgeDocumentJobs
	aiProviderStore    ports.AIProviderConfigStore
	aiWorkflowProvider ports.AIWorkflowProviderRuntime
	aiProviderUpdateMu sync.Mutex
	inspectionRuns     *runEstimate
	logins             *loginLimiter
	inspection         inspectionReports
	analysisRuns       *runEstimate
	protocolListeners  protocolCommander
	onboarding         *onboarding.Service
	events             *eventSnapshots
	ops                *opscenter.Service
	video              atomic.Pointer[video.Service]
	videoOwner         func() (local bool, endpoint string)
	fireSafety         *firesafety.Service
	sites              *sites.Service
	notifications      *notify.Service
	access             accessCache
	externalData       *externaldata.Service
	capacityMQTT       ports.CapacityRetainedCleaner
	topicBrokers       topicBrokers
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
	// New only reads the engine: the process installs the scoped repository and
	// the Harness token issuer before the API and the engine's consumers start.
	if _, ok := engine.Repo.(*devicescope.Repository); !ok && log != nil {
		log.Warn("engine repository is not device-scoped; requests are not limited to granted devices")
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.HandleMethodNotAllowed = true
	router.RedirectTrailingSlash = false
	s := &Server{
		cfg:            cfg,
		engine:         engine,
		fireSafety:     firesafety.New(engine.Repo),
		sites:          siteService(engine),
		onboarding:     onboarding.New(engine.Repo, engine.Parsers, cfg.DataDir, cfg.ModbusAllowedCIDRs),
		auth:           auth.New(cfg.JWTSecret),
		harnessAuth:    harnessIssuer(cfg, engine),
		metrics:        m,
		log:            log,
		router:         router,
		inspectionRuns: newRunEstimate(healthInspectionEstimateDefault),
		logins:         newLoginLimiter(nil),
		inspection:     inspectionReports{pdfs: newInspectionPDFCache(), requests: make(chan struct{}, 8)},
		analysisRuns:   newRunEstimate(aiAnalysisEstimateDefault),
		events:         newEventSnapshots(),
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

// harnessIssuer verifies Harness MCP credentials with the issuer the engine
// signs them with, or with one built from the same key when none is installed.
func harnessIssuer(cfg config.Config, engine *core.Engine) *auth.Manager {
	if issuer, ok := engine.HarnessTokens.(*auth.Manager); ok {
		return issuer
	}
	return auth.New(auth.HarnessSecret(cfg.JWTSecret, cfg.HarnessJWTSecret))
}

// Onboarding is the device registration and ingress service; the process also
// uses it to check standard MQTT reports, so both share one rate budget.
func (s *Server) Onboarding() *onboarding.Service { return s.onboarding }

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
		s.fail(w, r, err, "无法签发登录凭据")
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
