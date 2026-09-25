package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"  /* 执行当前语句并推进处理流程。 */
	"io"       /* 执行当前语句并推进处理流程。 */
	"log/slog" /* 执行当前语句并推进处理流程。 */
	"testing"  /* 执行当前语句并推进处理流程。 */
	"time"     /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/adapters/knowledge"
	"iot-platform/internal/adapters/local"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/aitest"
	"iot-platform/internal/config"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"    /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"   /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports"
) /* 结束当前表达式或代码块。 */

func TestAIAnalysisJobReportsProgressAndPersistsResult(t *testing.T) { /* 定义 TestAIAnalysisJobReportsProgressAndPersistsResult 函数。 */
	repo := memory.NewRepository()                /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir()) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	release := make(chan struct{})                                                                                                                                  /* 更新 release 的值。 */
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 engine 的值。 */
	engine.AIWorkflows, engine.HarnessTokens = &aitest.Workflows{Answer: func(ports.AIWorkflowRequest) (string, error) { <-release; return testAnalysisAnswer, nil }}, aitest.Tokens()
	api := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))                                                                                                                               /* 更新 api 的值。 */
	_, _, err = repo.UpsertAlarm(context.Background(), model.Alarm{ID: "alarm-progress", TenantID: "tenant-a", DeviceID: "device-a", AlarmType: "SMOKE_DETECTED", AlarmLevel: "HIGH", Status: "ACTIVE", LastTriggeredAt: time.Now().UnixMilli()}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	job := api.startAIAnalysisJob("tenant-a", "alarm-progress", "operator", model.AIAnalysisScopeNone, testRunIdentity("operator")) /* 更新 job 的值。 */
	if job.Status != "running" || job.Progress != 8 || job.EstimatedRemainingMs <= 0 {                                              /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected initial progress: %#v", aiAnalysisJobView(job)) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	close(release)                              /* 执行当前语句并推进处理流程。 */
	deadline := time.Now().Add(2 * time.Second) /* 更新 deadline 的值。 */
	for time.Now().Before(deadline) {           /* 循环处理当前数据。 */
		api.aiAnalysisMu.RLock()                                                                                                /* 执行当前语句并推进处理流程。 */
		current := cloneAIAnalysisJob(api.aiAnalysisJobs[alarmJobKey("tenant-a", "alarm-progress", model.AIAnalysisScopeNone)]) /* 更新 current 的值。 */
		api.aiAnalysisMu.RUnlock()                                                                                              /* 执行当前语句并推进处理流程。 */
		if current != nil && current.Status != "running" {                                                                      /* 判断条件并选择处理分支。 */
			if current.Status != "succeeded" || current.Progress != 100 || current.Analysis.Summary != "研判完成" { /* 判断条件并选择处理分支。 */
				t.Fatalf("unexpected completed progress: %#v", aiAnalysisJobView(current)) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if saved, getErr := repo.GetAIAnalysis(context.Background(), "tenant-a", "alarm-progress", model.AIAnalysisScopeNone); getErr != nil || saved.Summary != "研判完成" { /* 判断条件并选择处理分支。 */
				t.Fatalf("analysis was not persisted: %#v err=%v", saved, getErr) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			return /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		time.Sleep(10 * time.Millisecond) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	t.Fatal("AI analysis job did not complete") /* 验证实际结果符合预期。 */
} /* 结束当前表达式或代码块。 */

func TestAIAnalysisProgressCanBeLoadedWithoutJobID(t *testing.T) { /* 定义 TestAIAnalysisProgressCanBeLoadedWithoutJobID 函数。 */
	repo := memory.NewRepository()                /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir()) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	release := make(chan struct{})                                                                                                                                  /* 更新 release 的值。 */
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 engine 的值。 */
	engine.KB = knowledge.NewLocal()
	engine.AIWorkflows, engine.HarnessTokens = &aitest.Workflows{Answer: func(ports.AIWorkflowRequest) (string, error) { <-release; return testAnalysisAnswer, nil }}, aitest.Tokens()
	api := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))                                                                                                                                      /* 更新 api 的值。 */
	_, _, err = repo.UpsertAlarm(context.Background(), model.Alarm{ID: "alarm-progress-resume", TenantID: "tenant-a", DeviceID: "device-a", AlarmType: "SMOKE_DETECTED", AlarmLevel: "HIGH", Status: "ACTIVE", LastTriggeredAt: time.Now().UnixMilli()}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                                      /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	job := api.startAIAnalysisJob("tenant-a", "alarm-progress-resume", "operator", model.AlarmAnalysisWorkflowID, testRunIdentity("operator")) // 未托管的测试令牌拥有知识库权限，进度按同一知识范围查询。 /* 更新 job 的值。 */
	server := newTestHTTPServer(api)                                                                                                           /* 更新 server 的值。 */
	defer server.Close()                                                                                                                       /* 安排函数结束时执行清理。 */
	defer close(release)                                                                                                                       /* 安排函数结束时执行清理。 */
	viewerToken, err := api.auth.Issue("viewer", "tenant-a", "viewer", nil, time.Hour)                                                         /* 检查错误并决定后续处理。 */
	if err != nil {                                                                                                                            /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	progress := requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/ai/alarm-analysis/alarm-progress-resume/progress", viewerToken, nil, 200) /* 更新 progress 的值。 */
	if progress["jobId"] != job.ID || progress["status"] != "running" {                                                                              /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected resumable progress: %#v", progress) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
