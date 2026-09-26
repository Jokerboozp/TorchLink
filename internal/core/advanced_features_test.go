package core

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
)

func TestGengineExpressionAndSafety(t *testing.T) {
	message := model.StandardMessage{TenantID: "t1", ProductID: "p1", Properties: map[string]any{"temperature": 88.5, "smoke": true}}
	rule := model.AlarmRule{TenantID: "t1", ProductID: "p1", Enabled: true, Expression: `Properties["temperature"] > 80 && Properties["smoke"] == true`}
	matched, err := EvaluateGengineExpression(rule.Expression, message)
	if err != nil || !matched {
		t.Fatalf("expected controlled Gengine expression to match: matched=%v err=%v", matched, err)
	}
	if err := ValidateGengineExpression(`system("rm")`); err == nil {
		t.Fatal("expected unsafe expression to be rejected")
	}
}

func TestRuleDraftValidatesThingModelAndConflicts(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	archive, _ := local.NewArchive(t.TempDir())
	engine := New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	product := model.Product{ID: "p1", TenantID: "t1", Metadata: map[string]any{"properties": []any{map[string]any{"id": "temperature"}}}}
	if err := repo.SaveProduct(ctx, product); err != nil {
		t.Fatal(err)
	}
	base := model.AlarmRule{ID: "r1", TenantID: "t1", ProductID: "p1", Name: "high temperature", AlarmType: "FIRE_RISK", Level: "HIGH", Match: "all", Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 80}}}
	if err := repo.SaveRule(ctx, base); err != nil {
		t.Fatal(err)
	}
	draft := base
	draft.ID = "r2"
	draft.Name = "duplicate"
	_, conflicts, err := engine.ValidateRuleDraft(ctx, draft)
	if err != nil || len(conflicts) == 0 {
		t.Fatalf("expected conflict, got conflicts=%v err=%v", conflicts, err)
	}
	draft.Conditions[0].Field = "undeclared"
	if _, _, err = engine.ValidateRuleDraft(ctx, draft); err == nil {
		t.Fatal("expected undeclared thing-model field to fail")
	}
	draft.ProductID = ""
	draft.Conditions[0].Field = "temperature"
	draft.Actions = []model.RuleAction{{Type: "OPEN_CAMERA", CameraID: "missing-camera"}}
	if _, _, err = engine.ValidateRuleDraft(ctx, draft); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("expected unavailable camera action to fail, got %v", err)
	}
	if err = repo.SaveVideoCameraMapping(ctx, model.VideoCameraMapping{TenantID: "t1", CameraID: "camera-1", CameraName: "camera-1", Brand: "测试品牌", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	draft.Actions = []model.RuleAction{{Type: "OPEN_CAMERA", CameraID: "camera-1"}, {Type: "OPEN_PAGE", Page: "alarms"}}
	if _, _, err = engine.ValidateRuleDraft(ctx, draft); err != nil {
		t.Fatalf("expected allowlisted UI actions to pass: %v", err)
	}
	for _, page := range []string{"protocolAssistant", "inspection"} {
		draft.Actions = []model.RuleAction{{Type: "OPEN_PAGE", Page: page}}
		if _, _, err = engine.ValidateRuleDraft(ctx, draft); err != nil {
			t.Fatalf("expected %s UI action to pass: %v", page, err)
		}
	}
}

func TestOfficeKnowledgeExtractionAndChunking(t *testing.T) {
	var data bytes.Buffer
	zw := zip.NewWriter(&data)
	file, _ := zw.Create("word/document.xml")
	_, _ = file.Write([]byte(`<w:document><w:body><w:p><w:r><w:t>消防泵维护步骤</w:t></w:r></w:p><w:p><w:r><w:t>检查水压</w:t></w:r></w:p></w:body></w:document>`))
	_ = zw.Close()
	text, err := ExtractKnowledgeText("manual.docx", data.Bytes())
	if err != nil || !strings.Contains(text, "消防泵维护步骤") {
		t.Fatalf("text=%q err=%v", text, err)
	}
	chunks := ChunkKnowledgeText(strings.Repeat(text, 20), 80, 10)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple overlapping chunks, got %d", len(chunks))
	}
}

func TestKnowledgeChunkDetailsUseUnicodeOffsetsAndOverlap(t *testing.T) {
	text := "甲乙丙丁戊己庚辛壬癸"
	chunks := ChunkKnowledgeTextDetailed(text, 6, 2)
	if len(chunks) != 2 {
		t.Fatalf("expected two chunks, got %#v", chunks)
	}
	first, second := chunks[0], chunks[1]
	if first.Index != 1 || first.StartChar != 0 || first.EndChar != 6 || first.CharacterCount != 6 || first.OverlapChars != 0 || first.Text != "甲乙丙丁戊己" {
		t.Fatalf("unexpected first chunk %#v", first)
	}
	if second.Index != 2 || second.StartChar != 4 || second.EndChar != 10 || second.CharacterCount != 6 || second.OverlapChars != 2 || second.Text != "戊己庚辛壬癸" {
		t.Fatalf("unexpected second chunk %#v", second)
	}
}

