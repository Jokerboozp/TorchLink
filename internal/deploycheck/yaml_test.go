package deploycheck

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDeploymentYAMLParses(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	files := []string{"compose.yaml", "compose.offline.yaml", filepath.Join("deploy", "k8s", "platform.yaml"), filepath.Join("deploy", "k8s", "backup-cronjob.yaml"), filepath.Join("ops", "prometheus", "prometheus.yml"), filepath.Join("ops", "prometheus", "alerts.yml"), filepath.Join("ops", "loki", "loki.yml")}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			f, err := os.Open(filepath.Join(root, name))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			decoder := yaml.NewDecoder(f)
			documents := 0
			for {
				var doc any
				err = decoder.Decode(&doc)
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				documents++
			}
			if documents == 0 {
				t.Fatal("no YAML documents")
			}
		})
	}
}

func TestProductionComposeDoesNotInjectAuthenticationFallbacks(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	content, err := os.ReadFile(filepath.Join(root, "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`\$\{IOT_(?:JWT_SECRET|ADMIN_PASSWORD):-([^}]*)\}`)
	matches := pattern.FindAllSubmatch(content, -1)
	for _, match := range matches {
		if len(match) == 2 && len(match[1]) != 0 {
			t.Fatal("production compose injects a fixed authentication fallback")
		}
	}
	for _, variable := range [][]byte{[]byte("${IOT_JWT_SECRET:?"), []byte("${IOT_ADMIN_PASSWORD:?")} {
		if !bytes.Contains(content, variable) {
			t.Fatal("production compose must require authentication variables")
		}
	}
}

func TestBackupServiceFollowsComposeLifecycle(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	content, err := os.ReadFile(filepath.Join(root, "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	var document struct {
		Services map[string]struct {
			Profiles []string `yaml:"profiles"`
			Restart  string   `yaml:"restart"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(content, &document); err != nil {
		t.Fatal(err)
	}
	backup, ok := document.Services["backup-service"]
	if !ok {
		t.Fatal("compose.yaml must define backup-service")
	}
	if len(backup.Profiles) != 0 {
		t.Fatalf("backup-service must start with the main system, got profiles %v", backup.Profiles)
	}
	if backup.Restart != "unless-stopped" {
		t.Fatalf("backup-service must follow the main system restart policy, got %q", backup.Restart)
	}
}

func TestLocalComposeKeepsBackupServiceInExplicitProfile(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	content, err := os.ReadFile(filepath.Join(root, "compose.local.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Services map[string]struct {
			Profiles []string `yaml:"profiles"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(content, &document); err != nil {
		t.Fatal(err)
	}
	backup, ok := document.Services["backup-service"]
	if !ok {
		t.Fatal("compose.local.yaml must define backup-service")
	}
	if len(backup.Profiles) != 1 || backup.Profiles[0] != "backup" {
		t.Fatalf("local backup-service must require the backup profile, got %v", backup.Profiles)
	}
}
