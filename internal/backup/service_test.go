package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func snapshotFixture(entries map[string][]byte) harnessSnapshot {
	s := harnessSnapshot{FormatVersion: 1}
	for path, body := range entries {
		hash := sha256.Sum256(body)
		s.Entries = append(s.Entries, harnessSnapshotEntry{Path: path, Base64: base64.StdEncoding.EncodeToString(body), SHA256: hex.EncodeToString(hash[:]), Size: int64(len(body))})
		s.TotalBytes += int64(len(body))
	}
	s.FileCount = int64(len(s.Entries))
	return s
}

func TestHarnessClusterSnapshotIsAuthenticatedConsistentAndRestorable(t *testing.T) {
	plugin := []byte(`{"id":"fire-operator","kind":"agent"}`)
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-IOT-Harness-Token") != "server-only" {
			t.Error("snapshot missing server token")
			w.WriteHeader(401)
			return
		}
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(409)
			return
		}
		json.NewEncoder(w).Encode(snapshotFixture(map[string][]byte{"plugins/fire-operator.json": plugin, "sessions/project/session/session.jsonl": []byte("first instance session")}))
	}))
	defer server.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(snapshotFixture(map[string][]byte{"plugins/fire-operator.json": plugin, "sessions/project/session/session.v2.jsonl.zstd": []byte("second instance session")}))
	}))
	defer second.Close()
	s := &Service{cfg: Config{HarnessToken: "server-only", HarnessSnapshotURLs: []string{server.URL + "/v1/backup/snapshot", second.URL + "/v1/backup/snapshot"}}}
	archive := filepath.Join(t.TempDir(), "harness.tar.gz")
	files, size, instances, err := s.archivePersistentAgents(context.Background(), archive)
	if err != nil || files != 4 || instances != 2 || calls != 2 {
		t.Fatalf("files=%d instances=%d calls=%d err=%v", files, instances, calls, err)
	}
	target := filepath.Join(t.TempDir(), "isolated")
	restored, bytes, err := restoreHarnessArchive(context.Background(), archive, target)
	if err != nil || restored != files || bytes != size {
		t.Fatal("snapshot did not restore", err)
	}
	for _, p := range []string{"instances/000/plugins/fire-operator.json", "instances/001/sessions/project/session/session.v2.jsonl.zstd"} {
		if _, err = os.Stat(filepath.Join(target, p)); err != nil {
			t.Fatal(err)
		}
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(snapshotFixture(map[string][]byte{"plugins/fire-operator.json": []byte(`{"id":"different"}`)}))
	}))
	defer bad.Close()
	s.cfg.HarnessSnapshotURLs = []string{server.URL + "/v1/backup/snapshot", bad.URL + "/v1/backup/snapshot"}
	if _, _, _, err = s.archivePersistentAgents(context.Background(), filepath.Join(t.TempDir(), "bad.tar.gz")); err == nil {
		t.Fatal("accepted inconsistent cluster Agent manifests")
	}
}

func TestHarnessSnapshotRejectsSecretsPathsAndCorruptData(t *testing.T) {
	for _, path := range []string{"runtime-home/.env", "providers.json", "plugins/../secret.json", "sessions/project/session/.env", "sessions/project/session/session.v01.jsonl", "plugins/agent.json\n"} {
		if validHarnessSnapshotPath(path) {
			t.Fatalf("accepted %q", path)
		}
	}
	for _, mutate := range []func(*harnessSnapshot){
		func(s *harnessSnapshot) { s.Entries[0].SHA256 = strings.Repeat("0", 64) },
		func(s *harnessSnapshot) { s.TotalBytes++ },
		func(s *harnessSnapshot) { s.Entries[0].Path = "providers.json" },
	} {
		snap := snapshotFixture(map[string][]byte{"plugins/agent.json": []byte("test")})
		mutate(&snap)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(snap) }))
		s := &Service{cfg: Config{HarnessToken: "token", HarnessSnapshotURLs: []string{server.URL + "/v1/backup/snapshot"}}}
		_, _, _, err := s.archivePersistentAgents(context.Background(), filepath.Join(t.TempDir(), "bad.tar.gz"))
		server.Close()
		if err == nil {
			t.Fatal("accepted corrupt snapshot")
		}
	}
}

