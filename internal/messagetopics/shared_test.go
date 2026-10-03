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

func sharedFixture(t *testing.T, protocol string) model.MessageTopicConfig {
	t.Helper()
	source := "mqtt.parsed"
	if protocol == "kafka" {
		source = "kafka.property-report"
	}
	cfg := model.MessageTopicConfig{Topics: []model.MessageTopicRoute{{ID: "shared", Name: "共享消息", Protocol: protocol, Topic: "partner-events", Enabled: true}}, Rules: []model.MessageTopicRule{{ID: "rule", Name: "设备事件", TopicID: "shared", SourceID: source, Enabled: true, DeviceScope: "selected", DeviceIDs: []string{"d"}, Format: "json", Fields: map[string]string{"device": "deviceId", "type": "messageType"}}}}
	if err := AccumulateExposure(&cfg, cfg.Rules[0]); err != nil {
		t.Fatal(err)
	}
	return cfg
}
func sharedIdentity() MessageTopicIdentity {
	return MessageTopicIdentity{Permissions: map[string]bool{"menu:devices": true, "menu:messageTopics": true}, DeviceScope: "selected", DeviceIDs: []string{"d"}, Version: "v1"}
}
func addSharedAccount(cfg *model.MessageTopicConfig, tenant, id string, read, write bool) {
	a := model.MessageTopicAccount{ID: id, Name: id, Username: id, Enabled: true, DeviceScope: "selected", DeviceIDs: []string{"d"}}
	c := model.MessageTopicCredential{ID: "credential_" + id, AccountID: id, Protocol: cfg.Topics[0].Protocol, Username: "broker_" + id, Status: "active", AccessVersion: "v1", CreatedAt: 900, ExpiresAt: 2000}
	if read {
		a.TopicIDs = []string{"shared"}
		c.Topics = []string{SharedDestination(tenant, cfg.Topics[0])}
	}
	if write {
		a.PublishTopicIDs = []string{"shared"}
		c.PublishTopics = []string{SharedDestination(tenant, cfg.Topics[0])}
	}
	cfg.Accounts = append(cfg.Accounts, a)
	cfg.Credentials = append(cfg.Credentials, c)
}

func TestRulePreviewFormatsPathsAndLimits(t *testing.T) {
	payload := []byte(`{"deviceId":"d","samples":[{"value":9007199254740993,"name":"烟感"}],"null":null}`)
	for _, tc := range []struct {
		name string
		rule model.MessageTopicRule
		want string
	}{
		{"original", model.MessageTopicRule{Format: "original"}, string(payload)},
		{"json", model.MessageTopicRule{Format: "json", Fields: map[string]string{"reading": "samples.0.value", "name": "samples.0.name"}}, `{"name":"烟感","reading":9007199254740993}`},
		{"text", model.MessageTopicRule{Format: "text", Template: "设备 {{ deviceId }}\n{{samples.0.name}}={{samples.0.value}}/{{null}}"}, "设备 d\n烟感=9007199254740993/null"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := PreviewRule(tc.rule, payload)
			if err != nil || string(got) != tc.want {
				t.Fatalf("preview=%s want=%s err=%v", got, tc.want, err)
			}
		})
	}
	for _, rule := range []model.MessageTopicRule{
		{Format: "json", Fields: map[string]string{"x": "samples.1.value"}},
		{Format: "json", Fields: map[string]string{"x": "samples.-1.value"}},
		{Format: "text", Template: "{{missing}}"},
		{Format: "text", Template: "{{deviceId"},
		{Format: "text", Template: "{{samples[0]}}"},
		{Format: "original", Fields: map[string]string{"x": "deviceId"}},
		{Format: "json"},
		{Format: "unknown"},
		{Format: "text", Template: strings.Repeat("x", (64<<10)+1)},
	} {
		if _, err := PreviewRule(rule, payload); err == nil {
			t.Fatalf("invalid preview accepted: %+v", rule)
		}
	}
	for _, input := range [][]byte{[]byte(`{} {}`), []byte(`not json`), []byte(strings.Repeat("x", MaxRulePayload+1))} {
		if _, err := PreviewRule(model.MessageTopicRule{Format: "original"}, input); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
	large, _ := json.Marshal(map[string]string{"value": strings.Repeat("x", 70<<10)})
	if _, err := PreviewRule(model.MessageTopicRule{Format: "text", Template: strings.Repeat("{{value}}", 4)}, large); err == nil {
		t.Fatal("expanded output size was unbounded")
	}
}

