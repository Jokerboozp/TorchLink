package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestObservedCommandTimeoutPreservesTerminalReply(t *testing.T) {
	for _, status := range []string{"SENT", "DISPATCHING"} {
		c := DeviceCommand{Status: status, CreatedAt: 1000}
		if c.ObservedOutcome(30999).Status != status || c.ObservedOutcome(31000).Status != "UNKNOWN" {
			t.Fatal("incorrect wait boundary")
		}
		if c.Status != status {
			t.Fatal("query mutated stored outcome")
		}
	}
	for _, status := range []string{"SUCCEEDED", "FAILED"} {
		if (DeviceCommand{Status: status, CreatedAt: 1000}).ObservedOutcome(40000).Status != status {
			t.Fatal("terminal outcome lost")
		}
	}
}

func TestLegacyQueuedCommandIsUnknownAndHidesExecutionToken(t *testing.T) {
	var c DeviceCommand
	if err := json.Unmarshal([]byte(`{"id":"old-command","status":"QUEUED","execution":{"nodeId":"old-node","token":"legacy-secret"}}`), &c); err != nil {
		t.Fatal(err)
	}
	observed := c.ObservedOutcome(100).Public()
	if observed.Status != "UNKNOWN" || observed.LastError == "" || c.Status != "QUEUED" {
		t.Fatal("legacy queue claimed execution or mutated history", observed)
	}
	encoded, err := json.Marshal(observed)
	if err != nil || strings.Contains(string(encoded), "legacy-secret") || strings.Contains(string(encoded), "execution") {
		t.Fatal("legacy execution metadata leaked", err)
	}
}
