package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestParseListPaginationSupportsPageAndLegacyOffset(t *testing.T) { /* 定义 TestParseListPaginationSupportsPageAndLegacyOffset 函数。 */
	tests := []struct { /* 更新 tests 的值。 */
		name                string /* 执行当前语句并推进处理流程。 */
		query               string /* 执行当前语句并推进处理流程。 */
		page, pageSize, off int    /* 执行当前语句并推进处理流程。 */
	}{ /* 结束当前表达式或代码块。 */
		{name: "page contract", query: "page=3&pageSize=50", page: 3, pageSize: 50, off: 100},                 /* 执行当前语句并推进处理流程。 */
		{name: "legacy contract", query: "limit=7&offset=14", page: 3, pageSize: 7, off: 14},                  /* 执行当前语句并推进处理流程。 */
		{name: "server cap", query: "page=2&pageSize=1000", page: 2, pageSize: maxPageSize, off: maxPageSize}, /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	for _, tt := range tests { /* 循环处理当前数据。 */
		t.Run(tt.name, func(t *testing.T) { /* 执行当前语句并推进处理流程。 */
			r := httptest.NewRequest("GET", "/api/v1/products?"+tt.query, nil)              /* 更新 r 的值。 */
			got := parseListPagination(r)                                                   /* 更新 got 的值。 */
			if got.Page != tt.page || got.PageSize != tt.pageSize || got.Offset != tt.off { /* 判断条件并选择处理分支。 */
				t.Fatalf("pagination=%+v, want page=%d pageSize=%d offset=%d", got, tt.page, tt.pageSize, tt.off) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		}) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestMemoryProductPaginationReturnsPageAndTotal(t *testing.T) { /* 定义 TestMemoryProductPaginationReturnsPageAndTotal 函数。 */
	repo := memory.NewRepository()       /* 更新 repo 的值。 */
	for index := 0; index < 3; index++ { /* 循环处理当前数据。 */
		if err := repo.SaveProduct(context.Background(), model.Product{TenantID: "tenant_001", ID: "product_" + string(rune('a'+index)), UpdatedAt: int64(index + 1)}); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	items, total, err := repo.ListProductsPage(context.Background(), "tenant_001", 2, 1) /* 更新 err 的值。 */
	if err != nil {                                                                      /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if total != 3 || len(items) != 2 { /* 判断条件并选择处理分支。 */
		t.Fatalf("page len=%d total=%d, want len=2 total=3", len(items), total) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestMemoryPropertyHistoryPaginationKeepsLatestPageChronological(t *testing.T) { /* 定义 TestMemoryPropertyHistoryPaginationKeepsLatestPageChronological 函数。 */
	repo := memory.NewRepository()        /* 更新 repo 的值。 */
	for index := 1; index <= 3; index++ { /* 循环处理当前数据。 */
		if err := repo.SaveStandardMessage(context.Background(), model.StandardMessage{ /* 判断条件并选择处理分支。 */
			TenantID: "tenant_001", DeviceID: "device_001", MessageID: "message_" + string(rune('0'+index)), Timestamp: int64(index), /* 执行当前语句并推进处理流程。 */
			Properties: map[string]any{"temperature": index}, /* 执行当前语句并推进处理流程。 */
		}); err != nil { /* 结束当前表达式或代码块。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	items, total, err := repo.PropertyHistoryPage(context.Background(), "tenant_001", "device_001", "temperature", 0, 0, 2, 0) /* 更新 err 的值。 */
	if err != nil {                                                                                                            /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if total != 3 || len(items) != 2 || items[0]["timestamp"] != int64(2) || items[1]["timestamp"] != int64(3) { /* 判断条件并选择处理分支。 */
		t.Fatalf("history page=%#v total=%d, want timestamps 2,3 and total 3", items, total) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
