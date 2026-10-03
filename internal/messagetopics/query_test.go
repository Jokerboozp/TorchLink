package messagetopics

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/model"
)

func reportQuery() model.MessageTopicQuery {
	return model.MessageTopicQuery{Dataset: "device_reports", DeviceScope: "selected", DeviceIDs: []string{"d"}, Mode: "realtime"}
}
func queryFixture(t *testing.T, protocol string) model.MessageTopicConfig {
	t.Helper()
	q := reportQuery()
	cfg := model.MessageTopicConfig{Topics: []model.MessageTopicRoute{{ID: "shared", Name: "设备数据", Protocol: protocol, Topic: "device", Enabled: true, Query: &q}}}
	if err := AccumulateQueryExposure(&cfg, "shared", q); err != nil {
		t.Fatal(err)
	}
	return cfg
}
func TestQuerySQLCompilesRoundTripsAndPreservesPrecedence(t *testing.T) {
	sql := `SELECT deviceId, properties.temperature AS temperature FROM device_reports WHERE productId = 'smoke' AND (properties.temperature >= 26.5 OR event.name IN ('alarm', 'fault')) AND tags.room CONTAINS 'O''Brien'`
	q, err := CompileQuerySQL(sql)
	if err != nil {
		t.Fatal(err)
	}
	if q.Mode != "realtime" || q.DeviceScope != "all" || q.Fields["temperature"] != "properties.temperature" {
		t.Fatal(q)
	}
	again, err := CompileQuerySQL(QuerySQL(q))
	if err != nil || !reflect.DeepEqual(q, again) {
		t.Fatalf("round trip mismatch: %+v %+v %v", q, again, err)
	}
	for _, tc := range []struct {
		payload string
		matched bool
	}{
		{`{"deviceId":"d","productId":"smoke","properties":{"temperature":27},"tags":{"room":"O'Brien 1"}}`, true},
		{`{"deviceId":"d","productId":"smoke","properties":{"temperature":20},"event":{"name":"alarm"},"tags":{"room":"O'Brien 1"}}`, true},
		{`{"deviceId":"d","productId":"other","properties":{"temperature":27},"tags":{"room":"O'Brien 1"}}`, false},
		{`{"deviceId":"d","productId":"smoke","properties":{"temperature":"27"},"tags":{"room":"O'Brien 1"}}`, false},
	} {
		out, matched, err := PreviewQuery(q, []byte(tc.payload))
		if err != nil || matched != tc.matched {
			t.Fatalf("%s => %s %v %v", tc.payload, out, matched, err)
		}
	}
	precedence, err := CompileQuerySQL("SELECT * FROM device_reports WHERE deviceId = 'a' OR deviceId = 'b' AND productId = 'p'")
	if err != nil {
		t.Fatal(err)
	}
	if _, matched, err := PreviewQuery(precedence, []byte(`{"deviceId":"a","productId":"other"}`)); err != nil || !matched {
		t.Fatal("AND precedence lost", err)
	}
	scheduled, err := CompileQuerySQL("select name, online from devices where online = TRUE;")
	if err != nil || scheduled.Mode != "interval" || scheduled.IntervalSeconds != 60 {
		t.Fatal(scheduled, err)
	}
}
func TestQuerySQLRejectsExecutableOrUnknownExpressions(t *testing.T) {
	for _, sql := range []string{
		"DELETE FROM devices", "SELECT * FROM secrets", "SELECT accessKey FROM devices", "SELECT raw.password FROM device_reports", "SELECT missing FROM device_reports", "SELECT * FROM devices; DROP TABLE users", "SELECT COUNT(*) FROM devices", "SELECT * FROM devices JOIN users ON id = id", "SELECT * FROM devices ORDER BY id", "SELECT * FROM devices LIMIT 1", "SELECT * FROM devices WHERE online = 'true'", "SELECT * FROM devices WHERE deviceId = 1", "SELECT * FROM device_reports WHERE unknown = 'a'", "SELECT * FROM devices WHERE id IN ()", "SELECT * FROM devices WHERE id = NULL", "SELECT * FROM devices WHERE id IN ('a', NULL)", "SELECT * FROM devices WHERE online > FALSE", "SELECT name AS x, status AS x FROM devices", "SELECT * FROM devices WHERE name = 'unterminated", "SELECT * FROM devices -- comment", "SELECT * FROM devices WHERE updatedAt > 1e999999999", "SELECT * FROM devices WHERE updatedAt > 1+2", "SELECT * FROM devices WHERE id = 'a' OR 1 = 1", "SELECT * FROM devices WHERE name LIKE '%a%'", "SELECT * FROM devices WHERE NOT online = TRUE", "SELECT * FROM devices WHERE name CONTAINS 1", "SELECT *, name FROM devices",
	} {
		if _, err := CompileQuerySQL(sql); err == nil {
			t.Errorf("invalid SQL accepted: %s", sql)
		}
	}
	if _, err := CompileQuerySQL("SELECT * FROM devices WHERE " + strings.Repeat("(", 9) + "online = TRUE" + strings.Repeat(")", 9)); err == nil {
		t.Fatal("unbounded grouping")
	}
	if _, err := CompileQuerySQL(strings.Repeat(" ", 16<<10) + "SELECT * FROM devices"); err == nil {
		t.Fatal("unbounded SQL")
	}
}

