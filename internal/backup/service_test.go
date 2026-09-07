package backup

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDockerToolCommandsReachHostDependencies(t *testing.T) {
	t.Setenv("IOT_BACKUP_TOOL_MODE", "docker")

	pg := pgDumpCommand(context.Background(), "postgres://iot:example@127.0.0.1:15432/iot?sslmode=disable")
	pgArgs := strings.Join(pg.Args, " ")
	wantHost := "host.docker.internal"
	if runtime.GOOS == "linux" {
		wantHost = "127.0.0.1"
	}
	if !strings.Contains(pgArgs, wantHost+":15432") || !strings.Contains(pgArgs, "pg_dump") {
		t.Fatalf("pg_dump Docker command does not target the host: %v", pg.Args)
	}

	rdbPath := filepath.Join(t.TempDir(), "redis.rdb")
	redis := redisCLICommand(context.Background(), "127.0.0.1", "16379", []string{"-h", "127.0.0.1", "-p", "16379", "--rdb", rdbPath})
	redisArgs := strings.Join(redis.Args, " ")
	if !strings.Contains(redisArgs, wantHost) || !strings.Contains(redisArgs, "--volume") || !strings.Contains(redisArgs, "/backup/redis.rdb") {
		t.Fatalf("redis Docker command does not mount its output: %v", redis.Args)
	}
}

func TestBackupIDAcceptedByWeaviate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID string `json:"id"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || !regexp.MustCompile(`^[a-z0-9_-]+$`).MatchString(body.ID) {
			http.Error(w, "invalid backup id", 422)
		}
	}))
	defer server.Close()
	s := &Service{cfg: Config{WeaviateURL: server.URL}}
	now := time.Date(2026, 9, 7, 8, 36, 44, 628000000, time.UTC)
	if err := s.weaviateBackup(context.Background(), newBackupID("FULL", now)); err != nil {
		t.Fatal(err)
	}
	if newBackupID("FULL", now) == newBackupID("FULL", now.Add(time.Nanosecond)) {
		t.Fatal("backup IDs collide")
	}
}
