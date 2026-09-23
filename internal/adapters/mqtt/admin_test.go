package mqttadapter /* 声明 mqttadapter 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"encoding/json"     /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"reflect"           /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestAdminBanBeforeKickAndRetry(t *testing.T) { /* 定义 TestAdminBanBeforeKickAndRetry 函数。 */
	var calls []string                                                                      /* 声明 calls。 */
	banned, kicked := false, false                                                          /* 更新 kicked 的值。 */
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 s 的值。 */
		u, p, ok := r.BasicAuth()               /* 更新 ok 的值。 */
		if !ok || u != "api" || p != "secret" { /* 判断条件并选择处理分支。 */
			t.Error("missing API auth") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		calls = append(calls, r.Method+" "+r.URL.Path) /* 更新 calls 的值。 */
		switch {                                       /* 根据条件选择处理路径。 */
		case r.Method == "POST": /* 处理当前分支。 */
			var v map[string]any                                                               /* 声明 v。 */
			json.NewDecoder(r.Body).Decode(&v)                                                 /* 执行当前语句并推进处理流程。 */
			if v["who"] != "device-key" || v["as"] != "username" || v["until"] != "infinity" { /* 判断条件并选择处理分支。 */
				t.Error(v) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if banned { /* 判断条件并选择处理分支。 */
				w.WriteHeader(400) /* 执行当前语句并推进处理流程。 */
				return             /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			banned = true      /* 更新 banned 的值。 */
			w.WriteHeader(200) /* 执行当前语句并推进处理流程。 */
		case r.URL.Path == "/api/v5/banned": /* 处理当前分支。 */
			json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"as": "username", "who": "device-key", "until": "infinity"}}}) /* 执行当前语句并推进处理流程。 */
		case r.Method == "GET": /* 处理当前分支。 */
			if !banned { /* 判断条件并选择处理分支。 */
				t.Error("listed before banned") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if r.URL.Query().Get("username") != "device-key" { /* 判断条件并选择处理分支。 */
				t.Error("unscoped lookup") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if kicked { /* 判断条件并选择处理分支。 */
				w.Write([]byte(`{"data":[]}`)) /* 执行当前语句并推进处理流程。 */
			} else { /* 结束当前表达式或代码块。 */
				w.Write([]byte(`{"data":[{"clientid":"client-1","username":"device-key"}]}`)) /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		case r.Method == "DELETE": /* 处理当前分支。 */
			kicked = true      /* 更新 kicked 的值。 */
			w.WriteHeader(204) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	})) /* 结束当前表达式或代码块。 */
	defer s.Close()                                                          /* 安排函数结束时执行清理。 */
	a := Admin{URL: s.URL, Key: "api", Secret: "secret"}                     /* 更新 a 的值。 */
	if e := a.RevokeUsername(context.Background(), "device-key"); e != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if !reflect.DeepEqual(calls, []string{"POST /api/v5/banned", "GET /api/v5/clients", "DELETE /api/v5/clients/client-1", "GET /api/v5/clients"}) { /* 判断条件并选择处理分支。 */
		t.Fatal(calls) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if e := a.RevokeUsername(context.Background(), "device-key"); e != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func TestAdminRejectsUnexpectedClientAndRedirect(t *testing.T) { /* 定义 TestAdminRejectsUnexpectedClientAndRedirect 函数。 */
	for _, redirect := range []bool{false, true} { /* 循环处理当前数据。 */
		t.Run(map[bool]string{false: "identity", true: "redirect"}[redirect], func(t *testing.T) { /* 执行当前语句并推进处理流程。 */
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 s 的值。 */
				if redirect { /* 判断条件并选择处理分支。 */
					w.Header().Set("Location", "http://localhost:1") /* 执行当前语句并推进处理流程。 */
					w.WriteHeader(302)                               /* 执行当前语句并推进处理流程。 */
					return                                           /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
				if r.Method == "DELETE" { /* 判断条件并选择处理分支。 */
					t.Error("wrong user kicked") /* 验证实际结果符合预期。 */
				} /* 结束当前表达式或代码块。 */
				if r.Method == "GET" { /* 判断条件并选择处理分支。 */
					w.Write([]byte(`{"data":[{"clientid":"x","username":"other"}]}`)) /* 执行当前语句并推进处理流程。 */
				} /* 结束当前表达式或代码块。 */
			})) /* 结束当前表达式或代码块。 */
			defer s.Close()                                              /* 安排函数结束时执行清理。 */
			a := Admin{URL: s.URL, Key: "a", Secret: "b"}                /* 更新 a 的值。 */
			if a.RevokeUsername(context.Background(), "device") == nil { /* 判断条件并选择处理分支。 */
				t.Fatal("unsafe response accepted") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		}) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