func TestQuerySQLQuotedPathsAndFormLimitsRoundTrip(t *testing.T) {
	q := reportQuery()
	q.Fields = map[string]string{"reading": "properties.temp-c", "site": "tags.site:code", "value": "properties.@value"}
	q.Filter = &model.MessageTopicFilter{Field: "properties.temp-c", Operator: "gt", Value: json.Number("26.5")}
	sql := QuerySQL(q)
	if !strings.Contains(sql, `"properties.temp-c"`) || !strings.Contains(sql, `"tags.site:code"`) {
		t.Fatal("unquoted path", sql)
	}
	compiled, err := CompileQuerySQL(sql)
	if err != nil {
		t.Fatal(sql, err)
	}
	compiled.DeviceScope, compiled.DeviceIDs = q.DeviceScope, q.DeviceIDs
	if !reflect.DeepEqual(q, compiled) {
		t.Fatalf("quoted fields changed: %+v %+v", q, compiled)
	}
	out, matched, err := PreviewQuery(compiled, []byte(`{"deviceId":"d","properties":{"temp-c":27,"@value":2},"tags":{"site:code":"a"}}`))
	if err != nil || !matched || string(out) != `{"reading":27,"site":"a","value":2}` {
		t.Fatal(string(out), matched, err)
	}
	for _, sql := range []string{`SELECT * FROM devices WHERE name 'CONTAINS' 'x'`, `SELECT * FROM devices WHERE name '=' 'x'`, `SELECT "password" FROM devices`, `SELECT "properties.unclosed FROM device_reports`} {
		if _, err := CompileQuerySQL(sql); err == nil {
			t.Fatal("invalid SQL accepted", sql)
		}
	}
	q = reportQuery()
	q.Filter = &model.MessageTopicFilter{Logic: "and"}
	for i := 0; i < 5; i++ {
		q.Filter.Children = append(q.Filter.Children, model.MessageTopicFilter{Field: "deviceId", Operator: "eq", Value: strings.Repeat("x", 4096)})
	}
	if ValidateQuery(q) == nil {
		t.Fatal("form generated SQL above its input limit")
	}
	q = reportQuery()
	q.Filter = &model.MessageTopicFilter{Logic: "and"}
	values := make([]any, 100)
	for i := range values {
		values[i] = json.Number("1")
	}
	for i := 0; i < 20; i++ {
		q.Filter.Children = append(q.Filter.Children, model.MessageTopicFilter{Field: "properties.value", Operator: "in", Value: values})
	}
	if ValidateQuery(q) == nil {
		t.Fatal("form generated token count above SQL input limit")
	}
}
func TestQueryPredicatesKeepTypesNullsAndLargeNumbers(t *testing.T) {
	for _, tc := range []struct {
		where, payload string
		matched        bool
	}{
		{"properties.value > 9007199254740992", `{"properties":{"value":9007199254740993}}`, true},
		{"properties.value = 9007199254740993", `{"properties":{"value":9007199254740992}}`, false},
		{"properties.value = 1.25e2", `{"properties":{"value":125}}`, true},
		{"properties.value != 1", `{"properties":{"value":"1"}}`, false},
		{"properties.value NOT IN (1, 2)", `{"properties":{"value":3}}`, true},
		{"properties.value NOT IN (1, 2)", `{"properties":{"value":"3"}}`, false},
		{"properties.value NOT IN (1, 2)", `{"properties":{"value":null}}`, false},
		{"properties.value != 1", `{}`, false},
		{"properties.value IS NULL", `{}`, true},
		{"properties.value IS NULL", `{"properties":{"value":null}}`, true},
		{"properties.value IS NOT NULL", `{"properties":{"value":false}}`, true},
		{"properties.value = FALSE", `{"properties":{"value":false}}`, true},
		{"properties.value = FALSE", `{"properties":{"value":0}}`, false},
		{"tags.label < 'b'", `{"tags":{"label":"a"}}`, true},
	} {
		q, err := CompileQuerySQL("SELECT * FROM device_reports WHERE " + tc.where)
		if err != nil {
			t.Fatal(err)
		}
		_, matched, err := PreviewQuery(q, []byte(tc.payload))
		if err != nil || matched != tc.matched {
			t.Fatalf("%s %s => %v %v", tc.where, tc.payload, matched, err)
		}
	}
	q, err := CompileQuerySQL("SELECT properties.value AS value FROM device_reports WHERE properties.value = 9007199254740993")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(q)
	var decoded model.MessageTopicQuery
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded.Filter.Value.(json.Number); !ok {
		t.Fatal("persisted filter converted to float64")
	}
	output, matched, err := PreviewQuery(decoded, []byte(`{"properties":{"value":9007199254740993}}`))
	if err != nil || !matched || string(output) != `{"value":9007199254740993}` {
		t.Fatal(string(output), matched, err)
	}
}
func TestQueryProjectionScopeAndInputBounds(t *testing.T) {
	q := reportQuery()
	out, matched, err := PreviewQuery(q, []byte(`{"deviceId":"d","properties":{"temperature":26.5},"raw":{"secret":"x"},"unknown":"x"}`))
	if err != nil || !matched || string(out) != `{"deviceId":"d","properties":{"temperature":26.5}}` {
		t.Fatal(string(out), matched, err)
	}
	q.Fields = map[string]string{"reading": "properties.temperature", "missing": "properties.humidity"}
	out, matched, err = PreviewQuery(q, []byte(`{"deviceId":"d","properties":{"temperature":26.5}}`))
	if err != nil || !matched || string(out) != `{"missing":null,"reading":26.5}` {
		t.Fatal(string(out), matched, err)
	}
	for _, input := range []string{`{"deviceId":"other"}`, `{}`} {
		if _, matched, err := PreviewQuery(q, []byte(input)); err != nil || matched {
			t.Fatal("device scope escaped", input, matched, err)
		}
	}
	for _, input := range []string{`[]`, `null`, `{} {}`, `invalid`, strings.Repeat("x", MaxPayload+1)} {
		if _, _, err := PreviewQuery(q, []byte(input)); err == nil {
			t.Fatal("invalid input accepted", input[:min(20, len(input))])
		}
	}
	alarms := q
	alarms.Dataset = "alarms"
	alarms.Fields = nil
	if _, matched, err := PreviewQuery(alarms, []byte(`{"deviceId":"d","source":"video"}`)); err != nil || matched {
		t.Fatal("device scope allowed video", matched, err)
	}
	devices := q
	devices.Dataset = "devices"
	devices.Mode = "interval"
	devices.IntervalSeconds = 60
	devices.Fields = nil
	out, matched, err = PreviewQuery(devices, []byte(`{"id":"d","name":"test","accessKey":"hidden","secretHint":"hidden","onboardingRequestHash":"hidden","connectionStatus":"CONNECTED"}`))
	if err != nil || !matched || strings.Contains(string(out), "hidden") || !strings.Contains(string(out), `"deviceId":"d"`) || !strings.Contains(string(out), "connectionStatus") {
		t.Fatal(string(out), matched, err)
	}
}
func TestQueryValidationBoundsAndUnknownFields(t *testing.T) {
	valid := reportQuery()
	changes := []func(*model.MessageTopicQuery){
		func(q *model.MessageTopicQuery) { q.Dataset = "sql_table" }, func(q *model.MessageTopicQuery) { q.Mode = "interval" }, func(q *model.MessageTopicQuery) { q.IntervalSeconds = 60 }, func(q *model.MessageTopicQuery) { q.DeviceIDs = nil }, func(q *model.MessageTopicQuery) { q.Fields = map[string]string{"x": "password"} }, func(q *model.MessageTopicQuery) { q.Fields = map[string]string{"x.y": "deviceId"} },
		func(q *model.MessageTopicQuery) {
			q.Filter = &model.MessageTopicFilter{Logic: "and", Field: "deviceId", Children: []model.MessageTopicFilter{{Field: "deviceId", Operator: "eq", Value: "d"}}}
		},
		func(q *model.MessageTopicQuery) { q.Filter = &model.MessageTopicFilter{Logic: "and"} },
		func(q *model.MessageTopicQuery) {
			q.Filter = &model.MessageTopicFilter{Field: "deviceId", Operator: "eq", Value: 1}
		},
		func(q *model.MessageTopicQuery) {
			q.Filter = &model.MessageTopicFilter{Field: "properties", Operator: "eq", Value: map[string]any{"x": 1}}
		},
		func(q *model.MessageTopicQuery) {
			q.Filter = &model.MessageTopicFilter{Field: "deviceId", Operator: "is_null", Value: "x"}
		},
		func(q *model.MessageTopicQuery) {
			q.Filter = &model.MessageTopicFilter{Field: "deviceId", Operator: "in", Value: []any{}}
		},
	}
	for i, change := range changes {
		q := *cloneQuery(&valid)
		change(&q)
		if err := ValidateQuery(q); err == nil {
			t.Errorf("invalid query %d accepted %+v", i, q)
		}
	}
}
func TestQueryRuntimeOnePublicationAcrossSources(t *testing.T) {
	ctx := context.Background()
	for _, protocol := range []string{"mqtt", "kafka"} {
		t.Run(protocol, func(t *testing.T) {
			cfg := queryFixture(t, protocol)
			cfg.Topics[0].Query.Fields = map[string]string{"device": "deviceId", "temperature": "properties.temperature"}
			cfg.Topics[0].Query.Filter = &model.MessageTopicFilter{Field: "properties.temperature", Operator: "gte", Value: 26}
			grantKey(&cfg, "t", "a")
			grantKey(&cfg, "t", "b")
			svc := New(newTestStore())
			svc.now = func() time.Time { return time.Unix(1000, 0) }
			svc.SetAccessResolver(func(context.Context, string, string) (MessageTopicIdentity, error) { return keyIdentity(), nil })
			if ok, err := svc.Save(ctx, "t", cfg); !ok || err != nil {
				t.Fatal(ok, err)
			}
			sources := []string{"/iot/parsed/t/p/d/PROPERTY_REPORT"}
			if protocol == "kafka" {
				sources = []string{model.TopicPropertyReport, model.TopicEventReport, model.TopicParsed}
			}
			for _, source := range sources {
				pubs := svc.Publications(ctx, protocol, source, []byte(`{"tenantId":"t","deviceId":"d","properties":{"temperature":27}}`))
				if len(pubs) != 2 || string(pubs[1].Payload) != `{"device":"d","temperature":27}` {
					t.Fatal("missing or duplicate query publication", source, pubs)
				}
				for _, payload := range []string{`{"tenantId":"t","deviceId":"d","properties":{"temperature":20}}`, `{"tenantId":"t","deviceId":"other","properties":{"temperature":27}}`, `{"tenantId":"other","deviceId":"d","properties":{"temperature":27}}`} {
					for _, pub := range svc.Publications(ctx, protocol, source, []byte(payload)) {
						if pub.Topic == Destination("t", cfg.Topics[0]) {
							t.Fatal("nonmatching or foreign message published", payload)
						}
					}
				}
			}
			cfg, _ = svc.Load(ctx, "t")
			cfg.Credentials[0].Status = "revoking"
			if ok, err := svc.Save(ctx, "t", cfg); !ok || err != nil {
				t.Fatal(ok, err)
			}
			if pubs := svc.Publications(ctx, protocol, sources[0], []byte(`{"tenantId":"t","deviceId":"d","properties":{"temperature":27}}`)); len(pubs) != 1 {
				t.Fatal("pending revocation failed to pause", pubs)
			}
		})
	}
}
func TestQueryConfigurationHistoricalBoundaryAndClone(t *testing.T) {
	cfg := queryFixture(t, "kafka")
	if len(cfg.Topics[0].Exposure) != 3 {
		t.Fatal("Kafka report exposure misses event routes")
	}
	q := *cloneQuery(cfg.Topics[0].Query)
	q.DeviceIDs = []string{"other"}
	if err := AccumulateQueryExposure(&cfg, "shared", q); err != nil {
		t.Fatal(err)
	}
	for _, e := range cfg.Topics[0].Exposure {
		if !slices.Equal(e.DeviceIDs, []string{"d", "other"}) {
			t.Fatal("historical exposure narrowed", e)
		}
	}
	if RouteAllowed(cfg.Topics[0], keyIdentity()) {
		t.Fatal("narrow user read historical devices")
	}
	for _, change := range []func(*model.MessageTopicConfig){
		func(c *model.MessageTopicConfig) { c.Topics[0].Exposure = nil },
		func(c *model.MessageTopicConfig) { c.Topics[0].Query = nil },
		func(c *model.MessageTopicConfig) { c.Topics[0].KeyIDs = []string{"k", "k"} },
	} {
		bad := cloneConfig(cfg)
		change(&bad)
		if !errors.Is(Validate("t", bad), ErrInvalidConfig) {
			t.Fatal("invalid query configuration accepted")
		}
	}
	cfg.Topics[0].Query.Filter = &model.MessageTopicFilter{Logic: "and", Children: []model.MessageTopicFilter{{Field: "deviceId", Operator: "in", Value: []any{"d", "e"}}}}
	cfg.Topics[0].Query.Fields = map[string]string{"device": "deviceId"}
	clone := cloneConfig(cfg)
	clone.Topics[0].Query.DeviceIDs[0] = "changed"
	clone.Topics[0].Query.Fields["device"] = "productId"
	clone.Topics[0].Query.Filter.Children[0].Value.([]any)[0] = "changed"
	if cfg.Topics[0].Query.DeviceIDs[0] != "d" || cfg.Topics[0].Query.Fields["device"] != "deviceId" || cfg.Topics[0].Query.Filter.Children[0].Value.([]any)[0] != "d" {
		t.Fatal("query cloned shallowly")
	}
	mqtt := queryFixture(t, "mqtt")
	mqtt.Topics[0].Topic = "/device"
	if err := Validate("t", mqtt); err != nil || Destination("t", mqtt.Topics[0]) != MQTTPrefix("t")+"device" {
		t.Fatal("simple MQTT path rejected", err)
	}
	mqtt.Topics[0].Topic = MQTTPrefix("other") + "device"
	if Validate("t", mqtt) == nil {
		t.Fatal("foreign tenant path accepted")
	}
}
