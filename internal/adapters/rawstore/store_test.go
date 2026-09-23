package rawstore /* 声明 rawstore 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context" /* 执行当前语句并推进处理流程。 */
	"fmt"     /* 执行当前语句并推进处理流程。 */
	"testing" /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type recordingDatabase struct { /* 定义 recordingDatabase 类型。 */
	name     string                      /* 执行当前语句并推进处理流程。 */
	messages map[string]model.RawMessage /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func newRecordingDatabase(name string) *recordingDatabase { /* 定义 newRecordingDatabase 函数。 */
	return &recordingDatabase{name: name, messages: map[string]model.RawMessage{}} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (d *recordingDatabase) SaveRawMessage(_ context.Context, value model.RawMessage) error { /* 定义 SaveRawMessage 函数。 */
	d.messages[value.MessageID] = value /* 更新 d.messages[value.MessageID] 的值。 */
	return nil                          /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (d *recordingDatabase) GetRawMessage(_ context.Context, _, messageID string) (model.RawMessage, error) { /* 定义 GetRawMessage 函数。 */
	value, ok := d.messages[messageID] /* 更新 ok 的值。 */
	if !ok {                           /* 判断条件并选择处理分支。 */
		return model.RawMessage{}, fmt.Errorf("%s: raw message not found", d.name) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return value, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

type recordingResolver struct { /* 定义 recordingResolver 类型。 */
	states map[string]model.DeviceState /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (r *recordingResolver) GetDeviceState(_ context.Context, _, deviceID string) (model.DeviceState, error) { /* 定义 GetDeviceState 函数。 */
	state, ok := r.states[deviceID] /* 更新 ok 的值。 */
	if !ok {                        /* 判断条件并选择处理分支。 */
		return model.DeviceState{}, fmt.Errorf("device state not found") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return state, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func rawMessage(id, device string, receivedAt int64) model.RawMessage { /* 定义 rawMessage 函数。 */
	return model.RawMessage{ /* 返回当前处理结果。 */
		MessageID:     id,                    /* 执行当前语句并推进处理流程。 */
		TenantID:      "tenant-1",            /* 执行当前语句并推进处理流程。 */
		ProductID:     "product-1",           /* 执行当前语句并推进处理流程。 */
		DeviceID:      device,                /* 执行当前语句并推进处理流程。 */
		ReceivedAt:    receivedAt,            /* 执行当前语句并推进处理流程。 */
		PayloadFormat: "json",                /* 执行当前语句并推进处理流程。 */
		Payload:       []byte(`{"value":1}`), /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestStoreRoutesByConfiguredAndObservedFrequency(t *testing.T) { /* 定义 TestStoreRoutesByConfiguredAndObservedFrequency 函数。 */
	postgres := newRecordingDatabase(PostgreSQLBucket)                   /* 更新 postgres 的值。 */
	clickhouse := newRecordingDatabase(ClickHouseBucket)                 /* 更新 clickhouse 的值。 */
	resolver := &recordingResolver{states: map[string]model.DeviceState{ /* 更新 resolver 的值。 */
		"slow-device": {ReportIntervalSec: 300, LastSeenAt: 1_000_000}, /* 执行当前语句并推进处理流程。 */
		"fast-device": {ReportIntervalSec: 10, LastSeenAt: 1_000_000},  /* 执行当前语句并推进处理流程。 */
	}} /* 结束当前表达式或代码块。 */
	store := New(Config{ /* 更新 store 的值。 */
		PostgreSQL:               postgres,   /* 执行当前语句并推进处理流程。 */
		ClickHouse:               clickhouse, /* 执行当前语句并推进处理流程。 */
		Resolver:                 resolver,   /* 执行当前语句并推进处理流程。 */
		HighFrequencyIntervalSec: 60,         /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */

	slowIndex, err := store.PutRaw(context.Background(), rawMessage("slow-1", "slow-device", 1_100_000)) /* 更新 err 的值。 */
	if err != nil {                                                                                      /* 判断条件并选择处理分支。 */
		t.Fatalf("store slow message: %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if slowIndex.ObjectBucket != PostgreSQLBucket { /* 判断条件并选择处理分支。 */
		t.Fatalf("slow message bucket = %q, want %q", slowIndex.ObjectBucket, PostgreSQLBucket) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	fastIndex, err := store.PutRaw(context.Background(), rawMessage("fast-1", "fast-device", 1_100_000)) /* 更新 err 的值。 */
	if err != nil {                                                                                      /* 判断条件并选择处理分支。 */
		t.Fatalf("store configured fast message: %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if fastIndex.ObjectBucket != ClickHouseBucket { /* 判断条件并选择处理分支。 */
		t.Fatalf("configured fast message bucket = %q, want %q", fastIndex.ObjectBucket, ClickHouseBucket) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	firstObserved, err := store.PutRaw(context.Background(), rawMessage("observed-1", "unknown-device", 2_000_000)) /* 更新 err 的值。 */
	if err != nil {                                                                                                 /* 判断条件并选择处理分支。 */
		t.Fatalf("store first observed message: %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if firstObserved.ObjectBucket != PostgreSQLBucket { /* 判断条件并选择处理分支。 */
		t.Fatalf("first observed message bucket = %q, want %q", firstObserved.ObjectBucket, PostgreSQLBucket) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	secondObserved, err := store.PutRaw(context.Background(), rawMessage("observed-2", "unknown-device", 2_001_000)) /* 更新 err 的值。 */
	if err != nil {                                                                                                  /* 判断条件并选择处理分支。 */
		t.Fatalf("store second observed message: %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if secondObserved.ObjectBucket != ClickHouseBucket { /* 判断条件并选择处理分支。 */
		t.Fatalf("second observed message bucket = %q, want %q", secondObserved.ObjectBucket, ClickHouseBucket) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	if _, err := store.GetRaw(context.Background(), secondObserved); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatalf("read clickhouse-tier raw message: %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if len(postgres.messages) != 2 { /* 判断条件并选择处理分支。 */
		t.Fatalf("postgres stored %d messages, want 2", len(postgres.messages)) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if len(clickhouse.messages) != 2 { /* 判断条件并选择处理分支。 */
		t.Fatalf("clickhouse stored %d messages, want 2", len(clickhouse.messages)) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestStoreFallsBackToAvailableDatabase(t *testing.T) { /* 定义 TestStoreFallsBackToAvailableDatabase 函数。 */
	clickhouse := newRecordingDatabase(ClickHouseBucket)                       /* 更新 clickhouse 的值。 */
	store := New(Config{ClickHouse: clickhouse, HighFrequencyIntervalSec: 60}) /* 更新 store 的值。 */

	index, err := store.PutRaw(context.Background(), rawMessage("fallback-1", "device-1", 3_000_000)) /* 更新 err 的值。 */
	if err != nil {                                                                                   /* 判断条件并选择处理分支。 */
		t.Fatalf("store with one database: %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if index.ObjectBucket != ClickHouseBucket { /* 判断条件并选择处理分支。 */
		t.Fatalf("fallback bucket = %q, want %q", index.ObjectBucket, ClickHouseBucket) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestStoreRejectsMissingDatabases(t *testing.T) { /* 定义 TestStoreRejectsMissingDatabases 函数。 */
	store := New(Config{})                                                                                        /* 更新 store 的值。 */
	if _, err := store.PutRaw(context.Background(), rawMessage("missing-1", "device-1", 4_000_000)); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("expected missing database error") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
