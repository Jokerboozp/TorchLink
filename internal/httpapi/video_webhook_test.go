package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"             /* 执行当前语句并推进处理流程。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"crypto/hmac"       /* 执行当前语句并推进处理流程。 */
	"crypto/sha256"     /* 执行当前语句并推进处理流程。 */
	"encoding/hex"      /* 执行当前语句并推进处理流程。 */
	"io"                /* 执行当前语句并推进处理流程。 */
	"log/slog"          /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"strconv"           /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */
	"time"              /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/adapters/local"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics"         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"          /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestVideoWebhookEnforcesPlatformTenantBinding(t *testing.T) { /* 定义 TestVideoWebhookEnforcesPlatformTenantBinding 函数。 */
	repo := memory.NewRepository()                /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir()) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 engine 的值。 */
	engine.Metrics = metrics.New()                                                                                                                                                    /* 更新 engine.Metrics 的值。 */
	cfg := config.Load()                                                                                                                                                              /* 更新 cfg 的值。 */
	cfg.DevMode = false                                                                                                                                                               /* 更新 cfg.DevMode 的值。 */
	cfg.JWTSecret = "test-secret-at-least-32-characters"                                                                                                                              /* 更新 cfg.JWTSecret 的值。 */
	cfg.VideoSecrets = map[string]string{"video-a": "secret-a"}                                                                                                                       /* 更新 cfg.VideoSecrets 的值。 */
	cfg.VideoPlatformTenants = map[string]string{"video-a": "tenant-a"}                                                                                                               /* 更新 cfg.VideoPlatformTenants 的值。 */
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), slog.New(slog.NewTextHandler(io.Discard, nil)))                                                                       /* 更新 api 的值。 */
	server := httptest.NewServer(api.Handler())                                                                                                                                       /* 更新 server 的值。 */
	defer server.Close()                                                                                                                                                              /* 安排函数结束时执行清理。 */
	if err := repo.SaveVideoCameraMapping(context.Background(), model.VideoCameraMapping{TenantID: "tenant-a", CameraID: "camera-a", Enabled: true}); err != nil {                    /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	valid := []byte(`{"eventId":"video-binding-valid","tenantId":"tenant-a","cameraId":"camera-a","alarmType":"FIRE"}`) /* 更新 valid 的值。 */
	status, _ := postSignedVideo(t, server.URL, valid, "video-a", "secret-a")                                           /* 更新 _ 的值。 */
	if status != http.StatusCreated {                                                                                   /* 判断条件并选择处理分支。 */
		t.Fatalf("valid bound webhook status=%d", status) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	unbound := []byte(`{"eventId":"video-binding-unbound","tenantId":"tenant-a","cameraId":"camera-b","alarmType":"FIRE"}`) /* 更新 unbound 的值。 */
	status, _ = postSignedVideo(t, server.URL, unbound, "video-a", "secret-a")                                              /* 更新 _ 的值。 */
	if status != http.StatusForbidden {                                                                                     /* 判断条件并选择处理分支。 */
		t.Fatalf("unbound camera webhook status=%d, want 403", status) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	spoofed := []byte(`{"eventId":"video-binding-spoofed","tenantId":"tenant-b","cameraId":"camera-b","alarmType":"FIRE"}`) /* 更新 spoofed 的值。 */
	status, _ = postSignedVideo(t, server.URL, spoofed, "video-a", "secret-a")                                              /* 更新 _ 的值。 */
	if status != http.StatusForbidden {                                                                                     /* 判断条件并选择处理分支。 */
		t.Fatalf("cross-tenant webhook status=%d, want 403", status) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func postSignedVideo(t *testing.T, baseURL string, body []byte, platform, secret string) (int, []byte) { /* 定义 postSignedVideo 函数。 */
	t.Helper()                                                                                                      /* 执行当前语句并推进处理流程。 */
	ts := time.Now().Unix()                                                                                         /* 更新 ts 的值。 */
	timestamp := fmtInt64(ts)                                                                                       /* 更新 timestamp 的值。 */
	mac := hmac.New(sha256.New, []byte(secret))                                                                     /* 更新 mac 的值。 */
	_, _ = mac.Write([]byte(timestamp))                                                                             /* 更新 _ 的值。 */
	_, _ = mac.Write(body)                                                                                          /* 更新 _ 的值。 */
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/integrations/video/alarm", bytes.NewReader(body)) /* 更新 err 的值。 */
	if err != nil {                                                                                                 /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	req.Header.Set("Content-Type", "application/json")              /* 执行当前语句并推进处理流程。 */
	req.Header.Set("X-Video-Platform-ID", platform)                 /* 执行当前语句并推进处理流程。 */
	req.Header.Set("X-Timestamp", timestamp)                        /* 执行当前语句并推进处理流程。 */
	req.Header.Set("X-Signature", hex.EncodeToString(mac.Sum(nil))) /* 执行当前语句并推进处理流程。 */
	resp, err := http.DefaultClient.Do(req)                         /* 更新 err 的值。 */
	if err != nil {                                                 /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer resp.Body.Close()          /* 安排函数结束时执行清理。 */
	data, _ := io.ReadAll(resp.Body) /* 更新 _ 的值。 */
	return resp.StatusCode, data     /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func fmtInt64(v int64) string { return strconv.FormatInt(v, 10) } /* 定义 fmtInt64 函数。 */
