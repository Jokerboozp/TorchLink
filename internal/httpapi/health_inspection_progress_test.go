package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"  /* 执行当前语句并推进处理流程。 */
	"io"       /* 执行当前语句并推进处理流程。 */
	"log/slog" /* 执行当前语句并推进处理流程。 */
	"net/http" /* 执行当前语句并推进处理流程。 */
	"sync"     /* 执行当前语句并推进处理流程。 */
	"testing"  /* 执行当前语句并推进处理流程。 */
	"time"     /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/adapters/local"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics"         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"          /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type progressInspectionAI struct { /* 定义 progressInspectionAI 类型。 */
	release <-chan struct{} /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (p progressInspectionAI) AnalyzeAlarm(context.Context, model.Alarm, []map[string]any, []string) (model.AIAnalysis, error) { /* 定义 AnalyzeAlarm 函数。 */
	return model.AIAnalysis{}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (p progressInspectionAI) Chat(context.Context, string, string) (string, error) { /* 定义 Chat 函数。 */
	<-p.release           /* 执行当前语句并推进处理流程。 */
	return "巡检建议已生成", nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (progressInspectionAI) RuleDraft(context.Context, string, string) (model.AlarmRule, error) { /* 定义 RuleDraft 函数。 */
	return model.AlarmRule{}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (progressInspectionAI) Health(context.Context) error { return nil } /* 定义 Health 函数。 */

func TestHealthInspectionJobCanBeLoadedWithoutJobID(t *testing.T) { /* 定义 TestHealthInspectionJobCanBeLoadedWithoutJobID 函数。 */
	repo := memory.NewRepository()                /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir()) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	release := make(chan struct{})                                                                                                                                  /* 更新 release 的值。 */
	var releaseOnce sync.Once                                                                                                                                       /* 声明 releaseOnce。 */
	releaseJob := func() { releaseOnce.Do(func() { close(release) }) }                                                                                              /* 更新 releaseJob 的值。 */
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 engine 的值。 */
	engine.AI = progressInspectionAI{release: release}                                                                                                              /* 更新 engine.AI 的值。 */
	api := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))                                                 /* 更新 api 的值。 */
	server := newTestHTTPServer(api)                                                                                                                                /* 更新 server 的值。 */
	defer server.Close()                                                                                                                                            /* 安排函数结束时执行清理。 */
	defer releaseJob()                                                                                                                                              /* 安排函数结束时执行清理。 */
	token, err := api.auth.Issue("viewer", "tenant-a", "viewer", nil, time.Hour)                                                                                    /* 检查错误并决定后续处理。 */
	if err != nil {                                                                                                                                                 /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	started := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/health-inspection/run", token, map[string]any{}, http.StatusAccepted) /* 更新 started 的值。 */
	if started["status"] != "running" || started["progress"] != float64(8) {                                                                                 /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected initial inspection progress: %#v", started) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	progress := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/health-inspection/progress", token, nil, http.StatusOK) /* 更新 progress 的值。 */
	if progress["jobId"] != started["jobId"] || progress["status"] != "running" {                                                              /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected resumable inspection progress: %#v", progress) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	releaseJob()                                /* 执行当前语句并推进处理流程。 */
	deadline := time.Now().Add(2 * time.Second) /* 更新 deadline 的值。 */
	for time.Now().Before(deadline) {           /* 循环处理当前数据。 */
		progress = requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/health-inspection/progress", token, nil, http.StatusOK) /* 更新 progress 的值。 */
		if progress["status"] == "succeeded" {                                                                                                    /* 判断条件并选择处理分支。 */
			if progress["progress"] != float64(100) { /* 判断条件并选择处理分支。 */
				t.Fatalf("unexpected completed inspection progress: %#v", progress) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if progress["report"] == nil { /* 判断条件并选择处理分支。 */
				t.Fatalf("completed inspection did not include report: %#v", progress) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			return /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		time.Sleep(10 * time.Millisecond) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	t.Fatal("health inspection job did not complete") /* 验证实际结果符合预期。 */
} /* 结束当前表达式或代码块。 */

// Inspection progress and results live in the repository, so a restarted
// process or another replica sees the same job and the running-job guard.
func TestHealthInspectionJobIsSharedAcrossServerInstances(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseJob := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseJob()
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.AI = progressInspectionAI{release: release}
	first := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	second := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	firstServer, secondServer := newTestHTTPServer(first), newTestHTTPServer(second)
	defer firstServer.Close()
	defer secondServer.Close()
	token, err := first.auth.Issue("viewer", "tenant-a", "viewer", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	started := requestJSON(t, firstServer.Client(), http.MethodPost, firstServer.URL+"/api/v1/ai/health-inspection/run", token, map[string]any{}, http.StatusAccepted)
	again := requestJSON(t, secondServer.Client(), http.MethodPost, secondServer.URL+"/api/v1/ai/health-inspection/run", token, map[string]any{}, http.StatusAccepted)
	if again["jobId"] != started["jobId"] {
		t.Fatalf("second instance started a duplicate inspection: first=%v second=%v", started["jobId"], again["jobId"])
	}
	releaseJob()
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		progress := requestJSON(t, secondServer.Client(), http.MethodGet, secondServer.URL+"/api/v1/ai/health-inspection/progress/"+started["jobId"].(string), token, nil, http.StatusOK)
		if progress["status"] == "succeeded" && progress["report"] != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("other instance did not observe the finished job: %v", progress)
		}
	}
	if _, ok := second.recentHealthInspection(context.Background(), "tenant-a"); !ok {
		t.Fatal("PDF download on another instance cannot reuse the finished report")
	}
}

func TestHealthInspectionStaleRunningJobIsMarkedInterrupted(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	api := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := newTestHTTPServer(api)
	defer server.Close()
	token, err := api.auth.Issue("viewer", "tenant-a", "viewer", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// A job left running by a process that stopped heartbeating.
	old := time.Now().Add(-2 * healthInspectionStaleAfter).UnixMilli()
	if _, err = repo.CreateHealthInspectionJob(context.Background(), model.HealthInspectionJob{ID: "inspection_job_orphan", TenantID: "tenant-a", Status: "running", Progress: 40, StartedAt: old, UpdatedAt: old}); err != nil {
		t.Fatal(err)
	}
	progress := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/health-inspection/progress", token, nil, http.StatusOK)
	if progress["jobId"] != "inspection_job_orphan" || progress["status"] != "failed" || progress["error"] == nil {
		t.Fatalf("orphaned job must be reported as interrupted: %v", progress)
	}
	started := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/health-inspection/run", token, map[string]any{}, http.StatusAccepted)
	if started["jobId"] == "inspection_job_orphan" || started["status"] != "running" {
		t.Fatalf("a new inspection must start after the interrupted one: %v", started)
	}
}
