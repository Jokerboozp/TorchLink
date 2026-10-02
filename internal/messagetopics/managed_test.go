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

type managedTestStore struct {
	*testStore
	alarm    model.Alarm
	alarmErr error
}

func (r *managedTestStore) GetAlarm(_ context.Context, tenant, id string) (model.Alarm, error) {
	if r.alarmErr != nil {
		return model.Alarm{}, r.alarmErr
	}
	if r.alarm.TenantID != tenant || r.alarm.ID != id {
		return model.Alarm{}, errors.New("not found")
	}
	return r.alarm, nil
}

func managedFixture(tenant, source string) model.MessageTopicConfig {
	protocol := strings.Split(source, ".")[0]
	cfg := model.MessageTopicConfig{
		Topics:      []model.MessageTopicRoute{{ID: "custom", Name: "外部数据", SourceID: source, Topic: "data", Enabled: true}},
		Accounts:    []model.MessageTopicAccount{{ID: "account", Name: "对接方", Username: "reader", Enabled: true, TopicIDs: []string{"custom"}, DeviceScope: "selected", DeviceIDs: []string{"d"}, SecretHash: "hashed", CreatedAt: 1}},
		Credentials: []model.MessageTopicCredential{{ID: "credential", AccountID: "account", Protocol: protocol, Username: "broker-user", AccessVersion: "v1", Status: "active", ExpiresAt: 2000}},
	}
	cfg.Credentials[0].Topics = []string{Destination(tenant, "custom", cfg.Credentials[0])}
	return cfg
}

func allowedIdentity() MessageTopicIdentity {
	return MessageTopicIdentity{Permissions: map[string]bool{"menu:devices": true, "menu:alarms": true}, DeviceScope: "selected", DeviceIDs: []string{"d", "other"}, Version: "v1"}
}

func TestManagedPublicationRequiresCurrentAccountUserScopeAndCredential(t *testing.T) {
	ctx := context.Background()
	for _, protocol := range []string{"mqtt", "kafka"} {
		t.Run(protocol, func(t *testing.T) {
			sourceID, source := "mqtt.parsed", "/iot/parsed/t/p/d/PROPERTY_REPORT"
			if protocol == "kafka" {
				sourceID, source = "kafka.property-report", model.TopicPropertyReport
			}
			base := managedFixture("t", sourceID)
			for _, tc := range []struct {
				name   string
				change func(*model.MessageTopicConfig, *MessageTopicIdentity)
				want   bool
			}{
				{"allowed intersection", func(*model.MessageTopicConfig, *MessageTopicIdentity) {}, true},
				{"user selected excludes", func(_ *model.MessageTopicConfig, i *MessageTopicIdentity) { i.DeviceIDs = []string{"other"} }, false},
				{"account selected excludes", func(c *model.MessageTopicConfig, _ *MessageTopicIdentity) {
					c.Accounts[0].DeviceIDs = []string{"other"}
				}, false},
				{"user no devices", func(_ *model.MessageTopicConfig, i *MessageTopicIdentity) { i.DeviceScope = "none" }, false},
				{"user no menu", func(_ *model.MessageTopicConfig, i *MessageTopicIdentity) { delete(i.Permissions, "menu:devices") }, false},
				{"changed access version", func(_ *model.MessageTopicConfig, i *MessageTopicIdentity) { i.Version = "v2" }, false},
				{"blank access version", func(_ *model.MessageTopicConfig, i *MessageTopicIdentity) { i.Version = "" }, false},
				{"account disabled", func(c *model.MessageTopicConfig, _ *MessageTopicIdentity) { c.Accounts[0].Enabled = false }, false},
				{"account expired", func(c *model.MessageTopicConfig, _ *MessageTopicIdentity) { c.Accounts[0].ExpiresAt = 1000 }, false},
				{"credential expired", func(c *model.MessageTopicConfig, _ *MessageTopicIdentity) { c.Credentials[0].ExpiresAt = 1000 }, false},
				{"credential provisioning", func(c *model.MessageTopicConfig, _ *MessageTopicIdentity) { c.Credentials[0].Status = "provisioning" }, false},
				{"credential revoking", func(c *model.MessageTopicConfig, _ *MessageTopicIdentity) { c.Credentials[0].Status = "revoking" }, false},
				{"route disabled", func(c *model.MessageTopicConfig, _ *MessageTopicIdentity) { c.Topics[0].Enabled = false }, false},
				{"all scope is still narrowed by account", func(c *model.MessageTopicConfig, i *MessageTopicIdentity) {
					i.DeviceScope = "all"
					c.Accounts[0].DeviceIDs = []string{"other"}
				}, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					repo := newTestStore()
					svc := New(repo)
					svc.now = func() time.Time { return time.Unix(1000, 0) }
					cfg, identity := cloneConfig(base), allowedIdentity()
					tc.change(&cfg, &identity)
					svc.SetAccessResolver(func(_ context.Context, tenant, username string) (MessageTopicIdentity, error) {
						if tenant != "t" || username != "reader" {
							t.Fatal("wrong identity", tenant, username)
						}
						return identity, nil
					})
					if ok, err := svc.Save(ctx, "t", cfg); !ok || err != nil {
						t.Fatal(ok, err)
					}
					got, err := svc.ManagedDestinations(ctx, protocol, source, body("t"))
					if err != nil || (len(got) > 0) != tc.want {
						t.Fatalf("targets=%v want=%v err=%v", got, tc.want, err)
					}
					if tc.want && !reflect.DeepEqual(got, base.Credentials[0].Topics) {
						t.Fatal("destination differs", got)
					}
				})
			}
		})
	}
}

