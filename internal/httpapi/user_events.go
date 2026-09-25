package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/json"
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"net/http"                    /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// Authenticated polling re-evaluates roles and device scope on every request.
// Managed accounts never receive a reusable tenant-wide broker credential.
func (s *Server) userEvents(w http.ResponseWriter, r *http.Request) { /* 定义 userEvents 函数。 */
	c := claims(r)                  /* 更新 c 的值。 */
	alarms := []model.Alarm{}       /* 更新 alarms 的值。 */
	states := []model.DeviceState{} /* 更新 states 的值。 */
	p := map[string]bool{"*": true} /* 更新 p 的值。 */
	var e error                     /* 声明 e。 */
	if c.TokenUse == "user" {       /* 判断条件并选择处理分支。 */
		_, p, e = s.managedIdentity(r, c) /* 更新 e 的值。 */
		if e != nil {                     /* 判断条件并选择处理分支。 */
			problem(w, 401, "会话已失效") /* 执行当前语句并推进处理流程。 */
			return                   /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} else if c.TokenUse != "" || c.Role != "admin" { /* 结束当前表达式或代码块。 */
		problem(w, 403, "此接口用于已登录用户消息") /* 执行当前语句并推进处理流程。 */
		return                          /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	wantAlarms := p["*"] || p["menu:alarms"] || p["menu:dashboard"]
	wantStates := p["*"] || p["menu:devices"] || p["menu:raw"]
	if wantAlarms || wantStates {
		allAlarms, allStates, err := s.events.get(r.Context(), s.unscopedRepo(), c.TenantID)
		if err != nil {
			problem(w, 503, "读取消息失败")
			return
		}
		if wantAlarms {
			alarms = scopedEventAlarms(r.Context(), c.TenantID, allAlarms)
		}
		if wantStates {
			states = scopedEventStates(r.Context(), c.TenantID, allStates)
		}
	}
	body, err := json.Marshal(map[string]any{"alarms": alarms, "devices": states, "permissions": permissionList(p), "accessVersion": requestAccessVersion(r.Context(), c)})
	if err != nil {
		problem(w, 500, "读取消息失败")
		return
	}
	// Pages poll every few seconds; an unchanged view answers 304 without a body.
	tag := eventETag(body)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("ETag", tag)
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(body, '\n'))
}
