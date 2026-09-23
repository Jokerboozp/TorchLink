package knowledge /* 声明 knowledge 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"  /* 执行当前语句并推进处理流程。 */
	"net/http" /* 执行当前语句并推进处理流程。 */
	"os"       /* 执行当前语句并推进处理流程。 */
	"strings"  /* 执行当前语句并推进处理流程。 */
	"testing"  /* 执行当前语句并推进处理流程。 */
	"time"     /* 执行当前语句并推进处理流程。 */

	"github.com/google/uuid"      /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestWeaviateLiveIndexAndScopeIsolation(t *testing.T) { /* 定义 TestWeaviateLiveIndexAndScopeIsolation 函数。 */
	endpoint := strings.TrimRight(os.Getenv("IOT_TEST_WEAVIATE_URL"), "/") /* 更新 endpoint 的值。 */
	if endpoint == "" {                                                    /* 判断条件并选择处理分支。 */
		t.Skip("IOT_TEST_WEAVIATE_URL not configured") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)                                                                                  /* 更新 cancel 的值。 */
	defer cancel()                                                                                                                                            /* 安排函数结束时执行清理。 */
	id := "audit-" + uuid.NewString()                                                                                                                         /* 更新 id 的值。 */
	in := ports.KnowledgeIndexInput{TenantID: id, WorkflowID: "audit-workflow", DocumentID: id, ChunkID: id, Content: []byte("联调专用资料：巡检柜第六路输入状态为一，模拟测试已通过。")} /* 更新 in 的值。 */
	objectID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(strings.Join([]string{in.TenantID, in.WorkflowID, in.ChunkID}, "\x00")))                               /* 更新 objectID 的值。 */
	t.Cleanup(func() {                                                                                                                                        /* 执行当前语句并推进处理流程。 */
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)                                                   /* 更新 cancel 的值。 */
		defer cancel()                                                                                                                /* 安排函数结束时执行清理。 */
		req, _ := http.NewRequestWithContext(cleanup, http.MethodDelete, endpoint+"/v1/objects/IotKnowledge/"+objectID.String(), nil) /* 更新 _ 的值。 */
		response, err := http.DefaultClient.Do(req)                                                                                   /* 更新 err 的值。 */
		if err != nil {                                                                                                               /* 判断条件并选择处理分支。 */
			t.Error("temporary knowledge object cleanup failed") /* 验证实际结果符合预期。 */
			return                                               /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		defer response.Body.Close()                                   /* 安排函数结束时执行清理。 */
		if response.StatusCode != 204 && response.StatusCode != 404 { /* 判断条件并选择处理分支。 */
			t.Errorf("temporary knowledge cleanup HTTP %d", response.StatusCode) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	}) /* 结束当前表达式或代码块。 */
	w := NewWeaviate(endpoint)                        /* 更新 w 的值。 */
	if err := w.IndexKnowledge(ctx, in); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal("live knowledge indexing failed") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	chunks, err := w.ListKnowledgeChunks(ctx, id, id)                              /* 更新 err 的值。 */
	if err != nil || len(chunks) != 1 || chunks[0].Content != string(in.Content) { /* 判断条件并选择处理分支。 */
		t.Fatal("indexed chunk readback mismatch") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	query := ports.KnowledgeSearchRequest{TenantID: id, WorkflowID: in.WorkflowID, Question: "第六路输入状态", Limit: 3} /* 更新 query 的值。 */
	hits, err := w.SearchKnowledge(ctx, query)                                                                    /* 更新 err 的值。 */
	if err != nil || len(hits) != 1 || hits[0].DocumentID != id {                                                 /* 判断条件并选择处理分支。 */
		t.Fatal("live scoped vector retrieval failed") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	query.WorkflowID = "unrelated-workflow"   /* 更新 query.WorkflowID 的值。 */
	hits, err = w.SearchKnowledge(ctx, query) /* 更新 err 的值。 */
	if err != nil || len(hits) != 0 {         /* 判断条件并选择处理分支。 */
		t.Fatal("knowledge leaked across workflow scope") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	query.WorkflowID = in.WorkflowID          /* 更新 query.WorkflowID 的值。 */
	query.TenantID = id + "-other"            /* 更新 query.TenantID 的值。 */
	hits, err = w.SearchKnowledge(ctx, query) /* 更新 err 的值。 */
	if err != nil || len(hits) != 0 {         /* 判断条件并选择处理分支。 */
		t.Fatal("knowledge leaked across tenant scope") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
