package main

import (
	"encoding/json"
	"iot-platform/internal/model"
	"testing"
)

func TestSelectionPreservesIdentityAndRejectsOtherTenants(t *testing.T) {
	msg := model.StandardMessage{TenantID: "t", MessageID: "m", RawMessageID: "raw", DeviceID: "d"}
	payload, _ := json.Marshal(msg)
	body, _ := json.Marshal(deadLetter{SourceTopic: model.TopicPropertyReport, ConsumerGroup: "storage", Payload: payload})
	got, b, err := selectMessage(body, "storage", "t", model.TopicPropertyReport, map[string]bool{"m": true})
	if err != nil || got.MessageID != "m" || string(b) != string(payload) {
		t.Fatal(got, err)
	}
	for _, tenant := range []string{"other", ""} {
		_, b, err = selectMessage(body, "storage", tenant, model.TopicPropertyReport, map[string]bool{"m": true})
		if err != nil || b != nil {
			t.Fatal("tenant leak", err)
		}
	}
	_, b, err = selectMessage(body, "storage", "t", model.TopicPropertyReport, map[string]bool{"other": true})
	if err != nil || b != nil {
		t.Fatal("unselected message", err)
	}
}
