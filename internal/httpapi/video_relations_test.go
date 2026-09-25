package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"io"                /* 执行当前语句并推进处理流程。 */
	"log/slog"          /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/adapters/local"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics"         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"          /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestVideoCameraRelationsHTTPEnforcesCameraToOneDevice(t *testing.T) { /* 定义 TestVideoCameraRelationsHTTPEnforcesCameraToOneDevice 函数。 */
	repo := memory.NewRepository()                /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir()) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 engine 的值。 */
	engine.Metrics = metrics.New()                                                                                                                                                    /* 更新 engine.Metrics 的值。 */
	if err = engine.Start(context.Background()); err != nil {                                                                                                                         /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cfg := config.Load()                                                                                           /* 更新 cfg 的值。 */
	cfg.DevMode = true                                                                                             /* 更新 cfg.DevMode 的值。 */
	cfg.JWTSecret = "test-secret-at-least-32-characters"                                                           /* 更新 cfg.JWTSecret 的值。 */
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), slog.New(slog.NewTextHandler(io.Discard, nil)))    /* 更新 api 的值。 */
	server := httptest.NewServer(api.Handler())                                                                    /* 更新 server 的值。 */
	defer server.Close()                                                                                           /* 安排函数结束时执行清理。 */
	login := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/auth/login", "", map[string]any{ /* 更新 login 的值。 */
		"username": "admin", "password": "admin123", "tenantId": "tenant_001", /* 执行当前语句并推进处理流程。 */
	}, http.StatusOK) /* 结束当前表达式或代码块。 */
	token := login["accessToken"].(string)                              /* 更新 token 的值。 */
	for index, deviceID := range []string{"device-001", "device-002"} { /* 循环处理当前数据。 */
		if err := repo.SaveManagedDevice(context.Background(), model.ManagedDevice{ID: deviceID, TenantID: "tenant_001", ProductID: "product", Name: deviceID, Status: "ENABLED", AccessKey: "access-" + string(rune('a'+index))}); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */

	camera := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/integrations/video/cameras", token, map[string]any{ /* 更新 camera 的值。 */
		"cameraId": "camera_one_device", "cameraName": "一号摄像头", "brand": "海康", "cameraPoint": "东侧入口", /* 执行当前语句并推进处理流程。 */
		"building": "A", "floor": "1", "room": "大厅", "deviceId": "device-001", "enabled": true, /* 执行当前语句并推进处理流程。 */
	}, http.StatusCreated) /* 结束当前表达式或代码块。 */
	if camera["deviceId"] != "device-001" || camera["brand"] != "海康" || camera["cameraPoint"] != "东侧入口" { /* 判断条件并选择处理分支。 */
		t.Fatalf("saved camera metadata = %#v", camera) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/integrations/video/cameras", token, map[string]any{ /* 执行当前语句并推进处理流程。 */
		"cameraId": "camera_two", "cameraName": "二号摄像头", "deviceId": "device-001", "enabled": true, /* 执行当前语句并推进处理流程。 */
	}, http.StatusCreated) /* 结束当前表达式或代码块。 */
	result := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/integrations/video/relations?relationType=device&targetId=device-001", token, nil, http.StatusOK) /* 更新 result 的值。 */
	items := result["items"].([]any)                                                                                                                                                /* 更新 items 的值。 */
	if len(items) != 2 {                                                                                                                                                            /* 判断条件并选择处理分支。 */
		t.Fatalf("reverse device lookup = %#v", result) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/integrations/video/relations?relationType=floor&targetId=1", token, nil, http.StatusUnprocessableEntity) /* 执行当前语句并推进处理流程。 */
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/integrations/video/cameras", token, map[string]any{                                                     /* 执行当前语句并推进处理流程。 */
		"cameraId": "camera_invalid", "cameraName": "非法摄像头", "relatedDeviceIds": []string{"device-001", "device-002"}, "enabled": true, /* 执行当前语句并推进处理流程。 */
	}, http.StatusUnprocessableEntity) /* 结束当前表达式或代码块。 */
	viewer := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/integrations/video/cameras", token, nil, http.StatusOK) /* 更新 viewer 的值。 */
	for _, item := range viewer["items"].([]any) {                                                                                        /* 循环处理当前数据。 */
		row := item.(map[string]any)                                                                                              /* 更新 row 的值。 */
		if _, exposed := row["streamUrl"]; exposed || row["cameraId"] == "camera_one_device" && row["deviceId"] != "device-001" { /* 判断条件并选择处理分支。 */
			t.Fatalf("camera response contains stream data or lost device relation: %#v", row) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
