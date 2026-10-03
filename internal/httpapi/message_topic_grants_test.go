package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"slices"
	"testing"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/messagetopics"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
)

type topicGrantKafkaFake struct {
	topicKafkaAdminFake
	read, write []string
	group       string
}

func (f *topicGrantKafkaFake) ProvisionGrants(_ context.Context, username, password, group string, read, write []string) error {
	if username == "" || password == "" || len(read)+len(write) == 0 || (len(read) > 0 && group != username) || (len(read) == 0 && group != "") {
		return errors.New("invalid independent grants")
	}
	f.provisioned = append(f.provisioned, username)
	f.read, f.write, f.group = slices.Clone(read), slices.Clone(write), group
	if f.duringProvision != nil {
		f.duringProvision()
	}
	return f.provisionErr
}

func newTopicGrantFixture(t *testing.T, protocol string, subscribe bool) (*Server, *memory.Repository, *topicGrantKafkaFake, model.MessageTopicConfig) {
	t.Helper()
	ctx := context.Background()
	repo := memory.NewRepository()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(ScopedRepository(repo), nil, local.NewBus(), local.NewRealtime(), nil, log)
	cfg := config.Load()
	cfg.JWTSecret = "topic-grants-test-signing-key-long-enough"
	cfg.MQTTBroker, cfg.MQTTPublicURL = "tcp://mqtt.invalid:1883", "tcp://mqtt.invalid:1883"
	cfg.KafkaBrokers, cfg.KafkaPublicBrokers = []string{"kafka.invalid:9092"}, []string{"kafka.invalid:9092"}
	api := New(cfg, engine, metrics.New(), log)
	if ok, err := repo.SaveAccessState(ctx, "t", model.AccessState{Users: []model.PlatformUser{{Username: "writer", Enabled: true, DeviceScope: "selected", Permissions: []string{"menu:messageTopics"}}}}); !ok || err != nil {
		t.Fatal(err)
	}
	broker := &topicGrantKafkaFake{}
	api.SetMessageTopicKafkaAdmin(broker)
	api.SetMessageTopicMQTTReadiness(func(context.Context) error { return nil })
	api.SetDeviceOperations(nil, func(_ context.Context, user string) error { broker.revoked = append(broker.revoked, user); return nil })
	state := model.MessageTopicConfig{Topics: []model.MessageTopicRoute{
		{ID: "read", Name: "订阅主题", Protocol: protocol, Topic: "read", Enabled: true},
		{ID: "write", Name: "发布主题", Protocol: protocol, Topic: "write", Enabled: true},
	}, Accounts: []model.MessageTopicAccount{{ID: "account", Name: "发布账号", Username: "writer", Enabled: true, PublishTopicIDs: []string{"write"}, DeviceScope: "selected", SecretHash: apiKeySecretHash("exchange-secret")}}}
	if subscribe {
		state.Accounts[0].TopicIDs = []string{"read"}
	}
	if err := api.validateMessageTopicAccount(ctx, "t", state, state.Accounts[0]); err != nil {
		t.Fatal("validate manual-topic account", err)
	}
	if ok, err := engine.MessageTopics.Save(ctx, "t", state); !ok || err != nil {
		t.Fatal(err)
	}
	state, err := engine.MessageTopics.Load(ctx, "t")
	if err != nil {
		t.Fatal(err)
	}
	return api, repo, broker, state
}

