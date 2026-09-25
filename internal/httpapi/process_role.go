package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httputil" /* 执行当前语句并推进处理流程。 */
	"net/url"           /* 执行当前语句并推进处理流程。 */
	"strings"           /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// Access routes execute on the process which owns transport sessions. Authentication
// is preserved and evaluated by the same API handlers at the destination.
func accessRoute(path string) bool { /* 定义 accessRoute 函数。 */
	if strings.HasPrefix(path, "/api/v1/device-ingest/") || path == "/api/v1/device-mqtt/token" || path == "/api/v1/connectors" || path == "/api/v1/onboarding" || path == "/api/v1/onboarding/preflight" { /* 判断条件并选择处理分支。 */
		return true /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if strings.HasPrefix(path, "/api/v1/device-registry/") && (strings.HasSuffix(path, "/connection") || strings.HasSuffix(path, "/commands")) { /* 判断条件并选择处理分支。 */
		return true /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return strings.HasPrefix(path, "/api/v2/device-access-profiles/") && (strings.HasSuffix(path, "/commands") || strings.HasSuffix(path, "/sessions") || strings.HasSuffix(path, "/test")) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) roleHandler() http.Handler { /* 定义 roleHandler 函数。 */
	if s.cfg.ProcessRole != "api" && s.cfg.ProcessRole != "gateway" && !s.cfg.AccessCoordination { /* 判断条件并选择处理分支。 */
		return s.router /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	local := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 local 的值。 */
		if target := s.executionTarget(r); target != "" { /* 判断条件并选择处理分支。 */
			executionProxy(w, r, target) /* 执行当前语句并推进处理流程。 */
			return                       /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		s.router.ServeHTTP(w, r) /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */
	if s.cfg.ProcessRole == "gateway" { /* 判断条件并选择处理分支。 */
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 返回当前处理结果。 */
			if !accessRoute(r.URL.Path) && !strings.HasPrefix(r.URL.Path, "/health/") { /* 判断条件并选择处理分支。 */
				problem(w, 404, "route is not served by access gateway") /* 执行当前语句并推进处理流程。 */
				return                                                   /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			local.ServeHTTP(w, r) /* 执行当前语句并推进处理流程。 */
		}) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if s.cfg.ProcessRole != "api" { /* 判断条件并选择处理分支。 */
		return local /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	target, err := url.Parse(s.cfg.AccessGatewayURL)                                              /* 更新 err 的值。 */
	if err != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") { /* 判断条件并选择处理分支。 */
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { problem(w, 503, "access gateway is not configured") }) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	proxy := httputil.NewSingleHostReverseProxy(target)                            /* 更新 proxy 的值。 */
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) { /* 更新 proxy.ErrorHandler 的值。 */
		problem(w, 503, "access gateway is unavailable; operation was not confirmed") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 返回当前处理结果。 */
		if accessRoute(r.URL.Path) { /* 判断条件并选择处理分支。 */
			proxy.ServeHTTP(w, r) /* 执行当前语句并推进处理流程。 */
			return                /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		local.ServeHTTP(w, r) /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
