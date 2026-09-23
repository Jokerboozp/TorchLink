package backup /* 声明 backup 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"             /* 执行当前语句并推进处理流程。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"encoding/json"     /* 执行当前语句并推进处理流程。 */
	"fmt"               /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"strings"           /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */
	"time"              /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestMessageQueriesFullAndDaily(t *testing.T) { /* 定义 TestMessageQueriesFullAndDaily 函数。 */
	loc := time.FixedZone("Asia/Shanghai", 8*3600)  /* 更新 loc 的值。 */
	start := time.Date(2026, 9, 7, 0, 0, 0, 0, loc) /* 更新 start 的值。 */
	for _, parsed := range []bool{false, true} {    /* 循环处理当前数据。 */
		pg, args, ch := messageQueries(time.Time{}, time.Now(), parsed)                       /* 更新 ch 的值。 */
		if strings.Contains(pg, "WHERE") || strings.Contains(ch, "WHERE") || len(args) != 0 { /* 判断条件并选择处理分支。 */
			t.Fatal("full backup must include all saved device timestamps") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		pg, args, ch = messageQueries(start, start.AddDate(0, 0, 1), parsed)                                 /* 更新 ch 的值。 */
		if len(args) != 2 || args[0] != start.UnixMilli() || args[1] != start.AddDate(0, 0, 1).UnixMilli() { /* 判断条件并选择处理分支。 */
			t.Fatalf("incorrect daily window: %v", args) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if !strings.Contains(pg, " >= $1") || !strings.Contains(pg, " < $2") || !strings.Contains(ch, fmt.Sprint(start.UnixMilli())) { /* 判断条件并选择处理分支。 */
			t.Fatal("daily window must be half open and preserve timezone") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if parsed && (!strings.Contains(pg, "standard_message") || !strings.Contains(pg, "processed_at") || !strings.Contains(ch, "iot_telemetry")) { /* 判断条件并选择处理分支。 */
			t.Fatal("parsed data source missing") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if !parsed && (!strings.Contains(pg, "raw_message_log") || !strings.Contains(ch, "iot_raw_message")) { /* 判断条件并选择处理分支。 */
			t.Fatal("raw data source missing") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestClickHouseExportPreservesPayloads(t *testing.T) { /* 定义 TestClickHouseExportPreservesPayloads 函数。 */
	for _, tc := range []struct { /* 循环处理当前数据。 */
		name, body string /* 执行当前语句并推进处理流程。 */
		unwrap     bool   /* 执行当前语句并推进处理流程。 */
	}{ /* 结束当前表达式或代码块。 */
		{"raw", `{"body":"{\"messageId\":\"raw1\",\"payload\":\"0102\"}"}`, true},     /* 执行当前语句并推进处理流程。 */
		{"parsed", `{"message_id":"parsed1","properties":{"temperature":42}}`, false}, /* 执行当前语句并推进处理流程。 */
	} { /* 结束当前表达式或代码块。 */
		t.Run(tc.name, func(t *testing.T) { /* 执行当前语句并推进处理流程。 */
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 server 的值。 */
				fmt.Fprint(w, tc.body) /* 执行当前语句并推进处理流程。 */
			})) /* 结束当前表达式或代码块。 */
			defer server.Close()                                                                                      /* 安排函数结束时执行清理。 */
			s := &Service{cfg: Config{ClickHouseURL: server.URL}}                                                     /* 更新 s 的值。 */
			var output bytes.Buffer                                                                                   /* 声明 output。 */
			count, err := s.exportClickHouseRows(context.Background(), "SELECT", tc.unwrap, json.NewEncoder(&output)) /* 更新 err 的值。 */
			if err != nil || count != 1 {                                                                             /* 判断条件并选择处理分支。 */
				t.Fatalf("count=%d err=%v", count, err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			var record rawLogRecord                                        /* 声明 record。 */
			if err = json.Unmarshal(output.Bytes(), &record); err != nil { /* 判断条件并选择处理分支。 */
				t.Fatal(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if record.Storage != "clickhouse" || !json.Valid(record.Message) { /* 判断条件并选择处理分支。 */
				t.Fatal("invalid record", output.String()) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if tc.unwrap && !bytes.Contains(record.Message, []byte("0102")) { /* 判断条件并选择处理分支。 */
				t.Fatal("lost raw payload") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if !tc.unwrap && !bytes.Contains(record.Message, []byte("temperature")) { /* 判断条件并选择处理分支。 */
				t.Fatal("lost parsed properties") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		}) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestClickHouseExportFailsOnCorruptData(t *testing.T) { /* 定义 TestClickHouseExportFailsOnCorruptData 函数。 */
	for _, body := range []string{`{"body":"not json"}`, `{"body":`} { /* 循环处理当前数据。 */
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })) /* 更新 server 的值。 */
		s := &Service{cfg: Config{ClickHouseURL: server.URL}}                                                                /* 更新 s 的值。 */
		var output bytes.Buffer                                                                                              /* 声明 output。 */
		_, err := s.exportClickHouseRows(context.Background(), "SELECT", true, json.NewEncoder(&output))                     /* 更新 err 的值。 */
		server.Close()                                                                                                       /* 执行当前语句并推进处理流程。 */
		if err == nil {                                                                                                      /* 判断条件并选择处理分支。 */
			t.Fatal("corrupt backup reported success") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
