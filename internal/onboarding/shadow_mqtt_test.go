package onboarding

import (
	"context"
	"encoding/json"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
	"testing"
	"time"
)

func TestMQTTShadowIdentityAndReadOnlyResponse(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	service := New(repo, nil, t.TempDir(), nil)
	if err := repo.SaveProduct(ctx, model.Product{TenantID: "t", ID: "p", Status: "ENABLED"}); err != nil {
		t.Fatal(err)
	}
	device := model.ManagedDevice{TenantID: "t", ProductID: "p", ID: "d", Status: "ENABLED", AccessKey: "key", SecretHash: Hash("real-test-secret"), Tags: map[string]string{"connector": "MQTT"}}
	if err := repo.SaveManagedDevice(ctx, device); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpdateDeviceShadow(ctx, model.ShadowUpdate{TenantID: "t", DeviceID: "d", Timestamp: time.Now().UnixMilli(), MessageID: "actual-property", Reported: map[string]any{"temperature": 42}}); err != nil {
		t.Fatal(err)
	}
	topic, data, err := service.MQTTShadowReply(ctx, "t", "p", "d", []byte(`{"id":"read-1"}`))
	if err != nil || topic != "/iot/down/t/p/d/shadow" {
		t.Fatal(topic, err)
	}
	var reply struct {
		ID     string             `json:"id"`
		Status string             `json:"status"`
		Shadow model.DeviceShadow `json:"shadow"`
	}
	if json.Unmarshal(data, &reply) != nil || reply.ID != "read-1" || reply.Status != "ok" || reply.Shadow.Reported["temperature"] != float64(42) {
		t.Fatal("shadow response", string(data))
	}
	for _, args := range [][4]string{{"other", "p", "d", `{"id":"x"}`}, {"t", "other", "d", `{"id":"x"}`}, {"t", "p", "d", `{"id":"x","tenantId":"other"}`}, {"t", "p", "d", `{"id":"x","desired":{"temperature":0}}`}, {"t", "p", "d", `{"id":"x"}{}`}} {
		if _, _, err := service.MQTTShadowReply(ctx, args[0], args[1], args[2], []byte(args[3])); err == nil {
			t.Fatal("invalid shadow query accepted", args)
		}
	}
	device.SecretHash = ""
	repo.SaveManagedDevice(ctx, device)
	if _, _, err := service.MQTTShadowReply(ctx, "t", "p", "d", []byte(`{"id":"after-revoke"}`)); err == nil {
		t.Fatal("disabled credential read shadow")
	}
	shadow, _ := repo.GetDeviceShadow(ctx, "t", "d")
	if len(shadow.Desired) != 0 || shadow.Reported["temperature"] != float64(42) {
		t.Fatal("read changed shadow", shadow)
	}
}
