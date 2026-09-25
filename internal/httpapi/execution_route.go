package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httputil" /* 执行当前语句并推进处理流程。 */
	"net/url"           /* 执行当前语句并推进处理流程。 */
	"strconv"           /* 执行当前语句并推进处理流程。 */
	"strings"           /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// Execution endpoints are advertised by configured runtimes through PostgreSQL,
// never taken from a request body or arbitrary redirect supplied by the device.
func (s *Server) executionTarget(r *http.Request) string { /* 定义 executionTarget 函数。 */
	if !s.cfg.AccessCoordination { /* 判断条件并选择处理分支。 */
		return "" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") /* 更新 token 的值。 */
	claim, err := s.auth.Parse(token)                                     /* 更新 err 的值。 */
	if err != nil {                                                       /* 判断条件并选择处理分支。 */
		return "" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")                       /* 更新 parts 的值。 */
	var profile string                                                               /* 声明 profile。 */
	if len(parts) >= 5 && parts[1] == "v2" && parts[2] == "device-access-profiles" { /* 判断条件并选择处理分支。 */
		profile = parts[3] /* 更新 profile 的值。 */
	} /* 结束当前表达式或代码块。 */
	if len(parts) == 5 && parts[1] == "v1" && parts[2] == "device-registry" && (parts[4] == "connection" || parts[4] == "commands") { /* 判断条件并选择处理分支。 */
		d, err := s.engine.Repo.GetManagedDevice(r.Context(), claim.TenantID, parts[3]) /* 更新 err 的值。 */
		if err != nil {                                                                 /* 判断条件并选择处理分支。 */
			return "" /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		profile = d.ConnectorProfileID /* 更新 profile 的值。 */
		if profile == "" {             /* 判断条件并选择处理分支。 */
			profiles, err := s.engine.Repo.ListDeviceAccessProfiles(r.Context(), claim.TenantID) /* 更新 err 的值。 */
			if err != nil {                                                                      /* 判断条件并选择处理分支。 */
				return "" /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			for _, p := range profiles { /* 循环处理当前数据。 */
				if p.DeviceID == d.ID { /* 判断条件并选择处理分支。 */
					if profile != "" { /* 判断条件并选择处理分支。 */
						return "" /* 返回当前处理结果。 */
					} /* 结束当前表达式或代码块。 */
					profile = p.ID /* 更新 profile 的值。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if profile == "" { /* 判断条件并选择处理分支。 */
		return "" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	lease, err := s.engine.Repo.GetExecutionLease(r.Context(), claim.TenantID, "profile/"+profile) /* 更新 err 的值。 */
	if err != nil || lease.Endpoint == "" || lease.Endpoint == s.cfg.AccessNodeURL {               /* 判断条件并选择处理分支。 */
		return "" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return lease.Endpoint /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func executionProxy(w http.ResponseWriter, r *http.Request, origin string) { /* 定义 executionProxy 函数。 */
	hops, _ := strconv.Atoi(r.Header.Get("X-Iot-Gateway-Hops")) /* 更新 _ 的值。 */
	if hops >= 2 {                                              /* 判断条件并选择处理分支。 */
		problem(w, 503, "execution routing changed; retry status lookup") /* 执行当前语句并推进处理流程。 */
		return                                                            /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	target, err := url.Parse(origin)                                                                                    /* 更新 err 的值。 */
	if err != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") || target.User != nil { /* 判断条件并选择处理分支。 */
		problem(w, 503, "invalid execution endpoint") /* 执行当前语句并推进处理流程。 */
		return                                        /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	forwarded := r.Clone(r.Context())                                              /* 更新 forwarded 的值。 */
	forwarded.Header.Set("X-Iot-Gateway-Hops", strconv.Itoa(hops+1))               /* 执行当前语句并推进处理流程。 */
	proxy := httputil.NewSingleHostReverseProxy(target)                            /* 更新 proxy 的值。 */
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) { /* 更新 proxy.ErrorHandler 的值。 */
		problem(w, 503, "execution node unavailable; command result is unconfirmed") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	proxy.ServeHTTP(w, forwarded) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