func TestMessageQueriesFullAndDaily(t *testing.T) {
	loc := time.FixedZone("Asia/Shanghai", 8*3600)
	start := time.Date(2026, 9, 7, 0, 0, 0, 0, loc)
	for _, parsed := range []bool{false, true} {
		pg, args, ch := messageQueries(time.Time{}, time.Now(), parsed)
		if strings.Contains(pg, "WHERE") || strings.Contains(ch, "WHERE") || len(args) != 0 {
			t.Fatal("full backup must include all saved device timestamps")
		}
		pg, args, ch = messageQueries(start, start.AddDate(0, 0, 1), parsed)
		if len(args) != 2 || args[0] != start.UnixMilli() || args[1] != start.AddDate(0, 0, 1).UnixMilli() {
			t.Fatalf("incorrect daily window: %v", args)
		}
		if !strings.Contains(pg, " >= $1") || !strings.Contains(pg, " < $2") || !strings.Contains(ch, fmt.Sprint(start.UnixMilli())) {
			t.Fatal("daily window must be half open and preserve timezone")
		}
		if parsed && (!strings.Contains(pg, "standard_message") || !strings.Contains(pg, "processed_at") || !strings.Contains(ch, "iot_telemetry")) {
			t.Fatal("parsed data source missing")
		}
		if !parsed && (!strings.Contains(pg, "raw_message_log") || !strings.Contains(ch, "iot_raw_message")) {
			t.Fatal("raw data source missing")
		}
	}
}

func TestPersistentAgentArchiveRoundTripAndUnsafeFiles(t *testing.T) {
	root := t.TempDir()
	plugin := filepath.Join(root, "plugins")
	if err := os.MkdirAll(plugin, 0700); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"name":"消防巡检","knowledge":"workflow-scoped"}`)
	if err := os.WriteFile(filepath.Join(plugin, "operator-agent.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "runtime-home"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "runtime-home", "providers.json"), []byte(`{"apiKey":"must-never-enter-artifacts"}`), 0600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "harness.tar.gz")
	files, size, err := archiveHarness(context.Background(), root, archive)
	if err != nil || files != 1 || size != int64(len(body)) {
		t.Fatalf("archive files=%d bytes=%d err=%v", files, size, err)
	}
	destination := filepath.Join(t.TempDir(), "restored")
	gotFiles, gotSize, err := restoreHarnessArchive(context.Background(), archive, destination)
	if err != nil || gotFiles != files || gotSize != size {
		t.Fatalf("restore files=%d bytes=%d err=%v", gotFiles, gotSize, err)
	}
	got, err := os.ReadFile(filepath.Join(destination, "plugins", "operator-agent.json"))
	if err != nil || !bytes.Equal(got, body) {
		t.Fatal("persistent Agent content changed")
	}
	if _, err = os.Stat(filepath.Join(destination, "runtime-home")); !os.IsNotExist(err) {
		t.Fatal("Provider configuration entered persistent Agent backup")
	}
	if _, _, err = restoreHarnessArchive(context.Background(), archive, destination); err == nil {
		t.Fatal("restoration overwrote an existing directory")
	}
	if err = os.Symlink(filepath.Join(t.TempDir(), "outside"), filepath.Join(plugin, "link")); err != nil {
		t.Skip("symlinks unavailable")
	}
	if _, _, err = archiveHarness(context.Background(), root, filepath.Join(t.TempDir(), "bad.tar.gz")); err == nil {
		t.Fatal("backup followed a symbolic link")
	}
}

func TestClickHouseExportPreservesPayloads(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		unwrap     bool
	}{
		{"raw", `{"body":"{\"messageId\":\"raw1\",\"payload\":\"0102\"}"}`, true},
		{"parsed", `{"message_id":"parsed1","properties":{"temperature":42}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			s := &Service{cfg: Config{ClickHouseURL: server.URL}}
			var output bytes.Buffer
			count, err := s.exportClickHouseRows(context.Background(), "SELECT", tc.unwrap, json.NewEncoder(&output))
			if err != nil || count != 1 {
				t.Fatalf("count=%d err=%v", count, err)
			}
			var record rawLogRecord
			if err = json.Unmarshal(output.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			if record.Storage != "clickhouse" || !json.Valid(record.Message) {
				t.Fatal("invalid record", output.String())
			}
			if tc.unwrap && !bytes.Contains(record.Message, []byte("0102")) {
				t.Fatal("lost raw payload")
			}
			if !tc.unwrap && !bytes.Contains(record.Message, []byte("temperature")) {
				t.Fatal("lost parsed properties")
			}
		})
	}
}

func TestClickHouseExportFailsOnCorruptData(t *testing.T) {
	for _, body := range []string{`{"body":"not json"}`, `{"body":`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		s := &Service{cfg: Config{ClickHouseURL: server.URL}}
		var output bytes.Buffer
		_, err := s.exportClickHouseRows(context.Background(), "SELECT", true, json.NewEncoder(&output))
		server.Close()
		if err == nil {
			t.Fatal("corrupt backup reported success")
		}
	}
}