func TestSharedExposureIsHistoricalAndOnlyWidens(t *testing.T) {
	cfg := sharedFixture(t, "mqtt")
	rule := cfg.Rules[0]
	rule.DeviceIDs = []string{"other"}
	if err := AccumulateExposure(&cfg, rule); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.Topics[0].Exposure[0].DeviceIDs, []string{"d", "other"}) {
		t.Fatal(cfg.Topics[0].Exposure)
	}
	identity := sharedIdentity()
	account := model.MessageTopicAccount{Enabled: true, DeviceScope: "all"}
	if RouteAllowed(cfg, "shared", identity, account) {
		t.Fatal("current narrow scope read broader broker history")
	}
	identity.DeviceIDs = append(identity.DeviceIDs, "other")
	if !RouteAllowed(cfg, "shared", identity, account) {
		t.Fatal("fully covered historical exposure denied")
	}
	cfg.Rules = nil
	if len(cfg.Topics[0].Exposure) != 1 {
		t.Fatal("deleting rule erased history boundary")
	}
	rule.DeviceScope, rule.DeviceIDs = "all", nil
	if err := AccumulateExposure(&cfg, rule); err != nil {
		t.Fatal(err)
	}
	rule.DeviceScope, rule.DeviceIDs = "selected", []string{"d"}
	if err := AccumulateExposure(&cfg, rule); err != nil {
		t.Fatal(err)
	}
	if cfg.Topics[0].Exposure[0].DeviceScope != "all" || len(cfg.Topics[0].Exposure[0].DeviceIDs) != 0 {
		t.Fatal("all exposure narrowed")
	}
	if RouteAllowed(cfg, "shared", identity, account) {
		t.Fatal("selected user read prior all-device history")
	}
}

func TestSharedTopicPureManualAndSensitiveSourcePermissions(t *testing.T) {
	cfg := sharedFixture(t, "kafka")
	cfg.Rules = nil
	cfg.Topics[0].Exposure = nil
	i := MessageTopicIdentity{Permissions: map[string]bool{"menu:messageTopics": true}, Version: "v"}
	a := model.MessageTopicAccount{Enabled: true, DeviceScope: "selected"}
	if !RouteAllowed(cfg, "shared", i, a) || !RoutePublishAllowed(cfg, "shared", i, a) {
		t.Fatal("manual messages incorrectly require a device ID or device menu")
	}
	cfg.Topics[0].Exposure = []model.MessageTopicExposure{{SourceID: "kafka.alarm-ai-analysis", DeviceScope: "selected", DeviceIDs: []string{"d"}}}
	i.DeviceScope, i.DeviceIDs = "all", nil
	a.DeviceScope = "all"
	i.Permissions["menu:devices"], i.Permissions["menu:alarms"] = true, true
	if RouteAllowed(cfg, "shared", i, a) {
		t.Fatal("analysis history escaped knowledge permission")
	}
	i.Permissions["menu:knowledge"] = true
	if RouteAllowed(cfg, "shared", i, a) {
		t.Fatal("analysis history escaped camera permission")
	}
	i.Permissions["menu:cameras"] = true
	if !RouteAllowed(cfg, "shared", i, a) {
		t.Fatal("analysis fully authorized reader denied")
	}
	cfg.Topics[0].Exposure[0].SourceID = "kafka.alarm-raised"
	delete(i.Permissions, "menu:cameras")
	if RouteAllowed(cfg, "shared", i, a) {
		t.Fatal("historical camera details escaped camera permission")
	}
	i.Permissions["menu:cameras"] = true
	if !RouteAllowed(cfg, "shared", i, a) {
		t.Fatal("alarm fully authorized reader denied")
	}
	legacy := model.MessageTopicConfig{}
	a.DeviceScope, a.DeviceIDs = "selected", []string{"d"}
	if RouteAllowed(legacy, "kafka.video-alarm", i, a) {
		t.Fatal("legacy video subscription accepted a selected-device account")
	}
	a.DeviceScope, a.DeviceIDs = "all", nil
	if !RouteAllowed(legacy, "kafka.video-alarm", i, a) {
		t.Fatal("legacy video fully authorized reader denied")
	}
}

