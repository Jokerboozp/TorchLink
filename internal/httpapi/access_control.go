package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"errors"   /* 执行当前语句并推进处理流程。 */
	"net/http" /* 执行当前语句并推进处理流程。 */
	"regexp"   /* 执行当前语句并推进处理流程。 */
	"sort"     /* 执行当前语句并推进处理流程。 */
	"strings"  /* 执行当前语句并推进处理流程。 */
	"time"     /* 执行当前语句并推进处理流程。 */

	"golang.org/x/crypto/bcrypt"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/auth"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type permissionItem struct { /* 定义 permissionItem 类型。 */
	ID   string `json:"id"`   /* 执行当前语句并推进处理流程。 */
	Name string `json:"name"` /* 执行当前语句并推进处理流程。 */
	Menu string `json:"menu"` /* 执行当前语句并推进处理流程。 */
	Kind string `json:"kind"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

var menuNames = map[string]string{"dashboard": "运行总览", "protocols": "设备通信协议", "products": "设备模板", "devices": "设备管理", "profiles": "平台接入点", "integration": "模拟设备测试", "cameras": "摄像头映射", "alarms": "告警中心", "inspection": "智能巡检", "raw": "原始报文", "rules": "告警规则", "knowledge": "知识库", "aiProviders": "模型管理", "ai": "智能助手", "backups": "备份中心", "access": "用户与权限"} /* 声明 menuNames。 */

// Route permissions use the router's canonical pattern, never a caller-supplied URL.
func routeMenu(path string) string { /* 定义 routeMenu 函数。 */
	if menu, ok := opsRouteMenu(path); ok {
		return menu
	}
	if strings.Contains(path, "/knowledge-binding") { /* 判断条件并选择处理分支。 */
		return "knowledge" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if strings.Contains(path, "/products/") && strings.Contains(path, "/protocol-binding") { /* 判断条件并选择处理分支。 */
		return "products" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if strings.HasSuffix(path, "/debug") { /* 判断条件并选择处理分支。 */
		return "integration" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, v := range [][2]string{{"/access/", "access"}, {"/health-inspection", "inspection"}, {"/protocol-assistant", "protocols"}, {"/protocol", "protocols"}, {"/modbus-tcp", "protocols"}, {"/device-access-profiles", "profiles"}, {"/onboarding", "devices"}, {"/test-devices", "integration"}, {"/providers", "aiProviders"}, {"/alarm-analysis", "alarms"}, {"/ai/rule-draft", "rules"}, {"/ai/", "ai"}, {"/knowledge/", "knowledge"}, {"/products", "products"}, {"/device-registry", "devices"}, {"/devices", "devices"}, {"/device-states", "devices"}, {"/discovered-devices", "devices"}, {"/connectors", "profiles"}, {"/integrations/video", "cameras"}, {"/raw-messages", "raw"}, {"/replays", "raw"}, {"/alarms", "alarms"}, {"/rules", "rules"}, {"/backups", "backups"}, {"/dashboard", "dashboard"}} { /* 循环处理当前数据。 */
		if strings.Contains(path, v[0]) { /* 判断条件并选择处理分支。 */
			return v[1] /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return "" /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func routeAction(method, path string) string { /* 定义 routeAction 函数。 */
	if name, ok := opsActionName(method, path); ok {
		return name
	}
	if method == "POST" && strings.HasSuffix(path, "/device-registry/:id/children") {
		return "登记子设备"
	}
	if strings.Contains(path, "/password") { /* 判断条件并选择处理分支。 */
		return "重置用户密码" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if strings.Contains(path, "/access/") { /* 判断条件并选择处理分支。 */
		resource := "角色"                      /* 更新 resource 的值。 */
		if strings.Contains(path, "/users") { /* 判断条件并选择处理分支。 */
			resource = "用户" /* 更新 resource 的值。 */
		} /* 结束当前表达式或代码块。 */
		switch method { /* 根据条件选择处理路径。 */
		case "POST": /* 处理当前分支。 */
			return "添加" + resource /* 返回当前处理结果。 */
		case "PUT": /* 处理当前分支。 */
			return "编辑" + resource /* 返回当前处理结果。 */
		case "DELETE": /* 处理当前分支。 */
			return "删除" + resource /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if strings.Contains(path, "/knowledge-binding") { /* 判断条件并选择处理分支。 */
		return "配置知识检索策略" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if strings.HasSuffix(path, "/workflows/admin") { /* 判断条件并选择处理分支。 */
		return "查看智能体配置" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if strings.Contains(path, "/workflows") { /* 判断条件并选择处理分支。 */
		switch method { /* 根据条件选择处理路径。 */
		case "POST": /* 处理当前分支。 */
			return "新建智能体" /* 返回当前处理结果。 */
		case "PUT": /* 处理当前分支。 */
			return "编辑 / 启停智能体" /* 返回当前处理结果。 */
		case "DELETE": /* 处理当前分支。 */
			return "删除智能体" /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if strings.HasSuffix(path, "/protocol-assistant/publish") { /* 判断条件并选择处理分支。 */
		return "保存生成的协议" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if strings.HasSuffix(path, "/alarm-analysis") { /* 判断条件并选择处理分支。 */
		return "执行告警研判" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if strings.HasSuffix(path, "/health-inspection") { /* 判断条件并选择处理分支。 */
		return "同步执行巡检" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if strings.HasSuffix(path, "/chat/stream") { /* 判断条件并选择处理分支。 */
		return "流式问答" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if method == "DELETE" && strings.Contains(path, "/protocols/:id/releases/:version") {
		return "删除协议版本"
	}
	for _, v := range [][2]string{{"/credentials", "管理设备凭据"}, {"/commands", "设备控制"}, {"/source-releases", "上传源码"}, {"/package-releases", "上传制品"}, {"/source", "下载源码"}, {"/package", "下载制品"}, {"/publish", "发布"}, {"/preview", "解析测试"}, {"/generate", "生成协议"}, {"/rollback", "回滚"}, {"/protocol-binding", "绑定协议"}, {"/restore-drill", "校验备份"}, {"/files/", "下载备份"}, {"/download", "下载报文"}, {"/replay", "回放报文"}, {"/debug", "发送测试报文"}, {"/test", "连接测试"}, {"/actions", "处置告警"}, {"/pdf", "下载报告"}, {"/run", "执行巡检"}, {"/chat", "发送提问"}, {"/password", "重置密码"}, {"/provision", "准备测试设备"}} { /* 循环处理当前数据。 */
		if strings.Contains(path, v[0]) { /* 判断条件并选择处理分支。 */
			return v[1] /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	switch method { /* 根据条件选择处理路径。 */
	case "POST": /* 处理当前分支。 */
		return "新增 / 执行" + menuNames[routeMenu(path)] /* 返回当前处理结果。 */
	case "PUT", "PATCH": /* 处理当前分支。 */
		return "编辑" + menuNames[routeMenu(path)] /* 返回当前处理结果。 */
	case "DELETE": /* 处理当前分支。 */
		return "删除" + menuNames[routeMenu(path)] /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return "查看" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func protectedRead(path string) bool { /* 定义 protectedRead 函数。 */
	return strings.HasSuffix(path, "/source") || strings.HasSuffix(path, "/package") || strings.Contains(path, "/files/") || strings.HasSuffix(path, "/download") || strings.HasSuffix(path, "/workflows/admin") || path == "/api/v1/ops/datasources/:uid" /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) permissionCatalog() []permissionItem { /* 定义 permissionCatalog 函数。 */
	items := []permissionItem{}       /* 更新 items 的值。 */
	for id, name := range menuNames { /* 循环处理当前数据。 */
		items = append(items, permissionItem{"menu:" + id, name, id, "menu"}) /* 更新 items 的值。 */
	} /* 结束当前表达式或代码块。 */
	for _, r := range s.router.Routes() { /* 循环处理当前数据。 */
		menu := routeMenu(r.Path) /* 更新 menu 的值。 */
		// The add-device wizard is covered by the ordinary add-device permission.
		if menu == "" || strings.Contains(r.Path, "/device-ingest") || strings.HasPrefix(r.Path, "/api/v1/onboarding") || r.Path == "/api/v1/integrations/video/alarm" {
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if r.Method == "GET" && !protectedRead(r.Path) { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		items = append(items, permissionItem{r.Method + " " + r.Path, routeAction(r.Method, r.Path), menu, "action"}) /* 更新 items 的值。 */
	} /* 结束当前表达式或代码块。 */
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID }) /* 执行当前语句并推进处理流程。 */
	return items                                                                /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func effectivePermissions(state model.AccessState, user model.PlatformUser) map[string]bool { /* 定义 effectivePermissions 函数。 */
	user = resolveUserDeviceScope(state, user)
	p := map[string]bool{}               /* 更新 p 的值。 */
	for _, v := range user.Permissions { /* 循环处理当前数据。 */
		p[v] = true /* 更新 p[v] 的值。 */
	} /* 结束当前表达式或代码块。 */
	for _, role := range state.Roles { /* 循环处理当前数据。 */
		for _, id := range user.RoleIDs { /* 循环处理当前数据。 */
			if role.ID == id { /* 判断条件并选择处理分支。 */
				for _, v := range role.Permissions { /* 循环处理当前数据。 */
					p[v] = true /* 更新 p[v] 的值。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	// Device data must not become visible through a related menu alone.
	if !p["menu:devices"] || user.DeviceScope == "none" || user.DeviceScope == "" { /* 判断条件并选择处理分支。 */
		delete(p, "menu:alarms") /* 执行当前语句并推进处理流程。 */
		delete(p, "menu:raw")    /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if !p["menu:devices"] || user.DeviceScope != "all" { /* 判断条件并选择处理分支。 */
		// These services produce tenant-wide artifacts or launch tenant-wide jobs.
		for _, menu := range []string{"inspection", "backups", "profiles", "integration", "rules", "cameras", "access"} { /* 循环处理当前数据。 */
			delete(p, "menu:"+menu) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		for _, action := range []string{"POST /api/v1/device-registry", "POST /api/v1/device-registry/:id/children", "POST /api/v1/device-states", "POST /api/v1/raw-messages", "POST /api/v1/raw-messages/replay"} { /* 循环处理当前数据。 */
			delete(p, action) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if !p["menu:devices"] || user.DeviceScope != "all" {
		for _, action := range []string{"POST /api/v1/ai/reports", "GET /api/v1/ai/workflows/admin", "POST /api/v1/ai/workflows", "PUT /api/v1/ai/workflows/:id", "DELETE /api/v1/ai/workflows/:id"} {
			delete(p, action)
		}
	}
	for id := range p { /* 循环处理当前数据。 */
		if parts := strings.SplitN(id, " ", 2); len(parts) == 2 && !p["menu:"+routeMenu(parts[1])] { /* 判断条件并选择处理分支。 */
			delete(p, id) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return p /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
type permissionsKey struct{}

// requestAllows reports whether a managed user may also use another route as
// part of the current request, such as creating a template while adding a
// device. Built-in role tokens were checked by the route's own role.
func requestAllows(r *http.Request, method, path string) bool {
	p, ok := r.Context().Value(permissionsKey{}).(map[string]bool)
	return !ok || allowsRoute(p, method, path)
}

func permissionList(p map[string]bool) []string { /* 定义 permissionList 函数。 */
	out := []string{}       /* 更新 out 的值。 */
	for id, ok := range p { /* 循环处理当前数据。 */
		if ok { /* 判断条件并选择处理分支。 */
			out = append(out, id) /* 更新 out 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	sort.Strings(out) /* 执行当前语句并推进处理流程。 */
	return out        /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func allowsRoute(p map[string]bool, method, path string) bool { /* 定义 allowsRoute 函数。 */
	if path == "/api/v1/auth/me" { /* 判断条件并选择处理分支。 */
		return true /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	// Broker token is restricted separately. Managed users cannot access generic MCP.
	if path == "/api/v1/events" { /* 判断条件并选择处理分支。 */
		return p["menu:devices"] || p["menu:alarms"] || p["menu:dashboard"] || p["menu:raw"] /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if path == "/api/v1/mqtt/token" || path == "/api/v1/mqtt/load-token" { /* 判断条件并选择处理分支。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if method == "POST" && path == "/api/v1/onboarding" {
		path = "/api/v1/device-registry"
	}
	if opsSharedRoute(path) {
		return hasOpsMenu(p)
	}
	menu := routeMenu(path) /* 更新 menu 的值。 */
	if menu == "" {         /* 判断条件并选择处理分支。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if method != "GET" || protectedRead(path) { /* 判断条件并选择处理分支。 */
		return p[method+" "+path] && p["menu:"+menu] /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if p["menu:"+menu] { /* 判断条件并选择处理分支。 */
		return true /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	// Read-only lookups needed by related pages, without granting mutation rights.
	lookups := map[string][]string{ /* 更新 lookups 的值。 */
		"/api/v1/protocol-packages": {"products"},                                                      /* 执行当前语句并推进处理流程。 */
		"/api/v1/alarms":            {"dashboard", "integration"}, "/api/v1/alarms/:id": {"dashboard"}, /* 执行当前语句并推进处理流程。 */
		"/api/v1/device-registry/:id/connection": {"integration"},                                                                                                                   /* 执行当前语句并推进处理流程。 */
		"/api/v1/device-registry/:id/history":    {"integration"}, "/api/v1/device-registry/:id/children": {"integration"}, "/api/v1/device-registry/:id/commands": {"integration"}, /* 执行当前语句并推进处理流程。 */
		"/api/v1/raw-messages/:id": {"integration", "devices"}, /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	for _, dep := range lookups[path] { /* 循环处理当前数据。 */
		if p["menu:"+dep] { /* 判断条件并选择处理分支。 */
			return true /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	deps := map[string][]string{"/api/v1/products": {"devices", "profiles", "protocols", "integration", "cameras", "rules", "knowledge"}, "/api/v1/device-registry": {"profiles", "integration", "cameras", "alarms", "inspection"}, "/api/v2/protocols": {"products", "devices", "profiles", "integration"}, "/api/v1/connectors": {"devices", "integration"}, "/api/v1/connectors/types": {"products", "integration"}, "/api/v1/ai/workflows": {"knowledge"}, "/api/v1/ai/providers": {"ai"}} /* 更新 deps 的值。 */
	for _, dep := range deps[path] {                                                                                                                                                                                                                                                                                                                                                                                                                                                            /* 循环处理当前数据。 */
		if p["menu:"+dep] { /* 判断条件并选择处理分支。 */
			return true /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return false /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) accessStore() (ports.AccessStore, error) { /* 定义 accessStore 函数。 */
	v, ok := s.engine.Repo.(ports.AccessStore) /* 更新 ok 的值。 */
	if !ok {                                   /* 判断条件并选择处理分支。 */
		return nil, errors.New("access storage unavailable") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return v, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) managedIdentity(r *http.Request, c auth.Claims) (model.PlatformUser, map[string]bool, error) { /* 定义 managedIdentity 函数。 */
	store, err := s.accessStore() /* 更新 err 的值。 */
	if err != nil {               /* 判断条件并选择处理分支。 */
		return model.PlatformUser{}, nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	state, err := store.LoadAccessState(r.Context(), c.TenantID) /* 更新 err 的值。 */
	if err != nil {                                              /* 判断条件并选择处理分支。 */
		return model.PlatformUser{}, nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, u := range state.Users { /* 循环处理当前数据。 */
		if u.Username == c.Username && u.Enabled && u.SessionVersion == c.SessionVersion { /* 判断条件并选择处理分支。 */
			permissions := effectivePermissions(state, u)
			s.stripOpsPermissions(c.TenantID, permissions)
			return resolveUserDeviceScope(state, u), permissions, nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return model.PlatformUser{}, nil, errors.New("account disabled or session revoked") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) accessRoutes() { /* 定义 accessRoutes 函数。 */
	s.router.GET("/api/v1/events", s.authorize("viewer"), s.endpoint(s.userEvents))                                            /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/access/device-options", s.authorize("admin"), s.endpoint(s.accessDeviceOptions))                     /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/auth/me", s.authorize("viewer"), s.endpoint(s.currentIdentity))                                      /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/access/permissions", s.authorize("admin"), s.endpoint(func(w http.ResponseWriter, r *http.Request) { /* 执行当前语句并推进处理流程。 */
		write(w, 200, map[string]any{"items": s.permissionCatalogFor(claims(r).TenantID)}) /* 执行当前语句并推进处理流程。 */
	})) /* 结束当前表达式或代码块。 */
	s.router.GET("/api/v1/access/users", s.authorize("admin"), s.endpoint(s.accessList))                              /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/access/roles", s.authorize("admin"), s.endpoint(s.accessList))                              /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/access/users", s.authorize("admin"), s.endpoint(s.accessSaveUser))                         /* 执行当前语句并推进处理流程。 */
	s.router.PUT("/api/v1/access/users/:id", s.authorize("admin"), s.endpoint(s.accessSaveUser, "id"))                /* 执行当前语句并推进处理流程。 */
	s.router.DELETE("/api/v1/access/users/:id", s.authorize("admin"), s.endpoint(s.accessDeleteUser, "id"))           /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/access/users/:id/password", s.authorize("admin"), s.endpoint(s.accessResetPassword, "id")) /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/access/roles", s.authorize("admin"), s.endpoint(s.accessSaveRole))                         /* 执行当前语句并推进处理流程。 */
	s.router.PUT("/api/v1/access/roles/:id", s.authorize("admin"), s.endpoint(s.accessSaveRole, "id"))                /* 执行当前语句并推进处理流程。 */
	s.router.DELETE("/api/v1/access/roles/:id", s.authorize("admin"), s.endpoint(s.accessDeleteRole, "id"))           /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) currentIdentity(w http.ResponseWriter, r *http.Request) { /* 定义 currentIdentity 函数。 */
	c := claims(r)            /* 更新 c 的值。 */
	perms := []string{"*"}    /* 更新 perms 的值。 */
	name := c.Username        /* 更新 name 的值。 */
	if c.TokenUse == "user" { /* 判断条件并选择处理分支。 */
		u, p, err := s.managedIdentity(r, c) /* 更新 err 的值。 */
		if err != nil {                      /* 判断条件并选择处理分支。 */
			problem(w, 401, err.Error()) /* 执行当前语句并推进处理流程。 */
			return                       /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		perms = permissionList(p) /* 更新 perms 的值。 */
		name = u.DisplayName      /* 更新 name 的值。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 200, map[string]any{"username": c.Username, "displayName": name, "tenantId": c.TenantID, "role": c.Role, "permissions": perms, "accessVersion": requestAccessVersion(r.Context(), c)}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) canConfigureAI(r *http.Request) bool { /* 定义 canConfigureAI 函数。 */
	c := claims(r)            /* 更新 c 的值。 */
	if c.TokenUse != "user" { /* 判断条件并选择处理分支。 */
		return c.Role == "admin" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	_, p, err := s.managedIdentity(r, c)                                                                                        /* 更新 err 的值。 */
	return err == nil && p["menu:aiProviders"] && (p["PUT /api/v1/ai/providers/config"] || p["POST /api/v1/ai/providers/test"]) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) accessState(w http.ResponseWriter, r *http.Request) (ports.AccessStore, model.AccessState, bool) { /* 定义 accessState 函数。 */
	store, err := s.accessStore() /* 更新 err 的值。 */
	if err != nil {               /* 判断条件并选择处理分支。 */
		problem(w, 503, err.Error())           /* 执行当前语句并推进处理流程。 */
		return nil, model.AccessState{}, false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	state, err := store.LoadAccessState(r.Context(), claims(r).TenantID) /* 更新 err 的值。 */
	if err != nil {                                                      /* 判断条件并选择处理分支。 */
		problem(w, 500, "读取用户权限失败") /* 执行当前语句并推进处理流程。 */
		return nil, state, false    /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return store, state, true /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) commitAccess(w http.ResponseWriter, r *http.Request, store ports.AccessStore, state model.AccessState) { /* 定义 commitAccess 函数。 */
	ok, err := store.SaveAccessState(r.Context(), claims(r).TenantID, state) /* 更新 err 的值。 */
	if err != nil {                                                          /* 判断条件并选择处理分支。 */
		problem(w, 500, "保存用户权限失败") /* 执行当前语句并推进处理流程。 */
		return                      /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !ok { /* 判断条件并选择处理分支。 */
		problem(w, 409, "配置已被其他操作更新，请刷新重试") /* 执行当前语句并推进处理流程。 */
		return                              /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 200, map[string]bool{"success": true}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) accessList(w http.ResponseWriter, r *http.Request) { /* 定义 accessList 函数。 */
	_, state, ok := s.accessState(w, r) /* 更新 ok 的值。 */
	if !ok {                            /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	// Stored roles may still name routes that were removed or merged.
	known := s.knownPermissionsFor(claims(r).TenantID)
	for i := range state.Roles {
		state.Roles[i].Permissions = keepKnown(state.Roles[i].Permissions, known)
	}
	for i := range state.Users {
		state.Users[i].Permissions = keepKnown(state.Users[i].Permissions, known)
	}
	if strings.HasSuffix(r.URL.Path, "/roles") { /* 判断条件并选择处理分支。 */
		if state.Roles == nil { /* 判断条件并选择处理分支。 */
			state.Roles = []model.PlatformRole{} /* 更新 state.Roles 的值。 */
		} /* 结束当前表达式或代码块。 */
		write(w, 200, map[string]any{"items": state.Roles}) /* 执行当前语句并推进处理流程。 */
		return                                              /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for i := range state.Users { /* 循环处理当前数据。 */
		state.Users[i].PasswordHash = "" /* 更新 state.Users[i].PasswordHash 的值。 */
	} /* 结束当前表达式或代码块。 */
	if state.Users == nil { /* 判断条件并选择处理分支。 */
		state.Users = []model.PlatformUser{} /* 更新 state.Users 的值。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 200, map[string]any{"items": state.Users, "tenantId": claims(r).TenantID}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

var identityPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{2,63}$`) /* 声明 identityPattern。 */

