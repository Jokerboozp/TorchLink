package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
)

func TestDataQualityHTTPConfigurationScopeAttachmentsAndReadPermission(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	if err := repo.SaveProduct(ctx, model.Product{TenantID: "t", ID: "p", ThingModel: &model.ThingModel{Properties: []model.ThingField{{Identifier: "pressure", DataType: "number", Unit: "kPa"}}}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: id, ProductID: "p", AccessKey: "test-" + id}); err != nil {
			t.Fatal(err)
		}
	}
	permissions := []string{"menu:devices", "menu:dataQuality", "POST /api/v1/data-quality/profiles", "POST /api/v1/data-quality/calibrations", "POST /api/v1/data-quality/calibrations/attachments", "GET /api/v1/data-quality/calibrations/attachments/:id", "POST /api/v1/data-quality/runs"}
	state := model.AccessState{Users: []model.PlatformUser{{Username: "analyst", Enabled: true, Permissions: permissions, DeviceScope: "selected", DeviceIDs: []string{"a"}, SessionVersion: 1}, {Username: "other", Enabled: true, Permissions: permissions, DeviceScope: "selected", DeviceIDs: []string{"a"}, SessionVersion: 1}}}
	if ok, err := repo.SaveAccessState(ctx, "t", state); err != nil || !ok {
		t.Fatal(ok, err)
	}
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	api := New(config.Config{AdminUser: "admin", AdminTenants: []string{"t"}, JWTSecret: "quality-api-test-secret-32-characters", DevMode: true}, &core.Engine{Repo: repo, Archive: archive}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, err := api.auth.IssueUser("analyst", "t", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	other, err := api.auth.IssueUser("other", "t", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(-time.Hour).UnixMilli()
	body := map[string]any{"attributeId": "pressure", "mode": "periodic", "effectiveFrom": start, "scheduleAnchor": start, "periodMs": 1000, "toleranceMs": 100, "valueType": "number", "unit": "kPa", "unitConfirmed": true, "minimumSamples": 30}
	q := map[string]any{"resourceId": "pressure", "expectedVersion": 0, "scope": "personal", "deviceIds": []string{"a"}, "body": body}
	profile := requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/data-quality/profiles", token, q, 201)
	if profile["version"] != float64(1) {
		t.Fatal(profile)
	}
	page := requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/data-quality/profiles", other, nil, 200)
	if page["total"] != float64(0) {
		t.Fatal("personal configuration leaked", page)
	}
	q["deviceIds"] = []string{"b"}
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/data-quality/profiles", token, q, 403)
	q["deviceIds"] = []string{"a"}
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/data-quality/profiles/publish", token, q, 403)
	body["toleranceMs"] = 500
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/data-quality/profiles", token, q, 422)
	body["toleranceMs"] = 100
	run := requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/data-quality/runs", token, map[string]any{"deviceIds": []string{"a"}, "start": start, "end": start + 40000, "parameters": model.QualityRunParameters{AttributeIDs: []string{"pressure"}, ProfileRevisionIDs: []string{profile["id"].(string)}}, "idempotencyKey": "read-permission"}, 202)
	// Ordinary series reading is granted through the application menu and
	// device scope. A queued run reports its state conflict, not an unassignable
	// GET-action permission failure.
	requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/data-quality/runs/"+run["id"].(string)+"/series?deviceId=a&attributeId=pressure", token, nil, 409)
	upload := func(scope, device string, status int) map[string]any {
		t.Helper()
		var b bytes.Buffer
		writer := multipart.NewWriter(&b)
		_ = writer.WriteField("deviceIdsJSON", `["`+device+`"]`)
		_ = writer.WriteField("scope", scope)
		file, e := writer.CreateFormFile("file", "校准依据.txt")
		if e != nil {
			t.Fatal(e)
		}
		_, _ = io.WriteString(file, "受控测试附件")
		_ = writer.Close()
		r, e := http.NewRequest("POST", server.URL+"/api/v1/data-quality/calibrations/attachments", &b)
		if e != nil {
			t.Fatal(e)
		}
		r.Header.Set("Content-Type", writer.FormDataContentType())
		r.Header.Set("Authorization", "Bearer "+token)
		resp, e := server.Client().Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != status {
			t.Fatalf("upload status %d: %s", resp.StatusCode, data)
		}
		var result map[string]any
		_ = json.Unmarshal(data, &result)
		return result
	}
	attachment := upload("personal", "a", 201)
	if strings.Contains(string(mustQualityJSON(t, attachment)), "storageKey") || attachment["objectKey"] != nil {
		t.Fatal("server storage identity exposed", attachment)
	}
	upload("shared", "a", 403)
	upload("personal", "b", 403)
	id := attachment["id"].(string)
	r, _ := http.NewRequest("GET", server.URL+"/api/v1/data-quality/calibrations/attachments/"+id, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	resp, err := server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(data) != "受控测试附件" {
		t.Fatal(resp.StatusCode, string(data))
	}
	requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/data-quality/calibrations/attachments/"+id, other, nil, 403)
	calibration := map[string]any{"resourceId": "calibration-a", "scope": "personal", "deviceIds": []string{"a"}, "body": map[string]any{"deviceId": "a", "attributeId": "pressure", "calibratedAt": start, "basis": "受控校准说明", "implementedBy": "测试人员", "attachments": []string{id}}}
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/data-quality/calibrations", token, calibration, 201)
	calibration["body"].(map[string]any)["objectKey"] = "external-object"
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/data-quality/calibrations", token, calibration, 422)
	state, err = repo.LoadAccessState(ctx, "t")
	if err != nil {
		t.Fatal(err)
	}
	state.Users[0].DeviceIDs = []string{}
	if ok, err := repo.SaveAccessState(ctx, "t", state); err != nil || !ok {
		t.Fatal(ok, err)
	}
	requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/data-quality/calibrations/attachments/"+id, token, nil, 403)
}
func mustQualityJSON(t *testing.T, value any) []byte {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