func TestSharedRulesPublishOnceAcrossAccountsAndFilterDevices(t *testing.T) {
	ctx := context.Background()
	for _, protocol := range []string{"mqtt", "kafka"} {
		t.Run(protocol, func(t *testing.T) {
			cfg := sharedFixture(t, protocol)
			addSharedAccount(&cfg, "t", "a", true, false)
			addSharedAccount(&cfg, "t", "b", true, true)
			svc := New(newTestStore())
			svc.now = func() time.Time { return time.Unix(1000, 0) }
			svc.SetAccessResolver(func(context.Context, string, string) (MessageTopicIdentity, error) { return sharedIdentity(), nil })
			if ok, err := svc.Save(ctx, "t", cfg); !ok || err != nil {
				t.Fatal(ok, err)
			}
			source := "/iot/parsed/t/p/d/PROPERTY_REPORT"
			if protocol == "kafka" {
				source = model.TopicPropertyReport
			}
			got, err := svc.Publications(ctx, protocol, source, body("t"))
			if err != nil || len(got) != 2 {
				t.Fatal("shared rule duplicated for accounts", got, err)
			}
			if got[0].Topic != source || got[1].Topic != SharedDestination("t", cfg.Topics[0]) || string(got[1].Payload) != `{"device":"d","type":"PROPERTY_REPORT"}` {
				t.Fatal("wrong source or projection", got)
			}
			other := []byte(`{"tenantId":"t","deviceId":"other","messageType":"PROPERTY_REPORT"}`)
			got, err = svc.Publications(ctx, protocol, source, other)
			if err != nil || len(got) != 1 {
				t.Fatal("rule device filter not applied", got, err)
			}
			cfg, _ = svc.Load(ctx, "t")
			cfg.Accounts = nil
			cfg.Credentials = nil
			if ok, err := svc.Save(ctx, "t", cfg); !ok || err != nil {
				t.Fatal(ok, err)
			}
			svc.SetAccessResolver(nil)
			got, err = svc.Publications(ctx, protocol, source, body("t"))
			if err != nil || len(got) != 2 {
				t.Fatal("no-account topic failed to publish for broker tools", got, err)
			}
		})
	}
}

func TestSharedRulePausesUntilUnsafeCredentialRevocationConfirmed(t *testing.T) {
	ctx := context.Background()
	for _, scenario := range []string{"revoking", "provisioning", "late provision after revoke", "active provision in flight", "expired", "account disabled", "account expired", "access version", "user scope", "account scope", "resolver failure", "revoked", "writer only"} {
		t.Run(scenario, func(t *testing.T) {
			cfg := sharedFixture(t, "kafka")
			addSharedAccount(&cfg, "t", "reader", true, false)
			identity := sharedIdentity()
			want := false
			resolveErr := error(nil)
			switch scenario {
			case "revoking", "provisioning", "revoked":
				cfg.Credentials[0].Status = scenario
				want = scenario == "revoked"
			case "late provision after revoke":
				cfg.Credentials[0].Status = "revoked"
				cfg.Credentials[0].Provisioning = true
			case "active provision in flight":
				cfg.Credentials[0].Provisioning = true
			case "expired":
				cfg.Credentials[0].ExpiresAt = 999
			case "account disabled":
				cfg.Accounts[0].Enabled = false
			case "account expired":
				cfg.Accounts[0].ExpiresAt = 999
			case "access version":
				identity.Version = "changed"
			case "user scope":
				identity.DeviceIDs = []string{"other"}
			case "account scope":
				cfg.Accounts[0].DeviceIDs = []string{"other"}
			case "resolver failure":
				resolveErr = errors.New("unavailable")
			case "writer only":
				cfg.Accounts = nil
				cfg.Credentials = nil
				addSharedAccount(&cfg, "t", "writer", false, true)
				identity.DeviceScope = "none"
				delete(identity.Permissions, "menu:devices")
				want = true
			}
			svc := New(newTestStore())
			svc.now = func() time.Time { return time.Unix(1000, 0) }
			svc.SetAccessResolver(func(context.Context, string, string) (MessageTopicIdentity, error) { return identity, resolveErr })
			if ok, err := svc.Save(ctx, "t", cfg); !ok || err != nil {
				t.Fatal(ok, err)
			}
			got, err := svc.Publications(ctx, "kafka", model.TopicPropertyReport, body("t"))
			if err != nil || (len(got) == 2) != want {
				t.Fatalf("publication=%v wantShared=%v err=%v", got, want, err)
			}
		})
	}
}

