package core /* 声明 core 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"archive/zip"       /* 执行当前语句并推进处理流程。 */
	"bytes"             /* 执行当前语句并推进处理流程。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"encoding/json"     /* 执行当前语句并推进处理流程。 */
	"io"                /* 执行当前语句并推进处理流程。 */
	"log/slog"          /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"strings"           /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */
	"time"              /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/adapters/local"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports"           /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestGengineExpressionAndSafety(t *testing.T) { /* 定义 TestGengineExpressionAndSafety 函数。 */
	message := model.StandardMessage{TenantID: "t1", ProductID: "p1", Properties: map[string]any{"temperature": 88.5, "smoke": true}}                    /* 更新 message 的值。 */
	rule := model.AlarmRule{TenantID: "t1", ProductID: "p1", Enabled: true, Expression: `Properties["temperature"] > 80 && Properties["smoke"] == true`} /* 更新 rule 的值。 */
	matched, err := EvaluateGengineExpression(rule.Expression, message)                                                                                  /* 更新 err 的值。 */
	if err != nil || !matched {                                                                                                                          /* 判断条件并选择处理分支。 */
		t.Fatalf("expected controlled Gengine expression to match: matched=%v err=%v", matched, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err := ValidateGengineExpression(`system("rm")`); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("expected unsafe expression to be rejected") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestRuleDraftValidatesThingModelAndConflicts(t *testing.T) { /* 定义 TestRuleDraftValidatesThingModelAndConflicts 函数。 */
	ctx := context.Background()                                                                                                                                /* 更新 ctx 的值。 */
	repo := memory.NewRepository()                                                                                                                             /* 更新 repo 的值。 */
	archive, _ := local.NewArchive(t.TempDir())                                                                                                                /* 更新 _ 的值。 */
	engine := New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 engine 的值。 */
	product := model.Product{ID: "p1", TenantID: "t1", Metadata: map[string]any{"properties": []any{map[string]any{"id": "temperature"}}}}                     /* 更新 product 的值。 */
	if err := repo.SaveProduct(ctx, product); err != nil {                                                                                                     /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	base := model.AlarmRule{ID: "r1", TenantID: "t1", ProductID: "p1", Name: "high temperature", AlarmType: "FIRE_RISK", Level: "HIGH", Match: "all", Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 80}}} /* 更新 base 的值。 */
	if err := repo.SaveRule(ctx, base); err != nil {                                                                                                                                                                                       /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	draft := base                                             /* 更新 draft 的值。 */
	draft.ID = "r2"                                           /* 更新 draft.ID 的值。 */
	draft.Name = "duplicate"                                  /* 更新 draft.Name 的值。 */
	_, conflicts, err := engine.ValidateRuleDraft(ctx, draft) /* 更新 err 的值。 */
	if err != nil || len(conflicts) == 0 {                    /* 判断条件并选择处理分支。 */
		t.Fatalf("expected conflict, got conflicts=%v err=%v", conflicts, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	draft.Conditions[0].Field = "undeclared"                          /* 更新 draft.Conditions[0].Field 的值。 */
	if _, _, err = engine.ValidateRuleDraft(ctx, draft); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("expected undeclared thing-model field to fail") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	draft.ProductID = ""                                                                                                 /* 更新 draft.ProductID 的值。 */
	draft.Conditions[0].Field = "temperature"                                                                            /* 更新 draft.Conditions[0].Field 的值。 */
	draft.Actions = []model.RuleAction{{Type: "OPEN_CAMERA", CameraID: "missing-camera"}}                                /* 更新 draft.Actions 的值。 */
	if _, _, err = engine.ValidateRuleDraft(ctx, draft); err == nil || !strings.Contains(err.Error(), "not available") { /* 判断条件并选择处理分支。 */
		t.Fatalf("expected unavailable camera action to fail, got %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = repo.SaveVideoCameraMapping(ctx, model.VideoCameraMapping{TenantID: "t1", CameraID: "camera-1", CameraName: "camera-1", Brand: "测试品牌", Enabled: true}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	draft.Actions = []model.RuleAction{{Type: "OPEN_CAMERA", CameraID: "camera-1"}, {Type: "OPEN_PAGE", Page: "alarms"}} /* 更新 draft.Actions 的值。 */
	if _, _, err = engine.ValidateRuleDraft(ctx, draft); err != nil {                                                    /* 判断条件并选择处理分支。 */
		t.Fatalf("expected allowlisted UI actions to pass: %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for _, page := range []string{"protocolAssistant", "inspection"} { /* 循环处理当前数据。 */
		draft.Actions = []model.RuleAction{{Type: "OPEN_PAGE", Page: page}} /* 更新 draft.Actions 的值。 */
		if _, _, err = engine.ValidateRuleDraft(ctx, draft); err != nil {   /* 判断条件并选择处理分支。 */
			t.Fatalf("expected %s UI action to pass: %v", page, err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestOfficeKnowledgeExtractionAndChunking(t *testing.T) { /* 定义 TestOfficeKnowledgeExtractionAndChunking 函数。 */
	var data bytes.Buffer                                                                                                                                /* 声明 data。 */
	zw := zip.NewWriter(&data)                                                                                                                           /* 更新 zw 的值。 */
	file, _ := zw.Create("word/document.xml")                                                                                                            /* 更新 _ 的值。 */
	_, _ = file.Write([]byte(`<w:document><w:body><w:p><w:r><w:t>消防泵维护步骤</w:t></w:r></w:p><w:p><w:r><w:t>检查水压</w:t></w:r></w:p></w:body></w:document>`)) /* 更新 _ 的值。 */
	_ = zw.Close()                                                                                                                                       /* 更新 _ 的值。 */
	text, err := ExtractKnowledgeText("manual.docx", data.Bytes())                                                                                       /* 更新 err 的值。 */
	if err != nil || !strings.Contains(text, "消防泵维护步骤") {                                                                                                /* 判断条件并选择处理分支。 */
		t.Fatalf("text=%q err=%v", text, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	chunks := ChunkKnowledgeText(strings.Repeat(text, 20), 80, 10) /* 更新 chunks 的值。 */
	if len(chunks) < 2 {                                           /* 判断条件并选择处理分支。 */
		t.Fatalf("expected multiple overlapping chunks, got %d", len(chunks)) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestKnowledgeChunkDetailsUseUnicodeOffsetsAndOverlap(t *testing.T) { /* 定义 TestKnowledgeChunkDetailsUseUnicodeOffsetsAndOverlap 函数。 */
	text := "甲乙丙丁戊己庚辛壬癸"                             /* 更新 text 的值。 */
	chunks := ChunkKnowledgeTextDetailed(text, 6, 2) /* 更新 chunks 的值。 */
	if len(chunks) != 2 {                            /* 判断条件并选择处理分支。 */
		t.Fatalf("expected two chunks, got %#v", chunks) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	first, second := chunks[0], chunks[1]                                                                                                                 /* 更新 second 的值。 */
	if first.Index != 1 || first.StartChar != 0 || first.EndChar != 6 || first.CharacterCount != 6 || first.OverlapChars != 0 || first.Text != "甲乙丙丁戊己" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected first chunk %#v", first) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if second.Index != 2 || second.StartChar != 4 || second.EndChar != 10 || second.CharacterCount != 6 || second.OverlapChars != 2 || second.Text != "戊己庚辛壬癸" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected second chunk %#v", second) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestSpreadsheetKnowledgeExtractionKeepsCells(t *testing.T) { /* 定义 TestSpreadsheetKnowledgeExtractionKeepsCells 函数。 */
	var data bytes.Buffer                                                                                                                            /* 声明 data。 */
	zw := zip.NewWriter(&data)                                                                                                                       /* 更新 zw 的值。 */
	shared, _ := zw.Create("xl/sharedStrings.xml")                                                                                                   /* 更新 _ 的值。 */
	_, _ = shared.Write([]byte(`<sst><si><t>temperature</t></si><si><t>unit</t></si></sst>`))                                                        /* 更新 _ 的值。 */
	sheet, _ := zw.Create("xl/worksheets/sheet1.xml")                                                                                                /* 更新 _ 的值。 */
	_, _ = sheet.Write([]byte(`<worksheet><sheetData><row><c><v>1</v></c><c t="s"><v>0</v></c><c t="s"><v>1</v></c></row></sheetData></worksheet>`)) /* 更新 _ 的值。 */
	_ = zw.Close()                                                                                                                                   /* 更新 _ 的值。 */
	text, err := ExtractKnowledgeText("point-table.xlsx", data.Bytes())                                                                              /* 更新 err 的值。 */
	if err != nil || !strings.Contains(text, "temperature") || !strings.Contains(text, "unit") || !strings.Contains(text, "1") {                     /* 判断条件并选择处理分支。 */
		t.Fatalf("spreadsheet text=%q err=%v", text, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestSpreadsheetKnowledgeExtractionResolvesSharedStringRows(t *testing.T) { /* 定义 TestSpreadsheetKnowledgeExtractionResolvesSharedStringRows 函数。 */
	var data bytes.Buffer                                                                                                                                                                                                          /* 声明 data。 */
	zw := zip.NewWriter(&data)                                                                                                                                                                                                     /* 更新 zw 的值。 */
	shared, _ := zw.Create("xl/sharedStrings.xml")                                                                                                                                                                                 /* 更新 _ 的值。 */
	_, _ = shared.Write([]byte(`<sst><si><t>变量名称</t></si><si><t>PLC 线圈地址</t></si><si><t>通讯心跳测试</t></si><si><t>M100</t></si></sst>`))                                                                                               /* 更新 _ 的值。 */
	sheet, _ := zw.Create("xl/worksheets/sheet1.xml")                                                                                                                                                                              /* 更新 _ 的值。 */
	_, _ = sheet.Write([]byte(`<worksheet><sheetData><row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c></row><row r="2"><c r="A2" t="s"><v>2</v></c><c r="B2" t="s"><v>3</v></c></row></sheetData></worksheet>`)) /* 更新 _ 的值。 */
	_ = zw.Close()                                                                                                                                                                                                                 /* 更新 _ 的值。 */
	text, err := ExtractKnowledgeText("point-table.xlsx", data.Bytes())                                                                                                                                                            /* 更新 err 的值。 */
	if err != nil || !strings.Contains(text, "变量名称") || !strings.Contains(text, "通讯心跳测试") || !strings.Contains(text, "M100") {                                                                                                     /* 判断条件并选择处理分支。 */
		t.Fatalf("spreadsheet text=%q err=%v", text, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if strings.Contains(text, "<sst>") || strings.Contains(text, "0\n1") { /* 判断条件并选择处理分支。 */
		t.Fatalf("spreadsheet text still contains raw shared-string indexes: %q", text) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestReplayDiffAndVideoFusion(t *testing.T) { /* 定义 TestReplayDiffAndVideoFusion 函数。 */
	ctx := context.Background()                                                                                                                     /* 更新 ctx 的值。 */
	repo := memory.NewRepository()                                                                                                                  /* 更新 repo 的值。 */
	archive, _ := local.NewArchive(t.TempDir())                                                                                                     /* 更新 _ 的值。 */
	bus := local.NewBus()                                                                                                                           /* 更新 bus 的值。 */
	engine := New(repo, archive, bus, local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 engine 的值。 */
	if err := engine.Start(ctx); err != nil {                                                                                                       /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	now := time.Now().UnixMilli()                                                                                                                                                                                            /* 更新 now 的值。 */
	raw := model.RawMessage{MessageID: "replay-1", TenantID: "t1", ProductID: "p1", DeviceID: "d1", Protocol: "json", PayloadFormat: "json", ReceivedAt: now, Payload: json.RawMessage(`{"properties":{"temperature":42}}`)} /* 更新 raw 的值。 */
	if _, _, err := engine.IngestRaw(ctx, raw); err != nil {                                                                                                                                                                 /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	replay, err := engine.StartReplay(ctx, model.ReplayRequest{TenantID: "t1", Start: now - 1, End: now + 1, Mode: "DIFF", RatePerSecond: 1000}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                              /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	deadline := time.Now().Add(2 * time.Second) /* 更新 deadline 的值。 */
	for time.Now().Before(deadline) {           /* 循环处理当前数据。 */
		replay, err = repo.GetReplay(ctx, replay.ID)    /* 更新 err 的值。 */
		if err == nil && replay.Status == "COMPLETED" { /* 判断条件并选择处理分支。 */
			break /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		time.Sleep(10 * time.Millisecond) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if replay.Status != "COMPLETED" || replay.DiffSummary["unchanged"] != 1 { /* 判断条件并选择处理分支。 */
		if len(replay.Diffs) > 0 { /* 判断条件并选择处理分支。 */
			t.Fatalf("unexpected replay previous=%#v current=%#v summary=%#v", *replay.Diffs[0].Previous, *replay.Diffs[0].Current, replay.DiffSummary) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		t.Fatalf("unexpected replay %#v", replay) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	if err = repo.SaveVideoCameraMapping(ctx, model.VideoCameraMapping{TenantID: "t1", CameraID: "cam1", CameraName: "Lobby", AreaID: "area-a", CityCode: "city", Enabled: true}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	first, created, err := engine.IngestVideo(ctx, model.VideoAlarmEvent{EventID: "v1", TenantID: "t1", CameraID: "cam1", AlarmType: "FIRE", Confidence: .8, EventTime: now}) /* 更新 err 的值。 */
	if err != nil || !created || first.AreaID != "area-a" {                                                                                                                   /* 判断条件并选择处理分支。 */
		t.Fatalf("first=%#v created=%v err=%v", first, created, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	second, created, err := engine.IngestVideo(ctx, model.VideoAlarmEvent{EventID: "v2", TenantID: "t1", CameraID: "cam1", AlarmType: "FIRE", Confidence: .9, EventTime: now + 1000}) /* 更新 err 的值。 */
	if err != nil || created || second.TriggerCount != 2 || second.Confidence != .9 {                                                                                                 /* 判断条件并选择处理分支。 */
		t.Fatalf("second=%#v created=%v err=%v", second, created, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestVideoMediaTransferIsAsynchronousAndUpdatesAlarm(t *testing.T) { /* 定义 TestVideoMediaTransferIsAsynchronousAndUpdatesAlarm 函数。 */
	release := make(chan struct{})                                                              /* 更新 release 的值。 */
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { /* 更新 media 的值。 */
		<-release                                    /* 执行当前语句并推进处理流程。 */
		w.Header().Set("Content-Type", "image/jpeg") /* 执行当前语句并推进处理流程。 */
		_, _ = w.Write([]byte("image"))              /* 更新 _ 的值。 */
	})) /* 结束当前表达式或代码块。 */
	defer media.Close()                                                                                                                                        /* 安排函数结束时执行清理。 */
	ctx := context.Background()                                                                                                                                /* 更新 ctx 的值。 */
	repo := memory.NewRepository()                                                                                                                             /* 更新 repo 的值。 */
	archive, _ := local.NewArchive(t.TempDir())                                                                                                                /* 更新 _ 的值。 */
	engine := New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 engine 的值。 */
	engine.VideoMediaAllowedHosts = []string{"127.0.0.1"}                                                                                                      /* 更新 engine.VideoMediaAllowedHosts 的值。 */
	if err := engine.Start(ctx); err != nil {                                                                                                                  /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	done := make(chan error, 1) /* 更新 done 的值。 */
	go func() {                 /* 执行当前语句并推进处理流程。 */
		_, _, err := engine.IngestVideo(ctx, model.VideoAlarmEvent{EventID: "async-media", TenantID: "t1", CameraID: "cam-media", CameraName: "Media camera", AreaID: "a1", AlarmType: "FIRE", EventTime: time.Now().UnixMilli(), SnapshotURL: media.URL + "/snapshot.jpg"}) /* 更新 err 的值。 */
		done <- err                                                                                                                                                                                                                                                          /* 执行当前语句并推进处理流程。 */
	}() /* 结束当前表达式或代码块。 */
	select { /* 根据条件选择处理路径。 */
	case err := <-done: /* 处理当前分支。 */
		if err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	case <-time.After(500 * time.Millisecond): /* 处理当前分支。 */
		t.Fatal("video ingest blocked on external media download") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	close(release)                              /* 执行当前语句并推进处理流程。 */
	deadline := time.Now().Add(2 * time.Second) /* 更新 deadline 的值。 */
	for time.Now().Before(deadline) {           /* 循环处理当前数据。 */
		alarms, _ := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "t1", Status: "ACTIVE"}) /* 更新 _ 的值。 */
		if len(alarms) > 0 {                                                                   /* 判断条件并选择处理分支。 */
			if event, ok := alarms[0].Details["videoEvent"].(model.VideoAlarmEvent); ok && strings.HasPrefix(event.SnapshotURL, "local://") { /* 判断条件并选择处理分支。 */
				return /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		time.Sleep(10 * time.Millisecond) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	t.Fatal("archived media URL was not written back to active alarm") /* 验证实际结果符合预期。 */
} /* 结束当前表达式或代码块。 */
