package backup

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"

	"iot-platform/internal/config"
)

// Opt in against a configured environment. Creates backup files and catalog
// records, but does not modify device data or restore anything.
func TestDeviceBackupIntegration(t *testing.T) {
	path := os.Getenv("IOT_BACKUP_TEST_ENV")
	if path == "" {
		t.Skip("set IOT_BACKUP_TEST_ENV to create and verify a real device backup")
	}
	if err := config.LoadEnvFile(path); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	s, err := New(ctx, Config{
		PostgresDSN: os.Getenv("IOT_POSTGRES_DSN"), ClickHouseURL: os.Getenv("IOT_CLICKHOUSE_URL"),
		MinIOEndpoint: os.Getenv("IOT_MINIO_ENDPOINT"), MinIOAccessKey: os.Getenv("IOT_MINIO_ACCESS_KEY"),
		MinIOSecretKey: os.Getenv("IOT_MINIO_SECRET_KEY"), MinIOUseTLS: os.Getenv("IOT_MINIO_USE_TLS") == "true",
		BackupDir: t.TempDir(), BackupTimezone: "Asia/Shanghai",
	})
	if err != nil {
		t.Fatal("backup connections failed; check configured endpoints")
	}
	defer s.Close()
	if err = s.Ready(ctx); err != nil {
		t.Fatal("backup dependencies are not ready")
	}
	manifest, err := s.Run(ctx, "FULL")
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Artifacts) != 3 {
		t.Fatalf("expected only two data files and manifest, got %d", len(manifest.Artifacts))
	}
	for _, artifact := range manifest.Artifacts {
		if artifact.Filename == "manifest.json" {
			continue
		}
		object, _, err := s.OpenArtifact(ctx, manifest.ID, artifact.Filename)
		if err != nil {
			t.Fatal(err)
		}
		gz, err := gzip.NewReader(object)
		if err != nil {
			object.Close()
			t.Fatal(err)
		}
		decoder := json.NewDecoder(gz)
		var count int
		for {
			var row rawLogRecord
			err = decoder.Decode(&row)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if !json.Valid(row.Message) || row.Storage == "" {
				t.Fatal("invalid exported record")
			}
			count++
		}
		gz.Close()
		object.Close()
		t.Logf("%s: %d valid records", artifact.Filename, count)
	}
	if _, err = s.Verify(ctx, manifest.ID); err != nil {
		t.Fatal(err)
	}
	t.Logf("Created and verified device backup %s", manifest.ID)
	daily, err := s.RunDaily(ctx, time.Now().In(s.rawBackupLocation()).AddDate(0, 0, -1))
	if err != nil {
		t.Fatal(err)
	}
	if daily.Type != "DEVICE_DAILY" || len(daily.Artifacts) != 3 {
		t.Fatal("daily backup must include raw and parsed data plus manifest")
	}
	if _, err = s.Verify(ctx, daily.ID); err != nil {
		t.Fatal(err)
	}
	t.Logf("Created and verified daily device backup %s", daily.ID)
}