func TestRuleFailurePreservesDefaultAndOtherRulePayload(t *testing.T) {
	cfg := sharedFixture(t, "kafka")
	second := cfg.Rules[0]
	second.ID = "text-rule"
	second.Format = "text"
	second.Fields = nil
	second.Template = "设备={{deviceId}}"
	cfg.Rules = append(cfg.Rules, second)
	cfg.Rules[0].Fields = map[string]string{"value": "missing"}
	svc := New(newTestStore())
	ctx := context.Background()
	if ok, err := svc.Save(ctx, "t", cfg); !ok || err != nil {
		t.Fatal(ok, err)
	}
	got, err := svc.Publications(ctx, "kafka", model.TopicPropertyReport, body("t"))
	if err != nil || len(got) != 2 || got[0].Topic != model.TopicPropertyReport || string(got[1].Payload) != "设备=d" {
		t.Fatal("one failed rule suppressed other publications", got, err)
	}
	recorder := &busRecorder{}
	if err := svc.WrapBus(recorder).Publish(ctx, model.TopicPropertyReport, "key", body("t")); err != nil {
		t.Fatal("optional rule failure propagated to the source publisher", err)
	}
	if len(recorder.topics) != 2 || recorder.topics[0] != model.TopicPropertyReport || string(recorder.payload) != "设备=d" {
		t.Fatal("wrapper dropped successful publications", recorder.topics)
	}
	manual := []byte("任意文本，不是 JSON")
	got, err = svc.Publications(ctx, "kafka", SharedDestination("t", cfg.Topics[0]), manual)
	if err != nil || len(got) != 1 || !slices.Equal(got[0].Payload, manual) {
		t.Fatal("manual publish was parsed or retriggered", got, err)
	}
}