func TestManagedPublicationSeparateCredentialsAndTenantAndFreshRevocation(t *testing.T) {
	ctx := context.Background()
	repo := newTestStore()
	svc := New(repo)
	svc.now = func() time.Time { return time.Unix(1000, 0) }
	svc.SetAccessResolver(func(context.Context, string, string) (MessageTopicIdentity, error) { return allowedIdentity(), nil })
	cfg := managedFixture("t", "kafka.property-report")
	otherCredential := cfg.Credentials[0]
	otherCredential.ID = "second"
	otherCredential.AccountID = "second-account"
	otherAccount := cfg.Accounts[0]
	otherAccount.ID = otherCredential.AccountID
	cfg.Accounts = append(cfg.Accounts, otherAccount)
	otherCredential.Topics = []string{Destination("t", "custom", otherCredential)}
	cfg.Credentials = append(cfg.Credentials, otherCredential)
	if ok, err := svc.Save(ctx, "t", cfg); !ok || err != nil {
		t.Fatal(ok, err)
	}
	targets, err := svc.Destinations(ctx, "kafka", model.TopicPropertyReport, body("t"))
	if err != nil || len(targets) != 3 || targets[0] != model.TopicPropertyReport || targets[1] == targets[2] {
		t.Fatal(targets, err)
	}
	other, err := svc.Destinations(ctx, "kafka", model.TopicPropertyReport, body("other"))
	if err != nil || !reflect.DeepEqual(other, []string{model.TopicPropertyReport}) {
		t.Fatal("cross tenant publication", other, err)
	}
	// Another replica changes durable state while this service has warm cache.
	second := New(repo)
	latest, _ := second.Load(ctx, "t")
	latest.Credentials[0].Status = "revoking"
	latest.Credentials[1].Status = "revoking"
	if ok, err := second.Save(ctx, "t", latest); !ok || err != nil {
		t.Fatal(ok, err)
	}
	targets, err = svc.ManagedDestinations(ctx, "kafka", model.TopicPropertyReport, body("t"))
	if err != nil || len(targets) != 0 {
		t.Fatal("cached authorization outlived revocation", targets, err)
	}
	if Destination("a/b", "custom", otherCredential) == Destination("a_b", "custom", otherCredential) {
		t.Fatal("tenant destination collision")
	}
}

