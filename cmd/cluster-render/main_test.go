package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"iot-platform/internal/clusterplan"
)

func TestReadSecretInputsKeepsKnownKeysOnly(t *testing.T) {
	in, err := readSecretInputs(strings.NewReader("servicePassword=a=b\r\ndeepseekApiKey=sk-1\nunknown=x\nnoequals\n"))
	if err != nil || in.ServicePassword != "a=b" || in.DeepSeekAPIKey != "sk-1" {
		t.Fatalf("%+v %v", in, err)
	}
}

func TestGenerateAndRenderInventory(t *testing.T) {
	dir := t.TempDir()
	inventory := filepath.Join(dir, "cluster", "inventory.yaml")
	if err := generateInventory(inventory, "demo", "10.0.0.1, 10.0.0.2,10.0.0.3", false, false); err != nil {
		t.Fatal(err)
	}
	if err := generateInventory(inventory, "demo", "10.0.0.1", false, false); err == nil {
		t.Fatal("an existing inventory was overwritten")
	}
	if err := generateInventory("", "demo", "10.0.0.1", false, false); err == nil {
		t.Fatal("missing path accepted")
	}

	// The checked-in example renders with the example secrets; env files are private.
	// Every secret filled, as clusterplan's own tests do; the file must be private.
	v := reflect.ValueOf(&clusterplan.Secrets{}).Elem()
	for i := 0; i < v.NumField(); i++ {
		f := v.Type().Field(i)
		if f.IsExported() && f.Type.Kind() == reflect.String && f.Tag.Get("yaml") != "-" && !strings.HasPrefix(f.Name, "Kafka") && !strings.HasPrefix(f.Name, "TLS") && f.Name != "MQTTPublicURL" && f.Name != "AlertWebhookURL" {
			v.Field(i).SetString("s3cret-" + strings.ToLower(f.Name) + "-0123456789abcdefghij")
		}
	}
	body, err := yaml.Marshal(v.Interface())
	if err != nil {
		t.Fatal(err)
	}
	secrets := filepath.Join(dir, "secrets.yaml")
	if err = os.WriteFile(secrets, body, 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	if err := run("../../deploy/cluster/inventory.example.yaml", secrets, out, false); err != nil {
		t.Fatal(err)
	}
	envFiles := 0
	err = filepath.WalkDir(out, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, _ := d.Info()
		if strings.HasSuffix(path, ".env") {
			envFiles++
			if info.Mode().Perm() != 0o600 {
				t.Errorf("%s has mode %v", path, info.Mode().Perm())
			}
		}
		return nil
	})
	if err != nil || envFiles == 0 {
		t.Fatalf("rendered env files %d %v", envFiles, err)
	}
}