func TestSharedValidationRetiredNamesAndSnapshots(t *testing.T) {
	valid := sharedFixture(t, "mqtt")
	addSharedAccount(&valid, "t", "rw", true, true)
	for _, tc := range []struct {
		name   string
		change func(*model.MessageTopicConfig)
	}{
		{"exposure missing", func(c *model.MessageTopicConfig) { c.Topics[0].Exposure = nil }},
		{"scope not covered", func(c *model.MessageTopicConfig) { c.Rules[0].DeviceIDs = []string{"other"} }},
		{"protocol mismatch", func(c *model.MessageTopicConfig) { c.Rules[0].SourceID = "kafka.parsed" }},
		{"invalid source", func(c *model.MessageTopicConfig) { c.Rules[0].SourceID = "mqtt.state" }},
		{"retired address", func(c *model.MessageTopicConfig) {
			c.RetiredTopics = []string{"mqtt:" + SharedDestination("t", c.Topics[0])}
		}},
		{"foreign full address", func(c *model.MessageTopicConfig) { c.Topics[0].Topic = MQTTPrefix("other") + "name" }},
		{"wildcard", func(c *model.MessageTopicConfig) { c.Topics[0].Topic = "events/#" }},
		{"managed namespace", func(c *model.MessageTopicConfig) { c.Topics[0].Topic = "managed/identity/hash" }},
		{"old source mixed", func(c *model.MessageTopicConfig) { c.Topics[0].SourceID = "mqtt.parsed" }},
		{"default route bypass", func(c *model.MessageTopicConfig) {
			c.Overrides = map[string]model.MessageTopicOverride{"mqtt.parsed": {Enabled: true, Topic: SharedDestination("t", c.Topics[0])}}
		}},
		{"unauthorized write", func(c *model.MessageTopicConfig) { c.Accounts[0].PublishTopicIDs = nil }},
		{"old route write", func(c *model.MessageTopicConfig) { c.Accounts[0].PublishTopicIDs = []string{"mqtt.parsed"} }},
		{"foreign credential", func(c *model.MessageTopicConfig) {
			c.Credentials[0].PublishTopics = []string{MQTTPrefix("other") + "private"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := cloneConfig(valid)
			tc.change(&cfg)
			if !errors.Is(Validate("t", cfg), ErrInvalidConfig) {
				t.Fatal("invalid shared topic configuration accepted")
			}
		})
	}
	cfg := cloneConfig(valid)
	cfg.Credentials[0].Status = "revoking"
	cfg.Topics = nil
	cfg.Rules = nil
	cfg.Accounts = nil
	if err := Validate("t", cfg); err != nil {
		t.Fatal("orphaned shared credential cannot be durably revoked", err)
	}
	clone := cloneConfig(valid)
	clone.Rules[0].DeviceIDs[0] = "changed"
	clone.Rules[0].Fields["type"] = "changed"
	clone.Topics[0].Exposure[0].DeviceIDs[0] = "changed"
	clone.Accounts[0].PublishTopicIDs[0] = "changed"
	clone.Credentials[0].PublishTopics[0] = "changed"
	if reflect.DeepEqual(clone, valid) || valid.Rules[0].DeviceIDs[0] != "d" || valid.Topics[0].Exposure[0].DeviceIDs[0] != "d" || valid.Rules[0].Fields["type"] != "messageType" || valid.Accounts[0].PublishTopicIDs[0] != "shared" || valid.Credentials[0].PublishTopics[0] == "changed" {
		t.Fatal("shared configuration clone aliases authorization state")
	}
}

func TestMQTTRenderCannotBypassSharedRuleExposure(t *testing.T) {
	cfg := sharedFixture(t, "mqtt")
	cfg.Overrides = map[string]model.MessageTopicOverride{"mqtt.parsed": {Enabled: true, Topic: MQTTPrefix("t") + "{productId}"}}
	svc := New(newTestStore())
	ctx := context.Background()
	if ok, err := svc.Save(ctx, "t", cfg); !ok || err != nil {
		t.Fatal(ok, err)
	}
	payload := []byte(`{"tenantId":"t","deviceId":"other","productId":"partner-events"}`)
	publications, err := svc.Publications(ctx, "mqtt", "/iot/parsed/t/partner-events/other/PROPERTY_REPORT", payload)
	if err == nil || len(publications) != 0 {
		t.Fatal("ordinary route published unauthorized content to shared topic", publications, err)
	}
}

func TestMQTTCachedOverrideCannotEnterNewOrRetiredSharedAddress(t *testing.T) {
	ctx := context.Background()
	for _, retired := range []bool{false, true} {
		repo := newTestStore()
		sender, writer := New(repo), New(repo)
		cfg := model.MessageTopicConfig{Overrides: map[string]model.MessageTopicOverride{"mqtt.parsed": {Enabled: true, Topic: MQTTPrefix("t") + "{productId}"}}}
		if ok, err := writer.Save(ctx, "t", cfg); !ok || err != nil {
			t.Fatal(ok, err)
		}
		payload := []byte(`{"tenantId":"t","deviceId":"other","productId":"partner-events"}`)
		source := "/iot/parsed/t/partner-events/other/PROPERTY_REPORT"
		if target, enabled, err := sender.Resolve(ctx, "mqtt", source, payload); err != nil || !enabled || target != MQTTPrefix("t")+"partner-events" {
			t.Fatal("cannot warm override cache", target, enabled, err)
		}
		cfg, _ = writer.Load(ctx, "t")
		if retired {
			cfg.RetiredTopics = []string{"mqtt:" + MQTTPrefix("t") + "partner-events"}
		} else {
			cfg.Topics = sharedFixture(t, "mqtt").Topics
		}
		if ok, err := writer.Save(ctx, "t", cfg); !ok || err != nil {
			t.Fatal(ok, err)
		}
		if target, enabled, err := sender.Resolve(ctx, "mqtt", source, payload); err == nil || enabled {
			t.Fatal("cached override entered reserved address", retired, target, enabled, err)
		}
	}
}

func TestReserveOverrideTargetsOnlyWidensHistory(t *testing.T) {
	oldKafka := model.MessageTopicReservedRoute{SourceID: "kafka.property-report", Topic: KafkaPrefix("t") + "old.properties"}
	oldMQTT := model.MessageTopicReservedRoute{SourceID: "mqtt.parsed", Topic: MQTTPrefix("t") + "{productId}/{deviceId}"}
	previous := model.MessageTopicConfig{
		RoutingHistory: []model.MessageTopicReservedRoute{oldKafka},
		Overrides: map[string]model.MessageTopicOverride{
			oldKafka.SourceID: {Enabled: true, Topic: oldKafka.Topic},
			oldMQTT.SourceID:  {Enabled: false, Topic: oldMQTT.Topic},
		},
	}
	currentKafka := model.MessageTopicReservedRoute{SourceID: oldKafka.SourceID, Topic: KafkaPrefix("t") + "new.properties"}
	cfg := model.MessageTopicConfig{Overrides: map[string]model.MessageTopicOverride{
		currentKafka.SourceID: {Enabled: true, Topic: currentKafka.Topic},
		"mqtt.parsed":         {Enabled: true},
		"kafka.event-report":  {Enabled: true, Topic: model.TopicEventReport},
	}}
	ReserveOverrideTargets(&cfg, previous)
	want := []model.MessageTopicReservedRoute{oldKafka, oldMQTT, currentKafka}
	if !slices.Equal(cfg.RoutingHistory, want) {
		t.Fatal("rename/reset lost old routes or reserved default routes", cfg.RoutingHistory)
	}
	ReserveOverrideTargets(&cfg, previous)
	if !slices.Equal(cfg.RoutingHistory, want) {
		t.Fatal("repeated reservation duplicated history", cfg.RoutingHistory)
	}
	if err := Validate("t", cfg); err != nil {
		t.Fatal("ordinary route cannot keep publishing to its own history", err)
	}
	deleted := model.MessageTopicConfig{}
	ReserveOverrideTargets(&deleted, cfg)
	if !slices.Equal(deleted.RoutingHistory, want) {
		t.Fatal("deletion dropped stored history", deleted.RoutingHistory)
	}
	cloned := cloneConfig(cfg)
	cloned.RoutingHistory[0].Topic = "changed"
	deleted.RoutingHistory[0].Topic = "also-changed"
	if cfg.RoutingHistory[0] != oldKafka || previous.RoutingHistory[0] != oldKafka {
		t.Fatal("route history aliases another configuration")
	}
}

func TestSharedTopicsCannotReuseResolvedOrHistoricalOverrideDestinations(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, protocol, sourceID, source, target, destination string
	}{
		{"kafka", "kafka", "kafka.property-report", model.TopicPropertyReport, KafkaPrefix("t") + "old.properties", KafkaPrefix("t") + "old.properties"},
		{"mqtt static", "mqtt", "mqtt.parsed", "/iot/parsed/t/product/d/PROPERTY_REPORT", MQTTPrefix("t") + "old.properties", MQTTPrefix("t") + "old.properties"},
		{"mqtt template", "mqtt", "mqtt.parsed", "/iot/parsed/t/product/d/PROPERTY_REPORT", MQTTPrefix("t") + "events.[v1]/{productId}/{deviceId}", MQTTPrefix("t") + "events.[v1]/product/d"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newTestStore()
			sender, writer := New(repo), New(repo)
			cfg := model.MessageTopicConfig{Overrides: map[string]model.MessageTopicOverride{tc.sourceID: {Enabled: true, Topic: tc.target}}}
			ReserveOverrideTargets(&cfg, model.MessageTopicConfig{})
			if ok, err := writer.Save(ctx, "t", cfg); !ok || err != nil {
				t.Fatal(ok, err)
			}
			payload := []byte(`{"tenantId":"t","productId":"product","deviceId":"d","messageType":"PROPERTY_REPORT"}`)
			pending, err := sender.Publications(ctx, tc.protocol, tc.source, payload)
			if err != nil || len(pending) != 1 || pending[0].Topic != tc.destination {
				t.Fatal("could not prepare old publication before reset", pending, err)
			}
			previous, _ := writer.Load(ctx, "t")
			cfg = cloneConfig(previous)
			delete(cfg.Overrides, tc.sourceID)
			ReserveOverrideTargets(&cfg, previous)
			if ok, err := writer.Save(ctx, "t", cfg); !ok || err != nil {
				t.Fatal("reset failed", ok, err)
			}
			cfg, _ = writer.Load(ctx, "t")
			cfg.Topics = []model.MessageTopicRoute{{ID: "shared", Name: "共享消息", Protocol: tc.protocol, Topic: pending[0].Topic, Enabled: true}}
			if ok, err := writer.Save(ctx, "t", cfg); ok || !errors.Is(err, ErrInvalidConfig) {
				t.Fatal("new shared topic can receive history or a resolved in-flight old publication", ok, err)
			}
			cfg.Topics[0].Topic = "unrelated-shared"
			if ok, err := writer.Save(ctx, "t", cfg); !ok || err != nil {
				t.Fatal("unrelated shared address was blocked", ok, err)
			}
		})
	}
}

