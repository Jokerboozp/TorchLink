package durablequeue /* 声明 durablequeue 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/json"               /* 执行当前语句并推进处理流程。 */
	"errors"                      /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"os"                          /* 执行当前语句并推进处理流程。 */
	"path/filepath"               /* 执行当前语句并推进处理流程。 */
	"testing"                     /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestQueueRestartRetryAndCapacity(t *testing.T) { /* 定义 TestQueueRestartRetryAndCapacity 函数。 */
	root := t.TempDir()                 /* 更新 root 的值。 */
	q, err := OpenQueue(root, 1<<20, 1) /* 更新 err 的值。 */
	if err != nil {                     /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if other, err := OpenQueue(root, 1<<20, 1); err == nil { /* 判断条件并选择处理分支。 */
		other.Close()                           /* 执行当前语句并推进处理流程。 */
		t.Fatal("two clients opened one queue") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	raw := model.RawMessage{TenantID: "tenant", MessageID: "first", Payload: json.RawMessage(`{"x":1}`)} /* 更新 raw 的值。 */
	if err = q.Put(raw); err != nil {                                                                    /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = q.Put(raw); err != nil || q.Depth() != 1 { /* 判断条件并选择处理分支。 */
		t.Fatal("duplicate queued", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	conflict := raw                                                   /* 更新 conflict 的值。 */
	conflict.Payload = json.RawMessage(`{"x":2}`)                     /* 更新 conflict.Payload 的值。 */
	if err = q.Put(conflict); !errors.Is(err, model.ErrRawConflict) { /* 判断条件并选择处理分支。 */
		t.Fatal("conflicting raw overwritten", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	next := raw                                           /* 更新 next 的值。 */
	next.MessageID = "second"                             /* 更新 next.MessageID 的值。 */
	if err = q.Put(next); !errors.Is(err, ErrQueueFull) { /* 判断条件并选择处理分支。 */
		t.Fatal("capacity silently exceeded", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = q.Close(); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	recovered, err := OpenQueue(root, 1<<20, 1) /* 更新 err 的值。 */
	if err != nil {                             /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer recovered.Close()                                    /* 安排函数结束时执行清理。 */
	saved, ok, err := recovered.Next()                         /* 更新 err 的值。 */
	if err != nil || !ok || saved.MessageID != raw.MessageID { /* 判断条件并选择处理分支。 */
		t.Fatal("restart lost pending message", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	// Simulated response loss leaves the same message available for retry.
	repeated, ok, err := recovered.Next()                           /* 更新 err 的值。 */
	if err != nil || !ok || repeated.MessageID != saved.MessageID { /* 判断条件并选择处理分支。 */
		t.Fatal("unacknowledged data removed", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = recovered.Ack(saved); err != nil || recovered.Depth() != 0 { /* 判断条件并选择处理分支。 */
		t.Fatal("acknowledged data retained", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = recovered.Put(next); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal("capacity not reclaimed", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestRejectedDataIsRetainedWithoutBlockingFollowingMessages(t *testing.T) { /* 定义 TestRejectedDataIsRetainedWithoutBlockingFollowingMessages 函数。 */
	q, err := OpenQueue(t.TempDir(), 1<<20, 2) /* 更新 err 的值。 */
	if err != nil {                            /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer q.Close()                                                                                     /* 安排函数结束时执行清理。 */
	first := model.RawMessage{TenantID: "tenant", MessageID: "rejected", Payload: json.RawMessage(`1`)} /* 更新 first 的值。 */
	next := model.RawMessage{TenantID: "tenant", MessageID: "next", Payload: json.RawMessage(`2`)}      /* 更新 next 的值。 */
	if err = q.Put(first); err != nil {                                                                 /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = q.Reject(first); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if q.Depth() != 0 || q.Rejected() != 1 { /* 判断条件并选择处理分支。 */
		t.Fatal("rejected entry lost or remained active") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = q.Put(first); !errors.Is(err, ErrQuarantined) { /* 判断条件并选择处理分支。 */
		t.Fatal("rejected duplicate silently accepted", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = q.Put(next); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	raw, ok, err := q.Next()                                  /* 更新 err 的值。 */
	if err != nil || !ok || raw.MessageID != next.MessageID { /* 判断条件并选择处理分支。 */
		t.Fatal("rejected entry blocked remaining queue", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = q.RetryRejected(); err != nil || q.Depth() != 2 || q.Rejected() != 0 { /* 判断条件并选择处理分支。 */
		t.Fatal("explicit retry lost entries", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestCorruptQueueEntryDoesNotBlockValidData(t *testing.T) { /* 定义 TestCorruptQueueEntryDoesNotBlockValidData 函数。 */
	for _, body := range []string{`{broken`, `{"tenantId":"foreign","messageId":"other"}`} { /* 循环处理当前数据。 */
		t.Run(body, func(t *testing.T) { /* 执行当前语句并推进处理流程。 */
			root := t.TempDir()                 /* 更新 root 的值。 */
			q, err := OpenQueue(root, 1<<20, 2) /* 更新 err 的值。 */
			if err != nil {                     /* 判断条件并选择处理分支。 */
				t.Fatal(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			defer q.Close()                                                                                /* 安排函数结束时执行清理。 */
			raw := model.RawMessage{TenantID: "tenant", MessageID: "first", Payload: json.RawMessage(`1`)} /* 更新 raw 的值。 */
			if err := q.Put(raw); err != nil {                                                             /* 判断条件并选择处理分支。 */
				t.Fatal(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			path := filepath.Join(root, queueName(raw))                    /* 更新 path 的值。 */
			if err := os.WriteFile(path, []byte(body), 0600); err != nil { /* 判断条件并选择处理分支。 */
				t.Fatal(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if _, ok, err := q.Next(); err == nil || ok { /* 判断条件并选择处理分支。 */
				t.Fatal("corruption not reported") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			data, err := os.ReadFile(path + ".corrupt") /* 更新 err 的值。 */
			if err != nil || string(data) != body {     /* 判断条件并选择处理分支。 */
				t.Fatal("original corruption lost", err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if q.Depth() != 0 || q.Corrupt() != 1 { /* 判断条件并选择处理分支。 */
				t.Fatal("quarantine not observable") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if err := q.RetryRejected(); err != nil || q.Depth() != 0 { /* 判断条件并选择处理分支。 */
				t.Fatal("corrupt data requeued", err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if err := q.Put(raw); !errors.Is(err, ErrQuarantined) { /* 判断条件并选择处理分支。 */
				t.Fatal("corrupt identity overwritten", err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			good := raw                         /* 更新 good 的值。 */
			good.MessageID = "second"           /* 更新 good.MessageID 的值。 */
			if err := q.Put(good); err != nil { /* 判断条件并选择处理分支。 */
				t.Fatal(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			third := raw                                            /* 更新 third 的值。 */
			third.MessageID = "third"                               /* 更新 third.MessageID 的值。 */
			if err := q.Put(third); !errors.Is(err, ErrQueueFull) { /* 判断条件并选择处理分支。 */
				t.Fatal("quarantine not counted in limit", err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			q.Close()                          /* 执行当前语句并推进处理流程。 */
			q, err = OpenQueue(root, 1<<20, 2) /* 更新 err 的值。 */
			if err != nil {                    /* 判断条件并选择处理分支。 */
				t.Fatal(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			defer q.Close()                                              /* 安排函数结束时执行清理。 */
			actual, ok, err := q.Next()                                  /* 更新 err 的值。 */
			if err != nil || !ok || actual.MessageID != good.MessageID { /* 判断条件并选择处理分支。 */
				t.Fatal("valid data blocked after restart", err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if err := q.Ack(actual); err != nil { /* 判断条件并选择处理分支。 */
				t.Fatal(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if q.Corrupt() != 1 { /* 判断条件并选择处理分支。 */
				t.Fatal("ack removed corrupt evidence") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		}) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
