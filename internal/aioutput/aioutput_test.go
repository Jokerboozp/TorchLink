package aioutput

import (
	"strings"
	"testing"
)

func TestDecodeAlarmAnalysisConfidence(t *testing.T) {
	for _, test := range []struct {
		name, confidence string
		want             float64
	}{
		{"numeric string", `"0.86"`, 0.86},
		{"text label", `"较高"`, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			content := `{"summary":"告警事实已核对，请人工复核现场。","possibleReasons":["传感器异常"],"suggestions":["现场复核设备"],"riskLevel":"HIGH","confidence":` + test.confidence + `}`
			analysis, err := DecodeAlarmAnalysis(content, "alarm-1", "test-model")
			if err != nil || analysis.Summary == "" || analysis.AlarmID != "alarm-1" || analysis.Model != "test-model" || analysis.Confidence != test.want {
				t.Fatalf("analysis=%#v err=%v", analysis, err)
			}
		})
	}
}

func TestDecodeRuleDraftNormalizesObjectShapedModelOutput(t *testing.T) {
	rule, err := DecodeRuleDraft(`{"name":"smoke_detector_high_alarm","alarmType":"smoke","level":"high","match":{"deviceType":"smoke_detector"},"conditions":{"smoke":true},"durationSeconds":0,"recovery":{"event":"smoke_clear"},"actions":{"type":"open_camera","cameraId":"camera-001"}}`)
	if err != nil {
		t.Fatal(err)
	}
	if rule.AlarmType != "SMOKE_DETECTED" || rule.Level != "HIGH" || rule.Match != "all" {
		t.Fatalf("unexpected normalized rule: %#v", rule)
	}
	if len(rule.Conditions) != 1 || rule.Conditions[0].Field != "smoke" || rule.Conditions[0].Operator != "eq" || rule.Conditions[0].Value != true {
		t.Fatalf("unexpected conditions: %#v", rule.Conditions)
	}
	if len(rule.Recovery) != 1 || rule.Recovery[0].Field != "event" || rule.Recovery[0].Value != "smoke_clear" {
		t.Fatalf("unexpected recovery: %#v", rule.Recovery)
	}
	if len(rule.Actions) != 1 || rule.Actions[0].Type != "OPEN_CAMERA" || rule.Actions[0].CameraID != "camera-001" {
		t.Fatalf("unexpected actions: %#v", rule.Actions)
	}
}

func TestExtractJSONMatchesBracesOutsideStrings(t *testing.T) {
	for _, test := range []struct{ name, content, want string }{
		{"surrounding braces", `结论如下 {参考} {"summary":"a","riskLevel":"LOW"} 备注 {见附件}`, `{"summary":"a","riskLevel":"LOW"}`},
		{"braces in strings", "```json\n{\"summary\":\"温度 {85} 摄氏度 \\\" }\",\"n\":{\"x\":1}}\n```", `{"summary":"温度 {85} 摄氏度 \" }","n":{"x":1}}`},
		{"first of two", `{"summary":"one"} {"summary":"two"}`, `{"summary":"one"}`},
		{"unterminated", `{"summary":"a"`, `{"summary":"a"`},
		{"trailing commas", "```json\n{\"summary\":\"a, }\",\"items\":[1,2,],\n}\n```", "{\"summary\":\"a, }\",\"items\":[1,2]\n}"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := ExtractJSON(test.content); got != test.want {
				t.Fatalf("got %q want %q", got, test.want)
			}
		})
	}
}

func TestDecodeAlarmAnalysisRejectsAndBoundsBadOutput(t *testing.T) {
	if _, err := DecodeAlarmAnalysis(`{"summary":"x","riskLevel":"SEVERE"}`, "a", "m"); err == nil {
		t.Fatal("unknown risk level accepted")
	}
	if _, err := DecodeAlarmAnalysis(`{"summary":"x"}`, "a", "m"); err == nil {
		t.Fatal("missing risk level accepted")
	}
	long := strings.Repeat("长", 800)
	reasons := `["` + long + `"` + strings.Repeat(`,"r"`, 20) + `,"  "]`
	analysis, err := DecodeAlarmAnalysis(`说明 {不是 JSON} {"summary":" 结论 ","riskLevel":"high","confidence":1.7,"possibleReasons":`+reasons+`,"suggestions":["复核"]} 以上`, "a", "m")
	if err != nil {
		t.Fatal(err)
	}
	if analysis.RiskLevel != "HIGH" || analysis.Summary != "结论" || analysis.Confidence != 1 || analysis.PromptVersion != "" {
		t.Fatalf("analysis=%#v", analysis)
	}
	if len(analysis.PossibleReasons) != MaxAnalysisItems || len([]rune(analysis.PossibleReasons[0])) != MaxAnalysisItemRune || len(analysis.Suggestions) != 1 {
		t.Fatalf("lists were not bounded: %d reasons, first %d runes", len(analysis.PossibleReasons), len([]rune(analysis.PossibleReasons[0])))
	}
}
