package httpapi

import (
	"context"
	"io"
	"iot-platform/internal/devicescope"
	"log/slog"
	"net/http/httptest"
	"slices"
	"testing"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
)

type deadLetterTestBus struct {
	*local.Bus
	replayed []string
}

func (b *deadLetterTestBus) DeadLetters(_ context.Context, group string, _ int) (int64, []model.DeadLetter, error) {
	if group != "processor" {
		return 0, nil, nil
	}
	return 1, []model.DeadLetter{{Group: group, Partition: 0, Offset: 7, Key: "t\x00d", SourceTopic: model.TopicDeviceBusiness, Error: "boom"}}, nil
}

func (b *deadLetterTestBus) ReplayDeadLetter(_ context.Context, group string, partition int, offset int64) (model.DeadLetter, error) {
	if offset != 7 {
		return model.DeadLetter{}, model.ErrNotFound
	}
	b.replayed = append(b.replayed, group)
	return model.DeadLetter{Group: group, SourceTopic: model.DeadLetterGroups[group]}, nil
}

func TestOpsDeadLettersListAndReplay(t *testing.T) {
	repo := &auditingRepo{Repository: memory.NewRepository()}
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword = "root", "root-password-test"
	cfg.AdminTenants = []string{"tenant_ops"}
	cfg.JWTSecret = "test-only-secret-for-dlq-at-least-32"
	cfg.DevMode = true
	cfg.Ops.Tenants = []string{"tenant_ops"}
	bus := &deadLetterTestBus{Bus: local.NewBus()}
	api := New(cfg, &core.Engine{Repo: devicescope.Wrap(repo), Bus: bus}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	req := func(method, path, token string, body any, status int) map[string]any {
		t.Helper()
		return requestJSON(t, server.Client(), method, server.URL+path, token, body, status)
	}
	root := req("POST", "/api/v1/auth/login", "", map[string]any{"username": "root", "password": cfg.AdminPassword, "tenantId": "tenant_ops"}, 200)["accessToken"].(string)
	groups := req("GET", "/api/v1/ops/overview/dead-letters", root, nil, 200)["groups"].([]any)
	if len(groups) != len(model.DeadLetterGroups) {
		t.Fatalf("groups=%v", groups)
	}
	req("GET", "/api/v1/ops/overview/dead-letters?group=unknown", root, nil, 422)
	req("POST", "/api/v1/ops/overview/dead-letters/processor/0/7/replay", root, nil, 202)
	req("POST", "/api/v1/ops/overview/dead-letters/processor/0/8/replay", root, nil, 404)
	req("POST", "/api/v1/ops/overview/dead-letters/other/0/7/replay", root, nil, 422)
	if !slices.Equal(bus.replayed, []string{"processor"}) || !slices.Contains(repo.actions(), "ops.dead_letter.replay@tenant_ops") {
		t.Fatalf("replayed=%v audits=%v", bus.replayed, repo.actions())
	}
	// Viewing dead letters is an explicit action, not implied by the menu.
	user := map[string]any{"username": "viewer", "password": "viewer-password-1", "enabled": true, "permissions": []string{"menu:opsOverview"}, "deviceScope": "all"}
	req("POST", "/api/v1/access/users", root, user, 200)
	viewer := req("POST", "/api/v1/auth/login", "", map[string]any{"username": "viewer", "password": "viewer-password-1", "tenantId": "tenant_ops"}, 200)["accessToken"].(string)
	req("GET", "/api/v1/ops/overview/dead-letters", viewer, nil, 403)
	req("POST", "/api/v1/ops/overview/dead-letters/processor/0/7/replay", viewer, nil, 403)
}
