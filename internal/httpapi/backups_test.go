package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"             /* 执行当前语句并推进处理流程。 */
	"encoding/json"     /* 执行当前语句并推进处理流程。 */
	"io"                /* 执行当前语句并推进处理流程。 */
	"log/slog"          /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */
	"time"              /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics"         /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestBackupEndpointsProxyRecordsFilesAndAdminActions(t *testing.T) { /* 定义 TestBackupEndpointsProxyRecordsFilesAndAdminActions 函数。 */
	var calls []string                                                                                 /* 声明 calls。 */
	backupServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 backupServer 的值。 */
		if r.Header.Get("Authorization") != "Bearer internal-backup-token" { /* 判断条件并选择处理分支。 */
			http.Error(w, "missing internal authorization", http.StatusUnauthorized) /* 执行当前语句并推进处理流程。 */
			return                                                                   /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		calls = append(calls, r.Method+" "+r.URL.RequestURI()) /* 更新 calls 的值。 */
		w.Header().Set("Content-Type", "application/json")     /* 执行当前语句并推进处理流程。 */
		switch r.URL.Path {                                    /* 根据条件选择处理路径。 */
		case "/backups": /* 处理当前分支。 */
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"id": "backup_full_1", "type": "FULL", "status": "COMPLETED"}}, "total": 1, "limit": 50, "offset": 0}) /* 更新 _ 的值。 */
		case "/backups/backup_full_1": /* 处理当前分支。 */
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "backup_full_1", "type": "FULL", "status": "COMPLETED", "checksum": "abc"}) /* 更新 _ 的值。 */
		case "/backups/backup_full_1/files": /* 处理当前分支。 */
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "backup_full_1", "type": "FULL", "artifacts": []map[string]any{{"filename": "manifest.json", "size": 12, "sha256": "abc"}}}) /* 更新 _ 的值。 */
		case "/backups/backup_full_1/files/manifest.json": /* 处理当前分支。 */
			w.Header().Set("Content-Type", "application/octet-stream")                    /* 执行当前语句并推进处理流程。 */
			w.Header().Set("Content-Disposition", `attachment; filename="manifest.json"`) /* 执行当前语句并推进处理流程。 */
			w.Header().Set("X-Checksum-SHA256", "abc")                                    /* 执行当前语句并推进处理流程。 */
			_, _ = io.WriteString(w, "manifest-body")                                     /* 更新 _ 的值。 */
		case "/backup": /* 处理当前分支。 */
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "backup_full_2", "type": r.URL.Query().Get("type")}) /* 更新 _ 的值。 */
		case "/restore/drill": /* 处理当前分支。 */
			_ = json.NewEncoder(w).Encode(map[string]any{"drillId": "drill_1", "backupId": r.URL.Query().Get("backupId"), "status": "COMPLETED", "artifactsChecked": 1}) /* 更新 _ 的值。 */
		default: /* 处理当前分支。 */
			http.NotFound(w, r) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	})) /* 结束当前表达式或代码块。 */
	defer backupServer.Close() /* 安排函数结束时执行清理。 */

	cfg := config.Config{BackupURL: backupServer.URL, BackupToken: "internal-backup-token", JWTSecret: "test-backup-secret-at-least-32-characters", CORSAllowedOrigins: []string{}} /* 更新 cfg 的值。 */
	engine := &core.Engine{Repo: memory.NewRepository()}                                                                                                                            /* 更新 engine 的值。 */
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))                                                                                          /* 更新 api 的值。 */
	server := httptest.NewServer(api.Handler())                                                                                                                                     /* 更新 server 的值。 */
	defer server.Close()                                                                                                                                                            /* 安排函数结束时执行清理。 */
	adminToken, err := api.auth.Issue("admin", "tenant_001", "admin", nil, time.Hour)                                                                                               /* 检查错误并决定后续处理。 */
	if err != nil {                                                                                                                                                                 /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	viewerToken, err := api.auth.Issue("viewer", "tenant_001", "viewer", nil, time.Hour) /* 检查错误并决定后续处理。 */
	if err != nil {                                                                      /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	list := backupJSONRequest(t, server.Client(), http.MethodGet, server.URL+"/api/v1/backups?type=FULL", adminToken, nil, http.StatusOK) /* 更新 list 的值。 */
	if list["total"] != float64(1) {                                                                                                      /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected backup list: %#v", list) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	detail := backupJSONRequest(t, server.Client(), http.MethodGet, server.URL+"/api/v1/backups/backup_full_1", viewerToken, nil, http.StatusOK) /* 更新 detail 的值。 */
	if detail["id"] != "backup_full_1" {                                                                                                         /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected backup detail: %#v", detail) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	backupJSONRequest(t, server.Client(), http.MethodGet, server.URL+"/api/v1/backups/backup_full_1/files", viewerToken, nil, http.StatusOK) /* 执行当前语句并推进处理流程。 */
	viewerDownloadReq, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/backups/backup_full_1/files/manifest.json", nil)             /* 更新 _ 的值。 */
	viewerDownloadReq.Header.Set("Authorization", "Bearer "+viewerToken)                                                                     /* 执行当前语句并推进处理流程。 */
	viewerDownloadResp, err := server.Client().Do(viewerDownloadReq)                                                                         /* 更新 err 的值。 */
	if err != nil {                                                                                                                          /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	viewerDownloadResp.Body.Close()                            /* 执行当前语句并推进处理流程。 */
	if viewerDownloadResp.StatusCode != http.StatusForbidden { /* 判断条件并选择处理分支。 */
		t.Fatalf("viewer download status=%d want=%d", viewerDownloadResp.StatusCode, http.StatusForbidden) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	downloadReq, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/backups/backup_full_1/files/manifest.json", nil) /* 更新 _ 的值。 */
	downloadReq.Header.Set("Authorization", "Bearer "+adminToken)                                                          /* 执行当前语句并推进处理流程。 */
	downloadResp, err := server.Client().Do(downloadReq)                                                                   /* 更新 err 的值。 */
	if err != nil {                                                                                                        /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	body, _ := io.ReadAll(downloadResp.Body)                                                                                                  /* 更新 _ 的值。 */
	downloadResp.Body.Close()                                                                                                                 /* 执行当前语句并推进处理流程。 */
	if downloadResp.StatusCode != http.StatusOK || string(body) != "manifest-body" || downloadResp.Header.Get("X-Checksum-SHA256") != "abc" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected backup download status=%d headers=%v body=%q", downloadResp.StatusCode, downloadResp.Header, body) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	run := backupJSONRequest(t, server.Client(), http.MethodPost, server.URL+"/api/v1/backups", adminToken, map[string]any{"type": "INCREMENTAL"}, http.StatusOK) /* 更新 run 的值。 */
	if run["type"] != "INCREMENTAL" {                                                                                                                             /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected manual backup response: %#v", run) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	drill := backupJSONRequest(t, server.Client(), http.MethodPost, server.URL+"/api/v1/backups/backup_full_1/restore-drill", adminToken, nil, http.StatusOK) /* 更新 drill 的值。 */
	if drill["backupId"] != "backup_full_1" {                                                                                                                 /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected restore drill response: %#v", drill) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	backupJSONRequest(t, server.Client(), http.MethodPost, server.URL+"/api/v1/backups", viewerToken, map[string]any{"type": "FULL"}, http.StatusForbidden) /* 执行当前语句并推进处理流程。 */

	joined := ""                 /* 更新 joined 的值。 */
	for _, call := range calls { /* 循环处理当前数据。 */
		joined += call + "\n" /* 更新 joined 的值。 */
	} /* 结束当前表达式或代码块。 */
	if !bytes.Contains([]byte(joined), []byte("GET /backups?limit=20&offset=0&type=FULL")) || !bytes.Contains([]byte(joined), []byte("GET /backups/backup_full_1/files?limit=20&offset=0")) || !bytes.Contains([]byte(joined), []byte("POST /backup?type=INCREMENTAL")) || !bytes.Contains([]byte(joined), []byte("POST /restore/drill?backupId=backup_full_1")) { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected backup-service calls: %s", joined) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestBackupEndpointSurfacesUpstreamFailureDetail(t *testing.T) { /* 定义 TestBackupEndpointSurfacesUpstreamFailureDetail 函数。 */
	backupServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 backupServer 的值。 */
		if r.Header.Get("Authorization") != "Bearer internal-backup-token" { /* 判断条件并选择处理分支。 */
			http.Error(w, "missing internal authorization", http.StatusUnauthorized) /* 执行当前语句并推进处理流程。 */
			return                                                                   /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		w.Header().Set("Content-Type", "application/json")                                                  /* 执行当前语句并推进处理流程。 */
		w.WriteHeader(http.StatusInternalServerError)                                                       /* 执行当前语句并推进处理流程。 */
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "postgres: pg_dump exited with status 1"}) /* 更新 _ 的值。 */
	})) /* 结束当前表达式或代码块。 */
	defer backupServer.Close() /* 安排函数结束时执行清理。 */

	cfg := config.Config{BackupURL: backupServer.URL, BackupToken: "internal-backup-token", JWTSecret: "test-backup-secret-at-least-32-characters", CORSAllowedOrigins: []string{}} /* 更新 cfg 的值。 */
	engine := &core.Engine{Repo: memory.NewRepository()}                                                                                                                            /* 更新 engine 的值。 */
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))                                                                                          /* 更新 api 的值。 */
	server := httptest.NewServer(api.Handler())                                                                                                                                     /* 更新 server 的值。 */
	defer server.Close()                                                                                                                                                            /* 安排函数结束时执行清理。 */
	adminToken, err := api.auth.Issue("admin", "tenant_001", "admin", nil, time.Hour)                                                                                               /* 检查错误并决定后续处理。 */
	if err != nil {                                                                                                                                                                 /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	result := backupJSONRequest(t, server.Client(), http.MethodPost, server.URL+"/api/v1/backups", adminToken, map[string]any{"type": "FULL"}, http.StatusBadGateway) /* 更新 result 的值。 */
	detail, _ := result["detail"].(string)                                                                                                                            /* 更新 _ 的值。 */
	if !bytes.Contains([]byte(detail), []byte("pg_dump exited with status 1")) {                                                                                      /* 判断条件并选择处理分支。 */
		t.Fatalf("backup upstream detail was lost: %q", detail) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func backupJSONRequest(t *testing.T, client *http.Client, method, endpoint, token string, body any, wantStatus int) map[string]any { /* 定义 backupJSONRequest 函数。 */
	t.Helper()           /* 执行当前语句并推进处理流程。 */
	var reader io.Reader /* 声明 reader。 */
	if body != nil {     /* 判断条件并选择处理分支。 */
		payload, err := json.Marshal(body) /* 更新 err 的值。 */
		if err != nil {                    /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		reader = bytes.NewReader(payload) /* 更新 reader 的值。 */
	} /* 结束当前表达式或代码块。 */
	request, err := http.NewRequest(method, endpoint, reader) /* 更新 err 的值。 */
	if err != nil {                                           /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if body != nil { /* 判断条件并选择处理分支。 */
		request.Header.Set("Content-Type", "application/json") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	request.Header.Set("Authorization", "Bearer "+token) /* 执行当前语句并推进处理流程。 */
	response, err := client.Do(request)                  /* 更新 err 的值。 */
	if err != nil {                                      /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer response.Body.Close()                                           /* 安排函数结束时执行清理。 */
	var result map[string]any                                             /* 声明 result。 */
	if err = json.NewDecoder(response.Body).Decode(&result); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatalf("decode %s: %v", endpoint, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if response.StatusCode != wantStatus { /* 判断条件并选择处理分支。 */
		t.Fatalf("%s status=%d want=%d body=%#v", endpoint, response.StatusCode, wantStatus, result) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	return result /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
