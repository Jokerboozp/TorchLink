package messagetopics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func deviceSnapshotQuery() model.MessageTopicQuery {
	return model.MessageTopicQuery{Dataset: "devices", Mode: "interval", IntervalSeconds: 60, DeviceScope: "all", Fields: map[string]string{"device": "deviceId", "online": "online"}}
}

func TestQuerySnapshotFiltersLargeDatasetAndNeverLeaksCredentials(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	for i := 0; i < 205; i++ {
		id := fmt.Sprintf("device-%03d", i)
		if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{ID: id, TenantID: "t", Name: id, Status: "ENABLED", AccessKey: "private_" + id, SecretHint: "secret hint", CreatedAt: int64(i)}); err != nil {
			t.Fatal(err)
		}
	}
	_ = repo.SaveManagedDevice(ctx, model.ManagedDevice{ID: "device-other", TenantID: "other", AccessKey: "private_other"})
	_ = repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: "t", DeviceID: "device-204", ConnectionStatus: "CONNECTED", BusinessStatus: "ONLINE", LastSeenAt: 77})
	_ = repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: "t", DeviceID: "device-203", ConnectionStatus: "UNKNOWN"})
	service := New(repo)
	query := deviceSnapshotQuery()
	query.Fields = nil
	query.Filter = &model.MessageTopicFilter{Field: "createdAt", Operator: "gte", Value: 200}
	output, err := service.Snapshot(ctx, "t", query)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(output, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Items) != 5 || strings.Contains(string(output), "private_") || strings.Contains(string(output), "secretHint") || strings.Contains(string(output), "device-other") {
		t.Fatalf("unexpected snapshot %s", output)
	}
	for _, device := range snapshot.Items {
		if device["deviceId"] == "device-203" && device["online"] != nil {
			t.Fatal("unknown connection treated as offline", device)
		}
		if device["deviceId"] == "device-204" && (device["online"] != true || device["lastSeenAt"] != float64(77)) {
			t.Fatal(device)
		}
	}
	query.Filter = nil
	query.DeviceScope, query.DeviceIDs = "selected", []string{"device-204"}
	output, err = service.Snapshot(ctx, "t", query)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(output, &snapshot)
	if len(snapshot.Items) != 1 || snapshot.Items[0]["deviceId"] != "device-204" {
		t.Fatalf("scope lost: %s", output)
	}
}

func TestQuerySnapshotsRejectPartialOversizedResultsAndKeepEmptyArray(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	s := New(repo)
	query := deviceSnapshotQuery()
	output, err := s.Snapshot(ctx, "t", query)
	if err != nil || !strings.Contains(string(output), `"items":[]`) {
		t.Fatalf("empty=%s err=%v", output, err)
	}
	for i := 0; i <= maxSnapshotRows; i++ {
		_ = repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: fmt.Sprint(i), AccessKey: "key_" + fmt.Sprint(i)})
	}
	if output, err := s.Snapshot(ctx, "t", query); err == nil || output != nil {
		t.Fatal("oversized result was partially returned")
	}
	query.DeviceScope, query.DeviceIDs = "selected", []string{"0"}
	query.Fields = map[string]string{"tags": "tags"}
	_ = repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: "0", AccessKey: "key_0", Tags: map[string]string{"large": strings.Repeat("x", MaxPayload)}})
	if output, err := s.Snapshot(ctx, "t", query); err == nil || output != nil {
		t.Fatal("oversized payload was returned")
	}
}

func TestQuerySamplesUseRealReportsAndRespectTenantAndDeviceScope(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	for _, id := range []string{"one", "two"} {
		_ = repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: id, AccessKey: "key_" + id})
		_ = repo.SaveStandardMessage(ctx, model.StandardMessage{TenantID: "t", DeviceID: id, MessageID: id, Properties: map[string]any{"temperature": 26}, Raw: map[string]any{"unselected": "raw bytes"}})
	}
	_ = repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "other", ID: "three", AccessKey: "key_three"})
	s := New(repo)
	query := model.MessageTopicQuery{Dataset: "device_reports", Mode: "realtime", DeviceScope: "selected", DeviceIDs: []string{"two"}}
	rows, err := s.QuerySamples(ctx, "t", query, 50)
	if err != nil || len(rows) != 1 || !strings.Contains(string(rows[0]), `"deviceId":"two"`) || strings.Contains(string(rows[0]), "raw bytes") {
		t.Fatalf("samples=%q err=%v", rows, err)
	}
	rows, err = s.QuerySamples(ctx, "other", query, 50)
	if err != nil || len(rows) != 0 {
		t.Fatal("sample crossed tenant", err)
	}
}

