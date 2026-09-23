package clickhouse /* 声明 clickhouse 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"encoding/json"     /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"strings"           /* 执行当前语句并推进处理流程。 */
	"sync/atomic"       /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestHealthChecksClickHouseAfterInitialization(t *testing.T) { /* 定义 TestHealthChecksClickHouseAfterInitialization 函数。 */
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { /* 更新 server 的值。 */
		w.WriteHeader(http.StatusOK) /* 执行当前语句并推进处理流程。 */
	})) /* 结束当前表达式或代码块。 */
	repo, err := New(context.Background(), server.URL, memory.NewRepository()) /* 更新 err 的值。 */
	if err != nil {                                                            /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	server.Close() /* 执行当前语句并推进处理流程。 */

	if err := repo.Health(context.Background()); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("ClickHouse is unreachable but repository health still reports success") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestClaimRetriesMissingTelemetryAfterTransientInsertFailure(t *testing.T) { /* 定义 TestClaimRetriesMissingTelemetryAfterTransientInsertFailure 函数。 */
	var telemetryAttempts atomic.Int32                                                    /* 声明 telemetryAttempts。 */
	server := newClickHouseTestServer(t, func(query string, w http.ResponseWriter) bool { /* 更新 server 的值。 */
		if strings.Contains(query, "INSERT INTO iot_telemetry") && telemetryAttempts.Add(1) == 1 { /* 判断条件并选择处理分支。 */
			http.Error(w, "temporary outage", http.StatusServiceUnavailable) /* 执行当前语句并推进处理流程。 */
			return true                                                      /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return false /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
	defer server.Close() /* 安排函数结束时执行清理。 */

	repo, err := New(context.Background(), server.URL, memory.NewRepository()) /* 更新 err 的值。 */
	if err != nil {                                                            /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	message := testTelemetryMessage()                                                     /* 更新 message 的值。 */
	if _, _, err = repo.ClaimStandardMessage(context.Background(), message); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("first ClickHouse insert unexpectedly succeeded") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	shouldProcess, _, err := repo.ClaimStandardMessage(context.Background(), message) /* 更新 err 的值。 */
	if err != nil || !shouldProcess {                                                 /* 判断条件并选择处理分支。 */
		t.Fatalf("retry claim failed: shouldProcess=%v err=%v", shouldProcess, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if got := telemetryAttempts.Load(); got != 2 { /* 判断条件并选择处理分支。 */
		t.Fatalf("transient ClickHouse insert was not retried: attempts=%d want=2", got) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestClaimDoesNotDuplicateExistingTelemetryDuringBusinessRetry(t *testing.T) { /* 定义 TestClaimDoesNotDuplicateExistingTelemetryDuringBusinessRetry 函数。 */
	var telemetryRows atomic.Int32                                                        /* 声明 telemetryRows。 */
	server := newClickHouseTestServer(t, func(query string, w http.ResponseWriter) bool { /* 更新 server 的值。 */
		switch { /* 根据条件选择处理路径。 */
		case strings.Contains(query, "INSERT INTO iot_telemetry"): /* 处理当前分支。 */
			telemetryRows.Add(1) /* 执行当前语句并推进处理流程。 */
		case strings.Contains(query, "SELECT count() AS total FROM iot_telemetry"): /* 处理当前分支。 */
			_ = json.NewEncoder(w).Encode(map[string]int32{"total": telemetryRows.Load()}) /* 更新 _ 的值。 */
			return true                                                                    /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return false /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
	defer server.Close() /* 安排函数结束时执行清理。 */

	repo, err := New(context.Background(), server.URL, memory.NewRepository()) /* 更新 err 的值。 */
	if err != nil {                                                            /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	message := testTelemetryMessage()                                                                                              /* 更新 message 的值。 */
	if shouldProcess, _, claimErr := repo.ClaimStandardMessage(context.Background(), message); claimErr != nil || !shouldProcess { /* 判断条件并选择处理分支。 */
		t.Fatalf("initial claim failed: shouldProcess=%v err=%v", shouldProcess, claimErr) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if shouldProcess, _, claimErr := repo.ClaimStandardMessage(context.Background(), message); claimErr != nil || !shouldProcess { /* 判断条件并选择处理分支。 */
		t.Fatalf("business retry claim failed: shouldProcess=%v err=%v", shouldProcess, claimErr) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if got := telemetryRows.Load(); got != 1 { /* 判断条件并选择处理分支。 */
		t.Fatalf("business retry duplicated telemetry: rows=%d want=1", got) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func newClickHouseTestServer(t *testing.T, handle func(string, http.ResponseWriter) bool) *httptest.Server { /* 定义 newClickHouseTestServer 函数。 */
	t.Helper()                                                                                /* 执行当前语句并推进处理流程。 */
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 返回当前处理结果。 */
		query := r.URL.Query().Get("query") /* 更新 query 的值。 */
		if handle(query, w) {               /* 判断条件并选择处理分支。 */
			return /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if strings.Contains(query, "SELECT count() AS total FROM iot_telemetry") { /* 判断条件并选择处理分支。 */
			_ = json.NewEncoder(w).Encode(map[string]int{"total": 0}) /* 更新 _ 的值。 */
			return                                                    /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		w.WriteHeader(http.StatusOK) /* 执行当前语句并推进处理流程。 */
	})) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func testTelemetryMessage() model.StandardMessage { /* 定义 testTelemetryMessage 函数。 */
	return model.StandardMessage{ /* 返回当前处理结果。 */
		TenantID: "tenant-test", MessageID: "message-test", DeviceID: "device-test", /* 执行当前语句并推进处理流程。 */
		MessageType: model.PropertyReport, Properties: map[string]any{"temperature": 25.0}, /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