func TestMessageTopicIndependentSubscribePublishCredentials(t *testing.T) {
	for _, protocol := range []string{"mqtt", "kafka"} {
		for _, subscribe := range []bool{false, true} {
			name := protocol + " publish-only"
			if subscribe {
				name = protocol + " read/write"
			}
			t.Run(name, func(t *testing.T) {
				api, _, broker, cfg := newTopicGrantFixture(t, protocol, subscribe)
				result, _, err := api.issueTopicCredential(context.Background(), "t", "account", protocol, "exchange-secret")
				if err != nil {
					t.Fatal(err)
				}
				wantWrite := messagetopics.SharedDestination("t", cfg.Topics[1])
				reads, writes := result["subscribeTopics"].([]string), result["publishTopics"].([]string)
				if !slices.Equal(reads, result["topics"].([]string)) || !slices.Equal(writes, []string{wantWrite}) {
					t.Fatal("wrong compatibility or publish snapshot", result)
				}
				if subscribe {
					if !slices.Equal(reads, []string{messagetopics.SharedDestination("t", cfg.Topics[0])}) {
						t.Fatal("shared read address was hashed", reads)
					}
				} else if len(reads) != 0 {
					t.Fatal("publisher gained subscribe grant")
				}
				if protocol == "mqtt" {
					token := result["password"].(string)
					claims, err := api.auth.Parse(token)
					if err != nil || claims.TokenUse != "topic-consumer" {
						t.Fatal("wrong token boundary", err)
					}
					if len(claims.ACL) != len(reads)+len(writes)+2 {
						t.Fatal("unexpected MQTT grant count", claims.ACL)
					}
					for _, acl := range claims.ACL {
						if acl.Permission != "allow" {
							continue
						}
						if acl.Action == "publish" && !slices.Contains(writes, acl.Topic) {
							t.Fatal("publish grant enlarged")
						}
						if acl.Action == "subscribe" && !slices.Contains(reads, acl.Topic) {
							t.Fatal("subscribe grant enlarged")
						}
					}
					server := httptest.NewServer(api.Handler())
					defer server.Close()
					requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/message-topics", token, nil, 403)
				} else if !slices.Equal(broker.read, reads) || !slices.Equal(broker.write, writes) || (subscribe != (broker.group != "")) {
					t.Fatal("broker did not receive independent grants")
				}
				stored, _ := api.engine.MessageTopics.Load(context.Background(), "t")
				if len(stored.Credentials) != 1 || stored.Credentials[0].Provisioning || stored.Credentials[0].Status != "active" {
					t.Fatal("credential did not finish activation")
				}
			})
		}
	}
}

func TestMessageTopicPublishRequiresSharedRouteAndBoundPermission(t *testing.T) {
	api, repo, _, cfg := newTopicGrantFixture(t, "mqtt", false)
	account := cfg.Accounts[0]
	account.PublishTopicIDs = []string{"mqtt.parsed"}
	if err := api.validateMessageTopicAccount(context.Background(), "t", cfg, account); err == nil {
		t.Fatal("legacy source accepted publish grant")
	}
	account = cfg.Accounts[0]
	cfg.Topics[1].Enabled = false
	if err := api.validateMessageTopicAccount(context.Background(), "t", cfg, account); err == nil {
		t.Fatal("disabled topic accepted publish grant")
	}
	cfg.Topics[1].Enabled = true
	state, _ := repo.LoadAccessState(context.Background(), "t")
	state.Users[0].Permissions = []string{"menu:devices"}
	if ok, err := repo.SaveAccessState(context.Background(), "t", state); !ok || err != nil {
		t.Fatal(err)
	}
	if err := api.validateMessageTopicAccount(context.Background(), "t", cfg, account); err == nil {
		t.Fatal("device permission substituted for message-topic permission")
	}
	if _, status, err := api.issueTopicCredential(context.Background(), "t", "account", "mqtt", "exchange-secret"); err == nil || status != 422 {
		t.Fatal("unauthorized publisher issued credentials", status, err)
	}
}

func TestMessageTopicKafkaPublishCannotFallBackToReadOnlyAdapter(t *testing.T) {
	api, _, _, _ := newTopicGrantFixture(t, "kafka", false)
	old := &topicKafkaAdminFake{}
	api.SetMessageTopicKafkaAdmin(old)
	if _, status, err := api.issueTopicCredential(context.Background(), "t", "account", "kafka", "exchange-secret"); err == nil || status != 503 {
		t.Fatal("unsupported write grant silently downgraded", status, err)
	}
	cfg, _ := api.engine.MessageTopics.Load(context.Background(), "t")
	if len(cfg.Credentials) != 0 || len(old.provisioned) != 0 {
		t.Fatal("unsupported adapter persisted/provisioned a credential")
	}
}

