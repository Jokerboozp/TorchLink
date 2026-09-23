package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"encoding/json"     /* 执行当前语句并推进处理流程。 */
	"io"                /* 执行当前语句并推进处理流程。 */
	"log/slog"          /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"strconv"           /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */
	"time"              /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics"         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestOversizedPaginationThroughHTTP(t *testing.T) { /* 定义 TestOversizedPaginationThroughHTTP 函数。 */
	repo := memory.NewRepository()                                                                                           /* 更新 repo 的值。 */
	if err := repo.SaveProduct(context.Background(), model.Product{ID: "first-product", TenantID: "tenant-a"}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	api := New(config.Config{JWTSecret: "audit-only-secret-at-least-32-characters"}, &core.Engine{Repo: repo}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 api 的值。 */
	token, err := api.auth.Issue("audit", "tenant-a", "viewer", nil, time.Minute)                                                                                             /* 检查错误并决定后续处理。 */
	if err != nil {                                                                                                                                                           /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for _, path := range []string{"/api/v1/ai/providers", "/api/v1/products"} { /* 循环处理当前数据。 */
		for _, query := range []string{"page=1&pageSize=20", "page=9223372036854775807&pageSize=20", "page=999999999999999999999999999999&pageSize=100", "offset=9223372036854775807&limit=1"} { /* 循环处理当前数据。 */
			r := httptest.NewRequest(http.MethodGet, path+"?"+query, nil) /* 更新 r 的值。 */
			r.Header.Set("Authorization", "Bearer "+token)                /* 执行当前语句并推进处理流程。 */
			w := httptest.NewRecorder()                                   /* 更新 w 的值。 */
			api.Handler().ServeHTTP(w, r)                                 /* 执行当前语句并推进处理流程。 */
			if w.Code != http.StatusOK {                                  /* 判断条件并选择处理分支。 */
				t.Fatalf("%s?%s: status=%d", path, query, w.Code) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			var result struct { /* 声明 result。 */
				Items []json.RawMessage `json:"items"` /* 执行当前语句并推进处理流程。 */
				Total int               `json:"total"` /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil { /* 判断条件并选择处理分支。 */
				t.Fatal(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if query != "page=1&pageSize=20" && len(result.Items) != 0 { /* 判断条件并选择处理分支。 */
				t.Errorf("%s?%s: oversized page returned first-page data", path, query) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if path == "/api/v1/products" && (result.Total != 1 || (query == "page=1&pageSize=20" && len(result.Items) != 1)) { /* 判断条件并选择处理分支。 */
				t.Errorf("product pagination lost total or normal page: %+v", result) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestPaginationArithmeticStaysInRange(t *testing.T) { /* 定义 TestPaginationArithmeticStaysInRange 函数。 */
	maxInt := int(^uint(0) >> 1)             /* 更新 maxInt 的值。 */
	for _, size := range []int{1, 20, 100} { /* 循环处理当前数据。 */
		for _, param := range []string{"page", "offset"} { /* 循环处理当前数据。 */
			r := httptest.NewRequest(http.MethodGet, "/?"+param+"="+strconv.Itoa(maxInt)+"&pageSize="+strconv.Itoa(size), nil) /* 更新 r 的值。 */
			p := parseListPagination(r)                                                                                        /* 更新 p 的值。 */
			if p.Page < 1 || p.Offset < 0 || p.Offset > maxInt-p.PageSize {                                                    /* 判断条件并选择处理分支。 */
				t.Fatalf("unsafe pagination: %+v", p) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestPageItemsHandlesInvalidAndOverflowingBounds(t *testing.T) { /* 定义 TestPageItemsHandlesInvalidAndOverflowingBounds 函数。 */
	for _, p := range []listPagination{{Offset: -1, PageSize: 20}, {Offset: 0, PageSize: -1}, {Offset: 1, PageSize: int(^uint(0) >> 1)}} { /* 循环处理当前数据。 */
		items, total := pageItems([]int{1, 2, 3}, p) /* 更新 total 的值。 */
		if total != 3 {                              /* 判断条件并选择处理分支。 */
			t.Fatalf("total=%d", total) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if p.Offset < 0 || p.PageSize < 0 { /* 判断条件并选择处理分支。 */
			if len(items) != 0 { /* 判断条件并选择处理分支。 */
				t.Fatalf("invalid bounds returned data: %v", items) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		} else if len(items) != 2 || items[0] != 2 { /* 结束当前表达式或代码块。 */
			t.Fatalf("overflowing size lost remaining items: %v", items) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
