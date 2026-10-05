package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/auth"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
)

func TestAIProviderURLFormat(t *testing.T) {
	tests := []struct {
		name, target string
		want         bool
	}{
		{name: "official API", target: "https://api.deepseek.com/v1", want: true},
		{name: "default HTTPS port", target: "https://api.deepseek.com:443", want: true},
		{name: "private vLLM port", target: "http://localhost:8000/v1", want: true},
		{name: "custom local port", target: "http://localhost:8080", want: true},
		{name: "remote LAN model", target: "http://192.168.10.20:9000/v1", want: true},
		{name: "custom cloud model", target: "https://models.example.com/v1", want: true},
		{name: "IPv6 model", target: "http://[::1]:8000", want: true},
		{name: "userinfo rejected", target: "https://token@api.deepseek.com", want: false},
		{name: "query rejected", target: "https://models.example.com?key=secret", want: false},
		{name: "fragment rejected", target: "https://models.example.com/#v1", want: false},
		{name: "markdown rejected", target: "[http://vllm:8000/v1](http://vllm:8000/v1)", want: false},
		{name: "unsupported scheme", target: "file:///tmp/provider", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validateAIProviderURL(tt.target) == nil; got != tt.want {
				t.Fatalf("valid provider URL(%q)=%v want=%v", tt.target, got, tt.want)
			}
		})
	}
}