func TestMessageTopicSharedSubscriptionRequiresFullHistoricalExposure(t *testing.T) {
	api, repo, _, cfg := newTopicGrantFixture(t, "mqtt", false)
	ctx := context.Background()
	cfg.Topics[1].Exposure = []model.MessageTopicExposure{{SourceID: "mqtt.parsed", DeviceScope: "all"}}
	account := cfg.Accounts[0]
	if err := api.validateMessageTopicAccount(ctx, "t", cfg, account); err != nil {
		t.Fatal("publishing incorrectly requires access to historical messages", err)
	}
	account.TopicIDs = []string{"write"}
	if err := api.validateMessageTopicAccount(ctx, "t", cfg, account); err == nil {
		t.Fatal("subscription ignored source permission and historical device scope")
	}
	state, _ := repo.LoadAccessState(ctx, "t")
	state.Users[0].Permissions = []string{"menu:messageTopics", "menu:devices"}
	state.Users[0].DeviceScope = "all"
	if ok, err := repo.SaveAccessState(ctx, "t", state); !ok || err != nil {
		t.Fatal(err)
	}
	if err := api.validateMessageTopicAccount(ctx, "t", cfg, account); err == nil {
		t.Fatal("selected account scope read a shared all-devices history")
	}
	account.DeviceScope = "all"
	if err := api.validateMessageTopicAccount(ctx, "t", cfg, account); err != nil {
		t.Fatal("complete source and account scope rejected", err)
	}
}

func TestMessageTopicKafkaPublishFailureAndInflightRevocation(t *testing.T) {
	for _, concurrentRevoke := range []bool{false, true} {
		name := "ACL failure"
		if concurrentRevoke {
			name = "revocation during provision"
		}
		t.Run(name, func(t *testing.T) {
			api, _, broker, _ := newTopicGrantFixture(t, "kafka", false)
			ctx := context.Background()
			if !concurrentRevoke {
				broker.provisionErr = errors.New("partial write ACL failure")
			} else {
				broker.duringProvision = func() {
					cfg, _ := api.engine.MessageTopics.Load(ctx, "t")
					if len(cfg.Credentials) != 1 || !cfg.Credentials[0].Provisioning {
						t.Fatal("in-flight provisioning was not persisted")
					}
					cfg.Accounts[0].Enabled = false
					cfg.Credentials[0].Status = "revoking"
					if ok, err := api.engine.MessageTopics.Save(ctx, "t", cfg); !ok || err != nil {
						t.Fatal(err)
					}
					if err := api.RetryMessageTopicRevocationsOnce(ctx); err != nil {
						t.Fatal(err)
					}
					cfg, _ = api.engine.MessageTopics.Load(ctx, "t")
					if cfg.Credentials[0].Status != "revoked" || !cfg.Credentials[0].Provisioning {
						t.Fatal("reconcile prematurely removed in-flight publication barrier")
					}
				}
			}
			_, status, err := api.issueTopicCredential(ctx, "t", "account", "kafka", "exchange-secret")
			wantStatus := 503
			if concurrentRevoke {
				wantStatus = 409
			}
			if err == nil || status != wantStatus {
				t.Fatal("failed provision activated", status, err)
			}
			cfg, _ := api.engine.MessageTopics.Load(ctx, "t")
			if len(cfg.Credentials) != 1 || cfg.Credentials[0].Status != "revoking" || cfg.Credentials[0].Provisioning {
				t.Fatal("late provision completion did not require a fresh revocation")
			}
			if err := api.RetryMessageTopicRevocationsOnce(ctx); err != nil {
				t.Fatal(err)
			}
			cfg, _ = api.engine.MessageTopics.Load(ctx, "t")
			if cfg.Credentials[0].Status != "revoked" || cfg.Credentials[0].Provisioning || len(broker.revoked) == 0 {
				t.Fatal("revocation did not complete")
			}
		})
	}
}
