package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"iot-platform/internal/protocolcatalog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestCatalogSigningCommand(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("Go compiler unavailable")
	}
	root := t.TempDir()
	binary := filepath.Join(root, "catalog-sign")
	if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	key := filepath.Join(root, "private.key")
	output, err := exec.Command(binary, "--generate-key", "--key", key).CombinedOutput()
	if err != nil {
		t.Fatal(err, string(output))
	}
	if _, err := exec.Command(binary, "--generate-key", "--key", key).CombinedOutput(); err == nil {
		t.Fatal("existing signing key overwritten")
	}
	payload, _ := json.Marshal(protocolcatalog.Payload{IssuedAt: time.Now().UnixMilli(), ExpiresAt: time.Now().Add(time.Hour).UnixMilli(), Entries: []protocolcatalog.Entry{}})
	input := filepath.Join(root, "payload.json")
	os.WriteFile(input, payload, 0600)
	target := filepath.Join(root, "catalog.json")
	command := func() *exec.Cmd {
		return exec.Command(binary, "--key", key, "--key-id", "release-1", "--input", input, "--output", target)
	}
	if out, err := command().CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	var signed protocolcatalog.Envelope
	data, _ := os.ReadFile(target)
	if json.Unmarshal(data, &signed) != nil {
		t.Fatal("invalid envelope")
	}
	encoded, _ := os.ReadFile(key)
	private, _ := base64.StdEncoding.DecodeString(string(encoded))
	signature, _ := base64.StdEncoding.DecodeString(signed.Signature)
	if !ed25519.Verify(ed25519.PrivateKey(private).Public().(ed25519.PublicKey), payload, signature) {
		t.Fatal("command did not sign exact payload")
	}
	if _, err := command().CombinedOutput(); err == nil {
		t.Fatal("existing signed catalog overwritten")
	}
}
