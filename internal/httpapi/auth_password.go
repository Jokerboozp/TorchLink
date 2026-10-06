package httpapi

import (
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"

	"iot-platform/internal/auth"
	"iot-platform/internal/model"
	"iot-platform/internal/version"
)

const passwordChangeTTL = 15 * time.Minute

// writeManagedSession issues a managed user's console session.
func (s *Server) writeManagedSession(w http.ResponseWriter, r *http.Request, state model.AccessState, u model.PlatformUser, tenant string) {
	token, err := s.auth.IssueUser(u.Username, tenant, u.SessionVersion, 8*time.Hour)
	if err != nil {
		s.fail(w, r, err, "创建会话失败")
		return
	}
	permissions := effectivePermissions(state, u)
	s.stripOpsPermissions(tenant, permissions)
	write(w, 200, map[string]any{"accessToken": token, "expiresIn": 28800, "tenantId": tenant, "role": "operator", "permissions": permissionList(permissions), "displayName": u.DisplayName, "accessVersion": s.accessVersion(resolveUserDeviceScope(state, u), permissions, tenant), "platformVersion": version.Version})
}

// changeOwnPassword lets a managed user change the password with the current
// one, using either a console session or the password-change token issued
// at login. Every existing session ends; the response is a new session.
func (s *Server) changeOwnPassword(w http.ResponseWriter, r *http.Request) {
	c, err := s.auth.Parse(auth.Bearer(r.Header.Get("Authorization")))
	if err != nil {
		problem(w, 401, err.Error())
		return
	}
	if c.TokenUse == "" {
		problem(w, 422, "内置管理员的密码由部署配置管理")
		return
	}
	if c.TokenUse != "user" && c.TokenUse != auth.TokenPasswordChange {
		problem(w, 403, "此凭据不能修改密码")
		return
	}
	var in struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	account := c.TenantID + "\x00" + c.Username
	if wait := s.logins.retryAfter(account); wait > 0 {
		lockedOut(w, wait)
		return
	}
	store, err := s.accessStore()
	if err != nil {
		problem(w, 503, "用户服务暂不可用")
		return
	}
	state, err := store.LoadAccessState(r.Context(), c.TenantID)
	if err != nil {
		problem(w, 503, "用户服务暂不可用")
		return
	}
	index := -1
	for i, u := range state.Users {
		if u.Username == c.Username && u.Enabled && u.SessionVersion == c.SessionVersion {
			index = i
		}
	}
	if index < 0 {
		problem(w, 401, "账户已停用或会话已失效，请重新登录")
		return
	}
	u := state.Users[index]
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.CurrentPassword)) != nil {
		s.logins.record(account, false)
		problem(w, 422, "当前密码不正确")
		return
	}
	if in.NewPassword == in.CurrentPassword {
		problem(w, 422, "新密码不能与当前密码相同")
		return
	}
	hash, err := hashPassword(in.NewPassword)
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	u.PasswordHash, u.MustChangePassword = hash, false
	u.SessionVersion++
	state.Users[index] = u
	ok, err := store.SaveAccessState(r.Context(), c.TenantID, state)
	if err != nil {
		s.fail(w, r, err, "保存密码失败")
		return
	}
	if !ok {
		problem(w, 409, "账户信息已被更新，请重新登录后再试")
		return
	}
	s.logins.record(account, true)
	s.engine.RecordAudit(r.Context(), model.AuditLog{TenantID: c.TenantID, Actor: c.Username, Action: "auth.password.change", TargetType: "user", TargetID: c.Username, CreatedAt: time.Now().UnixMilli()})
	s.writeManagedSession(w, r, state, u, c.TenantID)
}