func TestTopicDeletionSuppressesBuiltinButPreservesOtherCustomChannels(t *testing.T) {
	ctx := context.Background()
	repo := newTestStore()
	svc := New(repo)
	svc.now = func() time.Time { return time.Unix(1000, 0) }
	svc.SetAccessResolver(func(context.Context, string, string) (MessageTopicIdentity, error) { return allowedIdentity(), nil })
	cfg := managedFixture("t", "kafka.property-report")
	cfg.Deleted = []string{"kafka.property-report"}
	if ok, err := svc.Save(ctx, "t", cfg); !ok || err != nil {
		t.Fatal(ok, err)
	}
	targets, err := svc.Destinations(ctx, "kafka", model.TopicPropertyReport, body("t"))
	if err != nil || !reflect.DeepEqual(targets, cfg.Credentials[0].Topics) {
		t.Fatal(targets, err)
	}
	cfg, _ = svc.Load(ctx, "t")
	cfg.Topics = nil
	cfg.Accounts = nil
	cfg.Credentials[0].Status = "revoking"
	if ok, err := svc.Save(ctx, "t", cfg); !ok || err != nil {
		t.Fatal("revoking record must survive deletion", ok, err)
	}
	targets, err = svc.Destinations(ctx, "kafka", model.TopicPropertyReport, body("t"))
	if err != nil || len(targets) != 0 {
		t.Fatal(targets, err)
	}
	internal, err := svc.Destinations(ctx, "kafka", model.TopicRaw, []byte("not JSON"))
	if err != nil || !reflect.DeepEqual(internal, []string{model.TopicRaw}) {
		t.Fatal("internal source changed", internal, err)
	}
}