func TestSpreadsheetKnowledgeExtractionKeepsCells(t *testing.T) {
	var data bytes.Buffer
	zw := zip.NewWriter(&data)
	shared, _ := zw.Create("xl/sharedStrings.xml")
	_, _ = shared.Write([]byte(`<sst><si><t>temperature</t></si><si><t>unit</t></si></sst>`))
	sheet, _ := zw.Create("xl/worksheets/sheet1.xml")
	_, _ = sheet.Write([]byte(`<worksheet><sheetData><row><c><v>1</v></c><c t="s"><v>0</v></c><c t="s"><v>1</v></c></row></sheetData></worksheet>`))
	_ = zw.Close()
	text, err := ExtractKnowledgeText("point-table.xlsx", data.Bytes())
	if err != nil || !strings.Contains(text, "temperature") || !strings.Contains(text, "unit") || !strings.Contains(text, "1") {
		t.Fatalf("spreadsheet text=%q err=%v", text, err)
	}
}

func TestSpreadsheetKnowledgeExtractionResolvesSharedStringRows(t *testing.T) {
	var data bytes.Buffer
	zw := zip.NewWriter(&data)
	shared, _ := zw.Create("xl/sharedStrings.xml")
	_, _ = shared.Write([]byte(`<sst><si><t>变量名称</t></si><si><t>PLC 线圈地址</t></si><si><t>通讯心跳测试</t></si><si><t>M100</t></si></sst>`))
	sheet, _ := zw.Create("xl/worksheets/sheet1.xml")
	_, _ = sheet.Write([]byte(`<worksheet><sheetData><row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c></row><row r="2"><c r="A2" t="s"><v>2</v></c><c r="B2" t="s"><v>3</v></c></row></sheetData></worksheet>`))
	_ = zw.Close()
	text, err := ExtractKnowledgeText("point-table.xlsx", data.Bytes())
	if err != nil || !strings.Contains(text, "变量名称") || !strings.Contains(text, "通讯心跳测试") || !strings.Contains(text, "M100") {
		t.Fatalf("spreadsheet text=%q err=%v", text, err)
	}
	if strings.Contains(text, "<sst>") || strings.Contains(text, "0\n1") {
		t.Fatalf("spreadsheet text still contains raw shared-string indexes: %q", text)
	}
}

func TestReplayDiffAndVideoFusion(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	archive, _ := local.NewArchive(t.TempDir())
	bus := local.NewBus()
	engine := New(repo, archive, bus, local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	raw := model.RawMessage{MessageID: "replay-1", TenantID: "t1", ProductID: "p1", DeviceID: "d1", Protocol: "json", PayloadFormat: "json", ReceivedAt: now, Payload: json.RawMessage(`{"properties":{"temperature":42}}`)}
	if _, _, err := engine.IngestRaw(ctx, raw); err != nil {
		t.Fatal(err)
	}
	replay, err := engine.StartReplay(ctx, model.ReplayRequest{TenantID: "t1", Start: now - 1, End: now + 1, Mode: "DIFF", RatePerSecond: 1000})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		replay, err = repo.GetReplay(ctx, replay.ID)
		if err == nil && replay.Status == "COMPLETED" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if replay.Status != "COMPLETED" || replay.DiffSummary["unchanged"] != 1 {
		if len(replay.Diffs) > 0 {
			t.Fatalf("unexpected replay previous=%#v current=%#v summary=%#v", *replay.Diffs[0].Previous, *replay.Diffs[0].Current, replay.DiffSummary)
		}
		t.Fatalf("unexpected replay %#v", replay)
	}

	if err = repo.SaveVideoCameraMapping(ctx, model.VideoCameraMapping{TenantID: "t1", CameraID: "cam1", CameraName: "Lobby", AreaID: "area-a", CityCode: "city", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	first, created, err := engine.IngestVideo(ctx, model.VideoAlarmEvent{EventID: "v1", TenantID: "t1", CameraID: "cam1", AlarmType: "FIRE", Confidence: .8, EventTime: now})
	if err != nil || !created || first.AreaID != "area-a" {
		t.Fatalf("first=%#v created=%v err=%v", first, created, err)
	}
	second, created, err := engine.IngestVideo(ctx, model.VideoAlarmEvent{EventID: "v2", TenantID: "t1", CameraID: "cam1", AlarmType: "FIRE", Confidence: .9, EventTime: now + 1000})
	if err != nil || created || second.TriggerCount != 2 || second.Confidence != .9 {
		t.Fatalf("second=%#v created=%v err=%v", second, created, err)
	}
}

func TestVideoMediaTransferIsAsynchronousAndUpdatesAlarm(t *testing.T) {
	release := make(chan struct{})
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("image"))
	}))
	defer media.Close()
	ctx := context.Background()
	repo := memory.NewRepository()
	archive, _ := local.NewArchive(t.TempDir())
	engine := New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.VideoMediaAllowedHosts = []string{"127.0.0.1"}
	if err := engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := engine.IngestVideo(ctx, model.VideoAlarmEvent{EventID: "async-media", TenantID: "t1", CameraID: "cam-media", CameraName: "Media camera", AreaID: "a1", AlarmType: "FIRE", EventTime: time.Now().UnixMilli(), SnapshotURL: media.URL + "/snapshot.jpg"})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("video ingest blocked on external media download")
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		alarms, _ := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "t1", Status: "ACTIVE"})
		if len(alarms) > 0 {
			if event, ok := alarms[0].Details["videoEvent"].(model.VideoAlarmEvent); ok && strings.HasPrefix(event.SnapshotURL, "local://") {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("archived media URL was not written back to active alarm")
}
