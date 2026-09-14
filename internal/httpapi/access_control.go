package httpapi

import (
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"iot-platform/internal/auth"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type permissionItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Menu string `json:"menu"`
	Kind string `json:"kind"`
}

var menuNames = map[string]string{"dashboard": "运行总览", "protocols": "协议管理", "products": "产品管理", "devices": "设备管理", "profiles": "接入网关", "integration": "接入测试", "cameras": "摄像头映射", "alarms": "告警中心", "inspection": "智能巡检", "raw": "原始报文", "rules": "告警规则", "knowledge": "知识库", "aiProviders": "模型管理", "ai": "智能助手", "backups": "备份中心", "access": "用户与权限"}

// Route permissions use the router's canonical pattern, never a caller-supplied URL.
func routeMenu(path string) string {
	if strings.Contains(path, "/knowledge-binding") {
		return "knowledge"
	}
	if strings.Contains(path, "/products/") && strings.Contains(path, "/protocol-binding") {
		return "products"
	}
	if strings.HasSuffix(path, "/debug") {
		return "integration"
	}
	for _, v := range [][2]string{{"/access/", "access"}, {"/health-inspection", "inspection"}, {"/protocol-assistant", "protocols"}, {"/protocol", "protocols"}, {"/modbus-tcp", "protocols"}, {"/device-access-profiles", "profiles"}, {"/onboarding", "integration"}, {"/test-devices", "integration"}, {"/providers", "aiProviders"}, {"/alarm-analysis", "alarms"}, {"/ai/rule-draft", "rules"}, {"/ai/", "ai"}, {"/knowledge/", "knowledge"}, {"/products", "products"}, {"/device-registry", "devices"}, {"/devices", "devices"}, {"/device-states", "devices"}, {"/discovered-devices", "devices"}, {"/connectors", "profiles"}, {"/integrations/video", "cameras"}, {"/raw-messages", "raw"}, {"/replays", "raw"}, {"/alarms", "alarms"}, {"/rules", "rules"}, {"/backups", "backups"}, {"/dashboard", "dashboard"}} {
		if strings.Contains(path, v[0]) {
			return v[1]
		}
	}
	return ""
}
func routeAction(method, path string) string {
	if strings.Contains(path, "/password") {
		return "重置用户密码"
	}
	if strings.Contains(path, "/access/") {
		resource := "角色"
		if strings.Contains(path, "/users") {
			resource = "用户"
		}
		switch method {
		case "POST":
			return "添加" + resource
		case "PUT":
			return "编辑" + resource
		case "DELETE":
			return "删除" + resource
		}
	}
	if strings.Contains(path, "/knowledge-binding") {
		return "配置知识检索策略"
	}
	if strings.HasSuffix(path, "/workflows/admin") {
		return "查看智能体配置"
	}
	if strings.Contains(path, "/workflows") {
		switch method {
		case "POST":
			return "新建智能体"
		case "PUT":
			return "编辑 / 启停智能体"
		case "DELETE":
			return "删除智能体"
		}
	}
	if strings.HasSuffix(path, "/protocol-assistant/publish") {
		return "保存生成的协议"
	}
	if strings.HasSuffix(path, "/alarm-analysis") {
		return "执行告警研判"
	}
	if strings.HasSuffix(path, "/health-inspection") {
		return "同步执行巡检"
	}
	if strings.HasSuffix(path, "/chat/stream") {
		return "流式问答"
	}
	for _, v := range [][2]string{{"/credentials", "管理设备凭据"}, {"/commands", "设备控制"}, {"/source-releases", "上传源码"}, {"/package-releases", "上传制品"}, {"/source", "下载源码"}, {"/package", "下载制品"}, {"/publish", "发布"}, {"/preview", "解析测试"}, {"/generate", "生成协议"}, {"/rollback", "回滚"}, {"/protocol-binding", "绑定协议"}, {"/restore-drill", "校验备份"}, {"/files/", "下载备份"}, {"/download", "下载报文"}, {"/replay", "回放报文"}, {"/debug", "发送测试报文"}, {"/test", "连接测试"}, {"/actions", "处置告警"}, {"/pdf", "下载报告"}, {"/run", "执行巡检"}, {"/chat", "发送提问"}, {"/password", "重置密码"}, {"/provision", "准备测试设备"}} {
		if strings.Contains(path, v[0]) {
			return v[1]
		}
	}
	switch method {
	case "POST":
		return "新增 / 执行" + menuNames[routeMenu(path)]
	case "PUT", "PATCH":
		return "编辑" + menuNames[routeMenu(path)]
	case "DELETE":
		return "删除" + menuNames[routeMenu(path)]
	default:
		return "查看"
	}
}
func protectedRead(path string) bool {
	return strings.HasSuffix(path, "/source") || strings.HasSuffix(path, "/package") || strings.Contains(path, "/files/") || strings.HasSuffix(path, "/download") || strings.HasSuffix(path, "/workflows/admin")
}
func (s *Server) permissionCatalog() []permissionItem {
	items := []permissionItem{}
	for id, name := range menuNames {
		items = append(items, permissionItem{"menu:" + id, name, id, "menu"})
	}
	for _, r := range s.router.Routes() {
		menu := routeMenu(r.Path)
		if menu == "" || strings.Contains(r.Path, "/device-ingest") || r.Path == "/api/v1/integrations/video/alarm" {
			continue
		}
		if r.Method == "GET" && !protectedRead(r.Path) {
			continue
		}
		items = append(items, permissionItem{r.Method + " " + r.Path, routeAction(r.Method, r.Path), menu, "action"})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}
func effectivePermissions(state model.AccessState, user model.PlatformUser) map[string]bool {
	p := map[string]bool{}
	for _, v := range user.Permissions {
		p[v] = true
	}
	for _, role := range state.Roles {
		for _, id := range user.RoleIDs {
			if role.ID == id {
				for _, v := range role.Permissions {
					p[v] = true
				}
			}
		}
	}
	// Device data must not become visible through a related menu alone.
	if !p["menu:devices"] || user.DeviceScope == "none" || user.DeviceScope == "" {
		delete(p, "menu:alarms")
		delete(p, "menu:raw")
	}
	if !p["menu:devices"] || user.DeviceScope != "all" {
		// These services produce tenant-wide artifacts or launch tenant-wide jobs.
		for _, menu := range []string{"ai", "inspection", "backups", "profiles", "integration", "rules", "cameras", "access"} {
			delete(p, "menu:"+menu)
		}
		for _, action := range []string{"POST /api/v1/device-registry", "POST /api/v1/device-states", "POST /api/v1/raw-messages", "POST /api/v1/raw-messages/replay"} {
			delete(p, action)
		}
	}
	for id := range p {
		if parts := strings.SplitN(id, " ", 2); len(parts) == 2 && !p["menu:"+routeMenu(parts[1])] {
			delete(p, id)
		}
	}
	return p
}
func permissionList(p map[string]bool) []string {
	out := []string{}
	for id, ok := range p {
		if ok {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}
func allowsRoute(p map[string]bool, method, path string) bool {
	if path == "/api/v1/auth/me" {
		return true
	}
	// Broker token is restricted separately. Managed users cannot access generic MCP.
	if path == "/api/v1/events" {
		return p["menu:devices"] || p["menu:alarms"] || p["menu:dashboard"] || p["menu:raw"]
	}
	if path == "/api/v1/mqtt/token" || path == "/api/v1/mqtt/load-token" {
		return false
	}
	menu := routeMenu(path)
	if menu == "" {
		return false
	}
	if method != "GET" || protectedRead(path) {
		return p[method+" "+path] && p["menu:"+menu]
	}
	if p["menu:"+menu] {
		return true
	}
	// Read-only lookups needed by related pages, without granting mutation rights.
	lookups := map[string][]string{
		"/api/v1/protocol-packages": {"products"},
		"/api/v1/alarms":            {"dashboard", "integration"}, "/api/v1/alarms/:id": {"dashboard"},
		"/api/v1/device-registry/:id/connection-guide": {"integration"}, "/api/v1/device-registry/:id/connection": {"integration"},
		"/api/v1/device-registry/:id/history": {"integration"}, "/api/v1/device-registry/:id/children": {"integration"}, "/api/v1/device-registry/:id/commands": {"integration"},
		"/api/v1/raw-messages/:id": {"integration", "devices"}, "/api/v1/rules": {"ai"},
	}
	for _, dep := range lookups[path] {
		if p["menu:"+dep] {
			return true
		}
	}
	deps := map[string][]string{"/api/v1/products": {"devices", "profiles", "protocols", "integration", "cameras", "rules", "knowledge"}, "/api/v1/device-registry": {"profiles", "integration", "cameras", "alarms", "inspection"}, "/api/v2/protocols": {"products", "devices", "profiles", "integration"}, "/api/v1/connectors": {"devices", "integration"}, "/api/v1/connectors/types": {"products", "integration"}, "/api/v1/ai/workflows": {"knowledge"}, "/api/v1/ai/providers": {"ai"}}
	for _, dep := range deps[path] {
		if p["menu:"+dep] {
			return true
		}
	}
	return false
}
func (s *Server) accessStore() (ports.AccessStore, error) {
	v, ok := s.engine.Repo.(ports.AccessStore)
	if !ok {
		return nil, errors.New("access storage unavailable")
	}
	return v, nil
}
func (s *Server) managedIdentity(r *http.Request, c auth.Claims) (model.PlatformUser, map[string]bool, error) {
	store, err := s.accessStore()
	if err != nil {
		return model.PlatformUser{}, nil, err
	}
	state, err := store.LoadAccessState(r.Context(), c.TenantID)
	if err != nil {
		return model.PlatformUser{}, nil, err
	}
	for _, u := range state.Users {
		if u.Username == c.Username && u.Enabled && u.SessionVersion == c.SessionVersion {
			return u, effectivePermissions(state, u), nil
		}
	}
	return model.PlatformUser{}, nil, errors.New("account disabled or session revoked")
}
func (s *Server) accessRoutes() {
	s.router.GET("/api/v1/events", s.authorize("viewer"), s.endpoint(s.userEvents))
	s.router.GET("/api/v1/access/device-options", s.authorize("admin"), s.endpoint(s.accessDeviceOptions))
	s.router.GET("/api/v1/auth/me", s.authorize("viewer"), s.endpoint(s.currentIdentity))
	s.router.GET("/api/v1/access/permissions", s.authorize("admin"), s.endpoint(func(w http.ResponseWriter, r *http.Request) {
		write(w, 200, map[string]any{"items": s.permissionCatalog()})
	}))
	s.router.GET("/api/v1/access/users", s.authorize("admin"), s.endpoint(s.accessList))
	s.router.GET("/api/v1/access/roles", s.authorize("admin"), s.endpoint(s.accessList))
	s.router.POST("/api/v1/access/users", s.authorize("admin"), s.endpoint(s.accessSaveUser))
	s.router.PUT("/api/v1/access/users/:id", s.authorize("admin"), s.endpoint(s.accessSaveUser, "id"))
	s.router.DELETE("/api/v1/access/users/:id", s.authorize("admin"), s.endpoint(s.accessDeleteUser, "id"))
	s.router.POST("/api/v1/access/users/:id/password", s.authorize("admin"), s.endpoint(s.accessResetPassword, "id"))
	s.router.POST("/api/v1/access/roles", s.authorize("admin"), s.endpoint(s.accessSaveRole))
	s.router.PUT("/api/v1/access/roles/:id", s.authorize("admin"), s.endpoint(s.accessSaveRole, "id"))
	s.router.DELETE("/api/v1/access/roles/:id", s.authorize("admin"), s.endpoint(s.accessDeleteRole, "id"))
}
func (s *Server) currentIdentity(w http.ResponseWriter, r *http.Request) {
	c := claims(r)
	perms := []string{"*"}
	name := c.Username
	if c.TokenUse == "user" {
		u, p, err := s.managedIdentity(r, c)
		if err != nil {
			problem(w, 401, err.Error())
			return
		}
		perms = permissionList(p)
		name = u.DisplayName
	}
	write(w, 200, map[string]any{"username": c.Username, "displayName": name, "tenantId": c.TenantID, "role": c.Role, "permissions": perms})
}

func (s *Server) canConfigureAI(r *http.Request) bool {
	c := claims(r)
	if c.TokenUse != "user" {
		return c.Role == "admin"
	}
	_, p, err := s.managedIdentity(r, c)
	return err == nil && p["menu:aiProviders"] && (p["PUT /api/v1/ai/providers/config"] || p["POST /api/v1/ai/providers/test"])
}
func (s *Server) accessState(w http.ResponseWriter, r *http.Request) (ports.AccessStore, model.AccessState, bool) {
	store, err := s.accessStore()
	if err != nil {
		problem(w, 503, err.Error())
		return nil, model.AccessState{}, false
	}
	state, err := store.LoadAccessState(r.Context(), claims(r).TenantID)
	if err != nil {
		problem(w, 500, "读取用户权限失败")
		return nil, state, false
	}
	return store, state, true
}
func (s *Server) commitAccess(w http.ResponseWriter, r *http.Request, store ports.AccessStore, state model.AccessState) {
	ok, err := store.SaveAccessState(r.Context(), claims(r).TenantID, state)
	if err != nil {
		problem(w, 500, "保存用户权限失败")
		return
	}
	if !ok {
		problem(w, 409, "配置已被其他操作更新，请刷新重试")
		return
	}
	write(w, 200, map[string]bool{"success": true})
}
func (s *Server) accessList(w http.ResponseWriter, r *http.Request) {
	_, state, ok := s.accessState(w, r)
	if !ok {
		return
	}
	if strings.HasSuffix(r.URL.Path, "/roles") {
		if state.Roles == nil {
			state.Roles = []model.PlatformRole{}
		}
		write(w, 200, map[string]any{"items": state.Roles})
		return
	}
	for i := range state.Users {
		state.Users[i].PasswordHash = ""
	}
	if state.Users == nil {
		state.Users = []model.PlatformUser{}
	}
	write(w, 200, map[string]any{"items": state.Users, "tenantId": claims(r).TenantID})
}

var identityPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{2,63}$`)

func hashPassword(password string) (string, error) {
	if len(password) < 10 || len(password) > 72 {
		return "", errors.New("密码长度须为10至72字节")
	}
	v, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(v), err
}
func (s *Server) validPermissions(values []string) bool {
	known := map[string]bool{}
	for _, v := range s.permissionCatalog() {
		known[v.ID] = true
	}
	for _, v := range values {
		if !known[v] {
			return false
		}
	}
	return true
}
func (s *Server) accessSaveUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username    string   `json:"username"`
		DisplayName string   `json:"displayName"`
		Password    string   `json:"password"`
		Enabled     bool     `json:"enabled"`
		RoleIDs     []string `json:"roleIds"`
		Permissions []string `json:"permissions"`
		DeviceScope string   `json:"deviceScope"`
		DeviceIDs   []string `json:"deviceIds"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	if in.RoleIDs == nil {
		in.RoleIDs = []string{}
	}
	if in.Permissions == nil {
		in.Permissions = []string{}
	}
	if in.DeviceScope == "" {
		in.DeviceScope = "none"
	}
	if in.DeviceScope != "none" && in.DeviceScope != "selected" && in.DeviceScope != "all" {
		problem(w, 422, "设备范围无效")
		return
	}
	if in.DeviceIDs == nil || in.DeviceScope != "selected" {
		in.DeviceIDs = []string{}
	}
	for _, id := range in.DeviceIDs {
		if _, err := s.unscopedRepo().GetManagedDevice(r.Context(), claims(r).TenantID, id); err != nil {
			problem(w, 422, "授权设备不存在或不属于当前租户")
			return
		}
	}
	if id := r.PathValue("id"); id != "" {
		in.Username = id
	}
	if !identityPattern.MatchString(in.Username) || in.Username == s.cfg.AdminUser {
		problem(w, 422, "用户名须为3至64位字母、数字、点、横线或下划线，且不能使用内置管理员名称")
		return
	}
	if !s.validPermissions(in.Permissions) {
		problem(w, 422, "存在无效权限")
		return
	}
	store, state, ok := s.accessState(w, r)
	if !ok {
		return
	}
	for _, id := range in.RoleIDs {
		found := false
		for _, role := range state.Roles {
			if role.ID == id {
				found = true
			}
		}
		if !found {
			problem(w, 422, "角色不存在")
			return
		}
	}
	index := -1
	for i, u := range state.Users {
		if u.Username == in.Username {
			index = i
		}
	}
	if r.Method == "POST" && index >= 0 {
		problem(w, 409, "用户名已存在")
		return
	}
	if r.Method == "PUT" && index < 0 {
		problem(w, 404, "用户不存在")
		return
	}
	u := model.PlatformUser{Username: in.Username, DisplayName: in.DisplayName, Enabled: in.Enabled, RoleIDs: in.RoleIDs, Permissions: in.Permissions, DeviceScope: in.DeviceScope, DeviceIDs: in.DeviceIDs, SessionVersion: time.Now().UnixNano()}
	if index >= 0 {
		u.PasswordHash = state.Users[index].PasswordHash
		u.SessionVersion = state.Users[index].SessionVersion + 1
	} else {
		hash, err := hashPassword(in.Password)
		if err != nil {
			problem(w, 422, err.Error())
			return
		}
		u.PasswordHash = hash
	}
	if u.Username == claims(r).Username {
		problem(w, 422, "不能修改当前登录账户，请使用另一个管理员账户")
		return
	}
	if index < 0 {
		state.Users = append(state.Users, u)
	} else {
		state.Users[index] = u
	}
	s.commitAccess(w, r, store, state)
}
func (s *Server) accessDeleteUser(w http.ResponseWriter, r *http.Request) {
	store, state, ok := s.accessState(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if id == claims(r).Username || id == s.cfg.AdminUser {
		problem(w, 422, "不能删除当前账户或内置管理员")
		return
	}
	for i, u := range state.Users {
		if u.Username == id {
			state.Users = append(state.Users[:i], state.Users[i+1:]...)
			s.commitAccess(w, r, store, state)
			return
		}
	}
	problem(w, 404, "用户不存在")
}
func (s *Server) accessResetPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	hash, err := hashPassword(in.Password)
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	store, state, ok := s.accessState(w, r)
	if !ok {
		return
	}
	for i, u := range state.Users {
		if u.Username == r.PathValue("id") {
			state.Users[i].PasswordHash = hash
			state.Users[i].SessionVersion++
			s.commitAccess(w, r, store, state)
			return
		}
	}
	problem(w, 404, "用户不存在")
}
func (s *Server) accessSaveRole(w http.ResponseWriter, r *http.Request) {
	var role model.PlatformRole
	if decode(w, r, &role) != nil {
		return
	}
	if role.Permissions == nil {
		role.Permissions = []string{}
	}
	if id := r.PathValue("id"); id != "" {
		role.ID = id
	}
	if !identityPattern.MatchString(role.ID) || strings.TrimSpace(role.Name) == "" || !s.validPermissions(role.Permissions) {
		problem(w, 422, "请填写有效角色标识、名称及权限")
		return
	}
	store, state, ok := s.accessState(w, r)
	if !ok {
		return
	}
	index := -1
	for i, v := range state.Roles {
		if v.ID == role.ID {
			index = i
		}
	}
	if r.Method == "POST" && index >= 0 {
		problem(w, 409, "角色标识已存在")
		return
	}
	if r.Method == "PUT" && index < 0 {
		problem(w, 404, "角色不存在")
		return
	}
	if index < 0 {
		state.Roles = append(state.Roles, role)
	} else {
		state.Roles[index] = role
	}
	s.commitAccess(w, r, store, state)
}
func (s *Server) accessDeleteRole(w http.ResponseWriter, r *http.Request) {
	store, state, ok := s.accessState(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	for _, u := range state.Users {
		for _, role := range u.RoleIDs {
			if role == id {
				problem(w, 409, "角色仍有用户使用，请先解除关联")
				return
			}
		}
	}
	for i, v := range state.Roles {
		if v.ID == id {
			state.Roles = append(state.Roles[:i], state.Roles[i+1:]...)
			s.commitAccess(w, r, store, state)
			return
		}
	}
	problem(w, 404, "角色不存在")
}
func (s *Server) loginManaged(w http.ResponseWriter, r *http.Request, username, password, tenant string) {
	store, err := s.accessStore()
	if err != nil {
		problem(w, 503, "用户服务暂不可用")
		return
	}
	state, err := store.LoadAccessState(r.Context(), tenant)
	if err != nil {
		problem(w, 503, "用户服务暂不可用")
		return
	}
	for _, u := range state.Users {
		if u.Username == username && u.Enabled && bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) == nil {
			token, err := s.auth.IssueUser(username, tenant, u.SessionVersion, 8*time.Hour)
			if err != nil {
				problem(w, 500, "创建会话失败")
				return
			}
			write(w, 200, map[string]any{"accessToken": token, "expiresIn": 28800, "tenantId": tenant, "role": "operator", "permissions": permissionList(effectivePermissions(state, u)), "displayName": u.DisplayName})
			return
		}
	}
	problem(w, 401, "invalid credentials")
}
