package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
)

func TestVideoCameraRelationsHTTPEnforcesCameraToOneDevice(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.Metrics = metrics.New()
	if err = engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DevMode = true
	cfg.JWTSecret = "test-secret-at-least-32-characters"
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	login := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/auth/login", "", map[string]any{
		"username": "admin", "password": "admin123", "tenantId": "tenant_001",
	}, http.StatusOK)
	token := login["accessToken"].(string)
	for index, deviceID := range []string{"device-001", "device-002"} {
		if err := repo.SaveManagedDevice(context.Background(), model.ManagedDevice{ID: deviceID, TenantID: "tenant_001", ProductID: "product", Name: deviceID, Status: "ENABLED", AccessKey: "access-" + string(rune('a'+index))}); err != nil {
			t.Fatal(err)
		}
	}

	camera := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/integrations/video/cameras", token, map[string]any{
		"cameraId": "camera_one_device", "cameraName": "一号摄像头", "brand": "海康", "cameraPoint": "东侧入口",
		"building": "A", "floor": "1", "room": "大厅", "deviceId": "device-001", "enabled": true,
	}, http.StatusCreated)
	if camera["deviceId"] != "device-001" || camera["brand"] != "海康" || camera["cameraPoint"] != "东侧入口" {
		t.Fatalf("saved camera metadata = %#v", camera)
	}
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/integrations/video/cameras", token, map[string]any{
		"cameraId": "camera_two", "cameraName": "二号摄像头", "deviceId": "device-001", "enabled": true,
	}, http.StatusCreated)
	result := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/integrations/video/relations?relationType=device&targetId=device-001", token, nil, http.StatusOK)
	items := result["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("reverse device lookup = %#v", result)
	}
	requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/integrations/video/relations?relationType=floor&targetId=1", token, nil, http.StatusUnprocessableEntity)
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/integrations/video/cameras", token, map[string]any{
		"cameraId": "camera_invalid", "cameraName": "非法摄像头", "relatedDeviceIds": []string{"device-001", "device-002"}, "enabled": true,
	}, http.StatusUnprocessableEntity)
	viewer := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/integrations/video/cameras", token, nil, http.StatusOK)
	for _, item := range viewer["items"].([]any) {
		row := item.(map[string]any)
		if _, exposed := row["streamUrl"]; exposed || row["cameraId"] == "camera_one_device" && row["deviceId"] != "device-001" {
			t.Fatalf("camera response contains stream data or lost device relation: %#v", row)
		}
	}
}
