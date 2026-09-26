package core

import (
	"iot-platform/internal/model"
	"testing"
)

func TestMatchRuleAll(t *testing.T) {
	rule := model.AlarmRule{TenantID: "t", Enabled: true, Match: "all", Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 80}, {Field: "smoke", Operator: "eq", Value: true}}}
	msg := model.StandardMessage{TenantID: "t", Properties: map[string]any{"temperature": 81.2, "smoke": true}}
	if !MatchRule(rule, msg) {
		t.Fatal("expected match")
	}
	msg.Properties["smoke"] = false
	if MatchRule(rule, msg) {
		t.Fatal("unexpected match")
	}
}

func TestMatchRuleEventPath(t *testing.T) {
	rule := model.AlarmRule{TenantID: "t", Enabled: true, Conditions: []model.RuleCondition{{Field: "event.smoke", Operator: "eq", Value: true}}}
	msg := model.StandardMessage{TenantID: "t", Event: map[string]any{"smoke": true}}
	if !MatchRule(rule, msg) {
		t.Fatal("event-prefixed condition should match the event field")
	}
}

func TestAlarmTopicSanitizesSegments(t *testing.T) {
	a := model.Alarm{CityCode: "city", DistrictCode: "district/escape", BuildingID: "A", DeviceType: "smoke", DeviceID: "d"}
	if got := a.MQTTTopic("raised"); got != "/iot/alarm/raised/city/district_escape/A/smoke/d" {
		t.Fatalf("unexpected topic %s", got)
	}
}
