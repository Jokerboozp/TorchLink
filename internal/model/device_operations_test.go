package model

import "testing"

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
