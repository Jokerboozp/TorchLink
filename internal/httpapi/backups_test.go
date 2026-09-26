package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
)

func TestBackupEndpointsProxyRecordsFilesAndAdminActions(t *testing.T) {
	var calls []string
	backupServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer internal-backup-token" {
			http.Error(w, "missing internal authorization", http.StatusUnauthorized)
			return
		}
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/backups":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"id": "backup_full_1", "type": "FULL", "status": "COMPLETED"}}, "total": 1, "limit": 50, "offset": 0})
		case "/backups/backup_full_1":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "backup_full_1", "type": "FULL", "status": "COMPLETED", "checksum": "abc"})
		case "/backups/backup_full_1/files":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "backup_full_1", "type": "FULL", "artifacts": []map[string]any{{"filename": "manifest.json", "size": 12, "sha256": "abc"}}})
		case "/backups/backup_full_1/files/manifest.json":
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", `attachment; filename="manifest.json"`)
			w.Header().Set("X-Checksum-SHA256", "abc")
			_, _ = io.WriteString(w, "manifest-body")
		case "/backup":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "backup_full_2", "type": r.URL.Query().Get("type")})
		case "/restore/drill":
			_ = json.NewEncoder(w).Encode(map[string]any{"drillId": "drill_1", "backupId": r.URL.Query().Get("backupId"), "status": "COMPLETED", "artifactsChecked": 1})
		default:
			http.NotFound(w, r)
		}
	}))
	defer backupServer.Close()

	cfg := config.Config{BackupURL: backupServer.URL, BackupToken: "internal-backup-token", JWTSecret: "test-backup-secret-at-least-32-characters", CORSAllowedOrigins: []string{}}
	engine := &core.Engine{Repo: memory.NewRepository()}
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	adminToken, err := api.auth.Issue("admin", "tenant_001", "admin", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	viewerToken, err := api.auth.Issue("viewer", "tenant_001", "viewer", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	list := backupJSONRequest(t, server.Client(), http.MethodGet, server.URL+"/api/v1/backups?type=FULL", adminToken, nil, http.StatusOK)
	if list["total"] != float64(1) {
		t.Fatalf("unexpected backup list: %#v", list)
	}
	detail := backupJSONRequest(t, server.Client(), http.MethodGet, server.URL+"/api/v1/backups/backup_full_1", viewerToken, nil, http.StatusOK)
	if detail["id"] != "backup_full_1" {
		t.Fatalf("unexpected backup detail: %#v", detail)
	}
	backupJSONRequest(t, server.Client(), http.MethodGet, server.URL+"/api/v1/backups/backup_full_1/files", viewerToken, nil, http.StatusOK)
	viewerDownloadReq, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/backups/backup_full_1/files/manifest.json", nil)
	viewerDownloadReq.Header.Set("Authorization", "Bearer "+viewerToken)
	viewerDownloadResp, err := server.Client().Do(viewerDownloadReq)
	if err != nil {
		t.Fatal(err)
	}
	viewerDownloadResp.Body.Close()
	if viewerDownloadResp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer download status=%d want=%d", viewerDownloadResp.StatusCode, http.StatusForbidden)
	}
	downloadReq, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/backups/backup_full_1/files/manifest.json", nil)
	downloadReq.Header.Set("Authorization", "Bearer "+adminToken)
	downloadResp, err := server.Client().Do(downloadReq)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(downloadResp.Body)
	downloadResp.Body.Close()
	if downloadResp.StatusCode != http.StatusOK || string(body) != "manifest-body" || downloadResp.Header.Get("X-Checksum-SHA256") != "abc" {
		t.Fatalf("unexpected backup download status=%d headers=%v body=%q", downloadResp.StatusCode, downloadResp.Header, body)
	}

	run := backupJSONRequest(t, server.Client(), http.MethodPost, server.URL+"/api/v1/backups", adminToken, map[string]any{"type": "INCREMENTAL"}, http.StatusOK)
	if run["type"] != "INCREMENTAL" {
		t.Fatalf("unexpected manual backup response: %#v", run)
	}
	drill := backupJSONRequest(t, server.Client(), http.MethodPost, server.URL+"/api/v1/backups/backup_full_1/restore-drill", adminToken, nil, http.StatusOK)
	if drill["backupId"] != "backup_full_1" {
		t.Fatalf("unexpected restore drill response: %#v", drill)
	}
	backupJSONRequest(t, server.Client(), http.MethodPost, server.URL+"/api/v1/backups", viewerToken, map[string]any{"type": "FULL"}, http.StatusForbidden)

	joined := ""
	for _, call := range calls {
		joined += call + "\n"
	}
	if !bytes.Contains([]byte(joined), []byte("GET /backups?limit=20&offset=0&type=FULL")) || !bytes.Contains([]byte(joined), []byte("GET /backups/backup_full_1/files?limit=20&offset=0")) || !bytes.Contains([]byte(joined), []byte("POST /backup?type=INCREMENTAL")) || !bytes.Contains([]byte(joined), []byte("POST /restore/drill?backupId=backup_full_1")) {
		t.Fatalf("unexpected backup-service calls: %s", joined)
	}
}

func TestBackupEndpointSurfacesUpstreamFailureDetail(t *testing.T) {
	backupServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer internal-backup-token" {
			http.Error(w, "missing internal authorization", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "postgres: pg_dump exited with status 1"})
	}))
	defer backupServer.Close()

	cfg := config.Config{BackupURL: backupServer.URL, BackupToken: "internal-backup-token", JWTSecret: "test-backup-secret-at-least-32-characters", CORSAllowedOrigins: []string{}}
	engine := &core.Engine{Repo: memory.NewRepository()}
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	adminToken, err := api.auth.Issue("admin", "tenant_001", "admin", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	result := backupJSONRequest(t, server.Client(), http.MethodPost, server.URL+"/api/v1/backups", adminToken, map[string]any{"type": "FULL"}, http.StatusBadGateway)
	detail, _ := result["detail"].(string)
	if !bytes.Contains([]byte(detail), []byte("pg_dump exited with status 1")) {
		t.Fatalf("backup upstream detail was lost: %q", detail)
	}
}

func backupJSONRequest(t *testing.T, client *http.Client, method, endpoint, token string, body any, wantStatus int) map[string]any {
	t.Helper()
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequest(method, endpoint, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result map[string]any
	if err = json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode %s: %v", endpoint, err)
	}
	if response.StatusCode != wantStatus {
		t.Fatalf("%s status=%d want=%d body=%#v", endpoint, response.StatusCode, wantStatus, result)
	}
	return result
}
