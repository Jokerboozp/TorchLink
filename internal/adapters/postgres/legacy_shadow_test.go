package postgres

import (
	"context"
	"encoding/json"
	"iot-platform/internal/model"
	"testing"
)

func seedLegacyShadow(t *testing.T, r *Repository) {
	t.Helper()
	ctx := context.Background()
	_, err := r.pool.Exec(ctx, `CREATE TABLE device_shadow(tenant_id text NOT NULL,device_id text NOT NULL,body jsonb NOT NULL,PRIMARY KEY(tenant_id,device_id)); CREATE TABLE device_shadow_change(tenant_id text NOT NULL,device_id text NOT NULL,version bigint NOT NULL,body jsonb NOT NULL,PRIMARY KEY(tenant_id,device_id,version))`)
	if err != nil {
		t.Fatal(err)
	}
	shadow := model.DeviceShadow{TenantID: "legacy-shadow", DeviceID: "device", Version: 9, DesiredVersion: 7, Desired: map[string]any{"target": float64(42)}, Reported: map[string]any{"target": float64(40)}}
	b, _ := json.Marshal(shadow)
	if _, err = r.pool.Exec(ctx, `INSERT INTO device_shadow VALUES($1,$2,$3)`, shadow.TenantID, shadow.DeviceID, b); err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(model.ShadowChange{Version: 7, Timestamp: 100, Actor: "legacy", Desired: shadow.Desired})
	if _, err = r.pool.Exec(ctx, `INSERT INTO device_shadow_change VALUES($1,$2,$3,$4)`, shadow.TenantID, shadow.DeviceID, 7, b); err != nil {
		t.Fatal(err)
	}
}
func verifyLegacyShadow(t *testing.T, r *Repository) {
	t.Helper()
	ctx := context.Background()
	s, err := r.GetDeviceShadow(ctx, "legacy-shadow", "device")
	if err != nil || s.Name != "" || s.Version != 9 || s.DesiredVersion != 7 || s.Reported["target"] != float64(40) || s.Delta["target"] != float64(42) {
		t.Fatal("migration changed legacy shadow", s, err)
	}
	h, err := r.ListShadowChanges(ctx, "legacy-shadow", "device", 20, 0)
	if err != nil || len(h) != 1 || h[0].Version != 7 || h[0].Actor != "legacy" {
		t.Fatal("legacy history lost", h, err)
	}
	s, err = r.GetDeviceShadow(ctx, "legacy-shadow", "device", "other")
	if err != nil || s.Version != 0 || len(s.Reported) != 0 {
		t.Fatal("legacy state leaked into named shadow", s, err)
	}
}