func TestHistoricalOverrideTemplateValidationAndLiteralMatching(t *testing.T) {
	reserved := model.MessageTopicReservedRoute{SourceID: "mqtt.parsed", Topic: MQTTPrefix("t") + "events.[v1]/device-{deviceId}/suffix"}
	for _, tc := range []struct {
		target string
		denied bool
	}{
		{"events.[v1]/device-d/suffix", true},
		{"events.[v1]/device-future-device/suffix", true},
		{"events.[v1]/device-d/extra/suffix", true},
		{"events.xv1/device-d/suffix", false},
		{"events.[v1]/device-d/suffix-more", false},
	} {
		cfg := model.MessageTopicConfig{RoutingHistory: []model.MessageTopicReservedRoute{reserved}, Topics: []model.MessageTopicRoute{{ID: "shared", Name: "共享消息", Protocol: "mqtt", Topic: tc.target, Enabled: true}}}
		err := Validate("t", cfg)
		if errors.Is(err, ErrInvalidConfig) != tc.denied {
			t.Fatalf("target=%s denied=%v err=%v", tc.target, tc.denied, err)
		}
	}
	for _, history := range [][]model.MessageTopicReservedRoute{
		{{SourceID: "missing", Topic: reserved.Topic}},
		{{SourceID: "mqtt.state", Topic: reserved.Topic}},
		{{SourceID: "mqtt.parsed", Topic: MQTTPrefix("other") + "events"}},
		{{SourceID: "mqtt.parsed", Topic: MQTTPrefix("t") + "{missing}"}},
		{{SourceID: "mqtt.parsed", Topic: MQTTPrefix("t") + "#"}},
		{{SourceID: "kafka.property-report", Topic: model.TopicPropertyReport}},
		{reserved, reserved},
	} {
		if err := Validate("t", model.MessageTopicConfig{RoutingHistory: history}); !errors.Is(err, ErrInvalidConfig) {
			t.Fatal("invalid history accepted", history, err)
		}
	}
}
