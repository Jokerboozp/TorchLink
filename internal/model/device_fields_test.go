package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestManagedDeviceReadsLegacySystemTags(t *testing.T) {
	var d ManagedDevice
	legacy := `{"id":"d","connector":"MQTT","tags":{"floor":"1","connector":"HTTP","connectorProfileId":"p","childAddress":"3","childType":"smoke","onboardingRequestHash":"h"}}`
	if err := json.Unmarshal([]byte(legacy), &d); err != nil {
		t.Fatal(err)
	}
	if d.Connector != "MQTT" || d.ConnectorProfileID != "p" || d.ChildAddress != "3" || d.ChildType != "smoke" || d.OnboardingRequestHash != "h" {
		t.Fatalf("fields %+v", d)
	}
	if len(d.Tags) != 1 || d.Tags["floor"] != "1" {
		t.Fatalf("system keys must leave the labels: %v", d.Tags)
	}
	data, _ := json.Marshal(d.Public(Product{}))
	if strings.Contains(string(data), "onboardingRequestHash") || !strings.Contains(string(data), `"connectorProfileId":"p"`) {
		t.Fatalf("public device %s", data)
	}
	if !SystemTag("childType") || SystemTag("floor") {
		t.Fatal("reserved label keys")
	}
}