func hashPassword(password string) (string, error) { /* 定义 hashPassword 函数。 */
	if len(password) < 10 || len(password) > 72 { /* 判断条件并选择处理分支。 */
		return "", errors.New("密码长度须为10至72字节") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	v, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost) /* 更新 err 的值。 */
	return string(v), err                                                       /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) knownPermissions() map[string]bool {
	known := map[string]bool{}
	for _, v := range s.permissionCatalog() {
		known[v.ID] = true
	}
	return known
}
func keepKnown(values []string, known map[string]bool) []string {
	out := []string{}
	for _, v := range values {
		if known[v] {
			out = append(out, v)
		}
	}
	return out
}
func (s *Server) validPermissions(values []string) bool { /* 定义 validPermissions 函数。 */
	known := map[string]bool{}                /* 更新 known 的值。 */
	for _, v := range s.permissionCatalog() { /* 循环处理当前数据。 */
		known[v.ID] = true /* 更新 known[v.ID] 的值。 */
	} /* 结束当前表达式或代码块。 */
	for _, v := range values { /* 循环处理当前数据。 */
		if !known[v] { /* 判断条件并选择处理分支。 */
			return false /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return true /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) accessSaveUser(w http.ResponseWriter, r *http.Request) { /* 定义 accessSaveUser 函数。 */
	var in struct { /* 声明 in。 */
		Username    string   `json:"username"`    /* 执行当前语句并推进处理流程。 */
		DisplayName string   `json:"displayName"` /* 执行当前语句并推进处理流程。 */
		Password    string   `json:"password"`    /* 执行当前语句并推进处理流程。 */
		Enabled     bool     `json:"enabled"`     /* 执行当前语句并推进处理流程。 */
		RoleIDs     []string `json:"roleIds"`     /* 执行当前语句并推进处理流程。 */
		Permissions []string `json:"permissions"` /* 执行当前语句并推进处理流程。 */
		DeviceScope string   `json:"deviceScope"` /* 执行当前语句并推进处理流程。 */
		DeviceIDs   []string `json:"deviceIds"`   /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if decode(w, r, &in) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if in.RoleIDs == nil { /* 判断条件并选择处理分支。 */
		in.RoleIDs = []string{} /* 更新 in.RoleIDs 的值。 */
	} /* 结束当前表达式或代码块。 */
	if in.Permissions == nil { /* 判断条件并选择处理分支。 */
		in.Permissions = []string{} /* 更新 in.Permissions 的值。 */
	} /* 结束当前表达式或代码块。 */
	if in.DeviceScope == "" { /* 判断条件并选择处理分支。 */
		in.DeviceScope = "none" /* 更新 in.DeviceScope 的值。 */
	} /* 结束当前表达式或代码块。 */
	if in.DeviceScope != "none" && in.DeviceScope != "selected" && in.DeviceScope != "all" && in.DeviceScope != "inherit" { /* 判断条件并选择处理分支。 */
		problem(w, 422, "设备范围无效") /* 执行当前语句并推进处理流程。 */
		return                    /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if in.DeviceIDs == nil || in.DeviceScope != "selected" { /* 判断条件并选择处理分支。 */
		in.DeviceIDs = []string{} /* 更新 in.DeviceIDs 的值。 */
	} /* 结束当前表达式或代码块。 */
	for _, id := range in.DeviceIDs { /* 循环处理当前数据。 */
		if _, err := s.unscopedRepo().GetManagedDevice(r.Context(), claims(r).TenantID, id); err != nil { /* 判断条件并选择处理分支。 */
			problem(w, 422, "授权设备不存在或不属于当前租户") /* 执行当前语句并推进处理流程。 */
			return                             /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if id := r.PathValue("id"); id != "" { /* 判断条件并选择处理分支。 */
		in.Username = id /* 更新 in.Username 的值。 */
	} /* 结束当前表达式或代码块。 */
	if !identityPattern.MatchString(in.Username) || in.Username == s.cfg.AdminUser { /* 判断条件并选择处理分支。 */
		problem(w, 422, "用户名须为3至64位字母、数字、点、横线或下划线，且不能使用内置管理员名称") /* 执行当前语句并推进处理流程。 */
		return                                                   /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !s.validPermissionsFor(claims(r).TenantID, in.Permissions) { /* 判断条件并选择处理分支。 */
		problem(w, 422, "存在无效权限") /* 执行当前语句并推进处理流程。 */
		return                    /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	store, state, ok := s.accessState(w, r) /* 更新 ok 的值。 */
	if !ok {                                /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, id := range in.RoleIDs { /* 循环处理当前数据。 */
		found := false                     /* 更新 found 的值。 */
		for _, role := range state.Roles { /* 循环处理当前数据。 */
			if role.ID == id { /* 判断条件并选择处理分支。 */
				found = true /* 更新 found 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if !found { /* 判断条件并选择处理分支。 */
			problem(w, 422, "角色不存在") /* 执行当前语句并推进处理流程。 */
			return                   /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	index := -1                     /* 更新 index 的值。 */
	for i, u := range state.Users { /* 循环处理当前数据。 */
		if u.Username == in.Username { /* 判断条件并选择处理分支。 */
			index = i /* 更新 index 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if r.Method == "POST" && index >= 0 { /* 判断条件并选择处理分支。 */
		problem(w, 409, "用户名已存在") /* 执行当前语句并推进处理流程。 */
		return                    /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if r.Method == "PUT" && index < 0 { /* 判断条件并选择处理分支。 */
		problem(w, 404, "用户不存在") /* 执行当前语句并推进处理流程。 */
		return                   /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	u := model.PlatformUser{Username: in.Username, DisplayName: in.DisplayName, Enabled: in.Enabled, RoleIDs: in.RoleIDs, Permissions: in.Permissions, DeviceScope: in.DeviceScope, DeviceIDs: in.DeviceIDs, SessionVersion: time.Now().UnixNano()} /* 更新 u 的值。 */
	if index >= 0 {                                                                                                                                                                                                                                 /* 判断条件并选择处理分支。 */
		u.PasswordHash = state.Users[index].PasswordHash         /* 更新 u.PasswordHash 的值。 */
		u.SessionVersion = state.Users[index].SessionVersion + 1 /* 更新 u.SessionVersion 的值。 */
	} else { /* 结束当前表达式或代码块。 */
		hash, err := hashPassword(in.Password) /* 更新 err 的值。 */
		if err != nil {                        /* 判断条件并选择处理分支。 */
			problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
			return                       /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		u.PasswordHash = hash /* 更新 u.PasswordHash 的值。 */
	} /* 结束当前表达式或代码块。 */
	if u.Username == claims(r).Username { /* 判断条件并选择处理分支。 */
		problem(w, 422, "不能修改当前登录账户，请使用另一个管理员账户") /* 执行当前语句并推进处理流程。 */
		return                                    /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if index < 0 { /* 判断条件并选择处理分支。 */
		state.Users = append(state.Users, u) /* 更新 state.Users 的值。 */
	} else { /* 结束当前表达式或代码块。 */
		state.Users[index] = u /* 更新 state.Users[index] 的值。 */
	} /* 结束当前表达式或代码块。 */
	s.commitAccess(w, r, store, state) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) accessDeleteUser(w http.ResponseWriter, r *http.Request) { /* 定义 accessDeleteUser 函数。 */
	store, state, ok := s.accessState(w, r) /* 更新 ok 的值。 */
	if !ok {                                /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	id := r.PathValue("id")                                /* 更新 id 的值。 */
	if id == claims(r).Username || id == s.cfg.AdminUser { /* 判断条件并选择处理分支。 */
		problem(w, 422, "不能删除当前账户或内置管理员") /* 执行当前语句并推进处理流程。 */
		return                            /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for i, u := range state.Users { /* 循环处理当前数据。 */
		if u.Username == id { /* 判断条件并选择处理分支。 */
			state.Users = append(state.Users[:i], state.Users[i+1:]...) /* 更新 state.Users 的值。 */
			s.commitAccess(w, r, store, state)                          /* 执行当前语句并推进处理流程。 */
			return                                                      /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	problem(w, 404, "用户不存在") /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) accessResetPassword(w http.ResponseWriter, r *http.Request) { /* 定义 accessResetPassword 函数。 */
	var in struct { /* 声明 in。 */
		Password string `json:"password"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if decode(w, r, &in) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	hash, err := hashPassword(in.Password) /* 更新 err 的值。 */
	if err != nil {                        /* 判断条件并选择处理分支。 */
		problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	store, state, ok := s.accessState(w, r) /* 更新 ok 的值。 */
	if !ok {                                /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for i, u := range state.Users { /* 循环处理当前数据。 */
		if u.Username == r.PathValue("id") { /* 判断条件并选择处理分支。 */
			state.Users[i].PasswordHash = hash /* 更新 state.Users[i].PasswordHash 的值。 */
			state.Users[i].SessionVersion++    /* 执行当前语句并推进处理流程。 */
			s.commitAccess(w, r, store, state) /* 执行当前语句并推进处理流程。 */
			return                             /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	problem(w, 404, "用户不存在") /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) accessSaveRole(w http.ResponseWriter, r *http.Request) { /* 定义 accessSaveRole 函数。 */
	var role model.PlatformRole     /* 声明 role。 */
	if decode(w, r, &role) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if role.DeviceScope == "" {
		role.DeviceScope = "none"
	}
	if role.DeviceScope != "none" && role.DeviceScope != "selected" && role.DeviceScope != "all" {
		problem(w, 422, "设备范围无效")
		return
	}
	if role.DeviceIDs == nil || role.DeviceScope != "selected" {
		role.DeviceIDs = []string{}
	}
	for _, id := range role.DeviceIDs {
		if _, err := s.unscopedRepo().GetManagedDevice(r.Context(), claims(r).TenantID, id); err != nil {
			problem(w, 422, "设备不存在或不属于当前租户")
			return
		}
	}

	if role.Permissions == nil { /* 判断条件并选择处理分支。 */
		role.Permissions = []string{} /* 更新 role.Permissions 的值。 */
	} /* 结束当前表达式或代码块。 */
	if id := r.PathValue("id"); id != "" { /* 判断条件并选择处理分支。 */
		role.ID = id /* 更新 role.ID 的值。 */
	} /* 结束当前表达式或代码块。 */
	if !identityPattern.MatchString(role.ID) || strings.TrimSpace(role.Name) == "" || !s.validPermissionsFor(claims(r).TenantID, role.Permissions) { /* 判断条件并选择处理分支。 */
		problem(w, 422, "请填写有效角色标识、名称及权限") /* 执行当前语句并推进处理流程。 */
		return                             /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	store, state, ok := s.accessState(w, r) /* 更新 ok 的值。 */
	if !ok {                                /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	index := -1                     /* 更新 index 的值。 */
	for i, v := range state.Roles { /* 循环处理当前数据。 */
		if v.ID == role.ID { /* 判断条件并选择处理分支。 */
			index = i /* 更新 index 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if r.Method == "POST" && index >= 0 { /* 判断条件并选择处理分支。 */
		problem(w, 409, "角色标识已存在") /* 执行当前语句并推进处理流程。 */
		return                     /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if r.Method == "PUT" && index < 0 { /* 判断条件并选择处理分支。 */
		problem(w, 404, "角色不存在") /* 执行当前语句并推进处理流程。 */
		return                   /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if index < 0 { /* 判断条件并选择处理分支。 */
		state.Roles = append(state.Roles, role) /* 更新 state.Roles 的值。 */
	} else { /* 结束当前表达式或代码块。 */
		state.Roles[index] = role /* 更新 state.Roles[index] 的值。 */
	} /* 结束当前表达式或代码块。 */
	s.commitAccess(w, r, store, state) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) accessDeleteRole(w http.ResponseWriter, r *http.Request) { /* 定义 accessDeleteRole 函数。 */
	store, state, ok := s.accessState(w, r) /* 更新 ok 的值。 */
	if !ok {                                /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	id := r.PathValue("id")         /* 更新 id 的值。 */
	for _, u := range state.Users { /* 循环处理当前数据。 */
		for _, role := range u.RoleIDs { /* 循环处理当前数据。 */
			if role == id { /* 判断条件并选择处理分支。 */
				problem(w, 409, "角色仍有用户使用，请先解除关联") /* 执行当前语句并推进处理流程。 */
				return                             /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	for i, v := range state.Roles { /* 循环处理当前数据。 */
		if v.ID == id { /* 判断条件并选择处理分支。 */
			state.Roles = append(state.Roles[:i], state.Roles[i+1:]...) /* 更新 state.Roles 的值。 */
			s.commitAccess(w, r, store, state)                          /* 执行当前语句并推进处理流程。 */
			return                                                      /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	problem(w, 404, "角色不存在") /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) loginManaged(w http.ResponseWriter, r *http.Request, username, password, tenant string) { /* 定义 loginManaged 函数。 */
	store, err := s.accessStore() /* 更新 err 的值。 */
	if err != nil {               /* 判断条件并选择处理分支。 */
		problem(w, 503, "用户服务暂不可用") /* 执行当前语句并推进处理流程。 */
		return                      /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	state, err := store.LoadAccessState(r.Context(), tenant) /* 更新 err 的值。 */
	if err != nil {                                          /* 判断条件并选择处理分支。 */
		problem(w, 503, "用户服务暂不可用") /* 执行当前语句并推进处理流程。 */
		return                      /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, u := range state.Users { /* 循环处理当前数据。 */
		if u.Username == username && u.Enabled && bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) == nil { /* 判断条件并选择处理分支。 */
			token, err := s.auth.IssueUser(username, tenant, u.SessionVersion, 8*time.Hour) /* 更新 err 的值。 */
			if err != nil {                                                                 /* 判断条件并选择处理分支。 */
				problem(w, 500, "创建会话失败") /* 执行当前语句并推进处理流程。 */
				return                    /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			write(w, 200, map[string]any{"accessToken": token, "expiresIn": 28800, "tenantId": tenant, "role": "operator", "permissions": permissionList(effectivePermissions(state, u)), "displayName": u.DisplayName, "accessVersion": accessVersion(resolveUserDeviceScope(state, u), effectivePermissions(state, u), tenant)}) /* 执行当前语句并推进处理流程。 */
			return                                                                                                                                                                                                                                                                                                                 /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	problem(w, 401, "invalid credentials") /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
