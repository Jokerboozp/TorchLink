package main

import (
	"bytes"
	"encoding/json"
	"gb26875-dahua/gb26875"
	"os"
	"strings"
	"testing"
)

func TestSamplesAndWireOperations(t *testing.T) {
	data, err := os.ReadFile("samples/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name                string
		Input               gb26875.RawMessage
		ExpectedMessageType gb26875.MessageType
		ExpectedProperties  map[string]any
		ExpectedEventType   string
		ExpectedTimestamp   int64
	}
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			msg, err := gb26875.Decode(c.Input)
			if err != nil {
				t.Fatal(err)
			}
			if msg.MessageType != c.ExpectedMessageType {
				t.Fatalf("type %s", msg.MessageType)
			}
			encoded, _ := json.Marshal(msg.Properties)
			var props map[string]any
			_ = json.Unmarshal(encoded, &props)
			for k, v := range c.ExpectedProperties {
				if props[k] != v {
					t.Fatalf("%s=%v want %v", k, props[k], v)
				}
			}
			if c.ExpectedEventType != "" && msg.Event["type"] != c.ExpectedEventType {
				t.Fatalf("event %v", msg.Event)
			}
			if c.ExpectedTimestamp != 0 && msg.Timestamp != c.ExpectedTimestamp {
				t.Fatalf("timestamp %d", msg.Timestamp)
			}
		})
	}
	raw := cases[0].Input
	var frame string
	_ = json.Unmarshal(raw.Payload, &frame)
	for n := 2; n < len(frame); n += 2 {
		r := gb26875.Handle(gb26875.Request{Version: 2, Operation: "ingress", Data: frame[:n]})
		if !r.NeedMore || r.Consumed != 0 || r.Reply != "" {
			t.Fatalf("fragment %d: %+v", n, r)
		}
	}
	for _, bad := range []string{"00" + frame, frame[:len(frame)-6] + "002323", "404001000103000000000000000000000000000000000000FFFF02"} {
		if r := gb26875.Handle(gb26875.Request{Version: 2, Operation: "ingress", Data: bad}); r.Error == "" {
			t.Fatalf("accepted corrupt frame: %+v", r)
		}
	}
	r := gb26875.Handle(gb26875.Request{Version: 2, Operation: "ingress", Data: frame + frame, Now: raw.ReceivedAt})
	if r.Error != "" || r.Consumed != len(frame)/2 || r.Reply == "" {
		t.Fatalf("coalesced frame: %+v", r)
	}
	var output bytes.Buffer
	state, _ := json.Marshal(gb26875.SessionState{Source: "123456789012", Sequence: 12})
	preserved := gb26875.Handle(gb26875.Request{Version: 2, Operation: "ingress", Data: frame, State: state})
	if preserved.State == nil || preserved.State.Sequence != 12 {
		t.Fatal("incoming report rewound command sequence")
	}
	encoded, _ := json.Marshal(raw)
	if err = run(bytes.NewReader(encoded), &output); err != nil || !strings.Contains(output.String(), "standardMessage") {
		t.Fatalf("v1 compatibility: %v %s", err, output.String())
	}
}

func TestMultipleComponentsPreserveSeparateAlarmLocations(t *testing.T) {
	b, err := os.ReadFile("samples/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name  string
		Input gb26875.RawMessage
	}
	if err = json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if c.Name != "multiple-components" {
			continue
		}
		msg, err := gb26875.Decode(c.Input)
		if err != nil {
			t.Fatal(err)
		}
		if _, exists := msg.Properties["nodeAddress"]; exists {
			t.Fatal("aggregate carries first object's address")
		}
		components := msg.Event["components"].([]map[string]any)
		if len(components) != 2 || components[0]["id"] == components[1]["id"] {
			t.Fatal(components)
		}
		if components[0]["alarms"].(map[string]bool)["FIRE"] || !components[1]["alarms"].(map[string]bool)["FIRE"] || components[1]["location"] != "alarm second" {
			t.Fatal(components)
		}
		return
	}
	t.Fatal("fixture missing")
}
