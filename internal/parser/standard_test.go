package parser

import (
	"encoding/json"
	"iot-platform/internal/model"
	"strings"
	"testing"
)

func TestStandardVersionOneAndLegacy(t *testing.T) {
	for _, tc := range []struct {
		kind, payload string
		valid         bool
	}{
		{"property", `{"id":"1","version":"1.0","timestamp":1000,"data":{"temperature":26.5}}`, true},
		{"property", `{"id":"1","shadow":"control","timestamp":1000,"data":{"temperature":26.5}}`, true},
		{"property", `{"id":"1","shadow":"../other","timestamp":1000,"data":{"temperature":26.5}}`, false},
		{"state", `{"id":"1","shadow":"control","timestamp":1000,"online":true}`, false},
		{"event", `{"id":"1","version":"1.0","timestamp":1000,"event":"fire_alarm","data":{"zone":3}}`, true},
		{"state", `{"id":"1","version":"1.0","timestamp":1000,"online":true}`, true},
		{"command-reply", `{"id":"1","version":"1.0","timestamp":1000,"commandId":"c","success":false,"data":{}}`, true},
		{"state", `{"id":"1","timestamp":1000,"data":{"connectionStatus":"CONNECTED"}}`, true},
		{"property", `{"id":"1","version":"2.0","timestamp":1000,"data":{"x":1}}`, false},
		{"event", `{"id":"1","version":"1.0","timestamp":1000,"data":{"zone":3}}`, false},
		{"property", `{"id":"1","id":"2","timestamp":1000,"data":{"x":1}}`, false},
		{"state", `{"id":"1","timestamp":1000,"online":true,"data":{"connectionStatus":"DISCONNECTED"}}`, false},
		{"command-reply", `{"id":"1","timestamp":1000,"commandId":"c","success":"true"}`, false},
		{"property", `{"id":"1","timestamp":999999999999999,"data":{"x":1}}`, false},
		{"property", `{"id":"1","timestamp":1000,"data":{"x":` + strings.Repeat("[", 17) + "1" + strings.Repeat("]", 17) + `}}`, false},
	} {
		t.Run(tc.kind+tc.payload, func(t *testing.T) {
			raw := model.RawMessage{Payload: json.RawMessage(tc.payload), ReceivedAt: 2000, Headers: map[string]string{"messageKind": tc.kind}, TenantID: "trusted", DeviceID: "device"}
			m, err := (StandardParser{}).Parse(raw)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
			if !tc.valid && m != nil {
				t.Fatal("invalid input produced business message")
			}
			if tc.valid && (m.TenantID != "trusted" || m.DeviceID != "device" || m.MessageType == model.AlarmReport) {
				t.Fatal("identity or alarm semantics changed")
			}
		})
	}
}