func TestManagedAlarmAnalysisLooksUpDeviceAndHonorsKnowledge(t *testing.T) {
	ctx := context.Background()
	repo := &managedTestStore{testStore: newTestStore(), alarm: model.Alarm{ID: "a", TenantID: "t", DeviceID: "d"}}
	svc := New(repo)
	svc.now = func() time.Time { return time.Unix(1000, 0) }
	identity := allowedIdentity()
	svc.SetAccessResolver(func(context.Context, string, string) (MessageTopicIdentity, error) { return identity, nil })
	cfg := managedFixture("t", "kafka.alarm-ai-analysis")
	if ok, err := svc.Save(ctx, "t", cfg); !ok || err != nil {
		t.Fatal(ok, err)
	}
	analysis := model.AIAnalysis{TenantID: "t", AlarmID: "a"}
	get := func() []string {
		t.Helper()
		payload, _ := json.Marshal(analysis)
		got, err := svc.ManagedDestinations(ctx, "kafka", model.TopicAlarmAIAnalysis, payload)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if len(get()) != 1 {
		t.Fatal("allowed alarm analysis denied")
	}
	analysis.KnowledgeScope = model.AlarmAnalysisWorkflowID
	if len(get()) != 0 {
		t.Fatal("knowledge escaped permission")
	}
	identity.Permissions["menu:knowledge"] = true
	if len(get()) != 1 {
		t.Fatal("knowledge permission did not allow analysis")
	}
	repo.alarm.DeviceID = "ungranted"
	if len(get()) != 0 {
		t.Fatal("analysis escaped device scope")
	}
	repo.alarm.DeviceID = "d"
	repo.alarmErr = errors.New("storage unavailable")
	if len(get()) != 0 {
		t.Fatal("lookup failure leaked analysis")
	}
}

func TestManagedPublicationFailsClosedForUnavailableIdentityAndAmbiguousDevice(t *testing.T) {
	ctx := context.Background()
	repo := newTestStore()
	svc := New(repo)
	svc.now = func() time.Time { return time.Unix(1000, 0) }
	cfg := managedFixture("t", "mqtt.parsed")
	if ok, err := svc.Save(ctx, "t", cfg); !ok || err != nil {
		t.Fatal(ok, err)
	}
	get := func(payload []byte) {
		t.Helper()
		targets, err := svc.ManagedDestinations(ctx, "mqtt", "/iot/parsed/t/p/d/PROPERTY_REPORT", payload)
		if err != nil || len(targets) != 0 {
			t.Fatal(targets, err)
		}
	}
	get(body("t"))
	svc.SetAccessResolver(func(context.Context, string, string) (MessageTopicIdentity, error) {
		return MessageTopicIdentity{}, errors.New("deleted user")
	})
	get(body("t"))
	svc.SetAccessResolver(func(context.Context, string, string) (MessageTopicIdentity, error) {
		i := allowedIdentity()
		i.DeviceScope = "all"
		return i, nil
	})
	get([]byte(`{"tenantId":"t"}`))
	if _, err := svc.ManagedDestinations(ctx, "mqtt", "/iot/parsed/other/p/d/PROPERTY_REPORT", body("t")); err == nil {
		t.Fatal("MQTT tenant mismatch accepted")
	}
}

func TestManagedConfigurationValidationAndDeepCopy(t *testing.T) {
	valid := managedFixture("t", "mqtt.parsed")
	for _, tc := range []struct {
		name   string
		change func(*model.MessageTopicConfig)
	}{
		{"reserved route", func(c *model.MessageTopicConfig) { c.Topics[0].ID = "mqtt.parsed" }},
		{"internal source", func(c *model.MessageTopicConfig) { c.Topics[0].SourceID = "mqtt.state" }},
		{"wildcard slug", func(c *model.MessageTopicConfig) { c.Topics[0].Topic = "#" }},
		{"internal tombstone", func(c *model.MessageTopicConfig) { c.Deleted = []string{"kafka.raw"} }},
		{"invalid grant", func(c *model.MessageTopicConfig) { c.Accounts[0].TopicIDs = []string{"kafka.raw"} }},
		{"duplicate credential", func(c *model.MessageTopicConfig) { c.Credentials = append(c.Credentials, c.Credentials[0]) }},
		{"foreign destination", func(c *model.MessageTopicConfig) {
			c.Credentials[0].Topics = []string{Destination("other", "custom", c.Credentials[0])}
		}},
		{"ungranted destination", func(c *model.MessageTopicConfig) {
			c.Credentials[0].Topics = []string{Destination("t", "mqtt.parsed", c.Credentials[0])}
		}},
		{"shared destination", func(c *model.MessageTopicConfig) { c.Credentials[0].Topics = []string{"/iot/parsed/t/#"} }},
		{"live dangling account", func(c *model.MessageTopicConfig) { c.Accounts = nil }},
		{"invalid credential status", func(c *model.MessageTopicConfig) { c.Credentials[0].Status = "oops" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := cloneConfig(valid)
			tc.change(&cfg)
			if !errors.Is(Validate("t", cfg), ErrInvalidConfig) {
				t.Fatal("invalid managed configuration accepted")
			}
		})
	}
	clone := cloneConfig(valid)
	clone.Accounts[0].TopicIDs[0] = "bad"
	clone.Accounts[0].DeviceIDs[0] = "bad"
	clone.Credentials[0].Topics[0] = "bad"
	clone.Topics[0].Name = "bad"
	if valid.Accounts[0].TopicIDs[0] != "custom" || valid.Accounts[0].DeviceIDs[0] != "d" || slices.Contains(valid.Credentials[0].Topics, "bad") || valid.Topics[0].Name == "bad" {
		t.Fatal("config clone shares nested state")
	}
}

func TestOrdinaryMQTTRouteCannotRenderIntoCredentialNamespace(t *testing.T) {
	svc := New(newTestStore())
	ctx := context.Background()
	cfg := configuration("mqtt.parsed", true, MQTTPrefix("t")+"{productId}/credential/route")
	if ok, err := svc.Save(ctx, "t", cfg); !ok || err != nil {
		t.Fatal(ok, err)
	}
	payload := []byte(`{"tenantId":"t","productId":"managed","deviceId":"d","messageType":"PROPERTY_REPORT"}`)
	if _, enabled, err := svc.Resolve(ctx, "mqtt", "/iot/parsed/t/managed/d/PROPERTY_REPORT", payload); err == nil || enabled {
		t.Fatal("ordinary publication bypassed credential authorization", enabled, err)
	}
}
