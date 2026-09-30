package aioutput

import "testing"

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
