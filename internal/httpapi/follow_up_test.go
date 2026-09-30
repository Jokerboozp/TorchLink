package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/analytics/response"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
)

func TestFollowUpCatalogUsesCurrentScopeBeforePaginationAndCounts(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	for _, id := range []string{"d1", "hidden"} {
		if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: id, Name: id, AccessKey: id}); err != nil {
			t.Fatal(err)
		}
	}
	state, err := repo.LoadAccessState(ctx, "t")
	if err != nil {
		t.Fatal(err)
	}
	state.Users = []model.PlatformUser{{Username: "reader", Enabled: true, SessionVersion: 1, DeviceScope: "selected", DeviceIDs: []string{"d1"}, Permissions: []string{"menu:devices", "menu:response"}}}
	if ok, err := repo.SaveAccessState(ctx, "t", state); err != nil || !ok {
		t.Fatal(ok, err)
	}
	api := New(config.Config{AdminUser: "admin", AdminTenants: []string{"t"}, JWTSecret: "follow-up-scope-secret-32-characters", DevMode: true}, &core.Engine{Repo: repo}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, v := range []struct{ id, device string }{{"mine", "d1"}, {"unreadable", "hidden"}} {
		body, _ := json.Marshal(model.CorrectiveAction{Title: v.id, Owner: "owner", Status: "OPEN", DueAt: time.Now().Add(time.Hour).UnixMilli()})
		_, err := api.analysis.Store.PutAnalysisConfig(ctx, model.AnalysisConfigRevision{ID: v.id + "-revision", TenantID: "t", Kind: response.CorrectiveKind, ResourceID: v.id, Scope: "SHARED", DeviceIDs: []string{v.device}, Creator: "admin", Body: body}, 0)
		if err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, err := api.auth.IssueUser("reader", "t", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	page := requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/follow-up-sources?limit=1", token, nil, 200)
	if page["total"] != float64(1) || page["items"].([]any)[0].(map[string]any)["resourceId"] != "mine" {
		t.Fatal("hidden main source leaked", page)
	}
	page = requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/follow-up-sources?limit=1&offset=1", token, nil, 200)
	if page["total"] != float64(1) || len(page["items"].([]any)) != 0 {
		t.Fatal("count/pagination used unfiltered sources", page)
	}
	state, err = repo.LoadAccessState(ctx, "t")
	if err != nil {
		t.Fatal(err)
	}
	state.Users[0].Permissions = []string{"menu:devices"}
	if ok, err := repo.SaveAccessState(ctx, "t", state); err != nil || !ok {
		t.Fatal(ok, err)
	}
	requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/follow-up-sources", token, nil, 403)
}
