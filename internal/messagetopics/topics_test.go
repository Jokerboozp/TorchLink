package messagetopics

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"iot-platform/internal/model"
)

func keyIdentity() MessageTopicIdentity {
	return MessageTopicIdentity{Permissions: map[string]bool{"menu:devices": true, "menu:messageTopics": true}, DeviceScope: "selected", DeviceIDs: []string{"d"}, Version: "v1"}
}

func grantKey(cfg *model.MessageTopicConfig, tenant, id string) {
	cfg.Topics[0].KeyIDs = append(cfg.Topics[0].KeyIDs, id)
	cfg.Credentials = append(cfg.Credentials, model.MessageTopicCredential{ID: "credential_" + id, KeyID: id, Protocol: cfg.Topics[0].Protocol, Username: "broker_" + id, Topics: []string{Destination(tenant, cfg.Topics[0])}, Status: "active", AccessVersion: "v1", CreatedAt: 900, ExpiresAt: 2000})
}

func TestValidationRejectsForeignRetiredAndUngrantedDestinations(t *testing.T) {
	cfg := queryFixture(t, "mqtt")
	grantKey(&cfg, "t", "a")
	if err := Validate("t", cfg); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*model.MessageTopicConfig){
		"foreign tenant":    func(c *model.MessageTopicConfig) { c.Topics[0].Topic = MQTTPrefix("other") + "device" },
		"wildcard":          func(c *model.MessageTopicConfig) { c.Topics[0].Topic = "device/#" },
		"retired address":   func(c *model.MessageTopicConfig) { c.RetiredTopics = []string{"mqtt:" + Destination("t", c.Topics[0])} },
		"duplicate address": func(c *model.MessageTopicConfig) { c.Topics = append(c.Topics, c.Topics[0]); c.Topics[1].ID = "copy" },
		"ungranted key":     func(c *model.MessageTopicConfig) { c.Topics[0].KeyIDs = nil },
		"two live leases": func(c *model.MessageTopicConfig) {
			c.Credentials = append(c.Credentials, c.Credentials[0])
			c.Credentials[1].ID = "second"
		},
		"missing key": func(c *model.MessageTopicConfig) { c.Credentials[0].KeyID = "" },
	} {
		bad := cloneConfig(cfg)
		change(&bad)
		if !errors.Is(Validate("t", bad), ErrInvalidConfig) {
			t.Errorf("%s accepted", name)
		}
	}
	// A revocation outlives its topic and key until the broker confirms it.
	pending := cloneConfig(cfg)
	pending.Topics[0].KeyIDs = nil
	pending.Credentials[0].Status = "revoking"
	if err := Validate("t", pending); err != nil {
		t.Fatal(err)
	}
}

func TestRetiredTopicAddressCannotBeReused(t *testing.T) {
	cfg := queryFixture(t, "kafka")
	if !RetireTopic("t", &cfg, "shared") || len(cfg.Topics) != 0 || !slices.Contains(cfg.RetiredTopics, "kafka:"+KafkaPrefix("t")+"device") {
		t.Fatal("topic address was not reserved", cfg)
	}
	again := queryFixture(t, "kafka")
	again.RetiredTopics = cfg.RetiredTopics
	if Validate("t", again) == nil {
		t.Fatal("retired Kafka topic reused")
	}
	if RetireTopic("t", &cfg, "missing") {
		t.Fatal("missing topic retired")
	}
}

func TestSanitizeDropsFormerRoutesAndKeepsRevocations(t *testing.T) {
	cfg := queryFixture(t, "mqtt")
	cfg.Topics = append(cfg.Topics, model.MessageTopicRoute{ID: "legacy", Name: "旧通道", Protocol: "mqtt", Topic: "legacy", Enabled: true}, model.MessageTopicRoute{ID: "managed", Name: "旧转发", Topic: "slug", Enabled: true})
	cfg.Credentials = []model.MessageTopicCredential{{ID: "old", Protocol: "kafka", Username: "old", Topics: []string{"x"}, Status: "active", ExpiresAt: 10}}
	sanitize("t", &cfg)
	if len(cfg.Topics) != 1 || cfg.Topics[0].ID != "shared" || !slices.Contains(cfg.RetiredTopics, "mqtt:"+MQTTPrefix("t")+"legacy") {
		t.Fatal("former routes were not dropped and reserved", cfg.Topics, cfg.RetiredTopics)
	}
	if cfg.Credentials[0].Status != "revoking" || Validate("t", cfg) != nil {
		t.Fatal("former credential was not kept for broker revocation", cfg.Credentials)
	}
}

func TestCredentialTopicsAndReadinessFollowKeyIdentity(t *testing.T) {
	ctx := context.Background()
	cfg := queryFixture(t, "mqtt")
	grantKey(&cfg, "t", "a")
	identity := keyIdentity()
	if got := CredentialTopics("t", cfg, "a", "mqtt", identity); !slices.Equal(got, []string{Destination("t", cfg.Topics[0])}) {
		t.Fatal(got)
	}
	if CredentialTopics("t", cfg, "other", "mqtt", identity) != nil || CredentialTopics("t", cfg, "a", "kafka", identity) != nil {
		t.Fatal("ungranted key or protocol received topics")
	}
	wide := identity
	wide.DeviceIDs = nil
	if CredentialTopics("t", cfg, "a", "mqtt", wide) != nil {
		t.Fatal("key without device coverage received topics")
	}
	svc := New(newTestStore())
	svc.now = func() time.Time { return time.Unix(1000, 0) }
	current := identity
	svc.SetAccessResolver(func(_ context.Context, _, key string) (MessageTopicIdentity, error) {
		if key != "a" {
			return MessageTopicIdentity{}, errors.New("unknown key")
		}
		return current, nil
	})
	if !svc.topicReady(ctx, "t", cfg, cfg.Topics[0]) {
		t.Fatal("current credential paused topic")
	}
	current.Version = "v2"
	if svc.topicReady(ctx, "t", cfg, cfg.Topics[0]) {
		t.Fatal("changed key or user policy kept publishing")
	}
	current = identity
	cfg.Credentials[0].Provisioning = true
	if svc.topicReady(ctx, "t", cfg, cfg.Topics[0]) {
		t.Fatal("provisioning race kept publishing")
	}
	cfg.Credentials[0].Provisioning, cfg.Credentials[0].Status = false, "revoked"
	if !svc.topicReady(ctx, "t", cfg, cfg.Topics[0]) {
		t.Fatal("revoked credential still paused topic")
	}
}
