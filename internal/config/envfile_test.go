package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadEnvFileLocalConfiguration(t *testing.T) {
	for _, key := range []string{"IOT_TEST_DSN", "IOT_TEST_LITERAL", "IOT_TEST_EMPTY", "IOT_TEST_OVERRIDE", "IOT_TEST_LAST"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("IOT_TEST_OVERRIDE", "from-process")
	t.Setenv("IOT_TEST_EMPTY", "")
	path := filepath.Join(t.TempDir(), "local.env")
	contents := "\ufeff# local config\r\nIOT_TEST_DSN=postgres://iot:abc@127.0.0.1:15432/iot?sslmode=disable\r\n" +
		"IOT_TEST_LITERAL='a$HOME#b=c' # literal secret\nIOT_TEST_EMPTY=file-value\nIOT_TEST_OVERRIDE=file-value\n" +
		"IOT_TEST_LAST=old\nIOT_TEST_LAST=\"new value\"\n"
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	if err := LoadEnvFile(path); err != nil {
		t.Fatal(err)
	}
	for key, expected := range map[string]string{
		"IOT_TEST_DSN":     "postgres://iot:abc@127.0.0.1:15432/iot?sslmode=disable",
		"IOT_TEST_LITERAL": "a$HOME#b=c", "IOT_TEST_EMPTY": "", "IOT_TEST_OVERRIDE": "from-process", "IOT_TEST_LAST": "new value",
	} {
		if os.Getenv(key) != expected {
			t.Errorf("unexpected value for %s", key)
		}
	}
}

func TestLoadEnvFileRejectsInvalidInputWithoutLeakingValues(t *testing.T) {
	for _, invalid := range []string{"bad line secret-value", "9KEY=secret-value", "IOT_TEST_BAD='secret-value", "IOT_TEST_BAD=\"secret-value\" trailing", "IOT_TEST_BAD=secret-value\x00"} {
		t.Run(invalid[:4], func(t *testing.T) {
			t.Setenv("IOT_TEST_ATOMIC", "")
			if err := os.Unsetenv("IOT_TEST_ATOMIC"); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "bad.env")
			if err := os.WriteFile(path, []byte("IOT_TEST_ATOMIC=must-not-load\n"+invalid), 0600); err != nil {
				t.Fatal(err)
			}
			err := LoadEnvFile(path)
			if err == nil || !strings.Contains(err.Error(), "line 2") {
				t.Fatalf("expected line error, got %v", err)
			}
			if strings.Contains(err.Error(), "secret-value") {
				t.Fatal("error leaked a secret")
			}
			if _, exists := os.LookupEnv("IOT_TEST_ATOMIC"); exists {
				t.Fatal("invalid file was partially applied")
			}
		})
	}
}

func TestLoadEnvFileRequiresExistingFile(t *testing.T) {
	if err := LoadEnvFile(filepath.Join(t.TempDir(), "missing.env")); err == nil {
		t.Fatal("missing file accepted")
	}
}