func TestBuiltinAdminCannotMintTokenForUnconfiguredTenant(t *testing.T) {
	cfg := config.Config{AdminUser: "admin", AdminPassword: "admin123", JWTSecret: "test-secret-at-least-32-characters"}
	s := &Server{cfg: cfg, auth: auth.New(cfg.JWTSecret), engine: &core.Engine{}}
	body, err := json.Marshal(map[string]string{"username": "admin", "password": "admin123", "tenantId": "tenant-attacker"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	s.login(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("unexpected status for unconfigured admin tenant: got=%d body=%s", resp.Code, resp.Body.String())
	}
	if bytes.Contains(resp.Body.Bytes(), []byte(`"accessToken"`)) {
		t.Fatal("unconfigured admin tenant response contains an access token")
	}
}

func TestBuiltinAdminCanUseConfiguredTenant(t *testing.T) {
	cfg := config.Config{AdminUser: "admin", AdminPassword: "admin123", AdminTenants: []string{"tenant-allowed"}, JWTSecret: "test-secret-at-least-32-characters"}
	s := &Server{cfg: cfg, auth: auth.New(cfg.JWTSecret), engine: &core.Engine{}}
	body, err := json.Marshal(map[string]string{"username": "admin", "password": "admin123", "tenantId": "tenant-allowed"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	s.login(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("configured admin tenant was rejected: status=%d body=%s", resp.Code, resp.Body.String())
	}
	var result struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	claims, err := s.auth.Parse(result.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if claims.TenantID != "tenant-allowed" || claims.Role != "admin" {
		t.Fatalf("unexpected configured admin claims: tenant=%q role=%q", claims.TenantID, claims.Role)
	}
}

// Repeated wrong passwords lock the account for a while; a success before the
// limit clears the count, and the lock ends on its own.
func TestLoginLimiterLocksAccountAfterRepeatedFailures(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newLoginLimiter(nil)
	l.now = func() time.Time { return now }
	for i := 0; i < loginMaxFailures-1; i++ {
		l.record("tenant\x00admin", false)
	}
	l.record("tenant\x00admin", true)
	for i := 0; i < loginMaxFailures-1; i++ {
		l.record("tenant\x00admin", false)
	}
	if l.retryAfter("tenant\x00admin") != 0 {
		t.Fatal("a success must reset earlier failures")
	}
	l.record("tenant\x00admin", false)
	if l.retryAfter("tenant\x00admin") <= 0 {
		t.Fatal("the account must be locked after the limit")
	}
	if l.retryAfter("tenant\x00other") != 0 {
		t.Fatal("other accounts must not be affected")
	}
	now = now.Add(loginLockout + time.Second)
	if l.retryAfter("tenant\x00admin") != 0 {
		t.Fatal("the lock must expire")
	}
}

// Routine successful requests (page polling, Prometheus scrapes) must not be
// written at the default info level; failures still are.
func TestAccessLogKeepsRoutineRequestsOutOfInfoLogs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var out bytes.Buffer
	s := &Server{log: slog.New(slog.NewJSONHandler(&out, &slog.HandlerOptions{Level: slog.LevelInfo}))}
	router := gin.New()
	router.Use(s.accessLog())
	router.GET("/metrics", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	router.GET("/missing", func(c *gin.Context) { c.Status(http.StatusNotFound) })
	router.GET("/broken", func(c *gin.Context) { c.Status(http.StatusBadGateway) })

	for _, path := range []string{"/metrics", "/metrics", "/missing", "/broken"} {
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}
	logged := out.String()
	if strings.Contains(logged, `"path":"/metrics"`) {
		t.Fatalf("successful request logged at info level: %s", logged)
	}
	if !strings.Contains(logged, `"level":"INFO","msg":"http request","method":"GET","path":"/missing"`) {
		t.Fatalf("client error not logged at info: %s", logged)
	}
	if !strings.Contains(logged, `"level":"WARN","msg":"http request","method":"GET","path":"/broken"`) {
		t.Fatalf("server error not logged at warn: %s", logged)
	}
}

func TestParseListPaginationSupportsPageAndLegacyOffset(t *testing.T) {
	tests := []struct {
		name                string
		query               string
		page, pageSize, off int
	}{
		{name: "page contract", query: "page=3&pageSize=50", page: 3, pageSize: 50, off: 100},
		{name: "legacy contract", query: "limit=7&offset=14", page: 3, pageSize: 7, off: 14},
		{name: "server cap", query: "page=2&pageSize=1000", page: 2, pageSize: maxPageSize, off: maxPageSize},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/api/v1/products?"+tt.query, nil)
			got := parseListPagination(r)
			if got.Page != tt.page || got.PageSize != tt.pageSize || got.Offset != tt.off {
				t.Fatalf("pagination=%+v, want page=%d pageSize=%d offset=%d", got, tt.page, tt.pageSize, tt.off)
			}
		})
	}
}

func TestMemoryProductPaginationReturnsPageAndTotal(t *testing.T) {
	repo := memory.NewRepository()
	for index := 0; index < 3; index++ {
		if err := repo.SaveProduct(context.Background(), model.Product{TenantID: "tenant_001", ID: "product_" + string(rune('a'+index)), UpdatedAt: int64(index + 1)}); err != nil {
			t.Fatal(err)
		}
	}
	items, total, err := repo.ListProductsPage(context.Background(), "tenant_001", 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(items) != 2 {
		t.Fatalf("page len=%d total=%d, want len=2 total=3", len(items), total)
	}
}

func TestMemoryPropertyHistoryPaginationKeepsLatestPageChronological(t *testing.T) {
	repo := memory.NewRepository()
	for index := 1; index <= 3; index++ {
		if err := repo.SaveStandardMessage(context.Background(), model.StandardMessage{
			TenantID: "tenant_001", DeviceID: "device_001", MessageID: "message_" + string(rune('0'+index)), Timestamp: int64(index),
			Properties: map[string]any{"temperature": index},
		}); err != nil {
			t.Fatal(err)
		}
	}
	items, total, err := repo.PropertyHistoryPage(context.Background(), "tenant_001", "device_001", "temperature", 0, 0, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(items) != 2 || items[0]["timestamp"] != int64(2) || items[1]["timestamp"] != int64(3) {
		t.Fatalf("history page=%#v total=%d, want timestamps 2,3 and total 3", items, total)
	}
}

func TestOversizedPaginationThroughHTTP(t *testing.T) {
	repo := memory.NewRepository()
	if err := repo.SaveProduct(context.Background(), model.Product{ID: "first-product", TenantID: "tenant-a"}); err != nil {
		t.Fatal(err)
	}
	api := New(config.Config{JWTSecret: "audit-only-secret-at-least-32-characters"}, &core.Engine{Repo: repo}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	token, err := api.auth.Issue("audit", "tenant-a", "viewer", nil, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/ai/providers", "/api/v1/products"} {
		for _, query := range []string{"page=1&pageSize=20", "page=9223372036854775807&pageSize=20", "page=999999999999999999999999999999&pageSize=100", "offset=9223372036854775807&limit=1"} {
			r := httptest.NewRequest(http.MethodGet, path+"?"+query, nil)
			r.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			api.Handler().ServeHTTP(w, r)
			if w.Code != http.StatusOK {
				t.Fatalf("%s?%s: status=%d", path, query, w.Code)
			}
			var result struct {
				Items []json.RawMessage `json:"items"`
				Total int               `json:"total"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if query != "page=1&pageSize=20" && len(result.Items) != 0 {
				t.Errorf("%s?%s: oversized page returned first-page data", path, query)
			}
			if path == "/api/v1/products" && (result.Total != 1 || (query == "page=1&pageSize=20" && len(result.Items) != 1)) {
				t.Errorf("product pagination lost total or normal page: %+v", result)
			}
		}
	}
}

func TestPaginationArithmeticStaysInRange(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, size := range []int{1, 20, 100} {
		for _, param := range []string{"page", "offset"} {
			r := httptest.NewRequest(http.MethodGet, "/?"+param+"="+strconv.Itoa(maxInt)+"&pageSize="+strconv.Itoa(size), nil)
			p := parseListPagination(r)
			if p.Page < 1 || p.Offset < 0 || p.Offset > maxInt-p.PageSize {
				t.Fatalf("unsafe pagination: %+v", p)
			}
		}
	}
}

func TestPageItemsHandlesInvalidAndOverflowingBounds(t *testing.T) {
	for _, p := range []listPagination{{Offset: -1, PageSize: 20}, {Offset: 0, PageSize: -1}, {Offset: 1, PageSize: int(^uint(0) >> 1)}} {
		items, total := pageItems([]int{1, 2, 3}, p)
		if total != 3 {
			t.Fatalf("total=%d", total)
		}
		if p.Offset < 0 || p.PageSize < 0 {
			if len(items) != 0 {
				t.Fatalf("invalid bounds returned data: %v", items)
			}
		} else if len(items) != 2 || items[0] != 2 {
			t.Fatalf("overflowing size lost remaining items: %v", items)
		}
	}
}

func TestSplitGatewayHTTPFlow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bus := local.NewBus()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	// Separate engines, shared test repository and queue. Only the API consumes Raw.
	gatewayEngine := core.New(ScopedRepository(repo), archive, bus, local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), log)
	apiEngine := core.New(ScopedRepository(repo), archive, bus, local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), log)
	if err = apiEngine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.JWTSecret = "split-gateway-test-signing-secret"
	cfg.ProcessRole = "gateway"
	gateway := New(cfg, gatewayEngine, metrics.New(), log)
	upstream := httptest.NewServer(gateway.Handler())
	defer upstream.Close()
	cfg.ProcessRole, cfg.AccessGatewayURL = "api", upstream.URL
	api := New(cfg, apiEngine, metrics.New(), log)
	token, err := api.auth.Issue("tester", "tenant", "admin", nil, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	call := func(handler http.Handler, method, path string, body any, auth string, credential model.DeviceCredential) *httptest.ResponseRecorder {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		if auth != "" {
			r.Header.Set("Authorization", "Bearer "+auth)
		}
		r.Header.Set("X-Device-Key", credential.AccessKey)
		r.Header.Set("X-Device-Secret", credential.Secret)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	payload := json.RawMessage(`{"id":"sample","timestamp":1788850000000,"data":{"temperature":42}}`)
	q := onboarding.EnrollRequest{RequestID: "req-split", NewProduct: &onboarding.NewProduct{ID: "product", Name: "产品", ProtocolPackageID: onboarding.StandardPackageID, Transport: "HTTP"}, Device: onboarding.EnrollDevice{ID: "device", Name: "设备"}, Connection: onboarding.EnrollConnection{Mode: onboarding.ModeStandard}}
	if r := call(gateway.Handler(), "POST", "/api/v1/auth/login", nil, "", model.DeviceCredential{}); r.Code != 404 {
		t.Fatal("gateway exposed login", r.Code)
	}
	if r := call(gateway.Handler(), "GET", "/metrics", nil, "", model.DeviceCredential{}); r.Code != 200 || !strings.Contains(r.Body.String(), "raw_archive_success_total") {
		t.Fatal("gateway metrics are not scrapeable per instance", r.Code)
	}
	if r := call(api.Handler(), "POST", "/api/v1/onboarding", q, "", model.DeviceCredential{}); r.Code != 401 {
		t.Fatal("forward bypassed auth", r.Code)
	}
	if r := call(api.Handler(), "GET", "/api/v1/onboarding/preflight?protocolPackageId="+onboarding.StandardPackageID, nil, token, model.DeviceCredential{}); r.Code != 200 {
		t.Fatal("preflight forwarding", r.Code, r.Body.String())
	}
	saved := call(api.Handler(), "POST", "/api/v1/onboarding", q, token, model.DeviceCredential{})
	var result onboarding.EnrollResult
	if saved.Code != 201 || json.Unmarshal(saved.Body.Bytes(), &result) != nil {
		t.Fatal("save", saved.Code, saved.Body.String())
	}
	ingest := call(api.Handler(), "POST", "/api/v1/device-ingest/standard/tenant/product/device/property", payload, "", result.Credential)
	if ingest.Code != 202 {
		t.Fatal("ingest", ingest.Code, ingest.Body.String())
	}
	var accepted struct {
		MessageID string `json:"messageId"`
	}
	_ = json.Unmarshal(ingest.Body.Bytes(), &accepted)
	deadline := time.Now().Add(3 * time.Second)
	for {
		msg, err := repo.GetStandardMessageByRaw(ctx, "tenant", accepted.MessageID)
		if err == nil {
			if msg.Properties["temperature"] != float64(42) {
				t.Fatal("decoded wrong value")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("API consumer did not parse gateway raw", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if r := call(api.Handler(), "GET", "/api/v1/device-registry/device/connection", nil, token, model.DeviceCredential{}); r.Code != 200 {
		t.Fatal("connection forwarding", r.Code)
	}
	upstream.Close()
	if r := call(api.Handler(), "GET", "/api/v1/onboarding/preflight?productId=product", nil, token, model.DeviceCredential{}); r.Code != 503 {
		t.Fatal("unavailable gateway falsely succeeded", r.Code)
	}
}

func TestExecutionRouteUsesTenantLeaseAndPreservesAuth(t *testing.T) {
	repo := memory.NewRepository()
	ctx := context.Background()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), log)
	cfg := config.Load()
	cfg.ProcessRole = "gateway"
	cfg.AccessCoordination = true
	cfg.AccessNodeURL = "http://local"
	cfg.JWTSecret = "routing-shared-test-secret"
	server := New(cfg, engine, metrics.New(), log)
	token, err := server.auth.Issue("operator", "tenant", "operator", nil, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var received bool
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("X-Iot-Gateway-Hops") != "1" {
			t.Error("forward lost auth or routing bound")
		}
		received = true
		w.WriteHeader(202)
	}))
	defer remote.Close()
	if _, ok, err := repo.AcquireExecutionLease(ctx, "tenant", "profile/profile", "remote", remote.URL, time.Minute); err != nil || !ok {
		t.Fatal(err)
	}
	if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant", ID: "device", ConnectorProfileID: "profile"}); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/v1/device-registry/device/commands", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, r)
	if w.Code != 202 || !received {
		t.Fatal("did not reach owning runtime", w.Code)
	}
	other, err := server.auth.Issue("operator", "other-tenant", "operator", nil, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+other)
	received = false
	if target := server.executionTarget(r); target != "" {
		t.Fatal("cross tenant routed", target)
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("X-Iot-Gateway-Hops", "2")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, r)
	if w.Code != 503 || received {
		t.Fatal("route loop not bounded", w.Code)
	}
}

func TestWorkerRolesServeOnlyHealthAndMetrics(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, role := range []string{config.RoleParser, config.RoleProcessor, config.RoleJobs} {
		engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), log)
		cfg := config.Load()
		cfg.ProcessRole, cfg.InstanceID = role, role+"-1"
		registry := metrics.New()
		registry.SetProcessInfo(role, cfg.InstanceID)
		server := New(cfg, engine, registry, log)
		call := func(path string) *httptest.ResponseRecorder {
			w := httptest.NewRecorder()
			server.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			return w
		}
		if w := call("/api/v1/devices"); w.Code != 404 {
			t.Fatal(role, "worker served a business route", w.Code)
		}
		if w := call("/metrics"); w.Code != 200 || !strings.Contains(w.Body.String(), `process_info{role="`+role+`",instance="`+role+`-1"} 1`) {
			t.Fatal(role, "worker metrics not attributable", w.Code)
		}
		w := call("/health/ready")
		var ready struct {
			Role   string            `json:"role"`
			Checks map[string]string `json:"checks"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &ready)
		if ready.Role != role {
			t.Fatal(role, "readiness does not name the role", w.Body.String())
		}
		if _, ok := ready.Checks["knowledge"]; ok {
			t.Fatal(role, "readiness checks an unused dependency")
		}
	}
}

func TestVideoRoutesFollowTheControlOwner(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), log)
	server := New(config.Load(), engine, metrics.New(), log)
	var forwarded atomic.Int32
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Iot-Gateway-Hops") != "1" || r.Header.Get("Authorization") != "Bearer user-token" {
			t.Error("forward lost auth or hop bound")
		}
		forwarded.Add(1)
		w.WriteHeader(201)
	}))
	defer owner.Close()
	local, endpoint := false, owner.URL
	server.SetVideoRouting(func() (bool, string) { return local, endpoint })
	call := func(path string) int {
		r := httptest.NewRequest("POST", path, nil)
		r.Header.Set("Authorization", "Bearer user-token")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, r)
		return w.Code
	}
	if code := call("/api/v1/video/cameras/c1/play-sessions"); code != 201 || forwarded.Load() != 1 {
		t.Fatal("live route not forwarded to the owner", code)
	}
	if code := call("/api/v1/video/hooks/on_play"); code != 201 || forwarded.Load() != 2 {
		t.Fatal("media hook not forwarded", code)
	}
	if code := call("/api/v1/devices"); forwarded.Load() != 2 || code == 201 {
		t.Fatal("non-video route forwarded")
	}
	endpoint = ""
	if code := call("/api/v1/video/status"); code != 503 {
		t.Fatal("no owner must be reported as unavailable", code)
	}
	local = true
	if code := call("/api/v1/video/status"); code == 503 || forwarded.Load() != 2 {
		t.Fatal("owner did not serve its own live routes", code)
	}
}
