package externaldata

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func fixtureMapping() Mapping {
	return Mapping{ItemsPath: "data.items", SuccessPath: "code", SuccessValue: float64(0), Fields: []Field{
		{Target: "id", Path: "event.id", Required: true},
		{Target: "objectId", Path: "camera.id"},
		{Target: "timestamp", Path: "at", Type: "timestamp", TimeFormat: "seconds"},
		{Target: "alarmType", Path: "category", Values: map[string]any{"fire": "火警"}},
		{Target: "online", Path: "online", Type: "boolean"},
		{Target: "content", Constant: true, Value: "视频告警"},
		{Target: "alarmLevel", Path: "missing", Default: "HIGH"},
		{Target: "data.scores", Path: "detections[*].confidence", Type: "json"},
	}}
}

func TestExtractTransformFieldsAndTime(t *testing.T) {
	m := fixtureMapping()
	items, err := Extract(m, []byte(`{"code":0,"data":{"items":[{"event":{"id":"e1"},"camera":{"id":42},"at":1700000000,"category":"fire","online":"1","detections":[{"confidence":0.8},{"confidence":0.7}]}]}}`))
	if err != nil || len(items) != 1 {
		t.Fatalf("extract: %v %s", err, items)
	}
	event, filtered, err := Transform(m, items[0])
	if err != nil || filtered {
		t.Fatalf("transform: %v %v", err, filtered)
	}
	if event.ID != "e1" || event.ObjectID != "42" || event.Timestamp != 1700000000000 || event.AlarmType != "火警" || event.AlarmLevel != "HIGH" || event.Online == nil || !*event.Online || event.Content != "视频告警" {
		t.Fatalf("unexpected event: %+v", event)
	}
	scores, ok := event.Data["scores"].([]any)
	if !ok || len(scores) != 2 {
		t.Fatalf("array mapping: %#v", event.Data)
	}
	m.Fields = []Field{{Target: "id", Path: "id"}, {Target: "timestamp", Path: "time", TimeFormat: "2006-01-02 15:04:05", Timezone: "Asia/Shanghai"}}
	event, _, err = Transform(m, json.RawMessage(`{"id":"a","time":"2026-10-02 08:00:00"}`))
	if err != nil || event.Timestamp != time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("timezone: %+v %v", event, err)
	}
}

func TestMapperFiltersCompositeAndDefaults(t *testing.T) {
	m := Mapping{IDFields: []string{"camera", "at"}, Fields: []Field{{Target: "timestamp", Path: "at"}, {Target: "data.details", Path: "payload", Default: map[string]any{"fallback": true}}}, Filters: []Filter{{Path: "kind", Operator: "in", Value: []any{"fire", "smoke"}}, {Path: "score", Operator: "gte", Value: 0.8}}}
	first, filtered, err := Transform(m, json.RawMessage(`{"camera":"a","at":1700000000000,"kind":"fire","score":0.9}`))
	if err != nil || filtered || len(first.ID) != 64 {
		t.Fatalf("composite: %+v %v %v", first, filtered, err)
	}
	second, _, err := Transform(m, json.RawMessage(`{"kind":"fire","score":0.9,"at":1700000000000,"camera":"a"}`))
	if err != nil || first.ID != second.ID {
		t.Fatal("composite ID must be stable")
	}
	_, filtered, err = Transform(m, json.RawMessage(`{"camera":"a","at":1700000000000,"kind":"other","score":0.9}`))
	if err != nil || !filtered {
		t.Fatal("expected filter")
	}
	_, _, err = Transform(m, json.RawMessage(`{"at":1700000000000,"kind":"fire","score":0.9}`))
	if err == nil {
		t.Fatal("missing composite ID accepted")
	}
}

func TestMappingRejectsInvalidPayloadAndMissingRequired(t *testing.T) {
	for _, body := range []string{`{} {}`, `{"code":1,"data":{"items":[]}}`, `{"code":0,"data":{"items":[1]}}`, `{"code":0}`, strings.Repeat(" ", MaxBodyBytes+1)} {
		if _, err := Extract(fixtureMapping(), []byte(body)); err == nil {
			t.Fatalf("accepted %.50s", body)
		}
	}
	m := Mapping{Fields: []Field{{Target: "id", Path: "id", Required: true}, {Target: "timestamp", Path: "time"}}}
	for _, raw := range []string{`{"time":1700000000000}`, `{"id":"a","time":0}`, `{"id":"a","time":"bad"}`, `{"id":"a","time":-1}`, `null`} {
		if _, _, err := Transform(m, json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if _, err := Extract(Mapping{}, []byte("["+strings.TrimSuffix(strings.Repeat("{},", 1001), ",")+"]")); err == nil {
		t.Fatal("accepted more than 1000 items")
	}
}

func TestPathsAndFilterOperators(t *testing.T) {
	root, err := decodeJSON([]byte(`{"a.b":{"list":[{"x":2},{"x":4}]},"tags":["fire","smoke"],"name":"camera 1"}`))
	if err != nil {
		t.Fatal(err)
	}
	value, ok := lookup(root, `$["a.b"].list[1].x`)
	if !ok || value != json.Number("4") {
		t.Fatalf("quoted path: %v", value)
	}
	for _, f := range []Filter{{Path: "tags", Operator: "contains", Value: "fire"}, {Path: "name", Operator: "contains", Value: "camera"}, {Path: "missing", Operator: "not_exists"}, {Path: "name", Operator: "exists"}, {Path: "name", Operator: "ne", Value: "other"}, {Path: "name", Operator: "not_in", Value: []any{"other"}}, {Path: `$['a.b'].list[0].x`, Operator: "lt", Value: 3.0}} {
		match, err := filterMatches(root, f)
		if err != nil || !match {
			t.Fatalf("filter %+v: %v %v", f, match, err)
		}
	}
	for _, path := range []string{"$oops", "a[", "a..b", "a.", "a[-1]", "a[]"} {
		if _, err := pathParts(path); err == nil {
			t.Fatalf("invalid path accepted: %s", path)
		}
	}
}

func TestNumericEventVersionAndFiltersPreserveIntegerPrecision(t *testing.T) {
	m := Mapping{Fields: []Field{{Target: "id", Path: "id"}, {Target: "timestamp", Path: "time"}, {Target: "version", Path: "version"}}, Filters: []Filter{{Path: "version", Operator: "gt", Value: json.Number("9007199254740992")}}}
	event, filtered, err := Transform(m, json.RawMessage(`{"id":"event","time":1700000000000,"version":9007199254740993}`))
	if err != nil || filtered || event.Version != 9007199254740993 {
		t.Fatalf("version precision: %+v %v %v", event, filtered, err)
	}
	if equalValue(json.Number("9007199254740992"), json.Number("9007199254740993")) {
		t.Fatal("distinct numeric identifiers compared equal")
	}
}