func saveScheduledQuery(t *testing.T, s *Service, query model.MessageTopicQuery) {
	t.Helper()
	cfg := model.MessageTopicConfig{Topics: []model.MessageTopicRoute{{ID: "snapshot", Name: "设备快照", Protocol: "mqtt", Topic: "device-list", Enabled: true, Query: &query}}}
	if err := AccumulateQueryExposure(&cfg, "snapshot", query); err != nil {
		t.Fatal(err)
	}
	if saved, err := s.Save(context.Background(), "t", cfg); err != nil || !saved {
		t.Fatal(saved, err)
	}
}

func TestQuerySchedulerIntervalDisableFailureAndRevocation(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	s := New(repo)
	now := time.Unix(1000, 0)
	s.now = func() time.Time { return now }
	saveScheduledQuery(t, s, deviceSnapshotQuery())
	runner := NewQueryScheduler(s)
	count := 0
	fail := false
	publish := func(_ context.Context, protocol, topic string, payload []byte) error {
		count++
		if protocol != "mqtt" || topic != MQTTPrefix("t")+"device-list" || !strings.Contains(string(payload), `"items":[]`) {
			t.Fatal(protocol, topic, string(payload))
		}
		if fail {
			return errors.New("uncertain broker timeout")
		}
		return nil
	}
	if err := runner.RunOnce(ctx, publish); err != nil {
		t.Fatal(err)
	}
	_ = runner.RunOnce(ctx, publish)
	if count != 1 {
		t.Fatal("sent before interval", count)
	}
	now = now.Add(time.Minute)
	fail = true
	if err := runner.RunOnce(ctx, publish); err == nil {
		t.Fatal("broker error hidden")
	}
	_ = runner.RunOnce(ctx, publish)
	if count != 2 {
		t.Fatal("uncertain publish retried", count)
	}
	fail = false
	cfg, _ := s.Load(ctx, "t")
	cfg.Topics[0].KeyIDs = []string{"key"}
	cfg.Credentials = []model.MessageTopicCredential{{ID: "c", KeyID: "key", Protocol: "mqtt", Username: "broker", Topics: []string{MQTTPrefix("t") + "device-list"}, Status: "revoking", AccessVersion: "v1", ExpiresAt: 3000}}
	if saved, err := s.Save(ctx, "t", cfg); !saved || err != nil {
		t.Fatal(saved, err)
	}
	now = now.Add(time.Minute)
	if err := runner.RunOnce(ctx, publish); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatal("published before revocation completed")
	}
	cfg, _ = s.Load(ctx, "t")
	cfg.Credentials[0].Status = "revoked"
	_, _ = s.Save(ctx, "t", cfg)
	_ = runner.RunOnce(ctx, publish)
	if count != 3 {
		t.Fatal("did not resume after revocation", count)
	}
	cfg, _ = s.Load(ctx, "t")
	cfg.Topics[0].Enabled = false
	_, _ = s.Save(ctx, "t", cfg)
	now = now.Add(time.Minute)
	_ = runner.RunOnce(ctx, publish)
	if count != 3 {
		t.Fatal("published disabled topic")
	}
}

type queryChangingRepository struct {
	ports.Repository
	beforeQuery func()
}

func (r *queryChangingRepository) ListMessageTopicDevices(ctx context.Context, tenant string, ids []string, limit int) ([]model.MessageTopicDeviceRecord, error) {
	if r.beforeQuery != nil {
		callback := r.beforeQuery
		r.beforeQuery = nil
		callback()
	}
	return r.Repository.ListMessageTopicDevices(ctx, tenant, ids, limit)
}

func TestScheduledQueryRechecksConfigurationAfterReading(t *testing.T) {
	ctx := context.Background()
	repo := &queryChangingRepository{Repository: memory.NewRepository()}
	s := New(repo)
	saveScheduledQuery(t, s, deviceSnapshotQuery())
	repo.beforeQuery = func() {
		cfg, _ := s.Load(ctx, "t")
		cfg.Topics[0].Enabled = false
		_, _ = s.Save(ctx, "t", cfg)
	}
	if err := NewQueryScheduler(s).RunOnce(ctx, func(context.Context, string, string, []byte) error {
		t.Fatal("stale query published after disable")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
