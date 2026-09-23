package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"net/http"                    /* 执行当前语句并推进处理流程。 */
	"strconv"                     /* 执行当前语句并推进处理流程。 */
	"time"                        /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func (s *Server) SetDeviceOperations(publish func(context.Context, string, []byte, byte, bool) error, revoke func(context.Context, string) error) { /* 定义 SetDeviceOperations 函数。 */
	s.onboarding.PublishCommand = publish /* 更新 s.onboarding.PublishCommand 的值。 */
	s.onboarding.RevokeUsername = revoke  /* 更新 s.onboarding.RevokeUsername 的值。 */
}                                                              /* 结束当前表达式或代码块。 */
func (s *Server) RunCredentialRevocations(ctx context.Context) { s.onboarding.RetryRevocations(ctx) } /* 定义 RunCredentialRevocations 函数。 */
func (s *Server) deviceOperationsRoutes() { /* 定义 deviceOperationsRoutes 函数。 */
	s.router.GET("/api/v1/device-registry/:id/history", s.authorize("viewer"), s.endpoint(s.deviceHistory, "id"))         /* 执行当前语句并推进处理流程。 */
	s.router.GET("/api/v1/device-registry/:id/commands", s.authorize("viewer"), s.endpoint(s.listDeviceCommands, "id"))   /* 执行当前语句并推进处理流程。 */
	s.router.POST("/api/v1/device-registry/:id/commands", s.authorize("operator"), s.endpoint(s.sendDeviceCommand, "id")) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func operationPage(r *http.Request) (int, int) { /* 定义 operationPage 函数。 */
	limit, _ := strconv.Atoi(r.URL.Query().Get("pageSize")) /* 更新 _ 的值。 */
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))      /* 更新 _ 的值。 */
	if limit < 1 || limit > 100 {                           /* 判断条件并选择处理分支。 */
		limit = 20 /* 更新 limit 的值。 */
	} /* 结束当前表达式或代码块。 */
	if page < 1 || page > 100000 { /* 判断条件并选择处理分支。 */
		page = 1 /* 更新 page 的值。 */
	} /* 结束当前表达式或代码块。 */
	return limit, (page - 1) * limit /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) operationDevice(w http.ResponseWriter, r *http.Request) bool { /* 定义 operationDevice 函数。 */
	_, e := s.engine.Repo.GetManagedDevice(r.Context(), claims(r).TenantID, r.PathValue("id")) /* 更新 e 的值。 */
	if e != nil {                                                                              /* 判断条件并选择处理分支。 */
		problem(w, 404, "device not found") /* 执行当前语句并推进处理流程。 */
		return false                        /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return true /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) deviceHistory(w http.ResponseWriter, r *http.Request) { /* 定义 deviceHistory 函数。 */
	if !s.operationDevice(w, r) { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	t, d := claims(r).TenantID, r.PathValue("id") /* 更新 d 的值。 */
	limit, offset := operationPage(r)             /* 更新 offset 的值。 */
	var items any                                 /* 声明 items。 */
	var total int                                 /* 声明 total。 */
	var e error                                   /* 声明 e。 */
	switch r.URL.Query().Get("kind") {            /* 根据条件选择处理路径。 */
	case "connection", "": /* 处理当前分支。 */
		items, total, e = s.engine.Repo.ListDeviceStateEvents(r.Context(), t, d, limit, offset) /* 更新 e 的值。 */
	case "property": /* 处理当前分支。 */
		items, total, e = s.engine.Repo.ListDeviceMessages(r.Context(), t, d, model.PropertyReport, limit, offset) /* 更新 e 的值。 */
	case "event": /* 处理当前分支。 */
		items, total, e = s.engine.Repo.ListDeviceMessages(r.Context(), t, d, model.EventReport, limit, offset) /* 更新 e 的值。 */
	default: /* 处理当前分支。 */
		problem(w, 422, "kind must be connection, event or property") /* 执行当前语句并推进处理流程。 */
		return                                                        /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if e != nil { /* 判断条件并选择处理分支。 */
		problem(w, 500, e.Error()) /* 执行当前语句并推进处理流程。 */
		return                     /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 200, map[string]any{"items": items, "total": total}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) listDeviceCommands(w http.ResponseWriter, r *http.Request) { /* 定义 listDeviceCommands 函数。 */
	if !s.operationDevice(w, r) { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	limit, offset := operationPage(r)                                                                                      /* 更新 offset 的值。 */
	items, total, e := s.engine.Repo.ListDeviceCommands(r.Context(), claims(r).TenantID, r.PathValue("id"), limit, offset) /* 更新 e 的值。 */
	if e != nil {                                                                                                          /* 判断条件并选择处理分支。 */
		problem(w, 500, e.Error()) /* 执行当前语句并推进处理流程。 */
		return                     /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for i := range items { /* 循环处理当前数据。 */
		items[i] = items[i].ObservedOutcome(time.Now().UnixMilli()).Public() /* 更新 items[i] 的值。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 200, map[string]any{"items": items, "total": total}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) sendDeviceCommand(w http.ResponseWriter, r *http.Request) { /* 定义 sendDeviceCommand 函数。 */
	if !s.operationDevice(w, r) { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var q model.DeviceCommand                       /* 声明 q。 */
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10) /* 更新 r.Body 的值。 */
	if decode(w, r, &q) != nil {                    /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	v, e := s.onboarding.SendCommand(r.Context(), claims(r).TenantID, r.PathValue("id"), q) /* 更新 e 的值。 */
	if e != nil {                                                                           /* 判断条件并选择处理分支。 */
		problem(w, 422, e.Error()) /* 执行当前语句并推进处理流程。 */
		return                     /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.audit(r, "device.command", "device", v.DeviceID, map[string]any{"commandId": v.ID, "status": v.Status}) /* 执行当前语句并推进处理流程。 */
	write(w, 202, v.Public())                                                                                 /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
