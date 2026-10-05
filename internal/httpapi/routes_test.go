package httpapi

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"testing"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
)

// The registered routes are pinned so reorganizing route registration cannot
// add, drop or change a route unnoticed. Set IOT_UPDATE_ROUTES=1 to rewrite
// testdata/routes.txt after an intended route change.
func TestRegisteredRoutesMatchSnapshot(t *testing.T) {
	api := New(config.Config{DevMode: true}, &core.Engine{Repo: memory.NewRepository()}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	routes := []string{}
	for _, r := range api.router.Routes() {
		routes = append(routes, r.Method+" "+r.Path)
	}
	sort.Strings(routes)
	got := strings.Join(routes, "\n") + "\n"
	if os.Getenv("IOT_UPDATE_ROUTES") == "1" {
		if err := os.WriteFile("testdata/routes.txt", []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile("testdata/routes.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("registered routes changed; run with IOT_UPDATE_ROUTES=1 if intended\n%s", diffLines(string(want), got))
	}
	// docs/API.md is generated from the same routes and the permission catalog.
	doc := apiReference(api)
	if os.Getenv("IOT_UPDATE_ROUTES") == "1" {
		if err := os.WriteFile("../../docs/API.md", []byte(doc), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if current, err := os.ReadFile("../../docs/API.md"); err != nil || string(current) != doc {
		t.Fatal("docs/API.md is out of date; run with IOT_UPDATE_ROUTES=1")
	}
}

// apiReference lists every route grouped by its resource, with the menu that
// shows it and the separately granted operation, if any.
func apiReference(api *Server) string {
	actions := map[string]string{}
	for _, item := range api.permissionCatalog() {
		if item.Kind == "action" {
			actions[item.ID] = item.Name
		}
	}
	groups := map[string][]string{}
	for _, r := range api.router.Routes() {
		parts := strings.Split(strings.Trim(r.Path, "/"), "/")
		group := "/" + parts[0]
		if len(parts) > 2 && parts[0] == "api" {
			group = "/" + strings.Join(parts[:3], "/")
		}
		menu, ok := opsRouteMenu(r.Path)
		if !ok {
			menu = routeMenu(r.Path)
		}
		name := menuNames[menu]
		if name == "" {
			name = "—"
		}
		action := actions[r.Method+" "+r.Path]
		if action == "" {
			action = "—"
		}
		groups[group] = append(groups[group], fmt.Sprintf("| `%s` | `%s` | %s | %s |", r.Method, r.Path, name, action))
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("# 接口清单\n\n")
	b.WriteString("本文件由 `IOT_UPDATE_ROUTES=1 go test ./internal/httpapi -run TestRegisteredRoutesMatchSnapshot` 根据已注册路由生成，请勿手工编辑。")
	b.WriteString("“菜单”为可见该接口所需的菜单权限；“单独授权的操作”为角色与用户权限中需另外勾选的操作，“—”表示随菜单或登录状态授权。")
	b.WriteString("所有业务接口还受租户隔离与用户设备范围约束，开放接口 `/api/open/v1` 使用绑定用户的密钥，见 [设备接入与协议](INTEGRATION.md)。\n")
	for _, key := range keys {
		rows := groups[key]
		sort.Slice(rows, func(i, j int) bool {
			pi, pj := strings.SplitN(rows[i], "|", 4)[2], strings.SplitN(rows[j], "|", 4)[2]
			return pi < pj || pi == pj && rows[i] < rows[j]
		})
		fmt.Fprintf(&b, "\n## `%s`\n\n| 方法 | 路径 | 菜单 | 单独授权的操作 |\n| --- | --- | --- | --- |\n%s\n", key, strings.Join(rows, "\n"))
	}
	return b.String()
}

func diffLines(want, got string) string {
	wantSet, gotSet := map[string]bool{}, map[string]bool{}
	for _, l := range strings.Split(want, "\n") {
		wantSet[l] = true
	}
	for _, l := range strings.Split(got, "\n") {
		gotSet[l] = true
	}
	var b strings.Builder
	for l := range wantSet {
		if !gotSet[l] {
			b.WriteString("- " + l + "\n")
		}
	}
	for l := range gotSet {
		if !wantSet[l] {
			b.WriteString("+ " + l + "\n")
		}
	}
	return b.String()
}
