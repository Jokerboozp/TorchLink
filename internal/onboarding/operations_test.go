package onboarding

import (
	"context"
	"encoding/json"
	"errors"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func operationService(t *testing.T) *Service {
	t.Helper()
	r := memory.NewRepository()
	ctx := context.Background()
	if e := r.SaveProduct(ctx, model.Product{TenantID: "t", ID: "p", Status: "ENABLED"}); e != nil {
		t.Fatal(e)
	}
	if e := r.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: "d", ProductID: "p", Status: "ENABLED", AccessKey: "old", SecretHash: Hash("secret"), Tags: map[string]string{"connector": "MQTT"}}); e != nil {
		t.Fatal(e)
	}
	return New(r, nil, "", nil)
}
func TestCommandConcurrencyAndEarlyReply(t *testing.T) {
	s := operationService(t)
	ctx := context.Background()
	var sent atomic.Int32
	s.PublishCommand = func(ctx context.Context, topic string, b []byte, qos byte, retained bool) error {
		sent.Add(1)
		if topic != "/iot/down/t/p/d/command" || retained {
			t.Error("wrong command routing")
		}
		var v map[string]any
		if json.Unmarshal(b, &v) != nil {
			t.Error("invalid envelope")
		}
		return s.Repo.CompleteDeviceCommand(ctx, "t", "d", "cmd1", map[string]any{"success": true}, 2)
	}
	q := model.DeviceCommand{Confirmed: true, ID: "cmd1", Type: "reboot", Data: map[string]any{}}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.SendCommand(ctx, "t", "d", q); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if sent.Load() != 1 {
		t.Fatal("duplicate physical dispatch", sent.Load())
	}
	v, _, e := s.Repo.ListDeviceCommands(ctx, "t", "d", 20, 0)
	if e != nil || len(v) != 1 || v[0].Status != "SUCCEEDED" {
		t.Fatal(v, e)
	}
	q.Data = map[string]any{"different": true}
	if _, e = s.SendCommand(ctx, "t", "d", q); e == nil {
		t.Fatal("conflicting id accepted")
	}
	if _, e = s.SendCommand(ctx, "other", "d", q); e == nil {
		t.Fatal("cross-tenant command accepted")
	}
}
func TestCommandUnknownIsNotRetried(t *testing.T) {
	s := operationService(t)
	n := 0
	s.PublishCommand = func(context.Context, string, []byte, byte, bool) error { n++; return errors.New("connection lost") }
	q := model.DeviceCommand{Confirmed: true, ID: "cmd1", Type: "open", Data: map[string]any{}}
	for i := 0; i < 2; i++ {
		v, e := s.SendCommand(context.Background(), "t", "d", q)
		if e != nil || v.Status != "UNKNOWN" {
			t.Fatal(v, e)
		}
	}
	if n != 1 {
		t.Fatal(n)
	}
}
func TestCredentialOutboxAndRecovery(t *testing.T) {
	s := operationService(t)
	ctx := context.Background()
	c, v, e := s.ChangeCredential(ctx, "t", "d", true)
	if e != nil || v.Status != "PENDING" || v.Username != "old" || c.Secret == "" {
		t.Fatal(v, e)
	}
	if _, e = s.Authenticate(ctx, "old", "secret"); e == nil {
		t.Fatal("old credential accepted")
	}
	if _, e = s.Authenticate(ctx, c.AccessKey, c.Secret); e != nil {
		t.Fatal(e)
	}
	s.RevokeUsername = func(_ context.Context, user string) error {
		if user != "old" {
			t.Fatal(user)
		}
		return nil
	}
	v = s.revoke(ctx, v)
	if v.Status != "REVOKED" {
		t.Fatal(v)
	}
	s.RevokeUsername = nil
	_, v, e = s.ChangeCredential(ctx, "t", "d", false)
	if e != nil || v.Username != c.AccessKey {
		t.Fatal(v, e)
	}
	if _, e = s.Authenticate(ctx, c.AccessKey, c.Secret); e == nil {
		t.Fatal("disabled credential accepted")
	}
	data, _ := json.Marshal(v)
	if strings.Contains(string(data), c.Secret) {
		t.Fatal("secret exposed")
	}
	items, _ := s.Repo.ListCredentialRevocations(ctx, "t", "d", false)
	if len(items) != 2 {
		t.Fatal(items)
	}
}
func TestThingModelAndRemovedEdgeValidation(t *testing.T) {
	s := operationService(t)
	ctx := context.Background()
	m := &model.ThingModel{Commands: []model.ThingOperation{{Identifier: "set", Fields: []model.ThingField{{Identifier: "value", DataType: "integer", Required: true}}}}}
	if e := ValidateThingModel(m); e != nil {
		t.Fatal(e)
	}
	m.Properties = []model.ThingField{{Identifier: "a", DataType: "bad"}}
	if ValidateThingModel(m) == nil {
		t.Fatal("invalid model accepted")
	}
	m.Properties = nil
	s.Repo.SaveProduct(ctx, model.Product{TenantID: "t", ID: "p", Status: "ENABLED", ThingModel: m})
	s.PublishCommand = func(context.Context, string, []byte, byte, bool) error { return nil }
	if _, e := s.SendCommand(ctx, "t", "d", model.DeviceCommand{Confirmed: true, ID: "c", Type: "set", Data: map[string]any{"value": 1.2}}); e == nil {
		t.Fatal("invalid integer accepted")
	}
	_, _, e := s.plan(ctx, "t", Request{ProductID: "p", DeviceID: "new", Name: "new", Profile: model.DeviceAccessProfile{EdgeNodeID: "edge"}})
	if e == nil {
		t.Fatal("removed edge assignment accepted")
	}
}

func TestCommandRequiresManualConfirmation(t *testing.T) {
	s := operationService(t)
	s.PublishCommand = func(context.Context, string, []byte, byte, bool) error {
		t.Fatal("unconfirmed command was dispatched")
		return nil
	}
	if _, e := s.SendCommand(context.Background(), "t", "d", model.DeviceCommand{ID: "unconfirmed", Type: "reset", Data: map[string]any{}}); e == nil {
		t.Fatal("unconfirmed command accepted")
	}
}
